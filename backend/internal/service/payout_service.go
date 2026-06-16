package service

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"linkpay-service/internal/domain"
	"linkpay-service/internal/repository"
)

const minPayoutCents = 1000 // R$10,00 — mínimo para acionar repasse

// PayoutRepository define os métodos de repositório usados pelo PayoutService.
type PayoutRepository interface {
	GetEligiblePurchases(ctx context.Context, tenantID string) ([]repository.EligiblePurchase, error)
	GetAnticipationEligiblePurchases(ctx context.Context, tenantID string) ([]repository.AnticipationEligiblePurchase, error)
	CreateAnticipationPayout(ctx context.Context, payout *domain.CreatorPayout, purchaseIDs []string) error
	GetTenantsWithEligiblePayouts(ctx context.Context) ([]string, error)
	GetTenantPixKey(ctx context.Context, tenantID string) (string, error)
	GetBalance(ctx context.Context, tenantID string) (*domain.Balance, error)
	CreatePayout(ctx context.Context, payout *domain.CreatorPayout, purchaseIDs []string) error
	UpdatePayoutTransferID(ctx context.Context, payoutID, transferID, status string) error
	UpdatePayoutStatus(ctx context.Context, asaasTransferID, status, errMsg string) error
	ListPayouts(ctx context.Context, tenantID string, limit, offset int) ([]domain.CreatorPayout, int, error)
}

// RedisClient define os métodos do Redis usados pelo PayoutService.
// Interface permite mock em testes sem depender de redis real.
type RedisClient interface {
	SetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.BoolCmd
	Del(ctx context.Context, keys ...string) *redis.IntCmd
}

// PayoutService gerencia o fluxo de repasse automático para produtores.
type PayoutService struct {
	repo  PayoutRepository
	asaas *AsaasClient
	redis RedisClient
}

// NewPayoutService cria um PayoutService com as dependências injetadas.
func NewPayoutService(repo PayoutRepository, asaas *AsaasClient, redis RedisClient) *PayoutService {
	return &PayoutService{repo: repo, asaas: asaas, redis: redis}
}

