# Lente 04 — Backend
**linkpay-service | SDD 2026-06-05**

Score: **3/10**

---

## 1. PLANEJADO

O linkpay-service foi concebido como o gateway financeiro do ecossistema IIT: receber pagamentos de produtos digitais via Asaas, processar confirmações via webhook, depositar splits de comissão para tenants (payouts) e servir arquivos protegidos (cursos, PDFs) via MinIO. A arquitetura esperada para esse escopo inclui:

- Webhook idempotente com verificação HMAC-SHA256 real e deduplicação via Redis
- Circuit breaker e retry com backoff exponencial nas chamadas ao Asaas
- Payout agendado e orquestrado (scheduled job) com distributed lock
- Streaming de assets do MinIO sem buffer em memória
- Cobertura de testes ≥ 80% nos services
- Migrations bidirecional (UP + DOWN) para rollback controlado
- Ledger de auditoria de comissões cobradas por transação

---

## 2. CONSTRUÍDO

- **Stack:** Go 1.24 + Fiber v2 + pgx/v5 + PostgreSQL 15 + RabbitMQ + Redis + MinIO — estrutura de diretórios padrão IIT (handlers/services/repositories/models/middleware)
- **Porta:** 3020 | **URL prod:** pay.institutoitinerante.com.br
- **Gateway:** integração com Asaas (cobrança + webhook)
- **Webhook handler:** recebe eventos do Asaas, verifica token, chama ProcessPaymentConfirmed
- **Payout:** função RunDailyPayouts implementada no service layer
- **Download de assets:** endpoint de download de arquivos do MinIO
- **Migrations:** 8 arquivos sequenciais (001–008), schema de pagamentos e tenants
- **Testes:** 3 arquivos `*_test.go` em ~5940 linhas Go totais
- **CI:** pipeline configurado com `continue-on-error: true` nos steps de teste

---

## 3. GAP

| # | Severidade | Descrição |
|---|-----------|-----------|
| 1 | **P0** | **Webhook sem idempotência:** `HandleWebhook` não verifica Redis/DB se `chargeID` já foi processado antes de chamar `ProcessPaymentConfirmed`. Asaas faz retry automático em falhas de rede — resultado: cobrança duplicada, split duplicado, entrega duplicada. Nenhum `SetNX` no handler. |
| 2 | **P0** | **Validação de webhook incorreta (VUL-001 incompleto):** `hmac.Equal([]byte(h.webhookSecret), []byte(token))` compara o secret diretamente com o token recebido — não é HMAC. O correto é `HMAC-SHA256(body, secret)` e comparar o digest com o token. Qualquer cliente que conheça o secret pode forjar requisições. |
| 3 | **P0** | **RunDailyPayouts nunca chamado:** função implementada no service, mas sem nenhuma invocação em `main.go` (nem ticker, nem cron, nem RabbitMQ consumer). Payouts para tenants nunca executam em produção. |
| 4 | **P1** | **Sem circuit breaker em ProcessPayment:** chamadas ao Asaas sem timeout customizado, sem retry com backoff exponencial, sem fallback. Timeout do HTTP client default do Go (0 = infinito) — um Asaas lento pode segurar goroutines indefinidamente. |
| 5 | **P1** | **Payout sem distributed lock:** com `replicas=1` atual não é problema imediato, mas ao escalar para 2 réplicas dois pods disparam payout para o mesmo tenant simultaneamente — double payout real para o vendedor. |
| 6 | **P1** | **Download ZIP sem streaming:** arquivos são lidos do MinIO para buffer em memória antes de serem escritos na response. Produto com 500 MB = 500 MB de heap por requisição concorrente — risco de OOM no pod. |
| 7 | **P2** | **`context.Background()` em goroutines de payout:** sem deadline, uma goroutine travada em chamada ao Asaas nunca é cancelada. Leak potencial em cada ciclo de payout se Asaas não responder. |
| 8 | **P2** | **Sem ledger de comissão:** `commission_percent` existe no schema, mas não há tabela de auditoria registrando qual percentual foi aplicado em cada transação. Impossível auditar ou contestar cobranças sem reconstruir a partir dos logs. |
| 9 | **P2** | **Migrations sem DOWN:** 8 migrations, nenhuma com arquivo `.down.sql` ou bloco DOWN comentado. Rollback de emergência requer SQL manual ad hoc — viola a regra R6. |
| 10 | **P2** | **Cobertura de testes < 5%:** 3 arquivos de teste para ~5940 linhas de código Go. CI tem `continue-on-error: true` — pipeline verde mesmo com testes falhando. Nenhum service crítico (webhook, payout, split) tem teste unitário. |
| 11 | **P3** | **Inconsistência de toolchain:** `go.mod` declara `go 1.24`, mas `toolchain go1.24.13` não usa `GOTOOLCHAIN=auto` no Dockerfile (a ser confirmado). |

