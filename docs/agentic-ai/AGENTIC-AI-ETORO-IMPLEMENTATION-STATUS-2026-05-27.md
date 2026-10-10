# eToro + Agentic AI — Implementation Status

Date: 2026-05-27  
Repository: `microservices-trading-bot`  
Branch: `feat/etoro-api-integration`

## Summary

This branch delivers the **cold-path research plane** (TradingAgents, S3 news ETL, Kafka topics) and wires operator-facing APIs on the **hot-path gateway** with an explicit human approval gate before any research-derived signal reaches `trading.signals`. Live eToro execution remains in `trading-engine` (`BROKER=etoro`).

**P0–P3 are code-complete** but most features are **disabled by default** in K8s (`RESEARCH_API_ENABLED=false`, `NEWS_ENABLED=false`). The work below operationalizes and hardens what is already built, then extends adjacent agentic tracks documented elsewhere.

## Completed

| Item | Location | Notes |
|------|----------|-------|
| Anthropic `LLMProvider` | `shared/pkg/agent/providers/anthropic/` | Official `github.com/anthropics/anthropic-sdk-go`; requires `ANTHROPIC_API_KEY` |
| Ops-agent LLM summaries | `services/ops-agent/internal/agent/ops_agent.go` | Uses provider when API key is set |
| News sentiment consumer (P1) | `services/strategy-executor/internal/news/` | Kafka `news.agentic` → in-memory store → buy-side gate |
| Research memo Kafka publish | `services/research-agent/app/kafka_publisher.py` | `KAFKA_PUBLISH_MEMOS=true` in research overlay |
| **Research memo S3 store (P2)** | `services/research-agent/app/s3_store.py` | `s3://{bucket}/research/memos/{run_id}.json` |
| **Operator research API (P2)** | `services/api-gateway/internal/api/research_handlers.go` | List/get memos, proxy `POST /api/v1/research/run` |
| **Approval → signal bridge (P3)** | `services/api-gateway/internal/research/signal_bridge.go` | `POST /api/v1/research/memos/{run_id}/approve` → `trading.signals` with `audit_id` |
| Approval audit in S3 | `services/api-gateway/internal/research/s3store.go` | `research/approvals/{audit_id}.json` |
| K8s env | `k8s/base/api-gateway.yaml`, `k8s/overlays/research/` | `RESEARCH_API_ENABLED`, S3 bucket refs |

## Phased roadmap

| Phase | Deliverable | Status |
|-------|-------------|--------|
| P0 | news-publisher, research-agent, research K8s overlay | **Done** |
| P1 | strategy-executor `news.agentic` sentiment filter | **Done (2026-05-25)** |
| P2 | research memo store (S3) + operator API in api-gateway | **Done (2026-05-25)** |
| P3 | Approved research → `TradeSignalEvent` bridge with audit id | **Done (2026-05-25)** |

## Next steps

Ordered by priority. Track owners and dates in your sprint board; check off here as items land.

### 1. Operationalize P0–P3 in staging (highest leverage)

P0–P3 are implemented but not turned on in overlays. Deliver **one documented e2e path** before production.

| # | Task | Owner hint | Exit criteria |
|---|------|------------|---------------|
| 1.1 | Deploy cold path: `kubectl apply -k k8s/overlays/research` | Platform | `news-publisher` and `research-agent` pods healthy in `trading-research` |
| 1.2 | Add **api-gateway IRSA** for S3 (`research/memos/`, `research/approvals/`) | Platform | Gateway SA can List/Get/Put; list memos returns 200 (not 503) |
| 1.3 | Enable on staging: `RESEARCH_API_ENABLED=true`, `RESEARCH_S3_BUCKET`, `KAFKA_BROKERS` | Platform | Operator curls below succeed |
| 1.4 | Enable news gate soak: `NEWS_ENABLED=true` on `strategy-executor` | Trading | Consumer logs `news.agentic consumer started`; BUY gating observable in logs |
| 1.5 | Staging e2e script or runbook | Platform + Trading | Run → list memo → approve → signal on `trading.signals`; eToro `DRY_RUN` or demo fill confirms book resolution (`AAPL`) |
| 1.6 | Verify cross-namespace DNS | Platform | `api-gateway` reaches `research-agent.trading-research.svc:8091` |

**Staging enable snippet:**

