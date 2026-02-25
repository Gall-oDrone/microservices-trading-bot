"""
CloudWatch Logs → S3 export for backtest_completion events.

Subscribe this Lambda to the backtesting service log group with a filter pattern:
  { $.event = "backtest_completion" }

Environment variables:
  EXPORT_BUCKET: S3 bucket name (required)
  EXPORT_PREFIX: Key prefix (default: backtests/log-export/)
"""

import json
import os
from datetime import datetime

import boto3

EXPORT_BUCKET = os.environ.get("EXPORT_BUCKET", "")
EXPORT_PREFIX = (os.environ.get("EXPORT_PREFIX", "backtests/log-export/")).rstrip("/") + "/"
s3 = boto3.client("s3")


def lambda_handler(event, context):
    if not EXPORT_BUCKET:
        raise ValueError("EXPORT_BUCKET environment variable is required")

    log_events = event.get("logEvents", [])
    written = 0
    for log_event in log_events:
        message = log_event.get("message", "").strip()
        if not message:
            continue
        try:
            # Try to parse as JSON (structured log line)
            data = json.loads(message)
        except json.JSONDecodeError:
            continue
        if data.get("event") != "backtest_completion":
            continue
        # Add metadata from CloudWatch
        data["_log_event_id"] = log_event.get("id")
        data["_log_timestamp"] = log_event.get("timestamp")
        data["_log_stream"] = event.get("logStream", "")
        data["_log_group"] = event.get("logGroup", "")

        ts = log_event.get("timestamp", 0)
        try:
            dt = datetime.utcfromtimestamp(ts / 1000.0)
        except (TypeError, OSError):
            dt = datetime.utcnow()
        key = f"{EXPORT_PREFIX}{dt.year:04d}/{dt.month:02d}/{dt.day:02d}/{log_event.get('id', written)}.json"
        body = json.dumps(data, default=str)
        s3.put_object(
            Bucket=EXPORT_BUCKET,
            Key=key,
            Body=body,
            ContentType="application/json",
        )
        written += 1
    return {"statusCode": 200, "exported": written}
