package client

// NotificationClient integra com notification-service para envio de email pós-compra.

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

// NotificationClient envia notificações HTTP ao notification-service.
type NotificationClient struct {
	baseURL      string
	serviceToken string
	httpClient   *http.Client
}

// PurchaseConfirmedPayload é o corpo enviado ao notification-service para
// disparar o email de confirmação de compra com o link de download.
type PurchaseConfirmedPayload struct {
	BuyerEmail    string `json:"buyer_email"`
	BuyerName     string `json:"buyer_name"`
	ProductName   string `json:"product_name"`
	DownloadURL   string `json:"download_url"`
	DownloadLimit int    `json:"download_limit"`
}

// NewNotificationClient constrói o client a partir de NOTIFICATION_SERVICE_URL.
// Retorna nil quando a variável está ausente — fail-open para ambientes sem
// notification-service (dev/CI).
func NewNotificationClient() *NotificationClient {
	baseURL := os.Getenv("NOTIFICATION_SERVICE_URL")
	if baseURL == "" {
		return nil
	}
	return &NotificationClient{
		baseURL:      baseURL,
		serviceToken: os.Getenv("SERVICE_TOKEN"),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// SendPurchaseConfirmation dispara o email de confirmação no notification-service.
// Assinatura compatível com service.NotificationSender.
func (c *NotificationClient) SendPurchaseConfirmation(ctx context.Context, buyerEmail, buyerName, productName, downloadURL string, downloadLimit int) error {
	payload := PurchaseConfirmedPayload{
		BuyerEmail:    buyerEmail,
		BuyerName:     buyerName,
		ProductName:   productName,
		DownloadURL:   downloadURL,
		DownloadLimit: downloadLimit,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("notification_client: marshal: %w", err)
	}

	url := c.baseURL + "/api/v1/notifications/purchase-confirmed"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("notification_client: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Service-Token", c.serviceToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("notification_client: do: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("notification_client: status=%d body=%s", resp.StatusCode, string(raw))
	}
	return nil
}
