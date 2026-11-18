package handler

// Testes de cobertura (issue #5) para os endpoints do LinkPay.
// Arquivo novo — não substitui nem altera os testes existentes.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"linkpay-service/internal/domain"
	"linkpay-service/internal/pkg/pulse"
	"linkpay-service/internal/storage"
)

const (
	covTenantUUID  = "11111111-1111-1111-1111-111111111111"
	covWebhookKey  = "cov-webhook-secret"
	covProductUUID = "22222222-2222-2222-2222-222222222222"
)

// covLinkPayStub implementa LinkPayServiceInterface com resultados configuráveis.
type covLinkPayStub struct {
	createProductResult *domain.DigitalProduct
	createProductErr    error

	getBySlugResult *domain.DigitalProduct
	getBySlugErr    error

	getByIDResult *domain.DigitalProduct
	getByIDErr    error
	productsByID  map[string]*domain.DigitalProduct

	listProductsResult []domain.DigitalProduct
	listProductsErr    error

	updateProductResult *domain.DigitalProduct
	updateProductErr    error

	updateStorageErr error
	deactivateErr    error

	initiateResult *domain.DigitalPurchase
	initiateErr    error

	processCalls int
	processErr   error

	downloadResult string
	downloadErr    error

	statsResult *domain.LinkPayStatsResponse
	statsErr    error

	addFileErr    error
	filesResult   []domain.DigitalProductFile
	filesErr      error
	getFileResult *domain.DigitalProductFile
	getFileErr    error
	deleteFileErr error

	purchaseResult *domain.DigitalPurchase
	purchaseErr    error

	refundErr error

	upsellsResult []domain.UpsellSuggestion
	upsellsErr    error

	updateCoverErr error

	commissionErr error
}

func (s *covLinkPayStub) CreateProduct(context.Context, string, *domain.CreateDigitalProductRequest) (*domain.DigitalProduct, error) {
	return s.createProductResult, s.createProductErr
}

func (s *covLinkPayStub) GetProductBySlug(context.Context, string) (*domain.DigitalProduct, error) {
	return s.getBySlugResult, s.getBySlugErr
}

func (s *covLinkPayStub) GetProductByID(_ context.Context, id, _ string) (*domain.DigitalProduct, error) {
	if p, ok := s.productsByID[id]; ok {
		return p, nil
	}
	return s.getByIDResult, s.getByIDErr
}

func (s *covLinkPayStub) ListProducts(context.Context, string) ([]domain.DigitalProduct, error) {
	return s.listProductsResult, s.listProductsErr
}

func (s *covLinkPayStub) UpdateProduct(context.Context, string, string, *domain.UpdateDigitalProductRequest) (*domain.DigitalProduct, error) {
	return s.updateProductResult, s.updateProductErr
}

func (s *covLinkPayStub) UpdateProductStorage(context.Context, string, string, string, string, string, int64, string) error {
	return s.updateStorageErr
}

func (s *covLinkPayStub) DeactivateProduct(context.Context, string, string) error {
	return s.deactivateErr
}

func (s *covLinkPayStub) InitiatePurchase(context.Context, string, *domain.InitiatePurchaseRequest) (*domain.DigitalPurchase, error) {
	return s.initiateResult, s.initiateErr
}

func (s *covLinkPayStub) ProcessPaymentConfirmed(context.Context, string) error {
	s.processCalls++
	return s.processErr
}

func (s *covLinkPayStub) Download(context.Context, string) (string, error) {
	return s.downloadResult, s.downloadErr
}

func (s *covLinkPayStub) GetSalesStats(context.Context, string) (*domain.LinkPayStatsResponse, error) {
	return s.statsResult, s.statsErr
}

func (s *covLinkPayStub) AddProductFile(context.Context, *domain.DigitalProductFile) error {
	return s.addFileErr
}

func (s *covLinkPayStub) ListProductFiles(context.Context, string) ([]domain.DigitalProductFile, error) {
	return s.filesResult, s.filesErr
}

func (s *covLinkPayStub) GetProductFile(context.Context, string) (*domain.DigitalProductFile, error) {
	return s.getFileResult, s.getFileErr
}

func (s *covLinkPayStub) DeleteProductFile(context.Context, string, string) error {
	return s.deleteFileErr
}

func (s *covLinkPayStub) GetPurchaseByToken(context.Context, string) (*domain.DigitalPurchase, error) {
	return s.purchaseResult, s.purchaseErr
}

func (s *covLinkPayStub) RefundPurchase(context.Context, string, string) error {
	return s.refundErr
}

func (s *covLinkPayStub) GetUpsellsByProductID(context.Context, string) ([]domain.UpsellSuggestion, error) {
	return s.upsellsResult, s.upsellsErr
}

func (s *covLinkPayStub) UpdateProductCover(context.Context, string, string, string) error {
	return s.updateCoverErr
}

func (s *covLinkPayStub) GetCommissionSummary(context.Context, string, string) (*domain.CommissionSummaryResponse, error) {
	if s.commissionErr != nil {
		return nil, s.commissionErr
	}
	return &domain.CommissionSummaryResponse{PeriodStart: "2026-01-01", PeriodEnd: "2026-01-31"}, nil
}

// --- helpers ---

func covNewLinkPay(t *testing.T, svc *covLinkPayStub, minio *storage.MinIOClient) *LinkPayHandler {
	t.Helper()
	return covNewLinkPayWithCart(t, svc, minio, "http://127.0.0.1:1")
}

func covNewLinkPayWithCart(t *testing.T, svc *covLinkPayStub, minio *storage.MinIOClient, cartURL string) *LinkPayHandler {
	t.Helper()
	t.Setenv("CART_SERVICE_URL", cartURL)
	t.Setenv("PULSE_URL", "http://127.0.0.1:1")
	return NewLinkPayHandler(svc, covWebhookKey, minio, pulse.New())
}

// covTenantApp cria um app Fiber com middleware que injeta tenant_id no contexto.
func covTenantApp(tenant any, setup func(app *fiber.App)) *fiber.App {
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	if tenant != nil {
		app.Use(func(c *fiber.Ctx) error {
			c.Locals("tenant_id", tenant)
			c.Locals("customer_id", "cust-cov")
			return c.Next()
		})
	}
	setup(app)
	return app
}

func covDo(t *testing.T, app *fiber.App, req *http.Request) *http.Response {
	t.Helper()
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	return resp
}

func covJSONReq(t *testing.T, app *fiber.App, method, path, body string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return covDo(t, app, req)
}

func covDecode(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return out
}

func covMultipart(t *testing.T, field, filename string, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	buf := &bytes.Buffer{}
	w := multipart.NewWriter(buf)
	part, err := w.CreateFormFile(field, filename)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("part.Write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("multipart close: %v", err)
	}
	return buf, w.FormDataContentType()
}

func covDoMultipart(t *testing.T, app *fiber.App, path, field, filename string, content []byte) *http.Response {
	t.Helper()
	buf, ct := covMultipart(t, field, filename, content)
	req := httptest.NewRequest(http.MethodPost, path, buf)
	req.Header.Set("Content-Type", ct)
	return covDo(t, app, req)
}

