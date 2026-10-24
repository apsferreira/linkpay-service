package service_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"linkpay-service/internal/domain"
	"linkpay-service/internal/service"
)

func covProduct(id, tenantID, slug string, price int, active bool) *domain.DigitalProduct {
	return &domain.DigitalProduct{
		ID:            id,
		TenantID:      tenantID,
		Slug:          slug,
		Name:          "Cov Product",
		PriceCents:    price,
		Currency:      "BRL",
		ProductType:   "ebook",
		DownloadLimit: 5,
		IsActive:      active,
		FileURL:       "https://files.example.com/f.pdf",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
}

func covPurchase(id, productID, tenantID, chargeID, status string) *domain.DigitalPurchase {
	return &domain.DigitalPurchase{
		ID:               id,
		ProductID:        productID,
		TenantID:         tenantID,
		CustomerEmail:    "buyer@example.com",
		CustomerName:     "Buyer",
		AmountCents:      10000,
		PaymentStatus:    status,
		ExternalChargeID: chargeID,
		CreatedAt:        time.Now(),
	}
}

// ─── delegating product methods ─────────────────────────────────────────────

func TestCovLinkPay_ProductDelegations(t *testing.T) {
	ctx := context.Background()
	repo := covNewLinkRepo()
	p := covProduct("prod-1", "tenant-1", "curso", 9900, true)
	repo.products[p.ID] = p
	svc := service.NewLinkPayService(repo, nil, nil, nil)

	if got, err := svc.GetProductBySlug(ctx, "curso"); err != nil || got.ID != "prod-1" {
		t.Errorf("GetProductBySlug = %+v, %v", got, err)
	}
	if _, err := svc.GetProductBySlug(ctx, "nope"); err == nil {
		t.Error("expected error for unknown slug")
	}
	if got, err := svc.GetProductByID(ctx, "prod-1", "tenant-1"); err != nil || got.ID != "prod-1" {
		t.Errorf("GetProductByID = %+v, %v", got, err)
	}
	if list, err := svc.ListProducts(ctx, "tenant-1"); err != nil || len(list) != 1 {
		t.Errorf("ListProducts = %+v, %v", list, err)
	}
	if err := svc.UpdateProductStorage(ctx, "prod-1", "tenant-1", "key", "bucket", "f.pdf", 10, "application/pdf"); err != nil {
		t.Errorf("UpdateProductStorage: %v", err)
	}
	if repo.lastStorageKey != "key" || repo.lastStorageBucket != "bucket" || repo.lastFileName != "f.pdf" ||
		repo.lastFileSize != 10 || repo.lastMimeType != "application/pdf" {
		t.Errorf("storage args not forwarded: %+v", repo)
	}
	if err := svc.UpdateProductCover(ctx, "prod-1", "tenant-1", "https://cdn/cover.png"); err != nil {
		t.Errorf("UpdateProductCover: %v", err)
	}
	if repo.lastCoverURL != "https://cdn/cover.png" {
		t.Errorf("cover url = %q", repo.lastCoverURL)
	}
	if err := svc.DeactivateProduct(ctx, "prod-1", "tenant-1"); err != nil {
		t.Errorf("DeactivateProduct: %v", err)
	}
	if repo.products["prod-1"].IsActive {
		t.Error("product should be inactive")
	}

	repo.stats = &domain.LinkPayStatsResponse{Period: "30d", TotalSales: 4}
	if st, err := svc.GetSalesStats(ctx, "tenant-1"); err != nil || st.TotalSales != 4 {
		t.Errorf("GetSalesStats = %+v, %v", st, err)
	}
	repo.commissionSummary = &domain.CommissionSummaryResponse{TotalSales: 9}
	if cs, err := svc.GetCommissionSummary(ctx, "2026-01-01", "2026-01-31"); err != nil || cs.TotalSales != 9 {
		t.Errorf("GetCommissionSummary = %+v, %v", cs, err)
	}
	repo.upsells = []domain.UpsellSuggestion{{ProductID: "prod-2", DisplayOrder: 1}}
	if up, err := svc.GetUpsellsByProductID(ctx, "prod-1"); err != nil || len(up) != 1 {
		t.Errorf("GetUpsellsByProductID = %+v, %v", up, err)
	}
}

func TestCovLinkPay_ProductDelegationErrors(t *testing.T) {
	ctx := context.Background()
	repo := covNewLinkRepo()
	repo.listProductsErr = errors.New("db")
	repo.updateStorageErr = errors.New("db")
	repo.updateCoverErr = errors.New("db")
	repo.deactivateErr = errors.New("db")
	repo.statsErr = errors.New("db")
	repo.commissionSummaryErr = errors.New("db")
	repo.upsellErr = errors.New("db")
	svc := service.NewLinkPayService(repo, nil, nil, nil)

	if _, err := svc.ListProducts(ctx, "t"); err == nil {
		t.Error("ListProducts should fail")
	}
	if err := svc.UpdateProductStorage(ctx, "p", "t", "k", "b", "f", 1, "m"); err == nil {
		t.Error("UpdateProductStorage should fail")
	}
	if err := svc.UpdateProductCover(ctx, "p", "t", "u"); err == nil {
		t.Error("UpdateProductCover should fail")
	}
	if err := svc.DeactivateProduct(ctx, "p", "t"); err == nil {
		t.Error("DeactivateProduct should fail")
	}
	if _, err := svc.GetSalesStats(ctx, "t"); err == nil {
		t.Error("GetSalesStats should fail")
	}
	if _, err := svc.GetCommissionSummary(ctx, "a", "b"); err == nil {
		t.Error("GetCommissionSummary should fail")
	}
	if _, err := svc.GetUpsellsByProductID(ctx, "p"); err == nil {
		t.Error("GetUpsellsByProductID should fail")
	}
}

// ─── UpdateProduct ──────────────────────────────────────────────────────────

func TestCovLinkPay_UpdateProduct(t *testing.T) {
	ctx := context.Background()
	repo := covNewLinkRepo()
	repo.products["prod-1"] = covProduct("prod-1", "tenant-1", "curso", 1000, true)
	svc := service.NewLinkPayService(repo, nil, nil, nil)

	inactive := false
	got, err := svc.UpdateProduct(ctx, "prod-1", "tenant-1", &domain.UpdateDigitalProductRequest{
		Name:          "Novo",
		Description:   "Desc",
		PriceCents:    2500,
		ProductType:   "course",
		FileURL:       "https://files/new.pdf",
		DownloadLimit: 9,
		IsActive:      &inactive,
	})
	if err != nil {
		t.Fatalf("UpdateProduct: %v", err)
	}
	if got.Name != "Novo" || got.Description != "Desc" || got.PriceCents != 2500 ||
		got.ProductType != "course" || got.FileURL != "https://files/new.pdf" ||
		got.DownloadLimit != 9 || got.IsActive {
		t.Errorf("product not updated: %+v", got)
	}
}

func TestCovLinkPay_UpdateProduct_NotFound(t *testing.T) {
	svc := service.NewLinkPayService(covNewLinkRepo(), nil, nil, nil)
	if _, err := svc.UpdateProduct(context.Background(), "missing", "t", &domain.UpdateDigitalProductRequest{}); err == nil {
		t.Fatal("expected not found")
	}
}

func TestCovLinkPay_UpdateProduct_RepoError(t *testing.T) {
	repo := covNewLinkRepo()
	repo.products["prod-1"] = covProduct("prod-1", "t", "s", 1000, true)
	repo.updateProductErr = errors.New("db")
	svc := service.NewLinkPayService(repo, nil, nil, nil)
	if _, err := svc.UpdateProduct(context.Background(), "prod-1", "t", &domain.UpdateDigitalProductRequest{Name: "x"}); err == nil {
		t.Fatal("expected update error")
	}
}

func TestCovLinkPay_CreateProduct_RepoError(t *testing.T) {
	repo := covNewLinkRepo()
	repo.createProductErr = errors.New("db")
	svc := service.NewLinkPayService(repo, nil, nil, nil)
	if _, err := svc.CreateProduct(context.Background(), "t", &domain.CreateDigitalProductRequest{Name: "x"}); err == nil {
		t.Fatal("expected create error")
	}
}

// ─── InitiatePurchase ───────────────────────────────────────────────────────

func covPurchaseAsaasServer(paymentStatus int, paymentBody string, onCustomer func(r *http.Request)) *covAsaasServer {
	return covNewAsaasServer(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/customers":
			if onCustomer != nil {
				onCustomer(r)
			}
			covJSON(w, http.StatusOK, `{"id":"cus_9","name":"Buyer","email":"buyer@example.com"}`)
		case "/payments":
			covJSON(w, paymentStatus, paymentBody)
		default:
			http.NotFound(w, r)
		}
	})
}

