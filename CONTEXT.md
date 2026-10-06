# linkpay-service — Context

## Propósito

Plataforma de venda de produtos digitais (infoprodutos) do ecossistema IIT — concorrente direto de Hotmart, Kiwify e Eduzz. Gerencia produtos digitais, landing pages públicas por slug, cobranças PIX/cartão via gateway, armazenamento de arquivos, repasses automáticos ao criador (payout) e assinaturas recorrentes. Extração autônoma do código LinkPay que vivia dentro do checkout-service ([ADR-003](../instituto-itinerante-ecosystem/docs/adr/ADR-003-linkpay-standalone.md)).

> **Nota de conformidade (STD-007):** integração direta com o gateway Asaas (clientes, cobranças, transferências) é uma **exceção documentada e aprovada** em `instituto-itinerante-ecosystem/docs/STANDARDS.md` (STD-007), amparada pelo ADR-003. Não é violação — mas o ADR-003 original previa o linkpay-service chamando o checkout-service como dependência externa para abrir cobranças; na prática o serviço evoluiu para integrar o Asaas diretamente (`internal/service/asaas_client.go`). Essa divergência entre o texto do ADR e a implementação real não está reconciliada em nenhum documento — **pendência de decisão do Antonio**: atualizar o ADR-003 para refletir a integração direta, ou tratar como desvio a corrigir.

## Stack

| Camada | Tecnologia |
|--------|-----------|
| Backend | Go 1.24 + Fiber v2 |
| Banco | PostgreSQL 15 — banco `linkpay_db` (192.168.30.121) |
| Storage | MinIO — bucket `linkpay` |
| Mensageria | Webhooks Asaas (inbound) + RabbitMQ (outbound, exchange `iit.events` — ex: `linkpay.purchase.confirmed` consumido pelo notification-service) |
| Cache/Idempotência | Redis (idempotência de webhook via `SetNX`, rate limiting) |
| Autenticação | JWT via auth-service (golang-jwt/v5) |
| Observabilidade | OpenTelemetry + Prometheus (`internal/telemetry/`) — implementado |
| Deploy | K3s — namespace `production`, ArgoCD auto-sync |
| Domínio público | `pay.institutoitinerante.com.br` |

## Linguagem ubíqua

| Termo | Definição |
|-------|-----------|
| **DigitalProduct** | Produto digital cadastrado por um criador; possui `slug` único, preço, `platform_commission_pct` e arquivos associados no MinIO |
| **DigitalPurchase** | Registro de uma compra concluída; vinculado a um `external_charge_id` (ID da cobrança no Asaas) e a um `download_token` de acesso |
| **Slug** | Identificador textual único do produto; define a URL pública da landing page (`GET /:slug`) |
| **DownloadToken** | Token de acesso aos arquivos do produto; gerado após pagamento confirmado, com `access_expires_at` |
| **CreatorPayout** | Repasse do saldo do criador via Asaas Transfers; pode ser automático (schedule) ou sob demanda |
| **CreatorConfig** | Configuração de payout por tenant: `pix_key`, `payout_schedule`; substitui as colunas removidas de `tenants` no checkout-service |
| **DigitalProductFile** | Arquivo individual associado a um produto (PDF, MP4, ZIP etc.); armazenado no MinIO por `storage_key` |
| **CommissionSplit** | Divisão da receita: `platform_commission_pct` (padrão 30%) para a plataforma, restante para o criador |
| **Anticipation** | Antecipação de recebíveis D+2 com taxa de 2%; solicitada pelo criador via dashboard |
| **DigitalSubscription** | Assinatura recorrente de plano digital; estados `ACTIVE`, `CANCELLED`, `SUSPENDED` |
| **WebhookEvent** | Evento Asaas recebido (`PAYMENT_CONFIRMED`, `TRANSFER_COMPLETED`, `SUBSCRIPTION_DELETED`) |

## Limites (bounded context)

