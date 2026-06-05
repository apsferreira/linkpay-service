# Lente 01 — Produto
## Análise Estratégica | linkpay-service

Data: 2026-06-05 | PM: SOUL — Instituto Itinerante

---

## 1. PLANEJADO

O **linkpay-service** se posiciona como a **plataforma unificada de monetização de conteúdo do ecossistema IIT**, permitindo que criadores de conteúdo (pessoas físicas responsáveis por produtos digitais em meu-library, focus-hub, socialmake, etc.) ganhem dinheiro através de:

### 1.1 Núcleo de Negócio (BKL-145 a BKL-1221)

1. **Produtos Digitais** — Armazenar, precificar (BRL) e vender ebooks, cursos, templates, workshops
   - Suporte a múltiplos formatos de arquivo (MinIO S3-compatível)
   - Limite de downloads por comprador
   - Slug único por tenant para rota pública
   - Comissão configurável (padrão: 10% IIT, 90% criador)

2. **Compras Avulsas** — Links de pagamento único gerados via Asaas
   - Métodos: PIX (D+2), Boleto (D+2), Cartão de crédito (D+15)
   - Holdback anti-chargeback (10% reservado 30 dias)
   - Access token com expiração para download
   - Rastreamento de compras por email/CPF

3. **Assinaturas Recorrentes** — Planos mensais/anuais (A/B/C) vinculados a produtos
   - Trial configurável (até 90 dias)
   - Renovação automática via Asaas
   - Acesso de longa duração (não expira como compra avulsa)
   - Webhooks de ciclo de vida (ativação, cancelamento, vencimento)

4. **Repasses Automáticos (Payouts)** — Job diário que transfere saldo para criadores
   - Processamento automático às 06h BRT se saldo >= R$10
   - Método: Transferência PIX via Asaas
   - Rastreamento por `AsaasTransferID`
   - Suporte a antecipação (taxa 2%, disponibilidade D+2 vs D+15)

5. **Analytics & Monetização Avançada**
   - Dashboard de vendas (período, total, breakdown por produto)
   - Preview de elegibilidade para antecipação
   - Upsells/cross-sells (BKL-1221)
   - Tracking via Pulse (fire-and-forget)

### 1.2 Arquitetura Planejada

- **Backend:** Go 1.24 + Fiber v2 (idêntico a outros serviços IIT)
- **Persistência:** PostgreSQL 15 (8 migrations sequenciais)
- **Storage:** MinIO (bucket por tenant, organização por produto)
- **Pagamentos:** Integração full-stack Asaas (webhooks HMAC)
- **Eventos:** RabbitMQ (publicação de compras confirmadas)
- **Autenticação:** JWT (compartilhado com auth-service, secret="***REDACTED-SEE-TRUENAS-credentials.txt***")
- **Observabilidade:** Pulse (evento fire-and-forget), slog em JSON
- **Docs:** Swagger OpenAPI v1.0

### 1.3 Modelo de Negócio Implícito

- **Receita IIT:** 10% de toda compra (configurável por tenant, permitindo 15-20% para SocialMake)
- **Custos IIT:** Taxa Asaas (~2% + antecipação 2%), armazenamento MinIO (~$0.02/GB/mês)
- **Ciclo de caixa:** D+2 a D+15 em holdback (vantagem de cash flow)
- **Escala:** Multi-tenant com isolamento de dados por `tenant_id`

---

## 2. CONSTRUÍDO

### 2.1 Cobertura Entregue (5940 linhas de Go, 8 migrations aplicadas)

| Feature | Status | Evidence |
|---------|--------|----------|
| Produtos digitais CRUD | ✅ Completo | handler + service + repository + domain model |
| Múltiplos arquivos por produto | ✅ Completo | `digital_product_files` table (BKL-1206) |
| Compras avulsas + Asaas | ✅ Completo | `linkpay_handler.InitiatePurchase` → Asaas chargeID + PIX/Boleto/Card |
| Download com token temporário | ✅ Completo | `GetPurchaseByToken` + `access_expires_at` |
| Assinaturas recorrentes | ✅ Completo | `DigitalSubscriptionPlan` + `DigitalSubscription` + Asaas sync |
| Repasses diários automáticos | ✅ Completo | `PayoutService.RunDailyPayouts()` + job scheduling (não codificado, delegado ao K8s CronJob) |
| Antecipação de recebíveis | ✅ Completo | `GetAnticipationEligiblePurchases` + 2% fee logic |
| Webhooks Asaas | ✅ Completo | `LinkPayHandler.HandleWebhook()` com HMAC validation (VUL-001) |
| Analytics básicas | ✅ Completo | `GetSalesStats` (vendas + receita por período) |
| Upsells/cross-sells | ✅ Completo | `product_upsells` table + `GetUpsellsByProductID` (BKL-1221) |
| Integração cart-service | ✅ Parcial | `CartClient` para enviar upsells, *sem testes de integração* |
| Pulse tracking | ✅ Parcial | Fire-and-forget de eventos (não rastreável) |
| RabbitMQ publisher | ✅ Parcial | Publisher criado, mas nenhuma fila configurada (topic não validado) |