func TestCovLinkPay_InitiatePurchase_SuccessPix(t *testing.T) {
	repo := covNewLinkRepo()
	repo.products["prod-1"] = covProduct("prod-1", "tenant-1", "curso", 10000, true)
	a := covPurchaseAsaasServer(http.StatusOK,
		`{"id":"pay_1","status":"PENDING","invoiceUrl":"https://asaas/inv/1","pixTransaction":{"encodedImage":"base64img","payload":"00020126"}}`, nil)
	defer a.close()
	svc := service.NewLinkPayService(repo, a.client(), nil, nil)

	p, err := svc.InitiatePurchase(context.Background(), "curso", &domain.InitiatePurchaseRequest{
		CustomerName: "Buyer", CustomerEmail: "buyer@example.com", PaymentMethod: "pix",
	})
	if err != nil {
		t.Fatalf("InitiatePurchase: %v", err)
	}
	if p.ExternalChargeID != "pay_1" || p.PixQRCode != "base64img" ||
		p.PixCopyPaste != "00020126" || p.PaymentURL != "https://asaas/inv/1" {
		t.Errorf("purchase = %+v", p)
	}
	if p.PlatformFeeCents != 3000 || p.CreatorEarnings != 7000 {
		t.Errorf("commission split = %d/%d", p.PlatformFeeCents, p.CreatorEarnings)
	}
	if len(repo.purchases) != 1 {
		t.Error("purchase was not persisted")
	}
}

