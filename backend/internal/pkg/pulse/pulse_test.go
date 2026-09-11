package pulse

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"
)

// TestClient_Track_PayloadContainsProduct garante que o payload enviado ao
// pulse-service sempre inclua o campo "product" com o valor fixo "linkpay".
// Regressão: BKL-1252 — sem esse campo o pulse-service normaliza para
// product="default", quebrando a atribuição de eventos por produto.
func TestClient_Track_PayloadContainsProduct(t *testing.T) {
	tests := []struct {
		name      string
		eventName string
		userID    string
		props     map[string]string
	}{
		{
			name:      "evento com user_id e properties",
			eventName: "purchase.completed",
			userID:    "user-123",
			props:     map[string]string{"amount": "100.00"},
		},
		{
			name:      "evento sem user_id (anonimo)",
			eventName: "landing_page.viewed",
			userID:    "",
			props:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var (
				mu      sync.Mutex
				gotBody map[string]interface{}
				gotPath string
			)

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				gotPath = r.URL.Path
				_ = json.NewDecoder(r.Body).Decode(&gotBody)
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			t.Setenv("PULSE_URL", server.URL)
			t.Setenv("SERVICE_TOKEN", "test-token")
			os.Unsetenv("PULSE_URL_UNUSED") // no-op, mantém padrão de limpeza

			client := New()
			client.Track(context.Background(), tt.eventName, tt.userID, tt.props)

			// Track é fire-and-forget (goroutine); aguarda a requisição chegar.
			deadline := time.Now().Add(2 * time.Second)
			for {
				mu.Lock()
				received := gotBody != nil
				mu.Unlock()
				if received || time.Now().After(deadline) {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}

			mu.Lock()
			defer mu.Unlock()

			if gotPath != "/api/v1/events/track" {
				t.Fatalf("path esperado /api/v1/events/track, obtido %q", gotPath)
			}

			if gotBody == nil {
				t.Fatal("payload nao foi recebido pelo servidor de teste")
			}

			product, ok := gotBody["product"]
			if !ok {
				t.Fatal("campo 'product' ausente no payload enviado ao pulse-service")
			}
			if product != "linkpay" {
				t.Fatalf("product esperado 'linkpay', obtido %v", product)
			}

			if gotBody["event_name"] != tt.eventName {
				t.Fatalf("event_name esperado %q, obtido %v", tt.eventName, gotBody["event_name"])
			}
		})
	}
}
