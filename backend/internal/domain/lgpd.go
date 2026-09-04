package domain

import "time"

// LGPD (Lei 13.709/2018) — direitos do titular (Art. 18) sobre os dados de compra.
//
// O linkpay-service não possui conta de usuário: a identidade do titular é
// provada por email + access_token de uma compra ou assinatura pertencente
// a esse email (token enviado ao titular na confirmação do pagamento).

// BuyerDataRetentionYears define o TTL de retenção dos dados pessoais do comprador.
// Após esse prazo os campos PII são anonimizados — LGPD Art. 16 permite a retenção
// por obrigação fiscal; os registros financeiros são mantidos para auditoria
// (commission_ledger e payouts referenciam digital_purchases).
const BuyerDataRetentionYears = 5

// BuyerDataRequest é o corpo dos endpoints de titularidade (Art. 18, V e VI).
type BuyerDataRequest struct {
	Email string `json:"email" validate:"required,email,max=255"`
	Token string `json:"token" validate:"required,max=64"`
}

// BuyerDataErasureResult resume o que foi anonimizado em DELETE /data-erasure
// (LGPD Art. 18, VI). O email é ecoado apenas para confirmação do próprio titular.
type BuyerDataErasureResult struct {
	Email               string `json:"email"`
	PurchasesErased     int64  `json:"purchases_erased"`
	SubscriptionsErased int64  `json:"subscriptions_erased"`
}

// BuyerPurchaseExport é a projeção mínima de uma compra para portabilidade
// (LGPD Art. 18, V) — sem tokens internos nem dados de terceiros.
type BuyerPurchaseExport struct {
	ID            string    `json:"id"`
	ProductID     string    `json:"product_id"`
	ProductName   string    `json:"product_name"`
	AmountCents   int       `json:"amount_cents"`
	PaymentMethod string    `json:"payment_method"`
	PaymentStatus string    `json:"payment_status"`
	DownloadCount int       `json:"download_count"`
	CreatedAt     time.Time `json:"created_at"`
}

// BuyerSubscriptionExport é a projeção mínima de uma assinatura para portabilidade.
type BuyerSubscriptionExport struct {
	ID               string     `json:"id"`
	ProductID        string     `json:"product_id"`
	ProductName      string     `json:"product_name"`
	PlanName         string     `json:"plan_name"`
	Status           string     `json:"status"`
	CurrentPeriodEnd *time.Time `json:"current_period_end,omitempty"`
	CancelledAt      *time.Time `json:"cancelled_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
}

// BuyerDataExport é o payload de POST /data-portability (LGPD Art. 18, V),
// em JSON estruturado e legível por máquina.
type BuyerDataExport struct {
	Email         string                    `json:"email"`
	GeneratedAt   time.Time                 `json:"generated_at"`
	Purchases     []BuyerPurchaseExport     `json:"purchases"`
	Subscriptions []BuyerSubscriptionExport `json:"subscriptions"`
}
