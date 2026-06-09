package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const defaultNotificationServiceURL = "http://notification-service:3030"

// PurchaseConfirmedPayload é o payload enviado ao notification-service
// quando uma compra LinkPay é confirmada.
type PurchaseConfirmedPayload struct {
	BuyerEmail    string `json:"buyer_email"`
	CustomerName  string `json:"customer_name"`
	ProductName   string `json:"product_name"`
	DownloadURL   string `json:"download_url"`
	DownloadLimit int    `json:"download_limit"`
	PurchaseID    string `json:"purchase_id"`
	ProductID     string `json:"product_id"`
}

// NotificationClient envia notificações ao notification-service via HTTP.
type NotificationClient struct {
	baseURL      string
	serviceToken string
	httpClient   *http.Client
}

// NewNotificationClient cria um NotificationClient lendo as variáveis de ambiente.
// NOTIFICATION_SERVICE_URL — URL base do notification-service (padrão: http://notification-service:3030).
// SERVICE_TOKEN        — token de autenticação inter-serviço (header X-Service-Token).
func NewNotificationClient() *NotificationClient {
	baseURL := os.Getenv("NOTIFICATION_SERVICE_URL")
	if baseURL == "" {
		baseURL = defaultNotificationServiceURL
	}
	return &NotificationClient{
		baseURL:      baseURL,
		serviceToken: os.Getenv("SERVICE_TOKEN"),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// SendPurchaseConfirmed notifica o notification-service sobre uma compra confirmada.
// Falha é retornada para que o caller decida se deve logar ou abortar.
func (c *NotificationClient) SendPurchaseConfirmed(ctx context.Context, buyerEmail, customerName, productName, downloadURL string, downloadLimit int, purchaseID, productID string) error {
	payload := PurchaseConfirmedPayload{
		BuyerEmail:    buyerEmail,
		CustomerName:  customerName,
		ProductName:   productName,
		DownloadURL:   downloadURL,
		DownloadLimit: downloadLimit,
		PurchaseID:    purchaseID,
		ProductID:     productID,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("notification_client: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/notifications/linkpay/purchase-confirmed", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("notification_client: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.serviceToken != "" {
		req.Header.Set("X-Service-Token", c.serviceToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("notification_client: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("notification_client: status=%d body=%s", resp.StatusCode, string(raw))
	}
	return nil
}
