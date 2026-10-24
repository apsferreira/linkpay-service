package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"linkpay-service/internal/domain"
)

// SubscriptionRepoInterface define os métodos de repositório usados pelo SubscriptionService.
type SubscriptionRepoInterface interface {
	// Planos
	CreatePlan(ctx context.Context, plan *domain.DigitalSubscriptionPlan) error
	GetPlanByID(ctx context.Context, planID, tenantID string) (*domain.DigitalSubscriptionPlan, error)
	ListPlansByProduct(ctx context.Context, productID string) ([]domain.DigitalSubscriptionPlan, error)
	UpdatePlan(ctx context.Context, plan *domain.DigitalSubscriptionPlan) error
	DeactivatePlan(ctx context.Context, planID, tenantID string) error

	// Assinaturas
	CreateSubscription(ctx context.Context, sub *domain.DigitalSubscription) error
	GetSubscriptionByToken(ctx context.Context, token string) (*domain.DigitalSubscription, error)
	GetSubscriptionByAsaasID(ctx context.Context, asaasID string) (*domain.DigitalSubscription, error)
	UpdateSubscriptionStatus(ctx context.Context, id, status string, periodEnd *time.Time) error
	CancelSubscription(ctx context.Context, id string) error
	ListSubscriptionsByTenant(ctx context.Context, tenantID string) ([]domain.DigitalSubscription, error)
	UpdateSubscriptionAsaasID(ctx context.Context, id, asaasID string) error

	// LGPD Art. 18 — direitos do titular (assinante)
	ListSubscriptionsByEmail(ctx context.Context, email string) ([]domain.BuyerSubscriptionExport, error)
	EraseSubscriptionsByEmail(ctx context.Context, email string) (int64, error)
	EraseSubscriptionsOlderThan(ctx context.Context, cutoff time.Time) (int64, error)
}

// SubscriptionRepository implementa SubscriptionRepoInterface usando PostgreSQL.
type SubscriptionRepository struct {
	db *pgxpool.Pool
}

// NewSubscriptionRepository cria um SubscriptionRepository.
func NewSubscriptionRepository(db *pgxpool.Pool) *SubscriptionRepository {
	return &SubscriptionRepository{db: db}
}

// --- Planos ---

// CreatePlan insere um novo plano de assinatura.
func (r *SubscriptionRepository) CreatePlan(ctx context.Context, plan *domain.DigitalSubscriptionPlan) error {
	query := `
		INSERT INTO digital_subscription_plans
			(id, product_id, tenant_id, name, slug, description, price_cents, interval,
			 trial_days, is_active, sort_order, features, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`
	features := plan.Features
	if features == nil {
		features = []string{}
	}
	_, err := r.db.Exec(ctx, query,
		plan.ID, plan.ProductID, plan.TenantID, plan.Name, plan.Slug, plan.Description,
		plan.PriceCents, plan.Interval, plan.TrialDays, plan.IsActive, plan.SortOrder,
		features, plan.CreatedAt, plan.UpdatedAt,
	)
	return err
}

// GetPlanByID busca um plano pelo ID. Se tenantID estiver vazio, não filtra por tenant.
func (r *SubscriptionRepository) GetPlanByID(ctx context.Context, planID, tenantID string) (*domain.DigitalSubscriptionPlan, error) {
	var query string
	var args []interface{}

	if tenantID == "" {
		query = `
			SELECT id, product_id, tenant_id, name, slug, COALESCE(description,''), price_cents,
			       interval, trial_days, is_active, sort_order, COALESCE(features, '{}'), created_at, updated_at
			FROM digital_subscription_plans WHERE id = $1
		`
		args = []interface{}{planID}
	} else {
		query = `
			SELECT id, product_id, tenant_id, name, slug, COALESCE(description,''), price_cents,
			       interval, trial_days, is_active, sort_order, COALESCE(features, '{}'), created_at, updated_at
			FROM digital_subscription_plans WHERE id = $1 AND tenant_id = $2
		`
		args = []interface{}{planID, tenantID}
	}

	row := r.db.QueryRow(ctx, query, args...)
	return scanPlan(row)
}

