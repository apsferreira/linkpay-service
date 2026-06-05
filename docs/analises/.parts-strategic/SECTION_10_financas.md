# SECTION 10 — Finanças | LinkPay Service
**Sweep SDD 2026-06-05**

---

## I. PLANEJADO — Modelo Financeiro

### 1.1 Estrutura de Comissões (BKL-603)

**Dinâmica:**
- Plataforma IIT retém **30%** de cada venda de produto digital (configurável por produto)
- Criador recebe **70%** líquido do valor da venda
- Comissão **obrigatória** em todos os produtos (CHECK constraint: 0–100%)

**Cálculo por transação:**
```
sale_amount_cents = 100,00
platform_fee_cents = sale_amount_cents * 30% = 30,00
creator_earnings_cents = sale_amount_cents - platform_fee_cents = 70,00
```

**Armazenamento:**
- `digital_products.platform_commission_pct` — % configurável por produto
- `digital_purchases.platform_fee_cents` — valor absoluto cobrado
- `digital_purchases.creator_earnings_cents` — valor líquido do criador

### 1.2 Repasse Automático (BKL-1213)

**Modelo:** Acumula vendas por criador → repassa via Asaas Transfers API a cada ciclo.

| Componente | Especificação |
|-----------|---|
| **Frequência** | Job diário às 06h BRT |
| **Trigger** | Saldo acumulado ≥ R$10,00 |
| **Método** | PIX (configurável por criador) |
| **Holding** | 10% reserva anti-chargeback |
| **Rastreamento** | `creator_payouts.asaas_transfer_id` |

**Fluxo:**
1. Job diário busca tenants com pix_key configurada
2. Soma `creator_earnings_cents` de compras não repassadas
3. Deduz reserva de 10% (held back)
4. Se saldo ≥ R$10,00 → cria registro + chama Asaas
5. Aguarda webhook de conclusão

### 1.3 Antecipação de Recebíveis (BKL-1214)

**Modelo:** Criador pode antecipar D+2 pagamentos de cartão com taxa de 2%.

| Parâmetro | Valor |
|-----------|-------|
| **Taxa de antecipação** | 2% do valor bruto |
| **Prazo** | D+2 (agenda via ScheduleDate do Asaas) |
| **Método** | PIX |
| **Min. elegível** | R$10,00 (após taxa) |

**Cálculo:**
```
eligible_cents = compras de cartão com payment_status=completed e created_at > now-2d
fee_cents = eligible_cents * 2%
net_cents = eligible_cents - fee_cents
```

---

## II. CONSTRUÍDO — Fluxo Financeiro Real

### 2.1 Integrações Implementadas

✅ **Asaas Transfers API**
- Endpoint: `POST /api/v1/transfers` (CreateTransfer)
- Retorna: `id`, `value`, `status`, `type`
- Webhook: `TRANSFER_COMPLETED`, `TRANSFER_FAILED`
- ExternalReference: mapeamento de payout_id → asaas_transfer_id

✅ **Deteccção de Tipo PIX**
- `DetectPixKeyType()` → identifica CPF/CNPJ/Email/Phone
- Necessário para Asaas (campo `pixAddressKeyType`)

✅ **Jobs Agendados**
- Job diário (`RunDailyPayouts`) no contexto de aplicação
- Processa todos os tenants com compras elegíveis
- Trata erros com log mas continua próximo tenant (não cancela job)

✅ **Saldo por Tenant**
- `GetBalance()` retorna: `available_cents`, `reserved_cents`, `total_cents`
- Reserva de 10% deduzida de cada repasse
- Suporta consulta via API pública

### 2.2 Fluxos Implementados

#### A) Repasse Manual (RequestManualPayout)
```go
1. Valida pix_key (salva ou fornecida)
2. Busca GetBalance(tenantID)
3. Valida: amountCents >= R$10,00 e <= balance.available
4. Seleciona compras elegíveis com greedy algorithm até atingir amount
5. Calcula reserva 10% em cada compra
6. Chama executePayout() → Asaas + cria registro
7. Retorna payout criado
```

**Status:** ✅ Implementado, testável via API

