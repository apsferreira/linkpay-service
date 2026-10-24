package service_test

// Shared, coverage-only test doubles. Every symbol is prefixed with cov/Cov so
// it cannot collide with the doubles declared by the pre-existing test files
// (mockRepo, mockPayoutRepo, fakeAsaas, mockRedis, fakeSubscriptionRepo, ...).

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"linkpay-service/internal/domain"
	"linkpay-service/internal/messaging"
	"linkpay-service/internal/pkg/pulse"
	"linkpay-service/internal/repository"
	"linkpay-service/internal/service"
)

// ─── LinkPay repository ──────────────────────────────────────────────────────

type covPurchaseStatusUpdate struct{ id, status, chargeID string }

type covLinkRepo struct {
	service.LinkPayRepoInterface

	mu        sync.Mutex
	products  map[string]*domain.DigitalProduct
	purchases map[string]*domain.DigitalPurchase // by ID
	byToken   map[string]*domain.DigitalPurchase
	byCharge  map[string]*domain.DigitalPurchase
	files     map[string]*domain.DigitalProductFile
	upsells   []domain.UpsellSuggestion

	getProductErr           error
	createProductErr        error
	listProductsErr         error
	updateProductErr        error
	deactivateErr           error
	createPurchaseErr       error
	updatePurchaseStatusErr error
	confirmErr              error
	incrementErr            error
	statsErr                error
	addFileErr              error
	listFilesErr            error
	getFileErr              error
	deleteFileErr           error
	upsellErr               error
	updateCoverErr          error
	updateStorageErr        error
	insertCommissionErr     error
	commissionSummaryErr    error
	listPurchasesErr        error
	erasePurchasesErr       error
	eraseOlderErr           error

	stats             *domain.LinkPayStatsResponse
	commissionSummary *domain.CommissionSummaryResponse
	buyerPurchases    []domain.BuyerPurchaseExport

	confirmCalls        int
	lastConfirmedToken  string
	lastCommissionEntry *domain.CommissionEntry
	lastStorageKey      string
	lastStorageBucket   string
	lastFileName        string
	lastMimeType        string
	lastFileSize        int64
	lastCoverURL        string
	deleteFileCalls     int
	statusUpdates       []covPurchaseStatusUpdate
}

func covNewLinkRepo() *covLinkRepo {
	return &covLinkRepo{
		products:  map[string]*domain.DigitalProduct{},
		purchases: map[string]*domain.DigitalPurchase{},
		byToken:   map[string]*domain.DigitalPurchase{},
		byCharge:  map[string]*domain.DigitalPurchase{},
		files:     map[string]*domain.DigitalProductFile{},
	}
}

func (m *covLinkRepo) CreateProduct(_ context.Context, p *domain.DigitalProduct) error {
	if m.createProductErr != nil {
		return m.createProductErr
	}
	m.products[p.ID] = p
	return nil
}

func (m *covLinkRepo) GetProductBySlug(_ context.Context, slug string) (*domain.DigitalProduct, error) {
	if m.getProductErr != nil {
		return nil, m.getProductErr
	}
	for _, p := range m.products {
		if p.Slug == slug {
			return p, nil
		}
	}
	return nil, errors.New("not found")
}

func (m *covLinkRepo) GetProductByID(_ context.Context, id, _ string) (*domain.DigitalProduct, error) {
	if m.getProductErr != nil {
		return nil, m.getProductErr
	}
	if p, ok := m.products[id]; ok {
		return p, nil
	}
	return nil, errors.New("not found")
}

func (m *covLinkRepo) ListProducts(_ context.Context, tenantID string) ([]domain.DigitalProduct, error) {
	if m.listProductsErr != nil {
		return nil, m.listProductsErr
	}
	out := []domain.DigitalProduct{}
	for _, p := range m.products {
		if p.TenantID == tenantID {
			out = append(out, *p)
		}
	}
	return out, nil
}

func (m *covLinkRepo) UpdateProduct(_ context.Context, p *domain.DigitalProduct) error {
	if m.updateProductErr != nil {
		return m.updateProductErr
	}
	m.products[p.ID] = p
	return nil
}

func (m *covLinkRepo) UpdateProductStorage(_ context.Context, id, _ string, storageKey, storageBucket, fileName string, fileSizeBytes int64, mimeType string) error {
	if m.updateStorageErr != nil {
		return m.updateStorageErr
	}
	m.lastStorageKey, m.lastStorageBucket, m.lastFileName = storageKey, storageBucket, fileName
	m.lastFileSize, m.lastMimeType = fileSizeBytes, mimeType
	if p, ok := m.products[id]; ok {
		p.StorageKey = storageKey
	}
	return nil
}

