package repository

import (
	"encoding/json"
	"testing"
	"time"

	"linkpay-service/internal/domain"
)

// --- GetEligiblePurchases ---------------------------------------------------

func TestCovGetEligiblePurchases(t *testing.T) {
	pool := covDB(t)
	ctx, cancel := covCtx(t)
	defer cancel()
	repo := NewPayoutRepository(pool)

	product := covNewProduct(t, pool, covTenantA, "payout-eligible")

	eligiblePix := covNewPurchase(t, pool, product, "pix3@example.com", "pix", "confirmed", 3*24*time.Hour)
	eligibleBoleto := covNewPurchase(t, pool, product, "boleto3@example.com", "boleto", "confirmed", 3*24*time.Hour)
	eligibleCard := covNewPurchase(t, pool, product, "card20@example.com", "credit_card", "confirmed", 20*24*time.Hour)

	// Not eligible: holdback still running for card, pix within 2 days.
	covNewPurchase(t, pool, product, "card3@example.com", "credit_card", "confirmed", 3*24*time.Hour)
	covNewPurchase(t, pool, product, "pix1@example.com", "pix", "confirmed", 24*time.Hour)
	// Not eligible: pending payment.
	covNewPurchase(t, pool, product, "pixpending@example.com", "pix", "pending", 3*24*time.Hour)

	// Not eligible: already attached to a payout.
	linked := covNewPurchase(t, pool, product, "pixlinked@example.com", "pix", "confirmed", 3*24*time.Hour)
	linkedPayout := covInsertPayoutRow(t, pool, covTenantA, covNow().Add(-48*time.Hour))
	covSetPurchasePayout(t, pool, linked.ID, linkedPayout)

	purchases, err := repo.GetEligiblePurchases(ctx, covTenantA)
	covNoErr(t, err, "GetEligiblePurchases")
	covEqual(t, len(purchases), 3, "eligible purchases")

	byID := map[string]int{}
	for _, p := range purchases {
		byID[p.ID] = p.CreatorEarnings
	}
	if _, ok := byID[eligiblePix.ID]; !ok {
		t.Fatalf("pix purchase missing from eligible list")
	}
	if _, ok := byID[eligibleBoleto.ID]; !ok {
		t.Fatalf("boleto purchase missing from eligible list")
	}
	if _, ok := byID[eligibleCard.ID]; !ok {
		t.Fatalf("credit_card purchase missing from eligible list")
	}
	covEqual(t, byID[eligiblePix.ID], eligiblePix.CreatorEarnings, "creator earnings scanned")

	empty, err := repo.GetEligiblePurchases(ctx, covTenantC)
	covNoErr(t, err, "GetEligiblePurchases empty")
	covEqual(t, len(empty), 0, "eligible purchases for empty tenant")

	_, err = repo.GetEligiblePurchases(ctx, "not-a-uuid")
	covWantErr(t, err, "GetEligiblePurchases invalid uuid")
}

