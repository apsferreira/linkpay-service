package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/google/uuid"
)

const (
	ExchangePayments      = "payments"
	ExchangeSubscriptions = "subscriptions"
	ExchangeIITEvents     = "iit.events" // exchange global de eventos do ecossistema IIT

	EventPaymentConfirmed             = "payment.confirmed"
	EventSubscriptionCreated          = "subscription.created"
	EventSubscriptionActivated        = "subscription.activated"
	EventSubscriptionCancelled        = "subscription.cancelled"
	EventLinkPayPurchaseConfirmed     = "linkpay.purchase.confirmed" // BKL-417
	EventAsaasPaymentConfirmed        = "asaas.payment.confirmed"   // pagamentos externos (events-service)

	maxRetries = 5
)

// PaymentConfirmedEvent é publicado quando um pagamento é confirmado no Asaas.
// Payload consumido pelo billing-service para reconciliação financeira.
type PaymentConfirmedEvent struct {
	EventType      string    `json:"event_type"`
	Version        string    `json:"version"`
	PaymentID      string    `json:"payment_id"`
	OrderID        string    `json:"order_id"`
	CustomerID     string    `json:"customer_id"`
	ContractID     string    `json:"contract_id,omitempty"`
	TenantID       string    `json:"tenant_id"`
	AmountCents    int64     `json:"amount_cents"`
	Currency       string    `json:"currency"`
	ProductSlug    string    `json:"product_slug"`
	AsaasPaymentID string    `json:"asaas_payment_id"`
	PaidAt         time.Time `json:"paid_at"`
}