func (m *covLinkRepo) DeactivateProduct(_ context.Context, id, _ string) error {
	if m.deactivateErr != nil {
		return m.deactivateErr
	}
	if p, ok := m.products[id]; ok {
		p.IsActive = false
	}
	return nil
}

func (m *covLinkRepo) CreatePurchase(_ context.Context, p *domain.DigitalPurchase) error {
	if m.createPurchaseErr != nil {
		return m.createPurchaseErr
	}
	m.purchases[p.ID] = p
	return nil
}

func (m *covLinkRepo) GetPurchaseByToken(_ context.Context, token string) (*domain.DigitalPurchase, error) {
	if p, ok := m.byToken[token]; ok {
		return p, nil
	}
	return nil, errors.New("not found")
}

func (m *covLinkRepo) GetPurchaseByID(_ context.Context, id string) (*domain.DigitalPurchase, error) {
	if p, ok := m.purchases[id]; ok {
		return p, nil
	}
	return nil, errors.New("not found")
}

func (m *covLinkRepo) GetPurchaseByChargeID(_ context.Context, chargeID string) (*domain.DigitalPurchase, error) {
	if p, ok := m.byCharge[chargeID]; ok {
		return p, nil
	}
	return nil, errors.New("not found")
}

func (m *covLinkRepo) UpdatePurchaseStatus(_ context.Context, id, status, chargeID string) error {
	if m.updatePurchaseStatusErr != nil {
		return m.updatePurchaseStatusErr
	}
	if p, ok := m.purchases[id]; ok {
		p.PaymentStatus = status
	}
	m.statusUpdates = append(m.statusUpdates, covPurchaseStatusUpdate{id, status, chargeID})
	return nil
}

func (m *covLinkRepo) ConfirmPurchase(_ context.Context, id, accessToken string, expiresAt time.Time) error {
	if m.confirmErr != nil {
		return m.confirmErr
	}
	m.confirmCalls++
	m.lastConfirmedToken = accessToken
	if p, ok := m.purchases[id]; ok {
		p.PaymentStatus = "confirmed"
		p.AccessToken = accessToken
		p.AccessExpiresAt = &expiresAt
		m.byToken[accessToken] = p
	}
	return nil
}

func (m *covLinkRepo) IncrementDownload(_ context.Context, token string) error {
	if m.incrementErr != nil {
		return m.incrementErr
	}
	if p, ok := m.byToken[token]; ok {
		p.DownloadCount++
	}
	return nil
}

func (m *covLinkRepo) GetSalesStats(_ context.Context, _ string, days int) (*domain.LinkPayStatsResponse, error) {
	if m.statsErr != nil {
		return nil, m.statsErr
	}
	if m.stats != nil {
		return m.stats, nil
	}
	return &domain.LinkPayStatsResponse{Period: "30d"}, nil
}

func (m *covLinkRepo) AddProductFile(_ context.Context, f *domain.DigitalProductFile) error {
	if m.addFileErr != nil {
		return m.addFileErr
	}
	m.files[f.ID] = f
	return nil
}

func (m *covLinkRepo) ListProductFiles(_ context.Context, productID string) ([]domain.DigitalProductFile, error) {
	if m.listFilesErr != nil {
		return nil, m.listFilesErr
	}
	out := []domain.DigitalProductFile{}
	for _, f := range m.files {
		if f.ProductID == productID {
			out = append(out, *f)
		}
	}
	return out, nil
}

func (m *covLinkRepo) GetProductFile(_ context.Context, fileID string) (*domain.DigitalProductFile, error) {
	if m.getFileErr != nil {
		return nil, m.getFileErr
	}
	if f, ok := m.files[fileID]; ok {
		return f, nil
	}
	return nil, errors.New("not found")
}

func (m *covLinkRepo) DeleteProductFile(_ context.Context, fileID, _ string) error {
	m.deleteFileCalls++
	if m.deleteFileErr != nil {
		return m.deleteFileErr
	}
	delete(m.files, fileID)
	return nil
}

func (m *covLinkRepo) GetUpsellsByProductID(_ context.Context, productID string) ([]domain.UpsellSuggestion, error) {
	if m.upsellErr != nil {
		return nil, m.upsellErr
	}
	return m.upsells, nil
}

func (m *covLinkRepo) UpdateProductCover(_ context.Context, id, _, coverImageURL string) error {
	if m.updateCoverErr != nil {
		return m.updateCoverErr
	}
	m.lastCoverURL = coverImageURL
	if p, ok := m.products[id]; ok {
		p.CoverImageURL = coverImageURL
	}
	return nil
}

func (m *covLinkRepo) InsertCommissionEntry(_ context.Context, e *domain.CommissionEntry) error {
	if m.insertCommissionErr != nil {
		return m.insertCommissionErr
	}
	m.lastCommissionEntry = e
	return nil
}

