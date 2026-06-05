# Lente 09 — Dados

> **Serviço:** linkpay-service | **Data:** 2026-06-05 | **Autor:** @data (SOUL — IIT)

---

## PLANEJADO

O linkpay-service tem `internal/pkg/pulse` client instalado, indicando intenção de instrumentação de eventos. O North Star Metric natural é **GMV mensal de criadores** (volume total de vendas de produtos digitais processado pela plataforma). Métricas derivadas: ticket médio, conversão de checkout, churn de criadores.

---

## CONSTRUÍDO

**Eventos Pulse publicados (confirmado por estrutura do código):**
- `purchase.confirmed` — publicado após ProcessPaymentConfirmed via webhook Asaas
- Possivelmente `purchase.started` — na InitiatePurchase

**Schema PostgreSQL disponível para queries:**
- `digital_products`: id, tenant_id, name, price_cents, commission_percent, active
- `digital_purchases`: id, product_id, buyer_email, buyer_document, amount_paid_cents, status, created_at
- `creator_payouts`: id, tenant_id, amount_cents, status, scheduled_for, completed_at
- `subscriptions`: id, product_id, buyer_email, status, current_period_end
- `product_upsells`: source_product_id, target_product_id, position

**Stats API existente:** GET /admin/stats retorna total de vendas, receita bruta, assinaturas ativas.

---

## GAP

| Evento ausente | Impacto | Bloqueia |
|---|---|---|
| `product.viewed` | Impossível calcular taxa de conversão (view→buy) | Funil de produto |
| `purchase.abandoned` | Sem dados de abandono de checkout | Recuperação de carrinho |
| `download.completed` | Sem prova de entrega do produto | SLA de produto digital |
| `creator.payout_processed` | Sem histórico de repasses no Pulse | Dashboard financeiro |
| `subscription.renewed` | Sem tracking de renovação vs churn | MRR real |
| `upsell.accepted` / `upsell.rejected` | Taxa de conversão de upsell = 0% (calculada) | Order bump |
| `creator.login` | Sem DAU/MAU de criadores | Saúde da base |

**Métricas impossíveis de calcular hoje:**
- GMV real (purchase.confirmed chega, mas sem distinção por período)
- Conversão checkout (view→buy): 0 dados de `product.viewed`
- LTV do comprador (email coletado mas sem histórico cross-produto)
- Churn rate de assinaturas (renovação não rastreada)
- Take rate real (comissão cobrada vs repasse)

**OTel:** telemetry package não é registrado no main.go → `/metrics` inexistente.

---

## ONDE ESTAMOS vs ONDE QUEREMOS

| Métrica | Hoje | Com instrumentação |
|---|---|---|
| GMV por período | Query SQL manual | Pulse dashboard em tempo real |
| Conversão checkout | Impossível (0 views) | View→Buy → Abandoned separados |
| Ticket médio | Query SQL | Pulse + filtros por criador/produto |
| Churn de assinatura | Impossível | renewed/cancelled events no Pulse |
| Saúde de creator | Zero | DAU/MAU, last_sale_at, streak |
| Take rate | Cálculo manual | commission_earned event automático |

---

## TOP 5 MOVIMENTOS

### Movimento 1 — Publicar purchase.confirmed com metadados completos (2h)
Enriquecer evento existente: `{product_id, tenant_id, buyer_email, amount_cents, commission_cents, gateway}`. Permite cálculo de GMV, take rate e ticket médio direto no Pulse.

### Movimento 2 — Evento download.completed (1h)
Publicar no Pulse após token usado com sucesso em HandleDownload/HandleDownloadZIP. Prova de entrega do produto — relevante para CDC Art. 49 (reembolso) e suporte.

### Movimento 3 — Evento creator.payout_processed (1h)
Publicar após UpdatePayoutStatus com status=DONE. Permite dashboard de repasses sem query SQL manual.

### Movimento 4 — Evento subscription.renewed (1h)
Publicar após HandleWebhook processar renovação de assinatura. Sem isso, MRR real é incalculável.

### Movimento 5 — Habilitar OTel no main.go (2h)
Registrar telemetry.RegisterFiber() e telemetry.NewPrometheusMiddleware(). Expor /metrics para Grafana. Latência p99 de download e checkout visível em produção.

---

*Score: 3/10 — Pulse client presente mas subutilizado. North Star sem dados confiáveis.*
