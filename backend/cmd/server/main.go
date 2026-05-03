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
	minioClient, err := storage.NewMinIOClient()
	if err != nil {
		log.Fatalf("init minio: %v", err)
	}
	logger.Info("MinIO conectado")

	// --- RabbitMQ (opcional) ---
	var publisher *messaging.Publisher
	if rabbitURL := os.Getenv("RABBITMQ_URL"); rabbitURL != "" {
		pub, pubErr := messaging.NewPublisher(rabbitURL)
		if pubErr != nil {
			logger.Warn("RabbitMQ indisponível — continuando sem mensageria", "error", pubErr)
		} else {
			publisher = pub
			logger.Info("RabbitMQ conectado")
		}
	}

	// --- Repositories ---
	linkPayRepo := repository.NewLinkPayRepository(db)
	payoutRepo := repository.NewPayoutRepository(db)
	subscriptionRepo := repository.NewSubscriptionRepository(db)

	// --- Services ---
	asaasBaseURL := getEnvOrDefault("ASAAS_BASE_URL", "https://sandbox.asaas.com/api/v3")
	asaasAPIKey := mustEnv("ASAAS_API_KEY")

	asaasClient := service.NewAsaasClient(asaasBaseURL, asaasAPIKey, logger)

	linkPaySvc := service.NewLinkPayService(linkPayRepo, asaasClient, publisher, nil)

	payoutSvc := service.NewPayoutService(payoutRepo, asaasClient)

	subscriptionSvc := service.NewSubscriptionService(subscriptionRepo, linkPayRepo, asaasClient)

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
	pub.Get("/subscriptions/:token", subscriptionHandler.GetSubscriptionStatus)
	pub.Post("/subscriptions/:token/cancel", subscriptionHandler.CancelSubscription)
	pub.Post("/products/:slug/cart", linkPayHandler.CreateCart) // BKL-1221: upsell via cart-service

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
	auth.Put("/products/:id/plans/:plan_id", subscriptionHandler.UpdatePlan)
	auth.Delete("/products/:id/plans/:plan_id", subscriptionHandler.DeactivatePlan)

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
