package service_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"linkpay-service/internal/service"
)

// ─── CreateCustomer ─────────────────────────────────────────────────────────

func TestCovAsaasCreateCustomer_Success(t *testing.T) {
	var gotAuth string
	a := covNewAsaasServer(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("access_token")
		if r.Method != http.MethodPost || r.URL.Path != "/customers" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q", ct)
		}
		req := covDecodeBody[map[string]string](t, r)
		if req["email"] != "buyer@example.com" || req["name"] != "Buyer" || req["cpfCnpj"] != "123" {
			t.Errorf("payload = %+v", req)
		}
		covJSON(w, http.StatusOK, `{"id":"cus_1","name":"Buyer","email":"buyer@example.com","cpfCnpj":"123"}`)
	})
	defer a.close()

	c := a.client()
	cust, err := c.CreateCustomer("Buyer", "buyer@example.com", "123")
	if err != nil {
		t.Fatalf("CreateCustomer: %v", err)
	}
	if cust.ID != "cus_1" || cust.Email != "buyer@example.com" || cust.CPF != "123" {
		t.Errorf("customer = %+v", cust)
	}
	if gotAuth != "cov-key" {
		t.Errorf("access_token header = %q", gotAuth)
	}
}

func TestCovAsaasCreateCustomer_HTTPError(t *testing.T) {
	a := covNewAsaasServer(func(w http.ResponseWriter, _ *http.Request) {
		covJSON(w, http.StatusBadRequest, `{"errors":[{"description":"cpf invalido"}]}`)
	})
	defer a.close()

	if _, err := a.client().CreateCustomer("X", "x@example.com", "000"); err == nil {
		t.Fatal("expected error for 400")
	} else if !strings.Contains(err.Error(), "status=400") || !strings.Contains(err.Error(), "cpf invalido") {
		t.Errorf("error should carry status and body, got %v", err)
	}
}

func TestCovAsaasCreateCustomer_InvalidJSON(t *testing.T) {
	a := covNewAsaasServer(func(w http.ResponseWriter, _ *http.Request) {
		covJSON(w, http.StatusOK, `{not-json`)
	})
	defer a.close()

	if _, err := a.client().CreateCustomer("X", "x@example.com", "0"); err == nil {
		t.Fatal("expected decode error")
	} else if !strings.Contains(err.Error(), "decode") {
		t.Errorf("error = %v", err)
	}
}

func TestCovAsaasCreateCustomer_NetworkError(t *testing.T) {
	a := covNewAsaasServer(func(w http.ResponseWriter, _ *http.Request) {})
	c := a.client()
	a.close() // server down → connection refused

	if _, err := c.CreateCustomer("X", "x@example.com", "0"); err == nil {
		t.Fatal("expected network error")
	} else if !strings.Contains(err.Error(), "retries") {
		t.Errorf("error = %v", err)
	}
}

func TestCovAsaasCreateCustomer_5xxRetriesThenFails(t *testing.T) {
	a := covNewAsaasServer(func(w http.ResponseWriter, _ *http.Request) {
		covJSON(w, http.StatusServiceUnavailable, `{"errors":"down"}`)
	})
	defer a.close()

	if _, err := a.client().CreateCustomer("X", "x@example.com", "0"); err == nil {
		t.Fatal("expected error after retries")
	}
	if got := a.callCount(); got != 3 {
		t.Errorf("attempts = %d, want 3 (maxRetries)", got)
	}
}

// ─── CreateTransfer ─────────────────────────────────────────────────────────

func TestCovAsaasCreateTransfer_Success(t *testing.T) {
	a := covNewAsaasServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/transfers" {
			t.Errorf("path = %s", r.URL.Path)
		}
		req := covDecodeBody[service.AsaasTransferRequest](t, r)
		if req.Value != 12.34 || req.PixAddressKey != "k" || req.ScheduleDate != "2026-01-02" {
			t.Errorf("payload = %+v", req)
		}
		covJSON(w, http.StatusOK, `{"id":"tr_1","status":"PENDING","value":12.34}`)
	})
	defer a.close()

	resp, err := a.client().CreateTransfer(service.AsaasTransferRequest{
		Value: 12.34, PixAddressKey: "k", PixAddressKeyType: "EVP", ScheduleDate: "2026-01-02",
	})
	if err != nil {
		t.Fatalf("CreateTransfer: %v", err)
	}
	if resp.ID != "tr_1" || resp.Status != "PENDING" || resp.Value != 12.34 {
		t.Errorf("resp = %+v", resp)
	}
}

