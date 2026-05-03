package service

// BKL-1208: service de assinaturas LinkPay com múltiplos planos (A/B/C).

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"

	"linkpay-service/internal/domain"
	"linkpay-service/internal/repository"
)

// SubscriptionService orquestra criação, renovação e cancelamento de assinaturas LinkPay.
type SubscriptionService struct {
	repo        repository.SubscriptionRepoInterface
	linkPayRepo LinkPayRepoInterface // para buscar produto e arquivos
	asaasClient *AsaasClient
}

func NewSubscriptionService(
	repo repository.SubscriptionRepoInterface,
	linkPayRepo LinkPayRepoInterface,
	asaasClient *AsaasClient,
) *SubscriptionService {
	return &SubscriptionService{
		repo:        repo,
		linkPayRepo: linkPayRepo,
		asaasClient: asaasClient,
	}
}

// --- Planos ---

// CreatePlan cria um novo plano de assinatura para um produto do tenant.
func (s *SubscriptionService) CreatePlan(ctx context.Context, productID, tenantID string, req *domain.CreateSubscriptionPlanRequest) (*domain.DigitalSubscriptionPlan, error) {
	// Valida que o produto pertence ao tenant
	if _, err := s.linkPayRepo.GetProductByID(ctx, productID, tenantID); err != nil {
		return nil, fmt.Errorf("produto não encontrado ou sem permissão")
	}

	now := time.Now()
	features := req.Features
	if features == nil {
		features = []string{}
	}

	plan := &domain.DigitalSubscriptionPlan{
		ID:          uuid.New().String(),
		ProductID:   productID,
		TenantID:    tenantID,
		Name:        req.Name,
		Slug:        req.Slug,
		Description: req.Description,
		PriceCents:  req.PriceCents,
		Interval:    req.Interval,
		TrialDays:   req.TrialDays,
		IsActive:    true,
		SortOrder:   req.SortOrder,
		Features:    features,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := s.repo.CreatePlan(ctx, plan); err != nil {
		return nil, fmt.Errorf("erro ao criar plano: %w", err)
	}
	return plan, nil
}

// ListPlansByProduct retorna os planos ativos de um produto (acesso público).
func (s *SubscriptionService) ListPlansByProduct(ctx context.Context, productID string) ([]domain.DigitalSubscriptionPlan, error) {
	return s.repo.ListPlansByProduct(ctx, productID)
}

// UpdatePlan atualiza campos editáveis de um plano do tenant.
func (s *SubscriptionService) UpdatePlan(ctx context.Context, planID, tenantID string, req *domain.UpdateSubscriptionPlanRequest) (*domain.DigitalSubscriptionPlan, error) {
	plan, err := s.repo.GetPlanByID(ctx, planID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("plano não encontrado")
	}

	if req.Name != "" {
		plan.Name = req.Name
	}
	if req.Description != "" {
		plan.Description = req.Description
	}
	if req.PriceCents > 0 {
		plan.PriceCents = req.PriceCents
	}
	if req.TrialDays > 0 {
		plan.TrialDays = req.TrialDays
	}
	if req.SortOrder > 0 {
		plan.SortOrder = req.SortOrder
	}
	if req.Features != nil {
		plan.Features = req.Features
	}
	if req.IsActive != nil {
		plan.IsActive = *req.IsActive
	}
	plan.UpdatedAt = time.Now()

	if err := s.repo.UpdatePlan(ctx, plan); err != nil {
		return nil, fmt.Errorf("erro ao atualizar plano: %w", err)
	}
	return plan, nil
}

// DeactivatePlan desativa um plano sem deletá-lo.
func (s *SubscriptionService) DeactivatePlan(ctx context.Context, planID, tenantID string) error {
	plan, err := s.repo.GetPlanByID(ctx, planID, tenantID)
	if err != nil {
		return fmt.Errorf("plano não encontrado")
	}
	inactive := false
	plan.IsActive = inactive
	plan.UpdatedAt = time.Now()
	return s.repo.UpdatePlan(ctx, plan)
}

// --- Assinaturas ---

// Subscribe cria uma assinatura recorrente no Asaas e registra no banco.
// BillingType fixo em PIX por ora — cartão requer tokenização adicional.
func (s *SubscriptionService) Subscribe(ctx context.Context, planID string, req *domain.SubscribeRequest) (*domain.DigitalSubscription, error) {
	// 1. Busca plano (sem filtro de tenant — página pública)
	// Usamos ListPlansByProduct via scan direto — GetPlanByID exige tenantID.
	// Busca pelo planID em digital_subscription_plans diretamente via repo público.
	plan, err := s.repo.GetPlanByID(ctx, planID, "") // tenantID vazio = sem filtro
	if err != nil || !plan.IsActive {
		return nil, fmt.Errorf("plano não encontrado ou inativo")
	}

	// 2. Cria customer no Asaas
	asaasCustomer, err := s.asaasClient.CreateCustomer(req.CustomerName, req.CustomerEmail, req.CustomerCPF)
	if err != nil {
		return nil, fmt.Errorf("erro ao criar customer no Asaas: %w", err)
	}

	// 3. Gera access_token (32 bytes = 64 chars hex)
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("erro ao gerar access token: %w", err)
	}
	accessToken := hex.EncodeToString(tokenBytes)

	// 4. Monta registro de assinatura (sem AsaasSubscriptionID ainda)
	now := time.Now()
	periodStart := now
	var periodEnd *time.Time
	if plan.Interval == "yearly" {
		end := now.AddDate(1, 0, 0)
		periodEnd = &end
	} else {
		end := now.AddDate(0, 1, 0)
		periodEnd = &end
	}

	// Período de trial: adiar period_start se trial > 0
	status := "active"
	if plan.TrialDays > 0 {
		status = "trialing"
		trialEnd := now.AddDate(0, 0, plan.TrialDays)
		periodEnd = &trialEnd
	}

	sub := &domain.DigitalSubscription{
		ID:                 uuid.New().String(),
		PlanID:             planID,
		TenantID:           plan.TenantID,
		CustomerEmail:      req.CustomerEmail,
		CustomerName:       req.CustomerName,
		CustomerCPF:        req.CustomerCPF,
		AsaasCustomerID:    asaasCustomer.ID,
		Status:             status,
		CurrentPeriodStart: &periodStart,
		CurrentPeriodEnd:   periodEnd,
		AccessToken:        accessToken,
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	// 5. Persiste antes de chamar Asaas (idempotência: se Asaas falhar, retry pode continuar)
	if err := s.repo.CreateSubscription(ctx, sub); err != nil {
		return nil, fmt.Errorf("erro ao registrar assinatura: %w", err)
	}

	// 6. Cria assinatura no Asaas
	nextDue := now.AddDate(0, 0, 1)
	if plan.TrialDays > 0 {
		nextDue = now.AddDate(0, 0, plan.TrialDays)
	}
	// Sobrescreve NextDueDate via campo do request — asaas_client usa time.Now() interno,
	// então passamos o sub.ID como ExternalReference e ajustamos na requisição direta.
	asaasResp, err := s.asaasClient.createLinkPaySubscription(asaasCustomer.ID, sub.ID, plan, nextDue)
	if err != nil {
		// Fail-open: assinatura registrada no banco, sem ID Asaas ainda.
		// Webhook futuro correlacionará pelo access_token via externalReference.
		log.Printf("[subscription] aviso: falha ao criar no Asaas, sub_id=%s: %v", sub.ID, err)
		sub.Plan = plan
		return sub, nil
	}

	// 7. Atualiza AsaasSubscriptionID
	sub.AsaasSubscriptionID = asaasResp.ID
	if err := s.repo.UpdateSubscriptionStatus(ctx, sub.ID, sub.Status, periodEnd); err != nil {
		log.Printf("[subscription] aviso: falha ao salvar AsaasID sub_id=%s: %v", sub.ID, err)
	}

	sub.Plan = plan
	return sub, nil
}

// RenewSubscription atualiza current_period_end ao receber PAYMENT_CONFIRMED de assinatura.
func (s *SubscriptionService) RenewSubscription(ctx context.Context, asaasSubID string) error {
	sub, err := s.repo.GetSubscriptionByAsaasID(ctx, asaasSubID)
	if err != nil {
		return fmt.Errorf("assinatura não encontrada para asaas_id=%s: %w", asaasSubID, err)
	}

	plan, err := s.repo.GetPlanByID(ctx, sub.PlanID, "")
	if err != nil {
		return fmt.Errorf("plano não encontrado para sub_id=%s: %w", sub.ID, err)
	}

	now := time.Now()
	var newEnd time.Time
	if plan.Interval == "yearly" {
		newEnd = now.AddDate(1, 0, 0)
	} else {
		newEnd = now.AddDate(0, 1, 0)
	}

	return s.repo.UpdateSubscriptionStatus(ctx, sub.ID, "active", &newEnd)
}

// MarkPastDue marca assinatura como past_due (PAYMENT_OVERDUE do Asaas).
func (s *SubscriptionService) MarkPastDue(ctx context.Context, asaasSubID string) error {
	sub, err := s.repo.GetSubscriptionByAsaasID(ctx, asaasSubID)
	if err != nil {
		return fmt.Errorf("assinatura não encontrada para asaas_id=%s: %w", asaasSubID, err)
	}
	return s.repo.UpdateSubscriptionStatus(ctx, sub.ID, "past_due", nil)
}

// CancelSubscription cancela a assinatura no Asaas e no banco.
func (s *SubscriptionService) CancelSubscription(ctx context.Context, token string) error {
	sub, err := s.repo.GetSubscriptionByToken(ctx, token)
	if err != nil {
		return fmt.Errorf("assinatura não encontrada")
	}
	if sub.Status == "cancelled" {
		return fmt.Errorf("assinatura já cancelada")
	}

	// Cancela no Asaas se tiver ID
	if sub.AsaasSubscriptionID != "" {
		if err := s.asaasClient.CancelSubscription(sub.AsaasSubscriptionID); err != nil {
			// Log mas não bloqueia — garante cancelamento local
			log.Printf("[subscription] aviso: falha ao cancelar no Asaas sub_id=%s: %v", sub.ID, err)
		}
	}

	return s.repo.CancelSubscription(ctx, sub.ID)
}

// CancelSubscriptionByAsaasID cancela localmente uma assinatura pelo ID Asaas (usado no webhook SUBSCRIPTION_DELETED).
func (s *SubscriptionService) CancelSubscriptionByAsaasID(ctx context.Context, asaasSubID string) error {
	sub, err := s.repo.GetSubscriptionByAsaasID(ctx, asaasSubID)
	if err != nil {
		return fmt.Errorf("assinatura não encontrada para asaas_id=%s: %w", asaasSubID, err)
	}
	if sub.Status == "cancelled" {
		return nil // idempotente
	}
	return s.repo.CancelSubscription(ctx, sub.ID)
}

// GetSubscription retorna status da assinatura pelo token público.
func (s *SubscriptionService) GetSubscription(ctx context.Context, token string) (*domain.DigitalSubscription, error) {
	sub, err := s.repo.GetSubscriptionByToken(ctx, token)
	if err != nil {
		return nil, fmt.Errorf("assinatura não encontrada")
	}

	plan, err := s.repo.GetPlanByID(ctx, sub.PlanID, "")
	if err == nil {
		sub.Plan = plan
	}
	return sub, nil
}

// GetSubscriberFiles retorna os arquivos do produto acessíveis pelo plano da assinatura.
func (s *SubscriptionService) GetSubscriberFiles(ctx context.Context, token string) ([]domain.SubscriberFile, error) {
	sub, err := s.repo.GetSubscriptionByToken(ctx, token)
	if err != nil {
		return nil, fmt.Errorf("assinatura não encontrada")
	}
	if sub.Status != "active" && sub.Status != "trialing" {
		return nil, fmt.Errorf("assinatura inativa: status=%s", sub.Status)
	}

	plan, err := s.repo.GetPlanByID(ctx, sub.PlanID, "")
	if err != nil {
		return nil, fmt.Errorf("plano não encontrado")
	}

	product, err := s.linkPayRepo.GetProductByID(ctx, plan.ProductID, plan.TenantID)
	if err != nil {
		return nil, fmt.Errorf("produto não encontrado")
	}

	files := []domain.SubscriberFile{}
	if product.StorageKey != "" || product.FileURL != "" {
		downloadURL := product.FileURL
		if product.StorageKey != "" {
			// URL de download via token de assinatura — frontend chama /public/subscriptions/:token/files
			downloadURL = fmt.Sprintf("/api/v1/public/subscriptions/%s/download", token)
		}
		files = append(files, domain.SubscriberFile{
			ProductID:   product.ID,
			ProductName: product.Name,
			DownloadURL: downloadURL,
			FileName:    product.FileName,
			MimeType:    product.MimeType,
		})
	}
	return files, nil
}

// ListSubscriptionsByTenant retorna todas as assinaturas de um tenant (admin).
func (s *SubscriptionService) ListSubscriptionsByTenant(ctx context.Context, tenantID string) ([]domain.DigitalSubscription, error) {
	return s.repo.ListSubscriptionsByTenant(ctx, tenantID)
}
