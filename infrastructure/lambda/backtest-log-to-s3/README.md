# Backtest completion log export (CloudWatch Logs → S3)

This Lambda function is triggered by a **CloudWatch Logs subscription filter** on the backtesting service log group. It writes each `backtest_completion` log event to S3 as a JSON object.

## Prerequisites

- Backtesting service logs to stdout (or a file) and those logs are collected into a **CloudWatch Log group** (e.g. `/ecs/backtesting` or `/eks/bitso-trading-dev/backtesting`).
- An S3 bucket for exported events.
- IAM: Lambda execution role with `s3:PutObject` on the bucket; CloudWatch Logs permission to invoke the Lambda.

## Setup

### 1. Create the Lambda function

- **Runtime:** Python 3.11 (or 3.12).
- **Handler:** `lambda_function.lambda_handler`.
- **Environment variables:**
  - `EXPORT_BUCKET` (required): S3 bucket name.
  - `EXPORT_PREFIX` (optional): Key prefix, default `backtests/log-export/`.
- **Timeout:** 30 seconds.
- **Memory:** 128 MB is sufficient.

No extra dependencies; only the standard library and `boto3` (included in the Lambda runtime).

### 2. Create a subscription filter on the log group

In CloudWatch Logs, add a **Subscription filter** on the backtesting service log group:

- **Filter pattern:** `{ $.event = "backtest_completion" }`  
  (if your log line is JSON with an `event` field)
- **Destination:** Lambda function → select this Lambda.

If your log format is different (e.g. the message is a raw JSON object with `event` inside), adjust the filter. For JSON lines, the pattern above works when the log line is valid JSON.

### 3. Grant CloudWatch Logs permission to invoke the Lambda

Add a resource-based policy on the Lambda so that the log group can invoke it:

```json
{
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": { "Service": "logs.<region>.amazonaws.com" },
      "Action": "lambda:InvokeFunction",
      "Resource": "arn:aws:lambda:<region>:<account>:function:backtest-log-to-s3",
      "Condition": {
        "ArnLike": { "SourceArn": "arn:aws:logs:<region>:<account>:log-group:/your-backtesting-log-group:*" }
      }
    }
  ]
}
```

Replace `<region>`, `<account>`, the function name, and the log group name.

## S3 layout

Objects are written under:

```
s3://<EXPORT_BUCKET>/<EXPORT_PREFIX>YYYY/MM/DD/<logEventId>.json
```

Each file is a JSON object containing the original log fields (e.g. `outcome`, `backtest_id`, `strategy`, metrics) plus `_log_event_id`, `_log_timestamp`, `_log_stream`, `_log_group`.

## Terraform example (optional)

You can deploy the Lambda and subscription with Terraform:

- Create a `aws_lambda_function` resource (zip the `lambda_function.py` or use a container image).
- Create `aws_cloudwatch_log_subscription_filter` with `log_group_name`, `filter_pattern = "{ $.event = \"backtest_completion\" }"`, and `destination_arn` = Lambda.
- Create `aws_lambda_permission` so `logs.amazonaws.com` can invoke the function for that log group.
- Grant the Lambda role `s3:PutObject` on the export bucket.

## Related

- **BACKTESTING-METRICS-AND-EXPORT.md** — Backtesting metrics, config snapshot, completion logging, and export options (webhook, Kafka, S3 export from the service).