func covPDFBytes() []byte {
	return []byte("%PDF-1.4\n1 0 obj\n<<>>\nendobj\ntrailer\n<<>>\n%%EOF\n")
}

func covPNGBytes() []byte {
	b := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00")
	return append(b, bytes.Repeat([]byte{0x00}, 16)...)
}

// covFakeS3 expõe um endpoint S3 mínimo (PUT/GET/DELETE) para exercitar o MinIO client.
func covFakeS3(t *testing.T) (*storage.MinIOClient, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// minio-go consulta a região do bucket antes de assinar requisições.
		if _, ok := r.URL.Query()["location"]; ok {
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/">us-east-1</LocationConstraint>`))
			return
		}
		switch r.Method {
		case http.MethodPut:
			_, _ = io.Copy(io.Discard, r.Body)
			w.Header().Set("ETag", `"cov-etag"`)
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			body := []byte("cov-file-content")
			w.Header().Set("Content-Type", "application/pdf")
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("ETag", `"cov-etag"`)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(body)
		case http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Setenv("MINIO_ENDPOINT", strings.TrimPrefix(srv.URL, "http://"))
	t.Setenv("MINIO_ACCESS_KEY", "cov-access")
	t.Setenv("MINIO_SECRET_KEY", "cov-secret")
	t.Setenv("MINIO_USE_SSL", "false")
	client, err := storage.NewMinIOClient()
	if err != nil || client == nil {
		srv.Close()
		t.Fatalf("NewMinIOClient: client=%v err=%v", client, err)
	}
	return client, srv
}

func covUnconfiguredMinio() *storage.MinIOClient { return &storage.MinIOClient{} }

// ─── extractTenantID ─────────────────────────────────────────────────────────

func TestCovExtractTenantID_AllBranches(t *testing.T) {
	valid := uuid.MustParse(covTenantUUID)

	cases := []struct {
		name     string
		local    any
		wantErr  bool
		wantUUID uuid.UUID
	}{
		{"uuid value", valid, false, valid},
		{"valid string", covTenantUUID, false, valid},
		{"invalid string", "not-a-uuid", true, uuid.Nil},
		{"unexpected type", 42, true, uuid.Nil},
		{"absent", nil, true, uuid.Nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := fiber.New(fiber.Config{DisableStartupMessage: true})
			if tc.local != nil {
				app.Use(func(c *fiber.Ctx) error {
					c.Locals("tenant_id", tc.local)
					return c.Next()
				})
			}
			app.Get("/tenant", func(c *fiber.Ctx) error {
				id, err := extractTenantID(c)
				if err != nil {
					return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": err.Error()})
				}
				return c.JSON(fiber.Map{"id": id.String()})
			})

			resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/tenant", nil))
			defer resp.Body.Close()
			if tc.wantErr {
				if resp.StatusCode != fiber.StatusUnauthorized {
					t.Fatalf("status = %d, queria 401", resp.StatusCode)
				}
				return
			}
			if resp.StatusCode != fiber.StatusOK {
				t.Fatalf("status = %d, queria 200", resp.StatusCode)
			}
			var body map[string]string
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body["id"] != tc.wantUUID.String() {
				t.Errorf("id = %q, queria %q", body["id"], tc.wantUUID.String())
			}
		})
	}
}

// ─── ListProducts ────────────────────────────────────────────────────────────

func TestCovListProducts(t *testing.T) {
	t.Run("unauthorized", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{}, nil)
		app := covTenantApp(nil, func(app *fiber.App) { app.Get("/products", h.ListProducts) })
		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/products", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusUnauthorized {
			t.Fatalf("status = %d, queria 401", resp.StatusCode)
		}
	})

	t.Run("service error", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{listProductsErr: errors.New("db down")}, nil)
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Get("/products", h.ListProducts) })
		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/products", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusInternalServerError {
			t.Fatalf("status = %d, queria 500", resp.StatusCode)
		}
	})

	t.Run("nil becomes empty list", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{}, nil)
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Get("/products", h.ListProducts) })
		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/products", nil))
		body := covDecode(t, resp)
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("status = %d, queria 200", resp.StatusCode)
		}
		products, ok := body["products"].([]any)
		if !ok || len(products) != 0 {
			t.Errorf("products = %v, queria []", body["products"])
		}
	})

	t.Run("success", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{
			listProductsResult: []domain.DigitalProduct{{ID: "p1", Name: "Curso"}},
		}, nil)
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Get("/products", h.ListProducts) })
		body := covDecode(t, covDo(t, app, httptest.NewRequest(http.MethodGet, "/products", nil)))
		products, ok := body["products"].([]any)
		if !ok || len(products) != 1 {
			t.Fatalf("products = %v", body["products"])
		}
	})
}

// ─── CreateProduct ───────────────────────────────────────────────────────────

func TestCovCreateProduct(t *testing.T) {
	validBody := `{"slug":"curso-go","name":"Curso Go","price_cents":2990,"product_type":"course","download_limit":1}`

	t.Run("unauthorized", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{}, nil)
		app := covTenantApp(nil, func(app *fiber.App) { app.Post("/products", h.CreateProduct) })
		resp := covJSONReq(t, app, http.MethodPost, "/products", validBody)
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusUnauthorized {
			t.Fatalf("status = %d, queria 401", resp.StatusCode)
		}
	})

	t.Run("invalid body", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{}, nil)
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Post("/products", h.CreateProduct) })
		resp := covJSONReq(t, app, http.MethodPost, "/products", "{not-json")
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("validation failure", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{}, nil)
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Post("/products", h.CreateProduct) })
		resp := covJSONReq(t, app, http.MethodPost, "/products", `{"slug":"x"}`)
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("service error", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{createProductErr: errors.New("boom")}, nil)
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Post("/products", h.CreateProduct) })
		resp := covJSONReq(t, app, http.MethodPost, "/products", validBody)
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusInternalServerError {
			t.Fatalf("status = %d, queria 500", resp.StatusCode)
		}
	})

	t.Run("success", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{
			createProductResult: &domain.DigitalProduct{ID: covProductUUID, Slug: "curso-go"},
		}, nil)
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Post("/products", h.CreateProduct) })
		body := covDecode(t, covJSONReq(t, app, http.MethodPost, "/products", validBody))
		if body["slug"] != "curso-go" {
			t.Errorf("slug = %v", body["slug"])
		}
	})
}

// ─── UpdateProduct / DeleteProduct / GetProduct ──────────────────────────────

