package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"linkpay-service/internal/domain"
	"linkpay-service/internal/handler"
)

// dataSubjectStub implementa DataSubjectServiceInterface (LGPD Art. 18).
type dataSubjectStub struct {
	eraseResult *domain.BuyerDataErasureResult
	eraseErr    error
	export      *domain.BuyerDataExport
	exportErr   error
	lastEmail   string
	lastToken   string
	eraseCalled bool
	exportCalls int
}

func (d *dataSubjectStub) EraseBuyerData(_ context.Context, email, token string) (*domain.BuyerDataErasureResult, error) {
	d.eraseCalled = true
	d.lastEmail, d.lastToken = email, token
	return d.eraseResult, d.eraseErr
}

func (d *dataSubjectStub) ExportBuyerData(_ context.Context, email, token string) (*domain.BuyerDataExport, error) {
	d.exportCalls++
	d.lastEmail, d.lastToken = email, token
	return d.export, d.exportErr
}

func newDataSubjectApp(svc *dataSubjectStub) *fiber.App {
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	h := handler.NewDataSubjectHandler(svc)
	app.Delete("/data-erasure", h.DataErasure)
	app.Post("/data-portability", h.DataPortability)
	return app
}

func doJSONReq(t *testing.T, app *fiber.App, method, path, body string) *http.Response {
	t.Helper()
	var rdr *bytes.Reader
	if body == "" {
		rdr = bytes.NewReader(nil)
	} else {
		rdr = bytes.NewReader([]byte(body))
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	return resp
}

func decodeBody(t *testing.T, resp *http.Response) map[string]interface{} {
	t.Helper()
	defer resp.Body.Close()
	var out map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return out
}

// ─── DELETE /data-erasure ───────────────────────────────────────────────────

func TestDataErasure_Ok(t *testing.T) {
	svc := &dataSubjectStub{
		eraseResult: &domain.BuyerDataErasureResult{
			Email: "buyer@example.com", PurchasesErased: 2, SubscriptionsErased: 1,
		},
	}
	app := newDataSubjectApp(svc)

	resp := doJSONReq(t, app, http.MethodDelete, "/data-erasure",
		`{"email":"  Buyer@Example.COM ","token":"tok-123"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	if !svc.eraseCalled {
		t.Fatal("service não foi chamado")
	}
	// Normalização: email minúsculo e sem espaços antes de chegar ao service.
	if svc.lastEmail != "buyer@example.com" {
		t.Errorf("email normalizado = %q, want buyer@example.com", svc.lastEmail)
	}
	if svc.lastToken != "tok-123" {
		t.Errorf("token = %q, want tok-123", svc.lastToken)
	}

	body := decodeBody(t, resp)
	if body["purchases_erased"] != float64(2) || body["subscriptions_erased"] != float64(1) {
		t.Errorf("resposta inesperada: %v", body)
	}
}

func TestDataErasure_MissingFields_BadRequest(t *testing.T) {
	svc := &dataSubjectStub{}
	app := newDataSubjectApp(svc)

	resp := doJSONReq(t, app, http.MethodDelete, "/data-erasure", `{"email":"buyer@example.com"}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if svc.eraseCalled {
		t.Fatal("service não deveria ser chamado com body inválido")
	}

	resp = doJSONReq(t, app, http.MethodDelete, "/data-erasure", `{"token":"tok"}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status sem email = %d, want 400", resp.StatusCode)
	}

	resp = doJSONReq(t, app, http.MethodDelete, "/data-erasure", `not-json`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status body inválido = %d, want 400", resp.StatusCode)
	}
}

func TestDataErasure_UnauthorizedSubject_Forbidden(t *testing.T) {
	svc := &dataSubjectStub{eraseErr: errors.New("token não pertence ao email informado")}
	app := newDataSubjectApp(svc)

	resp := doJSONReq(t, app, http.MethodDelete, "/data-erasure",
		`{"email":"outro@example.com","token":"tok-123"}`)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

// ─── POST /data-portability ─────────────────────────────────────────────────

func TestDataPortability_Ok(t *testing.T) {
	now := time.Now().UTC()
	svc := &dataSubjectStub{
		export: &domain.BuyerDataExport{
			Email:       "buyer@example.com",
			GeneratedAt: now,
			Purchases: []domain.BuyerPurchaseExport{
				{ID: "p1", ProductID: "prod-1", ProductName: "Ebook", AmountCents: 2990,
					PaymentMethod: "pix", PaymentStatus: "confirmed", CreatedAt: now},
			},
			Subscriptions: []domain.BuyerSubscriptionExport{},
		},
	}
	app := newDataSubjectApp(svc)

	resp := doJSONReq(t, app, http.MethodPost, "/data-portability",
		`{"email":"buyer@example.com","token":"tok-abc"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if svc.exportCalls != 1 {
		t.Fatalf("exportCalls = %d, want 1", svc.exportCalls)
	}

	body := decodeBody(t, resp)
	if body["email"] != "buyer@example.com" {
		t.Errorf("email = %v", body["email"])
	}
	purchases, ok := body["purchases"].([]interface{})
	if !ok || len(purchases) != 1 {
		t.Fatalf("purchases ausente/vazio: %v", body["purchases"])
	}
	first := purchases[0].(map[string]interface{})
	if first["product_name"] != "Ebook" {
		t.Errorf("product_name = %v", first["product_name"])
	}
	// Garantia de minimização: export nunca carrega access_token/customer_cpf.
	if _, leak := first["access_token"]; leak {
		t.Error("access_token não pode aparecer na portabilidade")
	}
	if _, leak := first["customer_cpf"]; leak {
		t.Error("customer_cpf não pode aparecer na portabilidade")
	}
}

func TestDataPortability_Forbidden(t *testing.T) {
	svc := &dataSubjectStub{exportErr: errors.New("token inválido")}
	app := newDataSubjectApp(svc)

	resp := doJSONReq(t, app, http.MethodPost, "/data-portability",
		`{"email":"buyer@example.com","token":"token-invalido"}`)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}