### 2.2 Endpoints Implementados

**Públicas** (sem autenticação):
- `GET /api/v1/public/products/:slug` — produto público
- `POST /api/v1/public/products/:slug/purchase` — inicia compra
- `GET /api/v1/public/download/:token` — download principal
- `GET /api/v1/public/download/:token/file/:fileId` — download arquivo específico
- `GET /api/v1/public/download/:token/zip` — download múltiplos arquivos em ZIP
- `POST /api/v1/public/products/:slug/cart` — cria cart com upsells (BKL-1221)
- `POST /api/v1/public/subscriptions/:token/cancel` — cancela assinatura

**Autenticadas** (JWT):
- `POST /api/v1/linkpay/products` — criar
- `GET /api/v1/linkpay/products` — listar
- `GET /api/v1/linkpay/products/:id` — detalhe
- `PUT /api/v1/linkpay/products/:id` — atualizar
- `DELETE /api/v1/linkpay/products/:id` — desativar
- `POST /api/v1/linkpay/products/:id/upload` — upload arquivo
- `GET /api/v1/linkpay/products/:id/files` — listar arquivos
- `DELETE /api/v1/linkpay/products/:id/files/:fileId` — deletar arquivo
- Planos: `POST/GET/PUT/DELETE /api/v1/linkpay/products/:id/plans/:plan_id`

**Admin** (JWT):
- `GET /api/v1/admin/linkpay/stats` — vendas + receita
- `GET /api/v1/admin/linkpay/balance` — saldo disponível
- `GET /api/v1/admin/linkpay/payouts` — histórico de repasses (paginado)
- `POST /api/v1/admin/linkpay/payouts/request` — saque manual
- `GET /api/v1/admin/linkpay/anticipation/preview` — quanto pode antecipar
- `POST /api/v1/admin/linkpay/anticipation/request` — solicita antecipação

### 2.3 Banco de Dados (8 migrations)

| Migration | Tabelas | Propósito |
|-----------|---------|-----------|
| 001 | `digital_products`, `digital_purchases` | Produtos e compras avulsas |
| 002 | `digital_products` (ALTER) | Coluna `platform_commission_pct` |
| 003 | `digital_products` (ALTER) | Colunas MinIO: `storage_key`, `storage_bucket`, `file_name`, `file_size_bytes`, `mime_type` |
| 004 | `creator_payouts` | Repasses (status, asaas_transfer_id, scheduled_for) |
| 005 | `subscription_plans`, `subscriptions` | Planos e assinaturas recorrentes |
| 006 | `creator_payouts` (ALTER) | Colunas antecipação: `is_anticipation`, `anticipation_fee_cents` |
| 007 | `creator_configs` | Config por tenant (pix_key, payout_method) |
| 008 | `product_upsells` | Sugestões de venda cruzada (BKL-1221) |

### 2.4 Testes

**Cobertura atual:** 136 linhas de testes (~2.3% do código)
- `product_test.go` — 71 linhas (2 testes de comissão e holdback)
- `holdback_test.go` — 65 linhas (5 testes de cálculo de reserve)
- **Apenas testes unitários de domain** (sem integração, sem handler, sem repository)
- **Zero testes** para Asaas, PayoutService, SubscriptionService, uploads MinIO, webhooks

### 2.5 Qualidade de Implementação

**Positivos:**
- ✅ Separação clara domain/handler/service/repository (padrão idêntico aos outros serviços IIT)
- ✅ Type safety com structs de domain models
- ✅ Middleware JWT + rate-limit + security headers
- ✅ Validação de entrada com `go-playground/validator`
- ✅ Logging com slog em JSON estruturado
- ✅ HMAC validation nos webhooks Asaas (VUL-001)
- ✅ Holdback anti-chargeback implementado nos cálculos
- ✅ Isolation de tenant via FK em todas as tabelas
- ✅ Multi-arquivo por produto com storage_key único

