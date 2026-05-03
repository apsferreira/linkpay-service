-- ADR-003: creator_configs substitui as colunas pix_key/payout_schedule
-- que foram adicionadas na tabela tenants do checkout-service.
-- Aqui temos ownership correto: configurações de creator pertencem ao linkpay-service.
CREATE TABLE IF NOT EXISTS creator_configs (
  tenant_id       UUID        PRIMARY KEY,
  pix_key         TEXT,
  payout_schedule VARCHAR(20) NOT NULL DEFAULT 'weekly'
    CHECK (payout_schedule IN ('daily','weekly','monthly','manual')),
  created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
