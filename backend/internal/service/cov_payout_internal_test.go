package service

// Coverage tests that need access to unexported methods (processTenantPayout,
// executePayout). All symbols are prefixed cov/Cov.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"linkpay-service/internal/domain"
	"linkpay-service/internal/pkg/pulse"
	"linkpay-service/internal/repository"
)

type covPayoutRepo struct {
	mu sync.Mutex

	tenants              []string
	tenantsErr           error
	pixKey               string
	pixKeyErr            error
	eligible             []repository.EligiblePurchase
	eligibleErr          error
	anticipation         []repository.AnticipationEligiblePurchase
	anticipationErr      error
	anticipationCalls    int
	anticipationErrAfter int
	balance              *domain.Balance
	balanceErr           error
	payouts              []domain.CreatorPayout
	listErr              error

	createPayoutErr   error
	createAnticErr    error
	updateTransferErr error
	updateStatusErr   error

	createPayoutCalls   int
	createAnticCalls    int
	updateTransferCalls int
	lastAmount          int
	lastPurchaseIDs     []string
	transferUpdates     [][3]string
}

func (m *covPayoutRepo) GetEligiblePurchases(_ context.Context, _ string) ([]repository.EligiblePurchase, error) {
	return m.eligible, m.eligibleErr
}

func (m *covPayoutRepo) GetAnticipationEligiblePurchases(_ context.Context, _ string) ([]repository.AnticipationEligiblePurchase, error) {
	m.anticipationCalls++
	if m.anticipationErrAfter > 0 && m.anticipationCalls > m.anticipationErrAfter {
		return nil, errors.New("db on second call")
	}
	return m.anticipation, m.anticipationErr
}

func (m *covPayoutRepo) CreateAnticipationPayout(_ context.Context, p *domain.CreatorPayout, ids []string) error {
	m.createAnticCalls++
	m.lastPurchaseIDs = append([]string(nil), ids...)
	return m.createAnticErr
}

func (m *covPayoutRepo) GetTenantsWithEligiblePayouts(_ context.Context) ([]string, error) {
	return m.tenants, m.tenantsErr
}

func (m *covPayoutRepo) GetTenantPixKey(_ context.Context, _ string) (string, error) {
	return m.pixKey, m.pixKeyErr
}

func (m *covPayoutRepo) GetBalance(_ context.Context, _ string) (*domain.Balance, error) {
	if m.balanceErr != nil {
		return nil, m.balanceErr
	}
	if m.balance != nil {
		return m.balance, nil
	}
	return &domain.Balance{}, nil
}

func (m *covPayoutRepo) CreatePayout(_ context.Context, p *domain.CreatorPayout, ids []string) error {
	m.createPayoutCalls++
	m.lastAmount = p.AmountCents
	m.lastPurchaseIDs = append([]string(nil), ids...)
	return m.createPayoutErr
}

func (m *covPayoutRepo) UpdatePayoutTransferID(_ context.Context, payoutID, transferID, status string) error {
	m.updateTransferCalls++
	m.transferUpdates = append(m.transferUpdates, [3]string{payoutID, transferID, status})
	return m.updateTransferErr
}

func (m *covPayoutRepo) UpdatePayoutStatus(_ context.Context, asaasID, status, errMsg string) error {
	return m.updateStatusErr
}

func (m *covPayoutRepo) ListPayouts(_ context.Context, _ string, _, _ int) ([]domain.CreatorPayout, int, error) {
	if m.listErr != nil {
		return nil, 0, m.listErr
	}
	return m.payouts, len(m.payouts), nil
}

type covRedis struct {
	setNXErr error
	locked   bool
	delErr   error
}

func (r *covRedis) SetNX(ctx context.Context, _ string, _ interface{}, _ time.Duration) *redis.BoolCmd {
	cmd := redis.NewBoolCmd(ctx)
	if r.setNXErr != nil {
		cmd.SetErr(r.setNXErr)
		return cmd
	}
	cmd.SetVal(!r.locked)
	return cmd
}

