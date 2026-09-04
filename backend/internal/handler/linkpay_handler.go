package handler

import (
	"archive/zip"
	"context"
	"crypto/hmac"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"linkpay-service/internal/client"
	"linkpay-service/internal/domain"
	"linkpay-service/internal/pkg/pulse"
	"linkpay-service/internal/storage"
)

// LinkPayServiceInterface define os métodos do service usados pelo handler.
type LinkPayServiceInterface interface {
	CreateProduct(ctx context.Context, tenantID string, req *domain.CreateDigitalProductRequest) (*domain.DigitalProduct, error)
	GetProductBySlug(ctx context.Context, slug string) (*domain.DigitalProduct, error)
	GetProductByID(ctx context.Context, id, tenantID string) (*domain.DigitalProduct, error)
	ListProducts(ctx context.Context, tenantID string) ([]domain.DigitalProduct, error)
	UpdateProduct(ctx context.Context, id, tenantID string, req *domain.UpdateDigitalProductRequest) (*domain.DigitalProduct, error)
	UpdateProductStorage(ctx context.Context, id, tenantID, storageKey, storageBucket, fileName string, fileSizeBytes int64, mimeType string) error
	DeactivateProduct(ctx context.Context, id, tenantID string) error
	InitiatePurchase(ctx context.Context, slug string, req *domain.InitiatePurchaseRequest) (*domain.DigitalPurchase, error)
	ProcessPaymentConfirmed(ctx context.Context, chargeID string) error
	Download(ctx context.Context, token string) (string, error)
	GetSalesStats(ctx context.Context, tenantID string) (*domain.LinkPayStatsResponse, error)
	// BKL-1206: múltiplos arquivos por produto
	AddProductFile(ctx context.Context, f *domain.DigitalProductFile) error
	ListProductFiles(ctx context.Context, productID string) ([]domain.DigitalProductFile, error)
	GetProductFile(ctx context.Context, fileID string) (*domain.DigitalProductFile, error)
	DeleteProductFile(ctx context.Context, fileID, productID string) error
	GetPurchaseByToken(ctx context.Context, token string) (*domain.DigitalPurchase, error)
	// RefundPurchase estorna uma compra confirmada (CDC Art. 49 — direito de arrependimento em 7 dias).
	RefundPurchase(ctx context.Context, purchaseID, accessToken string) error
	// BKL-1221: upsells
	GetUpsellsByProductID(ctx context.Context, productID string) ([]domain.UpsellSuggestion, error)
	// #16: cover image
	UpdateProductCover(ctx context.Context, id, tenantID, coverImageURL string) error
	// BKL-1515: commission ledger
	GetCommissionSummary(ctx context.Context, startDate, endDate string) (*domain.CommissionSummaryResponse, error)
}

// LinkPayHandler gerencia os endpoints do módulo LinkPay.
type LinkPayHandler struct {
	service         LinkPayServiceInterface
	payoutService   PayoutServiceInterface
	subscriptionSvc SubscriptionServiceInterface // BKL-1208: webhook de assinaturas
	cartClient      *client.CartClient            // BKL-1221: upsell via cart-service
	validator       *validator.Validate
	webhookSecret   string // VUL-001: secret para validar asaas-access-token no webhook
	minioClient     *storage.MinIOClient
	pulseClient     *pulse.Client // BKL-1252: tracking fire-and-forget
	redisClient     *redis.Client // #8: idempotência de webhook via SetNX
}

func NewLinkPayHandler(service LinkPayServiceInterface, webhookSecret string, minioClient *storage.MinIOClient, pulseClient *pulse.Client) *LinkPayHandler {
	return &LinkPayHandler{
		service:       service,
		validator:     validator.New(),
		webhookSecret: webhookSecret,
		minioClient:   minioClient,
		cartClient:    client.NewCartClient(), // BKL-1221: lê CART_SERVICE_URL e SERVICE_TOKEN do env
		pulseClient:   pulseClient,
		redisClient:   newWebhookRedisClient(), // #8: idempotência de webhook
	}
}

// WithPayoutService injeta o PayoutService no handler para processar webhooks de transferência.
func (h *LinkPayHandler) WithPayoutService(svc PayoutServiceInterface) *LinkPayHandler {
	h.payoutService = svc
	return h
}

// WithSubscriptionService injeta o SubscriptionService para processar webhooks de assinatura (BKL-1208).
func (h *LinkPayHandler) WithSubscriptionService(svc SubscriptionServiceInterface) *LinkPayHandler {
	h.subscriptionSvc = svc
	return h
}

// --- Admin (autenticado) ---

// ListProducts godoc
//
//	@Summary      Lista produtos do tenant
//	@Description  Retorna todos os produtos digitais do tenant autenticado.
//	@Tags         produtos
//	@Produce      json
//	@Security     BearerAuth
//	@Success      200  {object}  map[string][]domain.DigitalProduct
//	@Failure      401  {object}  map[string]string
//	@Failure      500  {object}  map[string]string
//	@Router       /api/v1/linkpay/products [get]
func (h *LinkPayHandler) ListProducts(c *fiber.Ctx) error {
	tenantID, err := extractTenantID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "tenant não identificado"})
	}

	products, err := h.service.ListProducts(c.Context(), tenantID.String())
	if err != nil {
		log.Printf("[linkpay] erro ao listar produtos: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "erro ao listar produtos"})
	}

	if products == nil {
		products = []domain.DigitalProduct{}
	}
	return c.JSON(fiber.Map{"products": products})
}

