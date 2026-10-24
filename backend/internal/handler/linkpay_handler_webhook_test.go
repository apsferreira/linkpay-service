package handler

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"

	"linkpay-service/internal/domain"
	"linkpay-service/internal/pkg/pulse"
	"linkpay-service/internal/storage"
)

// mockWebhookService implementa LinkPayServiceInterface para testar idempotência.
type mockWebhookService struct {
	processPaymentConfirmedCalls int
}

func (m *mockWebhookService) CreateProduct(_ context.Context, _ string, _ *domain.CreateDigitalProductRequest) (*domain.DigitalProduct, error) {
	return nil, nil
}
func (m *mockWebhookService) GetProductBySlug(_ context.Context, _ string) (*domain.DigitalProduct, error) {
	return nil, nil
}
func (m *mockWebhookService) GetProductByID(_ context.Context, _, _ string) (*domain.DigitalProduct, error) {
	return nil, nil
}
func (m *mockWebhookService) ListProducts(_ context.Context, _ string) ([]domain.DigitalProduct, error) {
	return nil, nil
}
func (m *mockWebhookService) UpdateProduct(_ context.Context, _, _ string, _ *domain.UpdateDigitalProductRequest) (*domain.DigitalProduct, error) {
	return nil, nil
}
func (m *mockWebhookService) UpdateProductStorage(_ context.Context, _, _, _, _, _ string, _ int64, _ string) error {
	return nil
}
func (m *mockWebhookService) DeactivateProduct(_ context.Context, _, _ string) error {
	return nil
}
func (m *mockWebhookService) InitiatePurchase(_ context.Context, _ string, _ *domain.InitiatePurchaseRequest) (*domain.DigitalPurchase, error) {
	return nil, nil
}
func (m *mockWebhookService) ProcessPaymentConfirmed(_ context.Context, chargeID string) error {
	m.processPaymentConfirmedCalls++
	return nil
}
func (m *mockWebhookService) Download(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (m *mockWebhookService) GetSalesStats(_ context.Context, _ string) (*domain.LinkPayStatsResponse, error) {
	return nil, nil
}
func (m *mockWebhookService) AddProductFile(_ context.Context, _ *domain.DigitalProductFile) error {
	return nil
}
func (m *mockWebhookService) ListProductFiles(_ context.Context, _ string) ([]domain.DigitalProductFile, error) {
	return nil, nil
}
func (m *mockWebhookService) GetProductFile(_ context.Context, _ string) (*domain.DigitalProductFile, error) {
	return nil, nil
}
func (m *mockWebhookService) DeleteProductFile(_ context.Context, _, _ string) error {
	return nil
}
func (m *mockWebhookService) GetPurchaseByToken(_ context.Context, _ string) (*domain.DigitalPurchase, error) {
	return nil, nil
}
func (m *mockWebhookService) GetUpsellsByProductID(_ context.Context, _ string) ([]domain.UpsellSuggestion, error) {
	return nil, nil
}
func (m *mockWebhookService) UpdateProductCover(_ context.Context, _, _, _ string) error { return nil }
func (m *mockWebhookService) RefundPurchase(_ context.Context, _, _ string) error        { return nil }
func (m *mockWebhookService) GetCommissionSummary(_ context.Context, _, _ string) (*domain.CommissionSummaryResponse, error) {
	return &domain.CommissionSummaryResponse{}, nil
}

// TestHandleWebhook_IdempotenciaRedis verifica que um segundo evento com o mesmo
// chargeID retorna 200 sem chamar ProcessPaymentConfirmed.
func TestHandleWebhook_IdempotenciaRedis(t *testing.T) {
	s, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run(): %v", err)
	}
	defer s.Close()

	redisClient := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer redisClient.Close()

	svc := &mockWebhookService{}
	handler := NewLinkPayHandler(svc, "secret123", &storage.MinIOClient{}, pulse.New())
	handler.setWebhookRedisClient(redisClient)

	app := fiber.New()
	app.Post("/webhook", handler.HandleWebhook)

	// Primeira chamada — deve processar normalmente
	payload := `{"event":"PAYMENT_CONFIRMED","payment":{"id":"charge-123"}}`
	req1 := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	req1.Header.Set("Content-Type", "application/json")
	req1.Header.Set("asaas-access-token", "secret123")
	req1.Header.Set("asaas-timestamp", strconv.FormatInt(time.Now().Unix(), 10))

	resp1, err := app.Test(req1, -1)
	if err != nil {
		t.Fatalf("primeira chamada: %v", err)
	}
	if resp1.StatusCode != fiber.StatusOK {
		t.Errorf("primeira chamada: status=%d, esperado 200", resp1.StatusCode)
	}
	if svc.processPaymentConfirmedCalls != 1 {
		t.Errorf("primeira chamada: ProcessPaymentConfirmed foi chamado %d vez(es), esperado 1", svc.processPaymentConfirmedCalls)
	}

	// Segunda chamada — mesmo chargeID, deve ser rejeitada pela idempotência
	svc.processPaymentConfirmedCalls = 0
	req2 := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("asaas-access-token", "secret123")
	req2.Header.Set("asaas-timestamp", strconv.FormatInt(time.Now().Unix(), 10))

	resp2, err := app.Test(req2, -1)
	if err != nil {
		t.Fatalf("segunda chamada: %v", err)
	}
	if resp2.StatusCode != fiber.StatusOK {
		t.Errorf("segunda chamada: status=%d, esperado 200", resp2.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp2.Body).Decode(&body); err != nil {
		t.Fatalf("decodificar body: %v", err)
	}
	if body["idempotent"] != true {
		t.Errorf("segunda chamada: idempotent=%v, esperado true", body["idempotent"])
	}
	if svc.processPaymentConfirmedCalls != 0 {
		t.Errorf("segunda chamada: ProcessPaymentConfirmed foi chamado %d vez(es), esperado 0", svc.processPaymentConfirmedCalls)
	}
}

// TestHandleWebhook_IdempotenciaChargeDiferente verifica que chargeIDs diferentes
// são processados independentemente.
func TestHandleWebhook_IdempotenciaChargeDiferente(t *testing.T) {
	s, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run(): %v", err)
	}
	defer s.Close()

	redisClient := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer redisClient.Close()

	svc := &mockWebhookService{}
	handler := NewLinkPayHandler(svc, "secret123", &storage.MinIOClient{}, pulse.New())
	handler.setWebhookRedisClient(redisClient)

	app := fiber.New()
	app.Post("/webhook", handler.HandleWebhook)

	// Primeira chamada com charge-abc
	payload1 := `{"event":"PAYMENT_CONFIRMED","payment":{"id":"charge-abc"}}`
	req1 := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload1))
	req1.Header.Set("Content-Type", "application/json")
	req1.Header.Set("asaas-access-token", "secret123")
	req1.Header.Set("asaas-timestamp", strconv.FormatInt(time.Now().Unix(), 10))
	resp1, _ := app.Test(req1, -1)
	if resp1.StatusCode != fiber.StatusOK {
		t.Errorf("primeira chamada: status=%d", resp1.StatusCode)
	}

	// Segunda chamada com charge-xyz (diferente)
	payload2 := `{"event":"PAYMENT_CONFIRMED","payment":{"id":"charge-xyz"}}`
	req2 := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload2))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("asaas-access-token", "secret123")
	req2.Header.Set("asaas-timestamp", strconv.FormatInt(time.Now().Unix(), 10))
	resp2, _ := app.Test(req2, -1)
	if resp2.StatusCode != fiber.StatusOK {
		t.Errorf("segunda chamada (charge diferente): status=%d", resp2.StatusCode)
	}

	// O serviço deve ter sido chamado 2 vezes (uma para cada charge)
	if svc.processPaymentConfirmedCalls != 2 {
		t.Errorf("ProcessPaymentConfirmed foi chamado %d vez(es), esperado 2", svc.processPaymentConfirmedCalls)
	}
}

