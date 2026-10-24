package repository

import (
	"context"
	"testing"
	"time"

	"linkpay-service/internal/domain"
)

// covNewPlan inserts a subscription plan through the repository.
func covNewPlan(t *testing.T, repo *SubscriptionRepository, product *domain.DigitalProduct, slug string, active bool) *domain.DigitalSubscriptionPlan {
	t.Helper()
	now := covNow()
	plan := &domain.DigitalSubscriptionPlan{
		ID: covUUID(t), ProductID: product.ID, TenantID: product.TenantID,
		Name: "Plan " + slug, Slug: slug, Description: "plan description",
		PriceCents: 2990, Interval: "monthly", TrialDays: 7,
		IsActive: active, SortOrder: 1, CreatedAt: now, UpdatedAt: now,
	}
	covNoErr(t, repo.CreatePlan(context.Background(), plan), "CreatePlan %s", slug)
	return plan
}

// covNewSubscription inserts a subscription through the repository.
func covNewSubscription(t *testing.T, repo *SubscriptionRepository, plan *domain.DigitalSubscriptionPlan, email, token, asaasID, status string, createdAgo time.Duration) *domain.DigitalSubscription {
	t.Helper()
	createdAt := covNow().Add(-createdAgo)
	start := createdAt
	end := createdAt.AddDate(0, 1, 0)
	sub := &domain.DigitalSubscription{
		ID: covUUID(t), PlanID: plan.ID, TenantID: plan.TenantID,
		CustomerEmail: email, CustomerName: "Sub " + email, CustomerCPF: "12345678901",
		AsaasSubscriptionID: asaasID, AsaasCustomerID: "cus_123",
		Status: status, CurrentPeriodStart: &start, CurrentPeriodEnd: &end,
		AccessToken: token, CreatedAt: createdAt, UpdatedAt: createdAt,
	}
	covNoErr(t, repo.CreateSubscription(context.Background(), sub), "CreateSubscription")
	return sub
}

// --- Plans ------------------------------------------------------------------