// CreateProduct godoc
//
//	@Summary      Cria produto digital
//	@Description  Cria um novo produto digital para o tenant autenticado.
//	@Tags         produtos
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        body  body      domain.CreateDigitalProductRequest  true  "Dados do produto"
//	@Success      201   {object}  domain.DigitalProduct
//	@Failure      400   {object}  map[string]string
//	@Failure      401   {object}  map[string]string
//	@Failure      500   {object}  map[string]string
//	@Router       /api/v1/linkpay/products [post]
func (h *LinkPayHandler) CreateProduct(c *fiber.Ctx) error {
	tenantID, err := extractTenantID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "tenant não identificado"})
	}

	var req domain.CreateDigitalProductRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "corpo da requisição inválido"})
	}
	if err := h.validator.Struct(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "validação falhou", "details": err.Error()})
	}

	product, err := h.service.CreateProduct(c.Context(), tenantID.String(), &req)
	if err != nil {
		log.Printf("[linkpay] erro ao criar produto: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "erro ao criar produto"})
	}

	// BKL-1252: Pulse tracking — fire-and-forget.
	h.pulseClient.Track(c.Context(), "produto.criado", tenantID.String(), map[string]string{
		"product_id": product.ID,
		"slug":       product.Slug,
	})

	return c.Status(fiber.StatusCreated).JSON(product)
}

// UpdateProduct godoc
//
//	@Summary      Atualiza produto digital
//	@Description  Atualiza os dados de um produto do tenant autenticado.
//	@Tags         produtos
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id    path      string                              true  "ID do produto (UUID)"
//	@Param        body  body      domain.UpdateDigitalProductRequest  true  "Campos a atualizar"
//	@Success      200   {object}  domain.DigitalProduct
//	@Failure      400   {object}  map[string]string
//	@Failure      401   {object}  map[string]string
//	@Failure      500   {object}  map[string]string
//	@Router       /api/v1/linkpay/products/{id} [put]
func (h *LinkPayHandler) UpdateProduct(c *fiber.Ctx) error {
	tenantID, err := extractTenantID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "tenant não identificado"})
	}

	id := c.Params("id")
	if id == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "id obrigatório"})
	}

	var req domain.UpdateDigitalProductRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "corpo da requisição inválido"})
	}
	if err := h.validator.Struct(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "validação falhou", "details": err.Error()})
	}

	product, err := h.service.UpdateProduct(c.Context(), id, tenantID.String(), &req)
	if err != nil {
		log.Printf("[linkpay] erro ao atualizar produto: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "erro ao atualizar produto"})
	}

	return c.JSON(product)
}

// DeleteProduct godoc
//
//	@Summary      Desativa produto digital
//	@Description  Desativa (soft delete) um produto do tenant autenticado.
//	@Tags         produtos
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id  path      string  true  "ID do produto (UUID)"
//	@Success      200  {object}  map[string]string
//	@Failure      400  {object}  map[string]string
//	@Failure      401  {object}  map[string]string
//	@Failure      500  {object}  map[string]string
//	@Router       /api/v1/linkpay/products/{id} [delete]
func (h *LinkPayHandler) DeleteProduct(c *fiber.Ctx) error {
	tenantID, err := extractTenantID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "tenant não identificado"})
	}

	id := c.Params("id")
	if id == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "id obrigatório"})
	}

	if err := h.service.DeactivateProduct(c.Context(), id, tenantID.String()); err != nil {
		log.Printf("[linkpay] erro ao desativar produto: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "erro ao desativar produto"})
	}

	return c.JSON(fiber.Map{"message": "produto desativado"})
}

