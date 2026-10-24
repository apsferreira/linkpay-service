package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"linkpay-service/internal/domain"
	"linkpay-service/internal/handler"
	"linkpay-service/internal/pkg/pulse"
)

// ─── mock: LinkPayServiceInterface (apenas os métodos usados em HandleWebhook) ─

type fakeLinkPayService struct {
	processCalls            int32
	processChargeIDs        []string
	processReturnIdempotent bool // se true, nenhum efeito (compra já confirmada)
	processErr              error
}

func (f *fakeLinkPayService) ProcessPaymentConfirmed(_ context.Context, chargeID string) error {
	atomic.AddInt32(&f.processCalls, 1)
	f.processChargeIDs = append(f.processChargeIDs, chargeID)
	return f.processErr
}

// stubs vazios — não usados nos testes de webhook
func (f *fakeLinkPayService) CreateProduct(context.Context, string, *domain.CreateDigitalProductRequest) (*domain.DigitalProduct, error) {
	return nil, nil
}
func (f *fakeLinkPayService) GetProductBySlug(context.Context, string) (*domain.DigitalProduct, error) {
	return nil, nil
}
func (f *fakeLinkPayService) GetProductByID(context.Context, string, string) (*domain.DigitalProduct, error) {
	return nil, nil
}
func (f *fakeLinkPayService) ListProducts(context.Context, string) ([]domain.DigitalProduct, error) {
	return nil, nil
}
func (f *fakeLinkPayService) UpdateProduct(context.Context, string, string, *domain.UpdateDigitalProductRequest) (*domain.DigitalProduct, error) {
	return nil, nil
}
func (f *fakeLinkPayService) UpdateProductStorage(context.Context, string, string, string, string, string, int64, string) error {
	return nil
}
func (f *fakeLinkPayService) DeactivateProduct(context.Context, string, string) error { return nil }
func (f *fakeLinkPayService) InitiatePurchase(context.Context, string, *domain.InitiatePurchaseRequest) (*domain.DigitalPurchase, error) {
	return nil, nil
}
func (f *fakeLinkPayService) Download(context.Context, string) (string, error) { return "", nil }
func (f *fakeLinkPayService) GetSalesStats(context.Context, string) (*domain.LinkPayStatsResponse, error) {
	return nil, nil
}
func (f *fakeLinkPayService) AddProductFile(context.Context, *domain.DigitalProductFile) error {
	return nil
}
func (f *fakeLinkPayService) ListProductFiles(context.Context, string) ([]domain.DigitalProductFile, error) {
	return nil, nil
}
func (f *fakeLinkPayService) GetProductFile(context.Context, string) (*domain.DigitalProductFile, error) {
	return nil, nil
}
func (f *fakeLinkPayService) DeleteProductFile(context.Context, string, string) error { return nil }
func (f *fakeLinkPayService) GetPurchaseByToken(context.Context, string) (*domain.DigitalPurchase, error) {
	return nil, nil
}
func (f *fakeLinkPayService) GetUpsellsByProductID(context.Context, string) ([]domain.UpsellSuggestion, error) {
	return nil, nil
}
func (f *fakeLinkPayService) RefundPurchase(context.Context, string, string) error { return nil }
func (f *fakeLinkPayService) UpdateProductCover(context.Context, string, string, string) error {
	return nil
}
func (f *fakeLinkPayService) GetCommissionSummary(context.Context, string, string) (*domain.CommissionSummaryResponse, error) {
	return &domain.CommissionSummaryResponse{}, nil
}

// ─── mock: PayoutServiceInterface ───────────────────────────────────────────

type fakePayoutService struct {
	transferCalls  int32
	transferEvents []string
	transferIDs    []string
	transferReturn error
}

func (f *fakePayoutService) GetBalance(context.Context, string) (*domain.Balance, error) {
	return nil, nil
}
func (f *fakePayoutService) ListPayouts(context.Context, string, int, int) (*domain.ListPayoutsResponse, error) {
	return nil, nil
}
func (f *fakePayoutService) RequestManualPayout(context.Context, string, string, int) (*domain.CreatorPayout, error) {
	return nil, nil
}
func (f *fakePayoutService) HandleTransferWebhook(_ context.Context, event, transferID, _ string) error {
	atomic.AddInt32(&f.transferCalls, 1)
	f.transferEvents = append(f.transferEvents, event)
	f.transferIDs = append(f.transferIDs, transferID)
	return f.transferReturn
}
func (f *fakePayoutService) PreviewAnticipation(context.Context, string) (*domain.AnticipationPreview, error) {
	return nil, nil
}
func (f *fakePayoutService) RequestAnticipation(context.Context, string, string) (*domain.CreatorPayout, error) {
	return nil, nil
}

