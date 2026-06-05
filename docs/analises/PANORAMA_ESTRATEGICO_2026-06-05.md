# Panorama Estratégico — linkpay-service
> Sweep SDD | Data: 2026-06-05 | 11 lentes de análise | SOUL — Instituto Itinerante

---

## Sumário Executivo

O **linkpay-service** é o gateway de monetização de conteúdo do ecossistema IIT. Núcleo técnico **sólido (backend 85% funcional)**, mas **bloqueado em conversão** pela ausência de frontend público. GMV atual: **R$0** (link compartilhável retorna 404). Operacionalmente, o job de repasse automático não está ativo em produção, compradores nunca recebem emails pós-compra, e a validação de segurança do webhook Asaas é incorreta (HMAC incompleto). Risco financeiro crítico: sem ledger de comissões, impossível auditar receita. Conformidade tributária em risco: sem NFS-e, sem rastreamento de chargebacks, sem rotação de dados LGPD.

**Veredito:** Produto pronto para fase de crescimento somente **após resolver 5 P0s** (frontend, payout scheduling, webhook idempotência, ledger financeiro, NFS-e). Tempo estimado: **4-6 semanas**.

---

## Score por Dimensão

| Dimensão | Score | Top Gap | Impacto |
|---|---|---|---|
| **Produto** | 5.5/10 | Job de payout não está ativo em K3s | Criadores não recebem |
| **Design** | 3/10 | Sem frontend público (404 no link) | GMV impossível |
| **Frontend** | 4/10 | Zero páginas HTML de conversão | Sem checkout, sem download UI |
| **Backend** | 3/10 | Webhook sem idempotência + HMAC incorreto | Pagamentos duplicados |
| **DevOps** | 5/10 | Tag `:main-latest` mutável em prod | Deploy não determinístico |
| **Qualidade** | 1.5/10 | <5% cobertura de testes, CI não bloqueia | Regressões em produção |
| **IA** | 2/10 | Zero inteligência, só upsells estáticos | Sem recomendação dinâmica |
| **Growth** | 2/10 | GMV = R$0 (404), sem OG tags | Criadores sem visibilidade |
| **Dados** | 3/10 | 7 eventos críticos faltando em Pulse | NSM impossível de medir |
| **Finanças** | 5.5/10 | Sem ledger de comissão, sem NFS-e, sem reconciliação | Auditoria falha, multa potencial |
| **Legal** | 3/10 | Sem reembolso CDC Art. 49, sem LGPD retention, buyer_email em logs | PROCON + ANPD |
| **TOTAL** | **3.9/110** | — | — |

---

## Top 10 Gaps Críticos (P0 + P1)

### P0 — Bloqueia Conversão e Receita

1. **Página de produto público não existe (404)** — GAP-GROWTH-01 | Frontend
   - Link compartilhável `pay.institutoitinerante.com.br/p/:slug` retorna 404
   - GMV impossível sem URL de conversão
   - Impacto: **R$0 faturado, creadores sem entender por quê**
   - Esforço: 5 dias (frontend)
   - Blocker: sim

2. **Job de payout diário não está schedulado em K3s** — GAP-PRODUTO-01 | DevOps + Backend
   - Função `RunDailyPayouts()` existe no código mas ninguém chama
   - Criadores nunca recebem (holdback 10% fica eternamente)
   - Impacto: **Credibilidade IIT por terra**
   - Esforço: 2 horas (adicionar ticker em main.go + K3s CronJob)
   - Blocker: sim

3. **Webhook HMAC inválido + sem idempotência** — GAP-BACKEND-02 + GAP-BACKEND-01 | Backend + Segurança
   - `hmac.Equal(secret, token)` ≠ `HMAC-SHA256(body, secret)` — qualquer cliente que saiba o secret pode forjar compra
   - Asaas retry automático → pagamento duplicado sem `SetNX` no Redis
   - Impacto: **Vendas perdidas, refunds manuais, confusão de logs**
   - Esforço: 4 horas (HMAC + Redis idempotência)
   - Blocker: sim

4. **Sem email transacional pós-compra** — GAP-DESIGN-04 | Backend
   - `ProcessPaymentConfirmed` não chama notification-service
   - Comprador não sabe que pode baixar o produto
   - Impacto: **Abandono de conversão, tickets de suporte**
   - Esforço: 2 horas (adicionar chamada HTTP)
   - Blocker: sim

5. **Testes <5%, CI não bloqueia falhas** — GAP-QUALIDADE-10 | QA + Backend
   - `continue-on-error: true` no CI
   - Regressões passam silenciosamente para produção
   - Impacto: **Bugs em payout, webhook, download — sem aviso prévio**
   - Esforço: 3 dias (suite de testes integração + CI gate)
   - Blocker: não imediato mas crítico