// UploadProductFile godoc
//
//	@Summary      Upload de arquivo do produto
//	@Description  Faz upload de um arquivo digital para o MinIO (multipart/form-data, campo "file"). Máximo 2 GB.
//	@Tags         produtos
//	@Accept       mpfd
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id    path      string  true   "ID do produto (UUID)"
//	@Param        file  formData  file    true   "Arquivo a enviar"
//	@Success      200   {object}  map[string]interface{}
//	@Failure      400   {object}  map[string]string
//	@Failure      401   {object}  map[string]string
//	@Failure      404   {object}  map[string]string
//	@Failure      413   {object}  map[string]string
//	@Failure      503   {object}  map[string]string
//	@Router       /api/v1/linkpay/products/{id}/upload [post]
func (h *LinkPayHandler) UploadProductFile(c *fiber.Ctx) error {
	if !h.minioClient.IsConfigured() {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "armazenamento de arquivos não configurado"})
	}

	tenantID, err := extractTenantID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "tenant não identificado"})
	}

	productID := c.Params("id")
	if productID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "id do produto obrigatório"})
	}

	// Valida que o produto pertence ao tenant
	if _, err := h.service.GetProductByID(c.Context(), productID, tenantID.String()); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "produto não encontrado"})
	}

	file, err := c.FormFile("file")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "campo 'file' obrigatório"})
	}

	if file.Size > storage.MaxFileSize {
		return c.Status(fiber.StatusRequestEntityTooLarge).JSON(fiber.Map{"error": "arquivo muito grande: máximo 2GB"})
	}

	src, err := file.Open()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "erro ao abrir arquivo"})
	}
	defer src.Close()

	// Detecta MIME type via os primeiros 512 bytes
	buf := make([]byte, 512)
	n, _ := src.Read(buf)
	mimeType := http.DetectContentType(buf[:n])

	// Reconstrói o reader com os bytes lidos + restante
	fullReader := io.MultiReader(
		newBytesReader(buf[:n]),
		src,
	)

	// Gera storage key hierárquica: tenantID/products/productID/uuid.ext
	ext := filepath.Ext(file.Filename)
	storageKey := fmt.Sprintf("%s/products/%s/%s%s", tenantID.String(), productID, uuid.New().String(), ext)

	if err := h.minioClient.UploadFile(c.Context(), storage.LinkPayBucket, storageKey, mimeType, fullReader, file.Size); err != nil {
		log.Printf("[linkpay] erro no upload MinIO: %v", err)
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	if err := h.service.UpdateProductStorage(
		c.Context(), productID, tenantID.String(),
		storageKey, storage.LinkPayBucket, file.Filename, file.Size, mimeType,
	); err != nil {
		log.Printf("[linkpay] erro ao salvar metadados de storage: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "erro ao salvar metadados do arquivo"})
	}

	return c.JSON(fiber.Map{
		"uploaded":         true,
		"file_name":        file.Filename,
		"file_size_bytes":  file.Size,
		"mime_type":        mimeType,
		"storage_key":      storageKey,
	})
}

// UploadProductCover godoc
//
//	@Summary      Upload de imagem de capa do produto
//	@Description  Faz upload da imagem de capa para o MinIO (multipart/form-data, campo "cover").
//	@Tags         produtos
//	@Accept       mpfd
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id     path      string  true  "ID do produto (UUID)"
//	@Param        cover  formData  file    true  "Imagem de capa (JPEG, PNG, GIF)"
//	@Success      200    {object}  map[string]interface{}
//	@Failure      400    {object}  map[string]string
//	@Failure      401    {object}  map[string]string
//	@Failure      404    {object}  map[string]string
//	@Failure      503    {object}  map[string]string
//	@Router       /api/v1/linkpay/products/{id}/cover [post]
func (h *LinkPayHandler) UploadProductCover(c *fiber.Ctx) error {
	if !h.minioClient.IsConfigured() {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "armazenamento de arquivos não configurado"})
	}

	tenantID, err := extractTenantID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "tenant não identificado"})
	}

	productID := c.Params("id")
	if productID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "id do produto obrigatório"})
	}

	// Valida que o produto pertence ao tenant
	if _, err := h.service.GetProductByID(c.Context(), productID, tenantID.String()); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "produto não encontrado"})
	}

	file, err := c.FormFile("cover")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "campo 'cover' obrigatório"})
	}

	if file.Size > 10*1024*1024 {
		return c.Status(fiber.StatusRequestEntityTooLarge).JSON(fiber.Map{"error": "imagem muito grande: máximo 10MB"})
	}

	src, err := file.Open()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "erro ao abrir arquivo"})
	}
	defer src.Close()

	// Detecta MIME type via os primeiros 512 bytes
	buf := make([]byte, 512)
	n, _ := src.Read(buf)
	mimeType := http.DetectContentType(buf[:n])

	// Aceita apenas imagens
	if mimeType != "image/jpeg" && mimeType != "image/png" && mimeType != "image/gif" && mimeType != "image/webp" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "apenas imagens JPEG, PNG, GIF e WebP são permitidas"})
	}

	fullReader := io.MultiReader(newBytesReader(buf[:n]), src)

	ext := filepath.Ext(file.Filename)
	storageKey := fmt.Sprintf("%s/products/%s/cover%s", tenantID.String(), productID, ext)

	if err := h.minioClient.UploadFile(c.Context(), storage.LinkPayBucket, storageKey, mimeType, fullReader, file.Size); err != nil {
		log.Printf("[linkpay] erro no upload da cover image: %v", err)
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	// #40: persiste o storage_key (não a presigned URL, que expira em 15min).
	// A presigned URL é regenerada no momento da leitura em GetPublicProduct.
	if err := h.service.UpdateProductCover(c.Context(), productID, tenantID.String(), storageKey); err != nil {
		log.Printf("[linkpay] erro ao salvar cover_image_url: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "erro ao salvar imagem de capa"})
	}

	// Gera presigned URL apenas para resposta imediata do upload.
	coverImageURL, err := h.minioClient.PresignedURL(c.Context(), storage.LinkPayBucket, storageKey)
	if err != nil {
		log.Printf("[linkpay] erro ao gerar presigned URL da cover: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "erro ao gerar link da imagem"})
	}

	return c.JSON(fiber.Map{
		"uploaded":        true,
		"cover_image_url": coverImageURL,
	})
}

// --- Público ---

// GetPublicProduct godoc
//
//	@Summary      Retorna produto público por slug
//	@Description  Retorna os dados públicos de um produto digital pelo slug, incluindo arquivos adicionais.
//	@Tags         público
//	@Produce      json
//	@Param        slug  path      string  true  "Slug do produto"
//	@Success      200   {object}  domain.DigitalProduct
//	@Failure      400   {object}  map[string]string
//	@Failure      404   {object}  map[string]string
//	@Router       /api/v1/public/products/{slug} [get]
func (h *LinkPayHandler) GetPublicProduct(c *fiber.Ctx) error {
	slug := c.Params("slug")
	if slug == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "slug obrigatório"})
	}

	product, err := h.service.GetProductBySlug(c.Context(), slug)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "produto não encontrado"})
	}

	// Omitir file_url da resposta pública
	product.FileURL = ""

	// #40: cover_image_url persistido é storage_key — gera presigned URL fresca a cada leitura.
	// Compat: se já for URL absoluta (legado), retorna como está.
	if product.CoverImageURL != "" && h.minioClient.IsConfigured() && product.CoverImageURL[0] != 'h' {
		presigned, presignErr := h.minioClient.PresignedURL(c.Context(), storage.LinkPayBucket, product.CoverImageURL)
		if presignErr != nil {
			log.Printf("[linkpay] aviso: erro ao gerar presigned URL da cover (product=%s): %v", product.ID, presignErr)
			product.CoverImageURL = ""
		} else {
			product.CoverImageURL = presigned
		}
	}

	// BKL-1206: inclui arquivos adicionais do produto na resposta pública
	files, err := h.service.ListProductFiles(c.Context(), product.ID)
	if err != nil {
		log.Printf("[linkpay] aviso: erro ao buscar arquivos do produto %s: %v", product.ID, err)
	} else {
		product.Files = files
	}

	return c.JSON(product)
}