func TestCovPlanCRUD(t *testing.T) {
	pool := covDB(t)
	ctx, cancel := covCtx(t)
	defer cancel()
	repo := NewSubscriptionRepository(pool)

	product := covNewProduct(t, pool, covTenantA, "plan-product")
	plan := covNewPlan(t, repo, product, "basic", true)
	premium := covNewPlan(t, repo, product, "premium", true)
	inactive := covNewPlan(t, repo, product, "legacy", false)

	// Features nil → stored as empty array and read back as empty slice.
	got, err := repo.GetPlanByID(ctx, plan.ID, covTenantA)
	covNoErr(t, err, "GetPlanByID with tenant")
	covEqual(t, got.ID, plan.ID, "plan id")
	covEqual(t, got.Name, plan.Name, "plan name")
	covEqual(t, got.Description, "plan description", "plan description")
	covEqual(t, got.PriceCents, 2990, "price")
	covEqual(t, got.Interval, "monthly", "interval")
	covEqual(t, got.TrialDays, 7, "trial days")
	covEqual(t, got.IsActive, true, "is active")
	covEqual(t, len(got.Features), 0, "features empty")

	// Empty tenant → no tenant filter.
	gotNoTenant, err := repo.GetPlanByID(ctx, plan.ID, "")
	covNoErr(t, err, "GetPlanByID without tenant")
	covEqual(t, gotNoTenant.ID, plan.ID, "plan id without tenant")

	// Tenant mismatch / unknown id → scan error.
	_, err = repo.GetPlanByID(ctx, plan.ID, covTenantB)
	covWantErr(t, err, "GetPlanByID wrong tenant")

	_, err = repo.GetPlanByID(ctx, "00000000-0000-0000-0000-000000000000", "")
	covWantErr(t, err, "GetPlanByID unknown")

	_, err = repo.GetPlanByID(ctx, "not-a-uuid", "")
	covWantErr(t, err, "GetPlanByID invalid uuid")

	// Only active plans, ordered by sort_order.
	plans, err := repo.ListPlansByProduct(ctx, product.ID)
	covNoErr(t, err, "ListPlansByProduct")
	covEqual(t, len(plans), 2, "active plans")
	covEqual(t, plans[0].ID, plan.ID, "first plan")
	covEqual(t, plans[1].ID, premium.ID, "second plan")

	// Empty list for a product without plans.
	otherProduct := covNewProduct(t, pool, covTenantA, "plan-product-empty")
	none, err := repo.ListPlansByProduct(ctx, otherProduct.ID)
	covNoErr(t, err, "ListPlansByProduct empty")
	if none == nil {
		t.Fatalf("empty plan list must be an empty slice")
	}
	covEqual(t, len(none), 0, "empty plan list")

	_, err = repo.ListPlansByProduct(ctx, "not-a-uuid")
	covWantErr(t, err, "ListPlansByProduct invalid uuid")

	// UpdatePlan persists the mutable fields.
	plan.Name = "Basic updated"
	plan.Description = "new description"
	plan.PriceCents = 3490
	plan.TrialDays = 0
	plan.SortOrder = 5
	plan.Features = []string{"a", "b"}
	plan.IsActive = false
	covNoErr(t, repo.UpdatePlan(ctx, plan), "UpdatePlan")

	got, err = repo.GetPlanByID(ctx, plan.ID, covTenantA)
	covNoErr(t, err, "GetPlanByID after update")
	covEqual(t, got.Name, "Basic updated", "updated name")
	covEqual(t, got.PriceCents, 3490, "updated price")
	covEqual(t, len(got.Features), 2, "updated features")
	covEqual(t, got.IsActive, false, "updated is_active")

	// Updating a plan of another tenant affects 0 rows (no error by design).
	wrongTenant := *plan
	wrongTenant.TenantID = covTenantB
	covNoErr(t, repo.UpdatePlan(ctx, &wrongTenant), "UpdatePlan wrong tenant")

	// Invalid uuid in the WHERE clause → error.
	invalid := *plan
	invalid.ID = "not-a-uuid"
	covWantErr(t, repo.UpdatePlan(ctx, &invalid), "UpdatePlan invalid uuid")

	// DeactivatePlan.
	covNoErr(t, repo.DeactivatePlan(ctx, premium.ID, covTenantA), "DeactivatePlan")
	got, err = repo.GetPlanByID(ctx, premium.ID, covTenantA)
	covNoErr(t, err, "GetPlanByID after deactivate")
	covEqual(t, got.IsActive, false, "plan deactivated")

	// Deactivating another tenant's plan is a silent no-op.
	covNoErr(t, repo.DeactivatePlan(ctx, premium.ID, covTenantB), "DeactivatePlan wrong tenant")
	covWantErr(t, repo.DeactivatePlan(ctx, "not-a-uuid", covTenantA), "DeactivatePlan invalid uuid")

	// Duplicate (product_id, slug) → constraint error.
	dup := &domain.DigitalSubscriptionPlan{
		ID: covUUID(t), ProductID: product.ID, TenantID: product.TenantID,
		Name: "dup", Slug: inactive.Slug, PriceCents: 1000, Interval: "monthly",
		IsActive: true, CreatedAt: covNow(), UpdatedAt: covNow(),
	}
	covWantErr(t, repo.CreatePlan(ctx, dup), "CreatePlan duplicate slug")

	// Invalid interval → CHECK constraint error.
	badInterval := &domain.DigitalSubscriptionPlan{
		ID: covUUID(t), ProductID: product.ID, TenantID: product.TenantID,
		Name: "bad interval", Slug: "bad-interval", PriceCents: 1000, Interval: "weekly",
		IsActive: true, CreatedAt: covNow(), UpdatedAt: covNow(),
	}
	covWantErr(t, repo.CreatePlan(ctx, badInterval), "CreatePlan invalid interval")

	// Invalid product uuid → FK/type error.
	badProduct := &domain.DigitalSubscriptionPlan{
		ID: covUUID(t), ProductID: "not-a-uuid", TenantID: product.TenantID,
		Name: "bad product", Slug: "bad-product", PriceCents: 1000, Interval: "monthly",
		IsActive: true, CreatedAt: covNow(), UpdatedAt: covNow(),
	}
	covWantErr(t, repo.CreatePlan(ctx, badProduct), "CreatePlan invalid product")
}

