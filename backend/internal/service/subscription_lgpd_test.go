package service_test

// Testes LGPD Art. 18 (eliminação/portabilidade/retenção) do SubscriptionService.

import (
	"context"
	"errors"
	"testing"
	"time"

	"linkpay-service/internal/domain"
	"linkpay-service/internal/repository"
	"linkpay-service/internal/service"
)

// fakeSubscriptionRepo implementa apenas os métodos usados nos fluxos LGPD;
// os demais métodos vêm do embed nil de SubscriptionRepoInterface.
type fakeSubscriptionRepo struct {
	repository.SubscriptionRepoInterface
	subsByToken  map[string]*domain.DigitalSubscription
	erasedEmails []string
	eraseErr     error
	listResult   []domain.BuyerSubscriptionExport
	listErr      error
	olderCutoffs []time.Time
	olderCount   int64
	olderErr     error
}

func (f *fakeSubscriptionRepo) GetSubscriptionByToken(_ context.Context, token string) (*domain.DigitalSubscription, error) {
	if s, ok := f.subsByToken[token]; ok {
		return s, nil
	}
	return nil, errors.New("not found")
}

func (f *fakeSubscriptionRepo) EraseSubscriptionsByEmail(_ context.Context, email string) (int64, error) {
	f.erasedEmails = append(f.erasedEmails, email)
	if f.eraseErr != nil {
		return 0, f.eraseErr
	}
	var n int64
	for _, s := range f.subsByToken {
		if s.CustomerEmail == email {
			n++
		}
	}
	return n, nil
}

func (f *fakeSubscriptionRepo) ListSubscriptionsByEmail(_ context.Context, _ string) ([]domain.BuyerSubscriptionExport, error) {
	return f.listResult, f.listErr
}

func (f *fakeSubscriptionRepo) EraseSubscriptionsOlderThan(_ context.Context, cutoff time.Time) (int64, error) {
	f.olderCutoffs = append(f.olderCutoffs, cutoff)
	return f.olderCount, f.olderErr
}

// fakeLinkRepoLGPD implementa apenas os métodos de compra usados nos fluxos LGPD.
type fakeLinkRepoLGPD struct {
	service.LinkPayRepoInterface
	purchasesByToken map[string]*domain.DigitalPurchase
	erasedEmails     []string
	eraseErr         error
	listResult       []domain.BuyerPurchaseExport
	listErr          error
	olderCount       int64
	olderErr         error
}

func (f *fakeLinkRepoLGPD) GetPurchaseByToken(_ context.Context, token string) (*domain.DigitalPurchase, error) {
	if p, ok := f.purchasesByToken[token]; ok {
		return p, nil
	}
	return nil, errors.New("not found")
}

func (f *fakeLinkRepoLGPD) ErasePurchasesByEmail(_ context.Context, email string) (int64, error) {
	f.erasedEmails = append(f.erasedEmails, email)
	if f.eraseErr != nil {
		return 0, f.eraseErr
	}
	var n int64
	for _, p := range f.purchasesByToken {
		if p.CustomerEmail == email {
			n++
		}
	}
	return n, nil
}

func (f *fakeLinkRepoLGPD) ListPurchasesByEmail(_ context.Context, _ string) ([]domain.BuyerPurchaseExport, error) {
	return f.listResult, f.listErr
}

func (f *fakeLinkRepoLGPD) ErasePurchasesOlderThan(_ context.Context, _ time.Time) (int64, error) {
	return f.olderCount, f.olderErr
}

func newLGPDService(subRepo *fakeSubscriptionRepo, linkRepo *fakeLinkRepoLGPD) *service.SubscriptionService {
	if subRepo.subsByToken == nil {
		subRepo.subsByToken = map[string]*domain.DigitalSubscription{}
	}
	if linkRepo.purchasesByToken == nil {
		linkRepo.purchasesByToken = map[string]*domain.DigitalPurchase{}
	}
	return service.NewSubscriptionService(subRepo, linkRepo, nil)
}

func purchaseWithToken(token, email string) *domain.DigitalPurchase {
	return &domain.DigitalPurchase{ID: "p-" + token, ProductID: "prod-1",
		CustomerEmail: email, CustomerName: "Comprador", CustomerCPF: "12345678901",
		PaymentStatus: "confirmed", CreatedAt: time.Now()}
}

func subscriptionWithToken(token, email string) *domain.DigitalSubscription {
	return &domain.DigitalSubscription{ID: "s-" + token, PlanID: "plan-1",
		CustomerEmail: email, CustomerName: "Assinante", Status: "active", CreatedAt: time.Now()}
}

// ─── EraseBuyerData ─────────────────────────────────────────────────────────

func TestEraseBuyerData_ValidPurchaseToken(t *testing.T) {
	subRepo := &fakeSubscriptionRepo{}
	linkRepo := &fakeLinkRepoLGPD{
		purchasesByToken: map[string]*domain.DigitalPurchase{
			"tok-1": purchaseWithToken("tok-1", "buyer@example.com"),
		},
	}
	svc := newLGPDService(subRepo, linkRepo)

	res, err := svc.EraseBuyerData(context.Background(), "  Buyer@Example.COM ", "tok-1")
	if err != nil {
		t.Fatalf("EraseBuyerData: %v", err)
	}
	if res.PurchasesErased != 1 || res.SubscriptionsErased != 0 {
		t.Errorf("resultado = %+v, want purchases=1 subscriptions=0", res)
	}
	// email normalizado propagado aos dois repositórios
	if len(linkRepo.erasedEmails) != 1 || linkRepo.erasedEmails[0] != "buyer@example.com" {
		t.Errorf("purchases erasure emails = %v", linkRepo.erasedEmails)
	}
	if len(subRepo.erasedEmails) != 1 || subRepo.erasedEmails[0] != "buyer@example.com" {
		t.Errorf("subscriptions erasure emails = %v", subRepo.erasedEmails)
	}
}

