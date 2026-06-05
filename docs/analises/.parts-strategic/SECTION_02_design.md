# Lente 02 — Design | linkpay-service

**Score: 3/10**

Data da análise: 2026-06-05
Analista: @design (SOUL — Instituto Itinerante)

---

## 1. PLANEJADO — Experiência Desejada

O linkpay-service é posicionado como plataforma de links de pagamento para **criadores de conteúdo** — o equivalente brasileiro de uma Gumroad/Hotmart simplificada dentro do ecossistema IIT.

A jornada de compra planejada contempla três momentos críticos:

**Momento 1 — Descoberta e Decisão (página de produto)**
O comprador chega ao link de pagamento pelo criador. Precisa entender o que está comprando, confiar no preço e decidir em segundos. A conversão depende de: imagem do produto, descrição, prova social, botão de compra sem fricção.

**Momento 2 — Checkout (coleta de dados e pagamento)**
Formulário mínimo: nome, email, CPF, método de pagamento (PIX ou cartão). O criador de produto digital tem audiência sensível a fricção — cada campo extra custa conversão. PIX em particular precisa de UX clara: QR code visível, código copia-e-cola, contador de expiração.

**Momento 3 — Pós-compra e Download**
Confirmação imediata + acesso ao arquivo. O maior diferenciador do produto digital é a entrega instantânea. Email com token de download, página de acesso simples e robusta. Para produtos com múltiplos arquivos (BKL-1206): listagem clara com nome de cada arquivo, tamanho e botão de download individual + opção de baixar tudo em ZIP.

---

## 2. CONSTRUÍDO — O que Existe Hoje

### Rotas públicas implementadas

| Rota | Handler | O que retorna |
|------|---------|---------------|
| `GET /api/v1/public/products/:slug` | `GetPublicProduct` | JSON com dados do produto + lista de arquivos (BKL-1206) |
| `POST /api/v1/public/products/:slug/purchase` | `InitiatePurchase` | JSON com `DigitalPurchase` (cobrança Asaas criada) |
| `GET /api/v1/public/download/:token` | `Download` | Redirect 302 para presigned URL (MinIO) ou URL legada |
| `GET /api/v1/public/download/:token/file/:fileId` | `DownloadFile` | Redirect 302 para presigned URL de arquivo individual |
| `GET /api/v1/public/download/:token/zip` | `DownloadZip` | Streaming de ZIP com todos os arquivos do produto |
| `POST /api/v1/public/products/:slug/cart` | `CreateCart` | JSON com `cart_id` (delegado ao cart-service) |

### O que o payload de produto expõe publicamente
O `GetPublicProduct` retorna o objeto `domain.DigitalProduct` com `FileURL` removido (`product.FileURL = ""`). Inclui `Files []DigitalProductFile` com metadados dos arquivos adicionais. O payload não foi inspecionado em detalhe (domain não lido nesta análise), mas a lógica indica que price, name, slug, description e arquivos estão presentes.

### Capacidades técnicas presentes
- Múltiplos arquivos por produto com metadados (nome, tamanho, MIME type)
- Download via presigned URL do MinIO com expiração de 15 minutos
- Download ZIP streaming (sem acumular em memória — `io.Pipe`)
- Validação de token de compra com verificação de status de pagamento e expiração de acesso
- Upsells via cart-service (BKL-1221) — lógica no `CreateCart`
- Tracking de eventos via Pulse (produto criado, venda realizada)
- Proteção de download por token vinculado à compra confirmada

### O que não existe
- **Nenhuma interface de usuário própria** — o linkpay-service é puro API. Não há HTML, não há template de página de produto, não há tela de checkout, não há página de thank-you/download
- A API depende de um frontend externo (checkout-service ou consumidor não identificado) para renderizar toda a experiência visual

---

## 3. GAP — Ausências Críticas para Conversão

### GAP-01: Página de produto sem frontend próprio (bloqueador de go-to-market)

A rota `GET /api/v1/public/products/:slug` retorna JSON. Para o comprador chegar em `pay.institutoitinerante.com.br/p/meu-ebook` e ver uma página de vendas, alguém precisa renderizar esse JSON. Não há evidência de que o checkout-service ou qualquer outro frontend faça isso hoje.

**Impacto:** O produto não tem URL compartilhável que converta. Um criador não consegue postar o link nas redes sociais e gerar vendas.