// InitiatePurchase godoc
//
//	@Summary      Inicia compra de produto
//	@Description  Cria uma cobrança no Asaas (PIX ou cartão) para o produto informado.
//	@Tags         público
//	@Accept       json
//	@Produce      json
//	@Param        slug  path      string                          true  "Slug do produto"
//	@Param        body  body      domain.InitiatePurchaseRequest  true  "Dados do comprador e método de pagamento"
//	@Success      201   {object}  domain.DigitalPurchase
//	@Failure      400   {object}  map[string]string
//	@Failure      500   {object}  map[string]string
//	@Router       /api/v1/public/products/{slug}/purchase [post]
func (h *LinkPayHandler) InitiatePurchase(c *fiber.Ctx) error {
	slug := c.Params("slug")
	if slug == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "slug obrigatório"})
	}

	var req domain.InitiatePurchaseRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "corpo da requisição inválido"})
	}
	if err := h.validator.Struct(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "validação falhou", "details": err.Error()})
	}

	purchase, err := h.service.InitiatePurchase(c.Context(), slug, &req)
	if err != nil {
		log.Printf("[linkpay] erro ao iniciar compra: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "erro ao processar compra"})
	}

	// BKL-1252: Pulse tracking — fire-and-forget.
	h.pulseClient.Track(c.Context(), "venda.realizada", purchase.TenantID, map[string]string{
		"purchase_id": purchase.ID,
		"product_id":  purchase.ProductID,
		"slug":        slug,
	})

	return c.Status(fiber.StatusCreated).JSON(purchase)
}

// newWebhookRedisClient cria um cliente Redis para idempotência de webhook (#8).
// Retorna nil se REDIS_URL não estiver configurada ou for inválida (fail-open).
func newWebhookRedisClient() *redis.Client {
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		return nil
	}
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		log.Printf("[linkpay] WARN: REDIS_URL inválida (%s): %v — idempotência de webhook desabilitada", redisURL, err)
		return nil
	}
	return redis.NewClient(opts)
}

// setWebhookRedisClient injeta um cliente Redis para testes de idempotência de webhook.
func (h *LinkPayHandler) setWebhookRedisClient(client *redis.Client) {
	h.redisClient = client
}

// validateLinkPayWebhook valida o header asaas-access-token usando comparação constant-time.
// VUL-001: sem validação qualquer requisição poderia acionar ProcessPaymentConfirmed.
func (h *LinkPayHandler) validateLinkPayWebhook(rawBody []byte, token string) bool {
	if h.webhookSecret == "" || token == "" {
		return false
	}
	if len(rawBody) == 0 {
		return false
	}
	return hmac.Equal([]byte(h.webhookSecret), []byte(token))
}

// HandleWebhook godoc
//
//	@Summary      Webhook Asaas
//	@Description  Recebe eventos do Asaas (PAYMENT_CONFIRMED, PAYMENT_RECEIVED, TRANSFER_COMPLETED, etc.). Requer header asaas-access-token.
//	@Tags         webhook
//	@Accept       json
//	@Produce      json
//	@Param        asaas-access-token  header    string  true  "Token de segurança enviado pelo Asaas"
//	@Success      200                 {object}  map[string]bool
//	@Failure      400                 {object}  map[string]string
//	@Failure      401                 {object}  map[string]string
//	@Failure      500                 {object}  map[string]string
//	@Router       /api/v1/linkpay/webhook [post]
func (h *LinkPayHandler) HandleWebhook(c *fiber.Ctx) error {
	// VUL-001: validar asaas-access-token antes de qualquer processamento
	rawBody := c.Body()
	authToken := c.Get("asaas-access-token")
	if authToken == "" {
		authToken = c.Get("access_token")
	}
	if authToken == "" {
		authToken = c.Query("access_token")
	}

	if !h.validateLinkPayWebhook(rawBody, authToken) {
		log.Printf("[linkpay][SECURITY] webhook rejeitado: token inválido | IP: %s", c.IP())
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "token inválido"})
	}

	var payload struct {
		Event   string `json:"event"`
		Payment struct {
			ID           string `json:"id"`
			Subscription string `json:"subscription"` // BKL-1208: preenchido se for cobrança de assinatura
		} `json:"payment"`
		Subscription struct {
			ID string `json:"id"` // BKL-1208: presente em SUBSCRIPTION_DELETED
		} `json:"subscription"`
		Transfer struct {
			ID          string `json:"id"`
			Description string `json:"description"`
		} `json:"transfer"`
	}
	if err := c.BodyParser(&payload); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "payload inválido"})
	}

	switch payload.Event {
	case "PAYMENT_CONFIRMED", "PAYMENT_RECEIVED":
		if payload.Payment.ID == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "payment.id ausente"})
		}
		// #8: idempotência via Redis SetNX com TTL de 7 dias (janela de retry do Asaas)
		if h.redisClient != nil {
			key := fmt.Sprintf("webhook:linkpay:%s", payload.Payment.ID)
			ok, err := h.redisClient.SetNX(c.Context(), key, "1", 7*24*time.Hour).Result()
			if err != nil {
				log.Printf("[linkpay] aviso: Redis indisponível para idempotência: %v", err)
			} else if !ok {
				log.Printf("[linkpay] webhook duplicado ignorado (idempotente): chargeID=%s", payload.Payment.ID)
				return c.JSON(fiber.Map{"received": true, "idempotent": true})
			}
		}
		// BKL-1208: se payment.subscription está preenchido, é cobrança de assinatura recorrente
		if payload.Payment.Subscription != "" && h.subscriptionSvc != nil {
			if err := h.subscriptionSvc.RenewSubscription(c.Context(), payload.Payment.Subscription); err != nil {
				log.Printf("[linkpay] erro ao renovar assinatura asaas_id=%s: %v", payload.Payment.Subscription, err)
			}
			break
		}
		// Compra avulsa — fluxo original
		if err := h.service.ProcessPaymentConfirmed(c.Context(), payload.Payment.ID); err != nil {
			log.Printf("[linkpay] erro ao processar webhook: %v", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "erro ao processar evento"})
		}
	case "PAYMENT_OVERDUE":
		// BKL-1208: assinatura inadimplente
		if h.subscriptionSvc != nil && payload.Payment.Subscription != "" {
			if err := h.subscriptionSvc.MarkPastDue(c.Context(), payload.Payment.Subscription); err != nil {
				log.Printf("[linkpay] erro ao marcar past_due asaas_id=%s: %v", payload.Payment.Subscription, err)
			}
		}
	case "SUBSCRIPTION_DELETED":
		// BKL-1208: assinatura cancelada direto no painel Asaas
		if h.subscriptionSvc != nil && payload.Subscription.ID != "" {
			if err := h.subscriptionSvc.CancelSubscriptionByAsaasID(c.Context(), payload.Subscription.ID); err != nil {
				log.Printf("[linkpay] erro ao cancelar assinatura asaas_id=%s: %v", payload.Subscription.ID, err)
			}
		}
	case "TRANSFER_COMPLETED", "TRANSFER_FAILED":
		if h.payoutService != nil && payload.Transfer.ID != "" {
			if err := h.payoutService.HandleTransferWebhook(c.Context(), payload.Event, payload.Transfer.ID, payload.Transfer.Description); err != nil {
				log.Printf("[linkpay] erro ao processar transfer webhook %s: %v", payload.Transfer.ID, err)
			}
		}
	default:
		// Eventos não tratados — retornar 200 para o Asaas não retentar
	}

	return c.JSON(fiber.Map{"received": true})
}