func TestCovUpdateProduct(t *testing.T) {
	validBody := `{"name":"Novo nome"}`

	t.Run("unauthorized", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{}, nil)
		app := covTenantApp(nil, func(app *fiber.App) { app.Put("/products/:id", h.UpdateProduct) })
		resp := covJSONReq(t, app, http.MethodPut, "/products/p1", validBody)
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusUnauthorized {
			t.Fatalf("status = %d, queria 401", resp.StatusCode)
		}
	})

	t.Run("missing id", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{}, nil)
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Put("/products", h.UpdateProduct) })
		resp := covJSONReq(t, app, http.MethodPut, "/products", validBody)
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("invalid body", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{}, nil)
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Put("/products/:id", h.UpdateProduct) })
		resp := covJSONReq(t, app, http.MethodPut, "/products/p1", "{oops")
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("validation failure", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{}, nil)
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Put("/products/:id", h.UpdateProduct) })
		resp := covJSONReq(t, app, http.MethodPut, "/products/p1", `{"price_cents":-5}`)
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("service error", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{updateProductErr: errors.New("boom")}, nil)
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Put("/products/:id", h.UpdateProduct) })
		resp := covJSONReq(t, app, http.MethodPut, "/products/p1", validBody)
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusInternalServerError {
			t.Fatalf("status = %d, queria 500", resp.StatusCode)
		}
	})

	t.Run("success", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{
			updateProductResult: &domain.DigitalProduct{ID: "p1", Name: "Novo nome"},
		}, nil)
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Put("/products/:id", h.UpdateProduct) })
		body := covDecode(t, covJSONReq(t, app, http.MethodPut, "/products/p1", validBody))
		if body["name"] != "Novo nome" {
			t.Errorf("name = %v", body["name"])
		}
	})
}

func TestCovDeleteProduct(t *testing.T) {
	t.Run("unauthorized", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{}, nil)
		app := covTenantApp(nil, func(app *fiber.App) { app.Delete("/products/:id", h.DeleteProduct) })
		resp := covDo(t, app, httptest.NewRequest(http.MethodDelete, "/products/p1", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusUnauthorized {
			t.Fatalf("status = %d, queria 401", resp.StatusCode)
		}
	})

	t.Run("missing id", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{}, nil)
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Delete("/products", h.DeleteProduct) })
		resp := covDo(t, app, httptest.NewRequest(http.MethodDelete, "/products", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("service error", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{deactivateErr: errors.New("boom")}, nil)
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Delete("/products/:id", h.DeleteProduct) })
		resp := covDo(t, app, httptest.NewRequest(http.MethodDelete, "/products/p1", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusInternalServerError {
			t.Fatalf("status = %d, queria 500", resp.StatusCode)
		}
	})

	t.Run("success and alias DeactivateProduct", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{}, nil)
		app := covTenantApp(covTenantUUID, func(app *fiber.App) {
			app.Delete("/products/:id", h.DeleteProduct)
			app.Delete("/alias/:id", h.DeactivateProduct)
		})
		body := covDecode(t, covDo(t, app, httptest.NewRequest(http.MethodDelete, "/products/p1", nil)))
		if body["message"] != "produto desativado" {
			t.Errorf("message = %v", body["message"])
		}
		resp := covDo(t, app, httptest.NewRequest(http.MethodDelete, "/alias/p1", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("alias status = %d, queria 200", resp.StatusCode)
		}
	})
}

func TestCovGetProduct(t *testing.T) {
	t.Run("unauthorized", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{}, nil)
		app := covTenantApp(nil, func(app *fiber.App) { app.Get("/products/:id", h.GetProduct) })
		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/products/p1", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusUnauthorized {
			t.Fatalf("status = %d, queria 401", resp.StatusCode)
		}
	})

	t.Run("missing id", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{}, nil)
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Get("/product", h.GetProduct) })
		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/product", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("not found", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{getByIDErr: errors.New("not found")}, nil)
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Get("/products/:id", h.GetProduct) })
		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/products/p1", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusNotFound {
			t.Fatalf("status = %d, queria 404", resp.StatusCode)
		}
	})

	t.Run("success", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{
			getByIDResult: &domain.DigitalProduct{ID: "p1", Name: "Curso"},
		}, nil)
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Get("/products/:id", h.GetProduct) })
		body := covDecode(t, covDo(t, app, httptest.NewRequest(http.MethodGet, "/products/p1", nil)))
		if body["name"] != "Curso" {
			t.Errorf("name = %v", body["name"])
		}
	})
}

// ─── UploadProductFile ───────────────────────────────────────────────────────

func TestCovUploadProductFile(t *testing.T) {
	present := &domain.DigitalProduct{ID: covProductUUID}

	t.Run("storage not configured (nil client)", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{getByIDResult: present}, nil)
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Post("/products/:id/upload", h.UploadProductFile) })
		resp := covDoMultipart(t, app, "/products/p1/upload", "file", "a.pdf", covPDFBytes())
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusServiceUnavailable {
			t.Fatalf("status = %d, queria 503", resp.StatusCode)
		}
	})

	t.Run("storage not configured (empty client)", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{getByIDResult: present}, covUnconfiguredMinio())
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Post("/products/:id/upload", h.UploadProductFile) })
		resp := covDoMultipart(t, app, "/products/p1/upload", "file", "a.pdf", covPDFBytes())
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusServiceUnavailable {
			t.Fatalf("status = %d, queria 503", resp.StatusCode)
		}
	})

	t.Run("unauthorized", func(t *testing.T) {
		minio, srv := covFakeS3(t)
		defer srv.Close()
		h := covNewLinkPay(t, &covLinkPayStub{getByIDResult: present}, minio)
		app := covTenantApp(nil, func(app *fiber.App) { app.Post("/products/:id/upload", h.UploadProductFile) })
		resp := covDoMultipart(t, app, "/products/p1/upload", "file", "a.pdf", covPDFBytes())
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusUnauthorized {
			t.Fatalf("status = %d, queria 401", resp.StatusCode)
		}
	})

	t.Run("missing product id", func(t *testing.T) {
		minio, srv := covFakeS3(t)
		defer srv.Close()
		h := covNewLinkPay(t, &covLinkPayStub{getByIDResult: present}, minio)
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Post("/upload", h.UploadProductFile) })
		resp := covDoMultipart(t, app, "/upload", "file", "a.pdf", covPDFBytes())
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("product not found", func(t *testing.T) {
		minio, srv := covFakeS3(t)
		defer srv.Close()
		h := covNewLinkPay(t, &covLinkPayStub{getByIDErr: errors.New("nope")}, minio)
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Post("/products/:id/upload", h.UploadProductFile) })
		resp := covDoMultipart(t, app, "/products/p1/upload", "file", "a.pdf", covPDFBytes())
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusNotFound {
			t.Fatalf("status = %d, queria 404", resp.StatusCode)
		}
	})

	t.Run("missing file field", func(t *testing.T) {
		minio, srv := covFakeS3(t)
		defer srv.Close()
		h := covNewLinkPay(t, &covLinkPayStub{getByIDResult: present}, minio)
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Post("/products/:id/upload", h.UploadProductFile) })
		buf, ct := covMultipart(t, "other", "a.pdf", covPDFBytes())
		req := httptest.NewRequest(http.MethodPost, "/products/p1/upload", buf)
		req.Header.Set("Content-Type", ct)
		resp := covDo(t, app, req)
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("mime not allowed", func(t *testing.T) {
		minio, srv := covFakeS3(t)
		defer srv.Close()
		h := covNewLinkPay(t, &covLinkPayStub{getByIDResult: present}, minio)
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Post("/products/:id/upload", h.UploadProductFile) })
		resp := covDoMultipart(t, app, "/products/p1/upload", "file", "a.txt", []byte("texto puro"))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("metadata error", func(t *testing.T) {
		minio, srv := covFakeS3(t)
		defer srv.Close()
		h := covNewLinkPay(t, &covLinkPayStub{getByIDResult: present, updateStorageErr: errors.New("db")}, minio)
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Post("/products/:id/upload", h.UploadProductFile) })
		resp := covDoMultipart(t, app, "/products/p1/upload", "file", "a.pdf", covPDFBytes())
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusInternalServerError {
			t.Fatalf("status = %d, queria 500", resp.StatusCode)
		}
	})

	t.Run("success", func(t *testing.T) {
		minio, srv := covFakeS3(t)
		defer srv.Close()
		h := covNewLinkPay(t, &covLinkPayStub{getByIDResult: present}, minio)
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Post("/products/:id/upload", h.UploadProductFile) })
		body := covDecode(t, covDoMultipart(t, app, "/products/p1/upload", "file", "a.pdf", covPDFBytes()))
		if body["uploaded"] != true {
			t.Errorf("uploaded = %v", body["uploaded"])
		}
		if body["mime_type"] != "application/pdf" {
			t.Errorf("mime_type = %v", body["mime_type"])
		}
	})
}