// ─── setup ───────────────────────────────────────────────────────────────────

const testWebhookSecret = "test-secret-do-asaas"

func newWebhookApp(svc *fakeLinkPayService, payoutSvc *fakePayoutService) *fiber.App {
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	h := handler.NewLinkPayHandler(svc, testWebhookSecret, nil, pulse.New())
	if payoutSvc != nil {
		h = h.WithPayoutService(payoutSvc)
	}
	app.Post("/webhook", h.HandleWebhook)
	return app
}

func postJSON(t *testing.T, app *fiber.App, body any, token string) *http.Response {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("asaas-access-token", token)
	}
	// #10: webhook exige timestamp recente — helper envia sempre um timestamp atual.
	req.Header.Set("asaas-timestamp", strconv.FormatInt(time.Now().Unix(), 10))
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("erro ao executar request: %v", err)
	}
	return resp
}

// ─── testes ──────────────────────────────────────────────────────────────────

func TestWebhook_TokenAusente_Rejeita(t *testing.T) {
	svc := &fakeLinkPayService{}
	app := newWebhookApp(svc, nil)

	resp := postJSON(t, app, map[string]any{"event": "PAYMENT_CONFIRMED"}, "")
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("status = %d, queria 401", resp.StatusCode)
	}
	if atomic.LoadInt32(&svc.processCalls) != 0 {
		t.Errorf("ProcessPaymentConfirmed chamado com token ausente")
	}
}

func TestWebhook_TokenInvalido_Rejeita(t *testing.T) {
	svc := &fakeLinkPayService{}
	app := newWebhookApp(svc, nil)

	resp := postJSON(t, app, map[string]any{"event": "PAYMENT_CONFIRMED"}, "token-errado")
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("status = %d, queria 401", resp.StatusCode)
	}
	if atomic.LoadInt32(&svc.processCalls) != 0 {
		t.Errorf("ProcessPaymentConfirmed chamado com token errado")
	}
}

func TestWebhook_PaymentConfirmed_ChamaService(t *testing.T) {
	svc := &fakeLinkPayService{}
	app := newWebhookApp(svc, nil)

	body := map[string]any{
		"event": "PAYMENT_CONFIRMED",
		"payment": map[string]any{
			"id": "pay_123",
		},
	}
	resp := postJSON(t, app, body, testWebhookSecret)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, queria 200", resp.StatusCode)
	}
	if atomic.LoadInt32(&svc.processCalls) != 1 {
		t.Errorf("ProcessPaymentConfirmed chamado %d vezes", svc.processCalls)
	}
	if len(svc.processChargeIDs) != 1 || svc.processChargeIDs[0] != "pay_123" {
		t.Errorf("chargeID = %v, queria [pay_123]", svc.processChargeIDs)
	}
}

func TestWebhook_PaymentConfirmed_SemPaymentID_RetornaBadRequest(t *testing.T) {
	svc := &fakeLinkPayService{}
	app := newWebhookApp(svc, nil)

	body := map[string]any{"event": "PAYMENT_CONFIRMED"}
	resp := postJSON(t, app, body, testWebhookSecret)
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, queria 400", resp.StatusCode)
	}
	if atomic.LoadInt32(&svc.processCalls) != 0 {
		t.Errorf("ProcessPaymentConfirmed chamado sem payment.id")
	}
}

// Idempotência: chamar o mesmo charge duas vezes deve invocar o service duas vezes,
// e o service (verificado no service_test) trata o duplicado como no-op.
// Aqui validamos que o webhook re-invoca sem erro 5xx — comportamento esperado pelo Asaas.
func TestWebhook_PaymentConfirmed_Reenvio_IdempotenteHTTP(t *testing.T) {
	svc := &fakeLinkPayService{}
	app := newWebhookApp(svc, nil)

	body := map[string]any{
		"event":   "PAYMENT_CONFIRMED",
		"payment": map[string]any{"id": "pay_dup"},
	}

	resp1 := postJSON(t, app, body, testWebhookSecret)
	resp2 := postJSON(t, app, body, testWebhookSecret)
	if resp1.StatusCode != fiber.StatusOK || resp2.StatusCode != fiber.StatusOK {
		t.Fatalf("status1=%d status2=%d", resp1.StatusCode, resp2.StatusCode)
	}
	if atomic.LoadInt32(&svc.processCalls) != 2 {
		t.Errorf("ProcessPaymentConfirmed deveria ter sido chamado 2x, foi %d", svc.processCalls)
	}
}