// TestCovGetBalance covers total / held / available / reserve math.
func TestCovGetBalance(t *testing.T) {
	pool := covDB(t)
	ctx, cancel := covCtx(t)
	defer cancel()
	repo := NewPayoutRepository(pool)

	// Empty tenant: all zeroes, next payout date still present.
	empty, err := repo.GetBalance(ctx, covTenantA)
	covNoErr(t, err, "GetBalance empty")
	covEqual(t, empty.TotalCents, 0, "empty total")
	covEqual(t, empty.HeldCents, 0, "empty held")
	covEqual(t, empty.AvailableCents, 0, "empty available")
	covEqual(t, empty.ReserveCents, 0, "empty reserve")
	if empty.NextPayoutDate == "" {
		t.Fatalf("NextPayoutDate must be set")
	}
	next, err := time.Parse("2006-01-02", empty.NextPayoutDate)
	covNoErr(t, err, "parse NextPayoutDate")
	covEqual(t, next.Weekday(), time.Monday, "next payout is a Monday")

	product := covNewProduct(t, pool, covTenantA, "balance-product")
	// available gross: card 20d + pix 5d
	covNewPurchase(t, pool, product, "avail1@example.com", "credit_card", "confirmed", 20*24*time.Hour)
	covNewPurchase(t, pool, product, "avail2@example.com", "pix", "confirmed", 5*24*time.Hour)
	// held: pix 1d
	covNewPurchase(t, pool, product, "held@example.com", "pix", "confirmed", 24*time.Hour)
	// counts toward total only: already paid out
	paid := covNewPurchase(t, pool, product, "paid@example.com", "credit_card", "confirmed", 20*24*time.Hour)
	paidPayout := covInsertPayoutRow(t, pool, covTenantA, covNow().Add(-24*time.Hour))
	covSetPurchasePayout(t, pool, paid.ID, paidPayout)
	// not confirmed: ignored everywhere
	covNewPurchase(t, pool, product, "pending@example.com", "pix", "pending", 20*24*time.Hour)

	balance, err := repo.GetBalance(ctx, covTenantA)
	covNoErr(t, err, "GetBalance")
	const earnings = 7000 // 10000 - 30%
	covEqual(t, balance.TotalCents, 4*earnings, "total")
	covEqual(t, balance.HeldCents, earnings, "held")
	covEqual(t, balance.ReserveCents, 2*earnings/10, "reserve (10%)")
	covEqual(t, balance.AvailableCents, 2*earnings-2*earnings/10, "available (90%)")

	// Other tenant unaffected.
	other, err := repo.GetBalance(ctx, covTenantB)
	covNoErr(t, err, "GetBalance other tenant")
	covEqual(t, other.TotalCents, 0, "other tenant total")

	_, err = repo.GetBalance(ctx, "not-a-uuid")
	covWantErr(t, err, "GetBalance invalid uuid")
}

// --- Tenants ----------------------------------------------------------------

func TestCovGetTenantsWithEligiblePayouts(t *testing.T) {
	pool := covDB(t)
	ctx, cancel := covCtx(t)
	defer cancel()
	repo := NewPayoutRepository(pool)

	productA := covNewProduct(t, pool, covTenantA, "tenants-a")
	productB := covNewProduct(t, pool, covTenantB, "tenants-b")
	productC := covNewProduct(t, pool, covTenantC, "tenants-c")

	covNewPurchase(t, pool, productA, "a1@example.com", "pix", "confirmed", 5*24*time.Hour)
	covNewPurchase(t, pool, productA, "a2@example.com", "pix", "confirmed", 6*24*time.Hour)
	covNewPurchase(t, pool, productB, "b1@example.com", "boleto", "confirmed", 5*24*time.Hour)
	// tenant C only has a held purchase (not eligible).
	covNewPurchase(t, pool, productC, "c1@example.com", "credit_card", "confirmed", time.Hour)

	tenants, err := repo.GetTenantsWithEligiblePayouts(ctx)
	covNoErr(t, err, "GetTenantsWithEligiblePayouts")
	covEqual(t, len(tenants), 2, "distinct tenants")
	seen := map[string]bool{}
	for _, id := range tenants {
		seen[id] = true
	}
	covEqual(t, seen[covTenantA], true, "tenant A present")
	covEqual(t, seen[covTenantB], true, "tenant B present")
	covEqual(t, seen[covTenantC], false, "tenant C absent")

	// Empty database.
	covResetDB(t)
	none, err := repo.GetTenantsWithEligiblePayouts(ctx)
	covNoErr(t, err, "GetTenantsWithEligiblePayouts empty")
	covEqual(t, len(none), 0, "no tenants with eligible payouts")
}