func (m *covLinkRepo) GetCommissionSummary(_ context.Context, _, _ string) (*domain.CommissionSummaryResponse, error) {
	if m.commissionSummaryErr != nil {
		return nil, m.commissionSummaryErr
	}
	if m.commissionSummary != nil {
		return m.commissionSummary, nil
	}
	return &domain.CommissionSummaryResponse{TotalSales: 1}, nil
}

func (m *covLinkRepo) ListPurchasesByEmail(_ context.Context, _ string) ([]domain.BuyerPurchaseExport, error) {
	if m.listPurchasesErr != nil {
		return nil, m.listPurchasesErr
	}
	return m.buyerPurchases, nil
}

func (m *covLinkRepo) ErasePurchasesByEmail(_ context.Context, _ string) (int64, error) {
	if m.erasePurchasesErr != nil {
		return 0, m.erasePurchasesErr
	}
	return 0, nil
}

func (m *covLinkRepo) ErasePurchasesOlderThan(_ context.Context, _ time.Time) (int64, error) {
	if m.eraseOlderErr != nil {
		return 0, m.eraseOlderErr
	}
	return 0, nil
}

// ─── Subscription repository ─────────────────────────────────────────────────

type covSubStatusUpdate struct {
	id        string
	status    string
	periodEnd *time.Time
}

type covSubRepo struct {
	repository.SubscriptionRepoInterface

	mu          sync.Mutex
	plans       map[string]*domain.DigitalSubscriptionPlan
	subsByToken map[string]*domain.DigitalSubscription
	subsByAsaas map[string]*domain.DigitalSubscription
	byTenant    map[string][]domain.DigitalSubscription
	planList    []domain.DigitalSubscriptionPlan

	createPlanErr     error
	getPlanErr        error
	listPlansErr      error
	updatePlanErr     error
	createSubErr      error
	getSubTokenErr    error
	getSubAsaasErr    error
	updateStatusErr   error
	cancelErr         error
	listByTenantErr   error
	listByEmailErr    error
	eraseByEmailErr   error
	eraseOlderErr     error
	listByEmailResult []domain.BuyerSubscriptionExport
	eraseByEmailCount int64
	eraseOlderCount   int64

	createdPlans  []domain.DigitalSubscriptionPlan
	updatedPlans  []domain.DigitalSubscriptionPlan
	createdSubs   []domain.DigitalSubscription
	statusUpdates []covSubStatusUpdate
	cancelledIDs  []string
}

func covNewSubRepo() *covSubRepo {
	return &covSubRepo{
		plans:       map[string]*domain.DigitalSubscriptionPlan{},
		subsByToken: map[string]*domain.DigitalSubscription{},
		subsByAsaas: map[string]*domain.DigitalSubscription{},
		byTenant:    map[string][]domain.DigitalSubscription{},
	}
}

func (m *covSubRepo) CreatePlan(_ context.Context, p *domain.DigitalSubscriptionPlan) error {
	if m.createPlanErr != nil {
		return m.createPlanErr
	}
	m.plans[p.ID] = p
	m.createdPlans = append(m.createdPlans, *p)
	return nil
}

func (m *covSubRepo) GetPlanByID(_ context.Context, planID, _ string) (*domain.DigitalSubscriptionPlan, error) {
	if m.getPlanErr != nil {
		return nil, m.getPlanErr
	}
	if p, ok := m.plans[planID]; ok {
		return p, nil
	}
	return nil, errors.New("not found")
}

func (m *covSubRepo) ListPlansByProduct(_ context.Context, productID string) ([]domain.DigitalSubscriptionPlan, error) {
	if m.listPlansErr != nil {
		return nil, m.listPlansErr
	}
	out := []domain.DigitalSubscriptionPlan{}
	for _, p := range m.plans {
		if p.ProductID == productID {
			out = append(out, *p)
		}
	}
	if len(out) == 0 {
		return m.planList, nil
	}
	return out, nil
}

func (m *covSubRepo) UpdatePlan(_ context.Context, p *domain.DigitalSubscriptionPlan) error {
	if m.updatePlanErr != nil {
		return m.updatePlanErr
	}
	m.plans[p.ID] = p
	m.updatedPlans = append(m.updatedPlans, *p)
	return nil
}

func (m *covSubRepo) DeactivatePlan(_ context.Context, planID, _ string) error {
	if p, ok := m.plans[planID]; ok {
		p.IsActive = false
	}
	return nil
}

func (m *covSubRepo) CreateSubscription(_ context.Context, s *domain.DigitalSubscription) error {
	if m.createSubErr != nil {
		return m.createSubErr
	}
	m.subsByToken[s.AccessToken] = s
	m.createdSubs = append(m.createdSubs, *s)
	return nil
}

