package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"linkpay-service/internal/domain"
	"linkpay-service/internal/repository"
	"linkpay-service/internal/service"
)

// ─── mocks ───────────────────────────────────────────────────────────────────

type mockPayoutRepo struct {
	mu sync.Mutex

	tenants            []string
	tenantsErr         error
	pixKeys            map[string]string
	pixKeyErr          error
	eligible           map[string][]repository.EligiblePurchase
	eligibleErr        error
	anticipationElig   map[string][]repository.AnticipationEligiblePurchase
	anticipationErr    error
	balance            map[string]*domain.Balance
	balanceErr         error
	listPayouts        map[string][]domain.CreatorPayout
	listPayoutsErr     error
	createPayoutErr    error
	createAnticipErr   error
	updateTransferErr  error
	updateStatusErr    error

	createPayoutCalls       int32
	createAnticipationCalls int32
	updateTransferCalls     int32
	updateStatusCalls       int32

	createdPayouts        []domain.CreatorPayout
	createdAnticipations  []domain.CreatorPayout
	transferUpdates       []transferUpdate
	statusUpdates         []statusUpdate
	lastPurchaseIDs       []string
	lastAnticipationIDs   []string
}

type transferUpdate struct{ payoutID, transferID, status string }
type statusUpdate struct{ asaasTransferID, status, errMsg string }

func newMockPayoutRepo() *mockPayoutRepo {
	return &mockPayoutRepo{
		pixKeys:          make(map[string]string),
		eligible:         make(map[string][]repository.EligiblePurchase),
		anticipationElig: make(map[string][]repository.AnticipationEligiblePurchase),
		balance:          make(map[string]*domain.Balance),
		listPayouts:      make(map[string][]domain.CreatorPayout),
	}
}

func (m *mockPayoutRepo) GetEligiblePurchases(_ context.Context, tenantID string) ([]repository.EligiblePurchase, error) {
	if m.eligibleErr != nil {
		return nil, m.eligibleErr
	}
	return m.eligible[tenantID], nil
}
func (m *mockPayoutRepo) GetAnticipationEligiblePurchases(_ context.Context, tenantID string) ([]repository.AnticipationEligiblePurchase, error) {
	if m.anticipationErr != nil {
		return nil, m.anticipationErr
	}
	return m.anticipationElig[tenantID], nil
}
func (m *mockPayoutRepo) CreateAnticipationPayout(_ context.Context, payout *domain.CreatorPayout, purchaseIDs []string) error {
	atomic.AddInt32(&m.createAnticipationCalls, 1)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastAnticipationIDs = append([]string(nil), purchaseIDs...)
	m.createdAnticipations = append(m.createdAnticipations, *payout)
	return m.createAnticipErr
}
func (m *mockPayoutRepo) GetTenantsWithEligiblePayouts(_ context.Context) ([]string, error) {
	return m.tenants, m.tenantsErr
}
func (m *mockPayoutRepo) GetTenantPixKey(_ context.Context, tenantID string) (string, error) {
	if m.pixKeyErr != nil {
		return "", m.pixKeyErr
	}
	return m.pixKeys[tenantID], nil
}
func (m *mockPayoutRepo) GetBalance(_ context.Context, tenantID string) (*domain.Balance, error) {
	if m.balanceErr != nil {
		return nil, m.balanceErr
	}
	if b, ok := m.balance[tenantID]; ok {
		return b, nil
	}
	return &domain.Balance{}, nil
}
func (m *mockPayoutRepo) CreatePayout(_ context.Context, payout *domain.CreatorPayout, purchaseIDs []string) error {
	atomic.AddInt32(&m.createPayoutCalls, 1)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastPurchaseIDs = append([]string(nil), purchaseIDs...)
	m.createdPayouts = append(m.createdPayouts, *payout)
	return m.createPayoutErr
}
func (m *mockPayoutRepo) UpdatePayoutTransferID(_ context.Context, payoutID, transferID, status string) error {
	atomic.AddInt32(&m.updateTransferCalls, 1)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.transferUpdates = append(m.transferUpdates, transferUpdate{payoutID, transferID, status})
	return m.updateTransferErr
}
func (m *mockPayoutRepo) UpdatePayoutStatus(_ context.Context, asaasTransferID, status, errMsg string) error {
	atomic.AddInt32(&m.updateStatusCalls, 1)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.statusUpdates = append(m.statusUpdates, statusUpdate{asaasTransferID, status, errMsg})
	return m.updateStatusErr
}
func (m *mockPayoutRepo) ListPayouts(_ context.Context, tenantID string, _, _ int) ([]domain.CreatorPayout, int, error) {
	if m.listPayoutsErr != nil {
		return nil, 0, m.listPayoutsErr
	}
	payouts := m.listPayouts[tenantID]
	return payouts, len(payouts), nil
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// fakeAsaas devolve um *AsaasClient apontando para um httptest.Server controlável.
type fakeAsaas struct {
	server       *httptest.Server
	transferID   string
	failNextWith int    // 0 = sucesso; >0 = devolve status code
	failBody     string
	calls        int32
	lastRequest  service.AsaasTransferRequest
}

func newFakeAsaas() *fakeAsaas {
	f := &fakeAsaas{transferID: "asaas-transfer-1"}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&f.calls, 1)
		if r.URL.Path != "/transfers" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		var req service.AsaasTransferRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.lastRequest = req
		if f.failNextWith != 0 {
			w.WriteHeader(f.failNextWith)
			_, _ = w.Write([]byte(f.failBody))
			return
		}
		_ = json.NewEncoder(w).Encode(service.AsaasTransferResponse{
			ID:     f.transferID,
			Status: "PENDING",
			Value:  req.Value,
		})
	}))
	return f
}

