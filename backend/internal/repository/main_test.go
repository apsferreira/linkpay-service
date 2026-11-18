package repository

// Integration test harness for the repository package.
//
// A single postgres:15 container is started per `go test` execution (TestMain),
// all migrations in ../../migrations are applied in lexicographic order, and each
// test starts from a clean database via covResetDB.
//
// All helper identifiers are prefixed with `cov` to avoid collisions with future
// production/test symbols.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"linkpay-service/internal/domain"
)

// covEncryptionKeyHex is a throwaway AES-256 key (32 bytes hex) used so that the
// LGPD CPF encryption path can be exercised end-to-end.
const covEncryptionKeyHex = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"

// covTenantA / covTenantB are fixed tenant ids used across the suite.
const (
	covTenantA = "11111111-1111-1111-1111-111111111111"
	covTenantB = "22222222-2222-2222-2222-222222222222"
	covTenantC = "33333333-3333-3333-3333-333333333333"
)

var (
	covDSN       string
	covPool      *pgxpool.Pool
	covContainer *tcpostgres.PostgresContainer
	covSetupErr  error
	covSetupOnce sync.Once
)

// covCovTables lists every table created by the migrations (plus the test-only
// `tenants` table) so the database can be reset between tests.
const covTablesToTruncate = `
	digital_product_files,
	commission_ledger,
	product_upsells,
	digital_subscriptions,
	digital_subscription_plans,
	creator_payouts,
	digital_purchases,
	digital_products,
	creator_configs,
	tenants
`

func TestMain(m *testing.M) {
	covSetupOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
		defer cancel()
		covContainer, covPool, covSetupErr = covStartPostgres(ctx)
	})

	if covSetupErr != nil {
		fmt.Fprintf(os.Stderr,
			"cov: postgres testcontainer unavailable — integration tests will be skipped: %v\n", covSetupErr)
	}

	// Deterministic key for the CPF encryption path (only set when not provided).
	if os.Getenv("ENCRYPTION_KEY") == "" {
		_ = os.Setenv("ENCRYPTION_KEY", covEncryptionKeyHex)
	}

	code := m.Run()

	if covPool != nil {
		covPool.Close()
	}
	if covContainer != nil {
		tearCtx, tearCancel := context.WithTimeout(context.Background(), 60*time.Second)
		_ = covContainer.Terminate(tearCtx)
		tearCancel()
	}
	os.Exit(code)
}

// covStartPostgres boots the container (default) or falls back to the Postgres
// instance pointed to by DATABASE_URL when Docker is unavailable — CI already
// provisions a postgres:15 service, so the suite stays deterministic there.
// In both cases the pool is opened and all migrations are applied.
func covStartPostgres(ctx context.Context) (*tcpostgres.PostgresContainer, *pgxpool.Pool, error) {
	ctr, pool, dsn, err := covStartContainer(ctx)
	if err != nil {
		if fallbackDSN := os.Getenv("DATABASE_URL"); fallbackDSN != "" {
			fmt.Fprintf(os.Stderr,
				"cov: postgres testcontainer unavailable (%v) — using DATABASE_URL for integration tests\n", err)

			pool, dsn, err = covOpenPool(ctx, fallbackDSN)
			if err != nil {
				return nil, nil, err
			}
			covDSN = dsn
			if err := covApplyMigrations(ctx, pool); err != nil {
				pool.Close()
				return nil, nil, err
			}
			return nil, pool, nil
		}
		return nil, nil, err
	}
	return ctr, pool, nil
}

