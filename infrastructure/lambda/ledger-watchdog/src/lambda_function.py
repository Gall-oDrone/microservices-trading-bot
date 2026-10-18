"""Off-machine watchdog for the daily executors (docs/frontend/FRONTEND-UI-PLAN-2026-10-03.md §8.10).

The executors and the R3 alerts run from the operator's workstation (scripts/install-ops-cron.sh):
if that machine is off or its cron stops, nothing runs and nothing alerts. This Lambda runs in AWS
every hour and checks the S3 copy that every run leaves (scripts/daily-executor-run.sh and
scripts/etoro-daily-executor-run.sh upload run-<UTC>.log last, after its "exit=" line): when the
newest run log under a ledger prefix is older than allowed it publishes to the operators' SNS topic.

Two calendars (per LEDGERS entry):
  name=prefix         daily: stale when the newest run log is older than MAX_AGE_HOURS (30 h; the
                      Bitso stage run is daily at 06:15 UTC).
  name=prefix:nyse    NYSE trading days (the eToro executor runs at 09:35 New York on trading days
                      only): stale when there is no run log at or after the most recent scheduled
                      run whose deadline has passed, the deadline being that run plus
                      MAX_AGE_HOURS_NYSE (6 h). Weekends and holidays never page; a missed Monday
                      run pages by about 15:35 New York. The calendar is computed here (weekends,
                      the ten scheduled NYSE holidays with their observance rules, Good Friday via
                      the anonymous Gregorian Easter, and the unscheduled closures), mirroring
                      shared/pkg/mktcal; US Eastern daylight time is applied by rule.

It keeps no state. With one invocation per clock hour (EventBridge cron(5 * * * ? *)) an alert is
sent on the first invocation after the deadline passes, then every REPEAT_HOURS while it stays
stale, and a "resolved" message on the first invocation after a run that ended a gap.
Windows are WINDOW_MINUTES wide (62: an invocation is never missed; a rare duplicate is possible).

Read-only on S3 (list + get under the ledger prefixes); publishes to one topic.

Environment:
  BUCKET              archive bucket (required)
  LEDGERS             name=prefix[:nyse][,name=prefix[:nyse]…]   (default stage=daily-executor/stage)
  TOPIC_ARN           SNS topic (required unless every event is a dry run)
  MAX_AGE_HOURS       default 30 (daily ledgers; 30 h allows a late run)
  MAX_AGE_HOURS_NYSE  default 6 (:nyse ledgers; hours after the 09:35 New York run)
  REPEAT_HOURS        default 12 (as ui-alerts -repeat)
  WINDOW_MINUTES      default 62

Events: {} (scheduled), {"dry_run": true} (evaluate and return the message, publish nothing),
{"test": true} (publish a test message).
"""

import datetime as dt
import os
import re

SUBJECT_PREFIX = "[mtb-ops] watchdog"
EXIT_RE = re.compile(r"^exit=(\S+)\s*$", re.M)
UPLOAD_RE = re.compile(r"^upload=(\S+)", re.M)
CALENDARS = ("daily", "nyse")
NYSE_RUN = (9, 35)  # the eToro executor's scheduled start, New York local time
DAY = dt.timedelta(days=1)


# --- NYSE calendar (mirrors shared/pkg/mktcal; civil dates) --------------------------------

SPECIAL_CLOSURES = {
    dt.date(2001, 9, 11): "September 11 attacks",
    dt.date(2001, 9, 12): "September 11 attacks",
    dt.date(2001, 9, 13): "September 11 attacks",
    dt.date(2001, 9, 14): "September 11 attacks",
    dt.date(2004, 6, 11): "National day of mourning (Reagan)",
    dt.date(2007, 1, 2): "National day of mourning (Ford)",
    dt.date(2012, 10, 29): "Hurricane Sandy",
    dt.date(2012, 10, 30): "Hurricane Sandy",
    dt.date(2018, 12, 5): "National day of mourning (G.H.W. Bush)",
    dt.date(2025, 1, 9): "National day of mourning (Carter)",
}