func (f *fakeAsaas) close() { f.server.Close() }

func (f *fakeAsaas) client() *service.AsaasClient {
	return service.NewAsaasClient(f.server.URL, "test-key", nil, nil)
}

// ─── RunDailyPayouts ─────────────────────────────────────────────────────────

func TestRunDailyPayouts_SemTenants(t *testing.T) {
	repo := newMockPayoutRepo()
	asaas := newFakeAsaas()
	defer asaas.close()

	svc := service.NewPayoutService(repo, asaas.client(), nil)
	if err := svc.RunDailyPayouts(context.Background()); err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if atomic.LoadInt32(&asaas.calls) != 0 {
		t.Errorf("Asaas chamado sem tenants elegíveis")
	}
	if atomic.LoadInt32(&repo.createPayoutCalls) != 0 {
		t.Errorf("CreatePayout chamado sem tenants")
	}
}

func TestRunDailyPayouts_ErroNaListaDeTenants(t *testing.T) {
	repo := newMockPayoutRepo()
	repo.tenantsErr = errors.New("db down")
	asaas := newFakeAsaas()
	defer asaas.close()

	svc := service.NewPayoutService(repo, asaas.client(), nil)
	if err := svc.RunDailyPayouts(context.Background()); err == nil {
		t.Fatal("esperava erro quando GetTenantsWithEligiblePayouts falha")
	}
}

func TestRunDailyPayouts_TenantSemPixKey_Pula(t *testing.T) {
	repo := newMockPayoutRepo()
	repo.tenants = []string{"tenant-1"}
	// pix_key vazia → tenant deve ser pulado sem erro e sem chamar Asaas
	repo.pixKeys["tenant-1"] = ""
	repo.eligible["tenant-1"] = []repository.EligiblePurchase{
		{ID: "p1", CreatorEarnings: 50000},
	}

	asaas := newFakeAsaas()
	defer asaas.close()

	svc := service.NewPayoutService(repo, asaas.client(), nil)
	if err := svc.RunDailyPayouts(context.Background()); err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if atomic.LoadInt32(&asaas.calls) != 0 {
		t.Errorf("Asaas chamado para tenant sem pix_key")
	}
	if atomic.LoadInt32(&repo.createPayoutCalls) != 0 {
		t.Errorf("CreatePayout chamado para tenant sem pix_key")
	}
}

