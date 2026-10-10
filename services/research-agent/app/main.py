import logging
import os

from fastapi import FastAPI, HTTPException
from pydantic import BaseModel, Field

from app import config
from app.news_client import fetch_news_context
from app.kafka_publisher import publish_research_memo
from app.s3_store import save_research_memo
from app.runner import run_trading_agents

logging.basicConfig(level=os.getenv("LOG_LEVEL", "INFO"))
logger = logging.getLogger("research-agent")

app = FastAPI(title="research-agent", version="0.1.0")


class ResearchRunRequest(BaseModel):
    ticker: str = Field(..., examples=["AAPL", "NVDA", "BTC-USD"])
    trade_date: str = Field(..., examples=["2026-03-22"])
    include_news_context: bool = True


@app.get("/health")
def health():
    return {"status": "ok", "read_only": config.READ_ONLY, "broker_target": config.BROKER_TARGET}


@app.post("/api/v1/research/run")
def research_run(req: ResearchRunRequest):
    if config.READ_ONLY is False:
        logger.warning("RESEARCH_READ_ONLY=false is not supported for production hot path")

    news_ctx = None
    if req.include_news_context:
        symbol = req.ticker.split("-")[0]
        news_ctx = fetch_news_context(config.NEWS_PUBLISHER_URL, symbol)

    try:
        memo = run_trading_agents(req.ticker, req.trade_date, news_ctx)
        memo["broker_target"] = config.BROKER_TARGET
        if config.KAFKA_PUBLISH_MEMOS:
            try:
                publish_research_memo(config.KAFKA_BROKERS, config.KAFKA_TOPIC_RESEARCH_MEMOS, memo)
            except Exception:
                logger.exception("failed to publish research memo to kafka")
        if config.RESEARCH_S3_ENABLED and config.RESEARCH_S3_BUCKET:
            try:
                s3_key = save_research_memo(config.RESEARCH_S3_BUCKET, config.RESEARCH_S3_PREFIX, memo)
                if s3_key:
                    memo["s3_object_key"] = s3_key
            except Exception:
                logger.exception("failed to persist research memo to S3")
        return memo
    except Exception as exc:
        logger.exception("trading agents run failed")
        raise HTTPException(status_code=500, detail=str(exc)) from exc


if __name__ == "__main__":
    import uvicorn

    uvicorn.run("app.main:app", host=config.HOST, port=config.PORT, reload=False)
