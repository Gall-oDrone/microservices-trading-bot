"""Off-machine watchdog for the daily executor (docs/frontend/FRONTEND-UI-PLAN-2026-10-03.md §8.10).

The executor and the R3 alerts run from the operator's workstation (scripts/install-ops-cron.sh):
if that machine is off or its cron stops, nothing runs and nothing alerts. This Lambda runs in AWS
every hour and checks the S3 copy that every run leaves (scripts/daily-executor-run.sh uploads
run-<UTC>.log last, after its "exit=" line): when the newest run log under a ledger prefix is older
than MAX_AGE_HOURS it publishes to the operators' SNS topic.

It keeps no state. With one invocation per clock hour (EventBridge cron(5 * * * ? *)) an alert is
sent on the first invocation after the age crosses MAX_AGE_HOURS, then every REPEAT_HOURS while
it stays stale, and a "resolved" message on the first invocation after a run that ended a gap.
Windows are WINDOW_MINUTES wide (62: an invocation is never missed; a rare duplicate is possible).

Read-only on S3 (list + get under the ledger prefixes); publishes to one topic.

Environment:
  BUCKET           archive bucket (required)
  LEDGERS          name=prefix[,name=prefix…]   (default stage=daily-executor/stage)
  TOPIC_ARN        SNS topic (required unless every event is a dry run)
  MAX_AGE_HOURS    default 30 (the run is daily at 06:15 UTC; 30 h allows a late run)
  REPEAT_HOURS     default 12 (as ui-alerts -repeat)
  WINDOW_MINUTES   default 62

Events: {} (scheduled), {"dry_run": true} (evaluate and return the message, publish nothing),
{"test": true} (publish a test message).
"""

import datetime as dt
import os
import re

SUBJECT_PREFIX = "[mtb-ops] watchdog"
EXIT_RE = re.compile(r"^exit=(\S+)\s*$", re.M)
UPLOAD_RE = re.compile(r"^upload=(\S+)", re.M)


def parse_ledgers(spec):
    """'stage=daily-executor/stage,x=p' -> [('stage', 'daily-executor/stage'), ('x', 'p')]."""
    out = []
    for part in (spec or "").split(","):
        part = part.strip()
        if not part:
            continue
        name, sep, prefix = part.partition("=")
        if not sep or not name.strip() or not prefix.strip("/ "):
            raise ValueError(f"bad LEDGERS entry {part!r}: want name=prefix")
        out.append((name.strip(), prefix.strip().strip("/")))
    if not out:
        raise ValueError("LEDGERS is empty")
    return out