func TestCovLinkPay_InitiatePurchase_CreditCardBillingType(t *testing.T) {
	repo := covNewLinkRepo()
	repo.products["prod-1"] = covProduct("prod-1", "tenant-1", "curso", 5000, true)
	var billing string
	a := covPurchaseAsaasServer(http.StatusOK, `{"id":"pay_2","status":"PENDING"}`, nil)
	// tap the /payments body
	a.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/payments" {
			req := covDecodeBody[service.AsaasPaymentRequest](t, r)
			billing = req.BillingType
			covJSON(w, http.StatusOK, `{"id":"pay_2","status":"PENDING"}`)
			return
		}
		covJSON(w, http.StatusOK, `{"id":"cus_9"}`)
	})
	defer a.close()
	svc := service.NewLinkPayService(repo, a.client(), nil, nil)

	if _, err := svc.InitiatePurchase(context.Background(), "curso", &domain.InitiatePurchaseRequest{
		CustomerName: "Buyer", CustomerEmail: "b@example.com", PaymentMethod: "credit_card",
	}); err != nil {
		t.Fatalf("InitiatePurchase: %v", err)
	}
	if billing != "CREDIT_CARD" {
		t.Errorf("billingType = %q, want CREDIT_CARD", billing)
	}
}

func TestCovLinkPay_InitiatePurchase_ProductErrors(t *testing.T) {
	repo := covNewLinkRepo()
	svc := service.NewLinkPayService(repo, nil, nil, nil)

	if _, err := svc.InitiatePurchase(context.Background(), "ghost", &domain.InitiatePurchaseRequest{}); err == nil ||
		!strings.Contains(err.Error(), "não encontrado") {
		t.Errorf("error = %v", err)
	}

	repo.products["off"] = covProduct("p-off", "t", "off", 100, false)
	if _, err := svc.InitiatePurchase(context.Background(), "off", &domain.InitiatePurchaseRequest{}); err == nil ||
		!strings.Contains(err.Error(), "indisponível") {
		t.Errorf("error = %v", err)
	}
}