### P1 — Impacta Experiência e Conformidade

6. **Ledger de comissão não existe (auditoria)** — GAP-FINANÇAS-01 | Finance + Backend
   - Impossível responder "quanto ganhamos em junho?"
   - Não conformidade tributária (multa ANPD potencial)
   - Impacto: **CEO sem visibilidade financeira, risco legal**
   - Esforço: 2 dias (migration + retroativo)
   - Blocker: não para conversão, mas para conformidade

7. **NFS-e não emitida (tributação)** — GAP-LEGAL-04 + GAP-FINANÇAS-04 | Finance + Legal
   - Comissões cobradas sem nota fiscal
   - Prefeitura Salvador pode multar 5% × 12 meses retroativo
   - Impacto: **Multa de R$X mil a R$X milhão**
   - Esforço: 3 dias (integração Focus NFe + testes)
   - Blocker: não para MVP, mas antes de escala

8. **Sem direito de reembolso (CDC Art. 49)** — GAP-LEGAL-03 | Legal + Backend
   - Comprador não consegue pedir reembolso em 7 dias
   - PROCON pode multar e forçar reembolso retroativo
   - Impacto: **Multa + reparação para toda a base de compras**
   - Esforço: 1 dia (POST /refund endpoint)
   - Blocker: não, mas crítico antes de escala

9. **Sem OG tags + cover_image_url (conversão social)** — GAP-DESIGN-05 + GAP-FRONTEND-03 | Design + Frontend
   - Links em WhatsApp/Instagram não geram preview
   - Abraça ~60-80% do tráfego de infoprodutos BR
   - Impacto: **Taxa de conversão 3-5x menor em redes sociais**
   - Esforço: 1 dia (adicionar campo + OG tags rendering)
   - Blocker: não para MVP, mas crítico para growth

10. **Reconciliação de payouts ausente** — GAP-FINANÇAS-02 | Finance + Backend
    - Webhook Asaas perdido → payout fica `processing` eternamente
    - Saldo desincronizado com realidade bancária
    - Impacto: **CEO vê status errado, criador reclama dinheiro "desaparecido"**
    - Esforço: 1 dia (daily reconciliation job)
    - Blocker: não imediato, mas risco operacional

---

## Roadmap 90 dias

### Semana 1-2 (P0 — Bloqueadores de Conversão)

| Tarefa | Esforço | Dono | Status |
|--------|---------|------|--------|
| **PROD-001:** Ativar RunDailyPayouts em K3s CronJob + ticker local | 2h | Backend + DevOps | Não iniciado |
| **BACKEND-001:** Validar webhook Asaas: HMAC-SHA256 + Redis SetNX idempotência | 4h | Backend | Não iniciado |
| **BACKEND-002:** Disparar email pós-compra (notification-service) | 2h | Backend | Não iniciado |
| **FRONTEND-001:** Decidir arquitetura de frontend (próprio vs checkout-service) | 4h | PM + Frontend | Não iniciado |
| **FRONTEND-002:** Página de produto public `/p/:slug` (React SPA) | 40h | Frontend | Não iniciado |
| **DESIGN-001:** Adicionar `cover_image_url` ao domain model + migration | 2h | Backend | Não iniciado |

**Milestone:** Link compartilhável funciona, comprador vê landing page + checkout + email de confirmação.

### Semana 3-6 (P1 — Qualidade, Conformidade, Observabilidade)

| Tarefa | Esforço | Dono | Status |
|--------|---------|------|--------|
| **QA-001:** Testes de integração (webhook, payout, download) | 30h | QA + Backend | Não iniciado |
| **QA-002:** Remover `continue-on-error: true` do CI + CI gate 80% coverage | 4h | DevOps + QA | Não iniciado |
| **BACKEND-003:** Ledger de comissão + migration retroativa | 16h | Finance + Backend | Não iniciado |
| **BACKEND-004:** NFS-e integration (Focus NFe ou NFE.io) | 16h | Finance + Backend | Não iniciado |
| **BACKEND-005:** Daily reconciliation job (fetch Asaas status) | 8h | Backend | Não iniciado |
| **BACKEND-006:** CDC Art. 49 refund endpoint | 4h | Backend | Não iniciado |
| **FRONTEND-003:** Página de download + listagem de arquivos | 16h | Frontend | Não iniciado |
| **FRONTEND-004:** Painel do criador: CRUD produtos + stats dashboard | 40h | Frontend | Não iniciado |
| **DATA-001:** Instrumentar 7 eventos Pulse (product.viewed, download.completed, etc) | 8h | Data + Backend | Não iniciado |

