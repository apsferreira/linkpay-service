package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"

	"linkpay-service/internal/messaging"
	"linkpay-service/internal/domain"
)

// LinkPayRepoInterface define os métodos do repository usados pelo serviço.
type LinkPayRepoInterface interface {
	CreateProduct(ctx context.Context, p *domain.DigitalProduct) error
	GetProductBySlug(ctx context.Context, slug string) (*domain.DigitalProduct, error)
	GetProductByID(ctx context.Context, id, tenantID string) (*domain.DigitalProduct, error)
	ListProducts(ctx context.Context, tenantID string) ([]domain.DigitalProduct, error)
	UpdateProduct(ctx context.Context, p *domain.DigitalProduct) error
	UpdateProductStorage(ctx context.Context, id, tenantID, storageKey, storageBucket, fileName string, fileSizeBytes int64, mimeType string) error
	DeactivateProduct(ctx context.Context, id, tenantID string) error
	CreatePurchase(ctx context.Context, p *domain.DigitalPurchase) error
	GetPurchaseByToken(ctx context.Context, token string) (*domain.DigitalPurchase, error)
	GetPurchaseByChargeID(ctx context.Context, chargeID string) (*domain.DigitalPurchase, error)
	UpdatePurchaseStatus(ctx context.Context, id, status, chargeID string) error
	ConfirmPurchase(ctx context.Context, id, accessToken string, expiresAt time.Time) error
	IncrementDownload(ctx context.Context, token string) error
	GetSalesStats(ctx context.Context, tenantID string, days int) (*domain.LinkPayStatsResponse, error)
	// BKL-1206: múltiplos arquivos por produto
	AddProductFile(ctx context.Context, f *domain.DigitalProductFile) error
	ListProductFiles(ctx context.Context, productID string) ([]domain.DigitalProductFile, error)
	GetProductFile(ctx context.Context, fileID string) (*domain.DigitalProductFile, error)
	DeleteProductFile(ctx context.Context, fileID, productID string) error
	// BKL-1221: upsells
	GetUpsellsByProductID(ctx context.Context, productID string) ([]domain.UpsellSuggestion, error)
}

// EmailSender é a interface mínima para envio de email de confirmação de compra.
// Implementada por clients.ResendClient.
type EmailSender interface {
	SendPurchaseConfirmation(ctx context.Context, to, customerName, productName, downloadURL string, downloadLimit int) error
}

// LinkPayPublisher define a interface de publicação de eventos — permite mock em testes.
type LinkPayPublisher interface {
	PublishLinkPayPurchaseConfirmed(ctx context.Context, event messaging.LinkPayPurchaseConfirmedEvent) error
}

// LinkPayService orquestra a lógica de negócio do módulo LinkPay.
type LinkPayService struct {
	repo        LinkPayRepoInterface
	asaasClient *AsaasClient
	publisher   LinkPayPublisher // nil se RabbitMQ não configurado (fail-open)
	emailSender EmailSender      // nil se RESEND_API_KEY não configurado (fail-open)
}

func NewLinkPayService(repo LinkPayRepoInterface, asaasClient *AsaasClient, publisher LinkPayPublisher, emailSender EmailSender) *LinkPayService {
	return &LinkPayService{repo: repo, asaasClient: asaasClient, publisher: publisher, emailSender: emailSender}
}

// --- Produtos ---

