package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"linkpay-service/internal/domain"
	"linkpay-service/internal/messaging"
	"linkpay-service/internal/service"
)

// ─── mocks ───────────────────────────────────────────────────────────────────

type mockRepo struct {
	products  map[string]*domain.DigitalProduct
	purchases map[string]*domain.DigitalPurchase // keyed by chargeID
	byToken   map[string]*domain.DigitalPurchase
	byID      map[string]*domain.DigitalPurchase // keyed by purchaseID

	downloadIncremented bool
	confirmCalled       bool
}

func newMockRepo() *mockRepo {
	return &mockRepo{
		products:  make(map[string]*domain.DigitalProduct),
		purchases: make(map[string]*domain.DigitalPurchase),
		byToken:   make(map[string]*domain.DigitalPurchase),
		byID:      make(map[string]*domain.DigitalPurchase),
	}
}

func (m *mockRepo) CreateProduct(_ context.Context, p *domain.DigitalProduct) error {
	m.products[p.ID] = p
	return nil
}
func (m *mockRepo) GetProductBySlug(_ context.Context, slug string) (*domain.DigitalProduct, error) {
	for _, p := range m.products {
		if p.Slug == slug {
			return p, nil
		}
	}
	return nil, errors.New("not found")
}
func (m *mockRepo) GetProductByID(_ context.Context, id, _ string) (*domain.DigitalProduct, error) {
	if p, ok := m.products[id]; ok {
		return p, nil
	}
	return nil, errors.New("not found")
}
func (m *mockRepo) ListProducts(_ context.Context, tenantID string) ([]domain.DigitalProduct, error) {
	var out []domain.DigitalProduct
	for _, p := range m.products {
		if p.TenantID == tenantID {
			out = append(out, *p)
		}
	}
	return out, nil
}
func (m *mockRepo) UpdateProduct(_ context.Context, p *domain.DigitalProduct) error {
	m.products[p.ID] = p
	return nil
}
func (m *mockRepo) UpdateProductStorage(_ context.Context, id, tenantID, storageKey, storageBucket, fileName string, fileSizeBytes int64, mimeType string) error {
	if p, ok := m.products[id]; ok {
		p.StorageKey = storageKey
	}
	return nil
}
func (m *mockRepo) DeactivateProduct(_ context.Context, id, _ string) error {
	if p, ok := m.products[id]; ok {
		p.IsActive = false
	}
	return nil
}
func (m *mockRepo) CreatePurchase(_ context.Context, p *domain.DigitalPurchase) error {
	m.byID[p.ID] = p
	return nil
}
func (m *mockRepo) GetPurchaseByToken(_ context.Context, token string) (*domain.DigitalPurchase, error) {
	if p, ok := m.byToken[token]; ok {
		return p, nil
	}
	return nil, errors.New("not found")
}
func (m *mockRepo) GetPurchaseByID(_ context.Context, id string) (*domain.DigitalPurchase, error) {
	if p, ok := m.byID[id]; ok {
		return p, nil
	}
	return nil, errors.New("not found")
}
func (m *mockRepo) GetPurchaseByChargeID(_ context.Context, chargeID string) (*domain.DigitalPurchase, error) {
	if p, ok := m.purchases[chargeID]; ok {
		return p, nil
	}
	return nil, errors.New("not found")
}
func (m *mockRepo) UpdatePurchaseStatus(_ context.Context, id, status, chargeID string) error {
	if p, ok := m.byID[id]; ok {
		p.PaymentStatus = status
	}
	return nil
}
func (m *mockRepo) ConfirmPurchase(_ context.Context, id, accessToken string, expiresAt time.Time) error {
	m.confirmCalled = true
	if p, ok := m.byID[id]; ok {
		p.PaymentStatus = "confirmed"
		p.AccessToken = accessToken
		p.AccessExpiresAt = &expiresAt
		m.byToken[accessToken] = p
	}
	return nil
}
func (m *mockRepo) IncrementDownload(_ context.Context, token string) error {
	m.downloadIncremented = true
	if p, ok := m.byToken[token]; ok {
		p.DownloadCount++
	}
	return nil
}
func (m *mockRepo) GetSalesStats(_ context.Context, tenantID string, days int) (*domain.LinkPayStatsResponse, error) {
	return &domain.LinkPayStatsResponse{}, nil
}
func (m *mockRepo) AddProductFile(_ context.Context, f *domain.DigitalProductFile) error {
	return nil
}
func (m *mockRepo) ListProductFiles(_ context.Context, productID string) ([]domain.DigitalProductFile, error) {
	return nil, nil
}
func (m *mockRepo) GetProductFile(_ context.Context, fileID string) (*domain.DigitalProductFile, error) {
	return nil, errors.New("not found")
}
func (m *mockRepo) DeleteProductFile(_ context.Context, fileID, productID string) error {
	return nil
}
func (m *mockRepo) GetUpsellsByProductID(_ context.Context, productID string) ([]domain.UpsellSuggestion, error) {
	return nil, nil
}