func TestRunDailyPayouts_AbaixoDoMinimo_Pula(t *testing.T) {
	repo := newMockPayoutRepo()
	repo.tenants = []string{"tenant-1"}
	repo.pixKeys["tenant-1"] = "user@example.com"
	// Após 10% de reserva, sobra R$9,00 < R$10,00 mínimo
	repo.eligible["tenant-1"] = []repository.EligiblePurchase{
		{ID: "p1", CreatorEarnings: 1000},
	}
	asaas := newFakeAsaas()
	defer asaas.close()

	svc := service.NewPayoutService(repo, asaas.client(), nil)
	if err := svc.RunDailyPayouts(context.Background()); err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if atomic.LoadInt32(&asaas.calls) != 0 {
		t.Errorf("Asaas chamado mesmo com saldo abaixo do mínimo")
	}
	if atomic.LoadInt32(&repo.createPayoutCalls) != 0 {
		t.Errorf("CreatePayout chamado mesmo abaixo do mínimo")
	}
}

func TestRunDailyPayouts_Sucesso_CriaTransferenciaAsaas(t *testing.T) {
	repo := newMockPayoutRepo()
	repo.tenants = []string{"tenant-1"}
	repo.pixKeys["tenant-1"] = "user@example.com"
	repo.eligible["tenant-1"] = []repository.EligiblePurchase{
		{ID: "p1", CreatorEarnings: 50000},
		{ID: "p2", CreatorEarnings: 50000},
	}
	asaas := newFakeAsaas()
	asaas.transferID = "tx-001"
	defer asaas.close()

	svc := service.NewPayoutService(repo, asaas.client(), nil)
	if err := svc.RunDailyPayouts(context.Background()); err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}

	if got := atomic.LoadInt32(&asaas.calls); got != 1 {
		t.Errorf("Asaas chamado %d vezes, queria 1", got)
	}
	if got := atomic.LoadInt32(&repo.createPayoutCalls); got != 1 {
		t.Errorf("CreatePayout chamado %d vezes, queria 1", got)
	}

	// 100000 brutos - 10% reserva = 90000 (R$900) líquidos para Asaas
	if asaas.lastRequest.Value != 900.0 {
		t.Errorf("valor enviado ao Asaas = %v, queria 900.0", asaas.lastRequest.Value)
	}
	if asaas.lastRequest.PixAddressKey != "user@example.com" {
		t.Errorf("PixAddressKey = %q, queria user@example.com", asaas.lastRequest.PixAddressKey)
	}
	if asaas.lastRequest.PixAddressKeyType != "EMAIL" {
		t.Errorf("PixAddressKeyType = %q, queria EMAIL", asaas.lastRequest.PixAddressKeyType)
	}

	if len(repo.transferUpdates) != 1 {
		t.Fatalf("UpdatePayoutTransferID chamado %d vezes", len(repo.transferUpdates))
	}
	if repo.transferUpdates[0].transferID != "tx-001" {
		t.Errorf("transferID salvo = %q, queria tx-001", repo.transferUpdates[0].transferID)
	}
	if repo.transferUpdates[0].status != "processing" {
		t.Errorf("status salvo = %q, queria processing", repo.transferUpdates[0].status)
	}
	if len(repo.lastPurchaseIDs) != 2 {
		t.Errorf("purchaseIDs persistidos = %d, queria 2", len(repo.lastPurchaseIDs))
	}
}

func TestRunDailyPayouts_AsaasFalha_MarcaPayoutFailed(t *testing.T) {
	repo := newMockPayoutRepo()
	repo.tenants = []string{"tenant-1"}
	repo.pixKeys["tenant-1"] = "user@example.com"
	repo.eligible["tenant-1"] = []repository.EligiblePurchase{
		{ID: "p1", CreatorEarnings: 50000},
	}
	asaas := newFakeAsaas()
	asaas.failNextWith = http.StatusInternalServerError
	asaas.failBody = "boom"
	defer asaas.close()

	svc := service.NewPayoutService(repo, asaas.client(), nil)
	err := svc.RunDailyPayouts(context.Background())
	if err == nil {
		t.Fatal("esperava erro propagado quando Asaas falha")
	}
	if atomic.LoadInt32(&repo.createPayoutCalls) != 1 {
		t.Errorf("CreatePayout deveria ter sido chamado antes do Asaas")
	}
	// Deve marcar como failed no banco
	if atomic.LoadInt32(&repo.updateTransferCalls) == 0 {
		t.Errorf("payout falho deveria ter UpdatePayoutTransferID chamado para marcar failed")
	}
	gotFailed := false
	for _, u := range repo.transferUpdates {
		if u.status == "failed" {
			gotFailed = true
		}
	}
	if !gotFailed {
		t.Errorf("nenhuma atualização marcou payout como failed")
	}
}