func (s *LinkPayService) CreateProduct(ctx context.Context, tenantID string, req *domain.CreateDigitalProductRequest) (*domain.DigitalProduct, error) {
	now := time.Now()
	limit := req.DownloadLimit
	if limit == 0 {
		limit = 5
	}
	p := &domain.DigitalProduct{
		ID:            uuid.New().String(),
		TenantID:      tenantID,
		Slug:          req.Slug,
		Name:          req.Name,
		Description:   req.Description,
		PriceCents:    req.PriceCents,
		Currency:      "BRL",
		ProductType:   req.ProductType,
		FileURL:       req.FileURL,
		DownloadLimit: limit,
		IsActive:      true,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.repo.CreateProduct(ctx, p); err != nil {
		return nil, fmt.Errorf("erro ao criar produto: %w", err)
	}
	return p, nil
}

func (s *LinkPayService) GetProductBySlug(ctx context.Context, slug string) (*domain.DigitalProduct, error) {
	return s.repo.GetProductBySlug(ctx, slug)
}

func (s *LinkPayService) ListProducts(ctx context.Context, tenantID string) ([]domain.DigitalProduct, error) {
	return s.repo.ListProducts(ctx, tenantID)
}

func (s *LinkPayService) UpdateProduct(ctx context.Context, id, tenantID string, req *domain.UpdateDigitalProductRequest) (*domain.DigitalProduct, error) {
	p, err := s.repo.GetProductByID(ctx, id, tenantID)
	if err != nil {
		return nil, err
	}
	if req.Name != "" {
		p.Name = req.Name
	}
	if req.Description != "" {
		p.Description = req.Description
	}
	if req.PriceCents > 0 {
		p.PriceCents = req.PriceCents
	}
	if req.ProductType != "" {
		p.ProductType = req.ProductType
	}
	if req.FileURL != "" {
		p.FileURL = req.FileURL
	}
	if req.DownloadLimit > 0 {
		p.DownloadLimit = req.DownloadLimit
	}
	if req.IsActive != nil {
		p.IsActive = *req.IsActive
	}
	p.UpdatedAt = time.Now()

	if err := s.repo.UpdateProduct(ctx, p); err != nil {
		return nil, fmt.Errorf("erro ao atualizar produto: %w", err)
	}
	return p, nil
}

// GetProductByID retorna um produto pelo ID dentro de um tenant (usado pelo handler de upload).
func (s *LinkPayService) GetProductByID(ctx context.Context, id, tenantID string) (*domain.DigitalProduct, error) {
	return s.repo.GetProductByID(ctx, id, tenantID)
}

// UpdateProductStorage persiste os metadados MinIO após upload bem-sucedido.
// BKL-1205: atualiza storage_key, storage_bucket, file_name, file_size_bytes, mime_type.
func (s *LinkPayService) UpdateProductStorage(ctx context.Context, id, tenantID, storageKey, storageBucket, fileName string, fileSizeBytes int64, mimeType string) error {
	return s.repo.UpdateProductStorage(ctx, id, tenantID, storageKey, storageBucket, fileName, fileSizeBytes, mimeType)
}

func (s *LinkPayService) DeactivateProduct(ctx context.Context, id, tenantID string) error {
	return s.repo.DeactivateProduct(ctx, id, tenantID)
}

// --- Compras ---

// InitiatePurchase cria a cobrança no Asaas e persiste a compra pendente.
func (s *LinkPayService) InitiatePurchase(ctx context.Context, slug string, req *domain.InitiatePurchaseRequest) (*domain.DigitalPurchase, error) {
	product, err := s.repo.GetProductBySlug(ctx, slug)
	if err != nil {
		return nil, fmt.Errorf("produto não encontrado")
	}
	if !product.IsActive {
		return nil, fmt.Errorf("produto indisponível")
	}

	// Criar customer no Asaas
	asaasCustomer, err := s.asaasClient.CreateCustomer(req.CustomerName, req.CustomerEmail, req.CustomerCPF)
	if err != nil {
		return nil, fmt.Errorf("erro ao registrar cliente: %w", err)
	}

	// BKL-603: calcular comissão da plataforma (30% default) e earnings do criador
	amountCents := product.PriceCents
	platformFee := product.PlatformFeeCents(amountCents)
	creatorEarnings := product.CreatorEarningsCents(amountCents)

	purchase := &domain.DigitalPurchase{
		ID:               uuid.New().String(),
		ProductID:        product.ID,
		TenantID:         product.TenantID,
		CustomerEmail:    req.CustomerEmail,
		CustomerName:     req.CustomerName,
		CustomerCPF:      req.CustomerCPF,
		AmountCents:      amountCents,
		PlatformFeeCents: platformFee,    // BKL-603
		CreatorEarnings:  creatorEarnings, // BKL-603
		PaymentMethod:    req.PaymentMethod,
		PaymentStatus:    "pending",
		DownloadCount:    0,
		CreatedAt:        time.Now(),
	}

	// Criar cobrança avulsa no Asaas
	billingType := "PIX"
	if req.PaymentMethod == "credit_card" {
		billingType = "CREDIT_CARD"
	}

	asaasReq := AsaasPaymentRequest{
		Customer:          asaasCustomer.ID,
		BillingType:       billingType,
		Value:             float64(product.PriceCents) / 100.0,
		DueDate:           time.Now().AddDate(0, 0, 3).Format("2006-01-02"),
		Description:       fmt.Sprintf("LinkPay: %s", product.Name),
		ExternalReference: purchase.ID,
	}

	asaasResp, err := s.asaasClient.createRawPayment(asaasReq)
	if err != nil {
		return nil, fmt.Errorf("erro ao criar cobrança: %w", err)
	}

	purchase.ExternalChargeID = asaasResp.ID

	if err := s.repo.CreatePurchase(ctx, purchase); err != nil {
		return nil, fmt.Errorf("erro ao persistir compra: %w", err)
	}

	// Popular campos Pix/URL (não persistidos)
	if asaasResp.PixTransaction != nil {
		purchase.PixQRCode = asaasResp.PixTransaction.EncodedImage
		purchase.PixCopyPaste = asaasResp.PixTransaction.Payload
	}
	if asaasResp.InvoiceURL != "" {
		purchase.PaymentURL = asaasResp.InvoiceURL
	}

	return purchase, nil
}

// ProcessPaymentConfirmed é chamado pelo webhook para confirmar uma compra LinkPay.
func (s *LinkPayService) ProcessPaymentConfirmed(ctx context.Context, chargeID string) error {
	purchase, err := s.repo.GetPurchaseByChargeID(ctx, chargeID)
	if err != nil {
		// Não é uma compra LinkPay — ignorar silenciosamente
		return nil
	}
	if purchase.PaymentStatus == "confirmed" {
		return nil // idempotência
	}

	token, err := generateSecureToken()
	if err != nil {
		return fmt.Errorf("erro ao gerar token de acesso: %w", err)
	}

	expiresAt := time.Now().AddDate(0, 0, 7)
	if err := s.repo.ConfirmPurchase(ctx, purchase.ID, token, expiresAt); err != nil {
		return fmt.Errorf("erro ao confirmar compra: %w", err)
	}

	log.Printf("[linkpay] compra confirmada: purchase_id=%s product_id=%s", purchase.ID, purchase.ProductID)

	baseURL := "https://checkout-api.institutoitinerante.com.br"
	downloadURL := fmt.Sprintf("%s/api/v1/public/download/%s", baseURL, token)

	// BKL-417: Publica evento para notification-service enviar email pós-compra via RabbitMQ.
	// Fail-open: se RabbitMQ não configurado ou falhar, tenta envio direto via Resend.
	if s.publisher != nil {
		evt := messaging.LinkPayPurchaseConfirmedEvent{
			PurchaseID:    purchase.ID,
			ProductID:     purchase.ProductID,
			TenantID:      purchase.TenantID,
			CustomerEmail: purchase.CustomerEmail,
			CustomerName:  purchase.CustomerName,
			AmountCents:   purchase.AmountCents,
			AccessToken:   token,
			DownloadURL:   downloadURL,
			ExpiresAt:     expiresAt,
			OccurredAt:    time.Now().UTC(),
		}
		if pubErr := s.publisher.PublishLinkPayPurchaseConfirmed(ctx, evt); pubErr != nil {
			log.Printf("[linkpay] falha ao publicar evento pós-compra (non-fatal): %v", pubErr)
		} else {
			return nil
		}
	}

	// Fallback: envio direto via Resend quando RabbitMQ não está disponível.
	if s.emailSender != nil {
		product, prodErr := s.repo.GetProductByID(ctx, purchase.ProductID, purchase.TenantID)
		if prodErr != nil {
			log.Printf("[linkpay] não foi possível buscar produto para email fallback: %v", prodErr)
			return nil
		}
		if emailErr := s.emailSender.SendPurchaseConfirmation(
			ctx,
			purchase.CustomerEmail,
			purchase.CustomerName,
			product.Name,
			downloadURL,
			product.DownloadLimit,
		); emailErr != nil {
			log.Printf("[linkpay] falha ao enviar email via Resend (non-fatal): %v", emailErr)
		}
	}

	return nil
}

// GetSalesStats retorna métricas de vendas dos últimos 30 dias para um tenant.
func (s *LinkPayService) GetSalesStats(ctx context.Context, tenantID string) (*domain.LinkPayStatsResponse, error) {
	return s.repo.GetSalesStats(ctx, tenantID, 30)
}

// Download valida o token e retorna a URL do arquivo para redirecionamento.
func (s *LinkPayService) Download(ctx context.Context, token string) (string, error) {
	purchase, err := s.repo.GetPurchaseByToken(ctx, token)
	if err != nil {
		return "", fmt.Errorf("token inválido")
	}
	if purchase.PaymentStatus != "confirmed" {
		return "", fmt.Errorf("pagamento não confirmado")
	}
	if purchase.AccessExpiresAt != nil && time.Now().After(*purchase.AccessExpiresAt) {
		return "", fmt.Errorf("link de download expirado")
	}

	product, err := s.repo.GetProductByID(ctx, purchase.ProductID, purchase.TenantID)
	if err != nil {
		return "", fmt.Errorf("produto não encontrado")
	}
	if purchase.DownloadCount >= product.DownloadLimit {
		return "", fmt.Errorf("limite de downloads atingido")
	}

	if err := s.repo.IncrementDownload(ctx, token); err != nil {
		log.Printf("[linkpay] erro ao incrementar download: %v", err)
	}

	// BKL-1205: retorna storage_key se arquivo foi migrado para MinIO; senão retorna file_url (legado).
	if product.StorageKey != "" {
		return product.StorageKey, nil
	}
	return product.FileURL, nil
}

// --- Arquivos de produto (BKL-1206) ---

// AddProductFile adiciona um arquivo adicional a um produto digital.
func (s *LinkPayService) AddProductFile(ctx context.Context, f *domain.DigitalProductFile) error {
	return s.repo.AddProductFile(ctx, f)
}

// ListProductFiles retorna todos os arquivos de um produto, ordenados por sort_order.
func (s *LinkPayService) ListProductFiles(ctx context.Context, productID string) ([]domain.DigitalProductFile, error) {
	return s.repo.ListProductFiles(ctx, productID)
}

// GetProductFile busca um arquivo pelo ID.
func (s *LinkPayService) GetProductFile(ctx context.Context, fileID string) (*domain.DigitalProductFile, error) {
	return s.repo.GetProductFile(ctx, fileID)
}

// DeleteProductFile remove um arquivo pelo ID validando que pertence ao produto.
func (s *LinkPayService) DeleteProductFile(ctx context.Context, fileID, productID string) error {
	return s.repo.DeleteProductFile(ctx, fileID, productID)
}

// GetPurchaseByToken expõe a busca de purchase pelo token para uso nos handlers de download.
func (s *LinkPayService) GetPurchaseByToken(ctx context.Context, token string) (*domain.DigitalPurchase, error) {
	return s.repo.GetPurchaseByToken(ctx, token)
}


// GetUpsellsByProductID retorna os produtos sugeridos como upsell para um produto. BKL-1221.
func (s *LinkPayService) GetUpsellsByProductID(ctx context.Context, productID string) ([]domain.UpsellSuggestion, error) {
	return s.repo.GetUpsellsByProductID(ctx, productID)
}

// generateSecureToken gera um token hexadecimal de 32 bytes (64 chars).
func generateSecureToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