func TestCovLinkPay_InitiatePurchase_CustomerError(t *testing.T) {
	repo := covNewLinkRepo()
	repo.products["prod-1"] = covProduct("prod-1", "t", "curso", 100, true)
	a := covPurchaseAsaasServer(http.StatusBadRequest, `{"errors":"bad"}`, nil)
	a.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		covJSON(w, http.StatusBadRequest, `{"errors":"cpf invalido"}`)
	})
	defer a.close()
	svc := service.NewLinkPayService(repo, a.client(), nil, nil)

	_, err := svc.InitiatePurchase(context.Background(), "curso", &domain.InitiatePurchaseRequest{CustomerEmail: "b@e.com", PaymentMethod: "pix"})
	if err == nil || !strings.Contains(err.Error(), "registrar cliente") {
		t.Fatalf("error = %v", err)
	}
}

func TestCovLinkPay_InitiatePurchase_PaymentError(t *testing.T) {
	repo := covNewLinkRepo()
	repo.products["prod-1"] = covProduct("prod-1", "t", "curso", 100, true)
	a := covPurchaseAsaasServer(http.StatusUnprocessableEntity, `{"errors":"valor minimo"}`, nil)
	defer a.close()
	svc := service.NewLinkPayService(repo, a.client(), nil, nil)

	_, err := svc.InitiatePurchase(context.Background(), "curso", &domain.InitiatePurchaseRequest{CustomerEmail: "b@e.com", PaymentMethod: "pix"})
	if err == nil || !strings.Contains(err.Error(), "criar cobrança") {
		t.Fatalf("error = %v", err)
	}
}

func TestCovLinkPay_InitiatePurchase_PaymentInvalidJSON(t *testing.T) {
	repo := covNewLinkRepo()
	repo.products["prod-1"] = covProduct("prod-1", "t", "curso", 100, true)
	a := covPurchaseAsaasServer(http.StatusOK, `not-json`, nil)
	defer a.close()
	svc := service.NewLinkPayService(repo, a.client(), nil, nil)

	if _, err := svc.InitiatePurchase(context.Background(), "curso", &domain.InitiatePurchaseRequest{CustomerEmail: "b@e.com", PaymentMethod: "pix"}); err == nil {
		t.Fatal("expected decode error")
	}
}

func TestCovLinkPay_InitiatePurchase_PersistError(t *testing.T) {
	repo := covNewLinkRepo()
	repo.products["prod-1"] = covProduct("prod-1", "t", "curso", 100, true)
	repo.createPurchaseErr = errors.New("db")
	a := covPurchaseAsaasServer(http.StatusOK, `{"id":"pay_1"}`, nil)
	defer a.close()
	svc := service.NewLinkPayService(repo, a.client(), nil, nil)

	_, err := svc.InitiatePurchase(context.Background(), "curso", &domain.InitiatePurchaseRequest{CustomerEmail: "b@e.com", PaymentMethod: "pix"})
	if err == nil || !strings.Contains(err.Error(), "persistir compra") {
		t.Fatalf("error = %v", err)
	}
}

// ─── ProcessPaymentConfirmed ────────────────────────────────────────────────

func covConfirmedFixture(t *testing.T, repo *covLinkRepo) *domain.DigitalPurchase {
	t.Helper()
	repo.products["prod-1"] = covProduct("prod-1", "tenant-1", "curso", 10000, true)
	p := covPurchase("purch-1", "prod-1", "tenant-1", "charge-1", "pending")
	repo.purchases[p.ID] = p
	repo.byCharge["charge-1"] = p
	return p
}

