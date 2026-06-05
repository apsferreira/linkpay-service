# Lente 07 — IA & Inteligência

> **Serviço:** linkpay-service | **Data:** 2026-06-05 | **Autor:** @ai (SOUL — IIT)

---

## PLANEJADO

O linkpay-service tem um schema de **upsells** (migration 008: `product_upsells`) que indica intenção de recomendação de produtos relacionados. O conceito de upsell contextual — sugerir produtos complementares durante o checkout — é o único ponto de IA previsto no design original. Não há nenhum documento de PRD ou ADR que mencione LLMs explicitamente.

---

## CONSTRUÍDO

Inteligência determinística existente:

**Upsell estático:** `product_upsells` tabela com `source_product_id → target_product_id + position`. Configurado manualmente pelo criador. Zero ranking dinâmico.

**Holdback logic:** `domain/holdback_test.go` sugere lógica de holdback de pagamento (aguardar D+N dias antes de liberar payout). Regra determinística baseada em dias.

**Seleção de gateway:** não existe múltiplos gateways (apenas Asaas) — sem routing inteligente.

---

## GAP

### Gap 1 — Geração automática de copy de produto
**Problema:** criadores precisam escrever título, descrição e tags SEO manualmente. Baixa adesão por criadores não-escritores.
**Modelo:** Haiku 4.5 — geração estruturada. Input: nome do produto + categoria. Output: `{title, description_short, description_long, seo_tags[]}`.
**Custo:** 100 produtos/mês × 2.000 tokens = $0.02/mês. Negligenciável.
**Prerequisito:** `cover_image_url` no schema (GAP-03 do @design).

### Gap 2 — Ranking dinâmico de upsells
**Problema:** upsells fixos por posição manual. Não considera perfil do comprador nem histórico de compras da sessão.
**Modelo correto: nenhum LLM.** Filtro colaborativo simples: "compradores de X também compraram Y". Com 50+ compras, uma query SQL de co-occurrence bate Haiku em precisão e custo zero.
**Prerequisito:** volume mínimo de 50 compras por produto para resultado estatisticamente válido.

### Gap 3 — Detecção de compra fraudulenta
**Problema:** sem análise de risco antes de aceitar pagamento. Emails temporários (temp-mail.org), IPs de datacenter e alta velocidade de tentativas passam sem bloqueio.
**Modelo:** regras determinísticas primeiro (regex de email temporário, geolocalização por IP). Haiku 4.5 para casos ambíguos.
**Custo:** 100 compras/mês × 500 tokens = $0.005/mês.

### Gap 4 — Previsão de churn de criadores
**Problema:** criadores param de vender sem nenhum sinal proativo. Oportunidade perdida de intervenção com Brio (attend-service).
**Modelo:** Sonnet 4.6 — análise de tendência de vendas + frequência de login no painel.
**Custo:** 20 criadores ativos × análise semanal × 5.000 tokens = $0.10/mês.
**Prerequisito:** eventos de login e vendas instrumentados no Pulse.

### Gap 5 — Precificação dinâmica assistida
**Problema:** criador define preço fixo sem benchmarking. Haiku pode sugerir faixa de preço baseada em produtos similares na plataforma.
**Status:** baixa prioridade. Requer volume de produtos suficiente para benchmarking interno.

---

## ONDE ESTAMOS vs ONDE QUEREMOS

| Dimensão | Hoje | Meta |
|---|---|---|
| Copy de produto | Manual 100% | Haiku gera draft em 1 clique |
| Upsell | Estático por posição | Co-occurrence SQL → Haiku para casos ambíguos |
| Fraude | Zero detecção | Regras determinísticas + Haiku para edge cases |
| Churn prediction | Reativo (criador já parou) | Sonnet detecta queda 2 semanas antes |
| Precificação | Feeling do criador | Benchmark interno da plataforma |
| Custo IA mensal | R$0 | R$0,50–R$1,50 para 100 criadores |

---

## TOP 5 MOVIMENTOS

### Movimento 1 — Geração de copy por Haiku (4h backend + 2h frontend)
Endpoint POST /api/v1/products/:id/generate-copy. Input: categoria + palavras-chave. Output: título + descrição. Criador edita antes de publicar — IA é assistente, não publisher autônomo. Custo: R$0,10/mês.

### Movimento 2 — Co-occurrence SQL para upsells (2h SQL + 1h query)
Query: `SELECT target_product_id, count(*) FROM digital_purchases WHERE buyer_email IN (SELECT buyer_email FROM digital_purchases WHERE product_id = $1) GROUP BY 1 ORDER BY 2 DESC LIMIT 3`. Sem LLM, sem custo de API.

### Movimento 3 — Validação de email temporário (2h)
Lista de domínios temp-mail (10.000+) em arquivo de texto. Validação na hora da InitiatePurchase. Zero custo de API. Reduz chargebacks de compradores fantasma.

### Movimento 4 — Detecção de churn (após ter Pulse instrumentado)
Job semanal: criadores sem venda nos últimos 14 dias que tinham vendas regulares nos 60 dias anteriores → evento churn_risk_detected para Brio agir. Prerequisito: Movimento 5 do @dados (instrumentação Pulse).

### Movimento 5 — Não implementar ML de preços agora
Volume atual insuficiente para treino. Revisar quando plataforma tiver 50+ criadores ativos e 1.000+ produtos vendidos.

---

*Score: 2/10 — zero IA hoje, mas oportunidades de alto valor/baixo custo identificadas.*