**Milestone:** Primeira venda real em sandbox, passando por todos os fluxos (compra → webhook → payout → refund). Criador vê stats em tempo real.

### Semana 7-12 (P2 — Crescimento, Escala, Otimização)

| Tarefa | Esforço | Dono | Status |
|--------|---------|------|--------|
| **GROWTH-001:** OG tags renderizadas corretamente em WhatsApp | 4h | Frontend | Não iniciado |
| **GROWTH-002:** Order Bump UI (upsells no checkout) | 16h | Frontend | Não iniciado |
| **GROWTH-003:** Pixel Meta + GTM integration | 8h | Frontend + Growth | Não iniciado |
| **IA-001:** Geração de copy por Haiku (título + descrição) | 6h | AI + Frontend | Não iniciado |
| **IA-002:** Ranking dinâmico de upsells (co-occurrence SQL) | 4h | Data + Backend | Não iniciado |
| **DEVOPS-001:** Fixar tag de imagem (remover `:main-latest`) | 30m | DevOps | Não iniciado |
| **DEVOPS-002:** Adicionar PDB + réplicas mínimas (alta disponibilidade) | 30m | DevOps | Não iniciado |
| **DEVOPS-003:** NetworkPolicy baseline + securityContext | 1h | DevOps | Não iniciado |
| **BACKEND-007:** Circuit breaker + retry com backoff em Asaas | 8h | Backend | Não iniciado |
| **LEGAL-001:** Mascarar PII em logs + LGPD data retention policy | 4h | Backend | Não iniciado |

**Milestone:** Plataforma pronta para escala: múltiplos criadores, reconciliação automática, observabilidade completa, conformidade legal.

---

## Riscos que Podem Matar o Produto

### 🔴 CRÍTICO (Bloqueiam Conversão + Receita)

1. **Sem página pública de produto**
   - Link para compartilhar não existe
   - GMV permanece R$0 indefinidamente
   - **Ação:** Resolver até fin Semana 1 (frontend architecture decision)

2. **Payout não rodando**
   - Criadores não recebem por dias/semanas
   - Churn 100% de novos criadores
   - **Ação:** Ativar CronJob em 24h

3. **Webhook com falha silenciosa**
   - Compra não se confirma, ou confirma duplicada
   - Vendas perdidas, refunds manuais caros
   - **Ação:** Validar em sandbox + testes até Semana 1

4. **Sem conformidade tributária (NFS-e)**
   - Multa retroativa da prefeitura
   - Bloqueio de novas emissões
   - **Ação:** Implementar antes de escala (Semana 4)

### 🟠 ALTO (Impactam Experiência + Legal)

5. **Sem testes + CI não bloqueia**
   - Regressão em produção = refunds + suporte manual
   - Crescimento impossível sem qualidade
   - **Ação:** Testes de integração + CI gate (Semana 3)

6. **Sem email transacional**
   - Comprador não sabe que produto foi liberado
   - Abandono pós-compra ~40%
   - **Ação:** 2h (Semana 1)

7. **Sem reembolso CDC Art. 49**
   - PROCON multa + força reembolso da base
   - Risco jurídico crescente
   - **Ação:** 1h endpoint (Semana 2)

8. **Chargebacks sem rastreamento**
   - Perda de margem cresce com volume
   - Impossível identificar padrão de fraude
   - **Ação:** Implementar tracking (Semana 3)

---

## Próximos Passos Imediatos (Esta Semana)

### ⚠️ HORA 0-4 (Hoje)

1. **Verificar se RunDailyPayouts está de fato inativo em K3s**
   ```bash
   kubectl get cronjob -n production | grep linkpay
   kubectl logs -n production cronjob/linkpay-payout --tail 100
   ```
   - Se CronJob não existe → criar em `/deploy/k8s/cronjob.yaml`
   - Se existe mas falha → debugar por quê

2. **Testar webhook Asaas em staging**
   - Acionar compra de teste no Asaas sandbox
   - Verificar se `ProcessPaymentConfirmed` é chamado
   - Confirmar que token de download é gerado
   - **Resultado esperado:** POST webhook chega, compra fica `confirmed` em 1s

3. **Decidir arquitetura de frontend**
   - Opção A: Servir HTML no linkpay-service via templates Go (SSR)
   - Opção B: Delegar tudo ao checkout-service (rota `/p/:slug`)
   - **Recomendação:** Opção B (evita terceiro sistema de templates)
   - **Decisão:** CEO (Antonio)

### ⚠️ HORA 4-8 (Amanhã)

4. **Validar HMAC do webhook manualmente**
   - Ler o código em `linkpay_handler.HandleWebhook()`
   - Confirmar se é `hmac.Equal(secret, token)` (ERRADO) ou `HMAC-SHA256(body, secret)` (CERTO)
   - Se errado: abrir issue P0, cronometer 4h para fix

