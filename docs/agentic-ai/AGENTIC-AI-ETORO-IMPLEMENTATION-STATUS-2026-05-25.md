# eToro + Agentic AI — Implementation Status

Date: 2026-05-25  
Repository: `microservices-trading-bot`  
Branch: `feat/etoro-api-integration`

## Summary

This iteration completes **P1** from `AGENTIC-AI-ETORO-TRADINGAGENTS-INTEGRATION-PLAN-2026-05-22.md` and wires the **Anthropic Go SDK** for `ops-agent` LLM summaries. Live eToro execution remains on the hot path (`trading-engine`, `BROKER=etoro`); research and news stay on the cold path.

## Completed in this change

| Item | Location | Notes |
|------|----------|-------|
| Anthropic `LLMProvider` | `shared/pkg/agent/providers/anthropic/` | Uses `github.com/anthropics/anthropic-sdk-go`; requires `ANTHROPIC_API_KEY` |
| Ops-agent LLM summaries | `services/ops-agent/internal/agent/ops_agent.go` | Uses provider output when API key is set |
| News sentiment consumer (P1) | `services/strategy-executor/internal/news/` | Kafka `news.agentic` → in-memory store → buy-side gate before `trading.signals` |
| Strategy-executor config | `NEWS_ENABLED`, `KAFKA_TOPIC_NEWS_AGENTIC`, thresholds | Default `NEWS_ENABLED=false` |
| Research memo Kafka publish | `services/research-agent/app/kafka_publisher.py` | `KAFKA_PUBLISH_MEMOS=true` in research overlay |
| K8s env | `k8s/base/strategy-executor.yaml`, `k8s/overlays/research/research-agent.yaml` | Topic refs from ConfigMaps |

## Phased roadmap (updated)

| Phase | Deliverable | Status |
|-------|-------------|--------|
| P0 | news-publisher, research-agent, research K8s overlay | Done (2026-05-22) |
| P1 | strategy-executor `news.agentic` sentiment filter | **Done (2026-05-25)** |
| P2 | research memo store (S3) + operator API in api-gateway | Next |
| P3 | Approved research → `TradeSignalEvent` bridge with audit id | Next |

## Tools and credentials required from you

| Tool / secret | Used by | Purpose |
|---------------|---------|---------|
| **Anthropic API key** (`ANTHROPIC_API_KEY`) | `ops-agent`, TradingAgents via `research-agent` | Incident triage summaries; multi-agent research |
| **OpenAI API key** (`OPENAI_API_KEY`, optional) | TradingAgents / `research-agent` | If TradingAgents config selects OpenAI |
| **eToro API keys** (`ETORO_PUBLIC_KEY`, `ETORO_PRIVATE_KEY`) | `trading-engine` only | Live/demo execution (hot path) |
| **AWS S3** (IRSA on `news-publisher`) | `news-publisher` | Read ETL bucket `test-financial-news-bucket` |
| **Kafka** | `news-publisher`, `strategy-executor`, `research-agent` | Topics `news.agentic`, `research.memos`, `trading.signals` |
| **Go 1.23+** | `shared`, `ops-agent`, `strategy-executor` | Anthropic SDK dependency |

No **Claude CLI (`ant`)** is required in-cluster; use it locally for ad-hoc API debugging if desired.

## Enable news gating in staging

```bash
kubectl set env deployment/strategy-executor -n <namespace> NEWS_ENABLED=true
```

Tune thresholds with `NEWS_MIN_SENTIMENT_FOR_BUY`, `NEWS_BLOCK_BEARISH_BUY`, `NEWS_HIGH_IMPACT_COOLDOWN`.

## Related documents

- `docs/agentic-ai/AGENTIC-AI-ETORO-TRADINGAGENTS-INTEGRATION-PLAN-2026-05-22.md`
- `docs/ETORO-INTEGRATION.md`
- `NEWS-INTEGRATION-IMPLEMENTATION-PLAN.md`
