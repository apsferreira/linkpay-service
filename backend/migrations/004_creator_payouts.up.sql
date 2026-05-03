-- BKL-1213: Tabela de repasses automáticos para produtores via Asaas Transfers API
CREATE TABLE IF NOT EXISTS creator_payouts (
  id                 UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id          UUID        NOT NULL,
  amount_cents       INT         NOT NULL,
  platform_fee_cents INT         NOT NULL DEFAULT 0,
  status             VARCHAR(20) NOT NULL DEFAULT 'pending',
  payout_method      VARCHAR(20) NOT NULL DEFAULT 'pix',
  pix_key            TEXT,
  asaas_transfer_id  VARCHAR(100),
  scheduled_for      TIMESTAMPTZ,
  completed_at       TIMESTAMPTZ,
  purchase_ids       JSONB       NOT NULL DEFAULT '[]',
  error_message      TEXT,
  created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_creator_payouts_tenant
  ON creator_payouts(tenant_id, status, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_creator_payouts_asaas
  ON creator_payouts(asaas_transfer_id)
  WHERE asaas_transfer_id IS NOT NULL;

-- Adiciona coluna payout_id em digital_purchases para rastrear quais compras já foram repassadas
ALTER TABLE digital_purchases
  ADD COLUMN IF NOT EXISTS payout_id UUID REFERENCES creator_payouts(id);

CREATE INDEX IF NOT EXISTS idx_digital_purchases_payout
  ON digital_purchases(payout_id)
  WHERE payout_id IS NOT NULL;

-- Config de saque por tenant