- **Possui:** `digital_products`, `digital_purchases`, `digital_product_files`, `creator_payouts`, `creator_configs`, `digital_subscription_plans`, `digital_subscriptions`, `product_upsells`, `commission_ledger`; cálculo e processamento de cobranças PIX/cartão **diretamente via Asaas** (exceção STD-007, ver nota de conformidade acima); geração e validação de download tokens; upload/download de arquivos no MinIO (bucket `linkpay`)
- **NÃO possui:** identidade/JWT (auth-service); perfil de cliente (customer-service); emissão de NF-e (billing-service); envio de email (notification-service, chamado via evento RabbitMQ ou fallback HTTP); tabela `tenants` global (shared com checkout-service, não alterada)
- **Upstream:** `auth-service` (validar JWT do criador), `notification-service` (email de confirmação + link de download — evento RabbitMQ com fallback HTTP direto), `cart-service` (criação de carrinho para upsell, `POST /api/v1/public/products/:slug/cart`), MinIO, Redis, Asaas (customers, cobranças, transferências e cancelamento de assinatura — integração direta via `internal/service/asaas_client.go`)
- **Downstream:** nenhum serviço IIT consome diretamente a API do linkpay-service; o evento `linkpay.purchase.confirmed` (exchange `iit.events`) é consumido pelo notification-service. O frontend público é a própria landing page SSR do serviço (`GET /:slug`); não há SPA dedicada identificada neste repo.
- **Configuração morta confirmada:** `deploy/k8s/configmap.yaml` define `CHECKOUT_SERVICE_URL` e `CUSTOMER_SERVICE_URL`, mas nenhum dos dois é lido em código Go (`grep` sem resultado) — sobra de uma integração planejada e nunca implementada, ou de uma removida sem limpar o manifesto. Não removido nesta auditoria (fora do escopo de docs); reportar ao time de backend/devops.

## Mapa de módulos

| Caminho | Responsabilidade |
|---------|-----------------|
| `backend/cmd/server/main.go` | Bootstrap: config, DB, MinIO, Redis, telemetry, rotas, job diário de payout, graceful shutdown |
| `backend/internal/handler/linkpay_handler.go` | Endpoints público (landing page, checkout, download, ZIP) + admin (CRUD produtos, upload, stats, commission-summary) + webhook Asaas |
| `backend/internal/handler/payout_handler.go` | Endpoints de saldo, listagem, solicitação de repasse e antecipação |
| `backend/internal/handler/subscription_handler.go` | Endpoints de planos e assinaturas recorrentes |
| `backend/internal/handler/public_page.go` | Landing page pública SSR (`GET /:slug`) com OG tags |
| `backend/internal/service/linkpay_service.go` | Regras de negócio: criar produto, processar compra, calcular comissão, gerar download token, commission ledger |
| `backend/internal/service/payout_service.go` | Lógica de repasse: calcular saldo, acionar Asaas Transfers, registrar payout, antecipação D+2 |
| `backend/internal/service/subscription_service.go` | Assinaturas recorrentes: criar, cancelar, processar renovação via webhook |
| `backend/internal/service/asaas_client.go` | Cliente HTTP direto para a API Asaas (customers, cobranças, transferências, cancelamento) — ver nota de conformidade STD-007 acima |
| `backend/internal/repository/` | Persistência: `linkpay_repository.go`, `payout_repository.go`, `subscription_repository.go` |
| `backend/internal/client/cart_client.go` | HTTP client para cart-service (criar carrinho de upsell) |
| `backend/internal/client/notification_client.go` | HTTP client de fallback para notification-service (se publicação no RabbitMQ falhar) |
| `backend/internal/storage/minio_client.go` | Upload/download/presigned URLs no bucket `linkpay` |
| `backend/internal/telemetry/` | OpenTelemetry + Prometheus |
| `backend/migrations/` | 10 migrations (001–010): produtos, comissão, storage, creator_payouts, assinaturas, antecipação, creator_configs, upsells, cover_image_url, commission ledger |

## Pontos de entrada

| Tipo | Rota / Local |
|------|-------------|
| Servidor HTTP | `backend/cmd/server/main.go` → `app.Listen(:3020)` |
| Landing page pública (SSR) | `GET /:slug` |
| Checkout público | `POST /api/v1/public/products/:slug/purchase` — cria cobrança direto no Asaas |
| Download | `GET /api/v1/public/download/:token`, `.../file/:fileId`, `.../zip` |
| Assinaturas públicas | `POST /api/v1/public/products/:id/subscribe`, `GET .../subscriptions/:token`, `POST .../subscriptions/:token/cancel` |
| Reembolso | `POST /api/v1/public/purchases/:id/refund` (CDC Art. 49) |
| Webhook Asaas | `POST /api/v1/linkpay/webhook` (PAYMENT_CONFIRMED/RECEIVED, PAYMENT_OVERDUE, TRANSFER_COMPLETED/FAILED, SUBSCRIPTION_DELETED) |
| Criador (JWT) | `GET/POST/PUT/DELETE /api/v1/linkpay/products`, upload, cover, planos |
| Admin (JWT) | `GET /api/v1/admin/linkpay/stats`, `/balance`, `/payouts`, `/anticipation/*`, `/commission-summary` |
| Health check | `GET /health` |
| Swagger UI | `GET /swagger/*` |