// ─── RequestManualPayout ────────────────────────────────────────────────────

func TestRequestManualPayout_AbaixoMinimo(t *testing.T) {
	repo := newMockPayoutRepo()
	repo.balance["tenant-1"] = &domain.Balance{AvailableCents: 500}
	asaas := newFakeAsaas()
	defer asaas.close()

	svc := service.NewPayoutService(repo, asaas.client(), nil)
	_, err := svc.RequestManualPayout(context.Background(), "tenant-1", "chave-pix", 0)
	if err == nil || !strings.Contains(err.Error(), "abaixo do mínimo") {
		t.Fatalf("esperava erro de mínimo, got %v", err)
	}
}

func TestRequestManualPayout_ValorMaiorQueDisponivel(t *testing.T) {
	repo := newMockPayoutRepo()
	repo.balance["tenant-1"] = &domain.Balance{AvailableCents: 5000}
	asaas := newFakeAsaas()
	defer asaas.close()

	svc := service.NewPayoutService(repo, asaas.client(), nil)
	_, err := svc.RequestManualPayout(context.Background(), "tenant-1", "chave-pix", 999900)
	if err == nil || !strings.Contains(err.Error(), "superior ao disponível") {
		t.Fatalf("esperava erro de saldo insuficiente, got %v", err)
	}
}

func TestRequestManualPayout_Sucesso(t *testing.T) {
	repo := newMockPayoutRepo()
	repo.balance["tenant-1"] = &domain.Balance{AvailableCents: 90000}
	// Disponíveis bruto: 100000 (após 10% reserva = 90000 líquido)
	repo.eligible["tenant-1"] = []repository.EligiblePurchase{
		{ID: "p1", CreatorEarnings: 100000},
	}
	repo.listPayouts["tenant-1"] = []domain.CreatorPayout{
		{ID: "pay-1", TenantID: "tenant-1", AmountCents: 90000, Status: "processing"},
	}
	asaas := newFakeAsaas()
	defer asaas.close()

	svc := service.NewPayoutService(repo, asaas.client(), nil)
	payout, err := svc.RequestManualPayout(context.Background(), "tenant-1", "chave-pix", 0)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if payout == nil || payout.ID != "pay-1" {
		t.Errorf("payout recém-criado não recuperado corretamente: %+v", payout)
	}
	if atomic.LoadInt32(&asaas.calls) != 1 {
		t.Errorf("Asaas não chamado")
	}
}

// ─── PreviewAnticipation / RequestAnticipation ─────────────────────────────

func TestPreviewAnticipation_SemElegiveis(t *testing.T) {
	repo := newMockPayoutRepo()
	asaas := newFakeAsaas()
	defer asaas.close()

	svc := service.NewPayoutService(repo, asaas.client(), nil)
	preview, err := svc.PreviewAnticipation(context.Background(), "tenant-1")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if preview != nil {
		t.Errorf("esperava preview nil para tenant sem compras, got %+v", preview)
	}
}

func TestPreviewAnticipation_CalculaTaxaECorpo(t *testing.T) {
	repo := newMockPayoutRepo()
	repo.anticipationElig["tenant-1"] = []repository.AnticipationEligiblePurchase{
		{ID: "c1", CreatorEarnings: 50000},
		{ID: "c2", CreatorEarnings: 50000},
	}
	asaas := newFakeAsaas()
	defer asaas.close()

	svc := service.NewPayoutService(repo, asaas.client(), nil)
	preview, err := svc.PreviewAnticipation(context.Background(), "tenant-1")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if preview.EligibleCents != 100000 {
		t.Errorf("EligibleCents = %d, queria 100000", preview.EligibleCents)
	}
	if preview.FeeCents != 2000 {
		t.Errorf("FeeCents = %d, queria 2000 (2%%)", preview.FeeCents)
	}
	if preview.NetCents != 98000 {
		t.Errorf("NetCents = %d, queria 98000", preview.NetCents)
	}
	if preview.EligiblePurchases != 2 {
		t.Errorf("EligiblePurchases = %d, queria 2", preview.EligiblePurchases)
	}
}