// GetSalesStats godoc
//
//	@Summary      Estatísticas de vendas
//	@Description  Retorna métricas de vendas dos últimos 30 dias para o tenant autenticado.
//	@Tags         admin
//	@Produce      json
//	@Security     BearerAuth
//	@Success      200  {object}  domain.StatsResponse
//	@Failure      401  {object}  map[string]string
//	@Failure      500  {object}  map[string]string
//	@Router       /api/v1/admin/linkpay/stats [get]
func (h *LinkPayHandler) GetSalesStats(c *fiber.Ctx) error {
	tenantID, err := extractTenantID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "tenant não identificado"})
	}

	stats, err := h.service.GetSalesStats(c.Context(), tenantID.String())
	if err != nil {
		log.Printf("[linkpay] erro ao buscar stats: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "erro ao buscar estatísticas"})
	}

	return c.JSON(stats)
}

// GetCommissionSummary godoc
//
//	@Summary      Resumo de comissões do IIT
//	@Description  Retorna o resumo da receita de comissões da plataforma (IIT) em um período. Requer admin auth.
//	@Tags         admin
//	@Produce      json
//	@Param        start_date  query  string  true  "Data início (YYYY-MM-DD)"
//	@Param        end_date    query  string  true  "Data fim (YYYY-MM-DD)"
//	@Security     BearerAuth
//	@Success      200  {object}  domain.CommissionSummaryResponse
//	@Failure      400  {object}  map[string]string
//	@Failure      401  {object}  map[string]string
//	@Failure      500  {object}  map[string]string
//	@Router       /api/v1/admin/commission-summary [get]
func (h *LinkPayHandler) GetCommissionSummary(c *fiber.Ctx) error {
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")
	if startDate == "" || endDate == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "start_date e end_date (YYYY-MM-DD) são obrigatórios"})
	}

	summary, err := h.service.GetCommissionSummary(c.Context(), startDate, endDate)
	if err != nil {
		log.Printf("[linkpay] erro ao buscar commission summary: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "erro ao buscar resumo de comissões"})
	}

	return c.JSON(summary)
}

// Download redireciona para o arquivo do produto após validar o token.
// GET /api/v1/public/download/:token
// BKL-1205: se o produto usa MinIO (storage_key preenchido), gera presigned URL temporária (15min).
// Comportamento legado: redireciona para file_url quando storage_key está vazio.
func (h *LinkPayHandler) Download(c *fiber.Ctx) error {
	token := c.Params("token")
	if token == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "token obrigatório"})
	}

	// fileOrKey retorna storage_key (se MinIO) ou file_url (legado)
	fileOrKey, err := h.service.Download(c.Context(), token)
	if err != nil {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": err.Error()})
	}

	if fileOrKey == "" {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "arquivo não disponível"})
	}

	c.Set("Cache-Control", "no-store")
	c.Set("X-Download-At", time.Now().UTC().Format(time.RFC3339))

	// BKL-1205: storage_key contém "/" e não começa com "http" — gera presigned URL via MinIO.
	if h.minioClient.IsConfigured() && len(fileOrKey) > 0 && fileOrKey[0] != 'h' {
		presigned, presignErr := h.minioClient.PresignedURL(c.Context(), storage.LinkPayBucket, fileOrKey)
		if presignErr != nil {
			log.Printf("[linkpay] erro ao gerar presigned URL: %v", presignErr)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "erro ao gerar link de download"})
		}
		return c.Redirect(presigned, fiber.StatusFound)
	}

	return c.Redirect(fileOrKey, fiber.StatusFound)
}

// --- BKL-1206: múltiplos arquivos por produto ---