5. **Verificar se notification-service é chamado em ProcessPaymentConfirmed**
   - Buscar por `NOTIFICATION_SERVICE_URL` no código
   - Se não chamado: adicionar chamada HTTP em 2h (P0)

### ⚠️ HORA 8-24 (Próximos 2 dias)

6. **Planejar PR inicial de frontend**
   - Definir componentes shadcn/ui (Card, Button, Form)
   - Esboçar rotas React
   - Criar issue no GitHub com spec mínima (landing + checkout 1 página)

7. **Registrar issues no GitHub com rótulos P0/P1**
   - Issue #7 (RunDailyPayouts never called) — P0
   - Issue #8 (Webhook without idempotency) — P0
   - Issue #9 (Coverage <5%) — P1
   - Issue #10 (HMAC incorrect) — P1
   - Issue #11 (Circuit breaker missing) — P1
   - ... (total 22 issues, já criadas conforme contexto)

---

## Matriz de Decisões Necessárias do CEO

| Decisão | Impacto | Prazo |
|---------|---------|-------|
| **Arquitetura de frontend (próprio vs checkout-service)** | Bloqueia desenvolvimento | HORA 0 |
| **Token de design para linkpay (cor primária, personalidade)** | Identidade visual | Semana 1 |
| **Política de comissão: 30% ou variável por tier?** | Pricing do produto | Semana 2 |
| **Integração NFS-e: contratar Focus NFe ou NFE.io?** | Budget + compliance | Semana 2 |
| **Prioridade: growth (OG tags) vs qualidade (testes)?** | Sequência de features | Semana 3 |
| **Go-live privado quando? (SocialMake criadores first?)** | Timeline de launch | Semana 4 |

---

## Referência de Arquivos Críticos

- **Backend:** `/Users/antonio.ferreira/projects/linkpay-service/backend/`
- **Handlers:** `/backend/internal/handler/linkpay_handler.go` (webhook + endpoints)
- **Services:** `/backend/internal/service/linkpay_service.go` (payout, assinatura)
- **Domain:** `/backend/internal/domain/` (models)
- **Migrations:** `/backend/migrations/` (8 migrations)
- **Manifests K3s:** `/deploy/k8s/` (deployment, service, configmap)
- **CI/CD:** `.github/workflows/ci.yml`
- **Dockerfile:** `Dockerfile` (multi-stage Go 1.24)

---

## Scores Finais por Lente

| Lente | Score | Justificativa |
|---|---|---|
| **01 — Produto** | 5.5/10 | Núcleo funcional, mas job de payout não ativo + sem testes |
| **02 — Design** | 3/10 | Sem página pública, sem OG tags, sem checkout visual |
| **03 — Frontend** | 4/10 | API 85% pronta, zero HTML, zero SPA |
| **04 — Backend** | 3/10 | HMAC inválido, sem idempotência webhook, sem circuit breaker |
| **05 — DevOps** | 5/10 | Tag mutável em prod, sem PDB, testes não bloqueiam |
| **06 — Qualidade** | 1.5/10 | <5% coverage, CI com continue-on-error, zero seg static |
| **07 — IA** | 2/10 | Zero inteligência, só upsells estáticos |
| **08 — Growth** | 2/10 | GMV=R$0, sem OG tags, sem pixel de rastreamento |
| **09 — Dados** | 3/10 | 7 eventos Pulse faltando, NSM impossível |
| **10 — Finanças** | 5.5/10 | Modelo sólido, mas ledger + NFS-e críticas faltando |
| **11 — Legal** | 3/10 | Sem refund, sem LGPD retention, PII em logs |

**SCORE GLOBAL: 3.9/10**

---

## Conclusão

O **linkpay-service está num ponto crítico de decisão**: infraestrutura técnica sólida, mas bloqueado em 5 frentes simultâneas (frontend, payout scheduling, webhook security, ledger, conformidade). Sem resolução dos P0s nas próximas 2 semanas, o produto permanece indefinidamente com GMV = R$0 e risco crescente de multa tributária.

**Recomendação:** Reunião de alinhamento com CEO (Antonio) para:
1. Decidir arquitetura de frontend
2. Priorizar roadmap (qualidade vs growth)
3. Autorizar 4-6 semanas de sprints focadas
4. Definir go-live privado com primeira cohort (SocialMake creators)

Sem essas decisões, o produto é apenas um sistema técnico sem receita, sem usuários e sem conformidade legal.

---

**Análise completa produzida por SOUL PM — Instituto Itinerante | 2026-06-05**