// TestHandleWebhook_IdempotenciaSemRedis verifica que sem Redis o fluxo segue normal.
func TestHandleWebhook_IdempotenciaSemRedis(t *testing.T) {
	svc := &mockWebhookService{}
	handler := NewLinkPayHandler(svc, "secret123", &storage.MinIOClient{}, pulse.New())

	app := fiber.New()
	app.Post("/webhook", handler.HandleWebhook)

	payload := `{"event":"PAYMENT_CONFIRMED","payment":{"id":"charge-456"}}`

	// Duas chamadas seguidas — ambas devem processar (sem idempotência)
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("asaas-access-token", "secret123")
		req.Header.Set("asaas-timestamp", strconv.FormatInt(time.Now().Unix(), 10))
		resp, _ := app.Test(req, -1)
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("chamada %d: status=%d", i+1, resp.StatusCode)
		}
	}

	if svc.processPaymentConfirmedCalls != 2 {
		t.Errorf("ProcessPaymentConfirmed foi chamado %d vez(es), esperado 2 (sem Redis)", svc.processPaymentConfirmedCalls)
	}
}

// TestHandleWebhook_IdempotenciaTTL verifica que a chave Redis tem TTL de 7 dias.
func TestHandleWebhook_IdempotenciaTTL(t *testing.T) {
	s, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run(): %v", err)
	}
	defer s.Close()

	redisClient := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer redisClient.Close()

	svc := &mockWebhookService{}
	handler := NewLinkPayHandler(svc, "secret123", &storage.MinIOClient{}, pulse.New())
	handler.setWebhookRedisClient(redisClient)

	app := fiber.New()
	app.Post("/webhook", handler.HandleWebhook)

	payload := `{"event":"PAYMENT_CONFIRMED","payment":{"id":"charge-ttl-test"}}`
	req := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("asaas-access-token", "secret123")
	req.Header.Set("asaas-timestamp", strconv.FormatInt(time.Now().Unix(), 10))
	_, _ = app.Test(req, -1)

	// Verifica TTL da chave no Redis
	ttl, err := redisClient.TTL(context.Background(), "webhook:linkpay:charge-ttl-test").Result()
	if err != nil {
		t.Fatalf("erro ao verificar TTL: %v", err)
	}
	expected := 7 * 24 * 60 * 60 // 7 dias em segundos
	if ttl.Seconds() < float64(expected-10) || ttl.Seconds() > float64(expected+10) {
		t.Errorf("TTL = %v, esperado ~%d segundos (7 dias)", ttl, expected)
	}
}