```bash
kubectl apply -k k8s/overlays/research

kubectl set env deployment/api-gateway -n <staging-namespace> \
  RESEARCH_API_ENABLED=true \
  RESEARCH_S3_BUCKET=test-financial-news-bucket \
  KAFKA_BROKERS=kafka:9092

kubectl set env deployment/strategy-executor -n <staging-namespace> \
  NEWS_ENABLED=true
```

### 2. Harden the research approval gate

The approve endpoint publishes to `trading.signals`. Treat it like a privileged trading action.

| # | Task | Location / notes | Exit criteria |
|---|------|------------------|---------------|
| 2.1 | Require auth on `/api/v1/research/*` when gateway auth is enabled | `api-gateway` middleware + routes | Unauthenticated approve returns 401 |
| 2.2 | **Idempotent approve** — reject or no-op if `run_id` already has an approval record | `research_handlers.go`, `s3store.go` | Second approve for same memo does not emit a second signal |
| 2.3 | **Reject / acknowledge** endpoint for HOLD and “no trade” decisions | New `POST .../memos/{run_id}/reject` | Operator can record review without publishing a signal |
| 2.4 | Optional **`instrument_id`** on approve (resolve once, store on memo or approval) | `signal_bridge.go`, `ETORO-INTEGRATION.md` | Approved signals include `metadata.instrument_id` when eToro book is set |
| 2.5 | Fail-safe audit: if S3 approval write fails after Kafka publish, surface error + compensating action doc | `research_handlers.go` | Runbook covers “signal sent, audit missing” |

### 3. News integration — post-P1 phases

See `NEWS-INTEGRATION-IMPLEMENTATION-PLAN.md` (summary table there is stale; body reflects P1–3 done).

| # | Task | Exit criteria |
|---|------|---------------|
| 3.1 | Staging tune: sentiment thresholds, TTL, cooldown (`NEWS_*` env on strategy-executor) | Documented defaults; no spurious BUY blocks in soak |
| 3.2 | Phase 4 Option A (optional): pass sentiment into strategy `Execute` context | At least one strategy reads sentiment when `NEWS_ENABLED` |
| 3.3 | Phase 6: metrics + Grafana — fetch errors, current sentiment, signals filtered by news | Dashboard row in `monitoring/grafana/` |
| 3.4 | Phase 5 (later): backtesting replay of historical sentiment | Deferred until live path stable |

### 4. `research.memos` and operator UX

| # | Task | Decision | Exit criteria |
|---|------|----------|---------------|
| 4.1 | Choose source of truth | **A:** S3-only (demote Kafka topic to optional telemetry) **B:** Add consumer | ADR or note in this doc |
| 4.2 | If B: lightweight consumer (cache latest memo per ticker for ops) | New service or api-gateway watcher | Memo list API can show “pending review” without S3 list latency |
| 4.3 | Operator UI or CLI wrapper over research HTTP API | Frontend / internal tool | Run, list, approve, reject without raw curl |

### 5. Parallel track — production reconciliation (not eToro-specific)

From `AGENTIC-AI-PRODUCTION-STRATEGY-2026-05-13.md`. Complements research memos; agents must not compute PnL.

| # | Task | Exit criteria |
|---|------|---------------|
| 5.1 | Versioned `reconciliation_report` JSON schema | Schema doc + golden file tests |
| 5.2 | Deterministic producer (canonical fills vs internal state) | Job or service writes immutable S3 artifacts |
| 5.3 | Read-only ops-agent tool: `fetch_reconciliation_report(run_id)` | Coordinator/ops-agent can narrate diffs, not derive balances |

### 6. Parallel track — ops-agent maturity

From `AGENTIC-AI-INTEGRATION-PLAN-2026-05-07.md`.

| # | Task | Exit criteria |
|---|------|---------------|
| 6.1 | Persistent audit store for ops-agent reports (replace in-memory map) | Reports survive pod restart |
| 6.2 | Audit schema for agent runs (prompt, tools, outcomes) | Shared type in `shared/pkg/agent` |
| 6.3 | LangSmith wiring for `agent-coordinator` (replace noop trace hook) | Multi-agent incidents have trace IDs |
| 6.4 | OpenAI provider SDK wiring (optional; Anthropic already works) | Config switch works end-to-end |

### 7. Documentation hygiene

