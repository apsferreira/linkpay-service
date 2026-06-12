package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"linkpay-service/internal/domain"
)

// LinkPayRepository gerencia produtos digitais e compras LinkPay.
type LinkPayRepository struct {
	db *pgxpool.Pool
}

func NewLinkPayRepository(db *pgxpool.Pool) *LinkPayRepository {
	return &LinkPayRepository{db: db}
}

// --- Produtos ---

// CreateProduct insere um novo produto digital.
// BKL-1117: inclui platform_commission_pct (default 30, conforme CommissionSplit em CONTEXT.md).
func (r *LinkPayRepository) CreateProduct(ctx context.Context, p *domain.DigitalProduct) error {
	if p.PlatformCommissionPct == 0 {
		p.PlatformCommissionPct = domain.DefaultPlatformCommissionPct
	}
	query := `
		INSERT INTO digital_products
			(id, tenant_id, slug, name, description, price_cents, currency, product_type,
			 file_url, cover_image_url, download_limit, is_active, platform_commission_pct, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	`
	_, err := r.db.Exec(ctx, query,
		p.ID, p.TenantID, p.Slug, p.Name, p.Description, p.PriceCents, p.Currency,
		p.ProductType, p.FileURL, p.CoverImageURL, p.DownloadLimit, p.IsActive, p.PlatformCommissionPct,
		p.CreatedAt, p.UpdatedAt,
	)
	return err
}

// GetProductBySlug busca um produto pelo slug (acesso público, sem filtro de tenant).
func (r *LinkPayRepository) GetProductBySlug(ctx context.Context, slug string) (*domain.DigitalProduct, error) {
	query := `
		SELECT id, tenant_id, slug, name, description, price_cents, currency, product_type,
		       COALESCE(file_url, ''), COALESCE(cover_image_url, ''),
		       download_limit, is_active, platform_commission_pct,
		       COALESCE(storage_key, ''), COALESCE(storage_bucket, ''), COALESCE(file_name, ''),
		       COALESCE(file_size_bytes, 0), COALESCE(mime_type, ''),
		       created_at, updated_at
		FROM digital_products
		WHERE slug = $1 AND is_active = TRUE
	`
	row := r.db.QueryRow(ctx, query, slug)
	return scanProduct(row)
}

// GetProductByID busca um produto pelo ID dentro de um tenant.
func (r *LinkPayRepository) GetProductByID(ctx context.Context, id, tenantID string) (*domain.DigitalProduct, error) {
	query := `
		SELECT id, tenant_id, slug, name, description, price_cents, currency, product_type,
		       COALESCE(file_url, ''), COALESCE(cover_image_url, ''),
		       download_limit, is_active, platform_commission_pct, created_at, updated_at
		FROM digital_products
		WHERE id = $1 AND tenant_id = $2
	`
	row := r.db.QueryRow(ctx, query, id, tenantID)
	return scanProduct(row)
}

// ListProducts lista todos os produtos de um tenant.
func (r *LinkPayRepository) ListProducts(ctx context.Context, tenantID string) ([]domain.DigitalProduct, error) {
	query := `
		SELECT id, tenant_id, slug, name, description, price_cents, currency, product_type,
		       COALESCE(file_url, ''), COALESCE(cover_image_url, ''),
		       download_limit, is_active, platform_commission_pct,
		       COALESCE(storage_key, ''), COALESCE(storage_bucket, ''), COALESCE(file_name, ''),
		       COALESCE(file_size_bytes, 0), COALESCE(mime_type, ''),
		       created_at, updated_at
		FROM digital_products
		WHERE tenant_id = $1
		ORDER BY created_at DESC
	`
	rows, err := r.db.Query(ctx, query, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var products []domain.DigitalProduct
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			return nil, err
		}
		products = append(products, *p)
	}
	return products, rows.Err()
}

// UpdateProduct atualiza campos de um produto (filtrando por tenant).
func (r *LinkPayRepository) UpdateProduct(ctx context.Context, p *domain.DigitalProduct) error {
	query := `
		UPDATE digital_products
		SET name = $1, description = $2, price_cents = $3, product_type = $4,
		    file_url = $5, download_limit = $6, is_active = $7, updated_at = $8
		WHERE id = $9 AND tenant_id = $10
	`
	tag, err := r.db.Exec(ctx, query,
		p.Name, p.Description, p.PriceCents, p.ProductType,
		p.FileURL, p.DownloadLimit, p.IsActive, p.UpdatedAt,
		p.ID, p.TenantID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("produto não encontrado")
	}
	return nil
}