func (r *covRedis) Del(ctx context.Context, _ ...string) *redis.IntCmd {
	cmd := redis.NewIntCmd(ctx)
	if r.delErr != nil {
		cmd.SetErr(r.delErr)
		return cmd
	}
	cmd.SetVal(1)
	return cmd
}

// covPayoutAsaas devolve um client apontando para um fake /transfers.
func covPayoutAsaas(t *testing.T, status int) (*AsaasClient, *int, func()) {
	t.Helper()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/transfers" {
			http.NotFound(w, r)
			return
		}
		if status >= 400 {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"errors":"nope"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(AsaasTransferResponse{ID: "tr-cov", Status: "PENDING"})
	}))
	return NewAsaasClient(srv.URL, "k", nil, nil), &calls, srv.Close
}

func covToday() string { return time.Now().UTC().Format("2006-01-02") }

// ─── processTenantPayout ────────────────────────────────────────────────────

func TestCovPayout_ProcessTenant_LockAcquireError(t *testing.T) {
	svc := NewPayoutService(&covPayoutRepo{}, nil, &covRedis{setNXErr: errors.New("redis down")})
	err := svc.processTenantPayout(context.Background(), "t1", covToday())
	if err == nil || !strings.Contains(err.Error(), "lock distribuído") {
		t.Fatalf("error = %v", err)
	}
}

func TestCovPayout_ProcessTenant_LockAlreadyHeldSkips(t *testing.T) {
	repo := &covPayoutRepo{pixKey: "user@example.com"}
	svc := NewPayoutService(repo, nil, &covRedis{locked: true})
	if err := svc.processTenantPayout(context.Background(), "t1", covToday()); err != nil {
		t.Fatalf("expected skip, got %v", err)
	}
}

func TestCovPayout_ProcessTenant_LockReleaseErrorIsLogged(t *testing.T) {
	repo := &covPayoutRepo{pixKeyErr: errors.New("db")}
	svc := NewPayoutService(repo, nil, &covRedis{delErr: errors.New("redis del")})
	err := svc.processTenantPayout(context.Background(), "t1", covToday())
	if err == nil || !strings.Contains(err.Error(), "pix_key") {
		t.Fatalf("error = %v", err)
	}
}

func TestCovPayout_ProcessTenant_PixKeyError(t *testing.T) {
	repo := &covPayoutRepo{pixKeyErr: errors.New("db")}
	svc := NewPayoutService(repo, nil, nil)
	if err := svc.processTenantPayout(context.Background(), "t1", covToday()); err == nil {
		t.Fatal("expected pix key error")
	}
}

func TestCovPayout_ProcessTenant_EligibleError(t *testing.T) {
	repo := &covPayoutRepo{pixKey: "user@example.com", eligibleErr: errors.New("db")}
	svc := NewPayoutService(repo, nil, nil)
	if err := svc.processTenantPayout(context.Background(), "t1", covToday()); err == nil {
		t.Fatal("expected eligible error")
	}
}

