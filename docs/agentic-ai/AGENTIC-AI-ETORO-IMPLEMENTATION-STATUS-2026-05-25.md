# eToro + Agentic AI — Implementation Status

Date: 2026-05-25  
Repository: `microservices-trading-bot`  
Branch: `feat/etoro-api-integration`

## Summary

This branch delivers the **cold-path research plane** (TradingAgents, S3 news ETL, Kafka topics) and wires operator-facing APIs on the **hot-path gateway** with an explicit human approval gate before any research-derived signal reaches `trading.signals`. Live eToro execution remains in `trading-engine` (`BROKER=etoro`).

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
- `docs/ETORO-INTEGRATION.md`
- `NEWS-INTEGRATION-IMPLEMENTATION-PLAN.md`
