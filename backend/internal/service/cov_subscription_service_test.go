package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"linkpay-service/internal/domain"
	"linkpay-service/internal/service"
)

func covPlan(id, productID, tenantID, interval string, trialDays int) *domain.DigitalSubscriptionPlan {
	return &domain.DigitalSubscriptionPlan{
		ID:         id,
		ProductID:  productID,
		TenantID:   tenantID,
		Name:       "Plano " + id,
		Slug:       id,
		PriceCents: 4900,
		Interval:   interval,
		TrialDays:  trialDays,
		IsActive:   true,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
}

func covSub(id, planID, email, status, asaasID string) *domain.DigitalSubscription {
	return &domain.DigitalSubscription{
		ID:                  id,
		PlanID:              planID,
		TenantID:            "tenant-1",
		CustomerEmail:       email,
		CustomerName:        "Assinante",
		Status:              status,
		AsaasSubscriptionID: asaasID,
		AccessToken:         "tok-" + id,
		CreatedAt:           time.Now(),
		UpdatedAt:           time.Now(),
	}
}

// covSubAsaasServer simula /customers e /subscriptions.
type covSubAsaasState struct {
	mu       sync.Mutex
	cycle    string
	dueDate  string
	extRef   string
	value    float64
	subCalls int
}

func covNewSubAsaasServer(customerStatus int, subStatus int, subBody string) (*covAsaasServer, *covSubAsaasState) {
	st := &covSubAsaasState{}
	a := covNewAsaasServer(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/customers":
			if customerStatus >= 400 {
				covJSON(w, customerStatus, `{"errors":"cpf invalido"}`)
				return
			}
			covJSON(w, http.StatusOK, `{"id":"cus_77","name":"Assinante","email":"sub@example.com"}`)
		case r.URL.Path == "/subscriptions" && r.Method == http.MethodPost:
			var req service.AsaasSubscriptionRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			st.mu.Lock()
			st.cycle, st.dueDate, st.extRef, st.value = req.Cycle, req.NextDueDate, req.ExternalReference, req.Value
			st.subCalls++
			st.mu.Unlock()
			if subStatus >= 400 {
				covJSON(w, subStatus, subBody)
				return
			}
			covJSON(w, http.StatusOK, `{"id":"asaas-sub-1","status":"ACTIVE"}`)
		default:
			http.NotFound(w, r)
		}
	})
	return a, st
}

// ─── Planos ────────────────────────────────────────────────────────────────

func TestCovSubscription_CreatePlan(t *testing.T) {
	ctx := context.Background()
	subRepo := covNewSubRepo()
	linkRepo := covNewLinkRepo()
	linkRepo.products["prod-1"] = covProduct("prod-1", "tenant-1", "curso", 1000, true)
	svc := service.NewSubscriptionService(subRepo, linkRepo, nil)

	plan, err := svc.CreatePlan(ctx, "prod-1", "tenant-1", &domain.CreateSubscriptionPlanRequest{
		Name: "Mensal", Slug: "mensal", PriceCents: 4900, Interval: "monthly", SortOrder: 2,
		Features: []string{"pdf"},
	})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if plan.ID == "" || !plan.IsActive || len(plan.Features) != 1 || plan.PriceCents != 4900 {
		t.Errorf("plan = %+v", plan)
	}
	if len(subRepo.createdPlans) != 1 {
		t.Error("plan not persisted")
	}

	// features nil → slice vazia (não nil)
	plan2, err := svc.CreatePlan(ctx, "prod-1", "tenant-1", &domain.CreateSubscriptionPlanRequest{
		Name: "Anual", Slug: "anual", PriceCents: 9900, Interval: "yearly",
	})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if plan2.Features == nil || len(plan2.Features) != 0 {
		t.Errorf("features = %#v, want empty slice", plan2.Features)
	}
}

func TestCovSubscription_CreatePlan_ProductNotFound(t *testing.T) {
	svc := service.NewSubscriptionService(covNewSubRepo(), covNewLinkRepo(), nil)
	_, err := svc.CreatePlan(context.Background(), "ghost", "t", &domain.CreateSubscriptionPlanRequest{Name: "x"})
	if err == nil || !strings.Contains(err.Error(), "produto não encontrado") {
		t.Fatalf("error = %v", err)
	}
}