func TestCovLinkPay_ProcessPaymentConfirmed_PublishesEventAndLedger(t *testing.T) {
	repo := covNewLinkRepo()
	_ = covConfirmedFixture(t, repo)
	repo.products["prod-1"].PlatformCommissionPct = 20
	pub := &covPublisher{}
	svc := service.NewLinkPayService(repo, nil, pub, nil)

	if err := svc.ProcessPaymentConfirmed(context.Background(), "charge-1"); err != nil {
		t.Fatalf("ProcessPaymentConfirmed: %v", err)
	}
	if repo.confirmCalls != 1 || repo.lastConfirmedToken == "" {
		t.Error("ConfirmPurchase not called with token")
	}
	if len(pub.events) != 1 || pub.events[0].PurchaseID != "purch-1" || pub.events[0].AccessToken == "" {
		t.Errorf("events = %+v", pub.events)
	}
	if repo.lastCommissionEntry == nil || repo.lastCommissionEntry.CommissionCents != 2000 ||
		repo.lastCommissionEntry.CreatorPayoutCents != 8000 {
		t.Errorf("commission entry = %+v", repo.lastCommissionEntry)
	}
}

func TestCovLinkPay_ProcessPaymentConfirmed_ConfirmError(t *testing.T) {
	repo := covNewLinkRepo()
	covConfirmedFixture(t, repo)
	repo.confirmErr = errors.New("db")
	svc := service.NewLinkPayService(repo, nil, &covPublisher{}, nil)

	err := svc.ProcessPaymentConfirmed(context.Background(), "charge-1")
	if err == nil || !strings.Contains(err.Error(), "confirmar compra") {
		t.Fatalf("error = %v", err)
	}
}

func TestCovLinkPay_ProcessPaymentConfirmed_PublisherFails_FallsBackToNotifier(t *testing.T) {
	repo := covNewLinkRepo()
	covConfirmedFixture(t, repo)
	pub := &covPublisher{err: errors.New("amqp down")}
	notif := &covNotifier{}
	svc := service.NewLinkPayServiceWithNotifier(repo, nil, pub, nil, notif)

	if err := svc.ProcessPaymentConfirmed(context.Background(), "charge-1"); err != nil {
		t.Fatalf("ProcessPaymentConfirmed: %v", err)
	}
	if notif.calls != 1 || notif.lastTo != "buyer@example.com" {
		t.Errorf("notifier calls = %d, to = %q", notif.calls, notif.lastTo)
	}
}

func TestCovLinkPay_ProcessPaymentConfirmed_NotifierFails_FallsBackToEmail(t *testing.T) {
	repo := covNewLinkRepo()
	covConfirmedFixture(t, repo)
	notif := &covNotifier{err: errors.New("http 500")}
	email := &covEmailSender{}
	svc := service.NewLinkPayServiceWithNotifier(repo, nil, nil, email, notif)

	if err := svc.ProcessPaymentConfirmed(context.Background(), "charge-1"); err != nil {
		t.Fatalf("ProcessPaymentConfirmed: %v", err)
	}
	if notif.calls != 1 || email.calls != 1 {
		t.Errorf("notif=%d email=%d, want 1/1", notif.calls, email.calls)
	}
}

func TestCovLinkPay_ProcessPaymentConfirmed_AllChannelsFail_NoError(t *testing.T) {
	repo := covNewLinkRepo()
	covConfirmedFixture(t, repo)
	pub := &covPublisher{err: errors.New("amqp")}
	notif := &covNotifier{err: errors.New("http")}
	email := &covEmailSender{err: errors.New("resend")}
	svc := service.NewLinkPayServiceWithNotifier(repo, nil, pub, email, notif)

	if err := svc.ProcessPaymentConfirmed(context.Background(), "charge-1"); err != nil {
		t.Fatalf("failures must be non-fatal, got %v", err)
	}
	if email.calls != 1 {
		t.Errorf("email calls = %d", email.calls)
	}
}

