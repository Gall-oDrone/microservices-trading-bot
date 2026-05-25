"""Persist research memos to S3 for operator review (cold path)."""

from __future__ import annotations

import json
import logging
from datetime import datetime, timezone

logger = logging.getLogger(__name__)


def save_research_memo(bucket: str, prefix: str, memo: dict) -> str | None:
    bucket = (bucket or "").strip()
    if not bucket:
        return None

    run_id = memo.get("run_id")
    if not run_id:
        logger.warning("memo missing run_id; skipping S3 persist")
        return None

    try:
        import boto3
    except ImportError as exc:
        logger.warning("boto3 not installed; skipping S3 persist: %s", exc)
        return None

    prefix = (prefix or "research/memos/").strip()
    if prefix and not prefix.endswith("/"):
        prefix += "/"
    key = f"{prefix}{run_id}.json"

    body = json.dumps(memo, default=str).encode("utf-8")
    client = boto3.client("s3")
    client.put_object(
        Bucket=bucket,
        Key=key,
        Body=body,
        ContentType="application/json",
        Metadata={
            "run_id": str(run_id),
            "ticker": str(memo.get("ticker", "")),
            "stored_at": datetime.now(timezone.utc).isoformat(),
        },
    )
    logger.info("stored research memo s3://%s/%s", bucket, key)
    return key