// ─── UploadProductCover ──────────────────────────────────────────────────────

func TestCovUploadProductCover(t *testing.T) {
	present := &domain.DigitalProduct{ID: covProductUUID}

	t.Run("storage not configured", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{getByIDResult: present}, nil)
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Post("/products/:id/cover", h.UploadProductCover) })
		resp := covDoMultipart(t, app, "/products/p1/cover", "cover", "c.png", covPNGBytes())
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusServiceUnavailable {
			t.Fatalf("status = %d, queria 503", resp.StatusCode)
		}
	})

	t.Run("unauthorized", func(t *testing.T) {
		minio, srv := covFakeS3(t)
		defer srv.Close()
		h := covNewLinkPay(t, &covLinkPayStub{getByIDResult: present}, minio)
		app := covTenantApp(nil, func(app *fiber.App) { app.Post("/products/:id/cover", h.UploadProductCover) })
		resp := covDoMultipart(t, app, "/products/p1/cover", "cover", "c.png", covPNGBytes())
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusUnauthorized {
			t.Fatalf("status = %d, queria 401", resp.StatusCode)
		}
	})

	t.Run("missing product id", func(t *testing.T) {
		minio, srv := covFakeS3(t)
		defer srv.Close()
		h := covNewLinkPay(t, &covLinkPayStub{getByIDResult: present}, minio)
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Post("/cover", h.UploadProductCover) })
		resp := covDoMultipart(t, app, "/cover", "cover", "c.png", covPNGBytes())
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("product not found", func(t *testing.T) {
		minio, srv := covFakeS3(t)
		defer srv.Close()
		h := covNewLinkPay(t, &covLinkPayStub{getByIDErr: errors.New("nope")}, minio)
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Post("/products/:id/cover", h.UploadProductCover) })
		resp := covDoMultipart(t, app, "/products/p1/cover", "cover", "c.png", covPNGBytes())
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusNotFound {
			t.Fatalf("status = %d, queria 404", resp.StatusCode)
		}
	})

	t.Run("missing cover field", func(t *testing.T) {
		minio, srv := covFakeS3(t)
		defer srv.Close()
		h := covNewLinkPay(t, &covLinkPayStub{getByIDResult: present}, minio)
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Post("/products/:id/cover", h.UploadProductCover) })
		buf, ct := covMultipart(t, "file", "c.png", covPNGBytes())
		req := httptest.NewRequest(http.MethodPost, "/products/p1/cover", buf)
		req.Header.Set("Content-Type", ct)
		resp := covDo(t, app, req)
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("non image mime", func(t *testing.T) {
		minio, srv := covFakeS3(t)
		defer srv.Close()
		h := covNewLinkPay(t, &covLinkPayStub{getByIDResult: present}, minio)
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Post("/products/:id/cover", h.UploadProductCover) })
		resp := covDoMultipart(t, app, "/products/p1/cover", "cover", "c.pdf", covPDFBytes())
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("cover persist error", func(t *testing.T) {
		minio, srv := covFakeS3(t)
		defer srv.Close()
		h := covNewLinkPay(t, &covLinkPayStub{getByIDResult: present, updateCoverErr: errors.New("db")}, minio)
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Post("/products/:id/cover", h.UploadProductCover) })
		resp := covDoMultipart(t, app, "/products/p1/cover", "cover", "c.png", covPNGBytes())
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusInternalServerError {
			t.Fatalf("status = %d, queria 500", resp.StatusCode)
		}
	})

	t.Run("success", func(t *testing.T) {
		minio, srv := covFakeS3(t)
		defer srv.Close()
		h := covNewLinkPay(t, &covLinkPayStub{getByIDResult: present}, minio)
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Post("/products/:id/cover", h.UploadProductCover) })
		body := covDecode(t, covDoMultipart(t, app, "/products/p1/cover", "cover", "c.png", covPNGBytes()))
		if body["uploaded"] != true {
			t.Errorf("uploaded = %v", body["uploaded"])
		}
		if body["cover_image_url"] == "" {
			t.Error("cover_image_url vazio")
		}
	})
}

// ─── GetPublicProduct ────────────────────────────────────────────────────────

func TestCovGetPublicProduct(t *testing.T) {
	t.Run("missing slug", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{}, nil)
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Get("/p", h.GetPublicProduct)
		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/p", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("not found", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{getBySlugErr: errors.New("nope")}, nil)
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Get("/products/:slug", h.GetPublicProduct)
		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/products/abc", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusNotFound {
			t.Fatalf("status = %d, queria 404", resp.StatusCode)
		}
	})

	t.Run("success with legacy cover and files error", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{
			getBySlugResult: &domain.DigitalProduct{
				ID: "p1", Slug: "abc", FileURL: "https://x/y.pdf",
				CoverImageURL: "https://cdn.example.com/cover.png",
			},
			filesErr: errors.New("db"),
		}, nil)
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Get("/products/:slug", h.GetPublicProduct)
		body := covDecode(t, covDo(t, app, httptest.NewRequest(http.MethodGet, "/products/abc", nil)))
		if _, has := body["file_url"]; has {
			t.Error("file_url deveria ser omitido na resposta pública")
		}
	})

	t.Run("success with storage key cover regenerated", func(t *testing.T) {
		minio, srv := covFakeS3(t)
		defer srv.Close()
		h := covNewLinkPay(t, &covLinkPayStub{
			getBySlugResult: &domain.DigitalProduct{
				ID: "p1", Slug: "abc", CoverImageURL: "tenant/products/p1/cover.png",
			},
			filesResult: []domain.DigitalProductFile{{ID: "f1", ProductID: "p1"}},
		}, minio)
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Get("/products/:slug", h.GetPublicProduct)
		body := covDecode(t, covDo(t, app, httptest.NewRequest(http.MethodGet, "/products/abc", nil)))
		if body["cover_image_url"] == "tenant/products/p1/cover.png" {
			t.Error("cover_image_url deveria ser a presigned URL")
		}
		files, ok := body["files"].([]any)
		if !ok || len(files) != 1 {
			t.Errorf("files = %v", body["files"])
		}
	})
}