func TestWebhook_PayloadInvalido_RetornaBadRequest(t *testing.T) {
	svc := &fakeLinkPayService{}
	app := newWebhookApp(svc, nil)

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader([]byte("{not-json")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("asaas-access-token", testWebhookSecret)
	req.Header.Set("asaas-timestamp", strconv.FormatInt(time.Now().Unix(), 10))

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("status = %d, queria 400", resp.StatusCode)
	}
}

// #10: replay attack — token válido porém timestamp antigo (fora da janela de 5 min)
// deve ser rejeitado com 401 e sem chamar o service.
func TestWebhook_TimestampExpirado_RejeitaReplay(t *testing.T) {
	svc := &fakeLinkPayService{}
	app := newWebhookApp(svc, nil)

	body := map[string]any{
		"event":   "PAYMENT_CONFIRMED",
		"payment": map[string]any{"id": "pay_replay"},
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("asaas-access-token", testWebhookSecret)
	req.Header.Set("asaas-timestamp", strconv.FormatInt(time.Now().Unix()-3600, 10)) // 1h atrás

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("status = %d, queria 401 para replay com timestamp expirado", resp.StatusCode)
	}
	if atomic.LoadInt32(&svc.processCalls) != 0 {
		t.Errorf("ProcessPaymentConfirmed não deveria ser chamado em replay expirado")
	}
}

// #10: timestamp ausente — falha fechada (fail-closed): 401, service não é chamado.
func TestWebhook_TimestampAusente_Rejeita(t *testing.T) {
	svc := &fakeLinkPayService{}
	app := newWebhookApp(svc, nil)

	body := map[string]any{
		"event":   "PAYMENT_CONFIRMED",
		"payment": map[string]any{"id": "pay_no_ts"},
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("asaas-access-token", testWebhookSecret)
	// sem header asaas-timestamp

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("status = %d, queria 401 para timestamp ausente", resp.StatusCode)
	}
	if atomic.LoadInt32(&svc.processCalls) != 0 {
		t.Errorf("ProcessPaymentConfirmed não deveria ser chamado sem timestamp")
	}
}

func TestWebhook_TransferCompleted_ChamaPayoutService(t *testing.T) {
	svc := &fakeLinkPayService{}
	payoutSvc := &fakePayoutService{}
	app := newWebhookApp(svc, payoutSvc)

	body := map[string]any{
		"event": "TRANSFER_COMPLETED",
		"transfer": map[string]any{
			"id":          "tx-abc",
			"description": "Repasse LinkPay",
		},
	}
	resp := postJSON(t, app, body, testWebhookSecret)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, queria 200", resp.StatusCode)
	}
	if atomic.LoadInt32(&payoutSvc.transferCalls) != 1 {
		t.Errorf("HandleTransferWebhook chamado %d vezes", payoutSvc.transferCalls)
	}
	if payoutSvc.transferEvents[0] != "TRANSFER_COMPLETED" || payoutSvc.transferIDs[0] != "tx-abc" {
		t.Errorf("eventos recebidos = %+v / %+v", payoutSvc.transferEvents, payoutSvc.transferIDs)
	}
}

func TestWebhook_EventoDesconhecido_Retorna200(t *testing.T) {
	svc := &fakeLinkPayService{}
	app := newWebhookApp(svc, nil)

	body := map[string]any{"event": "SOMETHING_UNRELATED"}
	resp := postJSON(t, app, body, testWebhookSecret)
	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("status = %d, queria 200 para evento desconhecido (evita retry)", resp.StatusCode)
	}
	if atomic.LoadInt32(&svc.processCalls) != 0 {
		t.Errorf("ProcessPaymentConfirmed não deveria ser chamado em evento desconhecido")
	}
	// drena body para silenciar linter
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}