// covStartContainer starts the throwaway postgres:15 container and opens a pool.
func covStartContainer(ctx context.Context) (*tcpostgres.PostgresContainer, *pgxpool.Pool, string, error) {
	ctr, err := tcpostgres.Run(ctx, "postgres:15",
		tcpostgres.WithDatabase("linkpay_test"),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("postgres"),
		tcpostgres.BasicWaitStrategies(),
		testcontainers.WithEnv(map[string]string{"TZ": "UTC"}),
	)
	if err != nil {
		if ctr != nil {
			_ = ctr.Terminate(context.Background())
		}
		return nil, nil, "", fmt.Errorf("start postgres container: %w", err)
	}

	dsn, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = ctr.Terminate(context.Background())
		return nil, nil, "", fmt.Errorf("connection string: %w", err)
	}

	pool, dsn, err := covOpenPool(ctx, dsn)
	if err != nil {
		_ = ctr.Terminate(context.Background())
		return nil, nil, "", err
	}

	covDSN = dsn

	if err := covApplyMigrations(ctx, pool); err != nil {
		pool.Close()
		_ = ctr.Terminate(context.Background())
		return nil, nil, "", err
	}
	return ctr, pool, dsn, nil
}

// covOpenPool opens and verifies a pool against dsn.
func covOpenPool(ctx context.Context, dsn string) (*pgxpool.Pool, string, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, "", fmt.Errorf("pgxpool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, "", fmt.Errorf("ping: %w", err)
	}
	return pool, dsn, nil
}

