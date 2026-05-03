-- BKL-603: Comissão da plataforma LinkPay (30%) — criador ganha 70%.
-- Adiciona campos de comissão em digital_products e earnings em digital_purchases.

-- Comissão configurável por produto (default 30% para a plataforma)
ALTER TABLE digital_products
  ADD COLUMN IF NOT EXISTS platform_commission_pct INTEGER NOT NULL DEFAULT 30
    CHECK (platform_commission_pct >= 0 AND platform_commission_pct <= 100);

-- Detalhamento de earnings por compra
ALTER TABLE digital_purchases
  ADD COLUMN IF NOT EXISTS platform_fee_cents INTEGER NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS creator_earnings_cents INTEGER NOT NULL DEFAULT 0;

-- Índice para relatórios de earnings por tenant/criador
CREATE INDEX IF NOT EXISTS idx_digital_purchases_earnings
  ON digital_purchases(tenant_id, payment_status, created_at DESC);

COMMENT ON COLUMN digital_products.platform_commission_pct IS 'Percentual de comissão da plataforma (0-100). Padrão: 30%.';
COMMENT ON COLUMN digital_purchases.platform_fee_cents IS 'Taxa da plataforma em centavos (30% do amount_cents por padrão).';
COMMENT ON COLUMN digital_purchases.creator_earnings_cents IS 'Valor líquido recebido pelo criador após dedução da comissão.';
