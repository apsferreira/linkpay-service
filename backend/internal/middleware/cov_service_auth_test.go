package middleware

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
)

// covUnreachablePool returns a pgxpool.Pool pointing at a closed port so that
// QueryRow fails with a connection error (mimicking an unavailable database).
func covUnreachablePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(),
		"postgres://covuser:covpass@127.0.0.1:1/covdb?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Skipf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestCovServiceAuthMissingHeader(t *testing.T) {
	app := fiber.New()
	app.Use(ServiceAuth(covUnreachablePool(t)))
	app.Get("/", func(c *fiber.Ctx) error { return c.SendString("ok") })

	req := httptest.NewRequest("GET", "/", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestCovServiceAuthUnreachablePool(t *testing.T) {
	app := fiber.New()
	app.Use(ServiceAuth(covUnreachablePool(t)))
	app.Get("/", func(c *fiber.Ctx) error { return c.SendString("ok") })

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Service-Token", "some-service-token")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("expected 401 when the pool is unreachable, got %d", resp.StatusCode)
	}
}
