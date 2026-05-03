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

// CreatePlan cria um plano de assinatura para um produto.
// POST /api/v1/linkpay/products/:id/plans
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

// ListPlans lista os planos ativos de um produto (público e admin).
// GET /api/v1/linkpay/products/:id/plans
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

// UpdatePlan atualiza um plano de assinatura.
// PUT /api/v1/linkpay/plans/:plan_id
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

// DeactivatePlan desativa um plano (soft delete).
// DELETE /api/v1/linkpay/plans/:plan_id
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

// Subscribe inicia uma assinatura para um plano.
// POST /api/v1/public/plans/:plan_id/subscribe
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

// GetSubscriptionStatus retorna o status da assinatura e arquivos disponíveis.
// GET /api/v1/public/subscriptions/:token
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

// CancelSubscription cancela a assinatura pelo token público.
// POST /api/v1/public/subscriptions/:token/cancel
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
