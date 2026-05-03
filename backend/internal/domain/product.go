package domain

import (
	"encoding/json"
	"time"
)

const (
	DefaultPlatformCommissionPct = 10 // 10% IIT, criador recebe 90%

	HoldbackPIXDays    = 2
	HoldbackBoletoDays = 2
	HoldbackCardDays   = 15
	HoldbackReservePct = 10 // 10% retido por 30d anti-chargeback

	AnticipationFeePercent = 2 // 2% de taxa sobre valor líquido elegível
	AnticipationDays       = 2 // liberação em D+2 ao invés de D+15
)

type DigitalProduct struct {
	ID                    string    `json:"id"                       db:"id"`
	TenantID              string    `json:"tenant_id"                db:"tenant_id"`
	Slug                  string    `json:"slug"                     db:"slug"`
	Name                  string    `json:"name"                     db:"name"`
	Description           string    `json:"description"              db:"description"`
	PriceCents            int       `json:"price_cents"              db:"price_cents"`
	Currency              string    `json:"currency"                 db:"currency"`
	ProductType           string    `json:"product_type"             db:"product_type"`
	FileURL               string    `json:"file_url,omitempty"       db:"file_url"`
	DownloadLimit         int       `json:"download_limit"           db:"download_limit"`
	IsActive              bool      `json:"is_active"                db:"is_active"`
	PlatformCommissionPct int       `json:"platform_commission_pct"  db:"platform_commission_pct"`
	StorageKey            string    `json:"storage_key,omitempty"    db:"storage_key"`
	StorageBucket         string    `json:"storage_bucket,omitempty" db:"storage_bucket"`
	FileName              string    `json:"file_name,omitempty"      db:"file_name"`
	FileSizeBytes         int64     `json:"file_size_bytes,omitempty" db:"file_size_bytes"`
	MimeType              string    `json:"mime_type,omitempty"      db:"mime_type"`
	CreatedAt             time.Time `json:"created_at"               db:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"               db:"updated_at"`
	Files                 []DigitalProductFile `json:"files,omitempty" db:"-"`
}

func (p *DigitalProduct) CreatorEarningsCents(amountCents int) int {
	commission := p.PlatformCommissionPct
	if commission <= 0 {
		commission = DefaultPlatformCommissionPct
	}
	return amountCents - (amountCents*commission/100)
}

func (p *DigitalProduct) PlatformFeeCents(amountCents int) int {
	commission := p.PlatformCommissionPct
	if commission <= 0 {
		commission = DefaultPlatformCommissionPct
	}
	return amountCents * commission / 100
}

type DigitalProductFile struct {
	ID            string    `json:"id"              db:"id"`
	ProductID     string    `json:"product_id"      db:"product_id"`
	StorageKey    string    `json:"storage_key"     db:"storage_key"`
	FileName      string    `json:"file_name"       db:"file_name"`
	FileSizeBytes int64     `json:"file_size_bytes" db:"file_size_bytes"`
	MimeType      string    `json:"mime_type"       db:"mime_type"`
	SortOrder     int       `json:"sort_order"      db:"sort_order"`
	CreatedAt     time.Time `json:"created_at"      db:"created_at"`
}

type DigitalPurchase struct {
	ID               string     `json:"id"                        db:"id"`
	ProductID        string     `json:"product_id"                db:"product_id"`
	TenantID         string     `json:"tenant_id"                 db:"tenant_id"`
	CustomerEmail    string     `json:"customer_email"            db:"customer_email"`
	CustomerName     string     `json:"customer_name"             db:"customer_name"`
	CustomerCPF      string     `json:"customer_cpf,omitempty"    db:"customer_cpf"`
	AmountCents      int        `json:"amount_cents"              db:"amount_cents"`
	PlatformFeeCents int        `json:"platform_fee_cents"        db:"platform_fee_cents"`
	CreatorEarnings  int        `json:"creator_earnings_cents"    db:"creator_earnings_cents"`
	PaymentMethod    string     `json:"payment_method"            db:"payment_method"`
	PaymentStatus    string     `json:"payment_status"            db:"payment_status"`
	ExternalChargeID string     `json:"external_charge_id,omitempty" db:"external_charge_id"`
	AccessToken      string     `json:"access_token,omitempty"    db:"access_token"`
	AccessExpiresAt  *time.Time `json:"access_expires_at,omitempty" db:"access_expires_at"`
	DownloadCount    int        `json:"download_count"            db:"download_count"`
	PayoutID         *string    `json:"payout_id,omitempty"       db:"payout_id"`
	AnticipationRequestedAt *time.Time `json:"anticipation_requested_at,omitempty" db:"anticipation_requested_at"`
	CreatedAt        time.Time  `json:"created_at"                db:"created_at"`

	// Campos transitórios (não persistidos)
	PixQRCode    string `json:"pix_qr_code,omitempty"`
	PixCopyPaste string `json:"pix_copy_paste,omitempty"`
	PaymentURL   string `json:"payment_url,omitempty"`
}