// --- Subscriptions ----------------------------------------------------------

func TestCovSubscriptionCRUD(t *testing.T) {
	pool := covDB(t)
	ctx, cancel := covCtx(t)
	defer cancel()
	repo := NewSubscriptionRepository(pool)

	product := covNewProduct(t, pool, covTenantA, "sub-product")
	plan := covNewPlan(t, repo, product, "sub-plan", true)

	// Empty asaas id is persisted as empty string (no scan surprises).
	sub := covNewSubscription(t, repo, plan, "sub@example.com", "sub-token-1", "", "active", 0)

	byToken, err := repo.GetSubscriptionByToken(ctx, "sub-token-1")
	covNoErr(t, err, "GetSubscriptionByToken")
	covEqual(t, byToken.ID, sub.ID, "subscription id")
	covEqual(t, byToken.CustomerEmail, "sub@example.com", "email")
	covEqual(t, byToken.Status, "active", "status")
	covEqual(t, byToken.AsaasSubscriptionID, "", "empty asaas id")
	covEqual(t, byToken.AsaasCustomerID, "cus_123", "asaas customer id")
	if byToken.CurrentPeriodStart == nil || byToken.CurrentPeriodEnd == nil {
		t.Fatalf("current period bounds not persisted")
	}

	// CPF is ciphertext, never plaintext (LGPD Art. 46).
	var storedCPF *string
	covNoErr(t, pool.QueryRow(ctx, `SELECT customer_cpf FROM digital_subscriptions WHERE id = $1`, sub.ID).Scan(&storedCPF), "read cpf")
	if storedCPF == nil || *storedCPF == "12345678901" {
		t.Fatalf("subscription CPF not encrypted: %v", storedCPF)
	}

	_, err = repo.GetSubscriptionByToken(ctx, "missing-token")
	covWantErr(t, err, "GetSubscriptionByToken missing")

	// asaas id can be attached after creation and looked up.
	covNoErr(t, repo.UpdateSubscriptionAsaasID(ctx, sub.ID, "sub_asaas_1"), "UpdateSubscriptionAsaasID")
	byAsaas, err := repo.GetSubscriptionByAsaasID(ctx, "sub_asaas_1")
	covNoErr(t, err, "GetSubscriptionByAsaasID")
	covEqual(t, byAsaas.ID, sub.ID, "subscription id by asaas")

	_, err = repo.GetSubscriptionByAsaasID(ctx, "sub_asaas_missing")
	covWantErr(t, err, "GetSubscriptionByAsaasID missing")

	covNoErr(t, repo.UpdateSubscriptionAsaasID(ctx, "00000000-0000-0000-0000-000000000000", "sub_x"), "UpdateSubscriptionAsaasID unknown")
	covWantErr(t, repo.UpdateSubscriptionAsaasID(ctx, "not-a-uuid", "sub_x"), "UpdateSubscriptionAsaasID invalid uuid")

	// Status update with an explicit period end.
	newEnd := covNow().AddDate(0, 1, 0).Truncate(time.Second)
	covNoErr(t, repo.UpdateSubscriptionStatus(ctx, sub.ID, "past_due", &newEnd), "UpdateSubscriptionStatus with period end")
	got, err := repo.GetSubscriptionByToken(ctx, "sub-token-1")
	covNoErr(t, err, "GetSubscriptionByToken after status")
	covEqual(t, got.Status, "past_due", "status past_due")
	if got.CurrentPeriodEnd == nil || !got.CurrentPeriodEnd.Equal(newEnd) {
		t.Fatalf("current_period_end = %v, want %v", got.CurrentPeriodEnd, newEnd)
	}

	// Status update with a nil period end clears the column.
	covNoErr(t, repo.UpdateSubscriptionStatus(ctx, sub.ID, "active", nil), "UpdateSubscriptionStatus nil period end")
	got, err = repo.GetSubscriptionByToken(ctx, "sub-token-1")
	covNoErr(t, err, "GetSubscriptionByToken after nil period")
	covEqual(t, got.Status, "active", "status active")
	if got.CurrentPeriodEnd != nil {
		t.Fatalf("current_period_end should be NULL, got %v", got.CurrentPeriodEnd)
	}

	covNoErr(t, repo.UpdateSubscriptionStatus(ctx, "00000000-0000-0000-0000-000000000000", "active", nil), "UpdateSubscriptionStatus unknown")
	covWantErr(t, repo.UpdateSubscriptionStatus(ctx, "not-a-uuid", "active", nil), "UpdateSubscriptionStatus invalid uuid")
	covWantErr(t, repo.UpdateSubscriptionStatus(ctx, sub.ID, "not-a-status", nil), "UpdateSubscriptionStatus invalid status value")

	// Second subscription, for listing and cancel flow.
	second := covNewSubscription(t, repo, plan, "second@example.com", "sub-token-2", "sub_asaas_2", "active", -time.Hour)

	subs, err := repo.ListSubscriptionsByTenant(ctx, covTenantA)
	covNoErr(t, err, "ListSubscriptionsByTenant")
	covEqual(t, len(subs), 2, "subscriptions by tenant")
	covEqual(t, subs[0].ID, second.ID, "ordered by created_at DESC")

	empty, err := repo.ListSubscriptionsByTenant(ctx, covTenantC)
	covNoErr(t, err, "ListSubscriptionsByTenant empty")
	if empty == nil {
		t.Fatalf("empty subscription list must be an empty slice")
	}
	covEqual(t, len(empty), 0, "empty subscription list")

	// An invalid uuid makes the query fail, but ListSubscriptionsByTenant does not
	// check rows.Err(), so the failure surfaces as an empty list (production quirk).
	_, err = repo.ListSubscriptionsByTenant(ctx, "not-a-uuid")
	covNoErr(t, err, "ListSubscriptionsByTenant invalid uuid")

	// CancelSubscription sets status and cancelled_at.
	covNoErr(t, repo.CancelSubscription(ctx, second.ID), "CancelSubscription")
	cancelled, err := repo.GetSubscriptionByToken(ctx, "sub-token-2")
	covNoErr(t, err, "GetSubscriptionByToken after cancel")
	covEqual(t, cancelled.Status, "cancelled", "cancelled status")
	if cancelled.CancelledAt == nil {
		t.Fatalf("cancelled_at not set")
	}

	covNoErr(t, repo.CancelSubscription(ctx, "00000000-0000-0000-0000-000000000000"), "CancelSubscription unknown")
	covWantErr(t, repo.CancelSubscription(ctx, "not-a-uuid"), "CancelSubscription invalid uuid")

	// Duplicate access token → unique violation.
	dup := *sub
	dup.ID = covUUID(t)
	dup.AccessToken = "sub-token-1"
	covWantErr(t, repo.CreateSubscription(ctx, &dup), "CreateSubscription duplicate token")

	// Invalid plan FK → error.
	badPlan := *sub
	badPlan.ID = covUUID(t)
	badPlan.AccessToken = "sub-token-3"
	badPlan.PlanID = "00000000-0000-0000-0000-000000000000"
	covWantErr(t, repo.CreateSubscription(ctx, &badPlan), "CreateSubscription invalid plan")
}