// --- #10: validação de timestamp do webhook Asaas (replay protection) ---

func newValidationHandler(secret string, maxAgeSeconds int64) *LinkPayHandler {
	return &LinkPayHandler{webhookSecret: secret, webhookMaxAgeSeconds: maxAgeSeconds}
}

func unixNow(t *testing.T) string {
	t.Helper()
	return strconv.FormatInt(time.Now().Unix(), 10)
}

// Token válido + timestamp recente (dentro da janela de 5 min) → aceito.
func TestValidateLinkPayWebhook_TokenValidoRecente_Aceita(t *testing.T) {
	h := newValidationHandler("secret123", 300)
	body := []byte(`{"event":"PAYMENT_CONFIRMED","payment":{"id":"charge-x"}}`)
	if !h.validateLinkPayWebhook(body, "secret123", unixNow(t)) {
		t.Error("webhook com token válido e timestamp recente deveria ser aceito")
	}
}

// Token válido mas timestamp expirado (fora da janela de 5 min) → rejeitado (replay).
func TestValidateLinkPayWebhook_TokenValidoExpirado_Rejeita(t *testing.T) {
	h := newValidationHandler("secret123", 300)
	body := []byte(`{"event":"PAYMENT_CONFIRMED","payment":{"id":"charge-x"}}`)
	old := strconv.FormatInt(time.Now().Unix()-301, 10) // 5min1s atrás
	if h.validateLinkPayWebhook(body, "secret123", old) {
		t.Error("webhook com timestamp expirado (replay) deveria ser rejeitado")
	}
}

