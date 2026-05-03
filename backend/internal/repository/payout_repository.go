package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"linkpay-service/internal/domain"
)

// PayoutRepository gerencia creator_payouts e cálculo de saldo disponível.
type PayoutRepository struct {
	db *pgxpool.Pool
}

func NewPayoutRepository(db *pgxpool.Pool) *PayoutRepository {
	return &PayoutRepository{db: db}
}

// EligiblePurchase representa uma compra com holdback expirado ainda não repassada.
type EligiblePurchase struct {
	ID              string
	CreatorEarnings int
}

// GetEligiblePurchases retorna compras confirmadas com holdback expirado e sem payout_id.
// Fase 1: libera 90% do earnings (reserva 10% globalmente).
func (r *PayoutRepository) GetEligiblePurchases(ctx context.Context, tenantID string) ([]EligiblePurchase, error) {
	query := `
		SELECT id, creator_earnings_cents
		FROM digital_purchases
		WHERE tenant_id = $1
		  AND payment_status = 'confirmed'
		  AND payout_id IS NULL
		  AND (
		    (payment_method = 'pix'    AND created_at <= NOW() - INTERVAL '2 days') OR
		    (payment_method = 'boleto' AND created_at <= NOW() - INTERVAL '2 days') OR
		    (payment_method = 'credit_card' AND created_at <= NOW() - INTERVAL '15 days')
		  )
		ORDER BY created_at ASC
	`
	rows, err := r.db.Query(ctx, query, tenantID)
	if err != nil {
		return nil, fmt.Errorf("erro ao buscar compras elegíveis: %w", err)
	}
	defer rows.Close()

	var purchases []EligiblePurchase
	for rows.Next() {
		var ep EligiblePurchase
		if err := rows.Scan(&ep.ID, &ep.CreatorEarnings); err != nil {
			return nil, fmt.Errorf("erro ao escanear compra elegível: %w", err)
		}
		purchases = append(purchases, ep)
	}
	return purchases, rows.Err()
}

// GetBalance calcula o saldo disponível, retido e reservado de um tenant.
func (r *PayoutRepository) GetBalance(ctx context.Context, tenantID string) (*domain.Balance, error) {
	// Total confirmado (histórico)
	totalQuery := `
		SELECT COALESCE(SUM(creator_earnings_cents), 0)
		FROM digital_purchases
		WHERE tenant_id = $1 AND payment_status = 'confirmed'
	`
	var totalCents int
	if err := r.db.QueryRow(ctx, totalQuery, tenantID).Scan(&totalCents); err != nil {
		return nil, fmt.Errorf("erro ao buscar total: %w", err)
	}

	// Em holdback (confirmed mas prazo não expirou)
	heldQuery := `
		SELECT COALESCE(SUM(creator_earnings_cents), 0)
		FROM digital_purchases
		WHERE tenant_id = $1
		  AND payment_status = 'confirmed'
		  AND payout_id IS NULL
		  AND NOT (
		    (payment_method = 'pix'    AND created_at <= NOW() - INTERVAL '2 days') OR
		    (payment_method = 'boleto' AND created_at <= NOW() - INTERVAL '2 days') OR
		    (payment_method = 'credit_card' AND created_at <= NOW() - INTERVAL '15 days')
		  )
	`
	var heldCents int
	if err := r.db.QueryRow(ctx, heldQuery, tenantID).Scan(&heldCents); err != nil {
		return nil, fmt.Errorf("erro ao buscar held: %w", err)
	}

	// Disponível bruto (holdback expirado, sem payout ainda)
	availableGrossQuery := `
		SELECT COALESCE(SUM(creator_earnings_cents), 0)
		FROM digital_purchases
		WHERE tenant_id = $1
		  AND payment_status = 'confirmed'
		  AND payout_id IS NULL
		  AND (
		    (payment_method = 'pix'    AND created_at <= NOW() - INTERVAL '2 days') OR
		    (payment_method = 'boleto' AND created_at <= NOW() - INTERVAL '2 days') OR
		    (payment_method = 'credit_card' AND created_at <= NOW() - INTERVAL '15 days')
		  )
	`
	var availableGross int
	if err := r.db.QueryRow(ctx, availableGrossQuery, tenantID).Scan(&availableGross); err != nil {
		return nil, fmt.Errorf("erro ao buscar disponível: %w", err)
	}

	// Reserva anti-chargeback: 10% do disponível bruto (Fase 1 — simplificado)
	reserveCents := availableGross * domain.HoldbackReservePct / 100
	availableCents := availableGross - reserveCents

	// Próxima data de repasse: próxima segunda-feira
	nextPayout := nextMonday(time.Now())

	return &domain.Balance{
		AvailableCents: availableCents,
		HeldCents:      heldCents,
		ReserveCents:   reserveCents,
		TotalCents:     totalCents,
		NextPayoutDate: nextPayout.Format("2006-01-02"),
	}, nil
}