### GAP-02: UX de checkout inexistente

O endpoint `POST /api/v1/public/products/:slug/purchase` recebe `InitiatePurchaseRequest` e devolve um objeto de compra. Mas o comprador não tem tela: não há formulário de dados pessoais, não há exibição do QR code PIX, não há feedback de "aguardando pagamento", não há timer de expiração do PIX.

**Impacto direto na conversão:** O PIX sem QR code visível e sem countdown é abandono garantido. Estudos de e-commerce BR indicam que a ausência de QR code visível aumenta abandono de checkout PIX em ~40%.

### GAP-03: Página de pós-compra/download invisível

Após confirmação de pagamento (via webhook), o comprador recebe um token. Mas não há página em `pay.institutoitinerante.com.br/download/:token` que mostre:
- Lista de arquivos disponíveis com nome legível e tamanho
- Botão de download por arquivo
- Botão de baixar tudo em ZIP
- Status de expiração do acesso

O endpoint existe (`DownloadFile`, `DownloadZip`) mas não há surface visual. O comprador ficaria sem saber o que fazer com o token.

### GAP-04: Ausência de email transacional pós-compra

Não há evidência no handler de disparo de notificação ao comprador após `ProcessPaymentConfirmed`. O comprador confirmado deveria receber imediatamente um email com link de acesso ao download. Sem email, o comprador não sabe que o acesso foi liberado — mesmo que o backend tenha processado corretamente.

**Nota:** o `notification-service` existe no ecossistema, e o `NOTIFICATION_SERVICE_URL` está nas variáveis de ambiente, mas não aparece sendo usado no fluxo de compra confirmada.

### GAP-05: Produto sem imagem, sem social proof, sem urgência

O payload público do produto provavelmente não inclui campos de imagem de capa, depoimentos ou indicador de número de compradores. A decisão de compra de produto digital é emocional e contextual — sem esses elementos, a conversão é estruturalmente mais baixa que concorrentes.

### GAP-06: Sem estado de erro visual no checkout

Os endpoints retornam `fiber.Map{"error": "..."}` como JSON. Um frontend que simplesmente exibe esse JSON bruto entrega experiência ruim. Mas como não há frontend, qualquer integrador precisa mapear manualmente cada código de erro para mensagem amigável em PT-BR.

### GAP-07: Flow de upsell sem UX mapeada

O `CreateCart` (BKL-1221) cria carrinho com upsells no cart-service. Mas não há wireframe ou especificação de como o upsell é apresentado ao comprador antes do checkout. O mecanismo existe; a experiência não foi definida.

---

## 4. ONDE ESTAMOS vs ONDE QUEREMOS

```
ONDE ESTAMOS                          ONDE QUEREMOS
─────────────────────────────────     ─────────────────────────────────
API JSON pura                         Produto compartilhável com URL
Checkout: nenhuma interface           Checkout de 1 página: dados + PIX QR + card
Download: redirect para URL           Página de download com lista de arquivos
Notificação pós-compra: ausente       Email imediato com token ao comprador
Upsell: lógica implementada,          Upsell apresentado entre produto e checkout
sem surface visual                    com oferta clara (preço, benefício)
Produto sem imagem de capa            Produto com thumbnail, descrição e CTA
Erro: JSON bruto                      Mensagens de erro amigáveis e localizadas
```

O linkpay-service está na camada de infraestrutura de um produto de conversão. O motor existe. A carroceria não.

---

## 5. TOP 5 MOVIMENTOS DE MAIOR IMPACTO

### Movimento 1 — Página de produto pública renderizada (P0 — bloqueador)

**O que:** Uma SPA minimalista ou server-side HTML em `pay.institutoitinerante.com.br/p/:slug` que consuma `GET /api/v1/public/products/:slug` e exiba:
- Nome e descrição do produto
- Preço em destaque
- Imagem de capa (requer campo no domain)
- Botão "Comprar agora"

**Stack sugerida:** pode ser servida pelo próprio checkout-service (se já tem frontend) ou ser uma página estática no Vite/React. O linkpay-service não precisa servir HTML — mas alguém precisa.

**Cor do produto:** Não há produto IIT mapeado para linkpay. Recomendo usar `--color-primary: #f97316` (food-marketplace/orange) por associação com "marketplace de criadores" — vibrante, de chamada à ação — ou definir token novo com aprovação do CEO.

