# linkpay-service

Serviço de pagamentos e repasses do ecossistema Instituto Itinerante. Integra com Asaas para geração de links de pagamento, gerenciamento de assinaturas e repasses automáticos para criadores de conteúdo. Consome e publica eventos via RabbitMQ.

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

# Aplicar migrations
psql $DATABASE_URL -f backend/migrations/001_create_tables.up.sql
psql $DATABASE_URL -f backend/migrations/002_add_commission.up.sql
psql $DATABASE_URL -f backend/migrations/003_add_storage.up.sql
psql $DATABASE_URL -f backend/migrations/004_creator_payouts.up.sql
psql $DATABASE_URL -f backend/migrations/005_subscriptions.up.sql
psql $DATABASE_URL -f backend/migrations/006_anticipation.up.sql
psql $DATABASE_URL -f backend/migrations/007_creator_configs.up.sql

make dev
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
| `RABBITMQ_URL` | Não | AMQP URL para eventos |
| `NOTIFICATION_SERVICE_URL` | Não | URL do notification-service |
| `SERVICE_TOKEN` | Não | Token para chamadas inter-servico |
| `BASE_URL` | Não | URL pública do serviço |
| `PORT` | Não | Porta HTTP (default: `3020`) |

## Endpoints principais

| Método | Path | Auth | Descrição |
|--------|------|------|-----------|
| GET | `/health` | — | Health check |
| POST | `/api/v1/links` | JWT | Gerar link de pagamento |
| GET | `/api/v1/links/:id` | JWT | Consultar link |
| POST | `/api/v1/webhooks/asaas` | HMAC | Receber webhook Asaas |
| GET | `/api/v1/subscriptions` | JWT | Listar assinaturas |
| POST | `/api/v1/subscriptions` | JWT | Criar assinatura |
| GET | `/api/v1/payouts` | JWT | Listar repasses |
| POST | `/api/v1/payouts/:id/process` | SERVICE | Processar repasse |

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
│   │   └── storage/             # MinIO client
│   ├── pkg/jwt/                 # JWT helpers
│   └── migrations/              # SQL migrations sequenciais
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
