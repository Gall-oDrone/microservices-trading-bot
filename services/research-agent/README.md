# research-agent

Cold-path HTTP service wrapping [Gall-oDrone/TradingAgents](https://github.com/Gall-oDrone/TradingAgents). Produces research memos only — **no live order placement**.

## Endpoints

- `GET /health`
- `POST /api/v1/research/run` — body: `{"ticker":"AAPL","trade_date":"2026-03-22","include_news_context":true}`

## Environment

| Variable | Default |
|----------|---------|
| `NEWS_PUBLISHER_URL` | `http://news-publisher:8090` |
| `KAFKA_BROKERS` | empty (no memo publish) |
| `KAFKA_PUBLISH_MEMOS` | `false` — set `true` to emit `research.memos` |
| `BROKER_TARGET` | `etoro` (metadata only) |
| `RESEARCH_READ_ONLY` | `true` |
| `OPENAI_API_KEY` / `TRADINGAGENTS_*` | Required by TradingAgents |

See `docs/agentic-ai/AGENTIC-AI-ETORO-TRADINGAGENTS-INTEGRATION-PLAN-2026-05-22.md`.
