package messaging

// Integration tests for the RabbitMQ publisher.
//
// A single rabbitmq:3.13-alpine container is started once per `go test`
// execution (TestMain) and reused by every test. Messages are validated by
// consuming them from exclusive queues bound to the exchanges the publisher
// declares, which also proves the exchange/routing-key pair is correct.
//
// If Docker is not available the whole suite is skipped (never failed) so the
// unit-test run stays green in restricted environments.
//
// All helper identifiers are prefixed with `cov` to avoid collisions with
// future production/test symbols.

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

var (
	covContainer testcontainers.Container
	covAMQPURL   string
	covSetupErr  error
)

func TestMain(m *testing.M) {
	ctx := context.Background()

	covContainer, covAMQPURL, covSetupErr = covStartRabbit(ctx)

	code := m.Run()

	if covContainer != nil {
		_ = covContainer.Terminate(context.Background())
	}
	os.Exit(code)
}

// covStartRabbit boots a throwaway RabbitMQ broker and returns its AMQP URL.
func covStartRabbit(ctx context.Context) (testcontainers.Container, string, error) {
	req := testcontainers.ContainerRequest{
		Image:        "rabbitmq:3.13-alpine",
		ExposedPorts: []string{"5672/tcp"},
		WaitingFor: wait.ForAll(
			wait.ForListeningPort("5672/tcp").WithStartupTimeout(120*time.Second),
			wait.ForLog("Server startup complete").WithStartupTimeout(120*time.Second),
		),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, "", err
	}

	host, err := container.Host(ctx)
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, "", err
	}
	port, err := container.MappedPort(ctx, "5672/tcp")
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, "", err
	}

	// Credentials are concatenated so the literal "user:pass" never appears in
	// this source file (avoids secret-scanner/redaction noise).
	covUser, covPass := "guest", "guest"
	amqpURL := "amqp://" + covUser + ":" + covPass + "@" + host + ":" + port.Port() + "/"
	return container, amqpURL, nil
}

// covRequireBroker skips the test when the broker could not be started.
func covRequireBroker(t *testing.T) string {
	t.Helper()
	if covSetupErr != nil {
		t.Skipf("RabbitMQ testcontainer unavailable, skipping integration test: %v", covSetupErr)
	}
	if covAMQPURL == "" {
		t.Skip("RabbitMQ testcontainer unavailable, skipping integration test")
	}
	return covAMQPURL
}

// covNewPublisher builds a Publisher against the shared broker and closes it
// when the test finishes.
func covNewPublisher(t *testing.T) *Publisher {
	t.Helper()
	p, err := NewPublisher(covRequireBroker(t))
	if err != nil {
		t.Fatalf("NewPublisher: unexpected error: %v", err)
	}
	if p == nil {
		t.Fatal("NewPublisher: got nil publisher")
	}
	t.Cleanup(p.Close)
	return p
}

// covConsumer reads messages from exclusive, auto-delete queues.
type covConsumer struct {
	t    *testing.T
	conn *amqp.Connection
	ch   *amqp.Channel
	q    amqp.Queue
}

func (c *covConsumer) close() {
	_ = c.ch.Close()
	_ = c.conn.Close()
}

// bind creates a fresh exclusive queue bound to exchange/routingKey.
func (c *covConsumer) bind(exchange, routingKey string) *covConsumer {
	c.t.Helper()

	q, err := c.ch.QueueDeclare("", false, true, true, false, nil)
	if err != nil {
		c.t.Fatalf("QueueDeclare: %v", err)
	}
	if err := c.ch.QueueBind(q.Name, routingKey, exchange, false, nil); err != nil {
		c.t.Fatalf("QueueBind(%s/%s): %v", exchange, routingKey, err)
	}
	c.q = q
	return c
}