func (m *covSubRepo) GetSubscriptionByToken(_ context.Context, token string) (*domain.DigitalSubscription, error) {
	if m.getSubTokenErr != nil {
		return nil, m.getSubTokenErr
	}
	if s, ok := m.subsByToken[token]; ok {
		return s, nil
	}
	return nil, errors.New("not found")
}

func (m *covSubRepo) GetSubscriptionByAsaasID(_ context.Context, asaasID string) (*domain.DigitalSubscription, error) {
	if m.getSubAsaasErr != nil {
		return nil, m.getSubAsaasErr
	}
	if s, ok := m.subsByAsaas[asaasID]; ok {
		return s, nil
	}
	return nil, errors.New("not found")
}

func (m *covSubRepo) UpdateSubscriptionStatus(_ context.Context, id, status string, periodEnd *time.Time) error {
	if m.updateStatusErr != nil {
		return m.updateStatusErr
	}
	m.statusUpdates = append(m.statusUpdates, covSubStatusUpdate{id, status, periodEnd})
	return nil
}

func (m *covSubRepo) CancelSubscription(_ context.Context, id string) error {
	if m.cancelErr != nil {
		return m.cancelErr
	}
	m.cancelledIDs = append(m.cancelledIDs, id)
	return nil
}

func (m *covSubRepo) ListSubscriptionsByTenant(_ context.Context, tenantID string) ([]domain.DigitalSubscription, error) {
	if m.listByTenantErr != nil {
		return nil, m.listByTenantErr
	}
	return m.byTenant[tenantID], nil
}

func (m *covSubRepo) UpdateSubscriptionAsaasID(_ context.Context, _, _ string) error { return nil }

func (m *covSubRepo) ListSubscriptionsByEmail(_ context.Context, _ string) ([]domain.BuyerSubscriptionExport, error) {
	if m.listByEmailErr != nil {
		return nil, m.listByEmailErr
	}
	return m.listByEmailResult, nil
}

func (m *covSubRepo) EraseSubscriptionsByEmail(_ context.Context, _ string) (int64, error) {
	if m.eraseByEmailErr != nil {
		return 0, m.eraseByEmailErr
	}
	return m.eraseByEmailCount, nil
}

func (m *covSubRepo) EraseSubscriptionsOlderThan(_ context.Context, _ time.Time) (int64, error) {
	if m.eraseOlderErr != nil {
		return 0, m.eraseOlderErr
	}
	return m.eraseOlderCount, nil
}

// ─── messaging / notification doubles ───────────────────────────────────────

type covPublisher struct {
	err    error
	events []messaging.LinkPayPurchaseConfirmedEvent
}

func (p *covPublisher) PublishLinkPayPurchaseConfirmed(_ context.Context, e messaging.LinkPayPurchaseConfirmedEvent) error {
	p.events = append(p.events, e)
	return p.err
}

type covEmailSender struct {
	err    error
	calls  int
	lastTo string
}

func (s *covEmailSender) SendPurchaseConfirmation(_ context.Context, to, _, _, _ string, _ int) error {
	s.calls++
	s.lastTo = to
	return s.err
}

type covNotifier struct {
	err    error
	calls  int
	lastTo string
}

func (n *covNotifier) SendPurchaseConfirmation(_ context.Context, buyerEmail, _, _, _ string, _ int) error {
	n.calls++
	n.lastTo = buyerEmail
	return n.err
}

// ─── Asaas HTTP fake ────────────────────────────────────────────────────────

type covAsaasServer struct {
	srv         *httptest.Server
	mu          sync.Mutex
	calls       int
	paths       []string
	bodies      []string
	pulseSrv    *httptest.Server
	pulseMu     sync.Mutex
	pulseEvents int
}

func covNewAsaasServer(h http.HandlerFunc) *covAsaasServer {
	a := &covAsaasServer{}
	a.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		a.calls++
		a.paths = append(a.paths, r.Method+" "+r.URL.Path)
		a.mu.Unlock()
		h(w, r)
	}))
	return a
}

func (a *covAsaasServer) client() *service.AsaasClient {
	return service.NewAsaasClient(a.srv.URL, "cov-key", nil, nil)
}

func (a *covAsaasServer) clientWithPulse(t *testing.T) *service.AsaasClient {
	t.Helper()
	a.pulseSrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		a.pulseMu.Lock()
		a.pulseEvents++
		a.pulseMu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(a.pulseSrv.Close)
	t.Setenv("PULSE_URL", a.pulseSrv.URL)
	return service.NewAsaasClient(a.srv.URL, "cov-key", nil, pulse.New())
}

func (a *covAsaasServer) callCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.calls
}

func (a *covAsaasServer) close() { a.srv.Close() }

// covJSON writes a JSON body with the given status.
func covJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

// covDecode decodes the last request body captured for a path suffix match.
func covDecodeBody[T any](t *testing.T, r *http.Request) T {
	t.Helper()
	var v T
	if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	return v
}
