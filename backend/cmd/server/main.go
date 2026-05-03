package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/jackc/pgx/v5/pgxpool"
	amqp "github.com/rabbitmq/amqp091-go"

	"linkpay-service/internal/handler"
	"linkpay-service/internal/messaging"
	"linkpay-service/internal/middleware"
	"linkpay-service/internal/repository"
	"linkpay-service/internal/service"
	"linkpay-service/internal/storage"
	"linkpay-service/pkg/jwt"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	// --- Banco de dados ---
	dbURL := mustEnv("DATABASE_URL")
	poolCfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		log.Fatalf("parse database url: %v", err)
	}
	poolCfg.MaxConns = 20
	poolCfg.MinConns = 2

	ctx := context.Background()
	db, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		log.Fatalf("connect db: %v", err)
	}
	defer db.Close()

	if err := db.Ping(ctx); err != nil {
		log.Fatalf("ping db: %v", err)
	}
	logger.Info("banco de dados conectado")

	// --- MinIO ---
	minioEndpoint := mustEnv("MINIO_ENDPOINT")
	minioKey := mustEnv("MINIO_ACCESS_KEY")
	minioSecret := mustEnv("MINIO_SECRET_KEY")
	minioSecure := os.Getenv("MINIO_USE_SSL") == "true"
	minioClient, err := storage.NewMinIOClient(minioEndpoint, minioKey, minioSecret, minioSecure)
	if err != nil {
		log.Fatalf("init minio: %v", err)
	}
	logger.Info("MinIO conectado", "endpoint", minioEndpoint)

	// --- RabbitMQ (opcional) ---
	var publisher *messaging.Publisher
	if rabbitURL := os.Getenv("RABBITMQ_URL"); rabbitURL != "" {
		conn, amqpErr := amqp.Dial(rabbitURL)
		if amqpErr != nil {
			logger.Warn("RabbitMQ indisponível — continuando sem mensageria", "error", amqpErr)
		} else {
			ch, chErr := conn.Channel()
			if chErr != nil {
				logger.Warn("falha ao abrir channel RabbitMQ", "error", chErr)
			} else {
				publisher = messaging.NewPublisher(ch, logger)
				logger.Info("RabbitMQ conectado")
				defer conn.Close()
			}
		}
	}

	// --- Repositories ---
	linkPayRepo := repository.NewLinkPayRepository(db)
	payoutRepo := repository.NewPayoutRepository(db)

	// --- Services ---
	asaasBaseURL := getEnvOrDefault("ASAAS_BASE_URL", "https://sandbox.asaas.com/api/v3")
	asaasAPIKey := mustEnv("ASAAS_API_KEY")
	notificationURL := os.Getenv("NOTIFICATION_SERVICE_URL")
	notificationToken := os.Getenv("SERVICE_TOKEN")
	baseURL := getEnvOrDefault("BASE_URL", "https://pay.institutoitinerante.com.br")

	linkPaySvc := service.NewLinkPayService(
		linkPayRepo,
		publisher,
		asaasBaseURL,
		asaasAPIKey,
		notificationURL,
		notificationToken,
		baseURL,
	)

	payoutSvc := service.NewPayoutService(payoutRepo, asaasBaseURL, asaasAPIKey, logger)

	subscriptionSvc := service.NewSubscriptionService(linkPayRepo, asaasBaseURL, asaasAPIKey)

	// --- Handlers ---
	webhookSecret := os.Getenv("ASAAS_WEBHOOK_SECRET")
	jwtSecret := mustEnv("JWT_SECRET")

	linkPayHandler := handler.NewLinkPayHandler(linkPaySvc, webhookSecret, minioClient)
	payoutHandler := handler.NewPayoutHandler(payoutSvc)
	subscriptionHandler := handler.NewSubscriptionHandler(subscriptionSvc)

	// --- Fiber app ---
	app := fiber.New(fiber.Config{
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		BodyLimit:    150 * 1024 * 1024, // 150 MB para uploads
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			code := fiber.StatusInternalServerError
			if e, ok := err.(*fiber.Error); ok {
				code = e.Code
			}
			return c.Status(code).JSON(fiber.Map{"error": err.Error()})
		},
	})

	app.Use(recover.New())
	app.Use(cors.New(cors.Config{
		AllowOrigins: getEnvOrDefault("CORS_ORIGINS", "*"),
		AllowHeaders: "Origin, Content-Type, Accept, Authorization",
		AllowMethods: "GET, POST, PUT, DELETE, OPTIONS",
	}))

	// --- Rotas ---
	authMiddleware := middleware.JWTAuth(jwt.NewJWTService(jwtSecret))

	// Públicas
	pub := app.Group("/api/v1/public")
	pub.Get("/products/:slug", linkPayHandler.GetPublicProduct)
	pub.Post("/products/:slug/purchase", linkPayHandler.InitiatePurchase)
	pub.Get("/download/:token", linkPayHandler.DownloadProduct)
	pub.Get("/download/:token/file/:fileId", linkPayHandler.DownloadProductFile)
	pub.Get("/download/:token/zip", linkPayHandler.DownloadAllFilesZip)
	pub.Post("/products/:id/subscribe", subscriptionHandler.Subscribe)
	pub.Get("/subscriptions/:token", subscriptionHandler.GetSubscription)
	pub.Get("/subscriber-files", subscriptionHandler.GetSubscriberFiles)
	pub.Post("/subscriptions/:token/cancel", subscriptionHandler.Cancel)

	// Webhook Asaas
	app.Post("/api/v1/linkpay/webhook", linkPayHandler.HandleWebhook)

	// Autenticados (creator/admin)
	auth := app.Group("/api/v1/linkpay", authMiddleware)
	auth.Post("/products", linkPayHandler.CreateProduct)
	auth.Get("/products", linkPayHandler.ListProducts)
	auth.Get("/products/:id", linkPayHandler.GetProduct)
	auth.Put("/products/:id", linkPayHandler.UpdateProduct)
	auth.Delete("/products/:id", linkPayHandler.DeactivateProduct)
	auth.Post("/products/:id/upload", linkPayHandler.UploadProductFile)
	auth.Get("/products/:id/files", linkPayHandler.ListProductFiles)
	auth.Delete("/products/:id/files/:fileId", linkPayHandler.DeleteProductFile)
	auth.Post("/products/:id/plans", subscriptionHandler.CreatePlan)
	auth.Get("/products/:id/plans", subscriptionHandler.ListPlans)
	auth.Put("/products/:id/plans/:planId", subscriptionHandler.UpdatePlan)
	auth.Delete("/products/:id/plans/:planId", subscriptionHandler.DeletePlan)

	// Admin
	admin := app.Group("/api/v1/admin/linkpay", authMiddleware)
	admin.Get("/stats", linkPayHandler.GetStats)
	admin.Get("/balance", payoutHandler.GetBalance)
	admin.Get("/payouts", payoutHandler.ListPayouts)
	admin.Post("/payouts/request", payoutHandler.RequestPayout)
	admin.Get("/anticipation/preview", payoutHandler.GetAnticipationPreview)
	admin.Post("/anticipation/request", payoutHandler.RequestAnticipation)

	// Health
	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok", "service": "linkpay-service"})
	})

	// --- Graceful shutdown ---
	port := getEnvOrDefault("PORT", "3020")
	go func() {
		logger.Info("linkpay-service iniciado", "port", port)
		if err := app.Listen(fmt.Sprintf(":%s", port)); err != nil {
			logger.Error("server error", "error", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.Info("shutting down linkpay-service...")
	_ = app.Shutdown()
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("env %s é obrigatória", key)
	}
	return v
}

func getEnvOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