func TestCovSubscription_CreatePlan_RepoError(t *testing.T) {
	linkRepo := covNewLinkRepo()
	linkRepo.products["prod-1"] = covProduct("prod-1", "t", "s", 100, true)
	subRepo := covNewSubRepo()
	subRepo.createPlanErr = errors.New("db")
	svc := service.NewSubscriptionService(subRepo, linkRepo, nil)

	if _, err := svc.CreatePlan(context.Background(), "prod-1", "t", &domain.CreateSubscriptionPlanRequest{Name: "x"}); err == nil {
		t.Fatal("expected create error")
	}
}

func TestCovSubscription_ListPlansByProduct(t *testing.T) {
	subRepo := covNewSubRepo()
	subRepo.plans["plan-1"] = covPlan("plan-1", "prod-1", "t", "monthly", 0)
	svc := service.NewSubscriptionService(subRepo, covNewLinkRepo(), nil)

	plans, err := svc.ListPlansByProduct(context.Background(), "prod-1")
	if err != nil || len(plans) != 1 {
		t.Errorf("plans = %+v, %v", plans, err)
	}
	subRepo.listPlansErr = errors.New("db")
	if _, err := svc.ListPlansByProduct(context.Background(), "prod-1"); err == nil {
		t.Error("expected list error")
	}
}

func TestCovSubscription_UpdatePlan(t *testing.T) {
	ctx := context.Background()
	subRepo := covNewSubRepo()
	subRepo.plans["plan-1"] = covPlan("plan-1", "prod-1", "tenant-1", "monthly", 0)
	svc := service.NewSubscriptionService(subRepo, covNewLinkRepo(), nil)

	inactive := false
	plan, err := svc.UpdatePlan(ctx, "plan-1", "tenant-1", &domain.UpdateSubscriptionPlanRequest{
		Name: "Novo", Description: "D", PriceCents: 5900, TrialDays: 7,
		SortOrder: 3, Features: []string{"a"}, IsActive: &inactive,
	})
	if err != nil {
		t.Fatalf("UpdatePlan: %v", err)
	}
	if plan.Name != "Novo" || plan.Description != "D" || plan.PriceCents != 5900 ||
		plan.TrialDays != 7 || plan.SortOrder != 3 || len(plan.Features) != 1 || plan.IsActive {
		t.Errorf("plan = %+v", plan)
	}
	if len(subRepo.updatedPlans) != 1 {
		t.Error("plan not persisted")
	}
}

func TestCovSubscription_UpdatePlan_NotFound(t *testing.T) {
	svc := service.NewSubscriptionService(covNewSubRepo(), covNewLinkRepo(), nil)
	if _, err := svc.UpdatePlan(context.Background(), "ghost", "t", &domain.UpdateSubscriptionPlanRequest{}); err == nil {
		t.Fatal("expected not found")
	}
}

func TestCovSubscription_UpdatePlan_RepoError(t *testing.T) {
	subRepo := covNewSubRepo()
	subRepo.plans["plan-1"] = covPlan("plan-1", "prod-1", "t", "monthly", 0)
	subRepo.updatePlanErr = errors.New("db")
	svc := service.NewSubscriptionService(subRepo, covNewLinkRepo(), nil)

	if _, err := svc.UpdatePlan(context.Background(), "plan-1", "t", &domain.UpdateSubscriptionPlanRequest{Name: "x"}); err == nil {
		t.Fatal("expected update error")
	}
}

func TestCovSubscription_DeactivatePlan(t *testing.T) {
	subRepo := covNewSubRepo()
	subRepo.plans["plan-1"] = covPlan("plan-1", "prod-1", "t", "monthly", 0)
	svc := service.NewSubscriptionService(subRepo, covNewLinkRepo(), nil)

	if err := svc.DeactivatePlan(context.Background(), "plan-1", "t"); err != nil {
		t.Fatalf("DeactivatePlan: %v", err)
	}
	if subRepo.plans["plan-1"].IsActive {
		t.Error("plan should be inactive")
	}
	if err := svc.DeactivatePlan(context.Background(), "ghost", "t"); err == nil {
		t.Error("expected not found")
	}

	subRepo.updatePlanErr = errors.New("db")
	if err := svc.DeactivatePlan(context.Background(), "plan-1", "t"); err == nil {
		t.Error("expected update error")
	}
}

// ─── Subscribe ─────────────────────────────────────────────────────────────

