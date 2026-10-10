# Financial News S3 ETL — Data Catalog and Platform Consumption

Date: 2026-05-22  
Repository: `microservices-trading-bot`  
Bucket: `s3://test-financial-news-bucket` (us-east-1)

## 1) Objective

Document the **authoritative layout** of the financial-news dataset in S3 and how the platform consumes the **ETL (transformed)** layer in production. Raw per-article CSV shards under `news/crypto/` are archival; **services read the transformed JSONL/CSV partitions**.

## 2) Bucket inventory (verified via AWS CLI)

| Metric | Value |
|--------|-------|
| Total objects | ~27,875 |
| Total size | ~354 MB |
| Primary vertical | Crypto (`news/crypto/`, `news/transformed/crypto/`) |

### 2.1 Raw ingest (archival)

- Path pattern: `news/crypto/YYYY/MM/DD/HH/MM/SS/<article_id>.csv`
- Also daily rollups: `news/crypto/YYYY-MM-DD_full_record.csv`
- Use for **reprocessing** and ETL replays; not for hot-path consumers.

### 2.2 Transformed ETL (production read path)

Hive-style partitions with `agentic=true` flag (LLM-enriched metadata):

```
news/transformed/crypto/agentic=true/year=YYYY/month=MM/day=DD/format=jsonl/news_transformed_yYYYY_mMM_dDD.jsonl
news/transformed/crypto/agentic=true/year=YYYY/month=MM/day=DD/format=csv/news_transformed_yYYYY_mMM_dDD.csv
```

Weekly/monthly aggregates also exist, e.g.:

- `.../year=2026/week=09/format=csv/news_transformed_y2026_w09.csv`
- `.../year=2026/month=03/format=csv/news_transformed_y2026_m03.csv`

### 2.3 JSONL record schema (ETL output)

Each line is a JSON object:

| Field | Description |
|-------|-------------|
| `id` | Article id |
| `title` | Headline |
| `summary`, `body` | Text (often empty in sample rows) |
| `metadata.source` | Publisher |
| `metadata.datetime` | ISO8601 publish time |
| `metadata.url` | Canonical URL |
| `metadata.llm_ticker` | Normalized ticker when present (e.g. `BTC-USD`) |
| `metadata.llm_entities` | Entity list |
| `metadata.llm_signal` | `bullish` / `bearish` / `neutral` |
| `metadata.llm_overall_sentiment` | Float in [-1, 1] typical |
| `metadata.llm_impact_level` | `high` / `medium` / `low` |
| `metadata.llm_actionable` | Boolean |
| `metadata.llm_sectors` | e.g. `["crypto"]` |

## 3) Platform consumer: `news-publisher`

Service: `services/news-publisher`

Responsibilities:

1. Resolve the **latest** (or configured) ETL JSONL object under `news/transformed/crypto/agentic=true/`.
2. Stream-parse JSONL and emit **`NewsAgenticEvent`** records to Kafka topic `news.agentic` (configurable).
3. Expose HTTP **`GET /api/v1/news/sentiment?symbol=BTC`** for cold-path agents and operators.

Properties:

- **Optional enrichment**: trading hot path continues if news-publisher is down.
- **No live trading**: this service never places orders.
- **IRSA**: Kubernetes ServiceAccount needs `s3:GetObject` + `s3:ListBucket` on `test-financial-news-bucket`.

## 4) Configuration reference

| Env var | Default | Purpose |
|---------|---------|---------|
| `NEWS_S3_BUCKET` | `test-financial-news-bucket` | Source bucket |
| `NEWS_S3_PREFIX` | `news/transformed/crypto/agentic=true/` | ETL root |
| `NEWS_S3_FORMAT` | `jsonl` | `jsonl` or `csv` |
| `KAFKA_TOPIC_NEWS_AGENTIC` | `news.agentic` | Output topic |
| `NEWS_PUBLISH_INTERVAL` | `5m` | Poll / publish cadence |

## 5) Related documents

- `docs/agentic-ai/AGENTIC-AI-ETORO-TRADINGAGENTS-INTEGRATION-PLAN-2026-05-22.md` — TradingAgents + eToro cold path
- `NEWS-INTEGRATION-IMPLEMENTATION-PLAN.md` — strategy-executor consumer phases
- `docs/agentic-ai/AGENTIC-AI-PRODUCTION-STRATEGY-2026-05-13.md` — deterministic vs agent layers
