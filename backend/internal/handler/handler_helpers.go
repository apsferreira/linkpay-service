package handler

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// extractTenantID extrai o tenant_id do contexto JWT, definido pelo middleware de autenticação.
func extractTenantID(c *fiber.Ctx) (uuid.UUID, error) {
	raw := c.Locals("tenant_id")
	if raw == nil {
		return uuid.Nil, fmt.Errorf("tenant_id ausente no contexto")
	}
	switch v := raw.(type) {
	case uuid.UUID:
		return v, nil
	case string:
		id, err := uuid.Parse(v)
		if err != nil {
			return uuid.Nil, fmt.Errorf("tenant_id inválido: %w", err)
		}
		return id, nil
	default:
		return uuid.Nil, fmt.Errorf("tenant_id em formato inesperado")
	}
}

// --- Aliases para compatibilidade com main.go ---

// DownloadProduct é alias para Download.
func (h *LinkPayHandler) DownloadProduct(c *fiber.Ctx) error {
	return h.Download(c)
}

// DownloadProductFile é alias para DownloadFile.
func (h *LinkPayHandler) DownloadProductFile(c *fiber.Ctx) error {
	return h.DownloadFile(c)
}

// DownloadAllFilesZip é alias para DownloadZip.
func (h *LinkPayHandler) DownloadAllFilesZip(c *fiber.Ctx) error {
	return h.DownloadZip(c)
}

// GetProduct godoc
//
//	@Summary      Retorna produto por ID
//	@Description  Retorna um produto digital pelo ID para o tenant autenticado.
//	@Tags         produtos
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id  path      string  true  "ID do produto (UUID)"
//	@Success      200  {object}  domain.DigitalProduct
//	@Failure      400  {object}  map[string]string
//	@Failure      401  {object}  map[string]string
//	@Failure      404  {object}  map[string]string
//	@Router       /api/v1/linkpay/products/{id} [get]
func (h *LinkPayHandler) GetProduct(c *fiber.Ctx) error {
	tenantID, err := extractTenantID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "tenant não identificado"})
	}

	id := c.Params("id")
	if id == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "id obrigatório"})
	}

	product, err := h.service.GetProductByID(c.Context(), id, tenantID.String())
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "produto não encontrado"})
	}

	return c.JSON(product)
}

// DeactivateProduct é alias para DeleteProduct.
func (h *LinkPayHandler) DeactivateProduct(c *fiber.Ctx) error {
	return h.DeleteProduct(c)
}

// GetStats é alias para GetSalesStats.
func (h *LinkPayHandler) GetStats(c *fiber.Ctx) error {
	return h.GetSalesStats(c)
}