// GetTenantsWithEligiblePayouts retorna IDs de tenants que têm compras elegíveis para repasse.
func (r *PayoutRepository) GetTenantsWithEligiblePayouts(ctx context.Context) ([]string, error) {
	query := `
		SELECT DISTINCT tenant_id::text
		FROM digital_purchases
		WHERE payment_status = 'confirmed'
		  AND payout_id IS NULL
		  AND (
		    (payment_method = 'pix'    AND created_at <= NOW() - INTERVAL '2 days') OR
		    (payment_method = 'boleto' AND created_at <= NOW() - INTERVAL '2 days') OR
		    (payment_method = 'credit_card' AND created_at <= NOW() - INTERVAL '15 days')
		  )
	`
	rows, err := r.db.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("erro ao buscar tenants: %w", err)
	}
	defer rows.Close()

	var tenants []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		tenants = append(tenants, id)
	}
	return tenants, rows.Err()
}

// GetTenantPixKey retorna a chave PIX configurada para um tenant.
func (r *PayoutRepository) GetTenantPixKey(ctx context.Context, tenantID string) (string, error) {
	query := `SELECT COALESCE(pix_key, '') FROM tenants WHERE id = $1`
	var pixKey string
	err := r.db.QueryRow(ctx, query, tenantID).Scan(&pixKey)
	if err != nil {
		if err == pgx.ErrNoRows {
			return "", nil
		}
		return "", fmt.Errorf("erro ao buscar pix_key do tenant: %w", err)
	}
	return pixKey, nil
}