func TestCovGetTenantPixKey(t *testing.T) {
	pool := covDB(t)
	ctx, cancel := covCtx(t)
	defer cancel()
	repo := NewPayoutRepository(pool)

	covInsertTenant(t, pool, covTenantA, "creator@example.com")

	key, err := repo.GetTenantPixKey(ctx, covTenantA)
	covNoErr(t, err, "GetTenantPixKey")
	covEqual(t, key, "creator@example.com", "pix key")

	// Tenant exists without a pix key → empty string, no error.
	covInsertTenant(t, pool, covTenantB, "")
	key, err = repo.GetTenantPixKey(ctx, covTenantB)
	covNoErr(t, err, "GetTenantPixKey null")
	covEqual(t, key, "", "empty pix key")

	// Unknown tenant → empty string, no error (pgx.ErrNoRows branch).
	key, err = repo.GetTenantPixKey(ctx, covTenantC)
	covNoErr(t, err, "GetTenantPixKey unknown tenant")
	covEqual(t, key, "", "unknown tenant pix key")

	_, err = repo.GetTenantPixKey(ctx, "not-a-uuid")
	covWantErr(t, err, "GetTenantPixKey invalid uuid")
}

// --- CreatePayout -----------------------------------------------------------

func TestCovCreatePayout(t *testing.T) {
	pool := covDB(t)
	ctx, cancel := covCtx(t)
	defer cancel()
	repo := NewPayoutRepository(pool)

	product := covNewProduct(t, pool, covTenantA, "create-payout")
	p1 := covNewPurchase(t, pool, product, "cp1@example.com", "pix", "confirmed", 5*24*time.Hour)
	p2 := covNewPurchase(t, pool, product, "cp2@example.com", "pix", "confirmed", 6*24*time.Hour)

	now := covNow()
	scheduled := now.Add(24 * time.Hour)
	payout := covPayoutFixture(covUUID(t), covTenantA, 14000, scheduled, now)

	covNoErr(t, repo.CreatePayout(ctx, payout, []string{p1.ID, p2.ID}), "CreatePayout")

	// Purchases are linked atomically.
	for _, id := range []string{p1.ID, p2.ID} {
		var payoutID *string
		covNoErr(t, pool.QueryRow(ctx, `SELECT payout_id FROM digital_purchases WHERE id = $1`, id).Scan(&payoutID), "read payout_id")
		if payoutID == nil || *payoutID != payout.ID {
			t.Fatalf("purchase %s not linked to payout (got %v)", id, payoutID)
		}
	}

	// And the payout row carries the purchase ids as JSON.
	list, total, err := repo.ListPayouts(ctx, covTenantA, 10, 0)
	covNoErr(t, err, "ListPayouts")
	covEqual(t, total, 1, "payout total")
	covEqual(t, len(list), 1, "payout rows")
	covEqual(t, list[0].ID, payout.ID, "payout id")
	var ids []string
	covNoErr(t, json.Unmarshal(list[0].PurchaseIDs, &ids), "unmarshal purchase ids")
	covEqual(t, len(ids), 2, "purchase ids count")

	// Duplicate payout id → insert fails and the transaction rolls back.
	dup := covPayoutFixture(payout.ID, covTenantA, 1000, scheduled, now)
	p3 := covNewPurchase(t, pool, product, "cp3@example.com", "pix", "confirmed", 7*24*time.Hour)
	covWantErr(t, repo.CreatePayout(ctx, dup, []string{p3.ID}), "CreatePayout duplicate id")
	var unlinked *string
	covNoErr(t, pool.QueryRow(ctx, `SELECT payout_id FROM digital_purchases WHERE id = $1`, p3.ID).Scan(&unlinked), "read payout_id after rollback")
	if unlinked != nil {
		t.Fatalf("purchase should not be linked after rollback, got %v", *unlinked)
	}

	// Invalid purchase uuid → the linking UPDATE fails, whole tx rolls back.
	badLinks := covPayoutFixture(covUUID(t), covTenantA, 1000, scheduled, now)
	covWantErr(t, repo.CreatePayout(ctx, badLinks, []string{"not-a-uuid"}), "CreatePayout invalid purchase id")

	// Invalid tenant uuid → insert fails.
	badTenant := covPayoutFixture(covUUID(t), "not-a-uuid", 1000, scheduled, now)
	covWantErr(t, repo.CreatePayout(ctx, badTenant, nil), "CreatePayout invalid tenant")
}

