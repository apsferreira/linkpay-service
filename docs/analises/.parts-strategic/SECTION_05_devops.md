# Lente 05 — DevOps
**linkpay-service | Sweep SDD 2026-06-05**
**Score: 5/10**

---

## 1. PLANEJADO

Pipeline GitOps canônico IIT:

- Build Docker com multi-stage (Go 1.24-alpine → alpine:3.20)
- Push para `ghcr.io/apsferreira/linkpay-service:<sha>` via GitHub Actions
- Commit automático do manifest atualizado com tag SHA imutável
- ArgoCD sincroniza o manifest do repositório para K3s namespace `production`
- Cloudflare Tunnel expõe o serviço publicamente via `linkpay.institutoitinerante.com.br`
- Resource limits obrigatórios em todo Deployment
- Observabilidade: annotations Prometheus scraping + Grafana

---

## 2. CONSTRUÍDO

### Dockerfile
- **Multi-stage correto:** `golang:1.24-alpine` (builder) → `alpine:3.20` (runtime)
- `GOTOOLCHAIN=auto` presente no `RUN` de build — conforme padrão IIT
- `CGO_ENABLED=0 GOOS=linux` corretos para cross-compilation
- `go.sum` copiado explicitamente (`COPY backend/go.mod backend/go.sum ./`) — dependências verificáveis
- Sem `USER nonroot`: container roda como root

### Manifests K8s (`deploy/k8s/`)
- **Deployment:** `replicas: 1`, namespace `production`
- **Imagem em produção:** `ghcr.io/apsferreira/linkpay-service:main-latest` — tag mutável hardcoded
- Readiness e Liveness probes configuradas em `/health` porta 3020
- Resource requests/limits definidos: `50m/200m` CPU, `128Mi/256Mi` RAM
- **Ausência:** HPA, PDB, NetworkPolicy, annotations Prometheus, IngressRoute Traefik
- ConfigMap cobre: `PORT`, `MINIO_*`, `CUSTOMER_SERVICE_URL`, `CHECKOUT_SERVICE_URL`
- Service: ClusterIP porta 80 → 3020

### CI/CD (`.github/workflows/ci.yml`)
- **Job `test`:** `continue-on-error: true` — falhas de teste **não bloqueiam o build**
- **Job `build-and-push`:** produz duas tags: `main-latest` (mutável) + `sha-<short>` (imutável)
- **Job `update-manifest`:** faz `sed` no `deployment.yaml` substituindo a tag por `sha-<short>` e commita
- **Problema estrutural:** o manifest base (`deployment.yaml`) hardcoda `main-latest`; o CI sobrescreve com SHA, mas a tag `main-latest` permanece publicada e pode ser puxada diretamente por qualquer `kubectl rollout restart`
- Pipeline só dispara em push para `main` — sem branch de staging, sem ambiente de homologação
- `go mod download` e `go test` sem cache configurado (`cache: false`)

### Makefile
- Targets: `run`, `build`, `test`, `migrate`, `docker-build`, `lint`
- `migrate` usa `psql` direto com `DATABASE_URL` — sem ferramenta de migration versionada (goose, migrate)
- Sem target `deploy`, `diff`, `sync` para ArgoCD

---

## 3. GAP — Problemas Identificados

| # | Problema | Severidade | Impacto |
|---|----------|------------|---------|
| G1 | Tag `:main-latest` mutável em produção | ALTO | Rollback ambíguo; deploy não é determinístico |
| G2 | `replicas: 1` sem PDB | ALTO | Zero downtime impossível; qualquer node drain derruba o serviço |
| G3 | `continue-on-error: true` nos testes | MEDIO | Regressões chegam silenciosamente em produção |
| G4 | Sem NetworkPolicy | MEDIO | Qualquer pod no namespace pode chamar linkpay diretamente; blast radius ampliado |
| G5 | Sem annotations Prometheus | MEDIO | Métricas não coletadas; ausente no Grafana |
| G6 | Sem HPA | BAIXO | Sem auto-escalonamento sob pico de carga de pagamentos |
| G7 | Container roda como root | BAIXO | Violação de PSS `restricted`; risco de escape de container |
| G8 | Sem IngressRoute Traefik | BAIXO | Serviço não acessível externamente (assumindo CF Tunnel depende de IngressRoute) |
| G9 | `go test` sem cache (`cache: false`) | BAIXO | CI mais lento que o necessário |
| G10 | Migrations via `psql` raw sem versionamento | BAIXO | Sem controle de estado aplicado; idempotência manual |

