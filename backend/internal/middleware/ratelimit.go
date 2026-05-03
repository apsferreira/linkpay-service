package middleware

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
)

// RateLimiter controla o número de requisições por IP em uma janela de tempo
// usando Redis como backend distribuído (SEC-012).
type RateLimiter struct {
	client *redis.Client
	max    int
	window time.Duration
}

// NewRateLimiter cria um RateLimiter com maxRequests por janela de tempo.
// A conexão Redis é configurada via env var REDIS_URL (padrão: redis://localhost:6379/0).
func NewRateLimiter(maxRequests int, window time.Duration) *RateLimiter {
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "redis://localhost:6379/0"
	}

	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		// BKL-974: substituído panic por log + noop para evitar crash loop no K3s.
		log.Printf("[ratelimit] WARN: URL Redis inválida (%s): %v — rate limiting desabilitado", redisURL, err)
		return &RateLimiter{client: nil, max: maxRequests, window: window}
	}

	return newRateLimiterWithClient(redis.NewClient(opts), maxRequests, window)
}

// newRateLimiterWithClient cria um RateLimiter com um cliente Redis fornecido externamente.
// Usado em testes para injetar um miniredis.
func newRateLimiterWithClient(client *redis.Client, maxRequests int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		client: client,
		max:    maxRequests,
		window: window,
	}
}

// Handler retorna o middleware Fiber para rate limiting distribuído via Redis.
// Usa INCR + EXPIRE: atômico por chave, compatível com múltiplas réplicas.
// BKL-974: se client for nil (Redis inválido no startup), faz pass-through sem limitar.
func (rl *RateLimiter) Handler() fiber.Handler {
	if rl.client == nil {
		return func(c *fiber.Ctx) error { return c.Next() }
	}
	return func(c *fiber.Ctx) error {
		ip := c.IP()
		key := fmt.Sprintf("rl:%s", ip)
		ctx := context.Background()

		count, err := rl.client.Incr(ctx, key).Result()
		if err != nil {
			// Falha no Redis: fail-open para não bloquear o serviço por indisponibilidade de cache
			return c.Next()
		}

		if count == 1 {
			// Primeira requisição na janela — define TTL
			rl.client.Expire(ctx, key, rl.window)
		}

		if count > int64(rl.max) {
			ttl, _ := rl.client.TTL(ctx, key).Result()
			retryAfter := int(ttl.Seconds())
			if retryAfter < 0 {
				retryAfter = int(rl.window.Seconds())
			}
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"error":       "too many requests",
				"retry_after": retryAfter,
			})
		}

		return c.Next()
	}
}