| # | Task | File |
|---|------|------|
| 7.1 | Update NEWS plan summary table to match P1–3 **Done** | `NEWS-INTEGRATION-IMPLEMENTATION-PLAN.md` |
| 7.2 | Update Kafka consumer row from “planned” to “done” | `AGENTIC-AI-ETORO-TRADINGAGENTS-INTEGRATION-PLAN-2026-05-22.md` |
| 7.3 | Revisit crypto-only news ETL vs equity research tickers (`AAPL`) | `AGENTIC-AI-FINANCIAL-NEWS-S3-ETL-2026-05-22.md` or runbook note |

## Known gaps (quick reference)

| Gap | Impact |
|-----|--------|
| `RESEARCH_API_ENABLED` / `NEWS_ENABLED` default **false** | Features invisible until explicitly enabled |
| No api-gateway S3 IRSA in `k8s/` | P2/P3 APIs fail at runtime without manual IAM |
| No `research.memos` **consumer** | Operator flow is S3-centric; Kafka topic is publish-only |
| Approve is not idempotent | Duplicate signals possible for same memo |
| No reject/ack API | HOLD memos cannot be closed out in audit trail |
| No Grafana metrics for news/research path | Hard to operate sentiment gate and approvals |
| OpenAI `LLMProvider` skeleton only | TradingAgents may need OpenAI if config selects it |

## Enable research operator API

On `api-gateway` (staging/production namespace):

```bash
kubectl set env deployment/api-gateway -n <namespace> \
  RESEARCH_API_ENABLED=true \
  RESEARCH_S3_BUCKET=test-financial-news-bucket \
  KAFKA_BROKERS=kafka:9092
```

Ensure the gateway service account can `s3:GetObject`, `s3:ListBucket`, and `s3:PutObject` on `research/memos/` and `research/approvals/` (same bucket as news ETL is acceptable).

## Operator workflows

**Run research (cold path):**

```bash
curl -X POST http://api-gateway:8085/api/v1/research/run \
  -H "Content-Type: application/json" \
  -d '{"ticker":"AAPL","trade_date":"2026-03-22","include_news_context":true}'
```

**List memos:**

```bash
curl http://api-gateway:8085/api/v1/research/memos?limit=20
```

**Approve memo → trading signal (human gate):**

```bash
curl -X POST "http://api-gateway:8085/api/v1/research/memos/{run_id}/approve" \
  -H "Content-Type: application/json" \
  -d '{"operator_id":"alice","amount":100,"book":"AAPL"}'
```

Response includes `audit_id` and `signal_event_id`. The emitted `TradeSignalEvent` carries `metadata.source=research-agent`, `metadata.audit_id`, and `metadata.research_run_id`.

## Tools and credentials required from you

| Tool / secret | Used by | Purpose |
|---------------|---------|---------|
| **Anthropic API key** (`ANTHROPIC_API_KEY`) | `ops-agent`, `research-agent` / TradingAgents | Incident triage; multi-agent research |
| **OpenAI API key** (`OPENAI_API_KEY`, optional) | TradingAgents | If framework config selects OpenAI |
| **eToro API keys** (`ETORO_PUBLIC_KEY`, `ETORO_PRIVATE_KEY`) | `trading-engine` only | Live/demo execution (hot path) |
| **AWS S3** (IRSA) | `news-publisher`, `research-agent`, `api-gateway` | News ETL read; memo write/read; approval audit |
| **Kafka** | news, strategy-executor, research-agent, api-gateway | `news.agentic`, `research.memos`, `trading.signals` |
| **Go 1.23+** | `shared`, `ops-agent`, `strategy-executor`, `api-gateway` | Anthropic + AWS SDK dependencies |

No **Claude CLI (`claude`)** is required in-cluster. Use the **Anthropic Go SDK** (already integrated in `shared/pkg/agent/providers/anthropic`) or **Python SDK** inside `research-agent` via TradingAgents. The CLI is optional for local ad-hoc debugging.

## Related documents

- `docs/agentic-ai/AGENTIC-AI-ETORO-TRADINGAGENTS-INTEGRATION-PLAN-2026-05-22.md`
- `docs/agentic-ai/AGENTIC-AI-PRODUCTION-STRATEGY-2026-05-13.md`
- `docs/agentic-ai/AGENTIC-AI-INTEGRATION-PLAN-2026-05-07.md`
- `docs/ETORO-INTEGRATION.md`
- `NEWS-INTEGRATION-IMPLEMENTATION-PLAN.md`