---

## 4. ONDE ESTAMOS vs ONDE QUEREMOS

```
ONDE ESTAMOS                          ONDE QUEREMOS
─────────────────────────────────     ─────────────────────────────────
Tag mutável :main-latest em prod      Somente SHA imutável no manifest
replicas=1, sem PDB                   replicas=2 mínimo + PDB maxUnavailable=1
Testes com continue-on-error          Testes bloqueantes — falha = sem deploy
Sem NetworkPolicy                     Ingress restrito a Traefik + customer-service
Sem Prometheus scraping               Annotations + ServiceMonitor ativos
Sem HPA                               HPA cpu=70% → 2-5 replicas
Container como root                   securityContext: runAsNonRoot=true, readOnlyRootFilesystem
Sem IngressRoute                      IngressRoute Traefik + CF Tunnel configurados
Migrations psql raw                   goose ou golang-migrate com estado rastreado
```

---

## 5. TOP 5 AÇÕES (priorizadas)

### A1 — Fixar tag de imagem em produção (ALTO | ~30 min)
Remover `main-latest` do `metadata-action`. O CI já gera `sha-<short>` e já faz o `sed` no manifest. O problema é que o arquivo base continua com `main-latest`. Solução: usar um placeholder no manifest base (`IMAGE_TAG`) e substituir via `sed` somente no CI, garantindo que nenhum deploy manual possa usar `main-latest`.

Risco: baixo. Rollback: reverter o commit do manifest.

### A2 — Remover `continue-on-error: true` dos testes (ALTO | ~10 min)
Testes que falham devem interromper o pipeline. Remover o flag nos steps `test` e `Run tests`. Manter o `continue-on-error` apenas no login DockerHub (infraestrutura opcional).

Risco: zero. Benefício imediato: regressões não chegam mais em prod.

### A3 — Adicionar réplicas mínimas + PDB (MEDIO | ~20 min)
No `deployment.yaml`: `replicas: 2`. Novo arquivo `deploy/k8s/pdb.yaml`:
```yaml
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: linkpay-pdb
  namespace: production
spec:
  maxUnavailable: 1
  selector:
    matchLabels:
      app: linkpay-service
```
Risco: aumento de uso de recursos (~128Mi RAM extra). Rollback: reduzir replicas para 1.

### A4 — Annotations Prometheus + securityContext (MEDIO | ~15 min)
No `deployment.yaml`, adicionar no `template.metadata.annotations`:
```yaml
prometheus.io/scrape: "true"
prometheus.io/port: "3020"
prometheus.io/path: "/metrics"
```
E no container spec:
```yaml
securityContext:
  runAsNonRoot: true
  runAsUser: 1000
  readOnlyRootFilesystem: true
  allowPrivilegeEscalation: false
```
Risco: baixo. Testar se o binário aceita FS read-only (sem escritas em disco).

### A5 — NetworkPolicy baseline (MEDIO | ~30 min)
Novo arquivo `deploy/k8s/network-policy.yaml` com:
- Ingress permitido: somente de pods com label `app.kubernetes.io/name=traefik` (namespace `kube-system`) e de `customer-service` e `checkout-service` no namespace `production`
- Egress permitido: DNS (UDP 53), PostgreSQL VM (192.168.30.121:5432), MinIO interno, customer-service, checkout-service

Risco: médio — testar com `--dry-run=server` antes de aplicar. Rollback: deletar o recurso NetworkPolicy.

---

## Referências de Arquivos

- `/Users/antonio.ferreira/projects/linkpay-service/deploy/k8s/deployment.yaml`
- `/Users/antonio.ferreira/projects/linkpay-service/deploy/k8s/service.yaml`
- `/Users/antonio.ferreira/projects/linkpay-service/deploy/k8s/configmap.yaml`
- `/Users/antonio.ferreira/projects/linkpay-service/Dockerfile`
- `/Users/antonio.ferreira/projects/linkpay-service/.github/workflows/ci.yml`
- `/Users/antonio.ferreira/projects/linkpay-service/Makefile`

---

*Gerado por SOUL DevOps Agent — 2026-06-05*
