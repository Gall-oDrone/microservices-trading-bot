# news-publisher

Cold-path service that reads **ETL financial news** from S3 (`test-financial-news-bucket`) and publishes `NewsAgenticEvent` records to Kafka (`news.agentic`).

See `docs/agentic-ai/AGENTIC-AI-FINANCIAL-NEWS-S3-ETL-2026-05-22.md`.

## Run locally

```bash
export NEWS_S3_BUCKET=test-financial-news-bucket
export KAFKA_BROKERS=localhost:9092
cd services/news-publisher && go run ./cmd
```

## HTTP

- `GET /health`
- `GET /api/v1/news/sentiment?symbol=BTC`
- `GET /api/v1/news/snapshot`