func TestCovSubscription_Subscribe_MonthlySuccess(t *testing.T) {
	subRepo := covNewSubRepo()
	subRepo.plans["plan-1"] = covPlan("plan-1", "prod-1", "tenant-1", "monthly", 0)
	a, st := covNewSubAsaasServer(http.StatusOK, http.StatusOK, "")
	defer a.close()
	svc := service.NewSubscriptionService(subRepo, covNewLinkRepo(), a.client())

	sub, err := svc.Subscribe(context.Background(), "plan-1", &domain.SubscribeRequest{
		CustomerName: "Assinante", CustomerEmail: "sub@example.com", CustomerCPF: "123",
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if sub.Status != "active" || sub.AccessToken == "" || sub.AsaasCustomerID != "cus_77" ||
		sub.AsaasSubscriptionID != "asaas-sub-1" || sub.Plan == nil {
		t.Errorf("sub = %+v", sub)
	}
	if sub.CurrentPeriodEnd == nil || sub.CurrentPeriodEnd.Before(time.Now().Add(27*24*time.Hour)) {
		t.Errorf("period end = %v, want ~+1 month", sub.CurrentPeriodEnd)
	}
	if st.cycle != "MONTHLY" || st.value != 49.0 || st.extRef != sub.ID {
		t.Errorf("asaas request cycle=%q value=%v extRef=%q", st.cycle, st.value, st.extRef)
	}
	if len(subRepo.statusUpdates) != 1 || subRepo.statusUpdates[0].status != "active" {
		t.Errorf("status updates = %+v", subRepo.statusUpdates)
	}
}

func TestCovSubscription_Subscribe_YearlyWithTrial(t *testing.T) {
	subRepo := covNewSubRepo()
	subRepo.plans["plan-1"] = covPlan("plan-1", "prod-1", "tenant-1", "yearly", 14)
	a, st := covNewSubAsaasServer(http.StatusOK, http.StatusOK, "")
	defer a.close()
	svc := service.NewSubscriptionService(subRepo, covNewLinkRepo(), a.client())

	sub, err := svc.Subscribe(context.Background(), "plan-1", &domain.SubscribeRequest{
		CustomerName: "A", CustomerEmail: "a@example.com",
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if sub.Status != "trialing" {
		t.Errorf("status = %q, want trialing", sub.Status)
	}
	if sub.CurrentPeriodEnd == nil || sub.CurrentPeriodEnd.After(time.Now().Add(15*24*time.Hour)) {
		t.Errorf("trial end = %v, want +14d", sub.CurrentPeriodEnd)
	}
	if st.cycle != "YEARLY" {
		t.Errorf("cycle = %q", st.cycle)
	}
	wantDue := time.Now().AddDate(0, 0, 14).Format("2006-01-02")
	if st.dueDate != wantDue {
		t.Errorf("nextDueDate = %q, want %q", st.dueDate, wantDue)
	}
}

func TestCovSubscription_Subscribe_PlanNotFoundOrInactive(t *testing.T) {
	subRepo := covNewSubRepo()
	a, _ := covNewSubAsaasServer(http.StatusOK, http.StatusOK, "")
	defer a.close()
	svc := service.NewSubscriptionService(subRepo, covNewLinkRepo(), a.client())

	if _, err := svc.Subscribe(context.Background(), "ghost", &domain.SubscribeRequest{}); err == nil {
		t.Error("expected plan not found")
	}

	inactive := covPlan("plan-off", "prod-1", "t", "monthly", 0)
	inactive.IsActive = false
	subRepo.plans["plan-off"] = inactive
	if _, err := svc.Subscribe(context.Background(), "plan-off", &domain.SubscribeRequest{}); err == nil {
		t.Error("expected inactive plan error")
	}
}

func TestCovSubscription_Subscribe_CustomerError(t *testing.T) {
	subRepo := covNewSubRepo()
	subRepo.plans["plan-1"] = covPlan("plan-1", "prod-1", "t", "monthly", 0)
	a, _ := covNewSubAsaasServer(http.StatusBadRequest, http.StatusOK, "")
	defer a.close()
	svc := service.NewSubscriptionService(subRepo, covNewLinkRepo(), a.client())

	_, err := svc.Subscribe(context.Background(), "plan-1", &domain.SubscribeRequest{CustomerEmail: "a@e.com"})
	if err == nil || !strings.Contains(err.Error(), "customer") {
		t.Fatalf("error = %v", err)
	}
}

func TestCovSubscription_Subscribe_CreateSubRepoError(t *testing.T) {
	subRepo := covNewSubRepo()
	subRepo.plans["plan-1"] = covPlan("plan-1", "prod-1", "t", "monthly", 0)
	subRepo.createSubErr = errors.New("db")
	a, _ := covNewSubAsaasServer(http.StatusOK, http.StatusOK, "")
	defer a.close()
	svc := service.NewSubscriptionService(subRepo, covNewLinkRepo(), a.client())

	if _, err := svc.Subscribe(context.Background(), "plan-1", &domain.SubscribeRequest{CustomerEmail: "a@e.com"}); err == nil {
		t.Fatal("expected create sub error")
	}
}

func TestCovSubscription_Subscribe_AsaasSubscriptionFails_FailOpen(t *testing.T) {
	subRepo := covNewSubRepo()
	subRepo.plans["plan-1"] = covPlan("plan-1", "prod-1", "tenant-1", "monthly", 0)
	a, _ := covNewSubAsaasServer(http.StatusOK, http.StatusBadRequest, `{"errors":"cycle invalido"}`)
	defer a.close()
	svc := service.NewSubscriptionService(subRepo, covNewLinkRepo(), a.client())

	sub, err := svc.Subscribe(context.Background(), "plan-1", &domain.SubscribeRequest{CustomerEmail: "a@e.com"})
	if err != nil {
		t.Fatalf("fail-open expected, got %v", err)
	}
	if sub.AsaasSubscriptionID != "" {
		t.Errorf("asaas id should be empty, got %q", sub.AsaasSubscriptionID)
	}
	if sub.Plan == nil {
		t.Error("plan should be attached on fail-open")
	}
	if len(subRepo.statusUpdates) != 0 {
		t.Error("status should not be updated on fail-open")
	}
}

func TestCovSubscription_Subscribe_UpdateStatusErrorIsNonFatal(t *testing.T) {
	subRepo := covNewSubRepo()
	subRepo.plans["plan-1"] = covPlan("plan-1", "prod-1", "tenant-1", "monthly", 0)
	subRepo.updateStatusErr = errors.New("db")
	a, _ := covNewSubAsaasServer(http.StatusOK, http.StatusOK, "")
	defer a.close()
	svc := service.NewSubscriptionService(subRepo, covNewLinkRepo(), a.client())

	sub, err := svc.Subscribe(context.Background(), "plan-1", &domain.SubscribeRequest{CustomerEmail: "a@e.com"})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if sub.AsaasSubscriptionID != "asaas-sub-1" {
		t.Errorf("asaas id = %q", sub.AsaasSubscriptionID)
	}
}

// ─── Renew / MarkPastDue ───────────────────────────────────────────────────

func TestCovSubscription_RenewSubscription(t *testing.T) {
	subRepo := covNewSubRepo()
	sub := covSub("sub-1", "plan-1", "a@e.com", "active", "asaas-1")
	subRepo.subsByAsaas["asaas-1"] = sub
	subRepo.plans["plan-1"] = covPlan("plan-1", "prod-1", "t", "monthly", 0)
	svc := service.NewSubscriptionService(subRepo, covNewLinkRepo(), nil)

	if err := svc.RenewSubscription(context.Background(), "asaas-1"); err != nil {
		t.Fatalf("RenewSubscription: %v", err)
	}
	if len(subRepo.statusUpdates) != 1 || subRepo.statusUpdates[0].status != "active" ||
		subRepo.statusUpdates[0].periodEnd == nil {
		t.Errorf("updates = %+v", subRepo.statusUpdates)
	}
	// yearly
	subRepo.plans["plan-1"].Interval = "yearly"
	if err := svc.RenewSubscription(context.Background(), "asaas-1"); err != nil {
		t.Fatalf("RenewSubscription yearly: %v", err)
	}
}

func TestCovSubscription_RenewSubscription_NotFound(t *testing.T) {
	svc := service.NewSubscriptionService(covNewSubRepo(), covNewLinkRepo(), nil)
	if err := svc.RenewSubscription(context.Background(), "ghost"); err == nil {
		t.Fatal("expected not found")
	}
}

func TestCovSubscription_RenewSubscription_PlanNotFound(t *testing.T) {
	subRepo := covNewSubRepo()
	subRepo.subsByAsaas["asaas-1"] = covSub("sub-1", "plan-x", "a@e.com", "active", "asaas-1")
	svc := service.NewSubscriptionService(subRepo, covNewLinkRepo(), nil)
	if err := svc.RenewSubscription(context.Background(), "asaas-1"); err == nil ||
		!strings.Contains(err.Error(), "plano não encontrado") {
		t.Fatalf("error = %v", err)
	}
}

func TestCovSubscription_MarkPastDue(t *testing.T) {
	subRepo := covNewSubRepo()
	subRepo.subsByAsaas["asaas-1"] = covSub("sub-1", "plan-1", "a@e.com", "active", "asaas-1")
	svc := service.NewSubscriptionService(subRepo, covNewLinkRepo(), nil)

	if err := svc.MarkPastDue(context.Background(), "asaas-1"); err != nil {
		t.Fatalf("MarkPastDue: %v", err)
	}
	if len(subRepo.statusUpdates) != 1 || subRepo.statusUpdates[0].status != "past_due" {
		t.Errorf("updates = %+v", subRepo.statusUpdates)
	}
	if err := svc.MarkPastDue(context.Background(), "ghost"); err == nil {
		t.Error("expected not found")
	}
}

// ─── Cancel ────────────────────────────────────────────────────────────────

func TestCovSubscription_CancelSubscription(t *testing.T) {
	subRepo := covNewSubRepo()
	subRepo.subsByToken["tok"] = covSub("sub-1", "plan-1", "a@e.com", "active", "asaas-1")
	var deleted string
	a := covNewAsaasServer(func(w http.ResponseWriter, r *http.Request) {
		deleted = r.URL.Path
		w.WriteHeader(http.StatusOK)
	})
	defer a.close()
	svc := service.NewSubscriptionService(subRepo, covNewLinkRepo(), a.client())

	if err := svc.CancelSubscription(context.Background(), "tok"); err != nil {
		t.Fatalf("CancelSubscription: %v", err)
	}
	if deleted != "/subscriptions/asaas-1" {
		t.Errorf("deleted = %q", deleted)
	}
	if len(subRepo.cancelledIDs) != 1 || subRepo.cancelledIDs[0] != "sub-1" {
		t.Errorf("cancelled = %+v", subRepo.cancelledIDs)
	}
}

func TestCovSubscription_CancelSubscription_NoAsaasID(t *testing.T) {
	subRepo := covNewSubRepo()
	subRepo.subsByToken["tok"] = covSub("sub-1", "plan-1", "a@e.com", "active", "")
	svc := service.NewSubscriptionService(subRepo, covNewLinkRepo(), nil)

	if err := svc.CancelSubscription(context.Background(), "tok"); err != nil {
		t.Fatalf("CancelSubscription: %v", err)
	}
	if len(subRepo.cancelledIDs) != 1 {
		t.Error("local cancel not called")
	}
}

func TestCovSubscription_CancelSubscription_NotFound(t *testing.T) {
	svc := service.NewSubscriptionService(covNewSubRepo(), covNewLinkRepo(), nil)
	if err := svc.CancelSubscription(context.Background(), "ghost"); err == nil {
		t.Fatal("expected not found")
	}
}

func TestCovSubscription_CancelSubscription_AlreadyCancelled(t *testing.T) {
	subRepo := covNewSubRepo()
	subRepo.subsByToken["tok"] = covSub("sub-1", "plan-1", "a@e.com", "cancelled", "")
	svc := service.NewSubscriptionService(subRepo, covNewLinkRepo(), nil)

	if err := svc.CancelSubscription(context.Background(), "tok"); err == nil ||
		!strings.Contains(err.Error(), "já cancelada") {
		t.Fatalf("error = %v", err)
	}
}

func TestCovSubscription_CancelSubscription_AsaasErrorStillCancelsLocally(t *testing.T) {
	subRepo := covNewSubRepo()
	subRepo.subsByToken["tok"] = covSub("sub-1", "plan-1", "a@e.com", "active", "asaas-1")
	a := covNewAsaasServer(func(w http.ResponseWriter, _ *http.Request) {
		covJSON(w, http.StatusBadRequest, `{"errors":"already removed"}`)
	})
	defer a.close()
	svc := service.NewSubscriptionService(subRepo, covNewLinkRepo(), a.client())

	if err := svc.CancelSubscription(context.Background(), "tok"); err != nil {
		t.Fatalf("CancelSubscription: %v", err)
	}
	if len(subRepo.cancelledIDs) != 1 {
		t.Error("local cancel must happen even when Asaas fails")
	}
}

func TestCovSubscription_CancelSubscription_LocalError(t *testing.T) {
	subRepo := covNewSubRepo()
	subRepo.subsByToken["tok"] = covSub("sub-1", "plan-1", "a@e.com", "active", "")
	subRepo.cancelErr = errors.New("db")
	svc := service.NewSubscriptionService(subRepo, covNewLinkRepo(), nil)

	if err := svc.CancelSubscription(context.Background(), "tok"); err == nil {
		t.Fatal("expected cancel error")
	}
}

func TestCovSubscription_CancelByAsaasID(t *testing.T) {
	subRepo := covNewSubRepo()
	subRepo.subsByAsaas["asaas-1"] = covSub("sub-1", "plan-1", "a@e.com", "active", "asaas-1")
	svc := service.NewSubscriptionService(subRepo, covNewLinkRepo(), nil)

	if err := svc.CancelSubscriptionByAsaasID(context.Background(), "asaas-1"); err != nil {
		t.Fatalf("CancelSubscriptionByAsaasID: %v", err)
	}
	if len(subRepo.cancelledIDs) != 1 {
		t.Error("cancel not called")
	}

	// idempotente
	subRepo.subsByAsaas["asaas-2"] = covSub("sub-2", "plan-1", "a@e.com", "cancelled", "asaas-2")
	if err := svc.CancelSubscriptionByAsaasID(context.Background(), "asaas-2"); err != nil {
		t.Errorf("idempotent cancel: %v", err)
	}
	if len(subRepo.cancelledIDs) != 1 {
		t.Error("already cancelled subscription must not be cancelled again")
	}

	if err := svc.CancelSubscriptionByAsaasID(context.Background(), "ghost"); err == nil {
		t.Error("expected not found")
	}
}

// ─── GetSubscription / GetSubscriberFiles ──────────────────────────────────

func TestCovSubscription_GetSubscription(t *testing.T) {
	subRepo := covNewSubRepo()
	subRepo.subsByToken["tok"] = covSub("sub-1", "plan-1", "a@e.com", "active", "")
	subRepo.plans["plan-1"] = covPlan("plan-1", "prod-1", "t", "monthly", 0)
	svc := service.NewSubscriptionService(subRepo, covNewLinkRepo(), nil)

	sub, err := svc.GetSubscription(context.Background(), "tok")
	if err != nil {
		t.Fatalf("GetSubscription: %v", err)
	}
	if sub.Plan == nil || sub.Plan.ID != "plan-1" {
		t.Errorf("plan not attached: %+v", sub.Plan)
	}

	if _, err := svc.GetSubscription(context.Background(), "ghost"); err == nil {
		t.Error("expected not found")
	}
}

func TestCovSubscription_GetSubscription_PlanLookupFails(t *testing.T) {
	subRepo := covNewSubRepo()
	subRepo.subsByToken["tok"] = covSub("sub-1", "plan-1", "a@e.com", "active", "")
	subRepo.getPlanErr = errors.New("db")
	svc := service.NewSubscriptionService(subRepo, covNewLinkRepo(), nil)

	sub, err := svc.GetSubscription(context.Background(), "tok")
	if err != nil {
		t.Fatalf("plan lookup must be non-fatal: %v", err)
	}
	if sub.Plan != nil {
		t.Error("plan should be nil")
	}
}

func TestCovSubscription_GetSubscriberFiles(t *testing.T) {
	ctx := context.Background()
	linkRepo := covNewLinkRepo()
	prod := covProduct("prod-1", "tenant-1", "curso", 100, true)
	prod.StorageKey = "s3/file.pdf"
	prod.FileName = "file.pdf"
	prod.MimeType = "application/pdf"
	linkRepo.products["prod-1"] = prod

	subRepo := covNewSubRepo()
	subRepo.subsByToken["tok"] = covSub("sub-1", "plan-1", "a@e.com", "active", "")
	subRepo.plans["plan-1"] = covPlan("plan-1", "prod-1", "tenant-1", "monthly", 0)
	svc := service.NewSubscriptionService(subRepo, linkRepo, nil)

	files, err := svc.GetSubscriberFiles(ctx, "tok")
	if err != nil {
		t.Fatalf("GetSubscriberFiles: %v", err)
	}
	if len(files) != 1 || files[0].DownloadURL != "/api/v1/public/subscriptions/tok/download" ||
		files[0].FileName != "file.pdf" || files[0].MimeType != "application/pdf" {
		t.Errorf("files = %+v", files)
	}

	// legado: só FileURL
	prod.StorageKey = ""
	files, err = svc.GetSubscriberFiles(ctx, "tok")
	if err != nil || len(files) != 1 || files[0].DownloadURL != prod.FileURL {
		t.Errorf("legacy files = %+v, err=%v", files, err)
	}

	// sem arquivo
	prod.FileURL = ""
	files, err = svc.GetSubscriberFiles(ctx, "tok")
	if err != nil || len(files) != 0 {
		t.Errorf("files = %+v, err=%v", files, err)
	}
}

func TestCovSubscription_GetSubscriberFiles_Errors(t *testing.T) {
	ctx := context.Background()
	linkRepo := covNewLinkRepo()
	linkRepo.products["prod-1"] = covProduct("prod-1", "tenant-1", "curso", 100, true)
	subRepo := covNewSubRepo()
	subRepo.subsByToken["tok"] = covSub("sub-1", "plan-1", "a@e.com", "past_due", "")
	subRepo.plans["plan-1"] = covPlan("plan-1", "prod-1", "tenant-1", "monthly", 0)
	svc := service.NewSubscriptionService(subRepo, linkRepo, nil)

	if _, err := svc.GetSubscriberFiles(ctx, "ghost"); err == nil {
		t.Error("expected token error")
	}
	if _, err := svc.GetSubscriberFiles(ctx, "tok"); err == nil ||
		!strings.Contains(err.Error(), "inativa") {
		t.Errorf("error = %v", err)
	}

	subRepo.subsByToken["tok"].Status = "trialing"
	subRepo.getPlanErr = errors.New("db")
	if _, err := svc.GetSubscriberFiles(ctx, "tok"); err == nil {
		t.Error("expected plan error")
	}
	subRepo.getPlanErr = nil

	linkRepo.getProductErr = errors.New("db")
	if _, err := svc.GetSubscriberFiles(ctx, "tok"); err == nil {
		t.Error("expected product error")
	}
}

func TestCovSubscription_ListSubscriptionsByTenant(t *testing.T) {
	subRepo := covNewSubRepo()
	subRepo.byTenant["tenant-1"] = []domain.DigitalSubscription{*covSub("s1", "p1", "a@e.com", "active", "")}
	svc := service.NewSubscriptionService(subRepo, covNewLinkRepo(), nil)

	subs, err := svc.ListSubscriptionsByTenant(context.Background(), "tenant-1")
	if err != nil || len(subs) != 1 {
		t.Errorf("subs = %+v, %v", subs, err)
	}
	subRepo.listByTenantErr = errors.New("db")
	if _, err := svc.ListSubscriptionsByTenant(context.Background(), "tenant-1"); err == nil {
		t.Error("expected list error")
	}
}

// ─── LGPD extras ───────────────────────────────────────────────────────────

func TestCovSubscription_LGPD_ValidationAndRepoErrors(t *testing.T) {
	ctx := context.Background()
	linkRepo := covNewLinkRepo()
	linkRepo.byToken["tok"] = &domain.DigitalPurchase{ID: "p1", CustomerEmail: "buyer@example.com"}
	subRepo := covNewSubRepo()
	svc := service.NewSubscriptionService(subRepo, linkRepo, nil)

	if _, err := svc.EraseBuyerData(ctx, "", "tok"); err == nil {
		t.Error("empty email must be rejected")
	}
	if _, err := svc.ExportBuyerData(ctx, "buyer@example.com", ""); err == nil {
		t.Error("empty token must be rejected")
	}

	linkRepo.erasePurchasesErr = errors.New("db")
	if _, err := svc.EraseBuyerData(ctx, "buyer@example.com", "tok"); err == nil {
		t.Error("expected purchase erase error")
	}
	linkRepo.erasePurchasesErr = nil
	subRepo.eraseByEmailErr = errors.New("db")
	if _, err := svc.EraseBuyerData(ctx, "buyer@example.com", "tok"); err == nil {
		t.Error("expected subscription erase error")
	}

	subRepo.listByEmailErr = errors.New("db")
	if _, err := svc.ExportBuyerData(ctx, "buyer@example.com", "tok"); err == nil {
		t.Error("expected subscription list error")
	}
	subRepo.listByEmailErr = nil
	linkRepo.listPurchasesErr = errors.New("db")
	if _, err := svc.ExportBuyerData(ctx, "buyer@example.com", "tok"); err == nil {
		t.Error("expected purchase list error")
	}
}

func TestCovSubscription_RetentionSweep_Errors(t *testing.T) {
	linkRepo := covNewLinkRepo()
	subRepo := covNewSubRepo()
	svc := service.NewSubscriptionService(subRepo, linkRepo, nil)

	linkRepo.erasePurchasesErr = nil
	// link side uses ErasePurchasesOlderThan (always nil in covLinkRepo) → force via sub error
	subRepo.eraseOlderErr = errors.New("db")
	if _, _, err := svc.RetentionSweep(context.Background()); err == nil {
		t.Error("expected subscription sweep error")
	}
}

func TestCovSubscription_RetentionSweep_Counts(t *testing.T) {
	subRepo := covNewSubRepo()
	subRepo.eraseOlderCount = 4
	svc := service.NewSubscriptionService(subRepo, covNewLinkRepo(), nil)
	p, s, err := svc.RetentionSweep(context.Background())
	if err != nil || p != 0 || s != 4 {
		t.Errorf("sweep = %d/%d, %v", p, s, err)
	}
}

// ─── extra branch coverage ─────────────────────────────────────────────────

func TestCovSubscription_ValidateDataSubject_SubWithoutEmail(t *testing.T) {
	subRepo := covNewSubRepo()
	subRepo.subsByToken["tok-noemail"] = covSub("s", "p", "", "active", "")
	svc := service.NewSubscriptionService(subRepo, covNewLinkRepo(), nil)

	_, err := svc.EraseBuyerData(context.Background(), "someone@example.com", "tok-noemail")
	if err == nil || !strings.Contains(err.Error(), "não pertence") {
		t.Fatalf("error = %v", err)
	}
}

func TestCovSubscription_RetentionSweep_PurchaseError(t *testing.T) {
	linkRepo := covNewLinkRepo()
	linkRepo.eraseOlderErr = errors.New("db")
	svc := service.NewSubscriptionService(covNewSubRepo(), linkRepo, nil)

	if _, _, err := svc.RetentionSweep(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "retenção de compras") {
		t.Fatalf("error = %v", err)
	}
}

func TestCovSubscription_Subscribe_SubscriptionDecodeError(t *testing.T) {
	subRepo := covNewSubRepo()
	subRepo.plans["plan-1"] = covPlan("plan-1", "prod-1", "tenant-1", "monthly", 0)
	a := covNewAsaasServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/customers" {
			covJSON(w, http.StatusOK, `{"id":"cus_1"}`)
			return
		}
		covJSON(w, http.StatusOK, `<not-json>`)
	})
	defer a.close()
	svc := service.NewSubscriptionService(subRepo, covNewLinkRepo(), a.client())

	sub, err := svc.Subscribe(context.Background(), "plan-1", &domain.SubscribeRequest{CustomerEmail: "a@e.com"})
	if err != nil {
		t.Fatalf("fail-open expected: %v", err)
	}
	if sub.AsaasSubscriptionID != "" {
		t.Errorf("asaas id = %q", sub.AsaasSubscriptionID)
	}
}

func TestCovSubscription_Subscribe_GatewayConnectionDrop(t *testing.T) {
	subRepo := covNewSubRepo()
	subRepo.plans["plan-1"] = covPlan("plan-1", "prod-1", "tenant-1", "monthly", 0)
	a := covNewAsaasServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/customers" {
			covJSON(w, http.StatusOK, `{"id":"cus_1"}`)
			return
		}
		// derruba a conexão sem resposta → erro de rede em createLinkPaySubscription
		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "no hijack", http.StatusInternalServerError)
			return
		}
		conn, _, err := hj.Hijack()
		if err == nil {
			_ = conn.Close()
		}
	})
	defer a.close()
	svc := service.NewSubscriptionService(subRepo, covNewLinkRepo(), a.client())

	sub, err := svc.Subscribe(context.Background(), "plan-1", &domain.SubscribeRequest{CustomerEmail: "a@e.com"})
	if err != nil {
		t.Fatalf("fail-open expected: %v", err)
	}
	if sub.AsaasSubscriptionID != "" {
		t.Errorf("asaas id = %q", sub.AsaasSubscriptionID)
	}
}