func TestCovLinkPay_ProcessPaymentConfirmed_ProductLookupFails_NilError(t *testing.T) {
	repo := covNewLinkRepo()
	covConfirmedFixture(t, repo)
	repo.getProductErr = errors.New("db down")
	notif := &covNotifier{}
	email := &covEmailSender{}
	svc := service.NewLinkPayServiceWithNotifier(repo, nil, nil, email, notif)

	if err := svc.ProcessPaymentConfirmed(context.Background(), "charge-1"); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if notif.calls != 0 || email.calls != 0 {
		t.Errorf("no channel should fire: notif=%d email=%d", notif.calls, email.calls)
	}
}

func TestCovLinkPay_ProcessPaymentConfirmed_NoNotifiers(t *testing.T) {
	repo := covNewLinkRepo()
	covConfirmedFixture(t, repo)
	svc := service.NewLinkPayService(repo, nil, nil, nil)

	if err := svc.ProcessPaymentConfirmed(context.Background(), "charge-1"); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if repo.confirmCalls != 1 {
		t.Error("purchase should be confirmed")
	}
}

func TestCovLinkPay_ProcessPaymentConfirmed_LedgerFailureIsNonFatal(t *testing.T) {
	repo := covNewLinkRepo()
	covConfirmedFixture(t, repo)
	repo.insertCommissionErr = errors.New("ledger down")
	pub := &covPublisher{}
	svc := service.NewLinkPayService(repo, nil, pub, nil)

	if err := svc.ProcessPaymentConfirmed(context.Background(), "charge-1"); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if len(pub.events) != 1 {
		t.Error("event should still be published")
	}
}

// ─── RecordCommissionEntry ──────────────────────────────────────────────────

func TestCovLinkPay_RecordCommissionEntry(t *testing.T) {
	repo := covNewLinkRepo()
	prod := covProduct("prod-1", "tenant-1", "curso", 10000, true)
	repo.products["prod-1"] = prod
	svc := service.NewLinkPayService(repo, nil, nil, nil)

	purch := covPurchase("p1", "prod-1", "tenant-1", "ch", "confirmed")
	purch.AmountCents = 12345
	if err := svc.RecordCommissionEntry(context.Background(), purch); err != nil {
		t.Fatalf("RecordCommissionEntry: %v", err)
	}
	// default 30%
	if repo.lastCommissionEntry.CommissionPercent != 30 || repo.lastCommissionEntry.CommissionCents != 3704 {
		t.Errorf("entry = %+v", repo.lastCommissionEntry)
	}
	if repo.lastCommissionEntry.CreatorPayoutCents != 12345-3704 {
		t.Errorf("creator payout = %d", repo.lastCommissionEntry.CreatorPayoutCents)
	}

	repo.getProductErr = errors.New("db")
	if err := svc.RecordCommissionEntry(context.Background(), purch); err == nil {
		t.Fatal("expected product error")
	}
}

// ─── files ─────────────────────────────────────────────────────────────────

func TestCovLinkPay_ProductFiles(t *testing.T) {
	ctx := context.Background()
	repo := covNewLinkRepo()
	svc := service.NewLinkPayService(repo, nil, nil, nil)

	f := &domain.DigitalProductFile{ID: "f1", ProductID: "p1", FileName: "a.pdf"}
	if err := svc.AddProductFile(ctx, f); err != nil {
		t.Fatalf("AddProductFile: %v", err)
	}
	if list, err := svc.ListProductFiles(ctx, "p1"); err != nil || len(list) != 1 {
		t.Errorf("ListProductFiles = %+v, %v", list, err)
	}
	if got, err := svc.GetProductFile(ctx, "f1"); err != nil || got.FileName != "a.pdf" {
		t.Errorf("GetProductFile = %+v, %v", got, err)
	}
	if err := svc.DeleteProductFile(ctx, "f1", "p1"); err != nil || repo.deleteFileCalls != 1 {
		t.Errorf("DeleteProductFile err=%v calls=%d", err, repo.deleteFileCalls)
	}
	if _, err := svc.GetProductFile(ctx, "ghost"); err == nil {
		t.Error("expected not found")
	}

	repo.addFileErr = errors.New("db")
	repo.listFilesErr = errors.New("db")
	repo.getFileErr = errors.New("db")
	repo.deleteFileErr = errors.New("db")
	if err := svc.AddProductFile(ctx, f); err == nil {
		t.Error("AddProductFile should fail")
	}
	if _, err := svc.ListProductFiles(ctx, "p1"); err == nil {
		t.Error("ListProductFiles should fail")
	}
	if _, err := svc.GetProductFile(ctx, "f1"); err == nil {
		t.Error("GetProductFile should fail")
	}
	if err := svc.DeleteProductFile(ctx, "f1", "p1"); err == nil {
		t.Error("DeleteProductFile should fail")
	}
}