def easter(y):
    """Western Easter Sunday (anonymous Gregorian algorithm)."""
    a = y % 19
    b, c = divmod(y, 100)
    d, e = divmod(b, 4)
    f = (b + 8) // 25
    g = (b - f + 1) // 3
    h = (19 * a + b - d - g + 15) % 30
    i, k = divmod(c, 4)
    l = (32 + 2 * e + 2 * i - h - k) % 7  # noqa: E741 - the algorithm's own letters
    m = (a + 11 * h + 22 * l) // 451
    month = (h + l - 7 * m + 114) // 31
    day = (h + l - 7 * m + 114) % 31 + 1
    return dt.date(y, month, day)


def nth_weekday(y, month, weekday, n):
    """The n-th weekday (Monday=0) of a month."""
    d = dt.date(y, month, 1)
    while d.weekday() != weekday:
        d += DAY
    return d + dt.timedelta(days=7 * (n - 1))


def last_weekday(y, month, weekday):
    d = (dt.date(y + 1, 1, 1) if month == 12 else dt.date(y, month + 1, 1)) - DAY
    while d.weekday() != weekday:
        d -= DAY
    return d


def _observed(h, d, name):
    if d.weekday() == 5:
        h[d - DAY] = name + " (observed)"
    elif d.weekday() == 6:
        h[d + DAY] = name + " (observed)"
    else:
        h[d] = name


def nyse_holidays(y):
    """The scheduled NYSE holidays of a year as {date: name} (observed dates)."""
    h = {}
    ny = dt.date(y, 1, 1)
    if ny.weekday() == 6:
        h[ny + DAY] = "New Year's Day (observed)"
    elif ny.weekday() != 5:  # a Saturday Jan 1 is not observed on Dec 31
        h[ny] = "New Year's Day"
    if y >= 1998:
        h[nth_weekday(y, 1, 0, 3)] = "Martin Luther King Jr. Day"
    h[nth_weekday(y, 2, 0, 3)] = "Washington's Birthday"
    h[easter(y) - 2 * DAY] = "Good Friday"
    h[last_weekday(y, 5, 0)] = "Memorial Day"
    if y >= 2022:
        _observed(h, dt.date(y, 6, 19), "Juneteenth")
    _observed(h, dt.date(y, 7, 4), "Independence Day")
    h[nth_weekday(y, 9, 0, 1)] = "Labor Day"
    h[nth_weekday(y, 11, 3, 4)] = "Thanksgiving Day"
    _observed(h, dt.date(y, 12, 25), "Christmas Day")
    return h


def is_trading_day(d):
    if d.weekday() >= 5:
        return False
    return d not in SPECIAL_CLOSURES and d not in nyse_holidays(d.year)


def ny_offset(d):
    """New York's UTC offset on civil date d (away from the 02:00 transition instants):
    EDT (-4 h) from the second Sunday of March to the day before the first Sunday of November."""
    if nth_weekday(d.year, 3, 6, 2) <= d < nth_weekday(d.year, 11, 6, 1):
        return dt.timedelta(hours=-4)
    return dt.timedelta(hours=-5)


def ny_date(t):
    """The New York civil date of a UTC instant (DST applied by rule)."""
    d = (t + dt.timedelta(hours=-5)).date()  # EST guess
    if (t + ny_offset(d)).date() != d:  # within the hour the guess is off during EDT
        d = (t + ny_offset(d)).date()
    return d


def scheduled_run(d):
    """The UTC instant of the 09:35 New York run on trading day d."""
    local = dt.datetime(d.year, d.month, d.day, NYSE_RUN[0], NYSE_RUN[1], tzinfo=dt.timezone.utc)
    return local - ny_offset(d)


def last_scheduled(t):
    """The most recent scheduled run at or before instant t."""
    d = ny_date(t)
    for _ in range(20):
        if is_trading_day(d) and scheduled_run(d) <= t:
            return scheduled_run(d)
        d -= DAY
    raise ValueError(f"no NYSE trading day within 20 days before {t:%Y-%m-%d}")


