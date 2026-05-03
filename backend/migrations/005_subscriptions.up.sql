-- BKL-1208: Assinaturas LinkPay com múltiplos planos (A/B/C)
CREATE TABLE IF NOT EXISTS digital_subscription_plans (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  product_id UUID NOT NULL REFERENCES digital_products(id) ON DELETE CASCADE,
  tenant_id UUID NOT NULL,
  name VARCHAR(100) NOT NULL,
  slug VARCHAR(50) NOT NULL,
  description TEXT,
  price_cents INT NOT NULL CHECK (price_cents > 0),
  interval VARCHAR(20) NOT NULL DEFAULT 'monthly' CHECK (interval IN ('monthly','yearly')),
  trial_days INT NOT NULL DEFAULT 0,
  is_active BOOLEAN NOT NULL DEFAULT TRUE,
  sort_order INT NOT NULL DEFAULT 0,
  features JSONB NOT NULL DEFAULT '[]',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (product_id, slug)
);

CREATE TABLE IF NOT EXISTS digital_subscriptions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  plan_id UUID NOT NULL REFERENCES digital_subscription_plans(id),
  tenant_id UUID NOT NULL,
  customer_email VARCHAR(255) NOT NULL,
  customer_name VARCHAR(255) NOT NULL,
  customer_cpf VARCHAR(14),
  asaas_subscription_id VARCHAR(100),
  asaas_customer_id VARCHAR(100),
  status VARCHAR(20) NOT NULL DEFAULT 'active'
    CHECK (status IN ('active','trialing','past_due','cancelled','expired')),
  current_period_start TIMESTAMPTZ,
  current_period_end TIMESTAMPTZ,
  cancelled_at TIMESTAMPTZ,
  access_token VARCHAR(64) UNIQUE NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_sub_plans_product ON digital_subscription_plans(product_id);
CREATE INDEX IF NOT EXISTS idx_subscriptions_token ON digital_subscriptions(access_token);
CREATE INDEX IF NOT EXISTS idx_subscriptions_tenant ON digital_subscriptions(tenant_id, status);
CREATE INDEX IF NOT EXISTS idx_subscriptions_asaas ON digital_subscriptions(asaas_subscription_id)
  WHERE asaas_subscription_id IS NOT NULL;