func TestCovPayout_ProcessTenant_NoPurchases(t *testing.T) {
	repo := &covPayoutRepo{pixKey: "user@example.com"}
	svc := NewPayoutService(repo, nil, nil)
	if err := svc.processTenantPayout(context.Background(), "t1", covToday()); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestCovPayout_ProcessTenant_BelowMinimum(t *testing.T) {
	repo := &covPayoutRepo{
		pixKey:   "user@example.com",
		eligible: []repository.EligiblePurchase{{ID: "p1", CreatorEarnings: 1000}},
	}
	svc := NewPayoutService(repo, nil, nil)
	if err := svc.processTenantPayout(context.Background(), "t1", covToday()); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if repo.createPayoutCalls != 0 {
		t.Error("payout should not be created below minimum")
	}
}

// ─── executePayout ──────────────────────────────────────────────────────────

func TestCovPayout_Execute_CreatePayoutError(t *testing.T) {
	repo := &covPayoutRepo{
		pixKey:          "user@example.com",
		eligible:        []repository.EligiblePurchase{{ID: "p1", CreatorEarnings: 50000}},
		createPayoutErr: errors.New("db"),
	}
	client, _, closeSrv := covPayoutAsaas(t, http.StatusOK)
	defer closeSrv()
	svc := NewPayoutService(repo, client, nil)

	err := svc.processTenantPayout(context.Background(), "t1", covToday())
	if err == nil || !strings.Contains(err.Error(), "criar payout") {
		t.Fatalf("error = %v", err)
	}
}

func TestCovPayout_Execute_TransferIDUpdateErrorIsNonFatal(t *testing.T) {
	repo := &covPayoutRepo{
		pixKey:            "user@example.com",
		eligible:          []repository.EligiblePurchase{{ID: "p1", CreatorEarnings: 50000}},
		updateTransferErr: errors.New("db"),
	}
	client, calls, closeSrv := covPayoutAsaas(t, http.StatusOK)
	defer closeSrv()
	svc := NewPayoutService(repo, client, nil)

	if err := svc.processTenantPayout(context.Background(), "t1", covToday()); err != nil {
		t.Fatalf("expected nil (non-fatal), got %v", err)
	}
	if *calls != 1 {
		t.Errorf("asaas calls = %d", *calls)
	}
	if repo.lastAmount != 45000 { // 50000 - 10%
		t.Errorf("amount = %d, want 45000", repo.lastAmount)
	}
}

func TestCovPayout_Execute_AsaasFailureAndTransferUpdateFailure(t *testing.T) {
	repo := &covPayoutRepo{
		pixKey:            "user@example.com",
		eligible:          []repository.EligiblePurchase{{ID: "p1", CreatorEarnings: 50000}},
		updateTransferErr: errors.New("db"),
	}
	client, _, closeSrv := covPayoutAsaas(t, http.StatusBadRequest)
	defer closeSrv()
	svc := NewPayoutService(repo, client, nil)

	err := svc.processTenantPayout(context.Background(), "t1", covToday())
	if err == nil || !strings.Contains(err.Error(), "Asaas Transfer") {
		t.Fatalf("error = %v", err)
	}
	foundFailed := false
	for _, u := range repo.transferUpdates {
		if u[2] == "failed" {
			foundFailed = true
		}
	}
	if !foundFailed {
		t.Errorf("payout should be marked failed: %+v", repo.transferUpdates)
	}
}

// ─── RequestManualPayout ───────────────────────────────────────────────────

func TestCovPayout_RequestManual_BalanceError(t *testing.T) {
	repo := &covPayoutRepo{balanceErr: errors.New("db")}
	svc := NewPayoutService(repo, nil, nil)
	if _, err := svc.RequestManualPayout(context.Background(), "t1", "k", 5000); err == nil {
		t.Fatal("expected balance error")
	}
}

func TestCovPayout_RequestManual_EligibleError(t *testing.T) {
	repo := &covPayoutRepo{
		balance:     &domain.Balance{AvailableCents: 100000},
		eligibleErr: errors.New("db"),
	}
	svc := NewPayoutService(repo, nil, nil)
	if _, err := svc.RequestManualPayout(context.Background(), "t1", "k", 5000); err == nil {
		t.Fatal("expected eligible error")
	}
}

func TestCovPayout_RequestManual_CannotSelectPurchases(t *testing.T) {
	repo := &covPayoutRepo{
		balance:  &domain.Balance{AvailableCents: 100000},
		eligible: []repository.EligiblePurchase{{ID: "big", CreatorEarnings: 100000}},
	}
	svc := NewPayoutService(repo, nil, nil)
	_, err := svc.RequestManualPayout(context.Background(), "t1", "k", 5000)
	if err == nil || !strings.Contains(err.Error(), "selecionar compras") {
		t.Fatalf("error = %v", err)
	}
}

func TestCovPayout_RequestManual_ListPayoutsError(t *testing.T) {
	repo := &covPayoutRepo{
		balance:  &domain.Balance{AvailableCents: 100000},
		eligible: []repository.EligiblePurchase{{ID: "p1", CreatorEarnings: 20000}},
		listErr:  errors.New("db"),
	}
	client, _, closeSrv := covPayoutAsaas(t, http.StatusOK)
	defer closeSrv()
	svc := NewPayoutService(repo, client, nil)

	_, err := svc.RequestManualPayout(context.Background(), "t1", "k", 0)
	if err == nil || !strings.Contains(err.Error(), "recuperar registro") {
		t.Fatalf("error = %v", err)
	}
}

func TestCovPayout_RequestManual_CreatePayoutError(t *testing.T) {
	repo := &covPayoutRepo{
		balance:         &domain.Balance{AvailableCents: 100000},
		eligible:        []repository.EligiblePurchase{{ID: "p1", CreatorEarnings: 20000}},
		createPayoutErr: errors.New("db"),
	}
	client, _, closeSrv := covPayoutAsaas(t, http.StatusOK)
	defer closeSrv()
	svc := NewPayoutService(repo, client, nil)

	if _, err := svc.RequestManualPayout(context.Background(), "t1", "k", 10000); err == nil {
		t.Fatal("expected create payout error")
	}
}

// ─── RequestAnticipation ───────────────────────────────────────────────────

func TestCovPayout_RequestAnticipation_PixKeyLookupError(t *testing.T) {
	repo := &covPayoutRepo{pixKeyErr: errors.New("db")}
	svc := NewPayoutService(repo, nil, nil)
	if _, err := svc.RequestAnticipation(context.Background(), "t1", ""); err == nil ||
		!strings.Contains(err.Error(), "chave PIX") {
		t.Fatalf("error = %v", err)
	}
}

func TestCovPayout_RequestAnticipation_PreviewError(t *testing.T) {
	repo := &covPayoutRepo{anticipationErr: errors.New("db")}
	svc := NewPayoutService(repo, nil, nil)
	if _, err := svc.RequestAnticipation(context.Background(), "t1", "k"); err == nil {
		t.Fatal("expected preview error")
	}
}

func TestCovPayout_RequestAnticipation_NoEligiblePurchases(t *testing.T) {
	repo := &covPayoutRepo{}
	svc := NewPayoutService(repo, nil, nil)
	if _, err := svc.RequestAnticipation(context.Background(), "t1", "k"); err == nil ||
		!strings.Contains(err.Error(), "nenhuma compra") {
		t.Fatalf("error = %v", err)
	}
}

func TestCovPayout_RequestAnticipation_CreateError(t *testing.T) {
	repo := &covPayoutRepo{
		anticipation:   []repository.AnticipationEligiblePurchase{{ID: "c1", CreatorEarnings: 100000}},
		createAnticErr: errors.New("db"),
	}
	client, _, closeSrv := covPayoutAsaas(t, http.StatusOK)
	defer closeSrv()
	svc := NewPayoutService(repo, client, nil)

	if _, err := svc.RequestAnticipation(context.Background(), "t1", "k"); err == nil {
		t.Fatal("expected create anticipation error")
	}
}

func TestCovPayout_RequestAnticipation_AsaasError(t *testing.T) {
	repo := &covPayoutRepo{
		anticipation: []repository.AnticipationEligiblePurchase{{ID: "c1", CreatorEarnings: 100000}},
	}
	client, _, closeSrv := covPayoutAsaas(t, http.StatusBadRequest)
	defer closeSrv()
	svc := NewPayoutService(repo, client, nil)

	_, err := svc.RequestAnticipation(context.Background(), "t1", "k")
	if err == nil || !strings.Contains(err.Error(), "antecipação") {
		t.Fatalf("error = %v", err)
	}
	if repo.createAnticCalls != 1 {
		t.Error("anticipation should be created locally before gateway call")
	}
	foundFailed := false
	for _, u := range repo.transferUpdates {
		if u[2] == "failed" {
			foundFailed = true
		}
	}
	if !foundFailed {
		t.Error("anticipation should be marked failed")
	}
}

func TestCovPayout_RequestAnticipation_UpdateTransferErrorIsNonFatal(t *testing.T) {
	repo := &covPayoutRepo{
		anticipation:      []repository.AnticipationEligiblePurchase{{ID: "c1", CreatorEarnings: 100000}},
		updateTransferErr: errors.New("db"),
	}
	client, _, closeSrv := covPayoutAsaas(t, http.StatusOK)
	defer closeSrv()
	svc := NewPayoutService(repo, client, nil)

	payout, err := svc.RequestAnticipation(context.Background(), "t1", "k")
	if err != nil {
		t.Fatalf("expected nil (non-fatal), got %v", err)
	}
	if payout.AsaasTransferID != "tr-cov" {
		t.Errorf("transfer id = %q", payout.AsaasTransferID)
	}
}

// ─── misc ──────────────────────────────────────────────────────────────────

func TestCovPayout_ListPayoutsError(t *testing.T) {
	repo := &covPayoutRepo{listErr: errors.New("db")}
	svc := NewPayoutService(repo, nil, nil)
	if _, err := svc.ListPayouts(context.Background(), "t1", 10, 0); err == nil {
		t.Fatal("expected list error")
	}
}

func TestCovPayout_HandleTransferWebhookRepoError(t *testing.T) {
	repo := &covPayoutRepo{updateStatusErr: errors.New("db")}
	svc := NewPayoutService(repo, nil, nil)
	if err := svc.HandleTransferWebhook(context.Background(), "TRANSFER_COMPLETED", "tx", ""); err == nil {
		t.Fatal("expected repo error")
	}
	if err := svc.HandleTransferWebhook(context.Background(), "TRANSFER_FAILED", "tx", "desc"); err == nil {
		t.Fatal("expected repo error")
	}
}

func TestCovPayout_GetBalanceError(t *testing.T) {
	repo := &covPayoutRepo{balanceErr: errors.New("db")}
	svc := NewPayoutService(repo, nil, nil)
	if _, err := svc.GetBalance(context.Background(), "t1"); err == nil {
		t.Fatal("expected balance error")
	}
}

func TestCovPayout_RequestManual_ExecuteError(t *testing.T) {
	repo := &covPayoutRepo{
		balance:         &domain.Balance{AvailableCents: 100000},
		eligible:        []repository.EligiblePurchase{{ID: "p1", CreatorEarnings: 20000}},
		createPayoutErr: errors.New("db"),
	}
	client, _, closeSrv := covPayoutAsaas(t, http.StatusOK)
	defer closeSrv()
	svc := NewPayoutService(repo, client, nil)

	if _, err := svc.RequestManualPayout(context.Background(), "t1", "k", 0); err == nil {
		t.Fatal("expected executePayout error to propagate")
	}
}

func TestCovPayout_RequestAnticipation_SecondFetchError(t *testing.T) {
	repo := &covPayoutRepo{
		anticipation:         []repository.AnticipationEligiblePurchase{{ID: "c1", CreatorEarnings: 100000}},
		anticipationErrAfter: 1, // preview OK, second fetch fails
	}
	client, _, closeSrv := covPayoutAsaas(t, http.StatusOK)
	defer closeSrv()
	svc := NewPayoutService(repo, client, nil)

	_, err := svc.RequestAnticipation(context.Background(), "t1", "k")
	if err == nil || !strings.Contains(err.Error(), "buscar compras") {
		t.Fatalf("error = %v", err)
	}
}

func TestCovPayout_RequestAnticipation_AsaasErrorWithUpdateError(t *testing.T) {
	repo := &covPayoutRepo{
		anticipation:      []repository.AnticipationEligiblePurchase{{ID: "c1", CreatorEarnings: 100000}},
		updateTransferErr: errors.New("db"),
	}
	client, _, closeSrv := covPayoutAsaas(t, http.StatusBadRequest)
	defer closeSrv()
	svc := NewPayoutService(repo, client, nil)

	if _, err := svc.RequestAnticipation(context.Background(), "t1", "k"); err == nil {
		t.Fatal("expected gateway error")
	}
}

func TestCovPayout_TrackFailureTimeout(t *testing.T) {
	// servidor lento + timeout curto no httpClient → erro contém "deadline exceeded"
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer slow.Close()

	pulseSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer pulseSrv.Close()
	t.Setenv("PULSE_URL", pulseSrv.URL)

	c := NewAsaasClient(slow.URL, "k", nil, pulse.New())
	c.httpClient.Timeout = 25 * time.Millisecond

	if _, err := c.CreateTransfer(AsaasTransferRequest{Value: 1}); err == nil {
		t.Fatal("expected timeout error")
	}
}
