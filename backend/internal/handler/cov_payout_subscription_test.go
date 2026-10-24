package handler

// Testes de cobertura (issue #5) para PayoutHandler, SubscriptionHandler e
// variantes de eventos do webhook. Arquivo novo.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"linkpay-service/internal/domain"
	"linkpay-service/internal/pkg/pulse"
)

// --- stubs ---

type covPayoutStub struct {
	balance    *domain.Balance
	balanceErr error

	listResult   *domain.ListPayoutsResponse
	listErr      error
	lastLimit    int
	lastOffset   int
	listCalls    int
	lastTenantID string

	payoutResult *domain.CreatorPayout
	payoutErr    error
	lastPixKey   string
	lastAmount   int

	transferCalls int
	lastEvent     string
	lastTransfer  string
	lastDesc      string
	transferErr   error

	preview    *domain.AnticipationPreview
	previewErr error

	anticipationResult *domain.CreatorPayout
	anticipationErr    error
}

func (p *covPayoutStub) GetBalance(_ context.Context, tenantID string) (*domain.Balance, error) {
	p.lastTenantID = tenantID
	return p.balance, p.balanceErr
}

func (p *covPayoutStub) ListPayouts(_ context.Context, tenantID string, limit, offset int) (*domain.ListPayoutsResponse, error) {
	p.listCalls++
	p.lastTenantID = tenantID
	p.lastLimit, p.lastOffset = limit, offset
	return p.listResult, p.listErr
}

func (p *covPayoutStub) RequestManualPayout(_ context.Context, tenantID, pixKey string, amountCents int) (*domain.CreatorPayout, error) {
	p.lastTenantID, p.lastPixKey, p.lastAmount = tenantID, pixKey, amountCents
	return p.payoutResult, p.payoutErr
}

func (p *covPayoutStub) HandleTransferWebhook(_ context.Context, event, transferID, description string) error {
	p.transferCalls++
	p.lastEvent, p.lastTransfer, p.lastDesc = event, transferID, description
	return p.transferErr
}

func (p *covPayoutStub) PreviewAnticipation(_ context.Context, tenantID string) (*domain.AnticipationPreview, error) {
	p.lastTenantID = tenantID
	return p.preview, p.previewErr
}

func (p *covPayoutStub) RequestAnticipation(_ context.Context, tenantID, pixKey string) (*domain.CreatorPayout, error) {
	p.lastTenantID, p.lastPixKey = tenantID, pixKey
	return p.anticipationResult, p.anticipationErr
}

type covSubscriptionStub struct {
	createPlanResult *domain.DigitalSubscriptionPlan
	createPlanErr    error

	listPlansResult []domain.DigitalSubscriptionPlan
	listPlansErr    error

	updatePlanResult *domain.DigitalSubscriptionPlan
	updatePlanErr    error

	deactivateErr error

	subscribeResult *domain.DigitalSubscription
	subscribeErr    error

	cancelErr error
	cancels   int

	cancelByAsaasCalls int
	cancelByAsaasErr   error

	sub    *domain.DigitalSubscription
	subErr error

	files    []domain.SubscriberFile
	filesErr error

	listSubs    []domain.DigitalSubscription
	listSubsErr error

	renewCalls int
	renewErr   error

	pastDueCalls int
	pastDueErr   error

	lastTenantID string
	lastPlanID   string
}

func (s *covSubscriptionStub) CreatePlan(_ context.Context, productID, tenantID string, _ *domain.CreateSubscriptionPlanRequest) (*domain.DigitalSubscriptionPlan, error) {
	s.lastPlanID, s.lastTenantID = productID, tenantID
	return s.createPlanResult, s.createPlanErr
}

func (s *covSubscriptionStub) ListPlansByProduct(_ context.Context, productID string) ([]domain.DigitalSubscriptionPlan, error) {
	s.lastPlanID = productID
	return s.listPlansResult, s.listPlansErr
}

func (s *covSubscriptionStub) UpdatePlan(_ context.Context, planID, tenantID string, _ *domain.UpdateSubscriptionPlanRequest) (*domain.DigitalSubscriptionPlan, error) {
	s.lastPlanID, s.lastTenantID = planID, tenantID
	return s.updatePlanResult, s.updatePlanErr
}

func (s *covSubscriptionStub) DeactivatePlan(_ context.Context, planID, tenantID string) error {
	s.lastPlanID, s.lastTenantID = planID, tenantID
	return s.deactivateErr
}

func (s *covSubscriptionStub) Subscribe(_ context.Context, planID string, _ *domain.SubscribeRequest) (*domain.DigitalSubscription, error) {
	s.lastPlanID = planID
	return s.subscribeResult, s.subscribeErr
}

func (s *covSubscriptionStub) CancelSubscription(_ context.Context, _ string) error {
	s.cancels++
	return s.cancelErr
}

func (s *covSubscriptionStub) CancelSubscriptionByAsaasID(_ context.Context, _ string) error {
	s.cancelByAsaasCalls++
	return s.cancelByAsaasErr
}

func (s *covSubscriptionStub) GetSubscription(_ context.Context, _ string) (*domain.DigitalSubscription, error) {
	return s.sub, s.subErr
}

func (s *covSubscriptionStub) GetSubscriberFiles(_ context.Context, _ string) ([]domain.SubscriberFile, error) {
	return s.files, s.filesErr
}

func (s *covSubscriptionStub) ListSubscriptionsByTenant(_ context.Context, tenantID string) ([]domain.DigitalSubscription, error) {
	s.lastTenantID = tenantID
	return s.listSubs, s.listSubsErr
}

func (s *covSubscriptionStub) RenewSubscription(_ context.Context, _ string) error {
	s.renewCalls++
	return s.renewErr
}

func (s *covSubscriptionStub) MarkPastDue(_ context.Context, _ string) error {
	s.pastDueCalls++
	return s.pastDueErr
}