// ─── RefundPurchase ────────────────────────────────────────────────────────

func covRefundFixture(repo *covLinkRepo) *domain.DigitalPurchase {
	p := covPurchase("p-refund", "prod-1", "tenant-1", "pay-refund", "confirmed")
	p.AccessToken = "tok-refund"
	repo.purchases[p.ID] = p
	return p
}

func TestCovLinkPay_RefundPurchase_Success(t *testing.T) {
	repo := covNewLinkRepo()
	covRefundFixture(repo)
	var refunded string
	a := covNewAsaasServer(func(w http.ResponseWriter, r *http.Request) {
		refunded = r.URL.Path
		covJSON(w, http.StatusOK, `{"id":"refund_1"}`)
	})
	defer a.close()
	svc := service.NewLinkPayService(repo, a.client(), nil, nil)

	if err := svc.RefundPurchase(context.Background(), "p-refund", "tok-refund"); err != nil {
		t.Fatalf("RefundPurchase: %v", err)
	}
	if refunded != "/payments/pay-refund/refund" {
		t.Errorf("refund path = %q", refunded)
	}
	if len(repo.statusUpdates) != 1 || repo.statusUpdates[0].status != "refunded" {
		t.Errorf("status updates = %+v", repo.statusUpdates)
	}
}

func TestCovLinkPay_RefundPurchase_NotFound(t *testing.T) {
	svc := service.NewLinkPayService(covNewLinkRepo(), nil, nil, nil)
	if err := svc.RefundPurchase(context.Background(), "ghost", "tok"); err == nil ||
		!strings.Contains(err.Error(), "não encontrada") {
		t.Fatalf("error = %v", err)
	}
}

func TestCovLinkPay_RefundPurchase_InvalidToken(t *testing.T) {
	repo := covNewLinkRepo()
	covRefundFixture(repo)
	svc := service.NewLinkPayService(repo, nil, nil, nil)

	if err := svc.RefundPurchase(context.Background(), "p-refund", "wrong"); err == nil ||
		!strings.Contains(err.Error(), "token") {
		t.Fatalf("error = %v", err)
	}
}

func TestCovLinkPay_RefundPurchase_NotConfirmed(t *testing.T) {
	repo := covNewLinkRepo()
	p := covRefundFixture(repo)
	p.PaymentStatus = "refunded"
	svc := service.NewLinkPayService(repo, nil, nil, nil)

	if err := svc.RefundPurchase(context.Background(), p.ID, "tok-refund"); err == nil ||
		!strings.Contains(err.Error(), "não está confirmada") {
		t.Fatalf("error = %v", err)
	}
}

func TestCovLinkPay_RefundPurchase_WindowExpired(t *testing.T) {
	repo := covNewLinkRepo()
	p := covRefundFixture(repo)
	p.CreatedAt = time.Now().Add(-8 * 24 * time.Hour)
	svc := service.NewLinkPayService(repo, nil, nil, nil)

	if err := svc.RefundPurchase(context.Background(), p.ID, "tok-refund"); err == nil ||
		!strings.Contains(err.Error(), "expirado") {
		t.Fatalf("error = %v", err)
	}
}

func TestCovLinkPay_RefundPurchase_AsaasError(t *testing.T) {
	repo := covNewLinkRepo()
	covRefundFixture(repo)
	a := covNewAsaasServer(func(w http.ResponseWriter, _ *http.Request) {
		covJSON(w, http.StatusBadRequest, `{"errors":"estorno indisponivel"}`)
	})
	defer a.close()
	svc := service.NewLinkPayService(repo, a.client(), nil, nil)

	if err := svc.RefundPurchase(context.Background(), "p-refund", "tok-refund"); err == nil ||
		!strings.Contains(err.Error(), "gateway") {
		t.Fatalf("error = %v", err)
	}
	if len(repo.statusUpdates) != 0 {
		t.Error("status must not change when gateway fails")
	}
}

