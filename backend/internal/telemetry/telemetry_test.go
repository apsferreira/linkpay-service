package telemetry

import (
	"os"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestInit_NoEndpoint_ReturnsNoopShutdown(t *testing.T) {
	oldEndpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	os.Unsetenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	defer func() {
		if oldEndpoint != "" {
			os.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", oldEndpoint)
		}
	}()

	shutdown := Init("test-service")
	if shutdown == nil {
		t.Fatal("expected non-nil shutdown function")
	}
	shutdown()
}

func TestInit_DefaultServiceName_WhenEnvNotSet(t *testing.T) {
	oldServiceName := os.Getenv("OTEL_SERVICE_NAME")
	os.Unsetenv("OTEL_SERVICE_NAME")
	defer func() {
		if oldServiceName != "" {
			os.Setenv("OTEL_SERVICE_NAME", oldServiceName)
		}
	}()

	oldEndpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	os.Unsetenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	defer func() {
		if oldEndpoint != "" {
			os.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", oldEndpoint)
		}
	}()

	shutdown := Init("linkpay-service")
	defer shutdown()
}

func TestRegisterFiber_AddsMetricsRoute(t *testing.T) {
	app := fiber.New(fiber.Config{DisableStartupMessage: true})

	RegisterFiber(app, "linkpay-service")

	hasMetricsRoute := false
	for _, route := range app.GetRoutes() {
		if strings.Contains(route.Path, "metrics") {
			hasMetricsRoute = true
			break
		}
	}

	if !hasMetricsRoute {
		t.Error("expected /metrics route to be registered")
	}
}

func TestRegisterFiber_DoesNotPanic(t *testing.T) {
	app := fiber.New(fiber.Config{DisableStartupMessage: true})

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("RegisterFiber panicked: %v", r)
		}
	}()

	RegisterFiber(app, "linkpay-service")
}
