# Lente 06 — Qualidade | linkpay-service
**SDD 2026-06-05 | QA Specialist — Instituto Itinerante de Tecnologia**

---

## 1. PLANEJADO

Com base na stack Go 1.24 + Fiber v2 + pgx/v5 e nos padrões IIT, o esperado para um serviço que movimenta dinheiro real seria:

| Camada | Framework / Ferramenta | Meta de Cobertura |
|---|---|---|
| Unit (domain + service) | `testing` + `testify` | 80% em services/ e domain/ |
| Integration (handlers + DB) | `testcontainers-go` + PostgreSQL real | handlers/ críticos |
| Segurança estática | `gosec` + `govulncheck` | Sem findings HIGH/CRITICAL |
| Secrets scan | TruffleHog (`--only-verified`) | Zero leaks — blocker absoluto |
| Lint | `golangci-lint` | Zero erros em paths críticos |
| CI gate | cobertura mínima + lint + gosec | Bloquear merge se < 80% em services/ |
| Performance | k6 | Endpoints `/webhook`, `/download/:token` |

Nada disso existe hoje de forma funcional ou bloqueante.

---

## 2. CONSTRUÍDO

Três arquivos de teste identificados no repositório:

| Arquivo | O que testa | Valor real |
|---|---|---|
| `internal/domain/holdback_test.go` | Structs e cálculos de holdback (período de retenção) | Baixo — cobre apenas lógica de struct, sem integração com payout |
| `internal/domain/product_test.go` | Validações de campos de Product | Baixo — happy path de construtores |
| `internal/service/linkpay_service_test.go` | Parcial do LinkPayService | Médio — mas não cobre fluxos de pagamento confirmado nem payout |

CI configurado com `go test ./... -timeout 60s` e `continue-on-error: true`.

**Consequência direta:** falha de teste não bloqueia merge nem deploy. O CI existe como telemetria, não como gate de qualidade.

Ausências confirmadas no pipeline:
- `gosec` — zero análise de segurança estática
- `golangci-lint` — zero análise de qualidade de código
- TruffleHog — zero varredura de secrets no histórico
- Cobertura gate — sem threshold configurado
- Testes de integração — sem `testcontainers-go` ou banco real em CI

---

## 3. GAP (priorizado por risco financeiro e legal)

### P0 — Risco Financeiro Direto (bloqueiam payout real)

| Componente | Gap | Impacto |
|---|---|---|
| `payout_service.RunDailyPayouts()` | Sem nenhum teste | Payout com valor errado ou duplicado não seria detectado antes de chegar à Asaas |
| `payout_service.processTenantPayout()` | Sem nenhum teste | Transferência criada com split incorreto ou holdback não respeitado |
| `HandleWebhook / ProcessPaymentConfirmed` | Sem nenhum teste | Confirmação duplicada pode liberar download múltiplas vezes; negação indevida bloqueia produto pago |
| Idempotência do webhook | Sem nenhum teste | Asaas reenvia eventos — sem idempotência, payout pode ser processado N vezes |

### P1 — Risco de Controle de Acesso e Receita

| Componente | Gap | Impacto |
|---|---|---|
| Download token validation | Sem teste | Token expirado ou de outro tenant pode dar acesso a produto pago de terceiro |
| `AnticipationPayout` | Sem teste | Cálculo de antecipação errado gera prejuízo direto ao produtor ou ao IIT |
| Renovação de assinatura via webhook | Sem teste | Falha silenciosa desativa assinante pagante sem aviso |

### P2 — Risco Operacional

| Componente | Gap | Impacto |
|---|---|---|
| MinIO upload/download presigned URL | Sem teste | URL expirada ou gerada para bucket errado quebra entrega do produto |
| Erro de parsing de webhook (payload malformado) | Sem teste | Panic não tratado derruba o serviço inteiro |
| Holdback calculation edge cases | Parcial | Datas no limite (fim de mês, fuso) podem gerar retenção incorreta |

### P3 — Risco de Postura de Segurança (sem mitigação de pipeline)

- Sem `gosec`: SQL injection em queries dinâmicas, uso inseguro de `crypto/rand`, SSRF em callbacks Asaas — nenhum desses seria detectado hoje
- Sem TruffleHog: credenciais Asaas API key, JWT secret, DATABASE_URL podem estar no histórico git sem que ninguém saiba
- Sem `govulncheck`: dependências com CVE conhecidos ficam em produção indefinidamente