// --- Anticipation -----------------------------------------------------------

func TestCovAnticipationFlow(t *testing.T) {
	pool := covDB(t)
	ctx, cancel := covCtx(t)
	defer cancel()
	repo := NewPayoutRepository(pool)

	product := covNewProduct(t, pool, covTenantA, "anticipation")
	eligible := covNewPurchase(t, pool, product, "ant3@example.com", "credit_card", "confirmed", 3*24*time.Hour)
	// Too old (already available, not anticipatable) and too recent (still held).
	covNewPurchase(t, pool, product, "ant20@example.com", "credit_card", "confirmed", 20*24*time.Hour)
	covNewPurchase(t, pool, product, "ant1@example.com", "credit_card", "confirmed", 24*time.Hour)
	// pix is never eligible for card anticipation.
	covNewPurchase(t, pool, product, "antpix@example.com", "pix", "confirmed", 3*24*time.Hour)

	list, err := repo.GetAnticipationEligiblePurchases(ctx, covTenantA)
	covNoErr(t, err, "GetAnticipationEligiblePurchases")
	covEqual(t, len(list), 1, "anticipation eligible purchases")
	covEqual(t, list[0].ID, eligible.ID, "eligible purchase id")
	covEqual(t, list[0].CreatorEarnings, eligible.CreatorEarnings, "creator earnings")

	// Mark inside a transaction (mirrors the service flow).
	tx, err := pool.Begin(ctx)
	covNoErr(t, err, "begin tx")
	covNoErr(t, repo.MarkAnticipationRequested(ctx, tx, []string{eligible.ID}, covTenantA), "MarkAnticipationRequested")
	covNoErr(t, tx.Commit(ctx), "commit tx")

	var requestedAt *time.Time
	covNoErr(t, pool.QueryRow(ctx, `SELECT anticipation_requested_at FROM digital_purchases WHERE id = $1`, eligible.ID).Scan(&requestedAt), "read anticipation_requested_at")
	if requestedAt == nil {
		t.Fatalf("anticipation_requested_at not set")
	}

	list, err = repo.GetAnticipationEligiblePurchases(ctx, covTenantA)
	covNoErr(t, err, "GetAnticipationEligiblePurchases after mark")
	covEqual(t, len(list), 0, "eligible purchases after mark")

	// Mark with one invalid uuid → error (tx aborted).
	tx2, err := pool.Begin(ctx)
	covNoErr(t, err, "begin tx2")
	covWantErr(t, repo.MarkAnticipationRequested(ctx, tx2, []string{"not-a-uuid"}, covTenantA), "MarkAnticipationRequested invalid uuid")
	_ = tx2.Rollback(ctx)

	// Empty tenant → empty list.
	empty, err := repo.GetAnticipationEligiblePurchases(ctx, covTenantC)
	covNoErr(t, err, "GetAnticipationEligiblePurchases empty")
	covEqual(t, len(empty), 0, "empty anticipation list")

	_, err = repo.GetAnticipationEligiblePurchases(ctx, "not-a-uuid")
	covWantErr(t, err, "GetAnticipationEligiblePurchases invalid uuid")
}