func TestCovAsaasCreateTransfer_HTTPError(t *testing.T) {
	a := covNewAsaasServer(func(w http.ResponseWriter, _ *http.Request) {
		covJSON(w, http.StatusUnprocessableEntity, `{"errors":"saldo insuficiente"}`)
	})
	defer a.close()

	if _, err := a.client().CreateTransfer(service.AsaasTransferRequest{Value: 1}); err == nil {
		t.Fatal("expected error")
	} else if !strings.Contains(err.Error(), "status=422") || !strings.Contains(err.Error(), "saldo insuficiente") {
		t.Errorf("error = %v", err)
	}
}

func TestCovAsaasCreateTransfer_InvalidJSON(t *testing.T) {
	a := covNewAsaasServer(func(w http.ResponseWriter, _ *http.Request) {
		covJSON(w, http.StatusOK, `<xml/>`)
	})
	defer a.close()

	if _, err := a.client().CreateTransfer(service.AsaasTransferRequest{Value: 1}); err == nil {
		t.Fatal("expected decode error")
	} else if !strings.Contains(err.Error(), "decode") {
		t.Errorf("error = %v", err)
	}
}

func TestCovAsaasCreateTransfer_NetworkError(t *testing.T) {
	a := covNewAsaasServer(func(w http.ResponseWriter, _ *http.Request) {})
	c := a.client()
	a.close()

	if _, err := c.CreateTransfer(service.AsaasTransferRequest{Value: 1}); err == nil {
		t.Fatal("expected network error")
	}
}

// ─── CancelSubscription ─────────────────────────────────────────────────────

func TestCovAsaasCancelSubscription_Success(t *testing.T) {
	a := covNewAsaasServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/subscriptions/sub_1" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("access_token") != "cov-key" {
			t.Errorf("missing access_token")
		}
		w.WriteHeader(http.StatusOK)
	})
	defer a.close()

	if err := a.client().CancelSubscription("sub_1"); err != nil {
		t.Fatalf("CancelSubscription: %v", err)
	}
}

func TestCovAsaasCancelSubscription_HTTPError(t *testing.T) {
	a := covNewAsaasServer(func(w http.ResponseWriter, _ *http.Request) {
		covJSON(w, http.StatusNotFound, `{"errors":"Subscription not found"}`)
	})
	defer a.close()

	err := a.client().CancelSubscription("missing")
	if err == nil || !strings.Contains(err.Error(), "status=404") {
		t.Fatalf("error = %v", err)
	}
}

func TestCovAsaasCancelSubscription_NetworkError(t *testing.T) {
	a := covNewAsaasServer(func(w http.ResponseWriter, _ *http.Request) {})
	c := a.client()
	a.close()

	if err := c.CancelSubscription("sub_1"); err == nil {
		t.Fatal("expected network error")
	}
}

// ─── RefundPayment ──────────────────────────────────────────────────────────

func TestCovAsaasRefundPayment_Success(t *testing.T) {
	a := covNewAsaasServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/payments/pay_1/refund" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		covJSON(w, http.StatusOK, `{"id":"refund_1"}`)
	})
	defer a.close()

	if err := a.client().RefundPayment("pay_1"); err != nil {
		t.Fatalf("RefundPayment: %v", err)
	}
}

func TestCovAsaasRefundPayment_HTTPError(t *testing.T) {
	a := covNewAsaasServer(func(w http.ResponseWriter, _ *http.Request) {
		covJSON(w, http.StatusBadRequest, `{"errors":"já estornado"}`)
	})
	defer a.close()

	err := a.client().RefundPayment("pay_1")
	if err == nil || !strings.Contains(err.Error(), "já estornado") {
		t.Fatalf("error = %v", err)
	}
}