type DigitalSubscriptionPlan struct {
	ID          string    `json:"id"`
	ProductID   string    `json:"product_id"`
	TenantID    string    `json:"tenant_id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Description string    `json:"description,omitempty"`
	PriceCents  int       `json:"price_cents"`
	Interval    string    `json:"interval"` // monthly | yearly
	TrialDays   int       `json:"trial_days"`
	IsActive    bool      `json:"is_active"`
	SortOrder   int       `json:"sort_order"`
	Features    []string  `json:"features"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type DigitalSubscription struct {
	ID                  string     `json:"id"`
	PlanID              string     `json:"plan_id"`
	TenantID            string     `json:"tenant_id"`
	CustomerEmail       string     `json:"customer_email"`
	CustomerName        string     `json:"customer_name"`
	CustomerCPF         string     `json:"customer_cpf,omitempty"`
	AsaasSubscriptionID string     `json:"asaas_subscription_id,omitempty"`
	AsaasCustomerID     string     `json:"asaas_customer_id,omitempty"`
	Status              string     `json:"status"`
	CurrentPeriodStart  *time.Time `json:"current_period_start,omitempty"`
	CurrentPeriodEnd    *time.Time `json:"current_period_end,omitempty"`
	CancelledAt         *time.Time `json:"cancelled_at,omitempty"`
	AccessToken         string     `json:"access_token"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
	Plan                *DigitalSubscriptionPlan `json:"plan,omitempty"`
}

type CreatorPayout struct {
	ID                   string          `json:"id"                            db:"id"`
	TenantID             string          `json:"tenant_id"                     db:"tenant_id"`
	AmountCents          int             `json:"amount_cents"                  db:"amount_cents"`
	PlatformFeeCents     int             `json:"platform_fee_cents"            db:"platform_fee_cents"`
	Status               string          `json:"status"                        db:"status"`
	PayoutMethod         string          `json:"payout_method"                 db:"payout_method"`
	PixKey               string          `json:"pix_key,omitempty"             db:"pix_key"`
	AsaasTransferID      string          `json:"asaas_transfer_id,omitempty"   db:"asaas_transfer_id"`
	ScheduledFor         *time.Time      `json:"scheduled_for,omitempty"       db:"scheduled_for"`
	CompletedAt          *time.Time      `json:"completed_at,omitempty"        db:"completed_at"`
	PurchaseIDs          json.RawMessage `json:"purchase_ids"                  db:"purchase_ids"`
	ErrorMessage         string          `json:"error_message,omitempty"       db:"error_message"`
	IsAnticipation       bool            `json:"is_anticipation"               db:"is_anticipation"`
	AnticipationFeeCents int             `json:"anticipation_fee_cents"        db:"anticipation_fee_cents"`
	CreatedAt            time.Time       `json:"created_at"                    db:"created_at"`
	UpdatedAt            time.Time       `json:"updated_at"                    db:"updated_at"`
}

type Balance struct {
	AvailableCents int    `json:"available_cents"`
	HeldCents      int    `json:"held_cents"`
	ReserveCents   int    `json:"reserve_cents"`
	TotalCents     int    `json:"total_cents"`
	NextPayoutDate string `json:"next_payout_date"`
}

type AnticipationPreview struct {
	EligibleCents     int    `json:"eligible_cents"`
	FeeCents          int    `json:"fee_cents"`
	NetCents          int    `json:"net_cents"`
	EligiblePurchases int    `json:"eligible_purchases"`
	AvailableAt       string `json:"available_at"`
}

// --- Requests ---

type CreateProductRequest struct {
	Slug          string `json:"slug"           validate:"required,min=2,max=100"`
	Name          string `json:"name"           validate:"required,max=255"`
	Description   string `json:"description"    validate:"max=5000"`
	PriceCents    int    `json:"price_cents"    validate:"required,min=1"`
	ProductType   string `json:"product_type"   validate:"required,oneof=digital ebook course template"`
	FileURL       string `json:"file_url"       validate:"omitempty,url,max=2048"`
	DownloadLimit int    `json:"download_limit" validate:"min=1,max=100"`
}

// CreateDigitalProductRequest é o alias canônico usado pelo handler e service.
type CreateDigitalProductRequest = CreateProductRequest

type UpdateProductRequest struct {
	Name          string `json:"name"           validate:"omitempty,max=255"`
	Description   string `json:"description"    validate:"omitempty,max=5000"`
	PriceCents    int    `json:"price_cents"    validate:"omitempty,min=1"`
	ProductType   string `json:"product_type"   validate:"omitempty,oneof=digital ebook course template"`
	FileURL       string `json:"file_url"       validate:"omitempty,url,max=2048"`
	DownloadLimit int    `json:"download_limit" validate:"omitempty,min=1,max=100"`
	IsActive      *bool  `json:"is_active"`
}

// UpdateDigitalProductRequest é o alias canônico usado pelo handler e service.
type UpdateDigitalProductRequest = UpdateProductRequest

type InitiatePurchaseRequest struct {
	CustomerName  string `json:"customer_name"  validate:"required,max=255"`
	CustomerEmail string `json:"customer_email" validate:"required,email,max=255"`
	CustomerCPF   string `json:"customer_cpf"   validate:"omitempty,max=14"`
	PaymentMethod string `json:"payment_method" validate:"required,oneof=pix credit_card"`
}

type CreateSubscriptionPlanRequest struct {
	Name        string   `json:"name"        validate:"required,max=100"`
	Slug        string   `json:"slug"        validate:"required,min=2,max=50"`
	Description string   `json:"description" validate:"omitempty,max=2000"`
	PriceCents  int      `json:"price_cents" validate:"required,min=1"`
	Interval    string   `json:"interval"    validate:"required,oneof=monthly yearly"`
	TrialDays   int      `json:"trial_days"  validate:"min=0,max=90"`
	SortOrder   int      `json:"sort_order"  validate:"min=0"`
	Features    []string `json:"features"    validate:"omitempty,max=20,dive,max=255"`
}

type UpdateSubscriptionPlanRequest struct {
	Name        string   `json:"name"        validate:"omitempty,max=100"`
	Description string   `json:"description" validate:"omitempty,max=2000"`
	PriceCents  int      `json:"price_cents" validate:"omitempty,min=1"`
	TrialDays   int      `json:"trial_days"  validate:"omitempty,min=0,max=90"`
	SortOrder   int      `json:"sort_order"  validate:"omitempty,min=0"`
	Features    []string `json:"features"    validate:"omitempty,max=20,dive,max=255"`
	IsActive    *bool    `json:"is_active"`
}

type SubscribeRequest struct {
	CustomerName  string `json:"customer_name"  validate:"required,max=255"`
	CustomerEmail string `json:"customer_email" validate:"required,email,max=255"`
	CustomerCPF   string `json:"customer_cpf"   validate:"omitempty,max=14"`
}

type PayoutRequestBody struct {
	PixKey      string `json:"pix_key"      validate:"required,max=255"`
	AmountCents int    `json:"amount_cents" validate:"omitempty,min=1000"`
}

type AnticipationRequestBody struct {
	PixKey string `json:"pix_key" validate:"omitempty,max=255"`
}

// --- Responses ---

type ProductStats struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Sales        int    `json:"sales"`
	RevenueCents int64  `json:"revenue_cents"`
}

// LinkPayProductStats é o alias canônico para stats de produto no LinkPay.
type LinkPayProductStats = ProductStats

type DailyStat struct {
	Date         string `json:"date"`
	Sales        int    `json:"sales"`
	RevenueCents int64  `json:"revenue_cents"`
}

// LinkPayDailyStat é o alias canônico para stats diárias no LinkPay.
type LinkPayDailyStat = DailyStat

type StatsResponse struct {
	Period            string         `json:"period"`
	TotalRevenueCents int64          `json:"total_revenue_cents"`
	TotalSales        int            `json:"total_sales"`
	Products          []ProductStats `json:"products"`
	DailySales        []DailyStat    `json:"daily_sales"`
}

// LinkPayStatsResponse é o alias canônico para a resposta de stats do LinkPay.
type LinkPayStatsResponse = StatsResponse

// UpsellSuggestion representa um produto sugerido como upsell/cross-sell. BKL-1221.
type UpsellSuggestion struct {
	ProductID    string `json:"product_id"    db:"product_id"`
	DisplayOrder int    `json:"display_order" db:"display_order"`
}

// CreateCartRequest é o body do endpoint POST /api/v1/public/products/:slug/cart. BKL-1221.
type CreateCartRequest struct {
	AddUpsells bool `json:"add_upsells"`
}

type ListPayoutsResponse struct {
	Data  []CreatorPayout `json:"data"`
	Total int             `json:"total"`
	Page  int             `json:"page"`
	Limit int             `json:"limit"`
}

type SubscriberFile struct {
	ProductID   string `json:"product_id"`
	ProductName string `json:"product_name"`
	DownloadURL string `json:"download_url"`
	FileName    string `json:"file_name"`
	MimeType    string `json:"mime_type"`
}