// DeactivateProduct desativa um produto (soft delete).
func (r *LinkPayRepository) DeactivateProduct(ctx context.Context, id, tenantID string) error {
	query := `
		UPDATE digital_products
		SET is_active = FALSE, updated_at = $1
		WHERE id = $2 AND tenant_id = $3
	`
	tag, err := r.db.Exec(ctx, query, time.Now(), id, tenantID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("produto não encontrado")
	}
	return nil
}

// UpdateProductStorage atualiza os metadados de armazenamento MinIO de um produto.
// BKL-1205: chamado após upload bem-sucedido para registrar storage_key, file_name, etc.
func (r *LinkPayRepository) UpdateProductStorage(ctx context.Context, id, tenantID, storageKey, storageBucket, fileName string, fileSizeBytes int64, mimeType string) error {
	query := `
		UPDATE digital_products
		SET storage_key = $1, storage_bucket = $2, file_name = $3,
		    file_size_bytes = $4, mime_type = $5, updated_at = $6
		WHERE id = $7 AND tenant_id = $8
	`
	tag, err := r.db.Exec(ctx, query,
		storageKey, storageBucket, fileName, fileSizeBytes, mimeType, time.Now(),
		id, tenantID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("produto não encontrado")
	}
	return nil
}

// UpdateProductCover persiste a URL da imagem de capa do produto.
// #16: chamado após upload bem-sucedido da cover image.
func (r *LinkPayRepository) UpdateProductCover(ctx context.Context, id, tenantID, coverImageURL string) error {
	query := `
		UPDATE digital_products
		SET cover_image_url = $1, updated_at = $2
		WHERE id = $3 AND tenant_id = $4
	`
	tag, err := r.db.Exec(ctx, query, coverImageURL, time.Now(), id, tenantID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("produto não encontrado")
	}
	return nil
}

// --- Compras ---

// CreatePurchase insere uma nova compra pendente.
// BKL-603: inclui platform_fee_cents e creator_earnings_cents.
func (r *LinkPayRepository) CreatePurchase(ctx context.Context, p *domain.DigitalPurchase) error {
	query := `
		INSERT INTO digital_purchases
			(id, product_id, tenant_id, customer_email, customer_name, customer_cpf,
			 amount_cents, platform_fee_cents, creator_earnings_cents,
			 payment_method, payment_status, external_charge_id,
			 access_token, access_expires_at, download_count, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
	`
	_, err := r.db.Exec(ctx, query,
		p.ID, p.ProductID, p.TenantID, p.CustomerEmail, p.CustomerName, p.CustomerCPF,
		p.AmountCents, p.PlatformFeeCents, p.CreatorEarnings,
		p.PaymentMethod, p.PaymentStatus, p.ExternalChargeID,
		p.AccessToken, p.AccessExpiresAt, p.DownloadCount, p.CreatedAt,
	)
	return err
}

// GetPurchaseByToken busca uma compra pelo token de acesso.
func (r *LinkPayRepository) GetPurchaseByToken(ctx context.Context, token string) (*domain.DigitalPurchase, error) {
	query := `
		SELECT id, product_id, tenant_id, customer_email, customer_name,
		       COALESCE(customer_cpf, ''), amount_cents, platform_fee_cents, creator_earnings_cents, payment_method, payment_status,
		       COALESCE(external_charge_id, ''), COALESCE(access_token, ''),
		       access_expires_at, download_count, created_at
		FROM digital_purchases
		WHERE access_token = $1
	`
	row := r.db.QueryRow(ctx, query, token)
	return scanPurchase(row)
}

// GetPurchaseByChargeID busca uma compra pelo ID externo de cobrança (Asaas).
func (r *LinkPayRepository) GetPurchaseByChargeID(ctx context.Context, chargeID string) (*domain.DigitalPurchase, error) {
	query := `
		SELECT id, product_id, tenant_id, customer_email, customer_name,
		       COALESCE(customer_cpf, ''), amount_cents, platform_fee_cents, creator_earnings_cents, payment_method, payment_status,
		       COALESCE(external_charge_id, ''), COALESCE(access_token, ''),
		       access_expires_at, download_count, created_at
		FROM digital_purchases
		WHERE external_charge_id = $1
	`
	row := r.db.QueryRow(ctx, query, chargeID)
	return scanPurchase(row)
}

