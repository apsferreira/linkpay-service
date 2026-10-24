package repository

// Error-propagation tests: every repository method must return an error (never
// panic, never silently succeed) when the database is unavailable. A pool is
// opened and immediately closed to simulate the failure without touching the
// shared container.

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"linkpay-service/internal/domain"
)

func covClosedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(covDSN)
	covNoErr(t, err, "parse dsn")
	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	covNoErr(t, err, "open throwaway pool")
	pool.Close()
	return pool
}

func TestCovLinkPayErrorPathsWithClosedPool(t *testing.T) {
	covDB(t)
	ctx, cancel := covCtx(t)
	defer cancel()

	repo := NewLinkPayRepository(covClosedPool(t))
	now := covNow()
	product := &domain.DigitalProduct{
		ID: covUUID(t), TenantID: covTenantA, Slug: "closed", Name: "closed",
		Description: "d", PriceCents: 1000, Currency: "BRL", ProductType: "digital",
		DownloadLimit: 1, IsActive: true, CreatedAt: now, UpdatedAt: now,
	}

	covWantErr(t, repo.CreateProduct(ctx, product), "CreateProduct closed pool")
	_, err := repo.GetProductBySlug(ctx, "closed")
	covWantErr(t, err, "GetProductBySlug closed pool")
	_, err = repo.GetProductByID(ctx, product.ID, covTenantA)
	covWantErr(t, err, "GetProductByID closed pool")
	_, err = repo.ListProducts(ctx, covTenantA)
	covWantErr(t, err, "ListProducts closed pool")
	covWantErr(t, repo.UpdateProduct(ctx, product), "UpdateProduct closed pool")
	covWantErr(t, repo.DeactivateProduct(ctx, product.ID, covTenantA), "DeactivateProduct closed pool")
	covWantErr(t, repo.UpdateProductStorage(ctx, product.ID, covTenantA, "k", "b", "f", 1, "m"), "UpdateProductStorage closed pool")
	covWantErr(t, repo.UpdateProductCover(ctx, product.ID, covTenantA, "u"), "UpdateProductCover closed pool")

	purchase := &domain.DigitalPurchase{
		ID: covUUID(t), ProductID: product.ID, TenantID: covTenantA,
		CustomerEmail: "x@example.com", CustomerName: "X", AmountCents: 1000,
		PaymentMethod: "pix", PaymentStatus: "pending", AccessToken: "closed-token",
		CreatedAt: now,
	}
	covWantErr(t, repo.CreatePurchase(ctx, purchase), "CreatePurchase closed pool")
	_, err = repo.GetPurchaseByToken(ctx, "closed-token")
	covWantErr(t, err, "GetPurchaseByToken closed pool")
	_, err = repo.GetPurchaseByID(ctx, purchase.ID)
	covWantErr(t, err, "GetPurchaseByID closed pool")
	_, err = repo.GetPurchaseByChargeID(ctx, "chg")
	covWantErr(t, err, "GetPurchaseByChargeID closed pool")
	covWantErr(t, repo.UpdatePurchaseStatus(ctx, purchase.ID, "confirmed", "chg"), "UpdatePurchaseStatus closed pool")
	covWantErr(t, repo.ConfirmPurchase(ctx, purchase.ID, "tok", now), "ConfirmPurchase closed pool")
	covWantErr(t, repo.IncrementDownload(ctx, "tok"), "IncrementDownload closed pool")

	_, err = repo.ListPurchasesByEmail(ctx, "x@example.com")
	covWantErr(t, err, "ListPurchasesByEmail closed pool")
	_, err = repo.ErasePurchasesByEmail(ctx, "x@example.com")
	covWantErr(t, err, "ErasePurchasesByEmail closed pool")
	_, err = repo.ErasePurchasesOlderThan(ctx, now)
	covWantErr(t, err, "ErasePurchasesOlderThan closed pool")

	_, err = repo.GetSalesStats(ctx, covTenantA, 30)
	covWantErr(t, err, "GetSalesStats closed pool")

	file := &domain.DigitalProductFile{ProductID: product.ID, StorageKey: "k", FileName: "f"}
	covWantErr(t, repo.AddProductFile(ctx, file), "AddProductFile closed pool")
	_, err = repo.ListProductFiles(ctx, product.ID)
	covWantErr(t, err, "ListProductFiles closed pool")
	_, err = repo.GetProductFile(ctx, covUUID(t))
	covWantErr(t, err, "GetProductFile closed pool")
	covWantErr(t, repo.DeleteProductFile(ctx, covUUID(t), product.ID), "DeleteProductFile closed pool")

	_, err = repo.GetUpsellsByProductID(ctx, product.ID)
	covWantErr(t, err, "GetUpsellsByProductID closed pool")

	entry := &domain.CommissionEntry{
		ID: covUUID(t), PurchaseID: purchase.ID, TenantID: covTenantA, ProductID: product.ID,
		GrossAmountCents: 1000, CommissionPercent: 30, CommissionCents: 300,
		CreatorPayoutCents: 700, CreatedAt: now,
	}
	covWantErr(t, repo.InsertCommissionEntry(ctx, entry), "InsertCommissionEntry closed pool")
	_, err = repo.GetCommissionSummary(ctx, "2026-01-01", "2026-01-31")
	covWantErr(t, err, "GetCommissionSummary closed pool")
}

