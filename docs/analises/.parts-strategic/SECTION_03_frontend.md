# Lente 03 — Frontend | linkpay-service

**Score: 4/10**
Data da análise: 2026-06-05
Analista: Frontend Specialist (IIT)

---

## 1. PLANEJADO — O que o produto deveria ter

O linkpay-service é uma plataforma de venda de produtos digitais (ebooks, cursos, templates, downloads). O fluxo de compra esperado para um produto desse tipo exige:

- **Página pública de produto** (`/p/:slug`) — landing page de conversão com: nome, descrição, preço, imagem de capa, botão de compra, depoimentos, garantia. Renderizada com OG tags para compartilhamento em redes sociais e WhatsApp.
- **Página de checkout** — formulário de nome, e-mail, CPF, seleção de método (PIX / cartão), exibição de QR code PIX ou redirect para Asaas.
- **Página de obrigado / entrega** — confirmação de compra com link de download imediato ou instrução de aguardar confirmação de pagamento.
- **Página de download** — acessada via token (`/download/:token`), exibe os arquivos disponíveis para download.
- **Painel do criador** — CRUD de produtos, upload de arquivos, dashboard de vendas e repasses.

---

## 2. CONSTRUÍDO — O que existe hoje

### Rotas públicas (JSON puro, sem HTML)

| Método | Rota | Finalidade |
|--------|------|-----------|
| GET | `/api/v1/public/products/:slug` | Retorna `DigitalProduct` em JSON — dados da página de produto |
| POST | `/api/v1/public/products/:slug/purchase` | Inicia compra — retorna `DigitalPurchase` com `pix_qr_code`, `pix_copy_paste`, `payment_url` |
| POST | `/api/v1/public/products/:slug/cart` | Cria carrinho com upsells via cart-service |
| GET | `/api/v1/public/download/:token` | Redirect 302 para presigned URL do MinIO |
| GET | `/api/v1/public/download/:token/file/:fileId` | Download de arquivo individual (redirect 302) |
| GET | `/api/v1/public/download/:token/zip` | Streaming ZIP de todos os arquivos do produto |

### Rotas admin (JWT)

| Método | Rota | Finalidade |
|--------|------|-----------|
| GET/POST | `/api/v1/linkpay/products` | CRUD de produtos |
| PUT/DELETE | `/api/v1/linkpay/products/:id` | Edição e desativação |
| POST | `/api/v1/linkpay/products/:id/upload` | Upload de arquivo principal (MinIO) |
| POST/GET/DELETE | `/api/v1/linkpay/products/:id/files` | Múltiplos arquivos por produto |
| GET | `/api/v1/admin/linkpay/stats` | Stats de vendas (30 dias) |
| POST | `/api/v1/linkpay/webhook` | Webhook Asaas (HMAC validado) |

### Contratos de API relevantes para o frontend

**`GET /api/v1/public/products/:slug` — response shape:**
```typescript
interface DigitalProduct {
  id: string
  tenant_id: string
  slug: string
  name: string
  description: string
  price_cents: number
  currency: string
  product_type: 'digital' | 'ebook' | 'course' | 'template'
  download_limit: number
  is_active: boolean
  platform_commission_pct: number
  file_name?: string
  file_size_bytes?: number
  mime_type?: string
  created_at: string
  updated_at: string
  files?: DigitalProductFile[]
  // file_url OMITIDO na resposta pública (zeroed no handler)
}

interface DigitalProductFile {
  id: string
  product_id: string
  storage_key: string
  file_name: string
  file_size_bytes: number
  mime_type: string
  sort_order: number
  created_at: string
}
```

**`POST /api/v1/public/products/:slug/purchase` — request/response:**
```typescript
interface InitiatePurchaseRequest {
  customer_name: string   // required, max 255
  customer_email: string  // required, email, max 255
  customer_cpf?: string   // optional, max 14
  payment_method: 'pix' | 'credit_card'
}

interface DigitalPurchase {
  id: string
  product_id: string
  tenant_id: string
  customer_email: string
  customer_name: string
  amount_cents: number
  payment_method: string
  payment_status: string
  access_token?: string
  access_expires_at?: string
  download_count: number
  created_at: string
  // Transitórios — retornados só na criação:
  pix_qr_code?: string      // base64 da imagem QR
  pix_copy_paste?: string   // string para "copia e cola"
  payment_url?: string      // redirect para Asaas (cartão)
}
```