func TestRequestAnticipation_SemPixKey(t *testing.T) {
	repo := newMockPayoutRepo()
	asaas := newFakeAsaas()
	defer asaas.close()

	svc := service.NewPayoutService(repo, asaas.client(), nil)
	_, err := svc.RequestAnticipation(context.Background(), "tenant-1", "")
	if err == nil || !strings.Contains(err.Error(), "chave PIX") {
		t.Fatalf("esperava erro de pix_key ausente, got %v", err)
	}
}

func TestRequestAnticipation_ValorAbaixoDoMinimo(t *testing.T) {
	repo := newMockPayoutRepo()
	repo.anticipationElig["tenant-1"] = []repository.AnticipationEligiblePurchase{
		{ID: "c1", CreatorEarnings: 500}, // R$5 -> líquido R$4,90
	}
	asaas := newFakeAsaas()
	defer asaas.close()

	svc := service.NewPayoutService(repo, asaas.client(), nil)
	_, err := svc.RequestAnticipation(context.Background(), "tenant-1", "chave-pix")
	if err == nil || !strings.Contains(err.Error(), "mínimo") {
		t.Fatalf("esperava erro de mínimo, got %v", err)
	}
}

func TestRequestAnticipation_Sucesso(t *testing.T) {
	repo := newMockPayoutRepo()
	repo.anticipationElig["tenant-1"] = []repository.AnticipationEligiblePurchase{
		{ID: "c1", CreatorEarnings: 100000},
	}
	asaas := newFakeAsaas()
	asaas.transferID = "tx-antecipa"
	defer asaas.close()

	svc := service.NewPayoutService(repo, asaas.client(), nil)
	payout, err := svc.RequestAnticipation(context.Background(), "tenant-1", "chave-pix")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if !payout.IsAnticipation {
		t.Errorf("IsAnticipation = false")
	}
	if payout.AnticipationFeeCents != 2000 {
		t.Errorf("AnticipationFeeCents = %d, queria 2000", payout.AnticipationFeeCents)
	}
	if payout.AmountCents != 98000 {
		t.Errorf("AmountCents = %d, queria 98000 (líquido)", payout.AmountCents)
	}
	if payout.Status != "scheduled" {
		t.Errorf("Status = %q, queria scheduled", payout.Status)
	}
	if payout.ScheduledFor == nil {
		t.Errorf("ScheduledFor não preenchido")
	}
	if asaas.lastRequest.ScheduleDate == "" {
		t.Errorf("ScheduleDate ausente no payload Asaas")
	}
	if got := atomic.LoadInt32(&repo.createAnticipationCalls); got != 1 {
		t.Errorf("CreateAnticipationPayout chamado %d vezes", got)
	}
}

func TestRequestAnticipation_UsaPixKeySalva(t *testing.T) {
	repo := newMockPayoutRepo()
	repo.pixKeys["tenant-1"] = "user@example.com"
	repo.anticipationElig["tenant-1"] = []repository.AnticipationEligiblePurchase{
		{ID: "c1", CreatorEarnings: 100000},
	}
	asaas := newFakeAsaas()
	defer asaas.close()

	svc := service.NewPayoutService(repo, asaas.client(), nil)
	if _, err := svc.RequestAnticipation(context.Background(), "tenant-1", ""); err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if asaas.lastRequest.PixAddressKey != "user@example.com" {
		t.Errorf("PixAddressKey enviada = %q, queria a salva", asaas.lastRequest.PixAddressKey)
	}
}

// ─── HandleTransferWebhook ───────────────────────────────────────────────────