func TestCovPayoutErrorPathsWithClosedPool(t *testing.T) {
	covDB(t)
	ctx, cancel := covCtx(t)
	defer cancel()

	repo := NewPayoutRepository(covClosedPool(t))
	now := covNow()

	_, err := repo.GetEligiblePurchases(ctx, covTenantA)
	covWantErr(t, err, "GetEligiblePurchases closed pool")
	_, err = repo.GetBalance(ctx, covTenantA)
	covWantErr(t, err, "GetBalance closed pool")
	_, err = repo.GetTenantsWithEligiblePayouts(ctx)
	covWantErr(t, err, "GetTenantsWithEligiblePayouts closed pool")
	_, err = repo.GetTenantPixKey(ctx, covTenantA)
	covWantErr(t, err, "GetTenantPixKey closed pool")

	payout := covPayoutFixture(covUUID(t), covTenantA, 1000, now, now)
	covWantErr(t, repo.CreatePayout(ctx, payout, nil), "CreatePayout closed pool")
	_, err = repo.GetAnticipationEligiblePurchases(ctx, covTenantA)
	covWantErr(t, err, "GetAnticipationEligiblePurchases closed pool")
	covWantErr(t, repo.CreateAnticipationPayout(ctx, payout, nil), "CreateAnticipationPayout closed pool")
	covWantErr(t, repo.UpdatePayoutTransferID(ctx, payout.ID, "tr", "pending"), "UpdatePayoutTransferID closed pool")
	covWantErr(t, repo.UpdatePayoutStatus(ctx, "tr", "completed", ""), "UpdatePayoutStatus closed pool")
	_, _, err = repo.ListPayouts(ctx, covTenantA, 10, 0)
	covWantErr(t, err, "ListPayouts closed pool")
}

func TestCovSubscriptionErrorPathsWithClosedPool(t *testing.T) {
	covDB(t)
	ctx, cancel := covCtx(t)
	defer cancel()

	repo := NewSubscriptionRepository(covClosedPool(t))
	now := covNow()

	plan := &domain.DigitalSubscriptionPlan{
		ID: covUUID(t), ProductID: covUUID(t), TenantID: covTenantA, Name: "p",
		Slug: "p", PriceCents: 1000, Interval: "monthly", IsActive: true,
		CreatedAt: now, UpdatedAt: now,
	}
	covWantErr(t, repo.CreatePlan(ctx, plan), "CreatePlan closed pool")
	_, err := repo.GetPlanByID(ctx, plan.ID, covTenantA)
	covWantErr(t, err, "GetPlanByID closed pool")
	_, err = repo.ListPlansByProduct(ctx, plan.ProductID)
	covWantErr(t, err, "ListPlansByProduct closed pool")
	covWantErr(t, repo.UpdatePlan(ctx, plan), "UpdatePlan closed pool")
	covWantErr(t, repo.DeactivatePlan(ctx, plan.ID, covTenantA), "DeactivatePlan closed pool")

	sub := &domain.DigitalSubscription{
		ID: covUUID(t), PlanID: plan.ID, TenantID: covTenantA,
		CustomerEmail: "x@example.com", CustomerName: "X", Status: "active",
		AccessToken: "closed-sub-token", CreatedAt: now, UpdatedAt: now,
	}
	covWantErr(t, repo.CreateSubscription(ctx, sub), "CreateSubscription closed pool")
	_, err = repo.GetSubscriptionByToken(ctx, "closed-sub-token")
	covWantErr(t, err, "GetSubscriptionByToken closed pool")
	_, err = repo.GetSubscriptionByAsaasID(ctx, "asaas")
	covWantErr(t, err, "GetSubscriptionByAsaasID closed pool")
	covWantErr(t, repo.UpdateSubscriptionStatus(ctx, sub.ID, "active", nil), "UpdateSubscriptionStatus closed pool")
	covWantErr(t, repo.CancelSubscription(ctx, sub.ID), "CancelSubscription closed pool")
	_, err = repo.ListSubscriptionsByTenant(ctx, covTenantA)
	covWantErr(t, err, "ListSubscriptionsByTenant closed pool")
	covWantErr(t, repo.UpdateSubscriptionAsaasID(ctx, sub.ID, "asaas"), "UpdateSubscriptionAsaasID closed pool")

	_, err = repo.ListSubscriptionsByEmail(ctx, "x@example.com")
	covWantErr(t, err, "ListSubscriptionsByEmail closed pool")
	_, err = repo.EraseSubscriptionsByEmail(ctx, "x@example.com")
	covWantErr(t, err, "EraseSubscriptionsByEmail closed pool")
	_, err = repo.EraseSubscriptionsOlderThan(ctx, now)
	covWantErr(t, err, "EraseSubscriptionsOlderThan closed pool")
}

// TestCovEncryptCPFWithoutKey covers the fail-safe branch: with an invalid
// ENCRYPTION_KEY the CPF is dropped (NULL) instead of being stored in plaintext.
func TestCovEncryptCPFWithoutKey(t *testing.T) {
	pool := covDB(t)
	ctx, cancel := covCtx(t)
	defer cancel()
	t.Setenv("ENCRYPTION_KEY", "invalid-key")

	product := covNewProduct(t, pool, covTenantA, "no-key-product")
	purchase := covNewPurchase(t, pool, product, "nokey@example.com", "pix", "confirmed", 0)

	var storedCPF *string
	covNoErr(t, pool.QueryRow(ctx, `SELECT customer_cpf FROM digital_purchases WHERE id = $1`, purchase.ID).Scan(&storedCPF), "read cpf")
	if storedCPF != nil {
		t.Fatalf("CPF must not be persisted without a valid key, got %q", *storedCPF)
	}
}
