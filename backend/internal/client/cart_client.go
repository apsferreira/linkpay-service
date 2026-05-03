package client

// BKL-1221: CartClient integra com cart-service para criar carrinhos de upsell no checkout.

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

const defaultCartServiceURL = "http://cart-service"

// CartClient integra com cart-service para upsell no checkout.
type CartClient struct {
	baseURL      string
	serviceToken string
	httpClient   *http.Client
}

// CreateCartRequest é o payload enviado ao cart-service para criar um novo carrinho.
type CreateCartRequest struct {
	TenantID      string `json:"tenant_id"`
	CustomerID    string `json:"customer_id,omitempty"`
	SourceService string `json:"source_service"` // "linkpay"
	SourceContext string `json:"source_context"` // product slug
}

// AddItemRequest é o payload para adicionar um item ao carrinho.
type AddItemRequest struct {
	ProductType string  `json:"product_type"` // "digital_product"
	ProductID   string  `json:"product_id"`
	Quantity    int     `json:"quantity"`
	Price       float64 `json:"price"`
	Name        string  `json:"name"`
}

// CartItem representa um item dentro do carrinho retornado pelo cart-service.
type CartItem struct {
	ID          string  `json:"id"`
	ProductID   string  `json:"product_id"`
	ProductType string  `json:"product_type"`
	Name        string  `json:"name"`
	Quantity    int     `json:"quantity"`
	Price       float64 `json:"price"`
}

// CartResponse é a resposta do cart-service após criar/atualizar um carrinho.
type CartResponse struct {
	ID    string     `json:"id"`
	Items []CartItem `json:"items"`
	Total float64    `json:"total"`
}

// NewCartClient cria um CartClient lendo as configurações de variáveis de ambiente.
// CART_SERVICE_URL — URL base do cart-service (padrão: http://cart-service).
// SERVICE_TOKEN    — token de autenticação inter-serviço (header X-Service-Token).
func NewCartClient() *CartClient {
	baseURL := os.Getenv("CART_SERVICE_URL")
	if baseURL == "" {
		baseURL = defaultCartServiceURL
	}
	return &CartClient{
		baseURL:      baseURL,
		serviceToken: os.Getenv("SERVICE_TOKEN"),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// CreateCart cria um novo carrinho no cart-service associado a um produto.
func (c *CartClient) CreateCart(ctx context.Context, tenantID, customerID, productSlug string) (*CartResponse, error) {
	payload := CreateCartRequest{
		TenantID:      tenantID,
		CustomerID:    customerID,
		SourceService: "linkpay",
		SourceContext: productSlug,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("cart_client: marshal CreateCart: %w", err)
	}

	resp, err := c.post(ctx, "/api/v1/carts", body)
	if err != nil {
		return nil, fmt.Errorf("cart_client: CreateCart: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("cart_client: CreateCart status=%d body=%s", resp.StatusCode, string(raw))
	}

	var cart CartResponse
	if err := json.NewDecoder(resp.Body).Decode(&cart); err != nil {
		return nil, fmt.Errorf("cart_client: CreateCart decode: %w", err)
	}
	return &cart, nil
}

// AddItem adiciona um item a um carrinho existente no cart-service.
func (c *CartClient) AddItem(ctx context.Context, cartID string, item AddItemRequest) (*CartResponse, error) {
	body, err := json.Marshal(item)
	if err != nil {
		return nil, fmt.Errorf("cart_client: marshal AddItem: %w", err)
	}

	path := fmt.Sprintf("/api/v1/carts/%s/items", cartID)
	resp, err := c.post(ctx, path, body)
	if err != nil {
		return nil, fmt.Errorf("cart_client: AddItem: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("cart_client: AddItem status=%d body=%s", resp.StatusCode, string(raw))
	}

	var cart CartResponse
	if err := json.NewDecoder(resp.Body).Decode(&cart); err != nil {
		return nil, fmt.Errorf("cart_client: AddItem decode: %w", err)
	}
	return &cart, nil
}

// --- helpers ---

func (c *CartClient) post(ctx context.Context, path string, body []byte) (*http.Response, error) {
	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Service-Token", c.serviceToken)
	return c.httpClient.Do(req)
}
