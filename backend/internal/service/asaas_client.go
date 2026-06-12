package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/sony/gobreaker"

	"linkpay-service/internal/domain"
	"linkpay-service/internal/pkg/pulse"
)

const (
	httpTimeout    = 10 * time.Second
	maxRetries     = 3
	cbInterval     = 30 * time.Second
	cbTimeout      = 60 * time.Second
	cbMinFailures  = 5
)

type retryableError struct {
	status int
	body   string
}

func (e *retryableError) Error() string {
	return fmt.Sprintf("asaas 5xx: status=%d body=%s", e.status, e.body)
}

type AsaasClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
	cb         *gobreaker.CircuitBreaker
	pulse      *pulse.Client
	logger     *slog.Logger
}

func NewAsaasClient(baseURL, apiKey string, logger *slog.Logger, pulseClient *pulse.Client) *AsaasClient {
	if logger == nil {
		logger = slog.Default()
	}

	cb := gobreaker.NewCircuitBreaker(gobreaker.Settings{
		Name:        "asaas-api",
		MaxRequests: 1,
		Interval:    cbInterval,
		Timeout:     cbTimeout,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.Requests >= cbMinFailures && counts.TotalFailures >= cbMinFailures
		},
		OnStateChange: func(name string, from, to gobreaker.State) {
			logger.Warn("circuit breaker state change",
				"from", from.String(),
				"to", to.String(),
			)
		},
	})

	return &AsaasClient{
		baseURL: baseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: httpTimeout,
		},
		cb:     cb,
		pulse:  pulseClient,
		logger: logger,
	}
}

// --- Tipos internos ---

type AsaasCustomer struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	CPF   string `json:"cpfCnpj"`
}

type AsaasPaymentRequest struct {
	Customer          string  `json:"customer"`
	BillingType       string  `json:"billingType"`
	Value             float64 `json:"value"`
	DueDate           string  `json:"dueDate"`
	Description       string  `json:"description"`
	ExternalReference string  `json:"externalReference"`
}

type AsaasPixTransaction struct {
	EncodedImage string `json:"encodedImage"`
	Payload      string `json:"payload"`
}

type AsaasPaymentResponse struct {
	ID             string               `json:"id"`
	Status         string               `json:"status"`
	InvoiceURL     string               `json:"invoiceUrl"`
	PixTransaction *AsaasPixTransaction `json:"pixTransaction,omitempty"`
}

type AsaasTransferRequest struct {
	Value             float64           `json:"value"`
	BankAccount       *AsaasBankAccount `json:"bankAccount,omitempty"`
	PixAddressKey     string            `json:"pixAddressKey,omitempty"`
	PixAddressKeyType string            `json:"pixAddressKeyType,omitempty"`
	Description       string            `json:"description,omitempty"`
	ExternalReference string            `json:"externalReference,omitempty"`
	ScheduleDate      string            `json:"scheduleDate,omitempty"`
}

func DetectPixKeyType(key string) string {
	if len(key) == 0 {
		return "EVP"
	}
	allDigits := true
	for _, c := range key {
		if c < '0' || c > '9' {
			allDigits = false
			break
		}
	}
	if allDigits {
		switch len(key) {
		case 11:
			return "CPF"
		case 14:
			return "CNPJ"
		}
	}
	if key[0] == '+' {
		return "PHONE"
	}
	for _, c := range key {
		if c == '@' {
			return "EMAIL"
		}
	}
	return "EVP"
}

type AsaasBankAccount struct {
	Bank        string `json:"bank"`
	AccountName string `json:"accountName"`
	Agency      string `json:"agency"`
	Account     string `json:"account"`
	AccountType string `json:"accountType"`
	CPF         string `json:"cpfCnpj"`
}

type AsaasTransferResponse struct {
	ID     string  `json:"id"`
	Status string  `json:"status"`
	Value  float64 `json:"value"`
}

type AsaasSubscriptionRequest struct {
	Customer          string  `json:"customer"`
	BillingType       string  `json:"billingType"`
	Value             float64 `json:"value"`
	NextDueDate       string  `json:"nextDueDate"`
	Cycle             string  `json:"cycle"`
	Description       string  `json:"description,omitempty"`
	ExternalReference string  `json:"externalReference,omitempty"`
}

type AsaasSubscriptionResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// --- Métodos públicos ---

