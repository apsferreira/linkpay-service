# Lente 11 — Legal

> **Serviço:** linkpay-service | **Data:** 2026-06-05 | **Autor:** @legal (SOUL — IIT)

---

## PLANEJADO

O linkpay-service coleta e processa dados pessoais de compradores (nome, email, CPF/CNPJ) e de criadores (pix_key, email). É intermediário de pagamentos, sujeito a LGPD (Lei 13.709/2018), CDC (Lei 8.078/1990) e potencialmente à regulação de pagamentos do Bacen (Lei 12.865/2013). O design original não menciona compliance explicitamente.

---

## CONSTRUÍDO

**Proteções existentes:**
- Webhook HMAC validation (VUL-001): impede confirmação falsa de pagamento por terceiros
- JWT middleware: endpoints admin protegidos por autenticação
- `buyer_document` armazenado para fins de emissão de nota fiscal
- Download token com expiração: controle de acesso a produto pago

---

## GAP

### Gap 1 — LGPD Art. 46: dados pessoais sem prazo de retenção
`buyer_email` e `buyer_document` (CPF/CNPJ) armazenados em `digital_purchases` indefinidamente. Sem policy de exclusão automática após período legal (5 anos para fins fiscais, conforme CTN Art. 195).

**Risco:** multa ANPD de até 2% do faturamento, máximo R$50M por infração.

### Gap 2 — LGPD Art. 18: sem endpoint de exclusão de dados do comprador
Não há DELETE /api/v1/purchases/:id nem mecanismo de anonimização. Comprador não pode exercer direito de exclusão.

### Gap 3 — CDC Art. 49: reembolso em 7 dias não implementado
Para produtos digitais, o CDC Art. 49 garante direito de arrependimento em 7 dias de compra remota. Não há endpoint de reembolso para o comprador. `RequestPayout` é para o criador, não para o comprador.

**Risco:** PROCON pode multar e obrigar reembolso retroativo de toda a base.

### Gap 4 — DPA com Asaas não documentado
Dados de compradores (CPF, email) são transmitidos ao Asaas para cobrança. Sem DPA (Data Processing Agreement) assinado, o IIT é solidariamente responsável por qualquer vazamento de dados processado pelo Asaas.

### Gap 5 — buyer_email em logs de erro
`linkpay_service.go:280` loga erro referenciando `buyer_email fallback` — possível exposição de email em logs de produção. Logs do K3s são retidos indefinidamente sem mascaramento.

**Risco:** LGPD Art. 46 — medidas técnicas inadequadas de segurança.

### Gap 6 — Lei 12.865/2013: limiar de intermediário de pagamentos
Se o volume processado ultrapassar limites do Bacen (R$500M/ano), o IIT precisa de autorização como Instituição de Pagamento. Monitorar conforme escala.

### Gap 7 — Termos de Uso do criador sem aceite registrado
Criadores onboardam sem aceitar Termos de Uso que defina: taxa de comissão, prazo de repasse, política de chargebacks, regras de conteúdo proibido. Sem aceite registrado, qualquer disputa de comissão é desvantajosa para o IIT.

---

## ONDE ESTAMOS vs ONDE QUEREMOS

| Obrigação Legal | Hoje | Meta |
|---|---|---|
| Retenção de dados LGPD | Indefinida | 5 anos + exclusão automática |
| Direito de exclusão (Art. 18) | Não existe | DELETE /data/buyer/:id |
| Reembolso CDC Art. 49 | Não existe | POST /purchases/:id/refund (7 dias) |
| DPA com Asaas | Não documentado | DPA assinado + registrado |
| Logs com PII | buyer_email em log de erro | Mascaramento: email → sha256 |
| Aceite de Termos do criador | Sem registro | contract_acceptances na criação de conta |
| NFS-e sobre comissão | Sem emissão | billing-service via Padrão ABRASF |

---

## TOP 5 MOVIMENTOS

### Movimento 1 — Implementar POST /purchases/:id/refund (CDC Art. 49) — P0
Endpoint que: verifica se purchase.created_at < 7 dias, cancela o download_token, solicita estorno ao Asaas, atualiza status da purchase para REFUNDED. Sem isso, qualquer reclamação no PROCON resulta em multa + reembolso forçado.
Esforço: 1 dia backend.

### Movimento 2 — Mascarar buyer_email nos logs — P1
Substituir `log.Printf` que referencia email por hash SHA256 dos primeiros 3 chars + domínio. Aplicar via wrapper de log. Esforço: 2h.

### Movimento 3 — Prazo de retenção de dados (LGPD) — P1
Migration: adicionar `expires_at = created_at + 5 years` em digital_purchases. Cron job mensal que anonimiza registros expirados (buyer_email → NULL, buyer_document → NULL). Esforço: 4h.

### Movimento 4 — DPA Asaas: solicitar assinatura formal — P2
Ação jurídica/comercial, não técnica. Contatar Asaas para assinar DPA padrão LGPD. Necessário para qualquer auditoria ou certificação futura. Esforço: 0h técnico.

### Movimento 5 — Aceite de Termos do criador — P2
Adicionar `accepted_terms_at` em creator_configs. No onboarding, exibir Termos de Uso com checkbox obrigatório antes de criar primeiro produto. Esforço: 4h backend + 2h frontend.

---

*Score: 3/10 — proteções básicas existem (HMAC, JWT) mas compliance LGPD/CDC está em risco real.*