// ─── GetBalance ─────────────────────────────────────────────────────────────

func TestCovPayoutGetBalance(t *testing.T) {
	t.Run("unauthorized", func(t *testing.T) {
		t.Setenv("PULSE_URL", "http://127.0.0.1:1")
		h := NewPayoutHandler(&covPayoutStub{}, pulse.New())
		app := covTenantApp(nil, func(app *fiber.App) { app.Get("/balance", h.GetBalance) })
		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/balance", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusUnauthorized {
			t.Fatalf("status = %d, queria 401", resp.StatusCode)
		}
	})

	t.Run("service error", func(t *testing.T) {
		t.Setenv("PULSE_URL", "http://127.0.0.1:1")
		h := NewPayoutHandler(&covPayoutStub{balanceErr: errors.New("boom")}, pulse.New())
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Get("/balance", h.GetBalance) })
		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/balance", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusInternalServerError {
			t.Fatalf("status = %d, queria 500", resp.StatusCode)
		}
	})

	t.Run("success", func(t *testing.T) {
		t.Setenv("PULSE_URL", "http://127.0.0.1:1")
		stub := &covPayoutStub{balance: &domain.Balance{AvailableCents: 1500, TotalCents: 1500}}
		h := NewPayoutHandler(stub, pulse.New())
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Get("/balance", h.GetBalance) })
		body := covDecode(t, covDo(t, app, httptest.NewRequest(http.MethodGet, "/balance", nil)))
		if body["available_cents"] != float64(1500) {
			t.Errorf("available_cents = %v", body["available_cents"])
		}
		if stub.lastTenantID != covTenantUUID {
			t.Errorf("tenantID = %q", stub.lastTenantID)
		}
	})
}

// ─── ListPayouts ────────────────────────────────────────────────────────────

func TestCovPayoutListPayouts(t *testing.T) {
	newStub := func() *covPayoutStub {
		return &covPayoutStub{listResult: &domain.ListPayoutsResponse{Page: 1, Limit: 20, Total: 0}}
	}

	t.Run("unauthorized", func(t *testing.T) {
		t.Setenv("PULSE_URL", "http://127.0.0.1:1")
		h := NewPayoutHandler(newStub(), pulse.New())
		app := covTenantApp(nil, func(app *fiber.App) { app.Get("/payouts", h.ListPayouts) })
		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/payouts", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusUnauthorized {
			t.Fatalf("status = %d, queria 401", resp.StatusCode)
		}
	})

	t.Run("defaults", func(t *testing.T) {
		t.Setenv("PULSE_URL", "http://127.0.0.1:1")
		stub := newStub()
		h := NewPayoutHandler(stub, pulse.New())
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Get("/payouts", h.ListPayouts) })
		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/payouts", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		if stub.lastLimit != 20 || stub.lastOffset != 0 {
			t.Errorf("limit/offset = %d/%d, queria 20/0", stub.lastLimit, stub.lastOffset)
		}
	})

	t.Run("paginação válida", func(t *testing.T) {
		t.Setenv("PULSE_URL", "http://127.0.0.1:1")
		stub := newStub()
		h := NewPayoutHandler(stub, pulse.New())
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Get("/payouts", h.ListPayouts) })
		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/payouts?page=3&limit=25", nil))
		defer resp.Body.Close()
		if stub.lastLimit != 25 || stub.lastOffset != 50 {
			t.Errorf("limit/offset = %d/%d, queria 25/50", stub.lastLimit, stub.lastOffset)
		}
	})

	t.Run("clamp de limit e page inválidos", func(t *testing.T) {
		t.Setenv("PULSE_URL", "http://127.0.0.1:1")
		for _, q := range []string{"?limit=0", "?limit=500", "?page=0", "?limit=-3&page=-2"} {
			stub := newStub()
			h := NewPayoutHandler(stub, pulse.New())
			app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Get("/payouts", h.ListPayouts) })
			resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/payouts"+q, nil))
			_ = resp.Body.Close()
			if stub.lastLimit != 20 {
				t.Errorf("%s: limit = %d, queria 20", q, stub.lastLimit)
			}
			if stub.lastOffset != 0 {
				t.Errorf("%s: offset = %d, queria 0", q, stub.lastOffset)
			}
		}
	})

	t.Run("service error", func(t *testing.T) {
		t.Setenv("PULSE_URL", "http://127.0.0.1:1")
		stub := newStub()
		stub.listErr = errors.New("boom")
		h := NewPayoutHandler(stub, pulse.New())
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Get("/payouts", h.ListPayouts) })
		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/payouts", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusInternalServerError {
			t.Fatalf("status = %d, queria 500", resp.StatusCode)
		}
	})

	t.Run("success with data", func(t *testing.T) {
		t.Setenv("PULSE_URL", "http://127.0.0.1:1")
		stub := &covPayoutStub{listResult: &domain.ListPayoutsResponse{
			Data: []domain.CreatorPayout{{ID: "po1", AmountCents: 5000}}, Total: 1, Page: 1, Limit: 20,
		}}
		h := NewPayoutHandler(stub, pulse.New())
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Get("/payouts", h.ListPayouts) })
		body := covDecode(t, covDo(t, app, httptest.NewRequest(http.MethodGet, "/payouts", nil)))
		if body["total"] != float64(1) {
			t.Errorf("total = %v", body["total"])
		}
	})
}

// ─── RequestPayout ──────────────────────────────────────────────────────────

