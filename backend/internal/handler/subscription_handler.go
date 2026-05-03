package handler

// BKL-1208: handler de planos e assinaturas LinkPay.

import (
	"context"
	"log"

	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"

	"linkpay-service/internal/domain"
)

// SubscriptionServiceInterface define os métodos do service usados pelo handler.
type SubscriptionServiceInterface interface {
	CreatePlan(ctx context.Context, productID, tenantID string, req *domain.CreateSubscriptionPlanRequest) (*domain.DigitalSubscriptionPlan, error)
	ListPlansByProduct(ctx context.Context, productID string) ([]domain.DigitalSubscriptionPlan, error)
	UpdatePlan(ctx context.Context, planID, tenantID string, req *domain.UpdateSubscriptionPlanRequest) (*domain.DigitalSubscriptionPlan, error)
	DeactivatePlan(ctx context.Context, planID, tenantID string) error
	Subscribe(ctx context.Context, planID string, req *domain.SubscribeRequest) (*domain.DigitalSubscription, error)
	CancelSubscription(ctx context.Context, token string) error
	CancelSubscriptionByAsaasID(ctx context.Context, asaasSubID string) error
	GetSubscription(ctx context.Context, token string) (*domain.DigitalSubscription, error)
	GetSubscriberFiles(ctx context.Context, token string) ([]domain.SubscriberFile, error)
	ListSubscriptionsByTenant(ctx context.Context, tenantID string) ([]domain.DigitalSubscription, error)
	RenewSubscription(ctx context.Context, asaasSubID string) error
	MarkPastDue(ctx context.Context, asaasSubID string) error
}

// SubscriptionHandler gerencia os endpoints de planos e assinaturas.
type SubscriptionHandler struct {
	service   SubscriptionServiceInterface
	validator *validator.Validate
}

func NewSubscriptionHandler(service SubscriptionServiceInterface) *SubscriptionHandler {
	return &SubscriptionHandler{
		service:   service,
		validator: validator.New(),
	}
}

// --- Admin / Produtor (autenticado) ---

// CreatePlan godoc
//
//	@Summary      Cria plano de assinatura
//	@Description  Cria um plano de assinatura recorrente para um produto do tenant autenticado.
//	@Tags         planos
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id    path      string                                 true  "ID do produto (UUID)"
//	@Param        body  body      domain.CreateSubscriptionPlanRequest   true  "Dados do plano"
//	@Success      201   {object}  domain.DigitalSubscriptionPlan
//	@Failure      400   {object}  map[string]string
//	@Failure      401   {object}  map[string]string
//	@Router       /api/v1/linkpay/products/{id}/plans [post]
func (h *SubscriptionHandler) CreatePlan(c *fiber.Ctx) error {
	tenantID, err := extractTenantID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "tenant não identificado"})
	}

	productID := c.Params("id")
	if productID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "id do produto obrigatório"})
	}

	var req domain.CreateSubscriptionPlanRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "corpo da requisição inválido"})
	}
	if err := h.validator.Struct(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "validação falhou", "details": err.Error()})
	}

	plan, err := h.service.CreatePlan(c.Context(), productID, tenantID.String(), &req)
	if err != nil {
		log.Printf("[subscription] erro ao criar plano: %v", err)
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(fiber.StatusCreated).JSON(plan)
}

// ListPlans godoc
//
//	@Summary      Lista planos do produto
//	@Description  Lista os planos de assinatura ativos de um produto.
//	@Tags         planos
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id  path      string  true  "ID do produto (UUID)"
//	@Success      200  {object}  map[string][]domain.DigitalSubscriptionPlan
//	@Failure      400  {object}  map[string]string
//	@Failure      500  {object}  map[string]string
//	@Router       /api/v1/linkpay/products/{id}/plans [get]
func (h *SubscriptionHandler) ListPlans(c *fiber.Ctx) error {
	productID := c.Params("id")
	if productID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "id do produto obrigatório"})
	}

	plans, err := h.service.ListPlansByProduct(c.Context(), productID)
	if err != nil {
		log.Printf("[subscription] erro ao listar planos: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "erro ao listar planos"})
	}

	return c.JSON(fiber.Map{"plans": plans})
}

// UpdatePlan godoc
//
//	@Summary      Atualiza plano de assinatura
//	@Description  Atualiza nome, preço, trial ou features de um plano do tenant autenticado.
//	@Tags         planos
//	@Accept       json
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id       path      string                               true  "ID do produto (UUID)"
//	@Param        plan_id  path      string                               true  "ID do plano (UUID)"
//	@Param        body     body      domain.UpdateSubscriptionPlanRequest  true  "Campos a atualizar"
//	@Success      200      {object}  domain.DigitalSubscriptionPlan
//	@Failure      400      {object}  map[string]string
//	@Failure      401      {object}  map[string]string
//	@Router       /api/v1/linkpay/products/{id}/plans/{plan_id} [put]
func (h *SubscriptionHandler) UpdatePlan(c *fiber.Ctx) error {
	tenantID, err := extractTenantID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "tenant não identificado"})
	}

	planID := c.Params("plan_id")
	if planID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "plan_id obrigatório"})
	}

	var req domain.UpdateSubscriptionPlanRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "corpo da requisição inválido"})
	}
	if err := h.validator.Struct(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "validação falhou", "details": err.Error()})
	}

	plan, err := h.service.UpdatePlan(c.Context(), planID, tenantID.String(), &req)
	if err != nil {
		log.Printf("[subscription] erro ao atualizar plano: %v", err)
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(plan)
}

