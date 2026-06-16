// @title           LinkPay API
// @version         1.0
// @description     Links de pagamento avulso, comissões e splits — Instituto Itinerante de Tecnologia
// @host            linkpay-service:3020
// @BasePath        /
// @securityDefinitions.apikey BearerAuth
// @in              header
// @name            Authorization
// @description     JWT Bearer token — formato: "Bearer <token>"
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
	"github.com/redis/go-redis/v9"
	fiberswagger "github.com/swaggo/fiber-swagger"

	_ "linkpay-service/docs/swagger"
	"linkpay-service/internal/client"
	"linkpay-service/internal/handler"
	"linkpay-service/internal/messaging"
	"linkpay-service/internal/middleware"
	"linkpay-service/internal/pkg/pulse"
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

	// --- Pulse (métricas fire-and-forget) — antes do AsaasClient pois é injetado nele ---
	pulseClient := pulse.New()

	// --- Redis (distributed lock para payout — evita race em múltiplas réplicas) ---
	redisURL := getEnvOrDefault("REDIS_URL", "redis://localhost:6379/0")
	redisOpts, err := redis.ParseURL(redisURL)
	if err != nil {
		log.Fatalf("parse redis url: %v", err)
	}
	payoutRedisClient := redis.NewClient(redisOpts)
	defer payoutRedisClient.Close()

	// --- Services ---
	asaasBaseURL := getEnvOrDefault("ASAAS_BASE_URL", "https://sandbox.asaas.com/api/v3")
	asaasAPIKey := mustEnv("ASAAS_API_KEY")

	asaasClient := service.NewAsaasClient(asaasBaseURL, asaasAPIKey, logger, pulseClient)

	linkPaySvc := service.NewLinkPayServiceWithNotifier(linkPayRepo, asaasClient, publisher, nil, client.NewNotificationClient())

	payoutSvc := service.NewPayoutService(payoutRepo, asaasClient, payoutRedisClient)

	subscriptionSvc := service.NewSubscriptionService(subscriptionRepo, linkPayRepo, asaasClient)

	// --- Handlers ---
	webhookSecret := os.Getenv("ASAAS_WEBHOOK_SECRET")
	jwtSecret := mustEnv("JWT_SECRET")

	// BKL-1252: Pulse client — fire-and-forget, compartilhado entre handlers e AsaasClient.

	linkPayHandler := handler.NewLinkPayHandler(linkPaySvc, webhookSecret, minioClient, pulseClient)
	payoutHandler := handler.NewPayoutHandler(payoutSvc, pulseClient)
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
	app.Use(middleware.SecurityHeaders())
	app.Use(cors.New(cors.Config{
		AllowOrigins: getEnvOrDefault("CORS_ORIGINS", "http://localhost:5173"),
		AllowHeaders: "Origin, Content-Type, Accept, Authorization",
		AllowMethods: "GET, POST, PUT, DELETE, OPTIONS",
	}))

	// --- Rotas ---
	authMiddleware := middleware.JWTAuth(jwt.NewJWTService(jwtSecret))

	// Página pública HTML compartilhável (issue #14)
	app.Get("/p/:slug", linkPayHandler.RenderPublicProductPage)

	// Públicas — rate limit 60 req/min por IP (SEC-012)
	rl := middleware.NewRateLimiter(60, time.Minute)
	pub := app.Group("/api/v1/public", rl.Handler())
	pub.Get("/products/:slug", linkPayHandler.GetPublicProduct)
	pub.Post("/products/:slug/purchase", linkPayHandler.InitiatePurchase)
	pub.Get("/download/:token", linkPayHandler.DownloadProduct)
	pub.Get("/download/:token/file/:fileId", linkPayHandler.DownloadProductFile)
	pub.Get("/download/:token/zip", linkPayHandler.DownloadAllFilesZip)
	pub.Post("/products/:id/subscribe", subscriptionHandler.Subscribe)
	pub.Get("/subscriptions/:token", subscriptionHandler.GetSubscriptionStatus)
	pub.Post("/subscriptions/:token/cancel", subscriptionHandler.CancelSubscription)
	pub.Post("/products/:slug/cart", linkPayHandler.CreateCart) // BKL-1221: upsell via cart-service
	pub.Post("/purchases/:id/refund", linkPayHandler.RefundPurchase) // CDC Art. 49: direito de arrependimento

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
	auth.Post("/products/:id/cover", linkPayHandler.UploadProductCover)
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

	// BKL-1515: commission ledger — auditoria financeira
	admin.Get("/commission-summary", linkPayHandler.GetCommissionSummary)

	// Swagger UI
	app.Get("/swagger/*", fiberswagger.WrapHandler)

	// Health
	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok", "service": "linkpay-service"})
	})

	// --- Job diário de repasse (06h BRT) ---
	// Issue #7: RunDailyPayouts precisa ser disparado todo dia, senão criadores nunca recebem.
	startDailyPayoutJob(payoutSvc, pulseClient, logger)

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

// startDailyPayoutJob agenda RunDailyPayouts para rodar todos os dias às 06h BRT.
// Em caso de falha, dispara evento Pulse para alertar o time on-call.
func startDailyPayoutJob(payoutSvc *service.PayoutService, pulseClient *pulse.Client, logger *slog.Logger) {
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		logger.Error("falha ao carregar fuso America/Sao_Paulo — caindo para UTC", "error", err)
		loc = time.UTC
	}

	go func() {
		for {
			now := time.Now().In(loc)
			next := time.Date(now.Year(), now.Month(), now.Day(), 6, 0, 0, 0, loc)
			if !next.After(now) {
				next = next.Add(24 * time.Hour)
			}
			wait := time.Until(next)
			logger.Info("payout diário agendado", "proximo_run_brt", next.Format(time.RFC3339), "em", wait.String())
			time.Sleep(wait)

			runCtx := context.Background()
			logger.Info("payout diário iniciando")
			if err := payoutSvc.RunDailyPayouts(runCtx); err != nil {
				logger.Error("payout diário falhou", "error", err)
				pulseClient.Track(runCtx, "linkpay.payout.daily.failed", "", map[string]string{
					"error": err.Error(),
				})
				continue
			}
			logger.Info("payout diário concluído com sucesso")
			pulseClient.Track(runCtx, "linkpay.payout.daily.success", "", nil)
		}
	}()
}

func getEnvOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// newWebhookRedisClient cria o cliente Redis para idempotência do webhook Asaas (#8).
// Retorna nil em caso de URL inválida ou ping falho — o handler segue em fail-open.
func newWebhookRedisClient(ctx context.Context, logger *slog.Logger) *redis.Client {
	redisURL := getEnvOrDefault("REDIS_URL", "redis://localhost:6379/0")
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		logger.Warn("REDIS_URL inválido — webhook seguirá sem idempotência", "error", err)
		return nil
	}
	client := redis.NewClient(opts)
	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		logger.Warn("Redis indisponível — webhook seguirá sem idempotência", "error", err)
		_ = client.Close()
		return nil
	}
	logger.Info("Redis conectado (idempotência de webhook)")
	return client
}