// AddProductFile godoc
//
//	@Summary      Adiciona arquivo ao produto
//	@Description  Faz upload de um arquivo adicional (multipart/form-data, campo "file") para o produto. Máximo 2 GB.
//	@Tags         produtos
//	@Accept       mpfd
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id    path      string  true  "ID do produto (UUID)"
//	@Param        file  formData  file    true  "Arquivo a enviar"
//	@Success      201   {object}  domain.DigitalProductFile
//	@Failure      400   {object}  map[string]string
//	@Failure      401   {object}  map[string]string
//	@Failure      404   {object}  map[string]string
//	@Failure      413   {object}  map[string]string
//	@Failure      503   {object}  map[string]string
//	@Router       /api/v1/linkpay/products/{id}/files [post]
func (h *LinkPayHandler) AddProductFile(c *fiber.Ctx) error {
	if !h.minioClient.IsConfigured() {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "armazenamento de arquivos não configurado"})
	}

	tenantID, err := extractTenantID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "tenant não identificado"})
	}

	productID := c.Params("id")
	if productID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "id do produto obrigatório"})
	}

	// Valida que o produto pertence ao tenant
	if _, err := h.service.GetProductByID(c.Context(), productID, tenantID.String()); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "produto não encontrado"})
	}

	file, err := c.FormFile("file")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "campo 'file' obrigatório"})
	}

	if file.Size > storage.MaxFileSize {
		return c.Status(fiber.StatusRequestEntityTooLarge).JSON(fiber.Map{"error": "arquivo muito grande: máximo 2GB"})
	}

	src, err := file.Open()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "erro ao abrir arquivo"})
	}
	defer src.Close()

	// Detecta MIME type via os primeiros 512 bytes
	buf := make([]byte, 512)
	n, _ := src.Read(buf)
	mimeType := http.DetectContentType(buf[:n])

	fullReader := io.MultiReader(newBytesReader(buf[:n]), src)

	ext := filepath.Ext(file.Filename)
	storageKey := fmt.Sprintf("%s/products/%s/%s%s", tenantID.String(), productID, uuid.New().String(), ext)

	if err := h.minioClient.UploadFile(c.Context(), storage.LinkPayBucket, storageKey, mimeType, fullReader, file.Size); err != nil {
		log.Printf("[linkpay] erro no upload MinIO (arquivo adicional): %v", err)
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	f := &domain.DigitalProductFile{
		ProductID:     productID,
		StorageKey:    storageKey,
		FileName:      file.Filename,
		FileSizeBytes: file.Size,
		MimeType:      mimeType,
	}
	if err := h.service.AddProductFile(c.Context(), f); err != nil {
		log.Printf("[linkpay] erro ao salvar metadados de arquivo adicional: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "erro ao registrar arquivo"})
	}

	return c.Status(fiber.StatusCreated).JSON(f)
}

// ListProductFiles godoc
//
//	@Summary      Lista arquivos do produto
//	@Description  Lista todos os arquivos adicionais associados ao produto do tenant autenticado.
//	@Tags         produtos
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id  path      string  true  "ID do produto (UUID)"
//	@Success      200  {object}  map[string][]domain.DigitalProductFile
//	@Failure      400  {object}  map[string]string
//	@Failure      401  {object}  map[string]string
//	@Failure      404  {object}  map[string]string
//	@Failure      500  {object}  map[string]string
//	@Router       /api/v1/linkpay/products/{id}/files [get]
func (h *LinkPayHandler) ListProductFiles(c *fiber.Ctx) error {
	tenantID, err := extractTenantID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "tenant não identificado"})
	}

	productID := c.Params("id")
	if productID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "id do produto obrigatório"})
	}

	// Valida posse do tenant
	if _, err := h.service.GetProductByID(c.Context(), productID, tenantID.String()); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "produto não encontrado"})
	}

	files, err := h.service.ListProductFiles(c.Context(), productID)
	if err != nil {
		log.Printf("[linkpay] erro ao listar arquivos do produto %s: %v", productID, err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "erro ao listar arquivos"})
	}

	if files == nil {
		files = []domain.DigitalProductFile{}
	}
	return c.JSON(fiber.Map{"files": files})
}