func TestCovCreateAnticipationPayout(t *testing.T) {
	pool := covDB(t)
	ctx, cancel := covCtx(t)
	defer cancel()
	repo := NewPayoutRepository(pool)

	product := covNewProduct(t, pool, covTenantA, "anticipation-payout")
	p := covNewPurchase(t, pool, product, "antpay@example.com", "credit_card", "confirmed", 3*24*time.Hour)

	now := covNow()
	payout := covPayoutFixture(covUUID(t), covTenantA, 6860, now.Add(2*time.Hour), now)
	payout.IsAnticipation = true
	payout.AnticipationFeeCents = 140

	covNoErr(t, repo.CreateAnticipationPayout(ctx, payout, []string{p.ID}), "CreateAnticipationPayout")

	var anticipationRequested *time.Time
	covNoErr(t, pool.QueryRow(ctx, `SELECT anticipation_requested_at FROM digital_purchases WHERE id = $1`, p.ID).Scan(&anticipationRequested), "read anticipation_requested_at")
	if anticipationRequested == nil {
		t.Fatalf("anticipation_requested_at not set by CreateAnticipationPayout")
	}

	list, total, err := repo.ListPayouts(ctx, covTenantA, 10, 0)
	covNoErr(t, err, "ListPayouts")
	covEqual(t, total, 1, "payout total")
	covEqual(t, list[0].IsAnticipation, true, "is_anticipation")
	covEqual(t, list[0].AnticipationFeeCents, 140, "anticipation fee")

	// Duplicate id → rollback, anticipation timestamp of the second purchase untouched.
	dup := covPayoutFixture(payout.ID, covTenantA, 1000, now, now)
	p2 := covNewPurchase(t, pool, product, "antpay2@example.com", "credit_card", "confirmed", 3*24*time.Hour)
	covWantErr(t, repo.CreateAnticipationPayout(ctx, dup, []string{p2.ID}), "CreateAnticipationPayout duplicate id")
	var untouched *time.Time
	covNoErr(t, pool.QueryRow(ctx, `SELECT anticipation_requested_at FROM digital_purchases WHERE id = $1`, p2.ID).Scan(&untouched), "read second anticipation")
	if untouched != nil {
		t.Fatalf("rollback should leave anticipation_requested_at NULL, got %v", untouched)
	}

	// Invalid purchase uuid → linking error.
	bad := covPayoutFixture(covUUID(t), covTenantA, 1000, now, now)
	covWantErr(t, repo.CreateAnticipationPayout(ctx, bad, []string{"not-a-uuid"}), "CreateAnticipationPayout invalid purchase id")

	// Invalid tenant uuid → insert error.
	badTenant := covPayoutFixture(covUUID(t), "not-a-uuid", 1000, now, now)
	covWantErr(t, repo.CreateAnticipationPayout(ctx, badTenant, nil), "CreateAnticipationPayout invalid tenant")
}

// --- Updates / listing ------------------------------------------------------

func TestCovUpdatePayoutTransferIDAndStatus(t *testing.T) {
	pool := covDB(t)
	ctx, cancel := covCtx(t)
	defer cancel()
	repo := NewPayoutRepository(pool)
	// UpdatePayoutStatus needs the simple query protocol (see covSimplePool).
	simpleRepo := NewPayoutRepository(covSimplePool(t))

	now := covNow()
	payout := covPayoutFixture(covUUID(t), covTenantA, 5000, now, now)
	covNoErr(t, repo.CreatePayout(ctx, payout, nil), "CreatePayout")

	covNoErr(t, repo.UpdatePayoutTransferID(ctx, payout.ID, "tr_123", "processing"), "UpdatePayoutTransferID")
	list, _, err := repo.ListPayouts(ctx, covTenantA, 10, 0)
	covNoErr(t, err, "ListPayouts after transfer id")
	covEqual(t, list[0].AsaasTransferID, "tr_123", "transfer id")
	covEqual(t, list[0].Status, "processing", "status")

	// Unknown payout id → no rows, no error.
	covNoErr(t, repo.UpdatePayoutTransferID(ctx, "00000000-0000-0000-0000-000000000000", "tr_x", "processing"), "UpdatePayoutTransferID unknown")
	covWantErr(t, repo.UpdatePayoutTransferID(ctx, "not-a-uuid", "tr_x", "processing"), "UpdatePayoutTransferID invalid uuid")

	// completed sets completed_at.
	covNoErr(t, simpleRepo.UpdatePayoutStatus(ctx, "tr_123", "completed", ""), "UpdatePayoutStatus completed")
	list, _, err = repo.ListPayouts(ctx, covTenantA, 10, 0)
	covNoErr(t, err, "ListPayouts after completed")
	covEqual(t, list[0].Status, "completed", "completed status")
	if list[0].CompletedAt == nil {
		t.Fatalf("completed_at not set for completed payout")
	}

	// failed clears completed_at and stores error_message.
	covNoErr(t, simpleRepo.UpdatePayoutStatus(ctx, "tr_123", "failed", "insufficient funds"), "UpdatePayoutStatus failed")
	list, _, err = repo.ListPayouts(ctx, covTenantA, 10, 0)
	covNoErr(t, err, "ListPayouts after failed")
	covEqual(t, list[0].Status, "failed", "failed status")
	covEqual(t, list[0].ErrorMessage, "insufficient funds", "error message")
	if list[0].CompletedAt != nil {
		t.Fatalf("completed_at should be cleared for non-completed status")
	}

	// Unknown transfer id / invalid status column width.
	covNoErr(t, simpleRepo.UpdatePayoutStatus(ctx, "tr_missing", "completed", ""), "UpdatePayoutStatus unknown transfer")
	covWantErr(t, simpleRepo.UpdatePayoutStatus(ctx, "", "this-status-is-way-too-long-for-the-column", ""), "UpdatePayoutStatus too long")
}