func (c *AsaasClient) CreateCustomer(name, email, cpf string) (*AsaasCustomer, error) {
	payload := map[string]string{
		"name":     name,
		"email":    email,
		"cpfCnpj": cpf,
	}
	body, _ := json.Marshal(payload)

	resp, err := c.doWithRetry(func() (*http.Response, error) {
		return c.post("/customers", body)
	})
	if err != nil {
		return nil, fmt.Errorf("asaas CreateCustomer: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("asaas CreateCustomer: status=%d body=%s", resp.StatusCode, string(raw))
	}

	var customer AsaasCustomer
	if err := json.NewDecoder(resp.Body).Decode(&customer); err != nil {
		return nil, fmt.Errorf("asaas CreateCustomer decode: %w", err)
	}
	return &customer, nil
}

func (c *AsaasClient) createRawPayment(req AsaasPaymentRequest) (*AsaasPaymentResponse, error) {
	body, _ := json.Marshal(req)

	resp, err := c.doWithRetry(func() (*http.Response, error) {
		return c.post("/payments", body)
	})
	if err != nil {
		return nil, fmt.Errorf("asaas createRawPayment: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("asaas createRawPayment: status=%d body=%s", resp.StatusCode, string(raw))
	}

	var payment AsaasPaymentResponse
	if err := json.NewDecoder(resp.Body).Decode(&payment); err != nil {
		return nil, fmt.Errorf("asaas createRawPayment decode: %w", err)
	}
	return &payment, nil
}

func (c *AsaasClient) CreateTransfer(req AsaasTransferRequest) (*AsaasTransferResponse, error) {
	body, _ := json.Marshal(req)

	resp, err := c.doWithRetry(func() (*http.Response, error) {
		return c.post("/transfers", body)
	})
	if err != nil {
		return nil, fmt.Errorf("asaas CreateTransfer: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("asaas CreateTransfer: status=%d body=%s", resp.StatusCode, string(raw))
	}

	var transfer AsaasTransferResponse
	if err := json.NewDecoder(resp.Body).Decode(&transfer); err != nil {
		return nil, fmt.Errorf("asaas CreateTransfer decode: %w", err)
	}
	return &transfer, nil
}

func (c *AsaasClient) createLinkPaySubscription(customerID, externalRef string, plan *domain.DigitalSubscriptionPlan, nextDue time.Time) (*AsaasSubscriptionResponse, error) {
	cycle := "MONTHLY"
	if plan.Interval == "yearly" {
		cycle = "YEARLY"
	}

	req := AsaasSubscriptionRequest{
		Customer:          customerID,
		BillingType:       "PIX",
		Value:             float64(plan.PriceCents) / 100.0,
		NextDueDate:       nextDue.Format("2006-01-02"),
		Cycle:             cycle,
		Description:       plan.Name,
		ExternalReference: externalRef,
	}
	body, _ := json.Marshal(req)

	resp, err := c.doWithRetry(func() (*http.Response, error) {
		return c.post("/subscriptions", body)
	})
	if err != nil {
		return nil, fmt.Errorf("asaas createLinkPaySubscription: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("asaas createLinkPaySubscription: status=%d body=%s", resp.StatusCode, string(raw))
	}

	var sub AsaasSubscriptionResponse
	if err := json.NewDecoder(resp.Body).Decode(&sub); err != nil {
		return nil, fmt.Errorf("asaas createLinkPaySubscription decode: %w", err)
	}
	return &sub, nil
}

func (c *AsaasClient) CancelSubscription(asaasSubID string) error {
	url := c.baseURL + "/subscriptions/" + asaasSubID

	resp, err := c.doWithRetry(func() (*http.Response, error) {
		req, reqErr := http.NewRequest(http.MethodDelete, url, nil)
		if reqErr != nil {
			return nil, reqErr
		}
		req.Header.Set("access_token", c.apiKey)
		return c.httpClient.Do(req)
	})
	if err != nil {
		return fmt.Errorf("asaas CancelSubscription: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("asaas CancelSubscription: status=%d body=%s", resp.StatusCode, string(raw))
	}
	return nil
}

// RefundPayment solicita o estorno de uma cobrança no Asaas.
// POST /payments/{paymentId}/refund
func (c *AsaasClient) RefundPayment(paymentID string) error {
	url := c.baseURL + "/payments/" + paymentID + "/refund"
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return fmt.Errorf("asaas RefundPayment: %w", err)
	}
	req.Header.Set("access_token", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("asaas RefundPayment: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("asaas RefundPayment: status=%d body=%s", resp.StatusCode, string(raw))
	}
	return nil
}

// --- Helpers HTTP ---

func (c *AsaasClient) post(path string, body []byte) (*http.Response, error) {
	url := c.baseURL + path
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("access_token", c.apiKey)
	return c.httpClient.Do(req)
}

func (c *AsaasClient) doWithRetry(reqFn func() (*http.Response, error)) (*http.Response, error) {
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(100*(1<<attempt)) * time.Millisecond
			time.Sleep(backoff)
		}

		result, cbErr := c.cb.Execute(func() (interface{}, error) {
			resp, reqErr := reqFn()
			if reqErr != nil {
				return nil, reqErr
			}

			if resp.StatusCode >= 500 {
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				return nil, &retryableError{status: resp.StatusCode, body: string(body)}
			}

			return resp, nil
		})

		if cbErr == nil {
			return result.(*http.Response), nil
		}

		lastErr = cbErr

		var retryErr *retryableError
		if errors.As(cbErr, &retryErr) {
			c.logger.Warn("asaas retryable error",
				"attempt", attempt+1,
				"max_attempts", maxRetries,
				"status", retryErr.status,
			)
			continue
		}

		if errors.Is(cbErr, gobreaker.ErrOpenState) {
			c.logger.Warn("asaas circuit breaker open — fail fast",
				"attempt", attempt+1,
			)
		}
		break
	}

	c.trackFailure(lastErr)

	return nil, fmt.Errorf("asaas request failed after %d retries: %w", maxRetries, lastErr)
}

func (c *AsaasClient) trackFailure(err error) {
	if c.pulse == nil {
		return
	}

	errMsg := err.Error()
	errType := "unknown"
	switch {
	case errors.Is(err, gobreaker.ErrOpenState):
		errType = "circuit_breaker_open"
	case strings.Contains(errMsg, "5xx"):
		errType = "5xx_retries_exhausted"
	case strings.Contains(errMsg, "timeout") || strings.Contains(errMsg, "deadline exceeded"):
		errType = "timeout"
	}

	c.pulse.Track(context.Background(), "asaas.failure", "", map[string]string{
		"error_type": errType,
		"error":      truncateString(errMsg, 200),
	})
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
