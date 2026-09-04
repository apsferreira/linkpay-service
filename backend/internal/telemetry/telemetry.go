// Package telemetry sets up OpenTelemetry tracing and Prometheus metrics for Fiber apps.
//
// Environment variables:
//   OTEL_EXPORTER_OTLP_ENDPOINT  e.g. obs-otel-collector:4317 (gRPC: host:port only, no scheme)
//   OTEL_SERVICE_NAME             e.g. linkpay-service
//
// When OTEL_EXPORTER_OTLP_ENDPOINT is not set, a no-op tracer is used so the
// app still starts cleanly in local development without a collector running.
package telemetry

import (
	"context"
	"log"
	"os"
	"strings"

	fiberprometheus "github.com/ansrivas/fiberprometheus/v2"
	otelfiber "github.com/gofiber/contrib/otelfiber/v2"
	"github.com/gofiber/fiber/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
)

// Init configures the global OpenTelemetry TracerProvider.
// Returns a shutdown function that must be deferred by the caller.
func Init(defaultServiceName string) func() {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	serviceName := os.Getenv("OTEL_SERVICE_NAME")
	if serviceName == "" {
		serviceName = defaultServiceName
	}

	if endpoint == "" {
		log.Println("[telemetry] OTEL_EXPORTER_OTLP_ENDPOINT not set — tracing disabled")
		return func() {}
	}

	endpoint = strings.TrimPrefix(endpoint, "https://")
	endpoint = strings.TrimPrefix(endpoint, "http://")

	ctx := context.Background()

	res, err := resource.New(ctx,
		resource.WithAttributes(semconv.ServiceName(serviceName)),
	)
	if err != nil {
		log.Printf("[telemetry] Resource creation failed: %v — tracing disabled", err)
		return func() {}
	}

	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		log.Printf("[telemetry] OTLP exporter failed: %v — tracing disabled", err)
		return func() {}
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(provider)

	log.Printf("[telemetry] Tracing configured — service=%s endpoint=%s", serviceName, endpoint)

	return func() {
		if err := provider.Shutdown(context.Background()); err != nil {
			log.Printf("[telemetry] Shutdown error: %v", err)
		}
	}
}

// RegisterFiber adds OTel tracing middleware and Prometheus /metrics to the Fiber app.
// Call this before registering application routes.
func RegisterFiber(app *fiber.App, serviceName string) {
	app.Use(otelfiber.Middleware())

	prom := fiberprometheus.New(serviceName)
	prom.RegisterAt(app, "/metrics")
	app.Use(prom.Middleware)
}
