package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"linkpay-service/internal/domain"
)

// AsaasClient encapsula as chamadas HTTP à API Asaas (pagamentos, repasses, assinaturas).
type AsaasClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
	logger     *slog.Logger
}

// NewAsaasClient cria um AsaasClient configurado com baseURL e apiKey.
func NewAsaasClient(baseURL, apiKey string, logger *slog.Logger) *AsaasClient {
	if logger == nil {
		logger = slog.Default()
	}
	return &AsaasClient{
		baseURL: baseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: logger,
	}
}

// --- Tipos internos ---

// AsaasCustomer representa a resposta de criação de customer no Asaas.
type AsaasCustomer struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	CPF   string `json:"cpfCnpj"`
}

// AsaasPaymentRequest monta o payload de criação de cobrança avulsa.
type AsaasPaymentRequest struct {
	Customer          string  `json:"customer"`
	BillingType       string  `json:"billingType"`
	Value             float64 `json:"value"`
	DueDate           string  `json:"dueDate"`
	Description       string  `json:"description"`
	ExternalReference string  `json:"externalReference"`
}

// AsaasPixTransaction contém os dados do QR Code Pix.
type AsaasPixTransaction struct {
	EncodedImage string `json:"encodedImage"`
	Payload      string `json:"payload"`
}

// AsaasPaymentResponse é a resposta de criação de cobrança.
type AsaasPaymentResponse struct {
	ID             string               `json:"id"`
	Status         string               `json:"status"`
	InvoiceURL     string               `json:"invoiceUrl"`
	PixTransaction *AsaasPixTransaction `json:"pixTransaction,omitempty"`
}

// AsaasTransferRequest monta o payload de criação de transferência (repasse).
type AsaasTransferRequest struct {
	Value             float64           `json:"value"`
	BankAccount       *AsaasBankAccount `json:"bankAccount,omitempty"`
	PixAddressKey     string            `json:"pixAddressKey,omitempty"`
	PixAddressKeyType string            `json:"pixAddressKeyType,omitempty"`
	Description       string            `json:"description,omitempty"`
	ExternalReference string            `json:"externalReference,omitempty"`
	ScheduleDate      string            `json:"scheduleDate,omitempty"`
}

// DetectPixKeyType infere o tipo de chave Pix a partir do valor informado.
func DetectPixKeyType(key string) string {
	if len(key) == 0 {
		return "EVP"
	}
	// CPF/CNPJ — apenas dígitos, 11 ou 14 chars
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
	// Telefone: começa com +
	if key[0] == '+' {
		return "PHONE"
	}
	// Email: contém @
	for _, c := range key {
		if c == '@' {
			return "EMAIL"
		}
	}
	return "EVP"
}

// AsaasBankAccount representa dados bancários para TED.
type AsaasBankAccount struct {
	Bank        string `json:"bank"`
	AccountName string `json:"accountName"`
	Agency      string `json:"agency"`
	Account     string `json:"account"`
	AccountType string `json:"accountType"`
	CPF         string `json:"cpfCnpj"`
}

// AsaasTransferResponse é a resposta de criação de transferência.
type AsaasTransferResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Value  float64 `json:"value"`
}

// AsaasSubscriptionRequest monta o payload de criação de assinatura recorrente.
type AsaasSubscriptionRequest struct {
	Customer          string  `json:"customer"`
	BillingType       string  `json:"billingType"`
	Value             float64 `json:"value"`
	NextDueDate       string  `json:"nextDueDate"`
	Cycle             string  `json:"cycle"` // MONTHLY | YEARLY
	Description       string  `json:"description,omitempty"`
	ExternalReference string  `json:"externalReference,omitempty"`
}

// AsaasSubscriptionResponse é a resposta de criação de assinatura.
type AsaasSubscriptionResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// --- Métodos ---

// CreateCustomer cadastra ou atualiza um customer no Asaas.
func (c *AsaasClient) CreateCustomer(name, email, cpf string) (*AsaasCustomer, error) {
	payload := map[string]string{
		"name":     name,
		"email":    email,
		"cpfCnpj": cpf,
	}
	body, _ := json.Marshal(payload)

	resp, err := c.post("/customers", body)
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

// createRawPayment cria uma cobrança avulsa no Asaas.
func (c *AsaasClient) createRawPayment(req AsaasPaymentRequest) (*AsaasPaymentResponse, error) {
	body, _ := json.Marshal(req)

	resp, err := c.post("/payments", body)
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

// CreateTransfer cria um repasse (transferência) no Asaas.
func (c *AsaasClient) CreateTransfer(req AsaasTransferRequest) (*AsaasTransferResponse, error) {
	body, _ := json.Marshal(req)

	resp, err := c.post("/transfers", body)
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

// createLinkPaySubscription cria uma assinatura recorrente no Asaas para um plano.
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

	resp, err := c.post("/subscriptions", body)
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

// CancelSubscription cancela uma assinatura no Asaas pelo ID externo.
func (c *AsaasClient) CancelSubscription(asaasSubID string) error {
	url := c.baseURL + "/subscriptions/" + asaasSubID
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("asaas CancelSubscription: %w", err)
	}
	req.Header.Set("access_token", c.apiKey)

	resp, err := c.httpClient.Do(req)
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