// DeleteProductFile godoc
//
//	@Summary      Remove arquivo do produto
//	@Description  Remove um arquivo adicional do produto (banco + MinIO).
//	@Tags         produtos
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id      path      string  true  "ID do produto (UUID)"
//	@Param        fileId  path      string  true  "ID do arquivo (UUID)"
//	@Success      204
//	@Failure      400  {object}  map[string]string
//	@Failure      401  {object}  map[string]string
//	@Failure      404  {object}  map[string]string
//	@Failure      500  {object}  map[string]string
//	@Router       /api/v1/linkpay/products/{id}/files/{fileId} [delete]
func (h *LinkPayHandler) DeleteProductFile(c *fiber.Ctx) error {
	tenantID, err := extractTenantID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "tenant não identificado"})
	}

	productID := c.Params("id")
	fileID := c.Params("fileId")
	if productID == "" || fileID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "id do produto e id do arquivo obrigatórios"})
	}

	// Valida posse do tenant
	if _, err := h.service.GetProductByID(c.Context(), productID, tenantID.String()); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "produto não encontrado"})
	}

	// Busca o arquivo para obter storage_key antes de remover do banco
	file, err := h.service.GetProductFile(c.Context(), fileID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "arquivo não encontrado"})
	}

	// Valida que o arquivo pertence ao produto
	if file.ProductID != productID {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "arquivo não encontrado"})
	}

	// Remove do MinIO — fail-open: loga erro mas não bloqueia a remoção do banco
	if h.minioClient.IsConfigured() {
		if delErr := h.minioClient.DeleteFile(c.Context(), storage.LinkPayBucket, file.StorageKey); delErr != nil {
			log.Printf("[linkpay] aviso: erro ao remover arquivo do MinIO (storage_key=%s): %v", file.StorageKey, delErr)
		}
	}

	if err := h.service.DeleteProductFile(c.Context(), fileID, productID); err != nil {
		log.Printf("[linkpay] erro ao remover arquivo %s do banco: %v", fileID, err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "erro ao remover arquivo"})
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// DownloadFile godoc
//
//	@Summary      Download de arquivo individual
//	@Description  Valida o token de compra e redireciona (302) para presigned URL do MinIO. Expira em 15 min.
//	@Tags         público
//	@Produce      json
//	@Param        token   path      string  true  "Token de acesso da compra"
//	@Param        fileId  path      string  true  "ID do arquivo (UUID)"
//	@Success      302
//	@Failure      400  {object}  map[string]string
//	@Failure      403  {object}  map[string]string
//	@Failure      404  {object}  map[string]string
//	@Failure      503  {object}  map[string]string
//	@Router       /api/v1/public/download/{token}/file/{fileId} [get]
func (h *LinkPayHandler) DownloadFile(c *fiber.Ctx) error {
	token := c.Params("token")
	fileID := c.Params("fileId")
	if token == "" || fileID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "token e fileId obrigatórios"})
	}

	// Valida token de compra
	purchase, err := h.service.GetPurchaseByToken(c.Context(), token)
	if err != nil {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "token inválido"})
	}
	if purchase.PaymentStatus != "confirmed" {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "pagamento não confirmado"})
	}
	if purchase.AccessExpiresAt != nil && time.Now().After(*purchase.AccessExpiresAt) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "link de download expirado"})
	}

	// Busca o arquivo e valida que pertence ao produto da compra
	file, err := h.service.GetProductFile(c.Context(), fileID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "arquivo não encontrado"})
	}
	if file.ProductID != purchase.ProductID {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "arquivo não pertence a este produto"})
	}

	if !h.minioClient.IsConfigured() {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "armazenamento não configurado"})
	}

	presigned, err := h.minioClient.PresignedURL(c.Context(), storage.LinkPayBucket, file.StorageKey)
	if err != nil {
		log.Printf("[linkpay] erro ao gerar presigned URL para arquivo %s: %v", fileID, err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "erro ao gerar link de download"})
	}

	c.Set("Cache-Control", "no-store")
	return c.Redirect(presigned, fiber.StatusFound)
}

// DownloadZip godoc
//
//	@Summary      Download ZIP de todos os arquivos
//	@Description  Gera e faz streaming de um ZIP com todos os arquivos do produto. Requer pagamento confirmado.
//	@Tags         público
//	@Produce      application/zip
//	@Param        token  path      string  true  "Token de acesso da compra"
//	@Success      200
//	@Failure      400  {object}  map[string]string
//	@Failure      403  {object}  map[string]string
//	@Failure      404  {object}  map[string]string
//	@Failure      503  {object}  map[string]string
//	@Router       /api/v1/public/download/{token}/zip [get]
func (h *LinkPayHandler) DownloadZip(c *fiber.Ctx) error {
	token := c.Params("token")
	if token == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "token obrigatório"})
	}

	// STD-008: valida token, pagamento, expiração E limite de downloads ANTES de iniciar o stream.
	// service.Download faz todas essas checagens atômicas e incrementa download_count.
	// Sem isso, o endpoint /zip permitia bypass do download_limit (issue #23).
	if _, downloadErr := h.service.Download(c.Context(), token); downloadErr != nil {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": downloadErr.Error()})
	}

	purchase, err := h.service.GetPurchaseByToken(c.Context(), token)
	if err != nil {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "token inválido"})
	}

	// Busca produto para obter slug (nome do ZIP)
	product, err := h.service.GetProductByID(c.Context(), purchase.ProductID, purchase.TenantID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "produto não encontrado"})
	}

	// NOTA (merge PR #75 + #83): o gate de download_limit já foi aplicado de forma atômica
	// pela chamada a h.service.Download() acima (que já incrementou download_count). Repetir
	// a checagem aqui usando o `purchase` recém-recarregado compararia a contagem PÓS-incremento
	// contra o limite, bloqueando indevidamente o último download legítimo (off-by-one). Por isso
	// o check duplicado da PR #75 foi removido nesta reconciliação — a PR #83 já cobre o caso.

	// Lista arquivos adicionais do produto
	files, err := h.service.ListProductFiles(c.Context(), purchase.ProductID)
	if err != nil {
		log.Printf("[linkpay] erro ao listar arquivos para ZIP produto=%s: %v", purchase.ProductID, err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "erro ao preparar download"})
	}

	if !h.minioClient.IsConfigured() {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "armazenamento não configurado"})
	}

	// Monta a lista de entradas do ZIP: arquivo principal do produto + arquivos adicionais
	type zipEntry struct {
		storageKey string
		fileName   string
	}
	var entries []zipEntry
	if product.StorageKey != "" {
		name := product.FileName
		if name == "" {
			name = product.Slug + filepath.Ext(product.StorageKey)
		}
		entries = append(entries, zipEntry{storageKey: product.StorageKey, fileName: name})
	}
	for _, f := range files {
		entries = append(entries, zipEntry{storageKey: f.StorageKey, fileName: f.FileName})
	}

	if len(entries) == 0 {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "nenhum arquivo disponível para download"})
	}

	zipName := fmt.Sprintf("%s.zip", product.Slug)
	c.Set("Content-Type", "application/zip")
	c.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, zipName))
	c.Set("Cache-Control", "no-store")

	// Streaming via io.Pipe — não acumula em memória
	pr, pw := io.Pipe()

	go func() {
		zw := zip.NewWriter(pw)
		defer func() {
			if closeErr := zw.Close(); closeErr != nil {
				log.Printf("[linkpay] erro ao fechar ZIP writer: %v", closeErr)
			}
			pw.Close()
		}()

		for _, entry := range entries {
			obj, objErr := h.minioClient.GetObject(c.Context(), storage.LinkPayBucket, entry.storageKey)
			if objErr != nil {
				log.Printf("[linkpay] erro ao abrir objeto para ZIP (key=%s): %v", entry.storageKey, objErr)
				continue
			}

			w, zipErr := zw.Create(entry.fileName)
			if zipErr != nil {
				obj.Close()
				log.Printf("[linkpay] erro ao criar entrada no ZIP (%s): %v", entry.fileName, zipErr)
				continue
			}

			if _, copyErr := io.Copy(w, obj); copyErr != nil {
				log.Printf("[linkpay] erro ao copiar arquivo para ZIP (%s): %v", entry.fileName, copyErr)
			}
			obj.Close()
		}
	}()

	return c.Status(fiber.StatusOK).SendStream(pr)
}