**Gaps críticos:**
- ❌ **Zero testes de integração** — não há verificação se Asaas real/fake retorna o esperado
- ❌ **RabbitMQ publisher não testado** — assume que Publisher é injetado e funciona, mas nenhum teste valida
- ❌ **Erro handling inconsistente** — alguns endpoints retornam `fiber.StatusInternalServerError` genérico sem rastreamento
- ❌ **Job diário não está no código** — `RunDailyPayouts` existe, mas quem chama? K8s CronJob? Não documentado
- ❌ **Asaas mock não existe** — testes dependem de stub do cliente, não do comportamento real
- ❌ **Upload de arquivo sem validação MIME** — aceita qualquer tipo antes de enviar para MinIO
- ❌ **Sem circuit breaker em Asaas** — timeout configurado mas sem retry/fallback
- ❌ **Sem testes de segurança** — HMAC validation não tem teste unitário
- ❌ **SQL injection risk mínimo mas presente** — repository usa pgx parameterizado (bom), mas sem testes
- ❌ **Sem observabilidade em falhas** — erros em Asaas/MinIO não propagam rastreamento de request

---

## 3. GAP (Planejado vs Construído)

### 3.1 Funcionalidades Implementadas, Não Validadas

| Feature | Crítica? | Impacto |
|---------|----------|--------|
| Webhooks Asaas | ALTA | Se falhar silenciosamente, compras não se confirmam |
| RabbitMQ event publishing | MÉDIA | Se não configurado, notificações não são enviadas (fail-open) |
| MinIO file storage | ALTA | Se falhar, downloads quebram (erro 5xx genérico) |
| Asaas link generation | ALTA | Sem PIX QR code ou boleto, compra não acontece |
| Job diário de payout | ALTA | Se não rodar (CronJob misconfigured), criadores não recebem |
| Integração cart-service | MÉDIA | Upsells não aparecem no checkout |

### 3.2 Funcionalidades Parcialmente Implementadas

| Feature | O que falta |
|---------|------------|
| **Download File** | Sem teste de concorrência; sem limite de banda |
| **Balance** | Calcula saldo mas não discrimina holdback por método (PIX vs Boleto vs Card) |
| **Subscription Renewal** | Webhook de renovação Asaas é tratado, mas sem testes de ciclo completo |
| **Anticipation Preview** | Cálculo existe, mas sem validação com Asaas (taxa pode mudar) |

### 3.3 Funcionalidades Não Implementadas (Out of Scope ou Planejadas)

| Feature | Justificativa |
|---------|-----------|
| **Fraud detection** | Não há; confia em Asaas (aceitável para MVP) |
| **Chargeback handling** | 10% reserve é manual; sem webhook de chargeback |
| **Dunning management** | Sem retry de falha de pagamento em assinatura |
| **Audit log** | Sem registro de quem modificou produtos/preços |
| **Multi-currency** | Apenas BRL hardcoded |

### 3.4 Tabela: Onde Estamos vs Onde Queremos

```
FEATURE                    | PLANEJADO  | IMPLEMENTADO | GAP      | BLOQUEADO?
---------------------------|------------|--------------|----------|----------
Produtos CRUD              | 100%       | 100%         | 0%       | Não
Múltiplos arquivos         | 100%       | 100%         | 0%       | Não
Compra avulsa (PIX/Card)   | 100%       | 95%*         | 5%       | Não (falta teste)
Assinatura recorrente      | 100%       | 90%          | 10%      | Não (falta webhook de renewal)
Payout automático diário   | 100%       | 80%**        | 20%      | Sim (job não está documentado)
Antecipação (D+2)          | 100%       | 85%          | 15%      | Não (sem validação real)
Analytics dashboard        | 100%       | 70%***       | 30%      | Não (falta frontend)
Upsells/cross-sells        | 100%       | 90%          | 10%      | Sim (cart-service integração não testada)
Webhooks Asaas HMAC        | 100%       | 100%         | 0%       | Não (mas sem teste)
RabbitMQ publishing        | 100%       | 50%          | 50%      | Não (fail-open, não configurado)

*  Implementado em serviço, sem teste de integração com Asaas real
** RunDailyPayouts existe, mas quem chama? Não há evidência de K8s CronJob configurado
*** Endpoint existe, mas não há dashboard frontend (SPA)
```

---

## 4. Diagrama: Fluxo de Dado vs Fluxo de Negócio

