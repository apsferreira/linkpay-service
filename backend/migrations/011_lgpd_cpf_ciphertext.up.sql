-- =============================================================================
-- MIGRATION 011: LGPD Art. 46 — CPF cifrado em repouso (AES-256-GCM)
-- =============================================================================
-- customer_cpf é dado pessoal (LGPD Art. 5º, I) com tratamento especial. A
-- partir desta migration a coluna passa a guardar APENAS ciphertext AES-256-GCM
-- no formato hex(nonce || ciphertext) (nonce de 12 bytes), produzido pela
-- aplicação em internal/pkg/crypto com a chave ENCRYPTION_KEY — mesmo padrão do
-- customer-service (migrations 013/019).
--
-- Valores legados em plaintext (VARCHAR(14), sempre < 64 chars) são
-- anonimizados (NULL): não é possível cifrá-los via SQL sem a chave da
-- aplicação, e mantê-los em claro viola o Art. 46. Mesmo tratamento dado pelo
-- customer-service (migration 019): o risco de exposição via dump supera a
-- perda do dado. O CPF é opcional no checkout e não é usado em consultas
-- internas após a criação da cobrança no Asaas (gateway guarda o dado).
-- Ciphertexts hex(nonce||ct) de CPF (11-14 chars) têm sempre >= 78 chars.

ALTER TABLE digital_purchases     ALTER COLUMN customer_cpf TYPE TEXT;
ALTER TABLE digital_subscriptions ALTER COLUMN customer_cpf TYPE TEXT;

UPDATE digital_purchases SET customer_cpf = NULL
 WHERE customer_cpf IS NOT NULL AND LENGTH(customer_cpf) < 64;
UPDATE digital_subscriptions SET customer_cpf = NULL
 WHERE customer_cpf IS NOT NULL AND LENGTH(customer_cpf) < 64;

-- Anonimização (LGPD Art. 18, VI e retenção Art. 16) zera nome/email do titular;
-- as colunas precisam aceitar NULL para os registros financeiros permanecerem.
ALTER TABLE digital_purchases     ALTER COLUMN customer_email DROP NOT NULL;
ALTER TABLE digital_purchases     ALTER COLUMN customer_name DROP NOT NULL;
ALTER TABLE digital_subscriptions ALTER COLUMN customer_email DROP NOT NULL;
ALTER TABLE digital_subscriptions ALTER COLUMN customer_name DROP NOT NULL;

-- Índice para os endpoints LGPD Art. 18 (busca por email do titular).
CREATE INDEX IF NOT EXISTS idx_subscriptions_email ON digital_subscriptions(customer_email);

COMMENT ON COLUMN digital_purchases.customer_cpf IS 'LGPD Art. 46: ciphertext AES-256-GCM hex(nonce||ciphertext) — nunca armazenar plaintext. NULL = titular não forneceu ou dado anonimizado.';
COMMENT ON COLUMN digital_subscriptions.customer_cpf IS 'LGPD Art. 46: ciphertext AES-256-GCM hex(nonce||ciphertext) — nunca armazenar plaintext. NULL = titular não forneceu ou dado anonimizado.';
COMMENT ON COLUMN digital_purchases.customer_email IS 'NULL após anonimização (LGPD Art. 18, VI / retenção Art. 16).';
COMMENT ON COLUMN digital_purchases.customer_name IS 'NULL após anonimização (LGPD Art. 18, VI / retenção Art. 16).';
COMMENT ON COLUMN digital_subscriptions.customer_email IS 'NULL após anonimização (LGPD Art. 18, VI / retenção Art. 16).';
COMMENT ON COLUMN digital_subscriptions.customer_name IS 'NULL após anonimização (LGPD Art. 18, VI / retenção Art. 16).';