// ─── InitiatePurchase ────────────────────────────────────────────────────────

func TestCovInitiatePurchase(t *testing.T) {
	validBody := `{"customer_name":"Ana","customer_email":"ana@example.com","payment_method":"pix"}`

	t.Run("missing slug", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{}, nil)
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Post("/purchase", h.InitiatePurchase)
		resp := covJSONReq(t, app, http.MethodPost, "/purchase", validBody)
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("invalid body", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{}, nil)
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Post("/products/:slug/purchase", h.InitiatePurchase)
		resp := covJSONReq(t, app, http.MethodPost, "/products/abc/purchase", "{oops")
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("validation failure", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{}, nil)
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Post("/products/:slug/purchase", h.InitiatePurchase)
		resp := covJSONReq(t, app, http.MethodPost, "/products/abc/purchase", `{"customer_name":"Ana"}`)
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("service error", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{initiateErr: errors.New("boom")}, nil)
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Post("/products/:slug/purchase", h.InitiatePurchase)
		resp := covJSONReq(t, app, http.MethodPost, "/products/abc/purchase", validBody)
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusInternalServerError {
			t.Fatalf("status = %d, queria 500", resp.StatusCode)
		}
	})

	t.Run("success", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{
			initiateResult: &domain.DigitalPurchase{ID: "pur1", ProductID: "p1", TenantID: covTenantUUID, PixQRCode: "qr"},
		}, nil)
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Post("/products/:slug/purchase", h.InitiatePurchase)
		body := covDecode(t, covJSONReq(t, app, http.MethodPost, "/products/abc/purchase", validBody))
		if body["pix_qr_code"] != "qr" {
			t.Errorf("pix_qr_code = %v", body["pix_qr_code"])
		}
	})
}

// ─── GetSalesStats / GetCommissionSummary ────────────────────────────────────

func TestCovGetSalesStats(t *testing.T) {
	t.Run("unauthorized", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{}, nil)
		app := covTenantApp(nil, func(app *fiber.App) { app.Get("/stats", h.GetSalesStats) })
		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/stats", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusUnauthorized {
			t.Fatalf("status = %d, queria 401", resp.StatusCode)
		}
	})

	t.Run("service error", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{statsErr: errors.New("boom")}, nil)
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Get("/stats", h.GetSalesStats) })
		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/stats", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusInternalServerError {
			t.Fatalf("status = %d, queria 500", resp.StatusCode)
		}
	})

	t.Run("success and alias GetStats", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{
			statsResult: &domain.LinkPayStatsResponse{Period: "30d", TotalSales: 3},
		}, nil)
		app := covTenantApp(covTenantUUID, func(app *fiber.App) {
			app.Get("/stats", h.GetSalesStats)
			app.Get("/alias", h.GetStats)
		})
		body := covDecode(t, covDo(t, app, httptest.NewRequest(http.MethodGet, "/stats", nil)))
		if body["total_sales"] != float64(3) {
			t.Errorf("total_sales = %v", body["total_sales"])
		}
		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/alias", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("alias status = %d", resp.StatusCode)
		}
	})
}

func TestCovGetCommissionSummary(t *testing.T) {
	h := covNewLinkPay(t, &covLinkPayStub{}, nil)
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	app.Get("/commission", h.GetCommissionSummary)

	check := func(t *testing.T, query string, want int) {
		t.Helper()
		path := "/commission"
		if query != "" {
			path += "?" + query
		}
		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, path, nil))
		defer resp.Body.Close()
		if resp.StatusCode != want {
			t.Fatalf("query %q: status = %d, queria %d", query, resp.StatusCode, want)
		}
	}

	check(t, "", fiber.StatusBadRequest)
	check(t, "start_date=2026-01-01", fiber.StatusBadRequest)
	check(t, "start_date=01-01-2026&end_date=2026-01-31", fiber.StatusBadRequest)
	check(t, "start_date=2026-01-01&end_date=31-01-2026", fiber.StatusBadRequest)

	body := covDecode(t, covDo(t, app, httptest.NewRequest(http.MethodGet, "/commission?start_date=2026-01-01&end_date=2026-01-31", nil)))
	if body["period_start"] != "2026-01-01" {
		t.Errorf("period_start = %v", body["period_start"])
	}

	errH := covNewLinkPay(t, &covLinkPayStub{commissionErr: errors.New("boom")}, nil)
	errApp := fiber.New(fiber.Config{DisableStartupMessage: true})
	errApp.Get("/commission", errH.GetCommissionSummary)
	resp := covDo(t, errApp, httptest.NewRequest(http.MethodGet, "/commission?start_date=2026-01-01&end_date=2026-01-31", nil))
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusInternalServerError {
		t.Fatalf("erro: status = %d, queria 500", resp.StatusCode)
	}
}

// ─── AddProductFile / ListProductFiles / DeleteProductFile ──────────────────

func TestCovAddProductFile(t *testing.T) {
	present := &domain.DigitalProduct{ID: covProductUUID}

	t.Run("storage not configured", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{getByIDResult: present}, nil)
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Post("/products/:id/files", h.AddProductFile) })
		resp := covDoMultipart(t, app, "/products/p1/files", "file", "a.pdf", covPDFBytes())
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusServiceUnavailable {
			t.Fatalf("status = %d, queria 503", resp.StatusCode)
		}
	})

	t.Run("unauthorized", func(t *testing.T) {
		minio, srv := covFakeS3(t)
		defer srv.Close()
		h := covNewLinkPay(t, &covLinkPayStub{getByIDResult: present}, minio)
		app := covTenantApp(nil, func(app *fiber.App) { app.Post("/products/:id/files", h.AddProductFile) })
		resp := covDoMultipart(t, app, "/products/p1/files", "file", "a.pdf", covPDFBytes())
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusUnauthorized {
			t.Fatalf("status = %d, queria 401", resp.StatusCode)
		}
	})

	t.Run("missing product id", func(t *testing.T) {
		minio, srv := covFakeS3(t)
		defer srv.Close()
		h := covNewLinkPay(t, &covLinkPayStub{getByIDResult: present}, minio)
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Post("/files", h.AddProductFile) })
		resp := covDoMultipart(t, app, "/files", "file", "a.pdf", covPDFBytes())
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("product not found", func(t *testing.T) {
		minio, srv := covFakeS3(t)
		defer srv.Close()
		h := covNewLinkPay(t, &covLinkPayStub{getByIDErr: errors.New("nope")}, minio)
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Post("/products/:id/files", h.AddProductFile) })
		resp := covDoMultipart(t, app, "/products/p1/files", "file", "a.pdf", covPDFBytes())
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusNotFound {
			t.Fatalf("status = %d, queria 404", resp.StatusCode)
		}
	})

	t.Run("missing file field", func(t *testing.T) {
		minio, srv := covFakeS3(t)
		defer srv.Close()
		h := covNewLinkPay(t, &covLinkPayStub{getByIDResult: present}, minio)
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Post("/products/:id/files", h.AddProductFile) })
		buf, ct := covMultipart(t, "other", "a.pdf", covPDFBytes())
		req := httptest.NewRequest(http.MethodPost, "/products/p1/files", buf)
		req.Header.Set("Content-Type", ct)
		resp := covDo(t, app, req)
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("upload rejected", func(t *testing.T) {
		minio, srv := covFakeS3(t)
		defer srv.Close()
		h := covNewLinkPay(t, &covLinkPayStub{getByIDResult: present}, minio)
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Post("/products/:id/files", h.AddProductFile) })
		resp := covDoMultipart(t, app, "/products/p1/files", "file", "a.txt", []byte("texto"))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("metadata error", func(t *testing.T) {
		minio, srv := covFakeS3(t)
		defer srv.Close()
		h := covNewLinkPay(t, &covLinkPayStub{getByIDResult: present, addFileErr: errors.New("db")}, minio)
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Post("/products/:id/files", h.AddProductFile) })
		resp := covDoMultipart(t, app, "/products/p1/files", "file", "a.pdf", covPDFBytes())
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusInternalServerError {
			t.Fatalf("status = %d, queria 500", resp.StatusCode)
		}
	})

	t.Run("success", func(t *testing.T) {
		minio, srv := covFakeS3(t)
		defer srv.Close()
		h := covNewLinkPay(t, &covLinkPayStub{getByIDResult: present}, minio)
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Post("/products/:id/files", h.AddProductFile) })
		body := covDecode(t, covDoMultipart(t, app, "/products/p1/files", "file", "a.pdf", covPDFBytes()))
		if body["file_name"] != "a.pdf" {
			t.Errorf("file_name = %v", body["file_name"])
		}
	})
}