def human(d):
    """A timedelta as '31 h' / '2 d 4 h' / '45 min'."""
    m = int(d.total_seconds() // 60)
    if m < 60:
        return f"{m} min"
    h = m // 60
    if h < 48:
        return f"{h} h"
    return f"{h // 24} d {h % 24} h"


def evaluate(runs, now, max_age, repeat, window):
    """Decide what to send for one ledger.

    runs: [(key, last_modified)] of the run-*.log objects, any order.
    Returns (kind, detail) with kind in {"stale", "resolved", "no-runs"} or None.
    """
    if not runs:
        # No baseline to measure from: remind at the 00 and 12 UTC invocations only.
        if now.hour % 12 == 0:
            return "no-runs", {}
        return None
    runs = sorted(runs, key=lambda r: r[1])
    key, newest = runs[-1]
    age = now - newest
    if age >= max_age:
        over = age - max_age
        if over % repeat < window:
            return "stale", {"key": key, "age": age, "first": over < window}
        return None
    if age < window and len(runs) >= 2:
        gap = newest - runs[-2][1]
        if gap >= max_age:
            return "resolved", {"key": key, "age": age, "gap": gap}
    return None


def list_runs(s3, bucket, prefix):
    runs = []
    token = None
    while True:
        kw = {"Bucket": bucket, "Prefix": prefix + "/run-"}
        if token:
            kw["ContinuationToken"] = token
        page = s3.list_objects_v2(**kw)
        for o in page.get("Contents", []):
            if o["Key"].endswith(".log"):
                runs.append((o["Key"], o["LastModified"]))
        if not page.get("IsTruncated"):
            return runs
        token = page.get("NextContinuationToken")


def run_summary(s3, bucket, key):
    """'exit=0, upload=ok' from the run log's tail, or '' when unreadable."""
    try:
        body = s3.get_object(Bucket=bucket, Key=key)["Body"].read()[-65536:].decode("utf-8", "replace")
    except Exception as e:  # noqa: BLE001 - informational only
        print(f"watchdog: cannot read s3://{bucket}/{key}: {e}")
        return ""
    parts = []
    m = EXIT_RE.findall(body)
    if m:
        parts.append(f"exit={m[-1]}")
    m = UPLOAD_RE.findall(body)
    if m:
        parts.append(f"upload={m[-1]}")
    return ", ".join(parts)


def ledger_age(s3, bucket, prefix, now):
    try:
        h = s3.head_object(Bucket=bucket, Key=prefix + "/ledger.jsonl")
    except Exception:  # noqa: BLE001 - informational only
        return "missing or unreadable"
    return f"{h['LastModified']:%Y-%m-%d %H:%M} UTC ({human(now - h['LastModified'])} ago)"


def render(findings, max_age):
    """findings: [(name, prefix, kind, detail, extra)] -> (subject, body)."""
    bad = [f for f in findings if f[2] != "resolved"]
    good = [f for f in findings if f[2] == "resolved"]
    bits = []
    if bad:
        bits.append(f"{len(bad)} stale")
    if good:
        bits.append(f"{len(good)} resolved")
    first = findings[0]
    lead = f"{first[0]}: " + (
        f"no run for {human(first[3]['age'])}" if first[2] == "stale"
        else "runs resumed" if first[2] == "resolved"
        else "no run logs in S3"
    )
    subject = f"{SUBJECT_PREFIX}: {', '.join(bits)} - {lead}"
    subject = subject.encode("ascii", "replace").decode()[:99]

    lines = [
        "Off-machine watchdog for the daily executor (Lambda mtb-ledger-watchdog).",
        f"A ledger is stale when its newest run log in S3 is older than {human(max_age)}.",
        "",
    ]
    for name, prefix, kind, d, extra in findings:
        lines.append(f"== {name} (s3://…/{prefix})")
        if kind == "stale":
            lines.append(f"STALE: last run log {d['key'].rsplit('/', 1)[-1]}, {human(d['age'])} ago"
                         + ("" if d["first"] else " (reminder)"))
            lines.append("The workstation that runs the executor and the local alerts may be off, or its")
            lines.append("cron stopped: check `crontab -l` and ~/.local/state/mtb-ops/executor.log.")
        elif kind == "resolved":
            lines.append(f"RESOLVED: new run log {d['key'].rsplit('/', 1)[-1]} after a {human(d['gap'])} gap.")
        else:
            lines.append("NO RUN LOGS under this prefix: is DAILY_EXECUTOR_S3_URI set in ops.env?")
        for k, v in extra.items():
            lines.append(f"  {k}: {v}")
        lines.append("")
    lines.append("Data health (on the workstation): http://127.0.0.1:5173/data-health")
    return subject, "\n".join(lines)


def lambda_handler(event, context, s3=None, sns=None, now=None):
    event = event or {}
    env = os.environ
    bucket = env["BUCKET"]
    ledgers = parse_ledgers(env.get("LEDGERS", "stage=daily-executor/stage"))
    max_age = dt.timedelta(hours=float(env.get("MAX_AGE_HOURS", "30")))
    repeat = dt.timedelta(hours=float(env.get("REPEAT_HOURS", "12")))
    window = dt.timedelta(minutes=float(env.get("WINDOW_MINUTES", "62")))
    now = now or dt.datetime.now(dt.timezone.utc)
    dry = bool(event.get("dry_run"))
    if s3 is None or sns is None:
        import boto3  # in the Lambda runtime; imported here so tests need no boto3

        s3 = s3 or boto3.client("s3")
        sns = sns or boto3.client("sns")
    topic = env.get("TOPIC_ARN", "")

    if event.get("test"):
        subject = f"{SUBJECT_PREFIX}: test message"
        body = f"Test from mtb-ledger-watchdog at {now:%Y-%m-%d %H:%M} UTC; ledgers: " + ", ".join(
            f"{n}=s3://{bucket}/{p}" for n, p in ledgers)
        sns.publish(TopicArn=topic, Subject=subject, Message=body)
        return {"sent": True, "subject": subject}

    findings, status = [], {}
    for name, prefix in ledgers:
        runs = list_runs(s3, bucket, prefix)
        newest = max(runs, key=lambda r: r[1]) if runs else None
        status[name] = {
            "runs": len(runs),
            "newest": newest[0] if newest else None,
            "age_hours": round((now - newest[1]).total_seconds() / 3600, 2) if newest else None,
        }
        res = evaluate(runs, now, max_age, repeat, window)
        if res is None:
            continue
        kind, d = res
        extra = {}
        if newest:
            s = run_summary(s3, bucket, newest[0])
            if s:
                extra["last run"] = s
        extra["ledger.jsonl"] = ledger_age(s3, bucket, prefix, now)
        findings.append((name, prefix, kind, d, extra))

    out = {"status": status, "sent": False}
    if not findings:
        print(f"watchdog: ok {status}")
        return out
    subject, body = render(findings, max_age)
    out.update(subject=subject, body=body)
    if dry:
        return out
    if not topic:
        raise RuntimeError("TOPIC_ARN is not set")
    sns.publish(TopicArn=topic, Subject=subject, Message=body)
    out["sent"] = True
    print(f"watchdog: sent {subject!r}")
    return out