func TestCovAsaasRefundPayment_NetworkError(t *testing.T) {
	a := covNewAsaasServer(func(w http.ResponseWriter, _ *http.Request) {})
	c := a.client()
	a.close()

	if err := c.RefundPayment("pay_1"); err == nil {
		t.Fatal("expected network error")
	}
}

// ─── retry / circuit breaker / pulse tracking ───────────────────────────────

func TestCovAsaasRetryExhausted_TracksPulse5xx(t *testing.T) {
	a := covNewAsaasServer(func(w http.ResponseWriter, _ *http.Request) {
		// corpo longo para exercitar truncateString (> 200 chars)
		covJSON(w, http.StatusInternalServerError, `{"errors":"`+strings.Repeat("e", 400)+`"}`)
	})
	defer a.close()

	c := a.clientWithPulse(t)
	if _, err := c.CreateTransfer(service.AsaasTransferRequest{Value: 1}); err == nil {
		t.Fatal("expected error after retries")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		a.pulseMu.Lock()
		n := a.pulseEvents
		a.pulseMu.Unlock()
		if n > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("pulse event was not tracked for exhausted 5xx retries")
}

func TestCovAsaasCircuitBreakerOpensAndFailsFast(t *testing.T) {
	a := covNewAsaasServer(func(w http.ResponseWriter, _ *http.Request) {
		covJSON(w, http.StatusInternalServerError, `{"errors":"down"}`)
	})
	defer a.close()

	c := a.clientWithPulse(t)
	// 2 chamadas geram >=5 falhas (3 + 2 antes do breaker abrir).
	for i := 0; i < 2; i++ {
		if _, err := c.CreateTransfer(service.AsaasTransferRequest{Value: 1}); err == nil {
			t.Fatalf("call %d: expected error", i)
		}
	}
	before := a.callCount()

	start := time.Now()
	if _, err := c.CreateTransfer(service.AsaasTransferRequest{Value: 1}); err == nil {
		t.Fatal("expected circuit breaker error")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("circuit breaker should fail fast, took %v", elapsed)
	}
	if got := a.callCount(); got != before {
		t.Errorf("breaker open but %d extra HTTP calls were made", got-before)
	}
}

func TestCovAsaasNetworkError_TracksPulse(t *testing.T) {
	a := covNewAsaasServer(func(w http.ResponseWriter, _ *http.Request) {})
	c := a.clientWithPulse(t)
	a.close()

	if _, err := c.CreateTransfer(service.AsaasTransferRequest{Value: 1}); err == nil {
		t.Fatal("expected network error")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		a.pulseMu.Lock()
		n := a.pulseEvents
		a.pulseMu.Unlock()
		if n > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("pulse event was not tracked for network failure")
}

func TestCovAsaasClient_NilLoggerUsesDefault(t *testing.T) {
	c := service.NewAsaasClient("http://127.0.0.1:1", "k", nil, nil)
	if c == nil {
		t.Fatal("client nil")
	}
	// exercita o caminho de erro com logger default
	if _, err := c.CreateTransfer(service.AsaasTransferRequest{Value: 1}); err == nil {
		t.Fatal("expected error")
	}
}

func TestCovAsaasErrorSerialization(t *testing.T) {
	// garante que os tipos de request serializam os campos opcionais corretamente
	b, err := json.Marshal(service.AsaasPaymentRequest{Customer: "c", Value: 10})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), `"customer":"c"`) {
		t.Errorf("payload = %s", b)
	}
}

func TestCovAsaas_InvalidBaseURL_RequestBuildError(t *testing.T) {
	c := service.NewAsaasClient("://bad-url", "k", nil, nil)
	if _, err := c.CreateCustomer("n", "e@e.com", "1"); err == nil {
		t.Error("expected request build error for CreateCustomer")
	}
	if _, err := c.CreateTransfer(service.AsaasTransferRequest{Value: 1}); err == nil {
		t.Error("expected request build error for CreateTransfer")
	}
	if err := c.CancelSubscription("sub"); err == nil {
		t.Error("expected request build error for CancelSubscription")
	}
	if err := c.RefundPayment("pay"); err == nil {
		t.Error("expected request build error for RefundPayment")
	}
}
