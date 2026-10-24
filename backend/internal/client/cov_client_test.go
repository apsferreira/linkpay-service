package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// --- CartClient construction ---

func TestCovNewCartClient_Defaults(t *testing.T) {
	t.Setenv("CART_SERVICE_URL", "")
	t.Setenv("SERVICE_TOKEN", "")

	c := NewCartClient()
	if c == nil {
		t.Fatal("expected non-nil CartClient")
	}
	if c.baseURL != defaultCartServiceURL {
		t.Fatalf("expected default baseURL %q, got %q", defaultCartServiceURL, c.baseURL)
	}
	if c.serviceToken != "" {
		t.Fatalf("expected empty token, got %q", c.serviceToken)
	}
	if c.httpClient == nil {
		t.Fatal("expected non-nil httpClient")
	}
	if c.httpClient.Timeout != 10*time.Second {
		t.Fatalf("expected 10s timeout, got %v", c.httpClient.Timeout)
	}
}

func TestCovNewCartClient_FromEnv(t *testing.T) {
	t.Setenv("CART_SERVICE_URL", "http://cart.internal:9999")
	t.Setenv("SERVICE_TOKEN", "svc-token")

	c := NewCartClient()
	if c.baseURL != "http://cart.internal:9999" {
		t.Fatalf("expected env baseURL, got %q", c.baseURL)
	}
	if c.serviceToken != "svc-token" {
		t.Fatalf("expected env token, got %q", c.serviceToken)
	}
}

// --- CartClient.CreateCart ---

func TestCovCreateCart_Success(t *testing.T) {
	var (
		gotPath  string
		gotToken string
		gotBody  CreateCartRequest
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotToken = r.Header.Get("X-Service-Token")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"cart-1","total":99.9,"items":[{"id":"it-1","product_id":"p-1","product_type":"digital_product","name":"Curso","quantity":1,"price":99.9}]}`))
	}))
	defer srv.Close()

	c := &CartClient{baseURL: srv.URL, serviceToken: "tok-cov", httpClient: srv.Client()}
	cart, err := c.CreateCart(context.Background(), "tenant-1", "cust-1", "curso-ia")
	if err != nil {
		t.Fatalf("CreateCart returned error: %v", err)
	}
	if cart.ID != "cart-1" || cart.Total != 99.9 || len(cart.Items) != 1 {
		t.Fatalf("unexpected cart: %+v", cart)
	}
	if cart.Items[0].ProductType != "digital_product" || cart.Items[0].Name != "Curso" {
		t.Fatalf("unexpected item: %+v", cart.Items[0])
	}
	if gotPath != "/api/v1/carts" {
		t.Fatalf("unexpected path %q", gotPath)
	}
	if gotToken != "tok-cov" {
		t.Fatalf("unexpected token %q", gotToken)
	}
	if gotBody.TenantID != "tenant-1" || gotBody.CustomerID != "cust-1" ||
		gotBody.SourceService != "linkpay" || gotBody.SourceContext != "curso-ia" {
		t.Fatalf("unexpected request body: %+v", gotBody)
	}
}

func TestCovCreateCart_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":"tenant not found"}`))
	}))
	defer srv.Close()

	c := &CartClient{baseURL: srv.URL, httpClient: srv.Client()}
	cart, err := c.CreateCart(context.Background(), "tenant-x", "", "slug")
	if err == nil {
		t.Fatal("expected error for 422 response")
	}
	if cart != nil {
		t.Fatalf("expected nil cart, got %+v", cart)
	}
	msg := err.Error()
	if !strings.Contains(msg, "status=422") {
		t.Fatalf("error should contain status, got %q", msg)
	}
	if !strings.Contains(msg, "tenant not found") {
		t.Fatalf("error should contain response body, got %q", msg)
	}
}

