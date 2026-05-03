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
//
//	@Summary      Saldo do produtor
//	@Description  Retorna saldo disponível, retido, em reserva e próxima data de repasse do tenant autenticado.
//	@Tags         admin
//	@Produce      json
//	@Security     BearerAuth
//	@Success      200  {object}  domain.Balance
//	@Failure      401  {object}  map[string]string
//	@Failure      500  {object}  map[string]string
//	@Router       /api/v1/admin/linkpay/balance [get]
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
//
//	@Summary      Lista repasses
//	@Description  Lista repasses do tenant autenticado com paginação (page, limit).
//	@Tags         admin
//	@Produce      json
//	@Security     BearerAuth
//	@Param        page   query     int  false  "Página (default 1)"
//	@Param        limit  query     int  false  "Itens por página, max 100 (default 20)"
//	@Success      200    {object}  domain.ListPayoutsResponse
//	@Failure      401    {object}  map[string]string
//	@Failure      500    {object}  map[string]string
//	@Router       /api/v1/admin/linkpay/payouts [get]
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
//
//	@Summary      Solicita saque manual
//	@Description  Solicita transferência PIX do saldo disponível. amount_cents=0 usa saldo total.
//	@Tags         admin
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        body  body      domain.PayoutRequestBody  true  "Chave PIX e valor"
//	@Success      201   {object}  domain.CreatorPayout
//	@Failure      400   {object}  map[string]string
//	@Failure      401   {object}  map[string]string
//	@Failure      422   {object}  map[string]string
//	@Router       /api/v1/admin/linkpay/payouts/request [post]
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
//
//	@Summary      Preview de antecipação
//	@Description  Retorna o preview de antecipação de recebíveis de cartão (taxa 2%, D+2). 204 se não houver elegíveis.
//	@Tags         admin
//	@Produce      json
//	@Security     BearerAuth
//	@Success      200  {object}  domain.AnticipationPreview
//	@Success      204
//	@Failure      401  {object}  map[string]string
//	@Failure      500  {object}  map[string]string
//	@Router       /api/v1/admin/linkpay/anticipation/preview [get]
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
//
//	@Summary      Solicita antecipação
//	@Description  Solicita antecipação de recebíveis de cartão com taxa de 2%, liberação em D+2.
//	@Tags         admin
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        body  body      domain.AnticipationRequestBody  true  "Chave PIX (opcional se já cadastrada)"
//	@Success      201   {object}  domain.CreatorPayout
//	@Failure      400   {object}  map[string]string
//	@Failure      401   {object}  map[string]string
//	@Failure      422   {object}  map[string]string
//	@Router       /api/v1/admin/linkpay/anticipation/request [post]
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