// --- LGPD: subscriber export / erasure --------------------------------------

func TestCovListSubscriptionsByEmail(t *testing.T) {
	pool := covDB(t)
	ctx, cancel := covCtx(t)
	defer cancel()
	repo := NewSubscriptionRepository(pool)

	product := covNewProduct(t, pool, covTenantA, "sub-export-product")
	plan := covNewPlan(t, repo, product, "sub-export-plan", true)
	covNewSubscription(t, repo, plan, "Holder@Example.com", "export-token-1", "asaas_export_1", "active", 0)

	exports, err := repo.ListSubscriptionsByEmail(ctx, "holder@example.com")
	covNoErr(t, err, "ListSubscriptionsByEmail")
	covEqual(t, len(exports), 1, "subscriptions exported")
	covEqual(t, exports[0].ProductID, product.ID, "product id")
	covEqual(t, exports[0].ProductName, product.Name, "product name")
	covEqual(t, exports[0].PlanName, plan.Name, "plan name")
	covEqual(t, exports[0].Status, "active", "status")

	empty, err := repo.ListSubscriptionsByEmail(ctx, "nobody@example.com")
	covNoErr(t, err, "ListSubscriptionsByEmail empty")
	if empty == nil {
		t.Fatalf("empty export must be an empty slice")
	}
	covEqual(t, len(empty), 0, "empty export")
}