func TestCovPayoutRequestPayout(t *testing.T) {
	t.Run("unauthorized", func(t *testing.T) {
		t.Setenv("PULSE_URL", "http://127.0.0.1:1")
		h := NewPayoutHandler(&covPayoutStub{}, pulse.New())
		app := covTenantApp(nil, func(app *fiber.App) { app.Post("/request", h.RequestPayout) })
		resp := covJSONReq(t, app, http.MethodPost, "/request", `{"pix_key":"a@b.com"}`)
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusUnauthorized {
			t.Fatalf("status = %d, queria 401", resp.StatusCode)
		}
	})

	t.Run("payload inválido", func(t *testing.T) {
		t.Setenv("PULSE_URL", "http://127.0.0.1:1")
		h := NewPayoutHandler(&covPayoutStub{}, pulse.New())
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Post("/request", h.RequestPayout) })
		resp := covJSONReq(t, app, http.MethodPost, "/request", "{oops")
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("validação falha", func(t *testing.T) {
		t.Setenv("PULSE_URL", "http://127.0.0.1:1")
		h := NewPayoutHandler(&covPayoutStub{}, pulse.New())
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Post("/request", h.RequestPayout) })

		resp := covJSONReq(t, app, http.MethodPost, "/request", `{"amount_cents":5000}`)
		_ = resp.Body.Close()
		if resp.StatusCode != fiber.StatusUnprocessableEntity {
			t.Fatalf("sem pix_key: status = %d, queria 422", resp.StatusCode)
		}

		resp2 := covJSONReq(t, app, http.MethodPost, "/request", `{"pix_key":"a@b.com","amount_cents":500}`)
		_ = resp2.Body.Close()
		if resp2.StatusCode != fiber.StatusUnprocessableEntity {
			t.Fatalf("amount baixo: status = %d, queria 422", resp2.StatusCode)
		}
	})

	t.Run("service error", func(t *testing.T) {
		t.Setenv("PULSE_URL", "http://127.0.0.1:1")
		h := NewPayoutHandler(&covPayoutStub{payoutErr: errors.New("saldo insuficiente")}, pulse.New())
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Post("/request", h.RequestPayout) })
		resp := covJSONReq(t, app, http.MethodPost, "/request", `{"pix_key":"a@b.com","amount_cents":5000}`)
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("success", func(t *testing.T) {
		t.Setenv("PULSE_URL", "http://127.0.0.1:1")
		stub := &covPayoutStub{payoutResult: &domain.CreatorPayout{ID: "po-1", AmountCents: 5000, Status: "pending"}}
		h := NewPayoutHandler(stub, pulse.New())
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Post("/request", h.RequestPayout) })
		resp := covJSONReq(t, app, http.MethodPost, "/request", `{"pix_key":"a@b.com","amount_cents":5000}`)
		body := covDecode(t, resp)
		if resp.StatusCode != fiber.StatusCreated {
			t.Fatalf("status = %d, queria 201", resp.StatusCode)
		}
		if body["id"] != "po-1" {
			t.Errorf("id = %v", body["id"])
		}
		if stub.lastPixKey != "a@b.com" || stub.lastAmount != 5000 {
			t.Errorf("pixKey/amount = %q/%d", stub.lastPixKey, stub.lastAmount)
		}
	})
}

// ─── GetAnticipationPreview / RequestAnticipation ───────────────────────────

func TestCovPayoutAnticipation(t *testing.T) {
	t.Run("preview unauthorized", func(t *testing.T) {
		t.Setenv("PULSE_URL", "http://127.0.0.1:1")
		h := NewPayoutHandler(&covPayoutStub{}, pulse.New())
		app := covTenantApp(nil, func(app *fiber.App) { app.Get("/preview", h.GetAnticipationPreview) })
		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/preview", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusUnauthorized {
			t.Fatalf("status = %d, queria 401", resp.StatusCode)
		}
	})

	t.Run("preview error", func(t *testing.T) {
		t.Setenv("PULSE_URL", "http://127.0.0.1:1")
		h := NewPayoutHandler(&covPayoutStub{previewErr: errors.New("boom")}, pulse.New())
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Get("/preview", h.GetAnticipationPreview) })
		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/preview", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusInternalServerError {
			t.Fatalf("status = %d, queria 500", resp.StatusCode)
		}
	})

	t.Run("preview vazio retorna 204", func(t *testing.T) {
		t.Setenv("PULSE_URL", "http://127.0.0.1:1")
		h := NewPayoutHandler(&covPayoutStub{}, pulse.New())
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Get("/preview", h.GetAnticipationPreview) })
		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/preview", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusNoContent {
			t.Fatalf("status = %d, queria 204", resp.StatusCode)
		}
	})

	t.Run("preview success", func(t *testing.T) {
		t.Setenv("PULSE_URL", "http://127.0.0.1:1")
		h := NewPayoutHandler(&covPayoutStub{
			preview: &domain.AnticipationPreview{EligibleCents: 10000, FeeCents: 200, NetCents: 9800},
		}, pulse.New())
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Get("/preview", h.GetAnticipationPreview) })
		body := covDecode(t, covDo(t, app, httptest.NewRequest(http.MethodGet, "/preview", nil)))
		if body["net_cents"] != float64(9800) {
			t.Errorf("net_cents = %v", body["net_cents"])
		}
	})

	t.Run("request unauthorized", func(t *testing.T) {
		t.Setenv("PULSE_URL", "http://127.0.0.1:1")
		h := NewPayoutHandler(&covPayoutStub{}, pulse.New())
		app := covTenantApp(nil, func(app *fiber.App) { app.Post("/anticipation", h.RequestAnticipation) })
		resp := covJSONReq(t, app, http.MethodPost, "/anticipation", `{}`)
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusUnauthorized {
			t.Fatalf("status = %d, queria 401", resp.StatusCode)
		}
	})

	t.Run("request payload inválido", func(t *testing.T) {
		t.Setenv("PULSE_URL", "http://127.0.0.1:1")
		h := NewPayoutHandler(&covPayoutStub{}, pulse.New())
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Post("/anticipation", h.RequestAnticipation) })
		resp := covJSONReq(t, app, http.MethodPost, "/anticipation", "{oops")
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("request validação falha", func(t *testing.T) {
		t.Setenv("PULSE_URL", "http://127.0.0.1:1")
		h := NewPayoutHandler(&covPayoutStub{}, pulse.New())
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Post("/anticipation", h.RequestAnticipation) })
		long := strings.Repeat("k", 300)
		resp := covJSONReq(t, app, http.MethodPost, "/anticipation", `{"pix_key":"`+long+`"}`)
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusUnprocessableEntity {
			t.Fatalf("status = %d, queria 422", resp.StatusCode)
		}
	})

	t.Run("request service error", func(t *testing.T) {
		t.Setenv("PULSE_URL", "http://127.0.0.1:1")
		h := NewPayoutHandler(&covPayoutStub{anticipationErr: errors.New("sem elegíveis")}, pulse.New())
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Post("/anticipation", h.RequestAnticipation) })
		resp := covJSONReq(t, app, http.MethodPost, "/anticipation", `{"pix_key":"a@b.com"}`)
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("request success", func(t *testing.T) {
		t.Setenv("PULSE_URL", "http://127.0.0.1:1")
		stub := &covPayoutStub{anticipationResult: &domain.CreatorPayout{ID: "po-ant", IsAnticipation: true}}
		h := NewPayoutHandler(stub, pulse.New())
		app := covTenantApp(covTenantUUID, func(app *fiber.App) { app.Post("/anticipation", h.RequestAnticipation) })
		resp := covJSONReq(t, app, http.MethodPost, "/anticipation", `{"pix_key":"a@b.com"}`)
		body := covDecode(t, resp)
		if resp.StatusCode != fiber.StatusCreated {
			t.Fatalf("status = %d, queria 201", resp.StatusCode)
		}
		if body["id"] != "po-ant" {
			t.Errorf("id = %v", body["id"])
		}
	})
}