func TestCovListProductFiles(t *testing.T) {
	present := &domain.DigitalProduct{ID: covProductUUID}

	t.Run("unauthorized", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{}, nil)
		app := covTenantApp(nil, func(app *fiber.App) { app.Get("/products/:id/files", h.ListProductFiles) })
		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/products/p1/files", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusUnauthorized {
			t.Fatalf("status = %d, queria 401", resp.StatusCode)
		}
	})

	t.Run("missing id", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{}, nil)
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Get("/files", h.ListProductFiles) })
		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/files", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("product not found", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{getByIDErr: errors.New("nope")}, nil)
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Get("/products/:id/files", h.ListProductFiles) })
		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/products/p1/files", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusNotFound {
			t.Fatalf("status = %d, queria 404", resp.StatusCode)
		}
	})

	t.Run("service error", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{getByIDResult: present, filesErr: errors.New("db")}, nil)
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Get("/products/:id/files", h.ListProductFiles) })
		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/products/p1/files", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusInternalServerError {
			t.Fatalf("status = %d, queria 500", resp.StatusCode)
		}
	})

	t.Run("nil files becomes empty list", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{getByIDResult: present}, nil)
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Get("/products/:id/files", h.ListProductFiles) })
		body := covDecode(t, covDo(t, app, httptest.NewRequest(http.MethodGet, "/products/p1/files", nil)))
		files, ok := body["files"].([]any)
		if !ok || len(files) != 0 {
			t.Errorf("files = %v", body["files"])
		}
	})

	t.Run("success", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{
			getByIDResult: present,
			filesResult:   []domain.DigitalProductFile{{ID: "f1"}, {ID: "f2"}},
		}, nil)
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Get("/products/:id/files", h.ListProductFiles) })
		body := covDecode(t, covDo(t, app, httptest.NewRequest(http.MethodGet, "/products/p1/files", nil)))
		files, ok := body["files"].([]any)
		if !ok || len(files) != 2 {
			t.Errorf("files = %v", body["files"])
		}
	})
}

func TestCovDeleteProductFile(t *testing.T) {
	present := &domain.DigitalProduct{ID: "p1"}
	fileOwned := &domain.DigitalProductFile{ID: "f1", ProductID: "p1", StorageKey: "tenant/products/p1/f.pdf"}

	t.Run("unauthorized", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{}, nil)
		app := covTenantApp(nil, func(app *fiber.App) { app.Delete("/products/:id/files/:fileId", h.DeleteProductFile) })
		resp := covDo(t, app, httptest.NewRequest(http.MethodDelete, "/products/p1/files/f1", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusUnauthorized {
			t.Fatalf("status = %d, queria 401", resp.StatusCode)
		}
	})

	t.Run("missing ids", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{}, nil)
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Delete("/files", h.DeleteProductFile) })
		resp := covDo(t, app, httptest.NewRequest(http.MethodDelete, "/files", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("product not found", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{getByIDErr: errors.New("nope")}, nil)
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Delete("/products/:id/files/:fileId", h.DeleteProductFile) })
		resp := covDo(t, app, httptest.NewRequest(http.MethodDelete, "/products/p1/files/f1", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusNotFound {
			t.Fatalf("status = %d, queria 404", resp.StatusCode)
		}
	})

	t.Run("file lookup error", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{getByIDResult: present, getFileErr: errors.New("nope")}, nil)
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Delete("/products/:id/files/:fileId", h.DeleteProductFile) })
		resp := covDo(t, app, httptest.NewRequest(http.MethodDelete, "/products/p1/files/f1", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusNotFound {
			t.Fatalf("status = %d, queria 404", resp.StatusCode)
		}
	})

	t.Run("file from another product", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{
			getByIDResult: present,
			getFileResult: &domain.DigitalProductFile{ID: "f1", ProductID: "other"},
		}, nil)
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Delete("/products/:id/files/:fileId", h.DeleteProductFile) })
		resp := covDo(t, app, httptest.NewRequest(http.MethodDelete, "/products/p1/files/f1", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusNotFound {
			t.Fatalf("status = %d, queria 404", resp.StatusCode)
		}
	})

	t.Run("delete error", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{getByIDResult: present, getFileResult: fileOwned, deleteFileErr: errors.New("db")}, nil)
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Delete("/products/:id/files/:fileId", h.DeleteProductFile) })
		resp := covDo(t, app, httptest.NewRequest(http.MethodDelete, "/products/p1/files/f1", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusInternalServerError {
			t.Fatalf("status = %d, queria 500", resp.StatusCode)
		}
	})

	t.Run("success removing from minio", func(t *testing.T) {
		minio, srv := covFakeS3(t)
		defer srv.Close()
		h := covNewLinkPay(t, &covLinkPayStub{getByIDResult: present, getFileResult: fileOwned}, minio)
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Delete("/products/:id/files/:fileId", h.DeleteProductFile) })
		resp := covDo(t, app, httptest.NewRequest(http.MethodDelete, "/products/p1/files/f1", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusNoContent {
			t.Fatalf("status = %d, queria 204", resp.StatusCode)
		}
	})
}

// ─── DownloadFile ────────────────────────────────────────────────────────────