---

## 4. ONDE ESTAMOS vs ONDE QUEREMOS

```
DIMENSÃO                  | HOJE                          | META
--------------------------|-------------------------------|-------------------------------
Idempotência webhook      | Ausente                       | Redis SetNX por chargeID (TTL 24h)
Validação webhook         | Comparação direta (insegura)  | HMAC-SHA256(body, secret)
Payout scheduling         | Função morta (nunca chamada)  | Ticker diário em main.go + lock
Circuit breaker Asaas     | Ausente                       | gobreaker + retry backoff 3x
Distributed lock payout   | Ausente                       | Redis SETNX por tenant_id
Streaming MinIO           | Buffer em memória             | io.Pipe / MinIO GetObject stream
Context deadline          | context.Background() livre    | context.WithTimeout por operação
Ledger de comissão        | Ausente                       | Tabela commission_audit por tx
Migrations rollback       | Sem DOWN                      | .down.sql em todas as migrations
Cobertura de testes       | < 5%                          | >= 80% nos services críticos
CI gate de cobertura      | continue-on-error: true       | Hard fail abaixo de 70%
```

---

## 5. TOP 5 MOVIMENTOS

| # | Movimento | Esforço | Impacto |
|---|-----------|---------|---------|
| 1 | **Corrigir validação HMAC do webhook** — implementar `HMAC-SHA256(body, secret)` e substituir a comparação direta. Adicionar `SetNX` no Redis com TTL 24h usando `chargeID` como chave antes de chamar `ProcessPaymentConfirmed`. Retornar 200 imediatamente se já processado. | **Baixo** (2–4h) | P0 — segurança + integridade financeira |
| 2 | **Ligar RunDailyPayouts em main.go** — adicionar ticker com `time.NewTicker(24 * time.Hour)`, `context.WithTimeout` de 10 min por ciclo, e Redis SETNX `payout:lock:<date>` para garantir execução única mesmo com múltiplas réplicas. | **Baixo** (3–5h) | P0 — payouts nunca rodam hoje |
| 3 | **Circuit breaker + retry no client Asaas** — usar `sony/gobreaker` com threshold de 5 falhas consecutivas e janela de 60s. Retry com backoff exponencial (máx 3 tentativas). Configurar `http.Client` com timeout de 30s. | **Médio** (1–2 dias) | P1 — resiliência em produção |
| 4 | **Streaming de assets MinIO** — substituir buffer em memória por `io.Pipe` ou passar o `io.ReadCloser` do `GetObject` diretamente para `c.SendStream()` do Fiber. Adicionar `Content-Length` do object metadata para evitar chunked desnecessário. | **Médio** (1 dia) | P1 — elimina risco de OOM |
| 5 | **Testes unitários nos services críticos + remover continue-on-error** — cobrir `HandleWebhook` (idempotência, HMAC inválido, HMAC válido), `ProcessPaymentConfirmed` (split correto, ledger criado) e `RunDailyPayouts` (lock, skip se já executado). Remover `continue-on-error: true` do CI. | **Alto** (3–5 dias) | P2 — confiança mínima para escalar |

---

*Lente gerada em 2026-06-05 | SDD linkpay-service | Backend Specialist IIT*