**`GET /api/v1/admin/linkpay/stats` — response shape:**
```typescript
interface StatsResponse {
  period: string
  total_revenue_cents: number
  total_sales: number
  products: Array<{
    id: string
    name: string
    sales: number
    revenue_cents: number
  }>
  daily_sales: Array<{
    date: string
    sales: number
    revenue_cents: number
  }>
}
```

---

## 3. GAP — Ausências que bloqueiam conversão

### GAP-01 (CRÍTICO) — Nenhuma página HTML pública existe
O serviço não serve nenhum HTML. Não há rota `GET /p/:slug` que retorne uma landing page de conversão. Um link de pagamento compartilhado hoje não tem destino — o comprador receberia `404` ou precisaria de um frontend separado já implantado.

### GAP-02 (CRÍTICO) — Zero OG tags / SEO
Sem HTML, não há `og:title`, `og:description`, `og:image`, `og:url`. Links compartilhados no WhatsApp e Instagram não geram preview. Isso destrói a taxa de conversão em canais sociais, que são o vetor primário de distribuição de produtos digitais.

### GAP-03 (ALTO) — Ausência de imagem de capa no modelo de produto
O `DigitalProduct` não tem campo `cover_image_url` ou equivalente. Não é possível montar uma landing page visualmente atraente sem imagem de capa. O OG:image também ficaria sem conteúdo.

### GAP-04 (ALTO) — Campo `description` sem suporte a rich text
A description é `string` plana (max 5000). Sem suporte a Markdown ou HTML sanitizado, a página de produto não consegue exibir formatação (bullets, negrito, seções). Produto concorrente (Hotmart, Kiwify) exibe descrição formatada como diferencial de apresentação.

### GAP-05 (MÉDIO) — Sem página de confirmação pós-compra
Após `POST /purchase` retornar `201`, o comprador precisa de uma tela: QR code renderizado (base64), instrução de aguardar, e — após confirmação Asaas — redirect para a área de download. Esse fluxo de "polling de status" ou redirect pós-webhook não existe.

### GAP-06 (MÉDIO) — Painel do criador não existe como SPA
As rotas admin (`/api/v1/linkpay/*`) existem e têm autenticação JWT, mas não há frontend React para o criador gerenciar produtos, fazer upload, ver stats e solicitar repasse. O criador teria que usar curl ou Insomnia.

### GAP-07 (BAIXO) — Sem upsell UI
A API de upsell (`CreateCart`, `GetUpsellsByProductID`) está implementada no backend (BKL-1221), mas sem uma página de produto que consuma esses dados, os upsells nunca são exibidos ao comprador.

---

## 4. ONDE ESTAMOS vs ONDE QUEREMOS

```
ONDE ESTAMOS (hoje)                    ONDE QUEREMOS (v1 completo)
──────────────────────────────────     ──────────────────────────────────
API JSON 100% funcional            →   API JSON 100% funcional (mantém)
Nenhum HTML público                →   Landing page /p/:slug com SSR/OG
Sem imagem de capa                 →   cover_image_url no produto
Description texto plano            →   Description Markdown (renderizado)
Checkout: formulário inexistente   →   Página checkout embutida ou delegada
Pós-compra: sem UI                 →   Tela de obrigado + polling de status
Download: funciona via redirect    →   Página HTML de download com lista
Painel criador: sem SPA            →   Dashboard React no checkout-service
Stats: só via API                  →   Gráficos no painel do criador
Upsells: implementados no back     →   Exibidos na página de produto
```

**Resumo:** O backend está 85% pronto para viabilizar a jornada completa de venda. O frontend está em 0% — nenhuma tela existe. A conversão hoje é tecnicamente impossível sem um frontend externo.

---