func TestCovDownloadFile(t *testing.T) {
	confirmed := &domain.DigitalPurchase{ID: "pur1", ProductID: "p1", PaymentStatus: "confirmed"}

	t.Run("missing params", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{}, nil)
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Get("/download/:token/file", h.DownloadFile)
		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/download/tok/file", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("invalid token", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{purchaseErr: errors.New("nope")}, nil)
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Get("/download/:token/file/:fileId", h.DownloadFile)
		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/download/tok/file/f1", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusForbidden {
			t.Fatalf("status = %d, queria 403", resp.StatusCode)
		}
	})

	t.Run("payment not confirmed", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{
			purchaseResult: &domain.DigitalPurchase{ID: "pur1", PaymentStatus: "pending"},
		}, nil)
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Get("/download/:token/file/:fileId", h.DownloadFile)
		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/download/tok/file/f1", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusForbidden {
			t.Fatalf("status = %d, queria 403", resp.StatusCode)
		}
	})

	t.Run("access expired", func(t *testing.T) {
		past := time.Now().Add(-time.Hour)
		h := covNewLinkPay(t, &covLinkPayStub{
			purchaseResult: &domain.DigitalPurchase{ID: "pur1", PaymentStatus: "confirmed", AccessExpiresAt: &past},
		}, nil)
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Get("/download/:token/file/:fileId", h.DownloadFile)
		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/download/tok/file/f1", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusForbidden {
			t.Fatalf("status = %d, queria 403", resp.StatusCode)
		}
	})

	t.Run("file not found", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{purchaseResult: confirmed, getFileErr: errors.New("nope")}, nil)
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Get("/download/:token/file/:fileId", h.DownloadFile)
		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/download/tok/file/f1", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusNotFound {
			t.Fatalf("status = %d, queria 404", resp.StatusCode)
		}
	})

	t.Run("file from another product", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{
			purchaseResult: confirmed,
			getFileResult:  &domain.DigitalProductFile{ID: "f1", ProductID: "p2"},
		}, nil)
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Get("/download/:token/file/:fileId", h.DownloadFile)
		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/download/tok/file/f1", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusForbidden {
			t.Fatalf("status = %d, queria 403", resp.StatusCode)
		}
	})

	t.Run("storage not configured", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{
			purchaseResult: confirmed,
			getFileResult:  &domain.DigitalProductFile{ID: "f1", ProductID: "p1", StorageKey: "k"},
		}, nil)
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Get("/download/:token/file/:fileId", h.DownloadFile)
		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/download/tok/file/f1", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusServiceUnavailable {
			t.Fatalf("status = %d, queria 503", resp.StatusCode)
		}
	})

	t.Run("success and alias DownloadProductFile", func(t *testing.T) {
		minio, srv := covFakeS3(t)
		defer srv.Close()
		h := covNewLinkPay(t, &covLinkPayStub{
			purchaseResult: confirmed,
			getFileResult:  &domain.DigitalProductFile{ID: "f1", ProductID: "p1", StorageKey: "tenant/products/p1/f.pdf"},
		}, minio)
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Get("/download/:token/file/:fileId", h.DownloadFile)
		app.Get("/alias/:token/file/:fileId", h.DownloadProductFile)

		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/download/tok/file/f1", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusFound {
			t.Fatalf("status = %d, queria 302", resp.StatusCode)
		}
		if loc := resp.Header.Get("Location"); !strings.Contains(loc, "tenant/products/p1/f.pdf") {
			t.Errorf("Location = %q", loc)
		}
		if resp.Header.Get("Cache-Control") != "no-store" {
			t.Errorf("Cache-Control = %q", resp.Header.Get("Cache-Control"))
		}

		respAlias := covDo(t, app, httptest.NewRequest(http.MethodGet, "/alias/tok/file/f1", nil))
		defer respAlias.Body.Close()
		if respAlias.StatusCode != fiber.StatusFound {
			t.Errorf("alias status = %d, queria 302", respAlias.StatusCode)
		}
	})
}

// ─── Download / DownloadZip ─────────────────────────────────────────────────

func TestCovDownload(t *testing.T) {
	t.Run("token vazio", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{}, nil)
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Get("/download", h.Download)
		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/download", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("minio presign and alias DownloadProduct", func(t *testing.T) {
		minio, srv := covFakeS3(t)
		defer srv.Close()
		h := covNewLinkPay(t, &covLinkPayStub{downloadResult: "tenant/products/p1/file.pdf"}, minio)
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Get("/download/:token", h.Download)
		app.Get("/alias/:token", h.DownloadProduct)

		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/download/tok", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusFound {
			t.Fatalf("status = %d, queria 302", resp.StatusCode)
		}
		if resp.Header.Get("X-Download-At") == "" {
			t.Error("X-Download-At ausente")
		}

		respAlias := covDo(t, app, httptest.NewRequest(http.MethodGet, "/alias/tok", nil))
		defer respAlias.Body.Close()
		if respAlias.StatusCode != fiber.StatusFound {
			t.Errorf("alias status = %d", respAlias.StatusCode)
		}
	})
}

func TestCovDownloadZip(t *testing.T) {
	confirmed := &domain.DigitalPurchase{ID: "pur1", ProductID: "p1", TenantID: covTenantUUID, PaymentStatus: "confirmed"}

	t.Run("root token missing", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{}, nil)
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Get("/zip", h.DownloadZip)
		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/zip", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("purchase token invalid", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{downloadResult: "k", purchaseErr: errors.New("nope")}, nil)
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Get("/zip/:token", h.DownloadZip)
		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/zip/tok", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusForbidden {
			t.Fatalf("status = %d, queria 403", resp.StatusCode)
		}
	})

	t.Run("product lookup error", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{
			downloadResult: "k", purchaseResult: confirmed, getByIDErr: errors.New("boom"),
		}, nil)
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Get("/zip/:token", h.DownloadZip)
		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/zip/tok", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusInternalServerError {
			t.Fatalf("status = %d, queria 500", resp.StatusCode)
		}
	})

	t.Run("list files error", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{
			downloadResult: "k", purchaseResult: confirmed,
			getByIDResult: &domain.DigitalProduct{ID: "p1", Slug: "curso"},
			filesErr:      errors.New("db"),
		}, nil)
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Get("/zip/:token", h.DownloadZip)
		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/zip/tok", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusInternalServerError {
			t.Fatalf("status = %d, queria 500", resp.StatusCode)
		}
	})

	t.Run("storage not configured", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{
			downloadResult: "k", purchaseResult: confirmed,
			getByIDResult: &domain.DigitalProduct{ID: "p1", Slug: "curso"},
		}, nil)
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Get("/zip/:token", h.DownloadZip)
		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/zip/tok", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusServiceUnavailable {
			t.Fatalf("status = %d, queria 503", resp.StatusCode)
		}
	})

	t.Run("no entries", func(t *testing.T) {
		minio, srv := covFakeS3(t)
		defer srv.Close()
		h := covNewLinkPay(t, &covLinkPayStub{
			downloadResult: "k", purchaseResult: confirmed,
			getByIDResult: &domain.DigitalProduct{ID: "p1", Slug: "curso"},
		}, minio)
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Get("/zip/:token", h.DownloadZip)
		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/zip/tok", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusNotFound {
			t.Fatalf("status = %d, queria 404", resp.StatusCode)
		}
	})

}