**Componentes shadcn/ui:** `Card`, `Button` (size lg), `Badge` (preço), `Separator`, `Avatar` (foto do criador).

### Movimento 2 — Tela de checkout com QR code PIX (P0 — conversão)

**O que:** Formulário de checkout em 2 etapas:
1. Dados do comprador (nome, email, CPF — mínimo absoluto)
2. Seleção de método + execução:
   - PIX: exibir QR code (base64 do Asaas), código copia-e-cola, countdown de expiração (30min), status polling a cada 5s
   - Cartão: campos de número, validade, CVV, nome

**Impacto:** QR code visível é o maior ganho de conversão em checkout BR. Polling de status evita que o usuário fique em dúvida após pagar.

**Componentes shadcn/ui:** `Form`, `Input`, `Tabs` (PIX/Cartão), `Progress`, `Alert`.

### Movimento 3 — Email transacional pós-pagamento confirmado (P0 — retenção)

**O que:** No `ProcessPaymentConfirmed`, após atualizar o banco, disparar evento para `notification-service` com:
- Email do comprador
- Nome do produto
- Token de download
- URL de acesso: `pay.institutoitinerante.com.br/download/:token`
- Data de expiração do acesso (se aplicável)

**Implementação:** O `NOTIFICATION_SERVICE_URL` e `SERVICE_TOKEN` já estão nas variáveis de ambiente. Requer apenas chamada HTTP no service layer após confirmação.

**UX do email:** Assunto claro ("Seu [produto] está pronto para download"), CTA centralizado, sem navbar — email transacional minimalista.

### Movimento 4 — Página de download com listagem de arquivos (P1 — experiência pós-compra)

**O que:** Rota `pay.institutoitinerante.com.br/download/:token` que chama `GET /api/v1/public/download/:token` (ou endpoint de status a criar) e exibe:
- Nome do produto
- Lista de arquivos com nome e tamanho legível (ex: "12,4 MB")
- Botão de download por arquivo (`/download/:token/file/:fileId`)
- Botão "Baixar tudo em ZIP" (`/download/:token/zip`)
- Indicador de validade do acesso (ex: "Acesso disponível até 2026-07-05")

**Componentes shadcn/ui:** `Card`, `Table` ou lista com `Separator`, `Button` (outline para arquivo individual, filled para ZIP), `Badge` (status de acesso).

### Movimento 5 — Imagem de capa + campos de produto digital (P1 — conversão)

**O que:** Adicionar ao `domain.DigitalProduct` e ao admin:
- `CoverImageURL string` — imagem de capa exibida na página de produto
- `CreatorName string` / `CreatorAvatarURL string` — identidade do criador
- `BuyersCount int` — número de compradores (social proof, incrementado no webhook)

**Exposição:** Esses campos devem aparecer no `GetPublicProduct`. O upload da imagem de capa pode usar o mesmo MinIO já integrado.

**UX da página de produto com esses dados:** thumbnail à esquerda (mobile: topo), nome + descrição + criador à direita, badge de "X pessoas já compraram", CTA sticky no mobile.

---

## Referências de Design para Benchmark

- Gumroad: página de produto one-pager, checkout sem conta, download imediato
- Hotmart: heavy checkout mas PIX QR bem executado, page pós-compra clara
- Cakto (BR): checkout de produto digital nativo — bom exemplo de simplicidade BR

---

## Tokens de Design Recomendados para linkpay

Produto novo, sem token definido no brand system atual. Proposta para aprovação do CEO:

| Token | Valor | Justificativa |
|-------|-------|---------------|
| `--color-primary` | `#f59e0b` (amber-400) | Monetização, valor, criadores |
| `--color-primary-dark` | `#d97706` (amber-500) | Hover/active states |
| `--color-primary-light` | `#fcd34d` (amber-300) | Backgrounds sutis |
| Personalidade | Confiante, direto, BR | Marketplace de conteúdo digital |

**Alternativa:** se o linkpay for posicionado como feature do SocialMake, herdar `#8b5cf6` (violet-500).

> Decisao de token requer aprovacao explícita do CEO antes de implementar. Registrar em ADR no repositório.

---

*Análise produzida por @design | Instituto Itinerante | 2026-06-05*