func TestCovListPayoutsPagination(t *testing.T) {
	pool := covDB(t)
	ctx, cancel := covCtx(t)
	defer cancel()
	repo := NewPayoutRepository(pool)

	now := covNow()
	covInsertPayoutRow(t, pool, covTenantA, now.Add(-3*time.Hour))
	covInsertPayoutRow(t, pool, covTenantA, now.Add(-2*time.Hour))
	third := covInsertPayoutRow(t, pool, covTenantA, now.Add(-time.Hour))
	covInsertPayoutRow(t, pool, covTenantB, now.Add(-time.Hour))

	page1, total, err := repo.ListPayouts(ctx, covTenantA, 2, 0)
	covNoErr(t, err, "ListPayouts page1")
	covEqual(t, total, 3, "total payouts")
	covEqual(t, len(page1), 2, "page1 size")
	covEqual(t, page1[0].ID, third, "ordered by created_at DESC")
	covEqual(t, page1[0].TenantID, covTenantA, "tenant id")
	covEqual(t, page1[0].PixKey, "", "empty pix key coalesced")
	covEqual(t, page1[0].AsaasTransferID, "", "empty transfer id coalesced")
	covEqual(t, page1[0].ErrorMessage, "", "empty error coalesced")

	page2, total, err := repo.ListPayouts(ctx, covTenantA, 2, 2)
	covNoErr(t, err, "ListPayouts page2")
	covEqual(t, total, 3, "total payouts page2")
	covEqual(t, len(page2), 1, "page2 size")

	empty, total, err := repo.ListPayouts(ctx, covTenantC, 10, 0)
	covNoErr(t, err, "ListPayouts empty")
	covEqual(t, total, 0, "empty total")
	if empty == nil {
		t.Fatalf("empty payouts must be an empty slice")
	}
	covEqual(t, len(empty), 0, "empty page size")

	_, _, err = repo.ListPayouts(ctx, "not-a-uuid", 10, 0)
	covWantErr(t, err, "ListPayouts invalid uuid")
}

// covPayoutFixture builds a CreatorPayout with sensible defaults.
func covPayoutFixture(id, tenantID string, amount int, scheduled, now time.Time) *domain.CreatorPayout {
	return &domain.CreatorPayout{
		ID: id, TenantID: tenantID, AmountCents: amount, PlatformFeeCents: 0,
		Status: "pending", PayoutMethod: "pix", PixKey: "creator@example.com",
		ScheduledFor: &scheduled, PurchaseIDs: json.RawMessage("[]"),
		CreatedAt: now, UpdatedAt: now,
	}
}
