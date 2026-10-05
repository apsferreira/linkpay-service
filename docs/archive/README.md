# Archive — análises históricas

Snapshots de auditoria datados, mantidos como registro histórico — **não refletem o estado atual do código**.

| Arquivo | Data | Status |
|---------|------|--------|
| `ANALISE_2026-06-04.md` | 2026-06-04 | Superado. A maioria dos P0/P1 listados já foi corrigida (ver git log): `DownloadZip` agora bloqueia por `download_count` (closes #1), comissão consistente em 30% em código e migration, OpenTelemetry implementado, rate limiting registrado, idempotência de webhook via Redis `SetNX` (closes #8), job diário de payout ativo em `main.go`. |
| `PANORAMA_ESTRATEGICO_2026-06-05.md` | 2026-06-05 | Superado. GMV=R$0/404 na página pública foi resolvido (landing page SSR em `GET /:slug`), commission ledger existe (migration 010 + endpoint `/commission-summary`), reembolso CDC Art. 49 implementado. |

Antes de citar qualquer gap destes documentos como "pendente", confirme contra o código atual (`backend/`) e `git log` — não contra o texto aqui.

Para o estado arquitetural atual, ver `../../CONTEXT.md` (corrigido em auditoria de 2026-10).
