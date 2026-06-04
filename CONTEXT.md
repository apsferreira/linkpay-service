# linkpay-service — Context

## Propósito

Plataforma de venda de produtos digitais (infoprodutos) do ecossistema IIT — concorrente direto de Hotmart, Kiwify e Eduzz. Gerencia produtos digitais, landing pages públicas por slug, cobranças PIX/cartão via gateway, armazenamento de arquivos, repasses automáticos ao criador (payout) e assinaturas recorrentes. Extração autônoma do código LinkPay que vivia dentro do checkout-service (ADR-003).

## Stack

| Camada | Tecnologia |
|--------|-----------|
| Backend | Go 1.24 + Fiber v2 |
| Banco | PostgreSQL 15 — banco `linkpay_db` (192.168.30.121) |
| Storage | MinIO — bucket `linkpay` |
| Mensageria | Webhooks Asaas (inbound) |
| Autenticação | JWT via auth-service (golang-jwt/v5) |
| Observabilidade | OpenTelemetry (obrigatório em prod) |
| Deploy | K3s — namespace `production`, ArgoCD auto-sync |
| Domínio público | `linkpay-api.institutoitinerante.com.br` |

## Linguagem ubíqua

| Termo | Definição |
|-------|-----------|
| **DigitalProduct** | Produto digital cadastrado por um criador; possui `slug` único, preço, `platform_commission_pct` e arquivos associados no MinIO |
| **DigitalPurchase** | Registro de uma compra concluída; vinculado a um `charge_id` do checkout-service e a um `download_token` de acesso |
| **Slug** | Identificador textual único do produto; define a URL pública da landing page (`/l/:slug`) |
| **DownloadToken** | Token de acesso aos arquivos do produto; gerado após pagamento confirmado, com `access_expires_at` |
| **CreatorPayout** | Repasse do saldo do criador via Asaas Transfers; pode ser automático (schedule) ou sob demanda |
| **CreatorConfig** | Configuração de payout por tenant: `pix_key`, `payout_schedule`; substitui as colunas removidas de `tenants` no checkout-service |
| **DigitalProductFile** | Arquivo individual associado a um produto (PDF, MP4, ZIP etc.); armazenado no MinIO por `storage_key` |
| **CommissionSplit** | Divisão da receita: `platform_commission_pct` (padrão 30%) para a plataforma, restante para o criador |
| **Anticipation** | Antecipação de recebíveis D+2 com taxa de 2%; solicitada pelo criador via dashboard |
| **DigitalSubscription** | Assinatura recorrente de plano digital; estados `ACTIVE`, `CANCELLED`, `SUSPENDED` |
| **WebhookEvent** | Evento Asaas recebido (`PAYMENT_CONFIRMED`, `TRANSFER_COMPLETED`, `SUBSCRIPTION_DELETED`) |

## Limites (bounded context)

- **Possui:** `digital_products`, `digital_purchases`, `digital_product_files`, `creator_payouts`, `creator_configs`, `digital_subscription_plans`, `digital_subscriptions`; cálculo de comissão; geração e validação de download tokens; upload/download de arquivos no MinIO (bucket `linkpay`)
- **NÃO possui:** processamento de cobranças PIX/cartão (checkout-service); identidade/JWT (auth-service); perfil de cliente (customer-service); emissão de NF-e (billing-service); notificações por email (notification-service); tabela `tenants` global (shared com checkout-service, não alterada)
- **Upstream:** `checkout-service` (criar cobranças via HTTP interno), `auth-service` (validar JWT do criador), `notification-service` (email de confirmação + link de download), MinIO, Asaas (webhooks inbound e Transfers API)
- **Downstream:** nenhum serviço IIT consome diretamente o linkpay-service; o frontend LinkPay (`iit-ui` ou SPA dedicada) consome sua API pública

## Mapa de módulos

| Caminho | Responsabilidade |
|---------|-----------------|
| `backend/cmd/api/main.go` | Bootstrap: config, DB, MinIO, rotas, graceful shutdown |
| `backend/internal/handlers/linkpay_handler.go` | Endpoints público (landing page, checkout, download, ZIP) + admin (CRUD produtos, upload, stats) + webhook Asaas |
| `backend/internal/handlers/payout_handler.go` | Endpoints de saldo, listagem e solicitação de repasse |
| `backend/internal/services/linkpay_service.go` | Regras de negócio: criar produto, processar compra, calcular comissão, gerar download token |
| `backend/internal/services/payout_service.go` | Lógica de repasse: calcular saldo, acionar Asaas Transfers, registrar payout |
| `backend/internal/services/subscription_service.go` | Assinaturas recorrentes: criar, cancelar, processar renovação via webhook |
| `backend/internal/repositories/` | Persistência: `linkpay_repository.go`, `payout_repository.go`, `subscription_repository.go` |
| `backend/internal/clients/checkout_client.go` | HTTP client para checkout-service (criar cobranças internas) |
| `backend/internal/clients/minio_client.go` | Upload/download/presigned URLs no bucket `linkpay` |
| `backend/migrations/` | 6 migrations (001–006): produtos, comissão, storage, creator_configs, planos, antecipação |

## Pontos de entrada

| Tipo | Rota / Local |
|------|-------------|
| Servidor HTTP | `backend/cmd/api/main.go` → `app.Listen(:3020)` |
| Landing page pública | `GET /l/:slug` |
| Checkout público | `POST /l/:slug/checkout` — retorna PIX QR code via checkout-service |
| Download | `GET /download/:token`, `GET /download/:token/file/:fileId`, `GET /download/:token/zip` |
| Webhook Asaas | `POST /webhook` (PAYMENT_CONFIRMED, TRANSFER_COMPLETED, SUBSCRIPTION_DELETED) |
| Admin (JWT criador) | `GET/POST/PUT/DELETE /api/v1/linkpay/products`, upload, stats, balance, payouts |
| Health check | `GET /internal/health` (liveness probe K3s) |
