-- BKL-1214: Antecipação de recebíveis de cartão de crédito D+2 com taxa de 2%
ALTER TABLE creator_payouts
  ADD COLUMN IF NOT EXISTS is_anticipation       BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN IF NOT EXISTS anticipation_fee_cents INT     NOT NULL DEFAULT 0;

ALTER TABLE digital_purchases
  ADD COLUMN IF NOT EXISTS anticipation_requested_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_digital_purchases_anticipation
  ON digital_purchases(tenant_id, anticipation_requested_at)
  WHERE anticipation_requested_at IS NOT NULL;
