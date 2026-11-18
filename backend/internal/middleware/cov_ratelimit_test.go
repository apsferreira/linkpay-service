package middleware

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
)

// covRLApp builds an app guarded by rl.Handler().
func covRLApp(rl *RateLimiter) *fiber.App {
	app := fiber.New()
	app.Use(rl.Handler())
	app.Get("/", func(c *fiber.Ctx) error { return c.SendString("ok") })
	return app
}

// covRLDo performs one request and returns status code plus decoded JSON body (if any).
func covRLDo(t *testing.T, app *fiber.App) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest("GET", "/", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	body := map[string]any{}
	if strings.Contains(resp.Header.Get("Content-Type"), "json") {
		_ = json.NewDecoder(resp.Body).Decode(&body)
	}
	return resp.StatusCode, body
}

// covNewMiniRedis starts a miniredis instance and returns it plus a client for it.
func covNewMiniRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	srv, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run(): %v", err)
	}
	client := redis.NewClient(&redis.Options{Addr: srv.Addr()})
	t.Cleanup(func() {
		_ = client.Close()
	})
	return srv, client
}

func TestCovNewRateLimiterInvalidURL(t *testing.T) {
	t.Setenv("REDIS_URL", "http://not-a-redis-url")

	rl := NewRateLimiter(1, time.Minute)
	if rl == nil {
		t.Fatal("NewRateLimiter returned nil")
	}
	if rl.client != nil {
		t.Fatalf("expected nil client for invalid REDIS_URL, got %v", rl.client)
	}
	if rl.max != 1 || rl.window != time.Minute {
		t.Fatalf("unexpected config: max=%d window=%s", rl.max, rl.window)
	}

	// Handler must be a pass-through when the client is nil.
	app := covRLApp(rl)
	for i := 1; i <= 3; i++ {
		if status, _ := covRLDo(t, app); status != fiber.StatusOK {
			t.Fatalf("request %d: expected 200 pass-through, got %d", i, status)
		}
	}
}

func TestCovNewRateLimiterDefaultURL(t *testing.T) {
	t.Setenv("REDIS_URL", "")

	rl := NewRateLimiter(5, time.Minute)
	if rl == nil {
		t.Fatal("NewRateLimiter returned nil")
	}
	if rl.client == nil {
		t.Fatal("expected a redis client for the default REDIS_URL")
	}
	if rl.max != 5 || rl.window != time.Minute {
		t.Fatalf("unexpected config: max=%d window=%s", rl.max, rl.window)
	}
	if err := rl.client.Close(); err != nil {
		t.Fatalf("closing client: %v", err)
	}
}

func TestCovRateLimiterNilClientPassThrough(t *testing.T) {
	rl := newRateLimiterWithClient(nil, 1, time.Minute)
	if rl.client != nil {
		t.Fatal("expected nil client")
	}

	app := covRLApp(rl)
	for i := 1; i <= 3; i++ {
		if status, _ := covRLDo(t, app); status != fiber.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i, status)
		}
	}
}

func TestCovRateLimiterAllowsUntilMaxThenBlocks(t *testing.T) {
	srv, client := covNewMiniRedis(t)
	defer srv.Close()

	rl := newRateLimiterWithClient(client, 2, time.Minute)
	app := covRLApp(rl)

	// First request: also exercises the count == 1 branch that sets the TTL.
	if status, _ := covRLDo(t, app); status != fiber.StatusOK {
		t.Fatalf("request 1: expected 200, got %d", status)
	}

	keys := srv.Keys()
	if len(keys) != 1 {
		t.Fatalf("expected exactly 1 redis key, got %v", keys)
	}
	key := keys[0]
	if !strings.HasPrefix(key, "rl:") {
		t.Errorf("key %q should be prefixed with rl:", key)
	}
	if ttl := srv.TTL(key); ttl <= 0 {
		t.Errorf("expected TTL to be applied on first request, got %s", ttl)
	}

	// Second request: still allowed.
	if status, _ := covRLDo(t, app); status != fiber.StatusOK {
		t.Fatalf("request 2: expected 200, got %d", status)
	}

	// Third request exceeds max=2 and must be throttled.
	status, body := covRLDo(t, app)
	if status != fiber.StatusTooManyRequests {
		t.Fatalf("request 3: expected 429, got %d", status)
	}
	if body["error"] != "too many requests" {
		t.Errorf("unexpected error message %v", body["error"])
	}
	retryAfter, ok := body["retry_after"].(float64)
	if !ok {
		t.Fatalf("retry_after missing or not numeric: %v", body["retry_after"])
	}
	if retryAfter < 1 || retryAfter > 60 {
		t.Errorf("retry_after = %v, want between 1 and 60", retryAfter)
	}

	// Counter must have been incremented in Redis (INCR semantics).
	val, err := client.Get(context.Background(), key).Result()
	if err != nil {
		t.Fatalf("reading counter: %v", err)
	}
	if val != "3" {
		t.Errorf("counter = %s, want 3", val)
	}
}

func TestCovRateLimiterSingleRequestLimit(t *testing.T) {
	srv, client := covNewMiniRedis(t)
	defer srv.Close()

	rl := newRateLimiterWithClient(client, 1, 30*time.Second)
	app := covRLApp(rl)

	if status, _ := covRLDo(t, app); status != fiber.StatusOK {
		t.Fatalf("request 1: expected 200, got %d", status)
	}
	status, body := covRLDo(t, app)
	if status != fiber.StatusTooManyRequests {
		t.Fatalf("request 2: expected 429, got %d", status)
	}
	retryAfter, _ := body["retry_after"].(float64)
	if retryAfter < 1 || retryAfter > 30 {
		t.Errorf("retry_after = %v, want between 1 and 30", retryAfter)
	}
}

func TestCovRateLimiterFailOpenWhenRedisClosed(t *testing.T) {
	srv, client := covNewMiniRedis(t)

	rl := newRateLimiterWithClient(client, 1, time.Minute)
	app := covRLApp(rl)

	// Sanity: limiter works while redis is up.
	if status, _ := covRLDo(t, app); status != fiber.StatusOK {
		t.Fatalf("request 1: expected 200, got %d", status)
	}

	srv.Close()

	if status, _ := covRLDo(t, app); status != fiber.StatusOK {
		t.Fatalf("closed redis: expected fail-open 200, got %d", status)
	}
}
