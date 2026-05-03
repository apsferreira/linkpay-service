package pulse

// BKL-1252: Cliente Pulse para linkpay-service — fire-and-forget, nunca bloqueia o fluxo principal.

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"
)

// Client envia eventos de tracking para o pulse-service de forma fire-and-forget.
// Falhas de tracking nunca bloqueiam o fluxo principal.
type Client struct {
	baseURL      string
	serviceToken string
	httpClient   *http.Client
}

// New cria um Client a partir das variáveis de ambiente PULSE_URL e SERVICE_TOKEN.
func New() *Client {
	return &Client{
		baseURL:      getEnv("PULSE_URL", "http://pulse-service"),
		serviceToken: os.Getenv("SERVICE_TOKEN"),
		httpClient:   &http.Client{Timeout: 3 * time.Second},
	}
}

// Track envia um evento ao pulse-service de forma assíncrona (fire-and-forget).
// userID deve ser o ID autenticado do customer. Deixar vazio string se não disponível.
func (c *Client) Track(ctx context.Context, eventName, userID string, props map[string]string) {
	go func() {
		payload := map[string]interface{}{
			"event_name": eventName,
			"user_id":    userID,
			"properties": props,
		}
		body, err := json.Marshal(payload)
		if err != nil {
			return
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			c.baseURL+"/api/v1/events/track", bytes.NewReader(body))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Service-Token", c.serviceToken)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			log.Printf("[pulse] erro ao enviar evento %s: %v", eventName, err)
			return
		}
		resp.Body.Close()
	}()
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