// ListPlansByProduct lista planos ativos de um produto, ordenados por sort_order.
func (r *SubscriptionRepository) ListPlansByProduct(ctx context.Context, productID string) ([]domain.DigitalSubscriptionPlan, error) {
	query := `
		SELECT id, product_id, tenant_id, name, slug, COALESCE(description,''), price_cents,
		       interval, trial_days, is_active, sort_order, COALESCE(features, '{}'), created_at, updated_at
		FROM digital_subscription_plans
		WHERE product_id = $1 AND is_active = TRUE
		ORDER BY sort_order ASC
	`
	rows, err := r.db.Query(ctx, query, productID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var plans []domain.DigitalSubscriptionPlan
	for rows.Next() {
		p, err := scanPlan(rows)
		if err != nil {
			return nil, err
		}
		plans = append(plans, *p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if plans == nil {
		plans = []domain.DigitalSubscriptionPlan{}
	}
	return plans, nil
}

// UpdatePlan atualiza nome, descrição, preço, trial_days, sort_order, features e is_active.
func (r *SubscriptionRepository) UpdatePlan(ctx context.Context, plan *domain.DigitalSubscriptionPlan) error {
	query := `
		UPDATE digital_subscription_plans
		SET name = $1, description = $2, price_cents = $3, trial_days = $4,
		    sort_order = $5, features = $6, is_active = $7, updated_at = NOW()
		WHERE id = $8 AND tenant_id = $9
	`
	features := plan.Features
	if features == nil {
		features = []string{}
	}
	_, err := r.db.Exec(ctx, query,
		plan.Name, plan.Description, plan.PriceCents, plan.TrialDays,
		plan.SortOrder, features, plan.IsActive, plan.ID, plan.TenantID,
	)
	return err
}

// DeactivatePlan desativa um plano (is_active = false).
func (r *SubscriptionRepository) DeactivatePlan(ctx context.Context, planID, tenantID string) error {
	_, err := r.db.Exec(ctx,
		`UPDATE digital_subscription_plans SET is_active = FALSE, updated_at = NOW() WHERE id = $1 AND tenant_id = $2`,
		planID, tenantID,
	)
	return err
}

// --- Assinaturas ---

// CreateSubscription insere um novo registro de assinatura.
// LGPD Art. 46: o CPF é cifrado (AES-256-GCM) antes de persistir.
func (r *SubscriptionRepository) CreateSubscription(ctx context.Context, sub *domain.DigitalSubscription) error {
	query := `
		INSERT INTO digital_subscriptions
			(id, plan_id, tenant_id, customer_email, customer_name, customer_cpf,
			 asaas_subscription_id, asaas_customer_id, status,
			 current_period_start, current_period_end, access_token, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
	`
	asaasSubID := ""
	if sub.AsaasSubscriptionID != "" {
		asaasSubID = sub.AsaasSubscriptionID
	}
	_, err := r.db.Exec(ctx, query,
		sub.ID, sub.PlanID, sub.TenantID, sub.CustomerEmail, sub.CustomerName, encryptCPF(sub.CustomerCPF, sub.ID),
		asaasSubID, sub.AsaasCustomerID, sub.Status,
		sub.CurrentPeriodStart, sub.CurrentPeriodEnd, sub.AccessToken,
		sub.CreatedAt, sub.UpdatedAt,
	)
	return err
}

// GetSubscriptionByToken busca assinatura pelo access_token.
func (r *SubscriptionRepository) GetSubscriptionByToken(ctx context.Context, token string) (*domain.DigitalSubscription, error) {
	query := `
		SELECT id, plan_id, tenant_id, customer_email, customer_name,
		       COALESCE(asaas_subscription_id,''), COALESCE(asaas_customer_id,''),
		       status, current_period_start, current_period_end, cancelled_at, access_token, created_at, updated_at
		FROM digital_subscriptions WHERE access_token = $1
	`
	row := r.db.QueryRow(ctx, query, token)
	return scanSubscription(row)
}

// GetSubscriptionByAsaasID busca assinatura pelo asaas_subscription_id.
func (r *SubscriptionRepository) GetSubscriptionByAsaasID(ctx context.Context, asaasID string) (*domain.DigitalSubscription, error) {
	query := `
		SELECT id, plan_id, tenant_id, customer_email, customer_name,
		       COALESCE(asaas_subscription_id,''), COALESCE(asaas_customer_id,''),
		       status, current_period_start, current_period_end, cancelled_at, access_token, created_at, updated_at
		FROM digital_subscriptions WHERE asaas_subscription_id = $1
	`
	row := r.db.QueryRow(ctx, query, asaasID)
	return scanSubscription(row)
}

// UpdateSubscriptionStatus atualiza status e current_period_end da assinatura.
func (r *SubscriptionRepository) UpdateSubscriptionStatus(ctx context.Context, id, status string, periodEnd *time.Time) error {
	_, err := r.db.Exec(ctx,
		`UPDATE digital_subscriptions SET status = $1, current_period_end = $2, updated_at = NOW() WHERE id = $3`,
		status, periodEnd, id,
	)
	return err
}

// CancelSubscription marca a assinatura como cancelada.
func (r *SubscriptionRepository) CancelSubscription(ctx context.Context, id string) error {
	_, err := r.db.Exec(ctx,
		`UPDATE digital_subscriptions SET status = 'cancelled', cancelled_at = NOW(), updated_at = NOW() WHERE id = $1`,
		id,
	)
	return err
}

// ListSubscriptionsByTenant lista assinaturas ativas de um tenant.
func (r *SubscriptionRepository) ListSubscriptionsByTenant(ctx context.Context, tenantID string) ([]domain.DigitalSubscription, error) {
	query := `
		SELECT id, plan_id, tenant_id, customer_email, customer_name,
		       COALESCE(asaas_subscription_id,''), COALESCE(asaas_customer_id,''),
		       status, current_period_start, current_period_end, cancelled_at, access_token, created_at, updated_at
		FROM digital_subscriptions WHERE tenant_id = $1 ORDER BY created_at DESC
	`
	rows, err := r.db.Query(ctx, query, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var subs []domain.DigitalSubscription
	for rows.Next() {
		s, err := scanSubscription(rows)
		if err != nil {
			return nil, err
		}
		subs = append(subs, *s)
	}
	if subs == nil {
		subs = []domain.DigitalSubscription{}
	}
	return subs, nil
}

// UpdateSubscriptionAsaasID atualiza o asaas_subscription_id de uma assinatura.
func (r *SubscriptionRepository) UpdateSubscriptionAsaasID(ctx context.Context, id, asaasID string) error {
	_, err := r.db.Exec(ctx,
		`UPDATE digital_subscriptions SET asaas_subscription_id = $1, updated_at = NOW() WHERE id = $2`,
		asaasID, id,
	)
	return err
}

// --- Scanners ---

type planScanner interface {
	Scan(dest ...interface{}) error
}

func scanPlan(row planScanner) (*domain.DigitalSubscriptionPlan, error) {
	var p domain.DigitalSubscriptionPlan
	if err := row.Scan(
		&p.ID, &p.ProductID, &p.TenantID, &p.Name, &p.Slug, &p.Description,
		&p.PriceCents, &p.Interval, &p.TrialDays, &p.IsActive, &p.SortOrder,
		&p.Features, &p.CreatedAt, &p.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("scanPlan: %w", err)
	}
	return &p, nil
}

type subscriptionScanner interface {
	Scan(dest ...interface{}) error
}

func scanSubscription(row subscriptionScanner) (*domain.DigitalSubscription, error) {
	var s domain.DigitalSubscription
	if err := row.Scan(
		&s.ID, &s.PlanID, &s.TenantID, &s.CustomerEmail, &s.CustomerName,
		&s.AsaasSubscriptionID, &s.AsaasCustomerID,
		&s.Status, &s.CurrentPeriodStart, &s.CurrentPeriodEnd, &s.CancelledAt,
		&s.AccessToken, &s.CreatedAt, &s.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("scanSubscription: %w", err)
	}
	return &s, nil
}

// --- LGPD Art. 18 — direitos do titular (assinante) ---

// ListSubscriptionsByEmail retorna a projeção das assinaturas de um titular para
// portabilidade (Art. 18, V). Não expõe tokens internos nem dados de terceiros.
func (r *SubscriptionRepository) ListSubscriptionsByEmail(ctx context.Context, email string) ([]domain.BuyerSubscriptionExport, error) {
	query := `
		SELECT s.id, COALESCE(dp.id::text, ''), COALESCE(dp.name, ''), COALESCE(pl.name, ''),
		       s.status, s.current_period_end, s.cancelled_at, s.created_at
		FROM digital_subscriptions s
		JOIN digital_subscription_plans pl ON pl.id = s.plan_id
		LEFT JOIN digital_products dp ON dp.id = pl.product_id
		WHERE LOWER(s.customer_email) = LOWER($1)
		ORDER BY s.created_at DESC
	`
	rows, err := r.db.Query(ctx, query, email)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var subs []domain.BuyerSubscriptionExport
	for rows.Next() {
		var s domain.BuyerSubscriptionExport
		if err := rows.Scan(
			&s.ID, &s.ProductID, &s.ProductName, &s.PlanName,
			&s.Status, &s.CurrentPeriodEnd, &s.CancelledAt, &s.CreatedAt,
		); err != nil {
			return nil, err
		}
		subs = append(subs, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if subs == nil {
		subs = []domain.BuyerSubscriptionExport{}
	}
	return subs, nil
}

// EraseSubscriptionsByEmail anonimiza os dados pessoais das assinaturas de um
// titular (LGPD Art. 18, VI). O registro da assinatura é mantido (retenção fiscal,
// Art. 16) sem PII.
func (r *SubscriptionRepository) EraseSubscriptionsByEmail(ctx context.Context, email string) (int64, error) {
	tag, err := r.db.Exec(ctx, `
		UPDATE digital_subscriptions
		SET customer_email = NULL, customer_name = NULL, customer_cpf = NULL
		WHERE LOWER(customer_email) = LOWER($1)
	`, email)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// EraseSubscriptionsOlderThan anonimiza PII de assinaturas anteriores ao corte —
// política de retenção de dados do comprador (LGPD Art. 16).
func (r *SubscriptionRepository) EraseSubscriptionsOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := r.db.Exec(ctx, `
		UPDATE digital_subscriptions
		SET customer_email = NULL, customer_name = NULL, customer_cpf = NULL
		WHERE created_at < $1 AND customer_email IS NOT NULL
	`, cutoff)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
