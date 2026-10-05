# linkpay-service

Plataforma de venda de produtos digitais (infoprodutos) do ecossistema Instituto Itinerante — concorrente direto de Hotmart/Kiwify/Eduzz. Produtos cadastrados por criadores com landing page pública por slug, cobrança PIX/cartão via integração direta com Asaas (exceção documentada STD-007/ADR-003 — ver `CONTEXT.md`), repasse automático (payout) e assinaturas recorrentes. Consome e publica eventos via RabbitMQ.

## Stack

- **Backend:** Go 1.24 + Fiber v2 + pgx/v5
- **Pagamentos:** Asaas (links de pagamento, assinaturas, webhooks)
- **Storage:** MinIO (comprovantes e documentos)
- **Mensageria:** RabbitMQ
- **Banco:** PostgreSQL 15
- **Deploy:** K3s (ArgoCD GitOps) via GHCR

## Rodar localmente

```bash
# Pre-requisito: shared-infra rodando
cd ../shared-infra && docker compose up -d

cd linkpay-service
cp .env.example .env
# editar .env com ASAAS_API_KEY e demais secrets

# Aplicar migrations (aplica todo backend/migrations/*.up.sql em ordem)
make migrate

make run
```

## Variáveis de ambiente

| Variável | Obrigatória | Descrição |
|----------|-------------|-----------|
| `DATABASE_URL` | Sim | PostgreSQL connection string |
| `JWT_SECRET` | Sim | Secret JWT (mesmo do auth-service) |
| `ASAAS_API_KEY` | Sim | Chave API Asaas |
| `ASAAS_BASE_URL` | Sim | Base URL Asaas (`https://sandbox.asaas.com/api/v3` em homolog) |
| `ASAAS_WEBHOOK_SECRET` | Sim | Secret para validar webhooks Asaas |
| `ASAAS_WEBHOOK_MAX_AGE_SECONDS` | Não | Janela (s) do timestamp do webhook Asaas p/ anti-replay (default: `300` = 5 min) |
| `MINIO_ENDPOINT` | Sim | Endpoint MinIO |
| `MINIO_ACCESS_KEY` | Sim | Access key MinIO |
| `MINIO_SECRET_KEY` | Sim | Secret key MinIO |
| `REDIS_URL` | Não (default `redis://localhost:6379/0`) | Idempotência de webhook (`SetNX`) e rate limiting |
| `RABBITMQ_URL` | Não | AMQP URL para eventos |
| `NOTIFICATION_SERVICE_URL` | Não | URL do notification-service (fallback se publish no RabbitMQ falhar) |
| `SERVICE_TOKEN` | Não | Token para chamadas inter-servico |
| `BASE_URL` | Não | URL pública do serviço |
| `CORS_ORIGINS` | Não (default `http://localhost:5173`) | Origens permitidas via CORS |
| `PORT` | Não | Porta HTTP (default: `3020`) |

## Endpoints principais

| Método | Path | Auth | Descrição |
|--------|------|------|-----------|
| GET | `/health` | — | Health check |
| GET | `/:slug` | — | Landing page pública do produto (SSR + OG tags) |
| GET | `/api/v1/public/products/:slug` | — | Dados públicos do produto |
| POST | `/api/v1/public/products/:slug/purchase` | — | Iniciar compra (cobrança Asaas) |
| GET | `/api/v1/public/download/:token` | — | Download do produto |
| GET | `/api/v1/public/download/:token/zip` | — | Download de todos os arquivos (ZIP) |
| POST | `/api/v1/public/products/:id/subscribe` | — | Assinar plano recorrente |
| POST | `/api/v1/public/purchases/:id/refund` | — | Reembolso (CDC Art. 49) |
| POST | `/api/v1/linkpay/webhook` | HMAC | Receber webhook Asaas |
| GET/POST/PUT/DELETE | `/api/v1/linkpay/products` | JWT | CRUD de produtos do criador |
| GET | `/api/v1/admin/linkpay/balance`, `/payouts` | JWT | Saldo e repasses |
| POST | `/api/v1/admin/linkpay/payouts/request` | JWT | Solicitar repasse |
| GET | `/api/v1/admin/linkpay/commission-summary` | JWT | Ledger de comissão (auditoria financeira) |

## Estrutura

```
linkpay-service/
├── backend/
│   ├── cmd/server/main.go       # Entry point
│   ├── internal/
│   │   ├── domain/              # Models
│   │   ├── handler/             # HTTP handlers (linkpay, payout, subscription)
│   │   ├── service/             # Logica de negocio
│   │   ├── repository/          # Acesso ao banco
│   │   ├── messaging/           # Publisher RabbitMQ
│   │   ├── middleware/          # Auth, rate-limit, security headers
│   │   ├── storage/             # MinIO client
│   │   ├── client/              # Clients HTTP p/ cart-service e notification-service
│   │   └── telemetry/           # OpenTelemetry + Prometheus
│   ├── pkg/jwt/                 # JWT helpers
│   └── migrations/              # SQL migrations sequenciais (001–010)
├── Makefile
├── Dockerfile
└── .env.example
```

## Producao (K3s)

| Recurso | Valor |
|---------|-------|
| URL pública | `https://pay.institutoitinerante.com.br` |
| Namespace K8s | `production` |
| Imagem | `ghcr.io/apsferreira/linkpay-service` |

```bash
KUBECONFIG=~/.kube/k3s-homelab kubectl logs -n production deploy/linkpay-service -f
```
