-- BKL-1221: tabela de upsells/cross-sells sugeridos por produto
-- DOWN:
-- DROP INDEX IF EXISTS idx_product_upsells_product;
-- DROP TABLE IF EXISTS product_upsells;

CREATE TABLE IF NOT EXISTS product_upsells (
    id                UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    product_id        UUID        NOT NULL REFERENCES digital_products(id) ON DELETE CASCADE,
    upsell_product_id UUID        NOT NULL REFERENCES digital_products(id) ON DELETE CASCADE,
    display_order     INT         NOT NULL DEFAULT 0,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(product_id, upsell_product_id)
);

CREATE INDEX IF NOT EXISTS idx_product_upsells_product ON product_upsells(product_id);