```
┌──────────────────────────────────────────────────────────────┐
│ PLANEJADO (Product Roadmap)                                  │
├──────────────────────────────────────────────────────────────┤
│ Criador → produto digital                                     │
│           ↓                                                    │
│           link público com preço (slug)                       │
│           ↓                                                    │
│         Comprador escolhe método (PIX/Card/Boleto)           │
│           ↓                                                    │
│         Asaas gera link/QR Code                              │
│           ↓                                                    │
│         Pagamento confirmado (webhook Asaas)                 │
│           ↓                                                    │
│         Access token gerado, download ativado                │
│           ↓                                                    │
│         Holdback: 10% reservado 30 dias                      │
│           ↓                                                    │
│         Job 06h BRT: saldo disponível → Payout via PIX      │
│           ↓                                                    │
│         Criador recebe ✅ / Antecipação se solicitado       │
└──────────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────────┐
│ CONSTRUÍDO (Current Implementation)                           │
├──────────────────────────────────────────────────────────────┤
│ Criador → produto digital ✅                                  │
│           ↓                                                    │
│           link público com preço (slug) ✅                    │
│           ↓                                                    │
│         Comprador escolhe método ✅                           │
│           ↓                                                    │
│         Asaas gera link/QR Code ⚠️ (sem teste)              │
│           ↓                                                    │
│         Pagamento confirmado (webhook) ⚠️ (não testado)     │
│           ↓                                                    │
│         Access token gerado ✅                                │
│           ↓                                                    │
│         Holdback 10% ✅                                       │
│           ↓                                                    │
│         Job 06h BRT ❌ (existe mas não está schedulado)      │
│           ↓                                                    │
│         Payout via PIX ⚠️ (implementado sem teste)          │
│           ↓                                                    │
│         Antecipação ⚠️ (lógica sim, validação com Asaas? Não)
└──────────────────────────────────────────────────────────────┘
```

---

## 5. TOP 5 MOVIMENTOS (Prioridade Produto/Negócio)

### 🔴 P0: CRÍTICO — Bloqueia receita

**1. Validar job de payout automático em produção (K3s CronJob)**
   - **Por quê:** Criadores não recebem se job não rodar (BKL-145 principal promise)
   - **O que fazer:** 
     - Encontrar arquivo de manifesto K3s CronJob (presumo em `/deploy/k8s/` ou ArgoCD)
     - Testar job em staging: `kubectl logs -n staging cronjob/linkpay-payout`
     - Garantir que `PayoutService.RunDailyPayouts()` é chamado corretamente
   - **Impacto:** Sem isso, produto não resolve problema de criadores
   - **Esforço:** 4h (diagnóstico + ajuste manifesto + validação)
   - **Dono:** DevOps + Backend

**2. Integração Asaas: Teste E2E (PIX + Boleto + Card)**
   - **Por quê:** Compra é 100% dependente de Asaas; webhook silencioso quebra confir- mação
   - **O que fazer:**
     - Criar suite de testes com Asaas sandbox (já existe credencial?)
     - Validar webhook HMAC parsing
     - Mock de purchase → webhook → confirmação
     - Validar 3 payment methods diferente
   - **Impacto:** Sem isso, podem haver vendas perdidas (webhook não chega ou não processa)
   - **Esforço:** 12h (setup sandbox, 3 testes de integração, CI/CD)
   - **Dono:** Backend + QA

### 🟠 P1: ALTA — Impacta experiência do usuário

**3. Frontend: Dashboard de Vendas & Payouts (React SPA)**
   - **Por quê:** Criadores precisam de visibilidade em tempo real (hoje só existe API)
   - **O que fazer:**
     - Criar SPA em React + TailwindCSS (padrão IIT)
     - Dashboard: vendas/período, receita total, próximo payout
     - Tabela: histórico de compras (email, método, data, valor)
     - Card de antecipação: saldo disponível, taxa, data de liberação
     - Gerenciamento de produtos: CRUD UI
   - **Impacto:** Sem isso, criadores usam curl ou não sabem quando recebem
   - **Esforço:** 40h (design + frontend + integração API)
   - **Dono:** Frontend

**4. Testes de Integração (Repository + Service)**
   - **Por quê:** Código crítico (payout, assinatura) sem teste = risco de regressão
   - **O que fazer:**
     - Setup testcontainers (PostgreSQL + MinIO)
     - Testes de service: CreateProduct → GetProductBySlug → ListProducts
     - Testes de payout: EligiblePurchases → CalculateBalance → CreatePayout
     - Testes de assinatura: CreatePlan → Subscribe → RenewalWebhook
     - Testes de webhook: HMAC validation, idempotência
   - **Impacto:** Bugs em produção → regressões custosas, refund manual
   - **Esforço:** 30h (suite completa + CI setup)
   - **Dono:** Backend + QA