func TestCovEraseSubscriptions(t *testing.T) {
	pool := covDB(t)
	ctx, cancel := covCtx(t)
	defer cancel()
	repo := NewSubscriptionRepository(pool)

	product := covNewProduct(t, pool, covTenantA, "sub-erase-product")
	plan := covNewPlan(t, repo, product, "sub-erase-plan", true)
	old := covNewSubscription(t, repo, plan, "erase-sub@example.com", "erase-token-1", "asaas_erase_1", "active", 30*24*time.Hour)
	kept := covNewSubscription(t, repo, plan, "keep-sub@example.com", "erase-token-2", "asaas_keep_1", "active", 0)

	n, err := repo.EraseSubscriptionsByEmail(ctx, "ERASE-SUB@example.com")
	covNoErr(t, err, "EraseSubscriptionsByEmail")
	covEqual(t, n, int64(1), "erased subscriptions")

	var email, name, cpf *string
	covNoErr(t, pool.QueryRow(ctx,
		`SELECT customer_email, customer_name, customer_cpf FROM digital_subscriptions WHERE id = $1`,
		old.ID).Scan(&email, &name, &cpf), "read anonymized subscription")
	if email != nil || name != nil || cpf != nil {
		t.Fatalf("subscription PII not anonymized: %v %v %v", email, name, cpf)
	}

	n, err = repo.EraseSubscriptionsByEmail(ctx, "erase-sub@example.com")
	covNoErr(t, err, "EraseSubscriptionsByEmail second run")
	covEqual(t, n, int64(0), "erased subscriptions second run")

	// Retention sweep only touches rows older than the cutoff.
	n, err = repo.EraseSubscriptionsOlderThan(ctx, covNow().Add(-5*24*time.Hour))
	covNoErr(t, err, "EraseSubscriptionsOlderThan")
	covEqual(t, n, int64(0), "erased older subscriptions (none old enough)")

	old2 := covNewSubscription(t, repo, plan, "old-sub2@example.com", "erase-token-3", "asaas_erase_2", "active", 20*24*time.Hour)
	n, err = repo.EraseSubscriptionsOlderThan(ctx, covNow().Add(-5*24*time.Hour))
	covNoErr(t, err, "EraseSubscriptionsOlderThan with old row")
	covEqual(t, n, int64(1), "erased old subscriptions")

	var oldEmail, keptEmail *string
	covNoErr(t, pool.QueryRow(ctx, `SELECT customer_email FROM digital_subscriptions WHERE id = $1`, old2.ID).Scan(&oldEmail), "old subscription email")
	covNoErr(t, pool.QueryRow(ctx, `SELECT customer_email FROM digital_subscriptions WHERE id = $1`, kept.ID).Scan(&keptEmail), "kept subscription email")
	if oldEmail != nil {
		t.Fatalf("old subscription PII not erased")
	}
	if keptEmail == nil || *keptEmail != "keep-sub@example.com" {
		t.Fatalf("recent subscription PII should be preserved, got %v", keptEmail)
	}

	// Invalid uuid is irrelevant here (no uuid predicate), but confirm the query
	// tolerates an empty set for unknown emails.
	n, err = repo.EraseSubscriptionsOlderThan(ctx, covNow().Add(-1000*24*time.Hour))
	covNoErr(t, err, "EraseSubscriptionsOlderThan far cutoff")
	covEqual(t, n, int64(0), "no subscriptions that old")
}
