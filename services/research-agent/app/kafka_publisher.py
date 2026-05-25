"""Optional Kafka publisher for research.memos (cold path)."""

from __future__ import annotations

import json
import logging

logger = logging.getLogger(__name__)


def publish_research_memo(brokers: str, topic: str, memo: dict) -> None:
    brokers = (brokers or "").strip()
    topic = (topic or "").strip()
    if not brokers or not topic:
        return

    try:
        from kafka import KafkaProducer
    except ImportError as exc:
        logger.warning("kafka-python not installed; skipping memo publish: %s", exc)
        return

    event = {
        "run_id": memo.get("run_id"),
        "timestamp_ms": memo.get("timestamp_ms"),
        "ticker": memo.get("ticker"),
        "trade_date": memo.get("trade_date"),
        "decision": memo.get("decision"),
        "framework": memo.get("framework", "trading-agents"),
        "broker_target": memo.get("broker_target"),
        "news_context_used": memo.get("news_context_used", False),
        "metadata": {
            "read_only": memo.get("read_only", True),
            "warning": memo.get("warning"),
        },
    }

    producer = KafkaProducer(
        bootstrap_servers=[b.strip() for b in brokers.split(",") if b.strip()],
        value_serializer=lambda v: json.dumps(v).encode("utf-8"),
        acks="all",
        retries=3,
    )
    try:
        future = producer.send(topic, event)
        future.get(timeout=15)
        logger.info("published research memo to %s run_id=%s", topic, event.get("run_id"))
    finally:
        producer.close()