// covApplyMigrations runs every *.up.sql file exactly once, in lexical order.
func covApplyMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	entries, err := os.ReadDir("../../migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".up.sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return fmt.Errorf("no migrations found in ../../migrations")
	}

	for _, name := range names {
		raw, err := os.ReadFile(filepath.Join("../../migrations", name))
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		if _, err := pool.Exec(ctx, string(raw)); err != nil {
			msg := strings.ToLower(err.Error())
			// Migrations use IF NOT EXISTS in most places but a few ALTERs are not
			// idempotent; tolerate "already exists"/"duplicate" only.
			if strings.Contains(msg, "already exists") || strings.Contains(msg, "duplicate") {
				continue
			}
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
	}

	// The production GetTenantPixKey reads a `tenants` table owned by the
	// checkout-service (not part of this repo's migrations). Create a compatible
	// stand-in for the integration tests.
	if _, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS tenants (
			id UUID PRIMARY KEY,
			pix_key TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`); err != nil {
		return fmt.Errorf("create tenants stub: %w", err)
	}
	return nil
}

// covDB returns the shared pool after resetting all tables. Skips (never fails)
// when the container could not be started.
func covDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if covPool == nil {
		t.Skipf("postgres testcontainer unavailable: %v", covSetupErr)
	}
	covResetDB(t)
	return covPool
}

// covResetDB truncates every application table so each test runs isolated.
func covResetDB(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := covPool.Exec(ctx, "TRUNCATE TABLE "+covTablesToTruncate+" RESTART IDENTITY CASCADE"); err != nil {
		t.Fatalf("reset database: %v", err)
	}
}

// covSimplePool opens an extra pool that speaks the simple query protocol.
//
// Two repository queries only resolve correctly under the simple protocol:
//   - GetSalesStats builds `($2 || ' days')::INTERVAL`, so the parameter is
//     inferred as text and pgx cannot encode an int with the extended protocol.
//   - UpdatePayoutStatus uses $1 both as a VARCHAR column value and in
//     `CASE WHEN $1 = 'completed'`, which Postgres rejects as "inconsistent
//     types deduced for parameter $1" in prepared statements.
//
// Both are production behaviours, not test concerns; the tests document them and
// still exercise the queries end-to-end through this pool.
func covSimplePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(covDSN)
	covNoErr(t, err, "parse dsn")
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	covNoErr(t, err, "open simple-protocol pool")
	t.Cleanup(pool.Close)
	return pool
}

func covCtx(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), 30*time.Second)
}

// --- assertion helpers -------------------------------------------------------

func covNoErr(t *testing.T, err error, format string, args ...any) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: unexpected error: %v", fmt.Sprintf(format, args...), err)
	}
}

func covWantErr(t *testing.T, err error, format string, args ...any) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: expected error, got nil", fmt.Sprintf(format, args...))
	}
}

func covEqual[T comparable](t *testing.T, got, want T, what string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s = %v, want %v", what, got, want)
	}
}

// --- data helpers ------------------------------------------------------------

func covUUID(t *testing.T) string {
	t.Helper()
	id, err := uuid.NewRandom()
	if err != nil {
		t.Fatalf("generate uuid: %v", err)
	}
	return id.String()
}

func covNow() time.Time {
	return time.Now().UTC().Truncate(time.Microsecond)
}

// covNewProduct inserts a product through the repository and returns it.
func covNewProduct(t *testing.T, pool *pgxpool.Pool, tenantID, slug string) *domain.DigitalProduct {
	t.Helper()
	now := covNow()
	p := &domain.DigitalProduct{
		ID:            covUUID(t),
		TenantID:      tenantID,
		Slug:          slug,
		Name:          "Product " + slug,
		Description:   "Description of " + slug,
		PriceCents:    10000,
		Currency:      "BRL",
		ProductType:   "digital",
		FileURL:       "https://files.example.com/" + slug + ".pdf",
		CoverImageURL: "https://files.example.com/" + slug + ".png",
		DownloadLimit: 5,
		IsActive:      true,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	covNoErr(t, NewLinkPayRepository(pool).CreateProduct(context.Background(), p), "create product %s", slug)
	return p
}

// covNewPurchase inserts a purchase through the repository. createdAgo shifts
// created_at into the past (used to exercise holdback windows).
func covNewPurchase(t *testing.T, pool *pgxpool.Pool, p *domain.DigitalProduct, email, method, status string, createdAgo time.Duration) *domain.DigitalPurchase {
	t.Helper()
	createdAt := covNow().Add(-createdAgo)
	expires := createdAt.Add(72 * time.Hour)
	pu := &domain.DigitalPurchase{
		ID:               covUUID(t),
		ProductID:        p.ID,
		TenantID:         p.TenantID,
		CustomerEmail:    email,
		CustomerName:     "Buyer " + email,
		CustomerCPF:      "12345678901",
		AmountCents:      p.PriceCents,
		PlatformFeeCents: p.PriceCents * 30 / 100,
		CreatorEarnings:  p.PriceCents * 70 / 100,
		PaymentMethod:    method,
		PaymentStatus:    status,
		ExternalChargeID: "chg_" + covUUID(t),
		AccessToken:      covPurchaseToken(t),
		AccessExpiresAt:  &expires,
		DownloadCount:    0,
		CreatedAt:        createdAt,
	}
	covNoErr(t, NewLinkPayRepository(pool).CreatePurchase(context.Background(), pu), "create purchase")
	return pu
}

func covPurchaseToken(t *testing.T) string {
	t.Helper()
	return "cov" + strings.ReplaceAll(covUUID(t), "-", "")
}

// covSetPurchasePayout links a purchase to a payout row directly via SQL.
func covSetPurchasePayout(t *testing.T, pool *pgxpool.Pool, purchaseID, payoutID string) {
	t.Helper()
	ctx, cancel := covCtx(t)
	defer cancel()
	_, err := pool.Exec(ctx, `UPDATE digital_purchases SET payout_id = $1 WHERE id = $2`, payoutID, purchaseID)
	covNoErr(t, err, "link purchase to payout")
}

// covInsertPayoutRow inserts a bare creator_payouts row (FK target for payout_id).
func covInsertPayoutRow(t *testing.T, pool *pgxpool.Pool, tenantID string, createdAt time.Time) string {
	t.Helper()
	id := covUUID(t)
	ctx, cancel := covCtx(t)
	defer cancel()
	_, err := pool.Exec(ctx, `
		INSERT INTO creator_payouts (id, tenant_id, amount_cents, status, purchase_ids, created_at, updated_at)
		VALUES ($1, $2, 0, 'pending', '[]'::jsonb, $3, $3)`,
		id, tenantID, createdAt)
	covNoErr(t, err, "insert payout row")
	return id
}

func covInsertTenant(t *testing.T, pool *pgxpool.Pool, tenantID, pixKey string) {
	t.Helper()
	ctx, cancel := covCtx(t)
	defer cancel()
	_, err := pool.Exec(ctx, `INSERT INTO tenants (id, pix_key) VALUES ($1, $2)`, tenantID, pixKey)
	covNoErr(t, err, "insert tenant")
}