func TestHandleTransferWebhook_Completed(t *testing.T) {
	repo := newMockPayoutRepo()
	asaas := newFakeAsaas()
	defer asaas.close()
	svc := service.NewPayoutService(repo, asaas.client(), nil)

	if err := svc.HandleTransferWebhook(context.Background(), "TRANSFER_COMPLETED", "tx-1", ""); err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(repo.statusUpdates) != 1 || repo.statusUpdates[0].status != "completed" {
		t.Errorf("esperava status completed, got %+v", repo.statusUpdates)
	}
}

func TestHandleTransferWebhook_Failed(t *testing.T) {
	repo := newMockPayoutRepo()
	asaas := newFakeAsaas()
	defer asaas.close()
	svc := service.NewPayoutService(repo, asaas.client(), nil)

	if err := svc.HandleTransferWebhook(context.Background(), "TRANSFER_FAILED", "tx-2", "saldo insuficiente"); err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(repo.statusUpdates) != 1 || repo.statusUpdates[0].status != "failed" || repo.statusUpdates[0].errMsg != "saldo insuficiente" {
		t.Errorf("esperava status failed + mensagem, got %+v", repo.statusUpdates)
	}
}

func TestHandleTransferWebhook_EventoDesconhecido(t *testing.T) {
	repo := newMockPayoutRepo()
	asaas := newFakeAsaas()
	defer asaas.close()
	svc := service.NewPayoutService(repo, asaas.client(), nil)

	if err := svc.HandleTransferWebhook(context.Background(), "TRANSFER_PENDING", "tx-3", ""); err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(repo.statusUpdates) != 0 {
		t.Errorf("evento desconhecido não deveria atualizar status")
	}
}

// ─── GetBalance / ListPayouts ────────────────────────────────────────────────

func TestGetBalance_Delegado(t *testing.T) {
	repo := newMockPayoutRepo()
	repo.balance["tenant-1"] = &domain.Balance{AvailableCents: 12345, NextPayoutDate: "2026-06-15"}
	asaas := newFakeAsaas()
	defer asaas.close()
	svc := service.NewPayoutService(repo, asaas.client(), nil)

	b, err := svc.GetBalance(context.Background(), "tenant-1")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if b.AvailableCents != 12345 {
		t.Errorf("AvailableCents = %d", b.AvailableCents)
	}
}

func TestListPayouts_CalculaPaginacao(t *testing.T) {
	repo := newMockPayoutRepo()
	repo.listPayouts["tenant-1"] = []domain.CreatorPayout{
		{ID: "p1"}, {ID: "p2"},
	}
	asaas := newFakeAsaas()
	defer asaas.close()
	svc := service.NewPayoutService(repo, asaas.client(), nil)

	resp, err := svc.ListPayouts(context.Background(), "tenant-1", 20, 20)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if resp.Page != 2 {
		t.Errorf("Page = %d, queria 2", resp.Page)
	}
	if resp.Limit != 20 {
		t.Errorf("Limit = %d, queria 20", resp.Limit)
	}
	if resp.Total != 2 {
		t.Errorf("Total = %d, queria 2", resp.Total)
	}
}

func TestListPayouts_LimitZeroNaoQuebra(t *testing.T) {
	repo := newMockPayoutRepo()
	asaas := newFakeAsaas()
	defer asaas.close()
	svc := service.NewPayoutService(repo, asaas.client(), nil)

	if _, err := svc.ListPayouts(context.Background(), "tenant-1", 0, 0); err != nil {
		t.Fatalf("erro inesperado para limit=0: %v", err)
	}
}

// ─── DetectPixKeyType ─────────────────────────────────────────────────────────

func TestDetectPixKeyType(t *testing.T) {
	cases := []struct {
		key  string
		want string
	}{
		{"", "EVP"},
		{"12345678901", "CPF"},
		{"12345678000199", "CNPJ"},
		{"+5571999999999", "PHONE"},
		{"user@example.com", "EMAIL"},
		{"a1b2c3d4-e5f6", "EVP"},
	}
	for _, c := range cases {
		if got := service.DetectPixKeyType(c.key); got != c.want {
			t.Errorf("DetectPixKeyType(%q) = %q, want %q", c.key, got, c.want)
		}
	}
}