type mockPublisher struct{ called bool }

func (m *mockPublisher) PublishLinkPayPurchaseConfirmed(_ context.Context, _ messaging.LinkPayPurchaseConfirmedEvent) error {
	m.called = true
	return nil
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func makeProduct(id, tenantID, slug string, priceCents int, active bool) *domain.DigitalProduct {
	return &domain.DigitalProduct{
		ID:            id,
		TenantID:      tenantID,
		Slug:          slug,
		Name:          "Produto Teste",
		PriceCents:    priceCents,
		Currency:      "BRL",
		ProductType:   "ebook",
		DownloadLimit: 5,
		IsActive:      active,
		FileURL:       "https://files.example.com/produto.pdf",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
}

// ─── testes de CreateProduct ─────────────────────────────────────────────────

func TestCreateProduct_DefaultDownloadLimit(t *testing.T) {
	repo := newMockRepo()
	svc := service.NewLinkPayService(repo, nil, nil, nil)

	req := &domain.CreateDigitalProductRequest{
		Slug:          "ebook-marketing",
		Name:          "Ebook Marketing",
		PriceCents:    2990,
		ProductType:   "ebook",
		DownloadLimit: 0, // deve usar default 5
	}

	p, err := svc.CreateProduct(context.Background(), "tenant-1", req)
	if err != nil {
		t.Fatalf("CreateProduct retornou erro: %v", err)
	}
	if p.DownloadLimit != 5 {
		t.Errorf("DownloadLimit = %d, want 5 (default)", p.DownloadLimit)
	}
	if p.TenantID != "tenant-1" {
		t.Errorf("TenantID = %q, want tenant-1", p.TenantID)
	}
	if p.Currency != "BRL" {
		t.Errorf("Currency = %q, want BRL", p.Currency)
	}
}

func TestCreateProduct_ExplicitDownloadLimit(t *testing.T) {
	repo := newMockRepo()
	svc := service.NewLinkPayService(repo, nil, nil, nil)

	req := &domain.CreateDigitalProductRequest{
		Slug:          "curso-go",
		Name:          "Curso Go",
		PriceCents:    9900,
		ProductType:   "course",
		DownloadLimit: 3,
	}

	p, err := svc.CreateProduct(context.Background(), "tenant-2", req)
	if err != nil {
		t.Fatalf("CreateProduct retornou erro: %v", err)
	}
	if p.DownloadLimit != 3 {
		t.Errorf("DownloadLimit = %d, want 3", p.DownloadLimit)
	}
}

// ─── testes de Download ───────────────────────────────────────────────────────

func TestDownload_LimiteAtingido(t *testing.T) {
	repo := newMockRepo()
	svc := service.NewLinkPayService(repo, nil, nil, nil)

	prod := makeProduct("prod-1", "tenant-1", "curso", 1000, true)
	prod.DownloadLimit = 2
	repo.products["prod-1"] = prod

	exp := time.Now().Add(7 * 24 * time.Hour)
	tok := "tok-abc"
	purchase := &domain.DigitalPurchase{
		ID:              "purch-1",
		ProductID:       "prod-1",
		TenantID:        "tenant-1",
		PaymentStatus:   "confirmed",
		DownloadCount:   2, // já esgotou
		AccessToken:     tok,
		AccessExpiresAt: &exp,
	}
	repo.byToken[tok] = purchase

	_, err := svc.Download(context.Background(), tok)
	if err == nil {
		t.Fatal("esperava erro por limite de downloads, mas não houve")
	}
}

func TestDownload_TokenExpirado(t *testing.T) {
	repo := newMockRepo()
	svc := service.NewLinkPayService(repo, nil, nil, nil)

	prod := makeProduct("prod-2", "tenant-1", "mini", 500, true)
	repo.products["prod-2"] = prod

	exp := time.Now().Add(-1 * time.Hour) // expirado no passado
	tok := "tok-expired"
	purchase := &domain.DigitalPurchase{
		ID:              "purch-2",
		ProductID:       "prod-2",
		TenantID:        "tenant-1",
		PaymentStatus:   "confirmed",
		DownloadCount:   0,
		AccessToken:     tok,
		AccessExpiresAt: &exp,
	}
	repo.byToken[tok] = purchase

	_, err := svc.Download(context.Background(), tok)
	if err == nil {
		t.Fatal("esperava erro por link expirado, mas não houve")
	}
}

func TestDownload_PagamentoNaoConfirmado(t *testing.T) {
	repo := newMockRepo()
	svc := service.NewLinkPayService(repo, nil, nil, nil)

	tok := "tok-pending"
	purchase := &domain.DigitalPurchase{
		ID:            "purch-3",
		ProductID:     "prod-3",
		PaymentStatus: "pending",
		AccessToken:   tok,
	}
	repo.byToken[tok] = purchase

	_, err := svc.Download(context.Background(), tok)
	if err == nil {
		t.Fatal("esperava erro por pagamento não confirmado, mas não houve")
	}
}

func TestDownload_Sucesso_IncrementaContador(t *testing.T) {
	repo := newMockRepo()
	svc := service.NewLinkPayService(repo, nil, nil, nil)

	prod := makeProduct("prod-4", "tenant-1", "video", 4900, true)
	prod.DownloadLimit = 5
	repo.products["prod-4"] = prod

	exp := time.Now().Add(7 * 24 * time.Hour)
	tok := "tok-ok"
	purchase := &domain.DigitalPurchase{
		ID:              "purch-4",
		ProductID:       "prod-4",
		TenantID:        "tenant-1",
		PaymentStatus:   "confirmed",
		DownloadCount:   1,
		AccessToken:     tok,
		AccessExpiresAt: &exp,
	}
	repo.byToken[tok] = purchase

	url, err := svc.Download(context.Background(), tok)
	if err != nil {
		t.Fatalf("Download retornou erro inesperado: %v", err)
	}
	if url == "" {
		t.Error("URL de download vazia")
	}
	if !repo.downloadIncremented {
		t.Error("IncrementDownload não foi chamado")
	}
}

// ─── testes de ProcessPaymentConfirmed ───────────────────────────────────────

func TestProcessPaymentConfirmed_Idempotente(t *testing.T) {
	repo := newMockRepo()
	pub := &mockPublisher{}
	svc := service.NewLinkPayService(repo, nil, pub, nil)

	// Compra já confirmada
	chargeID := "charge-already-confirmed"
	purchase := &domain.DigitalPurchase{
		ID:            "purch-5",
		ProductID:     "prod-5",
		TenantID:      "tenant-1",
		PaymentStatus: "confirmed",
		AccessToken:   "tok-conf",
	}
	repo.purchases[chargeID] = purchase
	repo.byID["purch-5"] = purchase

	err := svc.ProcessPaymentConfirmed(context.Background(), chargeID)
	if err != nil {
		t.Fatalf("ProcessPaymentConfirmed retornou erro: %v", err)
	}
	// Não deve ter chamado ConfirmPurchase nem publicado evento
	if repo.confirmCalled {
		t.Error("ConfirmPurchase chamado mesmo com compra já confirmada (não idempotente)")
	}
	if pub.called {
		t.Error("publisher chamado mesmo com compra já confirmada")
	}
}

func TestProcessPaymentConfirmed_ChargeDesconhecido(t *testing.T) {
	repo := newMockRepo()
	svc := service.NewLinkPayService(repo, nil, nil, nil)

	// chargeID de outro serviço — deve ignorar sem erro
	err := svc.ProcessPaymentConfirmed(context.Background(), "charge-unknown")
	if err != nil {
		t.Fatalf("esperava nil para charge desconhecido, got: %v", err)
	}
}

func TestProcessPaymentConfirmed_PublicaEvento(t *testing.T) {
	repo := newMockRepo()
	pub := &mockPublisher{}
	svc := service.NewLinkPayService(repo, nil, pub, nil)

	prod := makeProduct("prod-6", "tenant-1", "livro", 2500, true)
	repo.products["prod-6"] = prod

	chargeID := "charge-new"
	purchase := &domain.DigitalPurchase{
		ID:            "purch-6",
		ProductID:     "prod-6",
		TenantID:      "tenant-1",
		CustomerEmail: "buyer@example.com",
		CustomerName:  "João Silva",
		AmountCents:   2500,
		PaymentStatus: "pending",
		AccessToken:   "",
	}
	repo.purchases[chargeID] = purchase
	repo.byID["purch-6"] = purchase

	err := svc.ProcessPaymentConfirmed(context.Background(), chargeID)
	if err != nil {
		t.Fatalf("ProcessPaymentConfirmed retornou erro: %v", err)
	}
	if !repo.confirmCalled {
		t.Error("ConfirmPurchase não foi chamado")
	}
	if !pub.called {
		t.Error("evento de compra confirmada não foi publicado")
	}
}