func TestCovCreateCart_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{not valid json`))
	}))
	defer srv.Close()

	c := &CartClient{baseURL: srv.URL, httpClient: srv.Client()}
	if _, err := c.CreateCart(context.Background(), "t", "c", "s"); err == nil {
		t.Fatal("expected decode error for invalid JSON")
	} else if !strings.Contains(err.Error(), "decode") {
		t.Fatalf("expected decode error, got %q", err)
	}
}

func TestCovCreateCart_ServerClosed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	c := &CartClient{baseURL: url, httpClient: &http.Client{Timeout: 2 * time.Second}}
	if _, err := c.CreateCart(context.Background(), "t", "c", "s"); err == nil {
		t.Fatal("expected connection error for closed server")
	} else if !strings.Contains(err.Error(), "CreateCart") {
		t.Fatalf("expected wrapped CreateCart error, got %q", err)
	}
}

func TestCovCreateCart_InvalidBaseURL(t *testing.T) {
	c := &CartClient{baseURL: "http://example.com/\x7f", httpClient: http.DefaultClient}
	if _, err := c.CreateCart(context.Background(), "t", "c", "s"); err == nil {
		t.Fatal("expected request construction error for invalid base URL")
	}
}

// --- CartClient.AddItem ---

func TestCovAddItem_Success(t *testing.T) {
	var (
		gotPath string
		gotBody AddItemRequest
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"id":"cart-9","total":50,"items":[]}`))
	}))
	defer srv.Close()

	c := &CartClient{baseURL: srv.URL, serviceToken: "tok", httpClient: srv.Client()}
	cart, err := c.AddItem(context.Background(), "cart-9", AddItemRequest{
		ProductType: "digital_product",
		ProductID:   "prod-9",
		Quantity:    2,
		Price:       25,
		Name:        "Ebook",
	})
	if err != nil {
		t.Fatalf("AddItem returned error: %v", err)
	}
	if cart.ID != "cart-9" || cart.Total != 50 {
		t.Fatalf("unexpected cart: %+v", cart)
	}
	if gotPath != "/api/v1/carts/cart-9/items" {
		t.Fatalf("unexpected path %q", gotPath)
	}
	if gotBody.ProductID != "prod-9" || gotBody.Quantity != 2 || gotBody.Name != "Ebook" || gotBody.Price != 25 {
		t.Fatalf("unexpected body: %+v", gotBody)
	}
}

func TestCovAddItem_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte("cart closed"))
	}))
	defer srv.Close()

	c := &CartClient{baseURL: srv.URL, httpClient: srv.Client()}
	cart, err := c.AddItem(context.Background(), "cart-1", AddItemRequest{ProductID: "p"})
	if err == nil {
		t.Fatal("expected error for 409 response")
	}
	if cart != nil {
		t.Fatalf("expected nil cart, got %+v", cart)
	}
	if !strings.Contains(err.Error(), "status=409") || !strings.Contains(err.Error(), "cart closed") {
		t.Fatalf("unexpected error: %q", err)
	}
}