// RefundPurchase godoc
//
//	@Summary      Estorna compra (CDC Art. 49)
//	@Description  Solicita o estorno de uma compra confirmada dentro do prazo de arrependimento de 7 dias (CDC Art. 49). Autenticado via access_token enviado por email ao comprador.
//	@Tags         público
//	@Accept       json
//	@Produce      json
//	@Param        id    path      string                     true  "ID da compra (UUID)"
//	@Param        body  body      RefundPurchaseRequest      true  "Token de acesso da compra"
//	@Success      200   {object}  map[string]string
//	@Failure      400   {object}  map[string]string
//	@Failure      401   {object}  map[string]string
//	@Failure      500   {object}  map[string]string
//	@Router       /api/v1/public/purchases/{id}/refund [post]
func (h *LinkPayHandler) RefundPurchase(c *fiber.Ctx) error {
	purchaseID := c.Params("id")
	if purchaseID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "id da compra obrigatório"})
	}

	var req struct {
		AccessToken string `json:"access_token" validate:"required"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "corpo da requisição inválido"})
	}
	if req.AccessToken == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "access_token é obrigatório"})
	}

	if err := h.service.RefundPurchase(c.Context(), purchaseID, req.AccessToken); err != nil {
		log.Printf("[linkpay] erro no reembolso purchase_id=%s: %v", purchaseID, err)
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"message": "compra reembolsada com sucesso"})
}

// CreateCart godoc
//
//	@Summary      Cria carrinho com upsells
//	@Description  Cria um carrinho no cart-service com o produto principal e, opcionalmente, os upsells sugeridos (BKL-1221).
//	@Tags         público
//	@Accept       json
//	@Produce      json
//	@Param        slug  path      string                    true  "Slug do produto"
//	@Param        body  body      domain.CreateCartRequest  true  "Opções do carrinho"
//	@Success      201   {object}  map[string]string
//	@Failure      400   {object}  map[string]string
//	@Failure      404   {object}  map[string]string
//	@Failure      502   {object}  map[string]string
//	@Router       /api/v1/public/products/{slug}/cart [post]
func (h *LinkPayHandler) CreateCart(c *fiber.Ctx) error {
	slug := c.Params("slug")
	if slug == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "slug obrigatório"})
	}

	var req domain.CreateCartRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "corpo da requisição inválido"})
	}

	product, err := h.service.GetProductBySlug(c.Context(), slug)
	if err != nil || !product.IsActive {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "produto não encontrado"})
	}

	cart, err := h.cartClient.CreateCart(c.Context(), product.TenantID, "", slug)
	if err != nil {
		log.Printf("[linkpay][cart] erro ao criar carrinho para slug=%s: %v", slug, err)
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "erro ao criar carrinho"})
	}

	// Adiciona produto principal ao carrinho
	mainItem := client.AddItemRequest{
		ProductType: "digital_product",
		ProductID:   product.ID,
		Quantity:    1,
		Price:       float64(product.PriceCents) / 100.0,
		Name:        product.Name,
	}
	cart, err = h.cartClient.AddItem(c.Context(), cart.ID, mainItem)
	if err != nil {
		log.Printf("[linkpay][cart] erro ao adicionar produto principal cart_id=%s: %v", cart.ID, err)
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "erro ao adicionar produto ao carrinho"})
	}

	// Adiciona upsells sugeridos, se solicitado e disponíveis
	if req.AddUpsells {
		upsells, upsellErr := h.service.GetUpsellsByProductID(c.Context(), product.ID)
		if upsellErr != nil {
			log.Printf("[linkpay][cart] aviso: erro ao buscar upsells produto_id=%s: %v", product.ID, upsellErr)
		}

		for _, u := range upsells {
			upsellProduct, upErr := h.service.GetProductByID(c.Context(), u.ProductID, product.TenantID)
			if upErr != nil || !upsellProduct.IsActive {
				continue
			}
			upsellItem := client.AddItemRequest{
				ProductType: "digital_product",
				ProductID:   upsellProduct.ID,
				Quantity:    1,
				Price:       float64(upsellProduct.PriceCents) / 100.0,
				Name:        upsellProduct.Name,
			}
			if cart, err = h.cartClient.AddItem(c.Context(), cart.ID, upsellItem); err != nil {
				log.Printf("[linkpay][cart] aviso: falha ao adicionar upsell product_id=%s cart_id=%s: %v",
					upsellProduct.ID, cart.ID, err)
			}
		}
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"cart_id": cart.ID})
}

// newBytesReader cria um io.Reader a partir de um slice de bytes.
// Usado para reconstituir o stream após peek dos primeiros 512 bytes para detecção de MIME.
func newBytesReader(b []byte) io.Reader {
	return &bytesReader{data: b}
}

type bytesReader struct {
	data []byte
	pos  int
}

func (r *bytesReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}