#### B) Repasse Automático Diário (RunDailyPayouts)
```go
1. Busca GetTenantsWithEligiblePayouts() — tenants com >R$10 acumulado
2. Para cada tenant:
   - Valida pix_key
   - Busca GetEligiblePurchases()
   - Soma creator_earnings, deduz 10% reserva
   - Se >= R$10 → chama executePayout()
3. Log de sucesso/falha por tenant
```

**Status:** ✅ Implementado, precisa schedule cron/systemd

#### C) Antecipação (RequestAnticipation)
```go
1. Valida pix_key
2. Chama PreviewAnticipation() → calcula fee 2% + net
3. Valida net >= R$10,00
4. Busca compras elegíveis (payment_status=completed, anticipation_requested_at IS NULL)
5. Cria payout com is_anticipation=TRUE, anticipation_fee_cents
6. Chama Asaas Transfer com ScheduleDate = now + 2 business days
7. Retorna payout agendado
```

**Status:** ✅ Implementado, testável via API

### 2.3 Webhook Integration (Asaas → LinkPay)

```go
HandleTransferWebhook(event, transferID, description):
  TRANSFER_COMPLETED → UpdatePayoutStatus(..., "completed", "")
  TRANSFER_FAILED    → UpdatePayoutStatus(..., "failed", description)
```

**Rastreamento:** `creator_payouts.asaas_transfer_id` ↔ Asaas transfer_id

---

## III. GAP — Ausências Críticas

### 3.1 Ledger de Comissões (CRÍTICO)

**Falta:** Tabela de auditoria de comissões cobradas da plataforma.

**Risco:**
- Impossível reconciliar: "Quanto a plataforma recebeu em junho?"
- Sem rastreamento de quem pagou qual comissão quando
- Conformidade tributária: NFS-e não consegue iterar sobre comissões cobradas

**Necessário:**
```sql
CREATE TABLE commission_ledger (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  purchase_id UUID NOT NULL,
  amount_cents INT NOT NULL,
  commission_pct INT NOT NULL,
  recorded_at TIMESTAMPTZ DEFAULT NOW(),
  FOREIGN KEY (purchase_id) REFERENCES digital_purchases(id)
);
-- Índices para: tenant_id, recorded_at (período contábil)
```

### 3.2 Reconciliação de Pagamentos (CRÍTICO)

**Falta:** Comparação automática Asaas vs LinkPay.

**Risco:**
- Transferência criada no Asaas mas webhook perdido → payout fica `processing` eternamente
- Saldo desincronizado com realidade bancária
- CEO não vê status real sem chamar API Asaas

**Necessário:**
```
Daily reconciliation job:
1. Fetch pending payouts (status='processing' or 'scheduled')
2. Query Asaas GET /transfers/{id} para cada um
3. Se status mudou no Asaas → UpdatePayoutStatus() com dados reais
4. Log discrepâncias em tabela separada (audit_discrepancies)
```

### 3.3 Tracking de Chargebacks (ALTO RISCO)

**Falta:** Registro de chargebacks em compras já repassadas.

**Risco:**
- Criador repassado R$100, depois cartão faz chargeback
- Holding de 10% é genérico — sem tracking por compra
- Impossível responsabilizar criador ou identificar fraude padrão

**Necessário:**
```sql
ALTER TABLE digital_purchases 
  ADD chargeback_date TIMESTAMPTZ,
  ADD chargeback_amount_cents INT;
  
CREATE TABLE chargeback_transactions (
  id UUID PRIMARY KEY,
  payout_id UUID,
  purchase_id UUID,
  amount_cents INT,
  reported_at TIMESTAMPTZ,
  resolved_at TIMESTAMPTZ
);
```

### 3.4 NFS-e (Nota Fiscal de Serviço Eletrônica) — CONFORMIDADE

**Falta:** Geração de NFS-e por comissão cobrada.

**Risco:**
- Receita de comissão não tributada formalmente
- Prefeitura Salvador pode multar por falta de nota
- ISS Simples Nacional não cobre comissões opacas

