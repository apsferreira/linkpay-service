package handler

import (
	"context"
	"log"
	"strconv"

	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"

	"linkpay-service/internal/domain"
)

// PayoutServiceInterface define os métodos do service usados pelo handler.
type PayoutServiceInterface interface {
	GetBalance(ctx context.Context, tenantID string) (*domain.Balance, error)
	ListPayouts(ctx context.Context, tenantID string, limit, offset int) (*domain.ListPayoutsResponse, error)
	RequestManualPayout(ctx context.Context, tenantID, pixKey string, amountCents int) (*domain.CreatorPayout, error)
	HandleTransferWebhook(ctx context.Context, event, transferID, description string) error
	PreviewAnticipation(ctx context.Context, tenantID string) (*domain.AnticipationPreview, error)
	RequestAnticipation(ctx context.Context, tenantID, pixKey string) (*domain.CreatorPayout, error)
}

// PayoutHandler gerencia os endpoints de repasse de produtores.
type PayoutHandler struct {
	service   PayoutServiceInterface
	validator *validator.Validate
}

func NewPayoutHandler(service PayoutServiceInterface) *PayoutHandler {
	return &PayoutHandler{
		service:   service,
		validator: validator.New(),
	}
}

// GetBalance godoc
// GET /api/v1/admin/linkpay/balance
// Retorna saldo disponível, retido e próxima data de repasse do tenant autenticado.
func (h *PayoutHandler) GetBalance(c *fiber.Ctx) error {
	tenantID, err := extractTenantID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "tenant não identificado"})
	}

	balance, err := h.service.GetBalance(c.Context(), tenantID.String())
	if err != nil {
		log.Printf("[payout] erro ao buscar saldo tenant %s: %v", tenantID, err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "erro ao buscar saldo"})
	}

	return c.JSON(balance)
}

// ListPayouts godoc
// GET /api/v1/admin/linkpay/payouts?page=1&limit=20
// Lista repasses do tenant autenticado com paginação.
func (h *PayoutHandler) ListPayouts(c *fiber.Ctx) error {
	tenantID, err := extractTenantID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "tenant não identificado"})
	}

	limit, _ := strconv.Atoi(c.Query("limit", "20"))
	page, _ := strconv.Atoi(c.Query("page", "1"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if page <= 0 {
		page = 1
	}
	offset := (page - 1) * limit

	resp, err := h.service.ListPayouts(c.Context(), tenantID.String(), limit, offset)
	if err != nil {
		log.Printf("[payout] erro ao listar payouts tenant %s: %v", tenantID, err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "erro ao listar repasses"})
	}

	return c.JSON(resp)
}

// RequestPayout godoc
// POST /api/v1/admin/linkpay/payouts/request
// Solicita saque manual do saldo disponível.
// Body: {"pix_key":"...","amount_cents":0} — amount_cents=0 usa saldo total disponível.
func (h *PayoutHandler) RequestPayout(c *fiber.Ctx) error {
	tenantID, err := extractTenantID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "tenant não identificado"})
	}

	var body domain.PayoutRequestBody
	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "payload inválido"})
	}
	if err := h.validator.Struct(&body); err != nil {
		return c.Status(fiber.StatusUnprocessableEntity).JSON(fiber.Map{"error": err.Error()})
	}

	payout, err := h.service.RequestManualPayout(c.Context(), tenantID.String(), body.PixKey, body.AmountCents)
	if err != nil {
		log.Printf("[payout] erro no saque manual tenant %s: %v", tenantID, err)
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(fiber.StatusCreated).JSON(payout)
}

// GetAnticipationPreview godoc
// GET /api/v1/admin/linkpay/anticipation/preview
// Retorna o preview de antecipação de recebíveis de cartão para o tenant autenticado.
// 204 se não houver compras elegíveis.
func (h *PayoutHandler) GetAnticipationPreview(c *fiber.Ctx) error {
	tenantID, err := extractTenantID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "tenant não identificado"})
	}

	preview, err := h.service.PreviewAnticipation(c.Context(), tenantID.String())
	if err != nil {
		log.Printf("[payout] erro ao calcular preview de antecipação tenant %s: %v", tenantID, err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "erro ao calcular antecipação"})
	}
	if preview == nil {
		return c.SendStatus(fiber.StatusNoContent)
	}

	return c.JSON(preview)
}

// RequestAnticipation godoc
// POST /api/v1/admin/linkpay/anticipation/request
// Solicita antecipação de recebíveis de cartão com taxa de 2%, liberação em D+2.
// Body: {"pix_key":"email@exemplo.com"} — pix_key opcional se já cadastrada no tenant.
func (h *PayoutHandler) RequestAnticipation(c *fiber.Ctx) error {
	tenantID, err := extractTenantID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "tenant não identificado"})
	}

	var body domain.AnticipationRequestBody
	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "payload inválido"})
	}
	if err := h.validator.Struct(&body); err != nil {
		return c.Status(fiber.StatusUnprocessableEntity).JSON(fiber.Map{"error": err.Error()})
	}

	payout, err := h.service.RequestAnticipation(c.Context(), tenantID.String(), body.PixKey)
	if err != nil {
		log.Printf("[payout] erro na antecipação tenant %s: %v", tenantID, err)
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(fiber.StatusCreated).JSON(payout)
}
