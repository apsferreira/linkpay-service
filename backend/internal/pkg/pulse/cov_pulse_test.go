package pulse

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// TestCovGetEnv covers both the override and default branches.
func TestCovGetEnv(t *testing.T) {
	t.Setenv("PULSE_COV_TEST_ENV", "configured")
	if got := getEnv("PULSE_COV_TEST_ENV", "fallback"); got != "configured" {
		t.Fatalf("expected override value, got %q", got)
	}

	os.Unsetenv("PULSE_COV_TEST_ENV")
	if got := getEnv("PULSE_COV_TEST_ENV", "fallback"); got != "fallback" {
		t.Fatalf("expected default value, got %q", got)
	}

	t.Setenv("PULSE_COV_TEST_ENV", "")
	if got := getEnv("PULSE_COV_TEST_ENV", "fallback"); got != "fallback" {
		t.Fatalf("expected default value for empty env, got %q", got)
	}
}

// TestCovNew_DefaultsAndOverride covers New with and without PULSE_URL.
func TestCovNew_DefaultsAndOverride(t *testing.T) {
	t.Setenv("PULSE_URL", "")
	t.Setenv("SERVICE_TOKEN", "tok")
	c := New()
	if c.baseURL != "http://pulse-service" {
		t.Fatalf("expected default baseURL, got %q", c.baseURL)
	}
	if c.serviceToken != "tok" {
		t.Fatalf("expected service token from env, got %q", c.serviceToken)
	}
	if c.httpClient == nil || c.httpClient.Timeout != 3*time.Second {
		t.Fatalf("expected http client with 3s timeout, got %+v", c.httpClient)
	}

	t.Setenv("PULSE_URL", "http://pulse.internal:8080")
	c = New()
	if c.baseURL != "http://pulse.internal:8080" {
		t.Fatalf("expected overridden baseURL, got %q", c.baseURL)
	}
}

// TestCovTrack_SendsPayloadAndHeaders asserts the full payload and the
// X-Service-Token header, waiting for the fire-and-forget goroutine.
func TestCovTrack_SendsPayloadAndHeaders(t *testing.T) {
	type captured struct {
		path   string
		token  string
		ctype  string
		method string
		body   map[string]interface{}
	}
	ch := make(chan captured, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		ch <- captured{
			path:   r.URL.Path,
			token:  r.Header.Get("X-Service-Token"),
			ctype:  r.Header.Get("Content-Type"),
			method: r.Method,
			body:   body,
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	t.Setenv("PULSE_URL", srv.URL)
	t.Setenv("SERVICE_TOKEN", "svc-token-cov")

	c := New()
	c.Track(context.Background(), "checkout.started", "user-42", map[string]string{
		"product_slug": "curso-ia",
		"step":         "2",
	})

	var got captured
	select {
	case got = <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for pulse request")
	}

	if got.method != http.MethodPost {
		t.Errorf("expected POST, got %s", got.method)
	}
	if got.path != "/api/v1/events/track" {
		t.Errorf("unexpected path %q", got.path)
	}
	if got.token != "svc-token-cov" {
		t.Errorf("expected X-Service-Token header, got %q", got.token)
	}
	if got.ctype != "application/json" {
		t.Errorf("expected JSON content type, got %q", got.ctype)
	}
	if got.body["event_name"] != "checkout.started" {
		t.Errorf("unexpected event_name: %v", got.body["event_name"])
	}
	if got.body["user_id"] != "user-42" {
		t.Errorf("unexpected user_id: %v", got.body["user_id"])
	}
	if got.body["product"] != "linkpay" {
		t.Errorf("unexpected product: %v", got.body["product"])
	}
	props, ok := got.body["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("properties is not an object: %T", got.body["properties"])
	}
	if props["product_slug"] != "curso-ia" || props["step"] != "2" {
		t.Errorf("unexpected properties: %v", props)
	}
}

// TestCovTrack_ClosedServer ensures a dead pulse-service never panics.
func TestCovTrack_ClosedServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	t.Setenv("PULSE_URL", url)
	t.Setenv("SERVICE_TOKEN", "tok")

	c := New()
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.Track(context.Background(), "purchase.completed", "user-1", map[string]string{"a": "b"})
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Track blocked on a closed server")
	}

	// Give the fire-and-forget goroutine a moment to hit the error path.
	time.Sleep(100 * time.Millisecond)
}

// TestCovTrack_InvalidURLNoPanic exercises the NewRequest error branch.
func TestCovTrack_InvalidURLNoPanic(t *testing.T) {
	c := &Client{
		baseURL:    "http://example.com/\x7f",
		httpClient: &http.Client{Timeout: time.Second},
	}
	c.Track(context.Background(), "evt", "user", nil)
	time.Sleep(100 * time.Millisecond)
}