func TestEraseBuyerData_ValidSubscriptionToken(t *testing.T) {
	subRepo := &fakeSubscriptionRepo{
		subsByToken: map[string]*domain.DigitalSubscription{
			"tok-sub": subscriptionWithToken("tok-sub", "assinante@example.com"),
		},
	}
	linkRepo := &fakeLinkRepoLGPD{}
	svc := newLGPDService(subRepo, linkRepo)

	res, err := svc.EraseBuyerData(context.Background(), "assinante@example.com", "tok-sub")
	if err != nil {
		t.Fatalf("EraseBuyerData: %v", err)
	}
	if res.SubscriptionsErased != 1 {
		t.Errorf("subscriptions = %d, want 1", res.SubscriptionsErased)
	}
}

func TestEraseBuyerData_TokenDeOutroEmail_Rejeitado(t *testing.T) {
	linkRepo := &fakeLinkRepoLGPD{
		purchasesByToken: map[string]*domain.DigitalPurchase{
			"tok-dono": purchaseWithToken("tok-dono", "dono@example.com"),
		},
	}
	svc := newLGPDService(&fakeSubscriptionRepo{}, linkRepo)

	if _, err := svc.EraseBuyerData(context.Background(), "vitima@example.com", "tok-dono"); err == nil {
		t.Fatal("esperava erro: token não pertence ao email informado")
	}
	if len(linkRepo.erasedEmails) != 0 {
		t.Error("nenhuma anonimização pode ocorrer quando a titularidade falha")
	}
}

func TestEraseBuyerData_TokenInvalido_Rejeitado(t *testing.T) {
	svc := newLGPDService(&fakeSubscriptionRepo{}, &fakeLinkRepoLGPD{})
	if _, err := svc.EraseBuyerData(context.Background(), "nobody@example.com", "token-inexistente"); err == nil {
		t.Fatal("esperava erro de token inválido")
	}
}

// ─── ExportBuyerData ────────────────────────────────────────────────────────

func TestExportBuyerData_Ok(t *testing.T) {
	now := time.Now().UTC()
	subRepo := &fakeSubscriptionRepo{
		listResult: []domain.BuyerSubscriptionExport{
			{ID: "s1", ProductName: "Curso", PlanName: "Mensal", Status: "active", CreatedAt: now},
		},
	}
	linkRepo := &fakeLinkRepoLGPD{
		purchasesByToken: map[string]*domain.DigitalPurchase{
			"tok-1": purchaseWithToken("tok-1", "buyer@example.com"),
		},
		listResult: []domain.BuyerPurchaseExport{
			{ID: "p1", ProductName: "Ebook", AmountCents: 2990, PaymentStatus: "confirmed", CreatedAt: now},
		},
	}
	svc := newLGPDService(subRepo, linkRepo)

	export, err := svc.ExportBuyerData(context.Background(), "buyer@example.com", "tok-1")
	if err != nil {
		t.Fatalf("ExportBuyerData: %v", err)
	}
	if export.Email != "buyer@example.com" {
		t.Errorf("email = %q", export.Email)
	}
	if len(export.Purchases) != 1 || len(export.Subscriptions) != 1 {
		t.Errorf("export incompleto: purchases=%d subscriptions=%d", len(export.Purchases), len(export.Subscriptions))
	}
}

func TestExportBuyerData_ListasVaziasNaoNulas(t *testing.T) {
	subRepo := &fakeSubscriptionRepo{} // listResult nil
	linkRepo := &fakeLinkRepoLGPD{}    // listResult nil
	svc := newLGPDService(subRepo, linkRepo)

	// titular existe via subscription token
	subRepo.subsByToken["tok-x"] = subscriptionWithToken("tok-x", "vazio@example.com")
	export, err := svc.ExportBuyerData(context.Background(), "vazio@example.com", "tok-x")
	if err != nil {
		t.Fatalf("ExportBuyerData: %v", err)
	}
	if export.Purchases == nil || export.Subscriptions == nil {
		t.Fatal("listas devem ser [] e não nil para serialização JSON estável")
	}
}

// ─── RetentionSweep ─────────────────────────────────────────────────────────

func TestRetentionSweep_AplicaTTLDeRetencao(t *testing.T) {
	subRepo := &fakeSubscriptionRepo{olderCount: 7}
	linkRepo := &fakeLinkRepoLGPD{olderCount: 3}
	svc := newLGPDService(subRepo, linkRepo)

	purchases, subs, err := svc.RetentionSweep(context.Background())
	if err != nil {
		t.Fatalf("RetentionSweep: %v", err)
	}
	if purchases != 3 || subs != 7 {
		t.Errorf("retenção = purchases:%d subscriptions:%d, want 3/7", purchases, subs)
	}
	if len(subRepo.olderCutoffs) != 1 {
		t.Fatalf("cutoff não registrado: %d chamadas", len(subRepo.olderCutoffs))
	}
	// cutoff deve ser ~5 anos atrás (BuyerDataRetentionYears)
	expected := time.Now().AddDate(-domain.BuyerDataRetentionYears, 0, 0)
	cut := subRepo.olderCutoffs[0]
	if cut.After(expected.Add(5*time.Minute)) || cut.Before(expected.Add(-5*time.Minute)) {
		t.Errorf("cutoff = %v, esperava ~%v (5 anos)", cut, expected)
	}
}
