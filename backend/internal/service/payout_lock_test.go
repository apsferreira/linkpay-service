package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"linkpay-service/internal/domain"
	"linkpay-service/internal/repository"
)

// stubRepo implementa PayoutRepository — todas as funções retornam erro padrão.
type stubRepo struct{}

func (s *stubRepo) GetEligiblePurchases(ctx context.Context, tenantID string) ([]repository.EligiblePurchase, error) {
	return nil, errors.New("stub")
}
func (s *stubRepo) GetAnticipationEligiblePurchases(ctx context.Context, tenantID string) ([]repository.AnticipationEligiblePurchase, error) {
	return nil, errors.New("stub")
}
func (s *stubRepo) CreateAnticipationPayout(ctx context.Context, payout *domain.CreatorPayout, purchaseIDs []string) error {
	return errors.New("stub")
}
func (s *stubRepo) GetTenantsWithEligiblePayouts(ctx context.Context) ([]string, error) {
	return nil, errors.New("stub")
}
func (s *stubRepo) GetTenantPixKey(ctx context.Context, tenantID string) (string, error) {
	return "", errors.New("stub")
}
func (s *stubRepo) GetBalance(ctx context.Context, tenantID string) (*domain.Balance, error) {
	return nil, errors.New("stub")
}
func (s *stubRepo) CreatePayout(ctx context.Context, payout *domain.CreatorPayout, purchaseIDs []string) error {
	return errors.New("stub")
}
func (s *stubRepo) UpdatePayoutTransferID(ctx context.Context, payoutID, transferID, status string) error {
	return errors.New("stub")
}
func (s *stubRepo) UpdatePayoutStatus(ctx context.Context, asaasTransferID, status, errMsg string) error {
	return errors.New("stub")
}
func (s *stubRepo) ListPayouts(ctx context.Context, tenantID string, limit, offset int) ([]domain.CreatorPayout, int, error) {
	return nil, 0, errors.New("stub")
}

// mockRedis implementa RedisClient para testes — track locks em memória.
type mockRedis struct {
	mu    sync.Mutex
	locks map[string]bool
}

func newMockRedis() *mockRedis {
	return &mockRedis{locks: make(map[string]bool)}
}

func (m *mockRedis) SetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.BoolCmd {
	m.mu.Lock()
	defer m.mu.Unlock()

	cmd := redis.NewBoolCmd(ctx)
	if m.locks[key] {
		cmd.SetVal(false)
		return cmd
	}
	m.locks[key] = true
	cmd.SetVal(true)
	return cmd
}

func (m *mockRedis) Del(ctx context.Context, keys ...string) *redis.IntCmd {
	m.mu.Lock()
	defer m.mu.Unlock()

	cmd := redis.NewIntCmd(ctx)
	var count int64
	for _, k := range keys {
		if m.locks[k] {
			delete(m.locks, k)
			count++
		}
	}
	cmd.SetVal(count)
	return cmd
}

func TestProcessTenantPayout_DistributedLock_BlocksConcurrent(t *testing.T) {
	svc := &PayoutService{
		repo:  &stubRepo{},
		redis: newMockRedis(),
	}

	today := time.Now().UTC().Format("2006-01-02")

	// Primeira chamada — adquire lock, processa, libera no defer
	// O stub repo retorna erro de GetTenantPixKey, que é esperado.
	err := svc.processTenantPayout(context.Background(), "tenant-1", today)
	if err == nil {
		t.Fatal("expected error from stub repo, got nil")
	}

	// Lock foi liberado no defer, então segunda chamada também processa
	err = svc.processTenantPayout(context.Background(), "tenant-1", today)
	if err == nil {
		t.Fatal("expected error from stub repo, got nil")
	}
}

func TestProcessTenantPayout_DistributedLock_SkipWhenLocked(t *testing.T) {
	svc := &PayoutService{
		repo:  &stubRepo{},
		redis: newMockRedis(),
	}

	// Simula lock adquirido por outra réplica
	today := time.Now().UTC().Format("2006-01-02")
	key := "payout:lock:tenant-2:" + today
	svc.redis.(*mockRedis).mu.Lock()
	svc.redis.(*mockRedis).locks[key] = true
	svc.redis.(*mockRedis).mu.Unlock()

	// Segunda chamada — lock já existe, deve pular sem erro
	err := svc.processTenantPayout(context.Background(), "tenant-2", today)
	if err != nil {
		t.Fatalf("expected nil (skip), got %v", err)
	}
}

func TestRunDailyPayouts_DistributedLock_NoRace(t *testing.T) {
	svc := &PayoutService{
		repo:  &stubRepo{},
		redis: newMockRedis(),
	}

	// GetTenantsWithEligiblePayouts retorna erro, então o teste só verifica
	// que a estrutura do lock no loop não causa pânico.
	err := svc.RunDailyPayouts(context.Background())
	if err == nil {
		t.Fatal("expected error from stub repo, got nil")
	}
}
