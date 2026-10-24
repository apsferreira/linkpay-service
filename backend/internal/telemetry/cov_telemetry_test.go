package telemetry

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
)

func TestCovInit_EndpointConfigured(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "127.0.0.1:1")
	t.Setenv("OTEL_SERVICE_NAME", "cov-service")

	shutdown := Init("fallback-service")
	if shutdown == nil {
		t.Fatal("expected non-nil shutdown function")
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		shutdown()
	}()

	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("shutdown blocked for too long")
	}
}

// TestCovInit_StripsSchemePrefixes covers the http:// and https:// trimming branches.
func TestCovInit_StripsSchemePrefixes(t *testing.T) {
	for _, endpoint := range []string{"http://127.0.0.1:1", "https://127.0.0.1:1"} {
		t.Run(endpoint, func(t *testing.T) {
			t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", endpoint)
			t.Setenv("OTEL_SERVICE_NAME", "")

			shutdown := Init("cov-default-service")
			if shutdown == nil {
				t.Fatal("expected non-nil shutdown function")
			}
			shutdown()
		})
	}
}

// TestCovInit_InvalidEndpoint_NoopShutdown covers the exporter-construction
// failure branch: a control character makes the gRPC target unparseable.
func TestCovInit_InvalidEndpoint_NoopShutdown(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "127.0.0.1:4317\x01")
	t.Setenv("OTEL_SERVICE_NAME", "cov-invalid")

	shutdown := Init("fallback")
	if shutdown == nil {
		t.Fatal("expected non-nil shutdown function")
	}
	shutdown()
}

// TestCovRegisterFiber_ServesMetrics exercises the registered /metrics route
// end to end to make sure the middleware chain is wired.
func TestCovRegisterFiber_ServesMetrics(t *testing.T) {
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	RegisterFiber(app, "cov-metrics-service")
	app.Get("/health", func(c *fiber.Ctx) error { return c.SendString("ok") })

	resp, err := app.Test(httptest.NewRequest("GET", "/health", nil), 5000)
	if err != nil {
		t.Fatalf("app.Test(/health) failed: %v", err)
	}
	_ = resp.Body.Close()

	resp, err = app.Test(httptest.NewRequest("GET", "/metrics", nil), 5000)
	if err != nil {
		t.Fatalf("app.Test(/metrics) failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200 from /metrics, got %d", resp.StatusCode)
	}
}