// UpdatePurchaseStatus atualiza status e charge ID de uma compra.
func (r *LinkPayRepository) UpdatePurchaseStatus(ctx context.Context, id, status, chargeID string) error {
	query := `
		UPDATE digital_purchases
		SET payment_status = $1, external_charge_id = $2
		WHERE id = $3
	`
	_, err := r.db.Exec(ctx, query, status, chargeID, id)
	return err
}

// ConfirmPurchase marca compra como confirmada e grava o access_token + expiração.
func (r *LinkPayRepository) ConfirmPurchase(ctx context.Context, id, accessToken string, expiresAt time.Time) error {
	query := `
		UPDATE digital_purchases
		SET payment_status = 'confirmed', access_token = $1, access_expires_at = $2
		WHERE id = $3
	`
	_, err := r.db.Exec(ctx, query, accessToken, expiresAt, id)
	return err
}

// IncrementDownload incrementa o contador de downloads de uma compra.
func (r *LinkPayRepository) IncrementDownload(ctx context.Context, token string) error {
	query := `
		UPDATE digital_purchases
		SET download_count = download_count + 1
		WHERE access_token = $1
	`
	_, err := r.db.Exec(ctx, query, token)
	return err
}

// GetSalesStats retorna métricas de vendas dos últimos N dias para um tenant.
func (r *LinkPayRepository) GetSalesStats(ctx context.Context, tenantID string, days int) (*domain.LinkPayStatsResponse, error) {
	// Total de vendas e receita no período
	totalQuery := `
		SELECT
			COUNT(*),
			COALESCE(SUM(amount_cents), 0)
		FROM digital_purchases
		WHERE tenant_id = $1
		  AND payment_status = 'confirmed'
		  AND created_at >= NOW() - ($2 || ' days')::INTERVAL
	`
	var totalSales int
	var totalRevenue int64
	if err := r.db.QueryRow(ctx, totalQuery, tenantID, days).Scan(&totalSales, &totalRevenue); err != nil {
		return nil, fmt.Errorf("erro ao buscar totais: %w", err)
	}

	// Vendas por produto
	productQuery := `
		SELECT
			dp.id,
			dp.name,
			COUNT(pu.id)                 AS sales,
			COALESCE(SUM(pu.amount_cents), 0) AS revenue_cents
		FROM digital_purchases pu
		JOIN digital_products dp ON dp.id = pu.product_id
		WHERE pu.tenant_id = $1
		  AND pu.payment_status = 'confirmed'
		  AND pu.created_at >= NOW() - ($2 || ' days')::INTERVAL
		GROUP BY dp.id, dp.name
		ORDER BY revenue_cents DESC
	`
	productRows, err := r.db.Query(ctx, productQuery, tenantID, days)
	if err != nil {
		return nil, fmt.Errorf("erro ao buscar stats por produto: %w", err)
	}
	defer productRows.Close()

	var products []domain.LinkPayProductStats
	for productRows.Next() {
		var p domain.LinkPayProductStats
		if err := productRows.Scan(&p.ID, &p.Name, &p.Sales, &p.RevenueCents); err != nil {
			return nil, fmt.Errorf("erro ao escanear produto: %w", err)
		}
		products = append(products, p)
	}
	if err := productRows.Err(); err != nil {
		return nil, err
	}

	// Vendas diárias
	dailyQuery := `
		SELECT
			TO_CHAR(created_at AT TIME ZONE 'America/Sao_Paulo', 'YYYY-MM-DD') AS day,
			COUNT(*)                          AS sales,
			COALESCE(SUM(amount_cents), 0)    AS revenue_cents
		FROM digital_purchases
		WHERE tenant_id = $1
		  AND payment_status = 'confirmed'
		  AND created_at >= NOW() - ($2 || ' days')::INTERVAL
		GROUP BY day
		ORDER BY day ASC
	`
	dailyRows, err := r.db.Query(ctx, dailyQuery, tenantID, days)
	if err != nil {
		return nil, fmt.Errorf("erro ao buscar stats diários: %w", err)
	}
	defer dailyRows.Close()

	var daily []domain.LinkPayDailyStat
	for dailyRows.Next() {
		var d domain.LinkPayDailyStat
		if err := dailyRows.Scan(&d.Date, &d.Sales, &d.RevenueCents); err != nil {
			return nil, fmt.Errorf("erro ao escanear dia: %w", err)
		}
		daily = append(daily, d)
	}
	if err := dailyRows.Err(); err != nil {
		return nil, err
	}

	if products == nil {
		products = []domain.LinkPayProductStats{}
	}
	if daily == nil {
		daily = []domain.LinkPayDailyStat{}
	}

	return &domain.LinkPayStatsResponse{
		Period:            fmt.Sprintf("last_%d_days", days),
		TotalRevenueCents: totalRevenue,
		TotalSales:        totalSales,
		Products:          products,
		DailySales:        daily,
	}, nil
}