// SubscriptionCreatedEvent é publicado quando uma assinatura é criada
type SubscriptionCreatedEvent struct {
	EventType           string     `json:"event_type"`
	Version             string     `json:"version"`
	SubscriptionID      string     `json:"subscription_id"`
	CustomerID          string     `json:"customer_id"`
	TenantID            string     `json:"tenant_id"`
	PlanSlug            string     `json:"plan_slug"`
	AmountCents         int64      `json:"amount_cents"`
	AsaasSubscriptionID string     `json:"asaas_subscription_id"`
	TrialEnd            *time.Time `json:"trial_end,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
}

// SubscriptionCancelledEvent é publicado quando uma assinatura é cancelada
type SubscriptionCancelledEvent struct {
	EventType      string    `json:"event_type"`
	Version        string    `json:"version"`
	SubscriptionID string    `json:"subscription_id"`
	CustomerID     string    `json:"customer_id"`
	TenantID       string    `json:"tenant_id"`
	PlanSlug       string    `json:"plan_slug"`
	CancelledAt    time.Time `json:"cancelled_at"`
}

// Publisher publica eventos de checkout no RabbitMQ.
// Thread-safe com auto-reconnect em caso de falha de publish.
type Publisher struct {
	rawURL  string
	mu      sync.Mutex
	conn    *amqp.Connection
	channel *amqp.Channel
}

// NewPublisher cria e configura o publisher RabbitMQ com retry na conexão inicial
func NewPublisher(amqpURL string) (*Publisher, error) {
	p := &Publisher{rawURL: amqpURL}
	if err := p.connect(); err != nil {
		return nil, err
	}
	return p, nil
}

func sanitizeURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "<invalid-url>"
	}
	if u.User != nil {
		u.User = url.User("***")
	}
	return u.String()
}

func (p *Publisher) connect() error {
	var err error
	for i := 0; i < maxRetries; i++ {
		p.conn, err = amqp.Dial(p.rawURL)
		if err == nil {
			break
		}
		log.Printf("[checkout.events] connection attempt %d/%d failed (url: %s): %v",
			i+1, maxRetries, sanitizeURL(p.rawURL), err)
		time.Sleep(time.Duration(i+1) * 2 * time.Second)
	}
	if err != nil {
		return fmt.Errorf("falha ao conectar no RabbitMQ após %d tentativas: %w", maxRetries, err)
	}

	p.channel, err = p.conn.Channel()
	if err != nil {
		p.conn.Close()
		return fmt.Errorf("falha ao abrir channel RabbitMQ: %w", err)
	}

	// Declarar exchanges duráveis (idempotente)
	for _, exchange := range []string{ExchangePayments, ExchangeSubscriptions, ExchangeIITEvents} {
		if err := p.channel.ExchangeDeclare(
			exchange,
			"topic",
			true,  // durable
			false, // auto-deleted
			false, // internal
			false, // no-wait
			nil,
		); err != nil {
			p.channel.Close()
			p.conn.Close()
			return fmt.Errorf("falha ao declarar exchange %s: %w", exchange, err)
		}
	}

	return nil
}

// Close fecha a conexão com o RabbitMQ
func (p *Publisher) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.channel != nil {
		p.channel.Close()
	}
	if p.conn != nil {
		p.conn.Close()
	}
}

// PublishPaymentConfirmed publica evento payment.confirmed
func (p *Publisher) PublishPaymentConfirmed(ctx context.Context, event PaymentConfirmedEvent) error {
	event.EventType = EventPaymentConfirmed
	event.Version = EventVersion
	return p.publish(ctx, ExchangePayments, EventPaymentConfirmed, event)
}

// PublishPaymentCompleted publica evento payment.confirmed (alias semântico para billing-service)
func (p *Publisher) PublishPaymentCompleted(ctx context.Context, event PaymentCompletedEvent) error {
	event.EventType = EventPaymentConfirmed
	event.Version = EventVersion
	return p.publish(ctx, ExchangePayments, EventPaymentConfirmed, event)
}

// PublishIITPaymentConfirmed publica evento payment.confirmed no exchange iit.events.
// Usado para integração com outros serviços do ecossistema IIT (BILLING-01).
// Falha silenciosa — não bloqueia o fluxo principal.
func (p *Publisher) PublishIITPaymentConfirmed(ctx context.Context, event IITPaymentConfirmedEvent) error {
	return p.publish(ctx, ExchangeIITEvents, EventPaymentConfirmed, event)
}

// PublishSubscriptionActivated publica evento subscription.activated
func (p *Publisher) PublishSubscriptionActivated(ctx context.Context, event SubscriptionActivatedEvent) error {
	event.EventType = EventSubscriptionActivated
	event.Version = EventVersion
	return p.publish(ctx, ExchangeSubscriptions, EventSubscriptionActivated, event)
}

// PublishSubscriptionCreated publica evento subscription.created
func (p *Publisher) PublishSubscriptionCreated(ctx context.Context, event SubscriptionCreatedEvent) error {
	event.EventType = EventSubscriptionCreated
	event.Version = EventVersion
	return p.publish(ctx, ExchangeSubscriptions, EventSubscriptionCreated, event)
}

// PublishSubscriptionCancelled publica evento subscription.cancelled
func (p *Publisher) PublishSubscriptionCancelled(ctx context.Context, event SubscriptionCancelledEvent) error {
	event.EventType = EventSubscriptionCancelled
	event.Version = EventVersion
	return p.publish(ctx, ExchangeSubscriptions, EventSubscriptionCancelled, event)
}

// PublishLinkPayPurchaseConfirmed publica evento linkpay.purchase.confirmed no exchange iit.events.
// BKL-417: consumido pelo notification-service para envio de email pós-compra.
// Falha silenciosa — não bloqueia confirmação do pagamento.
func (p *Publisher) PublishLinkPayPurchaseConfirmed(ctx context.Context, event LinkPayPurchaseConfirmedEvent) error {
	event.EventType = EventLinkPayPurchaseConfirmed
	event.Version = EventVersion
	return p.publish(ctx, ExchangeIITEvents, EventLinkPayPurchaseConfirmed, event)
}

// PublishAsaasPaymentConfirmed publica evento asaas.payment.confirmed no exchange iit.events.
// Disparado quando o webhook do Asaas refere-se a um pagamento sem order/subscription no checkout-service
// (ex: ingressos do events-service com external_reference = ticketID).
// Falha silenciosa — não bloqueia o retorno 200 ao Asaas.
func (p *Publisher) PublishAsaasPaymentConfirmed(ctx context.Context, event AsaasPaymentConfirmedEvent) error {
	return p.publish(ctx, ExchangeIITEvents, EventAsaasPaymentConfirmed, event)
}

func (p *Publisher) publish(ctx context.Context, exchange, routingKey string, payload interface{}) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("falha ao serializar evento %s: %w", routingKey, err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	msg := amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		MessageId:    uuid.NewString(),
		Timestamp:    time.Now().UTC(),
		Body:         body,
	}

	pubCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	err = p.channel.PublishWithContext(pubCtx, exchange, routingKey, false, false, msg)
	if err != nil {
		// Tenta reconectar uma vez em caso de falha
		log.Printf("[checkout.events] publish falhou, tentando reconectar: %v", err)
		if reconnErr := p.connect(); reconnErr != nil {
			return fmt.Errorf("publish falhou e reconexão falhou: %w", err)
		}
		// Novo contexto para retry
		retryCtx, retryCancel := context.WithTimeout(ctx, 5*time.Second)
		defer retryCancel()
		msg.MessageId = uuid.NewString()
		if retryErr := p.channel.PublishWithContext(retryCtx, exchange, routingKey, false, false, msg); retryErr != nil {
			return fmt.Errorf("falha ao publicar evento %s após reconexão: %w", routingKey, retryErr)
		}
	}

	log.Printf("[checkout.events] evento publicado: exchange=%s routing_key=%s message_id=%s", exchange, routingKey, msg.MessageId)
	return nil
}