// RunDailyPayouts processa repasses pendentes para todos os tenants elegíveis.
// Chamado pelo job diário às 06h BRT.
func (s *PayoutService) RunDailyPayouts(ctx context.Context) error {
	tenants, err := s.repo.GetTenantsWithEligiblePayouts(ctx)
	if err != nil {
		return fmt.Errorf("erro ao buscar tenants elegíveis: %w", err)
	}

	log.Printf("[payout] job iniciado — %d tenants elegíveis", len(tenants))

	var errs []error
	for _, tenantID := range tenants {
		if err := s.processTenantPayout(ctx, tenantID); err != nil {
			log.Printf("[payout] erro no tenant %s: %v", tenantID, err)
			errs = append(errs, fmt.Errorf("tenant %s: %w", tenantID, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("falhas em %d tenants: %v", len(errs), errs[0])
	}
	log.Printf("[payout] job concluído com sucesso")
	return nil
}

// processTenantPayout calcula saldo e executa repasse para um tenant específico.
// Usa lock distribuído no Redis (SETNX) para evitar duplicidade em múltiplas réplicas.
func (s *PayoutService) processTenantPayout(ctx context.Context, tenantID string) error {
	today := time.Now().UTC().Format("2006-01-02")
	lockKey := fmt.Sprintf("payout:lock:%s:%s", tenantID, today)

	if s.redis != nil {
		ok, err := s.redis.SetNX(ctx, lockKey, "1", 24*time.Hour).Result()
		if err != nil {
			return fmt.Errorf("erro ao adquirir lock distribuído para tenant %s: %w", tenantID, err)
		}
		if !ok {
			log.Printf("[payout] tenant %s já em processamento hoje — pulando", tenantID)
			return nil
		}
		defer func() {
			if s.redis.Del(ctx, lockKey).Err() != nil {
				log.Printf("[payout] WARN: erro ao liberar lock %s", lockKey)
			}
		}()
	}

	pixKey, err := s.repo.GetTenantPixKey(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("erro ao buscar pix_key: %w", err)
	}
	if pixKey == "" {
		log.Printf("[payout] tenant %s sem pix_key configurada — pulando", tenantID)
		return nil
	}

	purchases, err := s.repo.GetEligiblePurchases(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("erro ao buscar compras elegíveis: %w", err)
	}
	if len(purchases) == 0 {
		return nil
	}

	// Calcula total disponível e aplica reserva anti-chargeback (10%)
	var grossCents int
	purchaseIDs := make([]string, 0, len(purchases))
	for _, p := range purchases {
		grossCents += p.CreatorEarnings
		purchaseIDs = append(purchaseIDs, p.ID)
	}
	reserveCents := grossCents * domain.HoldbackReservePct / 100
	availableCents := grossCents - reserveCents

	if availableCents < minPayoutCents {
		log.Printf("[payout] tenant %s saldo disponível R$%.2f abaixo do mínimo — pulando", tenantID, float64(availableCents)/100)
		return nil
	}

	return s.executePayout(ctx, tenantID, pixKey, availableCents, purchaseIDs)
}

// RequestManualPayout processa um saque manual solicitado pelo produtor.
func (s *PayoutService) RequestManualPayout(ctx context.Context, tenantID, pixKey string, amountCents int) (*domain.CreatorPayout, error) {
	balance, err := s.repo.GetBalance(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("erro ao buscar saldo: %w", err)
	}

	if amountCents == 0 {
		amountCents = balance.AvailableCents
	}

	if amountCents < minPayoutCents {
		return nil, fmt.Errorf("saldo disponível R$%.2f abaixo do mínimo de R$10,00", float64(amountCents)/100)
	}
	if amountCents > balance.AvailableCents {
		return nil, fmt.Errorf("valor solicitado R$%.2f superior ao disponível R$%.2f",
			float64(amountCents)/100, float64(balance.AvailableCents)/100)
	}

	purchases, err := s.repo.GetEligiblePurchases(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("erro ao buscar compras elegíveis: %w", err)
	}

	// Seleciona compras até atingir o amountCents solicitado (com reserva 10%)
	var accumulated int
	selectedIDs := make([]string, 0, len(purchases))
	for _, p := range purchases {
		net := p.CreatorEarnings - (p.CreatorEarnings*domain.HoldbackReservePct/100)
		if accumulated+net > amountCents {
			break
		}
		accumulated += net
		selectedIDs = append(selectedIDs, p.ID)
	}

	if accumulated < minPayoutCents {
		return nil, fmt.Errorf("não foi possível selecionar compras suficientes para o valor solicitado")
	}

	if err := s.executePayout(ctx, tenantID, pixKey, accumulated, selectedIDs); err != nil {
		return nil, err
	}

	// Retorna o payout recém-criado listando o primeiro da lista
	payouts, _, err := s.repo.ListPayouts(ctx, tenantID, 1, 0)
	if err != nil || len(payouts) == 0 {
		return nil, fmt.Errorf("payout criado mas erro ao recuperar registro: %w", err)
	}
	return &payouts[0], nil
}

// executePayout cria o registro de payout e aciona a Asaas Transfers API.
func (s *PayoutService) executePayout(ctx context.Context, tenantID, pixKey string, amountCents int, purchaseIDs []string) error {
	now := time.Now().UTC()
	payoutID := uuid.New().String()

	payout := &domain.CreatorPayout{
		ID:           payoutID,
		TenantID:     tenantID,
		AmountCents:  amountCents,
		Status:       "processing",
		PayoutMethod: "pix",
		PixKey:       pixKey,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := s.repo.CreatePayout(ctx, payout, purchaseIDs); err != nil {
		return fmt.Errorf("erro ao criar payout: %w", err)
	}

	// Chama Asaas Transfers API
	transferReq := AsaasTransferRequest{
		Value:             float64(amountCents) / 100.0,
		PixAddressKey:     pixKey,
		PixAddressKeyType: DetectPixKeyType(pixKey),
		Description:       fmt.Sprintf("Repasse LinkPay — %d compras", len(purchaseIDs)),
		ExternalReference: payoutID,
	}

	transferResp, err := s.asaas.CreateTransfer(transferReq)
	if err != nil {
		// Registra falha no payout mas não reverte as compras (reprocessar manualmente)
		_ = s.repo.UpdatePayoutStatus(ctx, "", "failed", err.Error())
		log.Printf("[payout] falha ao chamar Asaas para payout %s: %v", payoutID, err)
		// Atualiza direto pelo ID do payout já que não temos asaas_transfer_id ainda
		if updErr := s.repo.UpdatePayoutTransferID(ctx, payoutID, "", "failed"); updErr != nil {
			log.Printf("[payout] erro ao marcar payout %s como failed: %v", payoutID, updErr)
		}
		return fmt.Errorf("erro ao acionar Asaas Transfer: %w", err)
	}

	if err := s.repo.UpdatePayoutTransferID(ctx, payoutID, transferResp.ID, "processing"); err != nil {
		log.Printf("[payout] payout %s criado no Asaas (%s) mas erro ao salvar transfer_id: %v", payoutID, transferResp.ID, err)
	}

	log.Printf("[payout] repasse de R$%.2f iniciado para tenant %s — Asaas transfer %s",
		float64(amountCents)/100, tenantID, transferResp.ID)
	return nil
}

// PreviewAnticipation calcula o preview de antecipação para o tenant.
// Retorna nil se não houver compras elegíveis.
func (s *PayoutService) PreviewAnticipation(ctx context.Context, tenantID string) (*domain.AnticipationPreview, error) {
	purchases, err := s.repo.GetAnticipationEligiblePurchases(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("erro ao buscar compras elegíveis para antecipação: %w", err)
	}
	if len(purchases) == 0 {
		return nil, nil
	}

	var eligibleCents int
	for _, p := range purchases {
		eligibleCents += p.CreatorEarnings
	}

	feeCents := eligibleCents * domain.AnticipationFeePercent / 100
	netCents := eligibleCents - feeCents

	return &domain.AnticipationPreview{
		EligibleCents:     eligibleCents,
		FeeCents:          feeCents,
		NetCents:          netCents,
		EligiblePurchases: len(purchases),
		AvailableAt:       "em 2 dias úteis",
	}, nil
}

// RequestAnticipation solicita antecipação de recebíveis de cartão.
// Cobra 2% de taxa e agenda repasse via Asaas em D+2.
func (s *PayoutService) RequestAnticipation(ctx context.Context, tenantID, pixKey string) (*domain.CreatorPayout, error) {
	// Busca pix_key salva se não fornecida
	if pixKey == "" {
		saved, err := s.repo.GetTenantPixKey(ctx, tenantID)
		if err != nil {
			return nil, fmt.Errorf("erro ao buscar chave PIX do tenant: %w", err)
		}
		pixKey = saved
	}
	if pixKey == "" {
		return nil, fmt.Errorf("nenhuma chave PIX fornecida ou cadastrada para o tenant")
	}

	preview, err := s.PreviewAnticipation(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if preview == nil {
		return nil, fmt.Errorf("nenhuma compra de cartão elegível para antecipação")
	}
	if preview.NetCents < minPayoutCents {
		return nil, fmt.Errorf("valor líquido R$%.2f abaixo do mínimo de R$10,00 para antecipação", float64(preview.NetCents)/100)
	}

	// Busca IDs das compras elegíveis
	purchases, err := s.repo.GetAnticipationEligiblePurchases(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("erro ao buscar compras: %w", err)
	}
	purchaseIDs := make([]string, 0, len(purchases))
	for _, p := range purchases {
		purchaseIDs = append(purchaseIDs, p.ID)
	}

	now := time.Now().UTC()
	scheduledFor := addBusinessDays(now, domain.AnticipationDays)
	payoutID := uuid.New().String()

	payout := &domain.CreatorPayout{
		ID:                   payoutID,
		TenantID:             tenantID,
		AmountCents:          preview.NetCents,
		PlatformFeeCents:     0,
		Status:               "scheduled",
		PayoutMethod:         "pix",
		PixKey:               pixKey,
		ScheduledFor:         &scheduledFor,
		IsAnticipation:       true,
		AnticipationFeeCents: preview.FeeCents,
		CreatedAt:            now,
		UpdatedAt:            now,
	}

	if err := s.repo.CreateAnticipationPayout(ctx, payout, purchaseIDs); err != nil {
		return nil, fmt.Errorf("erro ao registrar antecipação: %w", err)
	}

	// Aciona Asaas Transfers API com scheduleDate = D+2
	transferReq := AsaasTransferRequest{
		Value:             float64(preview.NetCents) / 100.0,
		PixAddressKey:     pixKey,
		PixAddressKeyType: DetectPixKeyType(pixKey),
		Description:       fmt.Sprintf("Antecipação LinkPay D+2 — %d compras (taxa 2%%)", len(purchaseIDs)),
		ExternalReference: payoutID,
		ScheduleDate:      scheduledFor.Format("2006-01-02"),
	}

	transferResp, err := s.asaas.CreateTransfer(transferReq)
	if err != nil {
		log.Printf("[payout] antecipação %s criada localmente mas falhou no Asaas: %v", payoutID, err)
		if updErr := s.repo.UpdatePayoutTransferID(ctx, payoutID, "", "failed"); updErr != nil {
			log.Printf("[payout] erro ao marcar antecipação %s como failed: %v", payoutID, updErr)
		}
		return nil, fmt.Errorf("erro ao acionar Asaas Transfer para antecipação: %w", err)
	}

	if err := s.repo.UpdatePayoutTransferID(ctx, payoutID, transferResp.ID, "scheduled"); err != nil {
		log.Printf("[payout] antecipação %s no Asaas (%s) mas erro ao salvar transfer_id: %v", payoutID, transferResp.ID, err)
	}

	payout.AsaasTransferID = transferResp.ID
	log.Printf("[payout] antecipação de R$%.2f agendada para tenant %s — Asaas transfer %s, taxa R$%.2f",
		float64(preview.NetCents)/100, tenantID, transferResp.ID, float64(preview.FeeCents)/100)

	return payout, nil
}

// addBusinessDays adiciona n dias úteis (seg-sex) à data t.
func addBusinessDays(t time.Time, n int) time.Time {
	added := 0
	current := t
	for added < n {
		current = current.AddDate(0, 0, 1)
		if current.Weekday() != time.Saturday && current.Weekday() != time.Sunday {
			added++
		}
	}
	return current
}

// GetBalance retorna o saldo disponível, retido e reservado de um tenant.
func (s *PayoutService) GetBalance(ctx context.Context, tenantID string) (*domain.Balance, error) {
	return s.repo.GetBalance(ctx, tenantID)
}

// ListPayouts lista os repasses de um tenant com paginação.
func (s *PayoutService) ListPayouts(ctx context.Context, tenantID string, limit, offset int) (*domain.ListPayoutsResponse, error) {
	payouts, total, err := s.repo.ListPayouts(ctx, tenantID, limit, offset)
	if err != nil {
		return nil, err
	}
	page := 1
	if limit > 0 {
		page = offset/limit + 1
	}
	return &domain.ListPayoutsResponse{
		Data:  payouts,
		Total: total,
		Page:  page,
		Limit: limit,
	}, nil
}

// HandleTransferWebhook processa eventos de transferência recebidos do webhook Asaas.
func (s *PayoutService) HandleTransferWebhook(ctx context.Context, event, transferID, description string) error {
	switch event {
	case "TRANSFER_COMPLETED":
		return s.repo.UpdatePayoutStatus(ctx, transferID, "completed", "")
	case "TRANSFER_FAILED":
		return s.repo.UpdatePayoutStatus(ctx, transferID, "failed", description)
	}
	return nil
}