---

## 4. ONDE ESTAMOS vs ONDE QUEREMOS

| Dimensão | Hoje | Meta (R4 IIT) | Delta |
|---|---|---|---|
| Cobertura geral (estimada) | < 5% | — | — |
| Cobertura services/ (estimada) | < 3% | 80% | **−77 pp** |
| Cobertura domain/ (estimada) | ~15% (structs básicos) | 80% | −65 pp |
| Testes de integração | 0 | handlers críticos cobertos | inexistente |
| Gosec no CI | Ausente | Obrigatório (zero HIGH) | inexistente |
| TruffleHog no CI | Ausente | Obrigatório (blocker) | inexistente |
| CI gate de cobertura | Ausente | Bloqueia merge < 80% services | inexistente |
| Mutation score | Desconhecido | > 60% em payout_service | não mensurável agora |

**Bug Escape Rate:** não mensurável — não há rastreamento de bugs encontrados em produção vs. detectados em testes. Dado o volume de paths críticos sem cobertura, qualquer estimativa honesta seria alta.

---

## 5. TOP 5 MOVIMENTOS (mais urgentes, com esforço estimado)

### M1 — Testes de integração para HandleWebhook + ProcessPaymentConfirmed
**Esforço:** 2–3 dias | **Risco coberto:** P0

Usar `testcontainers-go` com PostgreSQL real. Cenários obrigatórios:
- Webhook válido confirma pagamento e libera exatamente 1 download token
- Webhook duplicado (mesmo `payment_id`) é idempotente — não gera segundo token
- Webhook com assinatura inválida retorna 401 e não persiste nada
- Payload malformado retorna 400 sem panic

Critério de aceite: CI bloqueia merge se qualquer desses falhar.

### M2 — Testes unitários para payout_service (RunDailyPayouts + processTenantPayout)
**Esforço:** 2 dias | **Risco coberto:** P0

Mock da interface Asaas client (já deve existir ou precisa ser extraída). Cenários:
- Holdback não vencido: payout não executado
- Holdback vencido, saldo correto: transferência criada com valor exato
- Falha na API Asaas: erro logado, tenant marcado como falha, outros tenants continuam
- Payout zero (sem vendas no período): chamada Asaas não disparada

### M3 — Testes de download token validation
**Esforço:** 1 dia | **Risco coberto:** P1

Cenários:
- Token válido do tenant correto: retorna presigned URL
- Token expirado: 403
- Token de outro tenant: 403 (não vazar que existe)
- Token inexistente: 404
- Token já consumido (se single-use): 410

### M4 — Adicionar gosec + TruffleHog ao CI como bloqueantes
**Esforço:** 4 horas | **Risco coberto:** P3 — segurança e secrets

```yaml
# .github/workflows/ci.yml — adicionar após go build
- name: gosec
  run: go install github.com/securego/gosec/v2/cmd/gosec@latest && gosec -severity high -confidence high ./...

- name: TruffleHog
  run: trufflehog git file://. --only-verified --fail
```

Remover `continue-on-error: true` do step de testes. Se o pipeline nunca falha, não é um gate — é decoração.

### M5 — Coverage gate no CI (threshold 80% em services/ e domain/)
**Esforço:** 2 horas | **Risco coberto:** Todos — impede regressão

```yaml
- name: Test with coverage
  run: |
    go test ./internal/service/... ./internal/domain/... \
      -coverprofile=coverage.out -timeout 60s
    go tool cover -func=coverage.out | \
      awk '/total/ {if ($3+0 < 80) {print "Coverage " $3 " < 80%"; exit 1}}'
```

Executar M1 e M2 antes de M5, senão o gate bloqueia imediatamente com a cobertura atual.

---

## Score: 1.5/10

**Justificativa:** O serviço movimenta dinheiro real via Asaas — payout, antecipação, confirmação de pagamento por webhook — e tem cobertura de testes próxima de zero nos paths que importam. O CI não bloqueia falhas. Não há análise de segurança estática, não há varredura de secrets, não há gate de cobertura. Os três arquivos de teste existentes cobrem apenas estruturas de domínio periféricas. Um bug em `processTenantPayout` ou idempotência de webhook pode causar prejuízo financeiro direto antes de ser detectado. Pela régua IIT (R1, R4), qualquer nova feature neste serviço deve ser bloqueada até M1 e M2 estarem completos.