// DeactivatePlan godoc
//
//	@Summary      Desativa plano de assinatura
//	@Description  Desativa (soft delete) um plano de assinatura do tenant autenticado.
//	@Tags         planos
//	@Produce      json
//	@Security     BearerAuth
//	@Param        id       path      string  true  "ID do produto (UUID)"
//	@Param        plan_id  path      string  true  "ID do plano (UUID)"
//	@Success      200      {object}  map[string]string
//	@Failure      400      {object}  map[string]string
//	@Failure      401      {object}  map[string]string
//	@Router       /api/v1/linkpay/products/{id}/plans/{plan_id} [delete]
func (h *SubscriptionHandler) DeactivatePlan(c *fiber.Ctx) error {
	tenantID, err := extractTenantID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "tenant não identificado"})
	}

	planID := c.Params("plan_id")
	if planID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "plan_id obrigatório"})
	}

	if err := h.service.DeactivatePlan(c.Context(), planID, tenantID.String()); err != nil {
		log.Printf("[subscription] erro ao desativar plano: %v", err)
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"message": "plano desativado"})
}

// ListAdminSubscriptions lista todos os assinantes do tenant autenticado.
// GET /api/v1/admin/linkpay/subscriptions
func (h *SubscriptionHandler) ListAdminSubscriptions(c *fiber.Ctx) error {
	tenantID, err := extractTenantID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "tenant não identificado"})
	}

	subs, err := h.service.ListSubscriptionsByTenant(c.Context(), tenantID.String())
	if err != nil {
		log.Printf("[subscription] erro ao listar assinaturas: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "erro ao listar assinaturas"})
	}

	return c.JSON(fiber.Map{"subscriptions": subs})
}

// --- Público (assinante) ---

// Subscribe godoc
//
//	@Summary      Inicia assinatura
//	@Description  Cria uma assinatura recorrente no Asaas para um plano de produto.
//	@Tags         público
//	@Accept       json
//	@Produce      json
//	@Param        id    path      string                    true  "ID do produto (UUID)"
//	@Param        body  body      domain.SubscribeRequest   true  "Dados do assinante"
//	@Success      201   {object}  domain.DigitalSubscription
//	@Failure      400   {object}  map[string]string
//	@Router       /api/v1/public/products/{id}/subscribe [post]
func (h *SubscriptionHandler) Subscribe(c *fiber.Ctx) error {
	planID := c.Params("plan_id")
	if planID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "plan_id obrigatório"})
	}

	var req domain.SubscribeRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "corpo da requisição inválido"})
	}
	if err := h.validator.Struct(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "validação falhou", "details": err.Error()})
	}

	sub, err := h.service.Subscribe(c.Context(), planID, &req)
	if err != nil {
		log.Printf("[subscription] erro ao assinar plano=%s: %v", planID, err)
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(fiber.StatusCreated).JSON(sub)
}

// GetSubscriptionStatus godoc
//
//	@Summary      Status da assinatura
//	@Description  Retorna o status da assinatura e os arquivos disponíveis para o assinante.
//	@Tags         público
//	@Produce      json
//	@Param        token  path      string  true  "Token da assinatura"
//	@Success      200    {object}  map[string]interface{}
//	@Failure      400    {object}  map[string]string
//	@Failure      404    {object}  map[string]string
//	@Router       /api/v1/public/subscriptions/{token} [get]
func (h *SubscriptionHandler) GetSubscriptionStatus(c *fiber.Ctx) error {
	token := c.Params("token")
	if token == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "token obrigatório"})
	}

	sub, err := h.service.GetSubscription(c.Context(), token)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "assinatura não encontrada"})
	}

	files, _ := h.service.GetSubscriberFiles(c.Context(), token)
	if files == nil {
		files = []domain.SubscriberFile{}
	}

	return c.JSON(fiber.Map{
		"subscription": sub,
		"files":        files,
	})
}

// CancelSubscription godoc
//
//	@Summary      Cancela assinatura
//	@Description  Cancela a assinatura pelo token público do assinante.
//	@Tags         público
//	@Produce      json
//	@Param        token  path      string  true  "Token da assinatura"
//	@Success      200    {object}  map[string]string
//	@Failure      400    {object}  map[string]string
//	@Router       /api/v1/public/subscriptions/{token}/cancel [post]
func (h *SubscriptionHandler) CancelSubscription(c *fiber.Ctx) error {
	token := c.Params("token")
	if token == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "token obrigatório"})
	}

	if err := h.service.CancelSubscription(c.Context(), token); err != nil {
		log.Printf("[subscription] erro ao cancelar assinatura: %v", err)
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{"message": "assinatura cancelada"})
}