func TestCovLinkPay_RefundPurchase_UpdateStatusError(t *testing.T) {
	repo := covNewLinkRepo()
	covRefundFixture(repo)
	repo.updatePurchaseStatusErr = errors.New("db")
	a := covNewAsaasServer(func(w http.ResponseWriter, _ *http.Request) {
		covJSON(w, http.StatusOK, `{"id":"refund_1"}`)
	})
	defer a.close()
	svc := service.NewLinkPayService(repo, a.client(), nil, nil)

	if err := svc.RefundPurchase(context.Background(), "p-refund", "tok-refund"); err == nil ||
		!strings.Contains(err.Error(), "atualizar status") {
		t.Fatalf("error = %v", err)
	}
}

func TestCovLinkPay_RefundPurchase_NoAsaasClient(t *testing.T) {
	repo := covNewLinkRepo()
	covRefundFixture(repo)
	svc := service.NewLinkPayService(repo, nil, nil, nil)

	if err := svc.RefundPurchase(context.Background(), "p-refund", "tok-refund"); err != nil {
		t.Fatalf("RefundPurchase without gateway: %v", err)
	}
}

// ─── Download extra paths ──────────────────────────────────────────────────

func TestCovLinkPay_Download_StorageKeyAndIncrementError(t *testing.T) {
	ctx := context.Background()
	repo := covNewLinkRepo()
	prod := covProduct("prod-1", "tenant-1", "curso", 100, true)
	prod.StorageKey = "s3/key.pdf"
	prod.DownloadLimit = 3
	repo.products["prod-1"] = prod

	exp := time.Now().Add(time.Hour)
	p := covPurchase("p", "prod-1", "tenant-1", "ch", "confirmed")
	p.AccessToken = "tok"
	p.AccessExpiresAt = &exp
	repo.byToken["tok"] = p

	repo.incrementErr = errors.New("redis down")
	svc := service.NewLinkPayService(repo, nil, nil, nil)
	url, err := svc.Download(ctx, "tok")
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if url != "s3/key.pdf" {
		t.Errorf("url = %q", url)
	}

	if _, err := svc.Download(ctx, "missing"); err == nil {
		t.Error("expected invalid token error")
	}

	prod.ID = "other"
	p.ProductID = "missing-prod"
	if _, err := svc.Download(ctx, "tok"); err == nil {
		t.Error("expected product not found")
	}
}

func TestCovLinkPay_GetPurchaseByToken(t *testing.T) {
	repo := covNewLinkRepo()
	p := covPurchase("p", "prod", "t", "ch", "confirmed")
	repo.byToken["tok"] = p
	svc := service.NewLinkPayService(repo, nil, nil, nil)

	if got, err := svc.GetPurchaseByToken(context.Background(), "tok"); err != nil || got.ID != "p" {
		t.Errorf("GetPurchaseByToken = %+v, %v", got, err)
	}
}

func TestCovLinkPay_InitiatePurchase_PaymentConnectionDrop(t *testing.T) {
	repo := covNewLinkRepo()
	repo.products["prod-1"] = covProduct("prod-1", "t", "curso", 100, true)
	a := covNewAsaasServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/customers" {
			covJSON(w, http.StatusOK, `{"id":"cus_1"}`)
			return
		}
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
	svc := service.NewLinkPayService(repo, a.client(), nil, nil)

	_, err := svc.InitiatePurchase(context.Background(), "curso", &domain.InitiatePurchaseRequest{
		CustomerEmail: "b@e.com", PaymentMethod: "pix",
	})
	if err == nil || !strings.Contains(err.Error(), "criar cobrança") {
		t.Fatalf("error = %v", err)
	}
}
