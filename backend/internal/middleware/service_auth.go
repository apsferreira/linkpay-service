package middleware

import (
	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ServiceAuth valida o header X-Service-Token para requisições service-to-service.
// Alternativa ao JWT para comunicação interna entre serviços IIT e clientes B2B
// externos cadastrados na tabela service_clients (Modelo A — Proxy API).
func ServiceAuth(db *pgxpool.Pool) fiber.Handler {
	return func(c *fiber.Ctx) error {
		token := c.Get("X-Service-Token")
		if token == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "X-Service-Token obrigatório",
			})
		}

		var (
			clientName    string
			termsAccepted bool
			isActive      bool
		)

		err := db.QueryRow(
			context.Background(),
			`SELECT name, terms_accepted, is_active
			   FROM service_clients
			  WHERE service_token = $1`,
			token,
		).Scan(&clientName, &termsAccepted, &isActive)

		if err != nil {
			// token não encontrado ou erro de banco — não expor detalhes
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "token inválido ou não autorizado",
			})
		}

		if !isActive {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "cliente desativado",
			})
		}

		if !termsAccepted {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "termos de uso B2B não aceitos — consulte docs/terms-of-service-b2b.md",
			})
		}

		c.Locals("service_client_name", clientName)
		return c.Next()
	}
}
