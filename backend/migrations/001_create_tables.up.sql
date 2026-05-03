-- BKL-145: LinkPay — tabelas para produtos digitais e compras

CREATE TABLE IF NOT EXISTS digital_products (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id     UUID NOT NULL,
  slug          VARCHAR(100) NOT NULL,
  name          VARCHAR(255) NOT NULL,
  description   TEXT,
  price_cents   INTEGER NOT NULL CHECK (price_cents > 0),
  currency      VARCHAR(3) NOT NULL DEFAULT 'BRL',
  product_type  VARCHAR(20) NOT NULL DEFAULT 'digital',
  file_url      TEXT,
  download_limit INTEGER NOT NULL DEFAULT 5,
  is_active     BOOLEAN NOT NULL DEFAULT TRUE,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (tenant_id, slug)
);

CREATE TABLE IF NOT EXISTS digital_purchases (
  id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  product_id         UUID NOT NULL REFERENCES digital_products(id),
  tenant_id          UUID NOT NULL,
  customer_email     VARCHAR(255) NOT NULL,
  customer_name      VARCHAR(255) NOT NULL,
  customer_cpf       VARCHAR(14),
  amount_cents       INTEGER NOT NULL,
  payment_method     VARCHAR(20) NOT NULL DEFAULT 'pix',
  payment_status     VARCHAR(20) NOT NULL DEFAULT 'pending',
  external_charge_id VARCHAR(255),
  access_token       VARCHAR(64) UNIQUE,
  access_expires_at  TIMESTAMPTZ,
  download_count     INTEGER NOT NULL DEFAULT 0,
  created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_digital_products_tenant ON digital_products(tenant_id);
CREATE INDEX IF NOT EXISTS idx_digital_products_slug   ON digital_products(slug);
CREATE INDEX IF NOT EXISTS idx_digital_purchases_product ON digital_purchases(product_id);
CREATE INDEX IF NOT EXISTS idx_digital_purchases_token   ON digital_purchases(access_token);
CREATE INDEX IF NOT EXISTS idx_digital_purchases_email   ON digital_purchases(customer_email);