// ─── SubscriptionHandler: planos ────────────────────────────────────────────

func covNewSubscriptionApp(t *testing.T, svc *covSubscriptionStub, tenant any, routes func(h *SubscriptionHandler) func(app *fiber.App)) *fiber.App {
	t.Helper()
	h := NewSubscriptionHandler(svc)
	return covTenantApp(tenant, routes(h))
}

func TestCovSubscriptionPlans(t *testing.T) {
	validPlanBody := `{"name":"Mensal","slug":"mensal","price_cents":4990,"interval":"monthly"}`

	t.Run("create plan unauthorized", func(t *testing.T) {
		app := covNewSubscriptionApp(t, &covSubscriptionStub{}, nil, func(h *SubscriptionHandler) func(*fiber.App) {
			return func(f *fiber.App) { f.Post("/products/:id/plans", h.CreatePlan) }
		})
		resp := covJSONReq(t, app, http.MethodPost, "/products/p1/plans", validPlanBody)
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusUnauthorized {
			t.Fatalf("status = %d, queria 401", resp.StatusCode)
		}
	})

	t.Run("create plan missing product id", func(t *testing.T) {
		app := covNewSubscriptionApp(t, &covSubscriptionStub{}, covTenantUUID, func(h *SubscriptionHandler) func(*fiber.App) {
			return func(f *fiber.App) { f.Post("/plans", h.CreatePlan) }
		})
		resp := covJSONReq(t, app, http.MethodPost, "/plans", validPlanBody)
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("create plan invalid body", func(t *testing.T) {
		app := covNewSubscriptionApp(t, &covSubscriptionStub{}, covTenantUUID, func(h *SubscriptionHandler) func(*fiber.App) {
			return func(f *fiber.App) { f.Post("/products/:id/plans", h.CreatePlan) }
		})
		resp := covJSONReq(t, app, http.MethodPost, "/products/p1/plans", "{oops")
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("create plan validation failure", func(t *testing.T) {
		app := covNewSubscriptionApp(t, &covSubscriptionStub{}, covTenantUUID, func(h *SubscriptionHandler) func(*fiber.App) {
			return func(f *fiber.App) { f.Post("/products/:id/plans", h.CreatePlan) }
		})
		resp := covJSONReq(t, app, http.MethodPost, "/products/p1/plans", `{"name":"X","slug":"x","interval":"weekly"}`)
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("create plan service error", func(t *testing.T) {
		app := covNewSubscriptionApp(t, &covSubscriptionStub{createPlanErr: errors.New("boom")}, covTenantUUID, func(h *SubscriptionHandler) func(*fiber.App) {
			return func(f *fiber.App) { f.Post("/products/:id/plans", h.CreatePlan) }
		})
		resp := covJSONReq(t, app, http.MethodPost, "/products/p1/plans", validPlanBody)
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("create plan success", func(t *testing.T) {
		stub := &covSubscriptionStub{createPlanResult: &domain.DigitalSubscriptionPlan{ID: "plan-1", Name: "Mensal"}}
		app := covNewSubscriptionApp(t, stub, covTenantUUID, func(h *SubscriptionHandler) func(*fiber.App) {
			return func(f *fiber.App) { f.Post("/products/:id/plans", h.CreatePlan) }
		})
		resp := covJSONReq(t, app, http.MethodPost, "/products/p1/plans", validPlanBody)
		body := covDecode(t, resp)
		if resp.StatusCode != fiber.StatusCreated {
			t.Fatalf("status = %d, queria 201", resp.StatusCode)
		}
		if body["id"] != "plan-1" {
			t.Errorf("id = %v", body["id"])
		}
		if stub.lastPlanID != "p1" || stub.lastTenantID != covTenantUUID {
			t.Errorf("planID/tenant = %q/%q", stub.lastPlanID, stub.lastTenantID)
		}
	})

	t.Run("list plans missing id", func(t *testing.T) {
		app := covNewSubscriptionApp(t, &covSubscriptionStub{}, covTenantUUID, func(h *SubscriptionHandler) func(*fiber.App) {
			return func(f *fiber.App) { f.Get("/plans", h.ListPlans) }
		})
		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/plans", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("list plans error", func(t *testing.T) {
		app := covNewSubscriptionApp(t, &covSubscriptionStub{listPlansErr: errors.New("db")}, covTenantUUID, func(h *SubscriptionHandler) func(*fiber.App) {
			return func(f *fiber.App) { f.Get("/products/:id/plans", h.ListPlans) }
		})
		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/products/p1/plans", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusInternalServerError {
			t.Fatalf("status = %d, queria 500", resp.StatusCode)
		}
	})

	t.Run("list plans success", func(t *testing.T) {
		stub := &covSubscriptionStub{listPlansResult: []domain.DigitalSubscriptionPlan{{ID: "plan-1"}, {ID: "plan-2"}}}
		app := covNewSubscriptionApp(t, stub, covTenantUUID, func(h *SubscriptionHandler) func(*fiber.App) {
			return func(f *fiber.App) { f.Get("/products/:id/plans", h.ListPlans) }
		})
		body := covDecode(t, covDo(t, app, httptest.NewRequest(http.MethodGet, "/products/p1/plans", nil)))
		plans, ok := body["plans"].([]any)
		if !ok || len(plans) != 2 {
			t.Errorf("plans = %v", body["plans"])
		}
	})
}

func TestCovSubscriptionUpdateDeactivate(t *testing.T) {
	t.Run("update unauthorized", func(t *testing.T) {
		app := covNewSubscriptionApp(t, &covSubscriptionStub{}, nil, func(h *SubscriptionHandler) func(*fiber.App) {
			return func(f *fiber.App) { f.Put("/products/:id/plans/:plan_id", h.UpdatePlan) }
		})
		resp := covJSONReq(t, app, http.MethodPut, "/products/p1/plans/plan-1", `{"name":"Novo"}`)
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusUnauthorized {
			t.Fatalf("status = %d, queria 401", resp.StatusCode)
		}
	})

	t.Run("update missing plan id", func(t *testing.T) {
		app := covNewSubscriptionApp(t, &covSubscriptionStub{}, covTenantUUID, func(h *SubscriptionHandler) func(*fiber.App) {
			return func(f *fiber.App) { f.Put("/products/:id/plans", h.UpdatePlan) }
		})
		resp := covJSONReq(t, app, http.MethodPut, "/products/p1/plans", `{"name":"Novo"}`)
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("update invalid body", func(t *testing.T) {
		app := covNewSubscriptionApp(t, &covSubscriptionStub{}, covTenantUUID, func(h *SubscriptionHandler) func(*fiber.App) {
			return func(f *fiber.App) { f.Put("/products/:id/plans/:plan_id", h.UpdatePlan) }
		})
		resp := covJSONReq(t, app, http.MethodPut, "/products/p1/plans/plan-1", "{oops")
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("update validation failure", func(t *testing.T) {
		app := covNewSubscriptionApp(t, &covSubscriptionStub{}, covTenantUUID, func(h *SubscriptionHandler) func(*fiber.App) {
			return func(f *fiber.App) { f.Put("/products/:id/plans/:plan_id", h.UpdatePlan) }
		})
		resp := covJSONReq(t, app, http.MethodPut, "/products/p1/plans/plan-1", `{"price_cents":-1}`)
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("update service error", func(t *testing.T) {
		app := covNewSubscriptionApp(t, &covSubscriptionStub{updatePlanErr: errors.New("boom")}, covTenantUUID, func(h *SubscriptionHandler) func(*fiber.App) {
			return func(f *fiber.App) { f.Put("/products/:id/plans/:plan_id", h.UpdatePlan) }
		})
		resp := covJSONReq(t, app, http.MethodPut, "/products/p1/plans/plan-1", `{"name":"Novo"}`)
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("update success", func(t *testing.T) {
		stub := &covSubscriptionStub{updatePlanResult: &domain.DigitalSubscriptionPlan{ID: "plan-1", Name: "Novo"}}
		app := covNewSubscriptionApp(t, stub, covTenantUUID, func(h *SubscriptionHandler) func(*fiber.App) {
			return func(f *fiber.App) { f.Put("/products/:id/plans/:plan_id", h.UpdatePlan) }
		})
		body := covDecode(t, covJSONReq(t, app, http.MethodPut, "/products/p1/plans/plan-1", `{"name":"Novo"}`))
		if body["name"] != "Novo" {
			t.Errorf("name = %v", body["name"])
		}
		if stub.lastPlanID != "plan-1" {
			t.Errorf("planID = %q", stub.lastPlanID)
		}
	})

	t.Run("deactivate unauthorized", func(t *testing.T) {
		app := covNewSubscriptionApp(t, &covSubscriptionStub{}, nil, func(h *SubscriptionHandler) func(*fiber.App) {
			return func(f *fiber.App) { f.Delete("/products/:id/plans/:plan_id", h.DeactivatePlan) }
		})
		resp := covDo(t, app, httptest.NewRequest(http.MethodDelete, "/products/p1/plans/plan-1", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusUnauthorized {
			t.Fatalf("status = %d, queria 401", resp.StatusCode)
		}
	})

	t.Run("deactivate missing plan id", func(t *testing.T) {
		app := covNewSubscriptionApp(t, &covSubscriptionStub{}, covTenantUUID, func(h *SubscriptionHandler) func(*fiber.App) {
			return func(f *fiber.App) { f.Delete("/products/:id/plans", h.DeactivatePlan) }
		})
		resp := covDo(t, app, httptest.NewRequest(http.MethodDelete, "/products/p1/plans", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("deactivate service error", func(t *testing.T) {
		app := covNewSubscriptionApp(t, &covSubscriptionStub{deactivateErr: errors.New("boom")}, covTenantUUID, func(h *SubscriptionHandler) func(*fiber.App) {
			return func(f *fiber.App) { f.Delete("/products/:id/plans/:plan_id", h.DeactivatePlan) }
		})
		resp := covDo(t, app, httptest.NewRequest(http.MethodDelete, "/products/p1/plans/plan-1", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("deactivate success", func(t *testing.T) {
		app := covNewSubscriptionApp(t, &covSubscriptionStub{}, covTenantUUID, func(h *SubscriptionHandler) func(*fiber.App) {
			return func(f *fiber.App) { f.Delete("/products/:id/plans/:plan_id", h.DeactivatePlan) }
		})
		body := covDecode(t, covDo(t, app, httptest.NewRequest(http.MethodDelete, "/products/p1/plans/plan-1", nil)))
		if body["message"] != "plano desativado" {
			t.Errorf("message = %v", body["message"])
		}
	})
}

// ─── ListAdminSubscriptions / Subscribe / status / cancel ───────────────────

func TestCovSubscriptionAdminAndPublic(t *testing.T) {
	t.Run("list admin unauthorized", func(t *testing.T) {
		app := covNewSubscriptionApp(t, &covSubscriptionStub{}, nil, func(h *SubscriptionHandler) func(*fiber.App) {
			return func(f *fiber.App) { f.Get("/subscriptions", h.ListAdminSubscriptions) }
		})
		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/subscriptions", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusUnauthorized {
			t.Fatalf("status = %d, queria 401", resp.StatusCode)
		}
	})

	t.Run("list admin error", func(t *testing.T) {
		app := covNewSubscriptionApp(t, &covSubscriptionStub{listSubsErr: errors.New("db")}, covTenantUUID, func(h *SubscriptionHandler) func(*fiber.App) {
			return func(f *fiber.App) { f.Get("/subscriptions", h.ListAdminSubscriptions) }
		})
		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/subscriptions", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusInternalServerError {
			t.Fatalf("status = %d, queria 500", resp.StatusCode)
		}
	})

	t.Run("list admin success", func(t *testing.T) {
		stub := &covSubscriptionStub{listSubs: []domain.DigitalSubscription{{ID: "sub-1", Status: "active"}}}
		app := covNewSubscriptionApp(t, stub, covTenantUUID, func(h *SubscriptionHandler) func(*fiber.App) {
			return func(f *fiber.App) { f.Get("/subscriptions", h.ListAdminSubscriptions) }
		})
		body := covDecode(t, covDo(t, app, httptest.NewRequest(http.MethodGet, "/subscriptions", nil)))
		subs, ok := body["subscriptions"].([]any)
		if !ok || len(subs) != 1 {
			t.Errorf("subscriptions = %v", body["subscriptions"])
		}
	})

	t.Run("subscribe missing plan id", func(t *testing.T) {
		app := covNewSubscriptionApp(t, &covSubscriptionStub{}, nil, func(h *SubscriptionHandler) func(*fiber.App) {
			return func(f *fiber.App) { f.Post("/subscribe", h.Subscribe) }
		})
		resp := covJSONReq(t, app, http.MethodPost, "/subscribe", `{}`)
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("subscribe invalid body", func(t *testing.T) {
		app := covNewSubscriptionApp(t, &covSubscriptionStub{}, nil, func(h *SubscriptionHandler) func(*fiber.App) {
			return func(f *fiber.App) { f.Post("/plans/:plan_id/subscribe", h.Subscribe) }
		})
		resp := covJSONReq(t, app, http.MethodPost, "/plans/plan-1/subscribe", "{oops")
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("subscribe validation failure", func(t *testing.T) {
		app := covNewSubscriptionApp(t, &covSubscriptionStub{}, nil, func(h *SubscriptionHandler) func(*fiber.App) {
			return func(f *fiber.App) { f.Post("/plans/:plan_id/subscribe", h.Subscribe) }
		})
		resp := covJSONReq(t, app, http.MethodPost, "/plans/plan-1/subscribe", `{"customer_name":"Ana"}`)
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("subscribe service error", func(t *testing.T) {
		app := covNewSubscriptionApp(t, &covSubscriptionStub{subscribeErr: errors.New("boom")}, nil, func(h *SubscriptionHandler) func(*fiber.App) {
			return func(f *fiber.App) { f.Post("/plans/:plan_id/subscribe", h.Subscribe) }
		})
		resp := covJSONReq(t, app, http.MethodPost, "/plans/plan-1/subscribe",
			`{"customer_name":"Ana","customer_email":"ana@example.com"}`)
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("subscribe success", func(t *testing.T) {
		stub := &covSubscriptionStub{subscribeResult: &domain.DigitalSubscription{ID: "sub-1", Status: "active"}}
		app := covNewSubscriptionApp(t, stub, nil, func(h *SubscriptionHandler) func(*fiber.App) {
			return func(f *fiber.App) { f.Post("/plans/:plan_id/subscribe", h.Subscribe) }
		})
		resp := covJSONReq(t, app, http.MethodPost, "/plans/plan-1/subscribe",
			`{"customer_name":"Ana","customer_email":"ana@example.com"}`)
		body := covDecode(t, resp)
		if resp.StatusCode != fiber.StatusCreated {
			t.Fatalf("status = %d, queria 201", resp.StatusCode)
		}
		if body["id"] != "sub-1" {
			t.Errorf("id = %v", body["id"])
		}
	})

	t.Run("status missing token", func(t *testing.T) {
		app := covNewSubscriptionApp(t, &covSubscriptionStub{}, nil, func(h *SubscriptionHandler) func(*fiber.App) {
			return func(f *fiber.App) { f.Get("/subscriptions/status", h.GetSubscriptionStatus) }
		})
		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/subscriptions/status", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("status not found", func(t *testing.T) {
		app := covNewSubscriptionApp(t, &covSubscriptionStub{subErr: errors.New("nope")}, nil, func(h *SubscriptionHandler) func(*fiber.App) {
			return func(f *fiber.App) { f.Get("/subscriptions/:token", h.GetSubscriptionStatus) }
		})
		resp := covDo(t, app, httptest.NewRequest(http.MethodGet, "/subscriptions/tok", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusNotFound {
			t.Fatalf("status = %d, queria 404", resp.StatusCode)
		}
	})

	t.Run("status success with nil files", func(t *testing.T) {
		stub := &covSubscriptionStub{
			sub:      &domain.DigitalSubscription{ID: "sub-1", Status: "active"},
			filesErr: errors.New("db"),
		}
		app := covNewSubscriptionApp(t, stub, nil, func(h *SubscriptionHandler) func(*fiber.App) {
			return func(f *fiber.App) { f.Get("/subscriptions/:token", h.GetSubscriptionStatus) }
		})
		body := covDecode(t, covDo(t, app, httptest.NewRequest(http.MethodGet, "/subscriptions/tok", nil)))
		files, ok := body["files"].([]any)
		if !ok || len(files) != 0 {
			t.Errorf("files = %v", body["files"])
		}
		if body["subscription"] == nil {
			t.Error("subscription ausente")
		}
	})

	t.Run("status success with files", func(t *testing.T) {
		stub := &covSubscriptionStub{
			sub:   &domain.DigitalSubscription{ID: "sub-1"},
			files: []domain.SubscriberFile{{ProductID: "p1", FileName: "a.pdf"}},
		}
		app := covNewSubscriptionApp(t, stub, nil, func(h *SubscriptionHandler) func(*fiber.App) {
			return func(f *fiber.App) { f.Get("/subscriptions/:token", h.GetSubscriptionStatus) }
		})
		body := covDecode(t, covDo(t, app, httptest.NewRequest(http.MethodGet, "/subscriptions/tok", nil)))
		files, ok := body["files"].([]any)
		if !ok || len(files) != 1 {
			t.Errorf("files = %v", body["files"])
		}
	})

	t.Run("cancel missing token", func(t *testing.T) {
		app := covNewSubscriptionApp(t, &covSubscriptionStub{}, nil, func(h *SubscriptionHandler) func(*fiber.App) {
			return func(f *fiber.App) { f.Post("/subscriptions/cancel", h.CancelSubscription) }
		})
		resp := covDo(t, app, httptest.NewRequest(http.MethodPost, "/subscriptions/cancel", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("cancel error", func(t *testing.T) {
		app := covNewSubscriptionApp(t, &covSubscriptionStub{cancelErr: errors.New("boom")}, nil, func(h *SubscriptionHandler) func(*fiber.App) {
			return func(f *fiber.App) { f.Post("/subscriptions/:token/cancel", h.CancelSubscription) }
		})
		resp := covDo(t, app, httptest.NewRequest(http.MethodPost, "/subscriptions/tok/cancel", nil))
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})

	t.Run("cancel success", func(t *testing.T) {
		stub := &covSubscriptionStub{}
		app := covNewSubscriptionApp(t, stub, nil, func(h *SubscriptionHandler) func(*fiber.App) {
			return func(f *fiber.App) { f.Post("/subscriptions/:token/cancel", h.CancelSubscription) }
		})
		body := covDecode(t, covDo(t, app, httptest.NewRequest(http.MethodPost, "/subscriptions/tok/cancel", nil)))
		if body["message"] != "assinatura cancelada" {
			t.Errorf("message = %v", body["message"])
		}
		if stub.cancels != 1 {
			t.Errorf("cancels = %d", stub.cancels)
		}
	})
}

// ─── Webhook: variantes de eventos ──────────────────────────────────────────

// covPostWebhook monta um POST /webhook válido (token + timestamp recentes).
func covPostWebhook(t *testing.T, app *fiber.App, payload string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("asaas-access-token", covWebhookKey)
	req.Header.Set("asaas-timestamp", strconv.FormatInt(time.Now().Unix(), 10))
	return covDo(t, app, req)
}

func TestCovWebhook_EventVariants(t *testing.T) {
	t.Run("payment confirmed for subscription renews", func(t *testing.T) {
		svc := &covLinkPayStub{}
		sub := &covSubscriptionStub{}
		h := covNewLinkPay(t, svc, nil).WithSubscriptionService(sub)
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Post("/webhook", h.HandleWebhook)

		resp := covPostWebhook(t, app, `{"event":"PAYMENT_CONFIRMED","payment":{"id":"pay-1","subscription":"asaas-sub-1"}}`)
		body := covDecode(t, resp)
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("status = %d, queria 200", resp.StatusCode)
		}
		if body["received"] != true {
			t.Errorf("received = %v", body["received"])
		}
		if sub.renewCalls != 1 {
			t.Errorf("renewCalls = %d, queria 1", sub.renewCalls)
		}
		if svc.processCalls != 0 {
			t.Errorf("processCalls = %d, queria 0 (fluxo de assinatura)", svc.processCalls)
		}
	})

	t.Run("payment confirmed subscription renew error still 200", func(t *testing.T) {
		sub := &covSubscriptionStub{renewErr: errors.New("boom")}
		h := covNewLinkPay(t, &covLinkPayStub{}, nil).WithSubscriptionService(sub)
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Post("/webhook", h.HandleWebhook)

		resp := covPostWebhook(t, app, `{"event":"PAYMENT_RECEIVED","payment":{"id":"pay-2","subscription":"asaas-sub-2"}}`)
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("status = %d, queria 200", resp.StatusCode)
		}
	})

	t.Run("payment confirmed process error returns 500", func(t *testing.T) {
		svc := &covLinkPayStub{processErr: errors.New("boom")}
		h := covNewLinkPay(t, svc, nil)
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Post("/webhook", h.HandleWebhook)

		resp := covPostWebhook(t, app, `{"event":"PAYMENT_CONFIRMED","payment":{"id":"pay-3"}}`)
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusInternalServerError {
			t.Fatalf("status = %d, queria 500", resp.StatusCode)
		}
	})

	t.Run("payment overdue marks past due", func(t *testing.T) {
		sub := &covSubscriptionStub{}
		h := covNewLinkPay(t, &covLinkPayStub{}, nil).WithSubscriptionService(sub)
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Post("/webhook", h.HandleWebhook)

		resp := covPostWebhook(t, app, `{"event":"PAYMENT_OVERDUE","payment":{"id":"pay-4","subscription":"asaas-sub-4"}}`)
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("status = %d, queria 200", resp.StatusCode)
		}
		if sub.pastDueCalls != 1 {
			t.Errorf("pastDueCalls = %d, queria 1", sub.pastDueCalls)
		}
	})

	t.Run("payment overdue without subscription service", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{}, nil)
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Post("/webhook", h.HandleWebhook)

		resp := covPostWebhook(t, app, `{"event":"PAYMENT_OVERDUE","payment":{"id":"pay-5","subscription":"x"}}`)
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("status = %d, queria 200", resp.StatusCode)
		}
	})

	t.Run("subscription deleted cancels by asaas id", func(t *testing.T) {
		sub := &covSubscriptionStub{cancelByAsaasErr: errors.New("boom")}
		h := covNewLinkPay(t, &covLinkPayStub{}, nil).WithSubscriptionService(sub)
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Post("/webhook", h.HandleWebhook)

		resp := covPostWebhook(t, app, `{"event":"SUBSCRIPTION_DELETED","subscription":{"id":"asaas-sub-9"}}`)
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("status = %d, queria 200", resp.StatusCode)
		}
		if sub.cancelByAsaasCalls != 1 {
			t.Errorf("cancelByAsaasCalls = %d, queria 1", sub.cancelByAsaasCalls)
		}
	})

	t.Run("subscription deleted without service or id", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{}, nil)
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Post("/webhook", h.HandleWebhook)

		resp := covPostWebhook(t, app, `{"event":"SUBSCRIPTION_DELETED","subscription":{"id":"x"}}`)
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("status = %d, queria 200", resp.StatusCode)
		}

		sub := &covSubscriptionStub{}
		h2 := covNewLinkPay(t, &covLinkPayStub{}, nil).WithSubscriptionService(sub)
		app2 := fiber.New(fiber.Config{DisableStartupMessage: true})
		app2.Post("/webhook", h2.HandleWebhook)
		resp2 := covPostWebhook(t, app2, `{"event":"SUBSCRIPTION_DELETED"}`)
		defer resp2.Body.Close()
		if sub.cancelByAsaasCalls != 0 {
			t.Errorf("cancelByAsaasCalls = %d, queria 0 (id ausente)", sub.cancelByAsaasCalls)
		}
	})

	t.Run("transfer failed notifies payout service", func(t *testing.T) {
		payout := &covPayoutStub{transferErr: errors.New("boom")}
		h := covNewLinkPay(t, &covLinkPayStub{}, nil).WithPayoutService(payout)
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Post("/webhook", h.HandleWebhook)

		resp := covPostWebhook(t, app, `{"event":"TRANSFER_FAILED","transfer":{"id":"tx-1","description":"Repasse"}}`)
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("status = %d, queria 200", resp.StatusCode)
		}
		if payout.transferCalls != 1 || payout.lastEvent != "TRANSFER_FAILED" {
			t.Errorf("transferCalls/event = %d/%q", payout.transferCalls, payout.lastEvent)
		}
		if payout.lastDesc != "Repasse" {
			t.Errorf("description = %q", payout.lastDesc)
		}
	})

	t.Run("transfer without payout service or id", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{}, nil)
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Post("/webhook", h.HandleWebhook)

		resp := covPostWebhook(t, app, `{"event":"TRANSFER_COMPLETED","transfer":{"id":"tx-2"}}`)
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("status = %d, queria 200", resp.StatusCode)
		}

		payout := &covPayoutStub{}
		h2 := covNewLinkPay(t, &covLinkPayStub{}, nil).WithPayoutService(payout)
		app2 := fiber.New(fiber.Config{DisableStartupMessage: true})
		app2.Post("/webhook", h2.HandleWebhook)
		resp2 := covPostWebhook(t, app2, `{"event":"TRANSFER_COMPLETED","transfer":{}}`)
		defer resp2.Body.Close()
		if payout.transferCalls != 0 {
			t.Errorf("transferCalls = %d, queria 0 (id ausente)", payout.transferCalls)
		}
	})

	t.Run("token via query string", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{}, nil)
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Post("/webhook", h.HandleWebhook)

		req := httptest.NewRequest(http.MethodPost,
			"/webhook?access_token="+covWebhookKey+"&timestamp="+strconv.FormatInt(time.Now().Unix(), 10),
			strings.NewReader(`{"event":"UNKNOWN_EVENT"}`))
		req.Header.Set("Content-Type", "application/json")
		resp := covDo(t, app, req)
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("status = %d, queria 200", resp.StatusCode)
		}
	})

	t.Run("token via access_token header", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{}, nil)
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Post("/webhook", h.HandleWebhook)

		req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(`{"event":"UNKNOWN_EVENT"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("access_token", covWebhookKey)
		req.Header.Set("timestamp", strconv.FormatInt(time.Now().Unix(), 10))
		resp := covDo(t, app, req)
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("status = %d, queria 200", resp.StatusCode)
		}
	})

	t.Run("invalid json with valid token returns 400", func(t *testing.T) {
		h := covNewLinkPay(t, &covLinkPayStub{}, nil)
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Post("/webhook", h.HandleWebhook)

		resp := covPostWebhook(t, app, `{not-json`)
		defer resp.Body.Close()
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status = %d, queria 400", resp.StatusCode)
		}
	})
}