## 5. TOP 5 MOVIMENTOS

### M1 — Decidir arquitetura de frontend (URGENTE — decisão de 1 dia)

**Pergunta central:** O linkpay-service deve servir HTML próprio via templates Go/Fiber, ou delega 100% ao checkout-service React?

**Recomendação:** Delegar ao checkout-service por duas razões:
1. O checkout-service já existe, já está no K3s (`checkout-api.institutoitinerante.com.br`), e foi construído para ser a camada de UX de compra.
2. Manter SSR em Go (html/template) criaria um terceiro sistema de templates a manter, sem ganho de performance significativo para esse caso.

**Exceção:** A rota `GET /p/:slug` poderia ser um redirect 301 para `checkout.institutoitinerante.com.br/p/:slug`, mantendo a URL curta de compartilhamento no domínio `pay.institutoitinerante.com.br`.

### M2 — Adicionar `cover_image_url` ao modelo de produto (2-3h backend)

Migração simples: adicionar coluna `cover_image_url TEXT` na tabela `digital_products`, expor no `CreateProductRequest` e `UpdateProductRequest`, e incluir na resposta pública. Sem isso, qualquer página de produto é visualmente pobre e o OG:image fica em branco.

```sql
-- migration 008
ALTER TABLE digital_products ADD COLUMN cover_image_url TEXT;
```

### M3 — Implementar página de produto no checkout-service com OG tags (sprint de 3-5 dias)

Criar rota `/p/:slug` no checkout-service (React + Vite) que:
- Faz `GET /api/v1/public/products/:slug` no linkpay-service
- Renderiza a landing page de conversão (nome, preço, descrição, imagem de capa)
- Injeta OG tags via `react-helmet-async` (ou meta tags estáticas via Vite SSG)
- Exibe botão "Comprar" que abre o formulário de checkout inline

Para OG tags funcionarem com bots de redes sociais (sem JS): considerar SSR mínimo via `vite-plugin-ssr` ou um endpoint Go que retorne HTML estático apenas para User-Agents de bots (técnica de "prerender on-demand").

### M4 — Implementar fluxo pós-compra com polling de status (sprint de 2-3 dias)

Após `POST /purchase`:
1. Exibir QR code PIX (renderizar base64 como `<img>`) + string copia-e-cola
2. Fazer polling em `GET /api/v1/public/purchase-status/:id` a cada 3s (endpoint ainda não existe — precisaria ser criado no backend)
3. Ao detectar `payment_status: "confirmed"`, redirecionar para `/download/:token`

Alternativa: usar o webhook Asaas para emitir evento via Server-Sent Events (SSE) ou WebSocket — mais elegante, mas mais complexo.

### M5 — Painel do criador integrado ao SocialMake ou como módulo do checkout-service admin (análise de 1 dia)

O criador precisa de uma UI para: criar produto, fazer upload, ver stats, solicitar repasse. Opções:
- **Opção A:** Módulo dentro do SocialMake (que já tem tenant, auth, e dashboard). Mais coeso se o IIT é o único criador agora.
- **Opção B:** Seção `/admin/linkpay` dentro do checkout-service, acessada com JWT do auth-service.
- **Opção C:** Frontend React standalone (`linkpay-admin`) — evitar: cria mais um app para manter.

Recomendação: **Opção B** no curto prazo. O checkout-service tem o contexto de compra; faz sentido o criador gerenciar produtos na mesma base de código que serve o comprador.

---

## Score: 4/10

**Justificativa:**
- Backend da API está bem construído (+4): rotas públicas funcionais, domínio correto, upload/download/upsell/webhook tudo implementado.
- Frontend inexistente (-6): zero HTML, zero OG tags, zero página de produto, zero painel de criador. Um link `pay.institutoitinerante.com.br/p/meu-ebook` hoje resulta em 404.
- O serviço não pode converter nenhuma venda sem um frontend externo que consuma a API.

**Para chegar a 8/10:** implementar M1 (decisão de arquitetura) + M2 (cover_image_url) + M3 (página de produto com OG) seria suficiente para viabilizar a primeira venda real.
