-- BKL-1515: Commission ledger — auditoria das comissões efetivamente cobradas por venda.
-- Permite reconciliar quanto o IIT ganhou em um período para FP&A, NFS-e e detecção de
-- discrepâncias entre Asaas e banco.

CREATE TABLE IF NOT EXISTS commission_ledger (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    purchase_id        UUID NOT NULL REFERENCES digital_purchases(id),
    tenant_id          UUID NOT NULL,
    product_id         UUID NOT NULL,
    gross_amount_cents INTEGER NOT NULL,
    commission_percent NUMERIC(5,2) NOT NULL,
    commission_cents   INTEGER NOT NULL,
    creator_payout_cents INTEGER NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Índice para consultas de receita do IIT por período
CREATE INDEX IF NOT EXISTS idx_commission_ledger_period
    ON commission_ledger(created_at DESC);

-- Índice único por purchase (garantir que cada compra gere exatamente um registro)
CREATE UNIQUE INDEX IF NOT EXISTS idx_commission_ledger_purchase
    ON commission_ledger(purchase_id);

COMMENT ON TABLE commission_ledger IS 'Ledger de auditoria das comissões da plataforma (IIT) por venda.';
COMMENT ON COLUMN commission_ledger.commission_percent IS 'Percentual de comissão da plataforma aplicado (ex: 30.00 = 30%)';
COMMENT ON COLUMN commission_ledger.commission_cents IS 'Valor da comissão em centavos (gross_amount_cents * commission_percent / 100)';
COMMENT ON COLUMN commission_ledger.creator_payout_cents IS 'Valor líquido pago ao criador após dedução da comissão';