// CreatePayout insere um novo creator_payout e marca as compras com o payout_id.
// Executa em transação para garantir atomicidade.
func (r *PayoutRepository) CreatePayout(ctx context.Context, payout *domain.CreatorPayout, purchaseIDs []string) error {
	ids, err := json.Marshal(purchaseIDs)
	if err != nil {
		return fmt.Errorf("erro ao serializar purchase_ids: %w", err)
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("erro ao iniciar transação: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	insertQuery := `
		INSERT INTO creator_payouts
			(id, tenant_id, amount_cents, platform_fee_cents, status, payout_method,
			 pix_key, asaas_transfer_id, scheduled_for, purchase_ids,
			 is_anticipation, anticipation_fee_cents, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`
	_, err = tx.Exec(ctx, insertQuery,
		payout.ID, payout.TenantID, payout.AmountCents, payout.PlatformFeeCents,
		payout.Status, payout.PayoutMethod, payout.PixKey, payout.AsaasTransferID,
		payout.ScheduledFor, ids,
		payout.IsAnticipation, payout.AnticipationFeeCents,
		payout.CreatedAt, payout.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("erro ao inserir payout: %w", err)
	}

	// Marca as compras como vinculadas ao payout
	updateQuery := `
		UPDATE digital_purchases
		SET payout_id = $1
		WHERE id = ANY($2::uuid[]) AND tenant_id = $3
	`
	_, err = tx.Exec(ctx, updateQuery, payout.ID, purchaseIDs, payout.TenantID)
	if err != nil {
		return fmt.Errorf("erro ao vincular compras ao payout: %w", err)
	}

	return tx.Commit(ctx)
}

// AnticipationEligiblePurchase representa uma compra elegível para antecipação de cartão.
type AnticipationEligiblePurchase struct {
	ID              string
	CreatorEarnings int
}

// GetAnticipationEligiblePurchases retorna compras de cartão entre D+2 e D+15
// que ainda não foram repassadas nem tiveram antecipação solicitada.
func (r *PayoutRepository) GetAnticipationEligiblePurchases(ctx context.Context, tenantID string) ([]AnticipationEligiblePurchase, error) {
	query := `
		SELECT id, creator_earnings_cents
		FROM digital_purchases
		WHERE tenant_id = $1
		  AND payment_method = 'credit_card'
		  AND payment_status = 'confirmed'
		  AND payout_id IS NULL
		  AND anticipation_requested_at IS NULL
		  AND created_at > NOW() - INTERVAL '15 days'
		  AND created_at <= NOW() - INTERVAL '2 days'
		ORDER BY created_at ASC
	`
	rows, err := r.db.Query(ctx, query, tenantID)
	if err != nil {
		return nil, fmt.Errorf("erro ao buscar compras elegíveis para antecipação: %w", err)
	}
	defer rows.Close()

	var purchases []AnticipationEligiblePurchase
	for rows.Next() {
		var p AnticipationEligiblePurchase
		if err := rows.Scan(&p.ID, &p.CreatorEarnings); err != nil {
			return nil, fmt.Errorf("erro ao escanear compra elegível: %w", err)
		}
		purchases = append(purchases, p)
	}
	return purchases, rows.Err()
}

// MarkAnticipationRequested atualiza anticipation_requested_at nas compras selecionadas.
// Chamado dentro da transação de antecipação no service.
func (r *PayoutRepository) MarkAnticipationRequested(ctx context.Context, tx pgx.Tx, purchaseIDs []string, tenantID string) error {
	query := `
		UPDATE digital_purchases
		SET anticipation_requested_at = NOW()
		WHERE id = ANY($1::uuid[]) AND tenant_id = $2
	`
	_, err := tx.Exec(ctx, query, purchaseIDs, tenantID)
	if err != nil {
		return fmt.Errorf("erro ao marcar antecipação nas compras: %w", err)
	}
	return nil
}

// CreateAnticipationPayout insere o payout de antecipação e marca as compras atomicamente.
func (r *PayoutRepository) CreateAnticipationPayout(ctx context.Context, payout *domain.CreatorPayout, purchaseIDs []string) error {
	ids, err := json.Marshal(purchaseIDs)
	if err != nil {
		return fmt.Errorf("erro ao serializar purchase_ids: %w", err)
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("erro ao iniciar transação: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	insertQuery := `
		INSERT INTO creator_payouts
			(id, tenant_id, amount_cents, platform_fee_cents, status, payout_method,
			 pix_key, asaas_transfer_id, scheduled_for, purchase_ids,
			 is_anticipation, anticipation_fee_cents, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`
	_, err = tx.Exec(ctx, insertQuery,
		payout.ID, payout.TenantID, payout.AmountCents, payout.PlatformFeeCents,
		payout.Status, payout.PayoutMethod, payout.PixKey, payout.AsaasTransferID,
		payout.ScheduledFor, ids,
		payout.IsAnticipation, payout.AnticipationFeeCents,
		payout.CreatedAt, payout.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("erro ao inserir payout de antecipação: %w", err)
	}

	if err := r.MarkAnticipationRequested(ctx, tx, purchaseIDs, payout.TenantID); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// UpdatePayoutTransferID atualiza o asaas_transfer_id e status de um payout.
func (r *PayoutRepository) UpdatePayoutTransferID(ctx context.Context, payoutID, transferID, status string) error {
	query := `
		UPDATE creator_payouts
		SET asaas_transfer_id = $1, status = $2, updated_at = NOW()
		WHERE id = $3
	`
	_, err := r.db.Exec(ctx, query, transferID, status, payoutID)
	return err
}

// UpdatePayoutStatus atualiza status, error_message e completed_at de um payout.
func (r *PayoutRepository) UpdatePayoutStatus(ctx context.Context, asaasTransferID, status, errMsg string) error {
	query := `
		UPDATE creator_payouts
		SET status        = $1,
		    error_message = $2,
		    completed_at  = CASE WHEN $1 = 'completed' THEN NOW() ELSE NULL END,
		    updated_at    = NOW()
		WHERE asaas_transfer_id = $3
	`
	_, err := r.db.Exec(ctx, query, status, errMsg, asaasTransferID)
	return err
}

// ListPayouts lista os repasses de um tenant com paginação.
func (r *PayoutRepository) ListPayouts(ctx context.Context, tenantID string, limit, offset int) ([]domain.CreatorPayout, int, error) {
	countQuery := `SELECT COUNT(*) FROM creator_payouts WHERE tenant_id = $1`
	var total int
	if err := r.db.QueryRow(ctx, countQuery, tenantID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("erro ao contar payouts: %w", err)
	}

	query := `
		SELECT id, tenant_id, amount_cents, platform_fee_cents, status, payout_method,
		       COALESCE(pix_key, ''), COALESCE(asaas_transfer_id, ''),
		       scheduled_for, completed_at, purchase_ids, COALESCE(error_message, ''),
		       is_anticipation, anticipation_fee_cents,
		       created_at, updated_at
		FROM creator_payouts
		WHERE tenant_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`
	rows, err := r.db.Query(ctx, query, tenantID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("erro ao listar payouts: %w", err)
	}
	defer rows.Close()

	var payouts []domain.CreatorPayout
	for rows.Next() {
		var p domain.CreatorPayout
		var rawIDs []byte
		if err := rows.Scan(
			&p.ID, &p.TenantID, &p.AmountCents, &p.PlatformFeeCents,
			&p.Status, &p.PayoutMethod, &p.PixKey, &p.AsaasTransferID,
			&p.ScheduledFor, &p.CompletedAt, &rawIDs, &p.ErrorMessage,
			&p.IsAnticipation, &p.AnticipationFeeCents,
			&p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("erro ao escanear payout: %w", err)
		}
		p.PurchaseIDs = json.RawMessage(rawIDs)
		payouts = append(payouts, p)
	}
	if payouts == nil {
		payouts = []domain.CreatorPayout{}
	}
	return payouts, total, rows.Err()
}

// nextMonday retorna a próxima segunda-feira a partir de t.
func nextMonday(t time.Time) time.Time {
	daysUntilMonday := (8 - int(t.Weekday())) % 7
	if daysUntilMonday == 0 {
		daysUntilMonday = 7
	}
	return t.AddDate(0, 0, daysUntilMonday).Truncate(24 * time.Hour)
}