### 🟡 P2: MÉDIO — Melhora de negócio & operação

**5. Validação & Observabilidade em Falhas de Integração**
   - **Por quê:** Erros em Asaas/MinIO retornam 500 genérico (sem rastreamento)
   - **O que fazer:**
     - Adicionar correlation ID em todos os requests (header X-Request-ID)
     - Instrumentar Asaas client com retry + circuit breaker
     - Adicionar traces estruturados em slog (request_id, service, duration)
     - Falhas de MinIO: retry com backoff, logs de upload progress
     - Dashboard: alertas para webhook Asaas não confirmado 2h+
   - **Impacto:** Debug de problemas em produção fica 10x mais rápido
   - **Esforço:** 16h (instrumentation + retry logic + monitoring)
   - **Dono:** Backend + DevOps

---

## Score: 5.5/10

### Justificativa

**Pontos positivos (contribuem para 5.5):**
- ✅ Núcleo de negócio (CRUD produtos, compra, payout) **implementado e funcional**
- ✅ Modelo de dados bem desenhado (multi-tenant, comissões, holdback)
- ✅ Integração Asaas completa em nível de código (chamadas corretas)
- ✅ Assinatura recorrente implementada (complexo, feito certo)
- ✅ Segurança básica (HMAC, JWT, validação)
- ✅ Arquitetura modular (padrão IIT)

**Pontos negativos (impedem chegar a 8+):**
- ❌ **Job de payout não está ativo** (principal promise do produto) = 1.5 pts
- ❌ **Zero testes de integração** = 0.8 pts
- ❌ **Sem dashboard frontend** (criador não vê dados) = 1.0 pt
- ❌ **Asaas webhook não foi validado em produção** = 0.8 pts
- ❌ **RabbitMQ event loop não configurado** = 0.5 pts
- ❌ **Falta observabilidade em falhas** = 0.4 pts

### O que Bloqueia 7.0+

1. **Job rodando + validado em K3s** (P0)
2. **Suite de testes E2E Asaas** (P0)
3. **Frontend dashboard básico** (P1)
4. **Observabilidade estruturada** (P1)

### O que Leva a 9.0+

Além dos P0s:
- Testes de carga (upload 500MB, payout 1000 produtos simultâneo)
- Chargeback handling & dispute workflow
- Multi-currency + conversão dinâmica
- Audit log completo (quem mudou preço, quando)
- Fraud scoring integrado

---

## Recomendações para o CEO (Antonio)

### Decisão Imediata

**Antes de escalar linkpay para produzirem, preciso de aprovação para:**

1. **Ativar job de payout em K3s staging** — testar que criadores recebem R$10 em 24h
   - Sem isso, produto não funciona (não é feature, é bug crítico)
   - Esforço: 2h, bloqueia tudo

2. **Validar Asaas real com uma compra teste** — PIX, Boleto, Card
   - Sem isso, não sei se webhook chega ou se há silent failures
   - Esforço: 3h, bloqueia produção

3. **Criar roadmap frontend** — 2-3 semanas para dashboard mínimo
   - Sem visibilidade, criadores não confiam no serviço
   - Impacto: Adopção baixa

### Timing

- **Semana 1:** P0s (payout + Asaas validation) — **BLOQUEADOR**
- **Semana 2-3:** Testes de integração + observabilidade
- **Semana 4-5:** Frontend dashboard (mínimo viável)
- **Semana 6:** Lançamento privado (SocialMake 1st, my-library creators 2nd)

### Risco Comercial

Se não resolver **job de payout**, qualquer criador que vende e não recebe em 24-48h vai criar issue pública. Credibilidade IIT cai.

Se **webhook Asaas falha silenciosamente**, compensação manual de refunds é possível mas não escala.

---

## Próximas Análises Necessárias

1. **Lente 02 — Arquitetura** (código vs padrões IIT, performance, segurança)
2. **Lente 03 — Dados & Analytics** (SQL queries, índices, replicação)
3. **Lente 04 — DevOps & Deploy** (manifesto K3s, CI/CD, monitoramento)
4. **Lente 05 — Negócio & Finanças** (unit economics, CAC, LTV, projeção 12m)