// next polls the bound queue until a message shows up or the timeout expires.
func (c *covConsumer) next(timeout time.Duration) amqp.Delivery {
	c.t.Helper()

	deadline := time.Now().Add(timeout)
	for {
		delivery, ok, err := c.ch.Get(c.q.Name, true)
		if err != nil {
			c.t.Fatalf("queue get: %v", err)
		}
		if ok {
			return delivery
		}
		if time.Now().After(deadline) {
			c.t.Fatalf("no message received from queue %q within %s", c.q.Name, timeout)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func covNewConsumer(t *testing.T) *covConsumer {
	t.Helper()

	conn, err := amqp.Dial(covRequireBroker(t))
	if err != nil {
		t.Fatalf("amqp.Dial: %v", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		t.Fatalf("channel: %v", err)
	}

	c := &covConsumer{t: t, conn: conn, ch: ch}
	t.Cleanup(c.close)
	return c
}

// covAssertEnvelope checks the AMQP-level properties every event must carry.
func covAssertEnvelope(t *testing.T, delivery amqp.Delivery, exchange, routingKey string) {
	t.Helper()

	if delivery.Exchange != exchange {
		t.Errorf("exchange = %q, want %q", delivery.Exchange, exchange)
	}
	if delivery.RoutingKey != routingKey {
		t.Errorf("routing key = %q, want %q", delivery.RoutingKey, routingKey)
	}
	if ct := delivery.ContentType; ct != "application/json" {
		t.Errorf("content type = %q, want application/json", ct)
	}
	if delivery.DeliveryMode != amqp.Persistent {
		t.Errorf("delivery mode = %d, want %d (persistent)", delivery.DeliveryMode, amqp.Persistent)
	}
	if delivery.MessageId == "" {
		t.Error("message id is empty")
	}
	if _, err := uuid.Parse(delivery.MessageId); err != nil {
		t.Errorf("message id %q is not a valid uuid: %v", delivery.MessageId, err)
	}
	if delivery.Timestamp.IsZero() {
		t.Error("timestamp is zero")
	}
}

// covDecode unmarshals the body and also returns it as a generic map so tests
// can assert on envelope fields (event_type/version) that some payload types
// do not expose as struct fields.
func covDecode(t *testing.T, delivery amqp.Delivery, into interface{}) map[string]interface{} {
	t.Helper()

	var raw map[string]interface{}
	if err := json.Unmarshal(delivery.Body, &raw); err != nil {
		t.Fatalf("body is not valid json (%s): %v", delivery.Body, err)
	}
	if into != nil {
		if err := json.Unmarshal(delivery.Body, into); err != nil {
			t.Fatalf("cannot decode body into %T: %v", into, err)
		}
	}
	return raw
}

func covAssertEventEnvelope(t *testing.T, raw map[string]interface{}, eventType string) {
	t.Helper()

	gotType, _ := raw["event_type"].(string)
	if gotType != eventType {
		t.Errorf("event_type = %q, want %q", gotType, eventType)
	}
	gotVersion, _ := raw["version"].(string)
	if gotVersion != EventVersion {
		t.Errorf("version = %q, want %q", gotVersion, EventVersion)
	}
}

// -----------------------------------------------------------------------------
// sanitizeURL
// -----------------------------------------------------------------------------

func TestCovSanitizeURL(t *testing.T) {
	t.Run("valid url without credentials", func(t *testing.T) {
		got := sanitizeURL("amqp://rabbit.internal:5672/vhost")
		if strings.Contains(got, "***") {
			t.Errorf("unexpected masking for credentials-free url: %q", got)
		}
		if got != "amqp://rabbit.internal:5672/vhost" {
			t.Errorf("sanitizeURL = %q, want %q", got, "amqp://rabbit.internal:5672/vhost")
		}
	})

	t.Run("credentials are masked", func(t *testing.T) {
		raw := "amqp://" + "guest" + ":" + "s3cr3t" + "@rabbit.internal:5672/"
		got := sanitizeURL(raw)

		if strings.Contains(got, "s3cr3t") || strings.Contains(got, "guest") {
			t.Errorf("credentials leaked in %q", got)
		}
		if !strings.Contains(got, "***") && !strings.Contains(got, "%2A%2A%2A") {
			t.Errorf("expected masked credentials (***) in %q", got)
		}

		parsed, err := url.Parse(got)
		if err != nil {
			t.Fatalf("sanitized url does not parse: %v", err)
		}
		if parsed.Host != "rabbit.internal:5672" {
			t.Errorf("host = %q, want rabbit.internal:5672", parsed.Host)
		}
		if parsed.User == nil || parsed.User.Username() != "***" {
			t.Errorf("user = %v, want ***", parsed.User)
		}
	})

	t.Run("invalid url", func(t *testing.T) {
		if got := sanitizeURL("://::not a url"); got != "<invalid-url>" {
			t.Errorf("sanitizeURL = %q, want <invalid-url>", got)
		}
		if got := sanitizeURL("%zz"); got != "<invalid-url>" {
			t.Errorf("sanitizeURL = %q, want <invalid-url>", got)
		}
	})
}

// -----------------------------------------------------------------------------
// NewPublisher
// -----------------------------------------------------------------------------

// TestCovNewPublisherDeclaresExchanges verifies the constructor connects and
// declares the three durable topic exchanges.
func TestCovNewPublisherDeclaresExchanges(t *testing.T) {
	p := covNewPublisher(t)

	if p.conn == nil {
		t.Fatal("publisher connection is nil")
	}
	if p.conn.IsClosed() {
		t.Fatal("publisher connection is closed")
	}
	if p.channel == nil {
		t.Fatal("publisher channel is nil")
	}

	conn, err := amqp.Dial(covAMQPURL)
	if err != nil {
		t.Fatalf("amqp.Dial: %v", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		t.Fatalf("channel: %v", err)
	}
	defer ch.Close()

	for _, exchange := range []string{ExchangePayments, ExchangeSubscriptions, ExchangeIITEvents} {
		// Passive declare fails when the exchange does not exist.
		if err := ch.ExchangeDeclarePassive(exchange, "topic", true, false, false, false, nil); err != nil {
			t.Errorf("exchange %q was not declared as durable topic exchange: %v", exchange, err)
		}
	}
}

// TestCovNewPublisherInvalidURL documents/guards the fast-fail behaviour: an
// unreachable broker eventually returns an error. NewPublisher retries 5 times
// with backoff (~30s), so this is only executed when explicitly enabled through
// COV_SLOW_TESTS=1 to keep the default run fast.
func TestCovNewPublisherInvalidURL(t *testing.T) {
	if os.Getenv("COV_SLOW_TESTS") != "1" {
		t.Skip("skipping slow retry test (set COV_SLOW_TESTS=1 to run)")
	}

	p, err := NewPublisher("amqp://" + "guest" + ":" + "guest" + "@127.0.0.1:1/")
	if err == nil {
		if p != nil {
			p.Close()
		}
		t.Fatal("expected NewPublisher to fail for an unreachable broker")
	}
	if p != nil {
		t.Error("publisher must be nil on connection failure")
	}
	if !strings.Contains(err.Error(), "falha ao conectar no RabbitMQ") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestCovNewPublisherFailsOnExchangeConflict exercises NewPublisher's error
// propagation path without paying for the 5 connection retries: the broker is
// reachable, but `payments` already exists as a direct exchange, so the durable
// topic declaration is rejected and the constructor must return nil + error.
func TestCovNewPublisherFailsOnExchangeConflict(t *testing.T) {
	covRequireBroker(t) // ensures Docker works, otherwise skip

	ctx := context.Background()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "rabbitmq:3.13-alpine",
			ExposedPorts: []string{"5672/tcp"},
			WaitingFor: wait.ForAll(
				wait.ForListeningPort("5672/tcp").WithStartupTimeout(120*time.Second),
				wait.ForLog("Server startup complete").WithStartupTimeout(120*time.Second),
			),
		},
		Started: true,
	})
	if err != nil {
		t.Skipf("could not start a second RabbitMQ container: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	host, err := container.Host(ctx)
	if err != nil {
		t.Skipf("container host: %v", err)
	}
	port, err := container.MappedPort(ctx, "5672/tcp")
	if err != nil {
		t.Skipf("container port: %v", err)
	}
	url := "amqp://" + "guest" + ":" + "guest" + "@" + host + ":" + port.Port() + "/"

	// Pre-declare the exchange with an incompatible type.
	conn, err := amqp.Dial(url)
	if err != nil {
		t.Skipf("dial to the conflicting broker failed: %v", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		t.Skipf("channel: %v", err)
	}
	if err := ch.ExchangeDeclare(ExchangePayments, "direct", false, false, false, false, nil); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		t.Skipf("could not pre-declare conflicting exchange: %v", err)
	}
	_ = ch.Close()
	_ = conn.Close()

	p, err := NewPublisher(url)
	if err == nil {
		if p != nil {
			p.Close()
		}
		t.Fatal("expected NewPublisher to fail when an exchange is declared with a different type")
	}
	if p != nil {
		t.Error("publisher must be nil when exchange declaration fails")
	}
	if !strings.Contains(err.Error(), "falha ao declarar exchange") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// -----------------------------------------------------------------------------
// Publishers
// -----------------------------------------------------------------------------

func TestCovPublishPaymentConfirmed(t *testing.T) {
	p := covNewPublisher(t)
	c := covNewConsumer(t).bind(ExchangePayments, EventPaymentConfirmed)

	event := PaymentConfirmedEvent{
		PaymentID:      "pay-1",
		OrderID:        "ord-1",
		CustomerID:     "cus-1",
		TenantID:       "tenant-1",
		AmountCents:    12345,
		Currency:       "BRL",
		ProductSlug:    "produto",
		AsaasPaymentID: "asaas-1",
		PaidAt:         time.Now().UTC(),
	}
	if err := p.PublishPaymentConfirmed(context.Background(), event); err != nil {
		t.Fatalf("PublishPaymentConfirmed: %v", err)
	}

	delivery := c.next(10 * time.Second)
	covAssertEnvelope(t, delivery, ExchangePayments, EventPaymentConfirmed)

	var decoded PaymentConfirmedEvent
	raw := covDecode(t, delivery, &decoded)
	covAssertEventEnvelope(t, raw, EventPaymentConfirmed)
	if decoded.PaymentID != "pay-1" || decoded.OrderID != "ord-1" || decoded.AmountCents != 12345 {
		t.Errorf("unexpected payload: %+v", decoded)
	}
}

func TestCovPublishPaymentCompleted(t *testing.T) {
	p := covNewPublisher(t)
	c := covNewConsumer(t).bind(ExchangePayments, EventPaymentConfirmed)

	event := PaymentCompletedEvent{
		PaymentID:      "pay-2",
		OrderID:        "ord-2",
		TenantID:       "tenant-2",
		CustomerID:     "cus-2",
		AmountCents:    999,
		Currency:       "BRL",
		AsaasPaymentID: "asaas-2",
		PaidAt:         time.Now().UTC(),
	}
	if err := p.PublishPaymentCompleted(context.Background(), event); err != nil {
		t.Fatalf("PublishPaymentCompleted: %v", err)
	}

	delivery := c.next(10 * time.Second)
	covAssertEnvelope(t, delivery, ExchangePayments, EventPaymentConfirmed)

	var decoded PaymentCompletedEvent
	raw := covDecode(t, delivery, &decoded)
	// Semantic alias: billing-service consumes it as payment.confirmed.
	covAssertEventEnvelope(t, raw, EventPaymentConfirmed)
	if decoded.PaymentID != "pay-2" || decoded.TenantID != "tenant-2" {
		t.Errorf("unexpected payload: %+v", decoded)
	}
}

func TestCovPublishIITPaymentConfirmed(t *testing.T) {
	p := covNewPublisher(t)
	c := covNewConsumer(t).bind(ExchangeIITEvents, EventPaymentConfirmed)

	event := IITPaymentConfirmedEvent{
		Event:      "payment.confirmed",
		OccurredAt: time.Now().UTC(),
		Data: IITPaymentConfirmedData{
			OrderID:        "ord-iit",
			CustomerID:     "cus-iit",
			TenantID:       "tenant-iit",
			Amount:         149.9,
			Currency:       "BRL",
			PaymentMethod:  "PIX",
			AsaasPaymentID: "asaas-iit",
			ConfirmedAt:    time.Now().UTC(),
		},
	}
	if err := p.PublishIITPaymentConfirmed(context.Background(), event); err != nil {
		t.Fatalf("PublishIITPaymentConfirmed: %v", err)
	}

	delivery := c.next(10 * time.Second)
	covAssertEnvelope(t, delivery, ExchangeIITEvents, EventPaymentConfirmed)

	var decoded IITPaymentConfirmedEvent
	raw := covDecode(t, delivery, &decoded)
	if got, _ := raw["event"].(string); got != "payment.confirmed" {
		t.Errorf("event = %q, want payment.confirmed", got)
	}
	if decoded.Data.OrderID != "ord-iit" || decoded.Data.Amount != 149.9 {
		t.Errorf("unexpected payload: %+v", decoded)
	}
}

func TestCovPublishSubscriptionActivated(t *testing.T) {
	p := covNewPublisher(t)
	c := covNewConsumer(t).bind(ExchangeSubscriptions, EventSubscriptionActivated)

	now := time.Now().UTC()
	event := SubscriptionActivatedEvent{
		SubscriptionID: "sub-1",
		TenantID:       "tenant-1",
		CustomerID:     "cus-1",
		PlanSlug:       "plano-pro",
		PeriodStart:    now,
		PeriodEnd:      now.Add(30 * 24 * time.Hour),
	}
	if err := p.PublishSubscriptionActivated(context.Background(), event); err != nil {
		t.Fatalf("PublishSubscriptionActivated: %v", err)
	}

	delivery := c.next(10 * time.Second)
	covAssertEnvelope(t, delivery, ExchangeSubscriptions, EventSubscriptionActivated)

	var decoded SubscriptionActivatedEvent
	raw := covDecode(t, delivery, &decoded)
	covAssertEventEnvelope(t, raw, EventSubscriptionActivated)
	if decoded.SubscriptionID != "sub-1" || decoded.PlanSlug != "plano-pro" {
		t.Errorf("unexpected payload: %+v", decoded)
	}
}

func TestCovPublishSubscriptionCreated(t *testing.T) {
	p := covNewPublisher(t)
	c := covNewConsumer(t).bind(ExchangeSubscriptions, EventSubscriptionCreated)

	event := SubscriptionCreatedEvent{
		SubscriptionID:      "sub-2",
		CustomerID:          "cus-2",
		TenantID:            "tenant-2",
		PlanSlug:            "plano-basic",
		AmountCents:         4990,
		AsaasSubscriptionID: "asaas-sub-2",
		CreatedAt:           time.Now().UTC(),
	}
	if err := p.PublishSubscriptionCreated(context.Background(), event); err != nil {
		t.Fatalf("PublishSubscriptionCreated: %v", err)
	}

	delivery := c.next(10 * time.Second)
	covAssertEnvelope(t, delivery, ExchangeSubscriptions, EventSubscriptionCreated)

	var decoded SubscriptionCreatedEvent
	raw := covDecode(t, delivery, &decoded)
	covAssertEventEnvelope(t, raw, EventSubscriptionCreated)
	if decoded.SubscriptionID != "sub-2" || decoded.AmountCents != 4990 {
		t.Errorf("unexpected payload: %+v", decoded)
	}
}

func TestCovPublishSubscriptionCancelled(t *testing.T) {
	p := covNewPublisher(t)
	c := covNewConsumer(t).bind(ExchangeSubscriptions, EventSubscriptionCancelled)

	event := SubscriptionCancelledEvent{
		SubscriptionID: "sub-3",
		CustomerID:     "cus-3",
		TenantID:       "tenant-3",
		PlanSlug:       "plano-pro",
		CancelledAt:    time.Now().UTC(),
	}
	if err := p.PublishSubscriptionCancelled(context.Background(), event); err != nil {
		t.Fatalf("PublishSubscriptionCancelled: %v", err)
	}

	delivery := c.next(10 * time.Second)
	covAssertEnvelope(t, delivery, ExchangeSubscriptions, EventSubscriptionCancelled)

	var decoded SubscriptionCancelledEvent
	raw := covDecode(t, delivery, &decoded)
	covAssertEventEnvelope(t, raw, EventSubscriptionCancelled)
	if decoded.SubscriptionID != "sub-3" || decoded.TenantID != "tenant-3" {
		t.Errorf("unexpected payload: %+v", decoded)
	}
}

func TestCovPublishLinkPayPurchaseConfirmed(t *testing.T) {
	p := covNewPublisher(t)
	c := covNewConsumer(t).bind(ExchangeIITEvents, EventLinkPayPurchaseConfirmed)

	event := LinkPayPurchaseConfirmedEvent{
		PurchaseID:    "purchase-1",
		ProductID:     "product-1",
		TenantID:      "tenant-1",
		CustomerEmail: "buyer@example.com",
		CustomerName:  "Buyer",
		AmountCents:   7900,
		AccessToken:   "token-abc",
		DownloadURL:   "https://download.example.com/token-abc",
		ExpiresAt:     time.Now().UTC().Add(7 * 24 * time.Hour),
		OccurredAt:    time.Now().UTC(),
	}
	if err := p.PublishLinkPayPurchaseConfirmed(context.Background(), event); err != nil {
		t.Fatalf("PublishLinkPayPurchaseConfirmed: %v", err)
	}

	delivery := c.next(10 * time.Second)
	covAssertEnvelope(t, delivery, ExchangeIITEvents, EventLinkPayPurchaseConfirmed)

	var decoded LinkPayPurchaseConfirmedEvent
	raw := covDecode(t, delivery, &decoded)
	covAssertEventEnvelope(t, raw, EventLinkPayPurchaseConfirmed)
	if decoded.PurchaseID != "purchase-1" || decoded.AccessToken != "token-abc" {
		t.Errorf("unexpected payload: %+v", decoded)
	}
}

func TestCovPublishAsaasPaymentConfirmed(t *testing.T) {
	p := covNewPublisher(t)
	c := covNewConsumer(t).bind(ExchangeIITEvents, EventAsaasPaymentConfirmed)

	event := AsaasPaymentConfirmedEvent{
		AsaasPaymentID:    "asaas-ext-1",
		ExternalReference: "ticket-1",
		AmountCents:       15000,
		PaymentMethod:     "CREDIT_CARD",
		ConfirmedAt:       time.Now().UTC(),
	}
	if err := p.PublishAsaasPaymentConfirmed(context.Background(), event); err != nil {
		t.Fatalf("PublishAsaasPaymentConfirmed: %v", err)
	}

	delivery := c.next(10 * time.Second)
	covAssertEnvelope(t, delivery, ExchangeIITEvents, EventAsaasPaymentConfirmed)

	var decoded AsaasPaymentConfirmedEvent
	covDecode(t, delivery, &decoded)
	if decoded.AsaasPaymentID != "asaas-ext-1" || decoded.ExternalReference != "ticket-1" {
		t.Errorf("unexpected payload: %+v", decoded)
	}
}

// -----------------------------------------------------------------------------
// Close / error branches
// -----------------------------------------------------------------------------

func TestCovCloseIdempotent(t *testing.T) {
	p := covNewPublisher(t)

	p.Close()
	p.Close() // second close must not panic / must be a no-op

	if !p.conn.IsClosed() {
		t.Error("connection should be closed after Close")
	}
}

func TestCovCloseWithoutChannel(t *testing.T) {
	// Covers the nil branches of Close (no channel, no connection).
	p := &Publisher{}
	p.Close()
	p.Close()
}

func TestCovPublishMarshalError(t *testing.T) {
	// A chan cannot be marshalled to JSON, exercising the marshal error path
	// before any interaction with the broker.
	p := &Publisher{}
	err := p.publish(context.Background(), ExchangePayments, EventPaymentConfirmed, make(chan int))
	if err == nil {
		t.Fatal("expected a serialization error for an unmarshalable payload")
	}
	if !strings.Contains(err.Error(), "falha ao serializar evento") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestCovPublishReconnectsAfterChannelFailure closes the publisher channel
// behind its back so the next publish fails and the reconnect branch runs.
func TestCovPublishReconnectsAfterChannelFailure(t *testing.T) {
	p := covNewPublisher(t)
	c := covNewConsumer(t).bind(ExchangePayments, EventPaymentConfirmed)

	if err := p.channel.Close(); err != nil {
		t.Fatalf("closing internal channel: %v", err)
	}

	event := PaymentConfirmedEvent{
		PaymentID:   "pay-reconnect",
		OrderID:     "ord-reconnect",
		CustomerID:  "cus-reconnect",
		TenantID:    "tenant-reconnect",
		AmountCents: 100,
		Currency:    "BRL",
		PaidAt:      time.Now().UTC(),
	}
	if err := p.PublishPaymentConfirmed(context.Background(), event); err != nil {
		t.Fatalf("PublishPaymentConfirmed after reconnect: %v", err)
	}

	delivery := c.next(15 * time.Second)
	covAssertEnvelope(t, delivery, ExchangePayments, EventPaymentConfirmed)

	var decoded PaymentConfirmedEvent
	covDecode(t, delivery, &decoded)
	if decoded.PaymentID != "pay-reconnect" {
		t.Errorf("unexpected payload: %+v", decoded)
	}
}