def prev_scheduled(s):
    """The scheduled run strictly before instant s."""
    return last_scheduled(s - dt.timedelta(seconds=1))


# --- ledgers ----------------------------------------------------------------------------------


def parse_ledgers(spec):
    """'stage=daily-executor/stage,e=p:nyse' -> [('stage', 'daily-executor/stage', 'daily'), ('e', 'p', 'nyse')]."""
    out = []
    for part in (spec or "").split(","):
        part = part.strip()
        if not part:
            continue
        name, sep, prefix = part.partition("=")
        prefix, colon, cal = prefix.partition(":")
        cal = cal.strip()
        if colon and not cal:
            raise ValueError(f"bad LEDGERS entry {part!r}: empty calendar after ':'")
        cal = cal or "daily"
        if not sep or not name.strip() or not prefix.strip("/ "):
            raise ValueError(f"bad LEDGERS entry {part!r}: want name=prefix[:nyse]")
        if cal not in CALENDARS:
            raise ValueError(f"bad LEDGERS entry {part!r}: calendar must be one of {CALENDARS}")
        out.append((name.strip(), prefix.strip().strip("/"), cal))
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
    """Decide what to send for one daily ledger.

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


def evaluate_nyse(runs, now, max_age, repeat, window):
    """Decide what to send for a ledger whose run is at 09:35 New York on NYSE trading days.

    Let R be the most recent scheduled run whose deadline R + max_age has passed. The ledger is
    stale when its newest run log is older than R: the alert goes out in the first window after
    the deadline ("first" when the run before R did happen, a reminder otherwise), then every
    repeat hours; when the next trading day's deadline passes, R moves and the cycle repeats.
    Weekends and holidays have no scheduled run, so Friday's run keeps the ledger fresh until
    Monday's deadline. "Resolved" is sent once, in the window after a run log that arrived
    after its own deadline or after one or more scheduled runs were missed.
    """
    if not runs:
        if now.hour % 12 == 0:
            return "no-runs", {}
        return None
    runs = sorted(runs, key=lambda r: r[1])
    key, newest = runs[-1]
    due = last_scheduled(now - max_age)
    if newest < due:
        over = now - (due + max_age)
        if over % repeat < window:
            return "stale", {"key": key, "age": now - newest, "due": due,
                             "first": over < window and newest >= prev_scheduled(due)}
        return None
    if now - newest < window and len(runs) >= 2:
        s1, s0 = last_scheduled(newest), last_scheduled(runs[-2][1])
        if newest >= s1 + max_age or prev_scheduled(s1) > s0:
            return "resolved", {"key": key, "age": now - newest, "gap": newest - runs[-2][1]}
    return None


def evaluate_for(cal, runs, now, max_ages, repeat, window):
    """Dispatch by calendar; max_ages is {"daily": timedelta, "nyse": timedelta}."""
    if cal == "nyse":
        return evaluate_nyse(runs, now, max_ages["nyse"], repeat, window)
    return evaluate(runs, now, max_ages["daily"], repeat, window)


RUN_KEY_RE = re.compile(r"run-(\d{8}T\d{6})Z\.log$")


def run_time(key, last_modified):
    """When the run happened: the UTC stamp in the key name (the wrapper names the log at start),
    else the object's LastModified. A bulk re-send (etoro-daily-executor-run.sh --upload-only)
    gives every log the same LastModified; the names still tell the runs apart and keep the
    staleness clock honest."""
    m = RUN_KEY_RE.search(key)
    if m:
        try:
            return dt.datetime.strptime(m.group(1), "%Y%m%dT%H%M%S").replace(tzinfo=dt.timezone.utc)
        except ValueError:
            pass
    return last_modified


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
                runs.append((o["Key"], run_time(o["Key"], o["LastModified"])))
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


def render(findings, max_ages):
    """findings: [(name, prefix, cal, kind, detail, extra)] -> (subject, body)."""
    bad = [f for f in findings if f[3] != "resolved"]
    good = [f for f in findings if f[3] == "resolved"]
    bits = []
    if bad:
        bits.append(f"{len(bad)} stale")
    if good:
        bits.append(f"{len(good)} resolved")
    first = findings[0]
    lead = f"{first[0]}: " + (
        f"no run for {human(first[4]['age'])}" if first[3] == "stale"
        else "runs resumed" if first[3] == "resolved"
        else "no run logs in S3"
    )
    subject = f"{SUBJECT_PREFIX}: {', '.join(bits)} - {lead}"
    subject = subject.encode("ascii", "replace").decode()[:99]

    lines = [
        "Off-machine watchdog for the daily executors (Lambda mtb-ledger-watchdog).",
        f"A daily ledger is stale when its newest run log in S3 is older than {human(max_ages['daily'])};",
        f"an NYSE ledger when no run log follows the last trading day's {NYSE_RUN[0]:02d}:{NYSE_RUN[1]:02d} New York run"
        f" within {human(max_ages['nyse'])}.",
        "",
    ]
    for name, prefix, cal, kind, d, extra in findings:
        lines.append(f"== {name} (s3://…/{prefix}{'' if cal == 'daily' else ', NYSE trading days'})")
        if kind == "stale":
            lines.append(f"STALE: last run log {d['key'].rsplit('/', 1)[-1]}, {human(d['age'])} ago"
                         + (f"; expected run {d['due']:%Y-%m-%d %H:%M} UTC" if d.get("due") else "")
                         + ("" if d["first"] else " (reminder)"))
            lines.append("The workstation that runs the executor and the local alerts may be off, or its")
            lines.append("cron stopped: check `crontab -l` and ~/.local/state/mtb-ops/"
                         + ("etoro.log." if cal == "nyse" else "executor.log."))
        elif kind == "resolved":
            lines.append(f"RESOLVED: new run log {d['key'].rsplit('/', 1)[-1]} after a {human(d['gap'])} gap.")
        else:
            lines.append("NO RUN LOGS under this prefix: is "
                         + ("ETORO_EXECUTOR_S3_URI" if cal == "nyse" else "DAILY_EXECUTOR_S3_URI")
                         + " set in ops.env?")
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
    max_ages = {
        "daily": dt.timedelta(hours=float(env.get("MAX_AGE_HOURS", "30"))),
        "nyse": dt.timedelta(hours=float(env.get("MAX_AGE_HOURS_NYSE", "6"))),
    }
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
            f"{n}=s3://{bucket}/{p}" + ("" if c == "daily" else f" ({c})") for n, p, c in ledgers)
        sns.publish(TopicArn=topic, Subject=subject, Message=body)
        return {"sent": True, "subject": subject}

    findings, status = [], {}
    for name, prefix, cal in ledgers:
        runs = list_runs(s3, bucket, prefix)
        newest = max(runs, key=lambda r: r[1]) if runs else None
        status[name] = {
            "calendar": cal,
            "runs": len(runs),
            "newest": newest[0] if newest else None,
            "age_hours": round((now - newest[1]).total_seconds() / 3600, 2) if newest else None,
        }
        if cal == "nyse":
            status[name]["due"] = f"{last_scheduled(now - max_ages['nyse']):%Y-%m-%dT%H:%MZ}"
        res = evaluate_for(cal, runs, now, max_ages, repeat, window)
        if res is None:
            continue
        kind, d = res
        extra = {}
        if newest:
            s = run_summary(s3, bucket, newest[0])
            if s:
                extra["last run"] = s
        extra["ledger.jsonl"] = ledger_age(s3, bucket, prefix, now)
        findings.append((name, prefix, cal, kind, d, extra))

    out = {"status": status, "sent": False}
    if not findings:
        print(f"watchdog: ok {status}")
        return out
    subject, body = render(findings, max_ages)
    out.update(subject=subject, body=body)
    if dry:
        return out
    if not topic:
        raise RuntimeError("TOPIC_ARN is not set")
    sns.publish(TopicArn=topic, Subject=subject, Message=body)
    out["sent"] = True
    print(f"watchdog: sent {subject!r}")
    return out
