# Lente 08 — Growth

> **Serviço:** linkpay-service | **Data:** 2026-06-05 | **Autor:** @growth (SOUL — IIT)

---

## PLANEJADO

O linkpay-service foi desenhado como alternativa ao Hotmart/Kiwify para criadores brasileiros no ecossistema IIT. O design original inclui: links de pagamento compartilháveis, assinaturas recorrentes, repasse automático de comissão ao criador, e upsells no checkout (migration 008). A URL `pay.institutoitinerante.com.br` era para ser o link que o criador compartilha no Instagram ou WhatsApp.

---

## CONSTRUÍDO

**Mecanismos de crescimento existentes:**
- **Comissão configurável por produto** (commission_percent — migration 002): modelo correto de marketplace
- **Schema de upsells** (migration 008): foundation para Order Bump no checkout
- **Stats API** (GET /admin/stats): criador pode ver vendas, mas sem dashboard visual
- **Multi-arquivo por produto** (BKL-1206): suporte a cursos com vídeos + PDFs
- **Assinaturas recorrentes** (migration 005): receita recorrente para criadores

---

## GAP

### Gap 1 — Página de produto: o link não existe
O criador recebe uma URL `pay.institutoitinerante.com.br/p/:slug` para compartilhar no WhatsApp. Essa URL retorna 404. **O produto não existe para o comprador.** É o gap mais crítico de conversão — zero GMV possível sem ele.

### Gap 2 — OG Tags ausentes: links sem preview no WhatsApp/Instagram
Mesmo que a página existisse hoje, sem `og:image`, `og:title` e `og:description`, links compartilhados no WhatsApp aparecem como texto simples. Em e-commerce brasileiro, WhatsApp responde por 60-80% das vendas de infoprodutos.

### Gap 3 — Order Bump sem UI de configuração
O schema de upsells existe mas o criador não tem onde configurar quais produtos sugerir no checkout. O comprador não vê nenhum upsell.

### Gap 4 — Zero pixel de rastreamento
Sem Meta Pixel ou Google Tag Manager, criadores não podem fazer retargeting de visitantes. Isso elimina completamente a possibilidade de ads pagos para tráfego frio.

### Gap 5 — Dashboard de criador inexistente
Stats via API apenas. Sem gráfico de vendas, sem ticket médio, sem ranking de produtos. Kiwify tem dashboard em tempo real — criadores que experimentam o linkpay não veem valor imediatamente.

### Gap 6 — Afiliados ausentes
Kiwify e Hotmart têm programa de afiliados como principal canal de aquisição de compradores. Sem afiliados, o crescimento depende 100% do tráfego orgânico do criador.

### Gap 7 — Thank-you page sem cross-sell
Após a compra, o comprador vê apenas a confirmação. Hotmart usa a thank-you page para oferecer o próximo produto do criador (cross-sell). Momento de maior receptividade do comprador.

---

## ONDE ESTAMOS vs ONDE QUEREMOS

| Feature | Hotmart/Kiwify | linkpay hoje | Gap |
|---|---|---|---|
| Página de produto | ✅ Builder drag-drop | ❌ 404 | Crítico |
| OG Tags / WhatsApp preview | ✅ Auto-gerado | ❌ | Crítico |
| Order Bump (upsell checkout) | ✅ | ❌ UI falta | Alto |
| Thank-you page | ✅ Customizável | ❌ | Alto |
| Pixel Meta/Google | ✅ | ❌ | Alto |
| Dashboard criador | ✅ Tempo real | ❌ Só API | Médio |
| Afiliados | ✅ | ❌ Schema falta | Médio |
| Email pós-compra | ✅ | ❌ Não disparado | P1 |
| Checkout 1 clique | ✅ | ❌ | Baixo |
| Coprodutor | ✅ | ❌ | Baixo |

**GMV possível hoje: R$0** (link compartilhável retorna 404)

---

## TOP 5 MOVIMENTOS

### Movimento 1 — Página de produto pública (bloqueador absoluto de GMV)
Página `/p/:slug` com: capa do produto, título, descrição, preço, CTA "Comprar agora", contador de compradores. Pode ser renderizada pelo checkout-service via redirect 301 ou SSR Fiber com template HTML.
Esforço: 3-5 dias. Impacto: desbloqueia 100% do GMV.

### Movimento 2 — OG Tags automáticas (2h adicional na página de produto)
`og:title`, `og:description`, `og:image` (capa do produto no MinIO), `og:url`. WhatsApp e Instagram leem automaticamente. Custo: zero. Impacto: multiplicador de 3-5x nas conversões de links compartilhados.

### Movimento 3 — Email pós-compra imediato (4h)
Chamar notification-service após ProcessPaymentConfirmed com: nome do produto, link de download, nome do criador. A infraestrutura já existe (NOTIFICATION_SERVICE_URL no .env). Impacto: redução de tickets de suporte e aumento de percepção de qualidade.

### Movimento 4 — Order Bump UI no checkout (1 semana)
Mostrar upsells configurados pelo criador no momento do pagamento. "Adicione também por R$X". Dados do schema já existem. Impacto estimado: +15-25% de ticket médio em produtos com upsell bem configurado.

### Movimento 5 — Dashboard visual básico do criador (1-2 semanas)
React SPA com: vendas do mês, receita líquida após comissão, top produtos, gráfico de vendas 30d. Pode ser módulo `/admin/linkpay` dentro do checkout-service. Impacto: retenção de criadores que onboardam.

---

*Score: 2/10 — fundação técnica sólida, mas GMV atual = R$0 por ausência de frontend.*