// --- Arquivos de produto (BKL-1206) ---

// AddProductFile insere um arquivo adicional para um produto digital.
// sort_order é calculado automaticamente como MAX(sort_order)+1 para o produto.
func (r *LinkPayRepository) AddProductFile(ctx context.Context, f *domain.DigitalProductFile) error {
	query := `
		INSERT INTO digital_product_files
			(product_id, storage_key, file_name, file_size_bytes, mime_type, sort_order)
		VALUES (
			$1, $2, $3, $4, $5,
			(SELECT COALESCE(MAX(sort_order), 0) + 1 FROM digital_product_files WHERE product_id = $1)
		)
		RETURNING id, created_at
	`
	row := r.db.QueryRow(ctx, query,
		f.ProductID, f.StorageKey, f.FileName, f.FileSizeBytes, f.MimeType,
	)
	return row.Scan(&f.ID, &f.CreatedAt)
}

// ListProductFiles retorna todos os arquivos de um produto, ordenados por sort_order.
func (r *LinkPayRepository) ListProductFiles(ctx context.Context, productID string) ([]domain.DigitalProductFile, error) {
	query := `
		SELECT id, product_id, storage_key, file_name,
		       COALESCE(file_size_bytes, 0), COALESCE(mime_type, ''), sort_order, created_at
		FROM digital_product_files
		WHERE product_id = $1
		ORDER BY sort_order ASC
	`
	rows, err := r.db.Query(ctx, query, productID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []domain.DigitalProductFile
	for rows.Next() {
		var f domain.DigitalProductFile
		if err := rows.Scan(
			&f.ID, &f.ProductID, &f.StorageKey, &f.FileName,
			&f.FileSizeBytes, &f.MimeType, &f.SortOrder, &f.CreatedAt,
		); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

// GetProductFile busca um arquivo específico pelo ID.
func (r *LinkPayRepository) GetProductFile(ctx context.Context, fileID string) (*domain.DigitalProductFile, error) {
	query := `
		SELECT id, product_id, storage_key, file_name,
		       COALESCE(file_size_bytes, 0), COALESCE(mime_type, ''), sort_order, created_at
		FROM digital_product_files
		WHERE id = $1
	`
	row := r.db.QueryRow(ctx, query, fileID)
	var f domain.DigitalProductFile
	err := row.Scan(
		&f.ID, &f.ProductID, &f.StorageKey, &f.FileName,
		&f.FileSizeBytes, &f.MimeType, &f.SortOrder, &f.CreatedAt,
	)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, fmt.Errorf("arquivo não encontrado")
		}
		return nil, err
	}
	return &f, nil
}

// DeleteProductFile remove um arquivo pelo ID, validando que pertence ao produto.
func (r *LinkPayRepository) DeleteProductFile(ctx context.Context, fileID, productID string) error {
	query := `DELETE FROM digital_product_files WHERE id = $1 AND product_id = $2`
	tag, err := r.db.Exec(ctx, query, fileID, productID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("arquivo não encontrado")
	}
	return nil
}

// --- helpers de scan ---

type scanner interface {
	Scan(dest ...any) error
}

// GetUpsellsByProductID retorna os produtos sugeridos como upsell para um produto, ordenados por display_order.
// BKL-1221.
func (r *LinkPayRepository) GetUpsellsByProductID(ctx context.Context, productID string) ([]domain.UpsellSuggestion, error) {
	query := `
		SELECT upsell_product_id::text, display_order
		FROM product_upsells
		WHERE product_id = $1
		ORDER BY display_order ASC
	`
	rows, err := r.db.Query(ctx, query, productID)
	if err != nil {
		return nil, fmt.Errorf("GetUpsellsByProductID: %w", err)
	}
	defer rows.Close()

	var upsells []domain.UpsellSuggestion
	for rows.Next() {
		var u domain.UpsellSuggestion
		if err := rows.Scan(&u.ProductID, &u.DisplayOrder); err != nil {
			return nil, fmt.Errorf("GetUpsellsByProductID scan: %w", err)
		}
		upsells = append(upsells, u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if upsells == nil {
		upsells = []domain.UpsellSuggestion{}
	}
	return upsells, nil
}

// --- Commission Ledger (BKL-1515) ---

// InsertCommissionEntry insere um registro no ledger de comissões após purchase confirmada.
func (r *LinkPayRepository) InsertCommissionEntry(ctx context.Context, entry *domain.CommissionEntry) error {
	query := `
		INSERT INTO commission_ledger
			(id, purchase_id, tenant_id, product_id,
			 gross_amount_cents, commission_percent, commission_cents, creator_payout_cents,
			 created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	_, err := r.db.Exec(ctx, query,
		entry.ID, entry.PurchaseID, entry.TenantID, entry.ProductID,
		entry.GrossAmountCents, entry.CommissionPercent, entry.CommissionCents, entry.CreatorPayoutCents,
		entry.CreatedAt,
	)
	return err
}

// GetCommissionSummary retorna o resumo de receita do IIT no período informado.
func (r *LinkPayRepository) GetCommissionSummary(ctx context.Context, startDate, endDate string) (*domain.CommissionSummaryResponse, error) {
	query := `
		SELECT
			COALESCE(SUM(gross_amount_cents), 0),
			COALESCE(SUM(commission_cents), 0),
			COALESCE(SUM(creator_payout_cents), 0),
			COUNT(*)
		FROM commission_ledger
		WHERE created_at >= $1::TIMESTAMPTZ
		  AND created_at < ($2::TIMESTAMPTZ + INTERVAL '1 day')
	`
	var resp domain.CommissionSummaryResponse
	var totalGross, totalCommission, totalPayout int64
	var totalSales int
	err := r.db.QueryRow(ctx, query, startDate, endDate).Scan(
		&totalGross, &totalCommission, &totalPayout, &totalSales,
	)
	if err != nil {
		return nil, fmt.Errorf("commission summary: %w", err)
	}
	resp.PeriodStart = startDate
	resp.PeriodEnd = endDate
	resp.TotalGrossCents = totalGross
	resp.TotalCommissionCents = totalCommission
	resp.TotalPayoutCents = totalPayout
	resp.TotalSales = totalSales
	return &resp, nil
}

func scanProduct(row scanner) (*domain.DigitalProduct, error) {
	var p domain.DigitalProduct
	err := row.Scan(
		&p.ID, &p.TenantID, &p.Slug, &p.Name, &p.Description,
		&p.PriceCents, &p.Currency, &p.ProductType, &p.FileURL,
		&p.CoverImageURL,
		&p.DownloadLimit, &p.IsActive, &p.PlatformCommissionPct,
		// BKL-1205: campos de storage MinIO
		&p.StorageKey, &p.StorageBucket, &p.FileName, &p.FileSizeBytes, &p.MimeType,
		&p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("produto não encontrado")
		}
		return nil, err
	}
	return &p, nil
}

func scanPurchase(row scanner) (*domain.DigitalPurchase, error) {
	var p domain.DigitalPurchase
	err := row.Scan(
		&p.ID, &p.ProductID, &p.TenantID, &p.CustomerEmail, &p.CustomerName,
		&p.CustomerCPF, &p.AmountCents, &p.PlatformFeeCents, &p.CreatorEarnings,
		&p.PaymentMethod, &p.PaymentStatus,
		&p.ExternalChargeID, &p.AccessToken, &p.AccessExpiresAt,
		&p.DownloadCount, &p.CreatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("compra não encontrada")
		}
		return nil, err
	}
	return &p, nil
}