func TestCovAddItem_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html>oops</html>`))
	}))
	defer srv.Close()

	c := &CartClient{baseURL: srv.URL, httpClient: srv.Client()}
	if _, err := c.AddItem(context.Background(), "cart-1", AddItemRequest{}); err == nil {
		t.Fatal("expected decode error for non-JSON body")
	} else if !strings.Contains(err.Error(), "AddItem decode") {
		t.Fatalf("expected decode error, got %q", err)
	}
}

func TestCovAddItem_ServerClosed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	c := &CartClient{baseURL: url, httpClient: &http.Client{Timeout: 2 * time.Second}}
	if _, err := c.AddItem(context.Background(), "cart-1", AddItemRequest{}); err == nil {
		t.Fatal("expected connection error for closed server")
	} else if !strings.Contains(err.Error(), "AddItem") {
		t.Fatalf("expected wrapped AddItem error, got %q", err)
	}
}

// --- post() helper ---

func TestCovPost_InvalidURL(t *testing.T) {
	// Control characters make http.NewRequestWithContext fail before any I/O.
	c := &CartClient{baseURL: "http://cart-service/\x01", httpClient: http.DefaultClient}
	resp, err := c.post(context.Background(), "/api/v1/carts", []byte(`{}`))
	if err == nil {
		if resp != nil {
			_ = resp.Body.Close()
		}
		t.Fatal("expected error for URL with control character")
	}
	if resp != nil {
		t.Fatalf("expected nil response, got %+v", resp)
	}
	if !strings.Contains(err.Error(), "invalid control character") {
		t.Fatalf("unexpected error: %q", err)
	}
}

func TestCovPost_SetsHeaders(t *testing.T) {
	var gotHeader http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := &CartClient{baseURL: srv.URL, serviceToken: "abc", httpClient: srv.Client()}
	resp, err := c.post(context.Background(), "/x", []byte(`{}`))
	if err != nil {
		t.Fatalf("post returned error: %v", err)
	}
	defer resp.Body.Close()

	if gotHeader.Get("Content-Type") != "application/json" {
		t.Fatalf("unexpected content type %q", gotHeader.Get("Content-Type"))
	}
	if gotHeader.Get("X-Service-Token") != "abc" {
		t.Fatalf("unexpected token %q", gotHeader.Get("X-Service-Token"))
	}
}

// --- NotificationClient ---

func TestCovNewNotificationClient_EnvAbsent(t *testing.T) {
	t.Setenv("NOTIFICATION_SERVICE_URL", "")
	if c := NewNotificationClient(); c != nil {
		t.Fatalf("expected nil client when env is absent, got %+v", c)
	}
}

func TestCovNewNotificationClient_EnvPresent(t *testing.T) {
	t.Setenv("NOTIFICATION_SERVICE_URL", "http://notification.internal:8080")
	t.Setenv("SERVICE_TOKEN", "notif-token")

	c := NewNotificationClient()
	if c == nil {
		t.Fatal("expected non-nil client when env is present")
	}
	if c.baseURL != "http://notification.internal:8080" {
		t.Fatalf("unexpected baseURL %q", c.baseURL)
	}
	if c.serviceToken != "notif-token" {
		t.Fatalf("unexpected token %q", c.serviceToken)
	}
	if c.httpClient == nil || c.httpClient.Timeout != 10*time.Second {
		t.Fatalf("unexpected http client: %+v", c.httpClient)
	}
}

func TestCovSendPurchaseConfirmation_Success(t *testing.T) {
	var (
		gotPath   string
		gotToken  string
		gotCType  string
		gotMethod string
		gotBody   PurchaseConfirmedPayload
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotToken = r.Header.Get("X-Service-Token")
		gotCType = r.Header.Get("Content-Type")
		gotMethod = r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := &NotificationClient{baseURL: srv.URL, serviceToken: "tok", httpClient: srv.Client()}
	err := c.SendPurchaseConfirmation(context.Background(), "buyer@example.com", "Buyer", "Curso", "https://dl/x", 3)
	if err != nil {
		t.Fatalf("SendPurchaseConfirmation returned error: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/v1/notifications/purchase-confirmed" {
		t.Fatalf("unexpected request %s %s", gotMethod, gotPath)
	}
	if gotToken != "tok" || gotCType != "application/json" {
		t.Fatalf("unexpected headers token=%q ctype=%q", gotToken, gotCType)
	}
	if gotBody.BuyerEmail != "buyer@example.com" || gotBody.BuyerName != "Buyer" ||
		gotBody.ProductName != "Curso" || gotBody.DownloadURL != "https://dl/x" ||
		gotBody.DownloadLimit != 3 {
		t.Fatalf("unexpected payload: %+v", gotBody)
	}
}

func TestCovSendPurchaseConfirmation_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid email"}`))
	}))
	defer srv.Close()

	c := &NotificationClient{baseURL: srv.URL, httpClient: srv.Client()}
	err := c.SendPurchaseConfirmation(context.Background(), "bad", "", "", "", 0)
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
	if !strings.Contains(err.Error(), "status=400") || !strings.Contains(err.Error(), "invalid email") {
		t.Fatalf("error should include status and body, got %q", err)
	}
}

func TestCovSendPurchaseConfirmation_NetworkError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	c := &NotificationClient{baseURL: url, httpClient: &http.Client{Timeout: 2 * time.Second}}
	err := c.SendPurchaseConfirmation(context.Background(), "b@e.com", "B", "P", "u", 1)
	if err == nil {
		t.Fatal("expected network error for closed server")
	}
	if !strings.Contains(err.Error(), "notification_client: do") {
		t.Fatalf("expected wrapped do error, got %q", err)
	}
}

func TestCovSendPurchaseConfirmation_InvalidBaseURL(t *testing.T) {
	c := &NotificationClient{baseURL: "http://notification/\x7f", httpClient: http.DefaultClient}
	err := c.SendPurchaseConfirmation(context.Background(), "b@e.com", "B", "P", "u", 1)
	if err == nil {
		t.Fatal("expected new request error for invalid base URL")
	}
	if !strings.Contains(err.Error(), "new request") {
		t.Fatalf("expected new request error, got %q", err)
	}
}