**Contexto atual:**
- LinkPay é **SaaS** (receita de comissão)
- Cada venda de criador = Serviço de Intermediação (código ABRASF: 1.05.10.00 ou 1.07.04.00)
- Em Salvador: 5% ISS (já incluso no Simples Nacional da IIT)

**Necessário:**
- Integração Focus NFe (R$109/mês, 200 docs) ou NFE.io
- Endpoint: `POST /api/nfs-e/emit` → busca `platform_commission_pct` de cada compra → emite NFS-e consolidada mensal
- Rastreamento em `commission_nfs_e` (número da nota, data, valor)

### 3.5 Circuit Breaker em Payout (RISCO OPERACIONAL)

**Falta:** Proteção contra loops de retry no Asaas.

**Risco:**
- Se Asaas cair e `executePayout()` falhar, próximo job tenta novamente com MESMO set de compras
- Sem limite de tentativas → possível double-spend se Asaas recuperar com lag
- Error handling atual: só registra falha, sem backoff

**Implementado:**
```go
// Linha 186-194: registra falha com erro
if err != nil {
  _ = s.repo.UpdatePayoutStatus(..., "failed", err.Error())
  log.Printf("[payout] falha ao chamar Asaas...")
  return fmt.Errorf("erro ao acionar Asaas Transfer: %w", err)
}
```

**Falta:**
- `failed` payouts não têm contagem de tentativas
- Sem max_retry_attempts ou exponential backoff
- Sem alert quando payout fica `failed` por > 24h

---

## IV. ONDE ESTAMOS vs ONDE QUEREMOS

### Matriz de Maturidade

| Dimensão | Status | Score | Objetivo |
|----------|--------|-------|----------|
| **Comissão** | Implementado (30% fixo/produto) | ✅ 8/10 | 9/10 — suportar comissão variável por tier |
| **Repasse** | Implementado (job diário, Asaas) | ✅ 7/10 | 9/10 — reconciliação automática + alertas |
| **Antecipação** | Implementado (D+2, 2%) | ✅ 8/10 | 9/10 — suportar múltiplas antecipações paralelas |
| **Auditoria** | CRÍTICO — sem ledger | ❌ 2/10 | 9/10 — ledger completo + rastreabilidade |
| **Conformidade** | CRÍTICO — sem NFS-e | ❌ 1/10 | 10/10 — NFS-e automática mensal |
| **Reconciliação** | CRÍTICO — sem sync Asaas | ❌ 2/10 | 9/10 — job diário + dashboard |
| **Chargeback** | Não implementado | ❌ 0/10 | 8/10 — tracking + cálculo de liability |
| **Resilência** | Básica (sem retry logic) | ⚠️ 4/10 | 8/10 — circuit breaker + backoff |

### Roadmap Financeiro

**Fase 1 (Crítica — antes de go-live):**
- [ ] Commission Ledger (LEDGER-01)
- [ ] NFS-e Integration (CONF-01)
- [ ] Daily Reconciliation Job (RECON-01)
- [ ] Max Retry + Circuit Breaker (RES-01)

**Fase 2 (MVP+2 semanas):**
- [ ] Chargeback Tracking (FRAUD-01)
- [ ] Dashboard FP&A (METRICS-01)

**Fase 3 (Otimização):**
- [ ] Antecipação paralela / múltiplas solicitações
- [ ] Comissão dinâmica por tier

---

## V. TOP 5 MOVIMENTOS — Riscos Financeiros/Tributários Críticos

### 🔴 CRÍTICO #1: Receita de Comissão Desaparecida (Auditoria)
**Problema:** Sem ledger, impossível responder "quanto ganhamos em junho?"
**Impacto:** 
- Relatório financeiro incompleto para CEO
- Impossibilidade de rastrear comissão por criador/produto
- Não conformidade tributária (multa potencial)
**Ação:** Criar `commission_ledger` + migração retroativa de compras existentes
**Timeline:** 2-3 dias de dev + testes
**Responsável:** Finance Agent + Backend

---

