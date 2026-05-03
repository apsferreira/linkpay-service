package messaging

import "time"

const (
	EventVersion = "1.0"
)

// IITPaymentConfirmedData contém os dados do evento payment.confirmed publicado no exchange iit.events.
// Schema definido pela tarefa BILLING-01 — consumido por outros serviços do ecossistema IIT.
type IITPaymentConfirmedData struct {
	OrderID        string    `json:"order_id"`
	CustomerID     string    `json:"customer_id"`
	TenantID       string    `json:"tenant_id"`
	Amount         float64   `json:"amount"`
	Currency       string    `json:"currency"`
	PaymentMethod  string    `json:"payment_method"`
	AsaasPaymentID string    `json:"asaas_payment_id"`
	ConfirmedAt    time.Time `json:"confirmed_at"`
}

// IITPaymentConfirmedEvent é o envelope publicado no exchange iit.events com routing key payment.confirmed.
type IITPaymentConfirmedEvent struct {
	Event      string                  `json:"event"`
	OccurredAt time.Time               `json:"occurred_at"`
	Data       IITPaymentConfirmedData `json:"data"`
}

// PaymentCompletedEvent evento publicado quando pagamento e confirmado via webhook Asaas.
// Exchange: payments | Routing key: payment.confirmed
type PaymentCompletedEvent struct {
	EventType      string    `json:"event_type"`
	Version        string    `json:"version"`
	PaymentID      string    `json:"payment_id"`
	OrderID        string    `json:"order_id"`
	TenantID       string    `json:"tenant_id"`
	CustomerID     string    `json:"customer_id"`
	ContractID     string    `json:"contract_id,omitempty"`
	ProductSlug    string    `json:"product_slug"`
	AmountCents    int64     `json:"amount_cents"`
	Currency       string    `json:"currency"`
	AsaasPaymentID string    `json:"asaas_payment_id"`
	PaidAt         time.Time `json:"paid_at"`
}

// LinkPayPurchaseConfirmedEvent é publicado no exchange iit.events quando uma compra LinkPay é confirmada.
// BKL-417: consumido pelo notification-service para disparar email pós-compra com link de download + upsell.
// Routing key: linkpay.purchase.confirmed
type LinkPayPurchaseConfirmedEvent struct {
	EventType       string    `json:"event_type"`
	Version         string    `json:"version"`
	PurchaseID      string    `json:"purchase_id"`
	ProductID       string    `json:"product_id"`
	TenantID        string    `json:"tenant_id"`
	CustomerEmail   string    `json:"customer_email"`
	CustomerName    string    `json:"customer_name"`
	AmountCents     int       `json:"amount_cents"`
	AccessToken     string    `json:"access_token"`      // token de download seguro
	DownloadURL     string    `json:"download_url"`      // URL completa de download
	ExpiresAt       time.Time `json:"expires_at"`        // expiração do link (7 dias)
	OccurredAt      time.Time `json:"occurred_at"`
}

// AsaasPaymentConfirmedEvent é publicado quando o checkout-service recebe um webhook do Asaas
// para um pagamento que não pertence a nenhum order/subscription do checkout-service.
// Exchange: iit.events | Routing key: asaas.payment.confirmed
// Consumido pelo events-service para confirmar ingressos do CaTracaVirtual.
type AsaasPaymentConfirmedEvent struct {
	AsaasPaymentID    string    `json:"asaas_payment_id"`
	ExternalReference string    `json:"external_reference"`
	AmountCents       int       `json:"amount_cents"`
	PaymentMethod     string    `json:"payment_method"`
	ConfirmedAt       time.Time `json:"confirmed_at"`
}

// SubscriptionActivatedEvent evento publicado quando uma assinatura e ativada (primeiro pagamento confirmado).
// Exchange: subscriptions | Routing key: subscription.activated
type SubscriptionActivatedEvent struct {
	EventType      string    `json:"event_type"`
	Version        string    `json:"version"`
	SubscriptionID string    `json:"subscription_id"`
	TenantID       string    `json:"tenant_id"`
	CustomerID     string    `json:"customer_id"`
	PlanSlug       string    `json:"plan_slug"`
	PeriodStart    time.Time `json:"period_start"`
	PeriodEnd      time.Time `json:"period_end"`
}