// Token inválido + timestamp recente → rejeitado.
func TestValidateLinkPayWebhook_TokenInvalido_Rejeita(t *testing.T) {
	h := newValidationHandler("secret123", 300)
	body := []byte(`{"event":"PAYMENT_CONFIRMED","payment":{"id":"charge-x"}}`)
	if h.validateLinkPayWebhook(body, "token-errado", unixNow(t)) {
		t.Error("webhook com token inválido deveria ser rejeitado")
	}
}

// Timestamp ausente ou não-numérico → rejeitado (fail-closed).
func TestValidateLinkPayWebhook_TimestampAusenteOuInvalido_Rejeita(t *testing.T) {
	h := newValidationHandler("secret123", 300)
	body := []byte(`{"event":"PAYMENT_CONFIRMED","payment":{"id":"charge-x"}}`)
	for _, ts := range []string{"", "abc", "12.5"} {
		if h.validateLinkPayWebhook(body, "secret123", ts) {
			t.Errorf("timestamp %q deveria ser rejeitado", ts)
		}
	}
}

// Secret vazio ou body vazio → rejeitado.
func TestValidateLinkPayWebhook_SecretOuBodyVazio_Rejeita(t *testing.T) {
	h := newValidationHandler("secret123", 300)
	if h.validateLinkPayWebhook([]byte(`{}`), "", unixNow(t)) {
		t.Error("token vazio deveria ser rejeitado")
	}
	empty := &LinkPayHandler{webhookSecret: "", webhookMaxAgeSeconds: 300}
	if empty.validateLinkPayWebhook([]byte(`{}`), "x", unixNow(t)) {
		t.Error("secret vazio deveria ser rejeitado")
	}
	if h.validateLinkPayWebhook(nil, "secret123", unixNow(t)) {
		t.Error("body vazio deveria ser rejeitado")
	}
}

// Janela configurável via env ASAAS_WEBHOOK_MAX_AGE_SECONDS (lida no construtor).
func TestNewLinkPayHandler_WebhookMaxAge_ConfiguravelViaEnv(t *testing.T) {
	svc := &mockWebhookService{}
	withEnv := func(t *testing.T, value string) int64 {
		t.Helper()
		t.Setenv("ASAAS_WEBHOOK_MAX_AGE_SECONDS", value)
		return NewLinkPayHandler(svc, "secret123", &storage.MinIOClient{}, pulse.New()).webhookMaxAgeSeconds
	}

	if got := withEnv(t, ""); got != 300 {
		t.Errorf("default ASAAS_WEBHOOK_MAX_AGE_SECONDS = %d, esperado 300", got)
	}
	if got := withEnv(t, "60"); got != 60 {
		t.Errorf("ASAAS_WEBHOOK_MAX_AGE_SECONDS=60 → janela %d, esperado 60", got)
	}
	if got := withEnv(t, "0"); got != 300 {
		t.Errorf("ASAAS_WEBHOOK_MAX_AGE_SECONDS=0 → janela %d, esperado default 300", got)
	}
	if got := withEnv(t, "abc"); got != 300 {
		t.Errorf("ASAAS_WEBHOOK_MAX_AGE_SECONDS=abc → janela %d, esperado default 300", got)
	}
}

// A janela configurada por env é aplicada na validação (60s: 30s aceito, 90s rejeitado).
func TestValidateLinkPayWebhook_JanelaCustomizadaPorEnv(t *testing.T) {
	t.Setenv("ASAAS_WEBHOOK_MAX_AGE_SECONDS", "60")
	svc := &mockWebhookService{}
	h := NewLinkPayHandler(svc, "secret123", &storage.MinIOClient{}, pulse.New())
	body := []byte(`{"event":"PAYMENT_CONFIRMED","payment":{"id":"charge-x"}}`)

	recent := strconv.FormatInt(time.Now().Unix()-30, 10)
	if !h.validateLinkPayWebhook(body, "secret123", recent) {
		t.Error("timestamp de 30s atrás deveria ser aceito com janela de 60s")
	}
	expired := strconv.FormatInt(time.Now().Unix()-90, 10)
	if h.validateLinkPayWebhook(body, "secret123", expired) {
		t.Error("timestamp de 90s atrás deveria ser rejeitado com janela de 60s")
	}
}