// ─── RefundPurchase ─────────────────────────────────────────────────────────

func TestCovRefundPurchase(t *testing.T) {
	t.Run("missing purchase id", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{}, nil)
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Post("/refund", h.RefundPurchase)
		resp := covJSONReq(t, app, http.MethodPost, "/refund", `{"access_token":"t"}`)
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("invalid body", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{}, nil)
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Post("/purchases/:id/refund", h.RefundPurchase)
		resp := covJSONReq(t, app, http.MethodPost, "/purchases/pur1/refund", "{oops")
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("missing access token", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{}, nil)
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Post("/purchases/:id/refund", h.RefundPurchase)
		resp := covJSONReq(t, app, http.MethodPost, "/purchases/pur1/refund", `{}`)
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("service error", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{refundErr: errors.New("fora do prazo")}, nil)
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Post("/purchases/:id/refund", h.RefundPurchase)
		resp := covJSONReq(t, app, http.MethodPost, "/purchases/pur1/refund", `{"access_token":"tok"}`)
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("success", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{}, nil)
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Post("/purchases/:id/refund", h.RefundPurchase)
		body := covDecode(t, covJSONReq(t, app, http.MethodPost, "/purchases/pur1/refund", `{"access_token":"tok"}`))
		if body["message"] != "compra reembolsada com sucesso" {
			t.Errorf("message = %v", body["message"])
		}
	})
}

// ─── CreateCart ─────────────────────────────────────────────────────────────

func TestCovCreateCart(t *testing.T) {
	activeProduct := &domain.DigitalProduct{
		ID: "p1", TenantID: covTenantUUID, Slug: "curso", Name: "Curso", PriceCents: 2990, IsActive: true,
	}

	t.Run("missing slug", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{}, nil)
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Post("/cart", h.CreateCart)
		resp := covJSONReq(t, app, http.MethodPost, "/cart", `{}`)
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("invalid body", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{}, nil)
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Post("/products/:slug/cart", h.CreateCart)
		resp := covJSONReq(t, app, http.MethodPost, "/products/curso/cart", "{oops")
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("product not found", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{getBySlugErr: errors.New("nope")}, nil)
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Post("/products/:slug/cart", h.CreateCart)
		resp := covJSONReq(t, app, http.MethodPost, "/products/curso/cart", `{}`)
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusNotFound {
			t.Fatalf("status = %d, queria 404", resp.StatusCode)
		}
	})

	t.Run("inactive product", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{
			getBySlugResult: &domain.DigitalProduct{ID: "p1", IsActive: false},
		}, nil)
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Post("/products/:slug/cart", h.CreateCart)
		resp := covJSONReq(t, app, http.MethodPost, "/products/curso/cart", `{}`)
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusNotFound {
			t.Fatalf("status = %d, queria 404", resp.StatusCode)
		}
	})

	t.Run("cart service unavailable", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{getBySlugResult: activeProduct}, nil)
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Post("/products/:slug/cart", h.CreateCart)
		resp := covJSONReq(t, app, http.MethodPost, "/products/curso/cart", `{}`)
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadGateway {
			t.Fatalf("status = %d, queria 502", resp.StatusCode)
		}
	})

	t.Run("success with upsells", func(t *testing.T) {
		cartSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"cart-1","total":10}`))
		}))
		defer cartSrv.Close()

		upsellActive := &domain.DigitalProduct{ID: "p2", IsActive: true, PriceCents: 1000, Name: "Upsell"}
		upsellInactive := &domain.DigitalProduct{ID: "p3", IsActive: false}
		h := covNewLinkPayWithCart(t, &covLinkPayStub{
			getBySlugResult: activeProduct,
			upsellsResult: []domain.UpsellSuggestion{
				{ProductID: "p2"}, {ProductID: "p3"}, {ProductID: "p4"},
			},
			productsByID: map[string]*domain.DigitalProduct{
				"p2": upsellActive,
				"p3": upsellInactive,
			},
			getByIDErr: errors.New("missing p4"),
		}, nil, cartSrv.URL)
		// p4 não está no map → cai no getByIDErr (continue)
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Post("/products/:slug/cart", h.CreateCart)

		body := covDecode(t, covJSONReq(t, app, http.MethodPost, "/products/curso/cart", `{"add_upsells":true}`))
		if body["cart_id"] != "cart-1" {
			t.Errorf("cart_id = %v", body["cart_id"])
		}
	})

	t.Run("success without upsells and upsell lookup error", func(t *testing.T) {
		cartSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"cart-9"}`))
		}))
		defer cartSrv.Close()

		h := covNewLinkPayWithCart(t, &covLinkPayStub{
			getBySlugResult: activeProduct,
			upsellsErr:      errors.New("db"),
		}, nil, cartSrv.URL)
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Post("/products/:slug/cart", h.CreateCart)

		body := covDecode(t, covJSONReq(t, app, http.MethodPost, "/products/curso/cart", `{"add_upsells":true}`))
		if body["cart_id"] != "cart-9" {
			t.Errorf("cart_id = %v", body["cart_id"])
		}
	})
}

// ─── newBytesReader ─────────────────────────────────────────────────────────

func TestCovNewBytesReader(t *testing.T) {
	r := newBytesReader([]byte("abc"))
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(out) != "abc" {
		t.Errorf("out = %q", out)
	}
	// segunda leitura retorna EOF
	buf := make([]byte, 1)
	if n, err := r.Read(buf); n != 0 || err != io.EOF {
		t.Errorf("Read após EOF = (%d, %v)", n, err)
	}
}

// ─── newWebhookRedisClient (#8) ─────────────────────────────────────────────

func TestCovNewWebhookRedisClient(t *testing.T) {
	t.Run("env ausente retorna nil", func(t *testing.T) {
		t.Setenv("REDIS_URL", "")
		if c := newWebhookRedisClient(); c != nil {
			t.Errorf("client = %v, queria nil", c)
		}
	})

	t.Run("env inválida retorna nil", func(t *testing.T) {
		t.Setenv("REDIS_URL", "://nao-e-url")
		if c := newWebhookRedisClient(); c != nil {
			t.Errorf("client = %v, queria nil", c)
		}
	})

	t.Run("env válida cria client", func(t *testing.T) {
		t.Setenv("REDIS_URL", "redis://127.0.0.1:6379/0")
		c := newWebhookRedisClient()
		if c == nil {
			t.Fatal("client nil para REDIS_URL válida")
		}
		_ = c.Close()
	})
}

// ─── WithSubscriptionService ────────────────────────────────────────────────

func TestCovWithSubscriptionService(t *testing.T) {
	h := covNewLinkPay(t, &covLinkPayStub{}, nil)
	sub := &covSubscriptionStub{}
	if got := h.WithSubscriptionService(sub); got.subscriptionSvc != sub {
		t.Error("subscriptionSvc não injetado")
	}
}

// garante que uuid é usado (evita import não utilizado em refactors)
var _ = uuid.New