### 🔴 CRÍTICO #2: Conformidade Tributária — NFS-e
**Problema:** Comissões cobradas sem nota fiscal. Prefeitura Salvador pode multar.
**Impacto:**
- Multa ISS retroativa (5% × 12 meses de comissão)
- Bloqueio de emissão de NFs futura
- Risco jurídico (LGPD + Lei de Tributação)
**Ação:** Integrar Focus NFe ou NFE.io, gerar NFS-e mensal consolidada
**Timeline:** 1 dia de integração + 2 dias de testes
**Custo:** R$109-119/mês de provedor + dev
**Responsável:** Finance Agent + Backend

---

### 🔴 CRÍTICO #3: Payout Stuck em Processing (Operacional)
**Problema:** Transferência criada no Asaas, webhook perdido → payout fica `processing` eternamente, saldo desincronizado.
**Impacto:**
- Criador vê saldo zerado mas dinheiro não chegou
- CEO vê payout pendente sem saber se foi processado
- Reprocessamento pode gerar double-spend
**Ação:** Daily reconciliation job — fetch status real do Asaas para payouts pendentes
**Timeline:** 1-2 dias de dev
**Responsável:** Backend + DevOps (configurar cron)

---

### 🔠 ALTO #4: Chargeback Sem Rastreamento (Risco Financeiro)
**Problema:** Criador é repassado, depois cartão faz chargeback. Holding de 10% é genérico — impossível iterar.
**Impacto:**
- Perda de margem em chargebacks recorrentes
- Sem padrão de fraude detectable
- Cresce exposure com volume
**Ação:** Implementar chargeback_ledger + cálculo de liability por criador
**Timeline:** 2-3 dias de dev
**Responsável:** Finance Agent + Backend

---

### 🔠 ALTO #5: Circuit Breaker Faltando (Resilência)
**Problema:** Payout falha no Asaas, job tenta novamente no mesmo set de compras sem backoff. Em caso de sucesso atrasado no Asaas, pode gerar double-spend.
**Impacto:**
- Dupla transferência para criador
- Reconciliação impossível se duplicação ocorre
**Ação:** Adicionar `retry_count`, `last_retry_at`, exponential backoff + limite de 5 tentativas
**Timeline:** 1 dia de dev
**Responsável:** Backend

---

## VI. SUMÁRIO FP&A

### Receita por Modelo
```
Receita de Comissão (SaaS):
  = Σ(digital_purchases.platform_fee_cents) / 100
  = Σ(sale_amount * 30%) por produto

Receita de Antecipação:
  = Σ(creator_payouts.anticipation_fee_cents) onde is_anticipation=TRUE
  = Σ(anticipated_amount * 2%)
```

### Custo por Modelo
```
Custo de Transferência Asaas:
  = Σ(creator_payouts.amount_cents) * taxa_asaas
  Asaas cobra 1.5% em PIX transfers (confirmar contrato)

Holding de Risco (Anti-Chargeback):
  = Σ(creator_payouts.reserved_cents) onde reserved_pct=10%
  Mantém em conta corrente da IIT até T+90
```

### Métricas de Saúde
| Métrica | Fórmula | Alerta |
|---------|---------|--------|
| Commission Margin | comissão / total_sales | < 25% = investigar |
| Payout Velocity | payouts_completed / payouts_total | < 95% = backlog |
| Average Holding | Σ(reserved_cents) / active_creators | > 30 dias = excesso |
| Chargeback Rate | chargebacks / total_purchases | > 1% = fraude |

---

## VII. Checklist Go-Live Financeiro

- [ ] Commission Ledger criada + migração retroativa ✅
- [ ] NFS-e Integration testada (1 emissão real)
- [ ] Daily Reconciliation Job rodando + alertas configurados
- [ ] Circuit Breaker implementado + max_retries=5
- [ ] CEO tem visibilidade: Dashboard MRR/Comissão/Payouts
- [ ] Asaas webhook duplicado em database (redundância)
- [ ] Documentação: fluxo de payout + troubleshooting

---

**Escrito:** 2026-06-05 | **Finance Agent (SOUL)**
**Score:** 5.5/10 — Modelo financeiro sólido, mas auditoria + conformidade críticas faltando.
