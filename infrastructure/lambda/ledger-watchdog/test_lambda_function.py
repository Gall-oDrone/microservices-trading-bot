"""Tests for the ledger watchdog: python3 -m unittest discover -s infrastructure/lambda/ledger-watchdog"""

import datetime as dt
import io
import os
import sys
import unittest
from unittest import mock

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "src"))
import lambda_function as lf  # noqa: E402

UTC = dt.timezone.utc
H = dt.timedelta(hours=1)
MAX, REP, WIN = 30 * H, 12 * H, dt.timedelta(minutes=62)


def t(s):
    return dt.datetime.strptime(s, "%Y-%m-%d %H:%M").replace(tzinfo=UTC)


class FakeS3:
    def __init__(self, objects, page=1000, fail_get=False):
        self.objects = dict(objects)  # key -> (last_modified, body)
        self.page = page
        self.fail_get = fail_get
        self.lists = 0

    def list_objects_v2(self, Bucket, Prefix, ContinuationToken=None):
        self.lists += 1
        keys = sorted(k for k in self.objects if k.startswith(Prefix))
        start = int(ContinuationToken or 0)
        chunk = keys[start:start + self.page]
        out = {"Contents": [{"Key": k, "LastModified": self.objects[k][0]} for k in chunk]}
        if start + self.page < len(keys):
            out.update(IsTruncated=True, NextContinuationToken=str(start + self.page))
        return out

    def get_object(self, Bucket, Key):
        if self.fail_get or Key not in self.objects:
            raise RuntimeError("NoSuchKey")
        return {"Body": io.BytesIO(self.objects[Key][1].encode())}

    def head_object(self, Bucket, Key):
        if Key not in self.objects:
            raise RuntimeError("404")
        return {"LastModified": self.objects[Key][0]}


class FakeSNS:
    def __init__(self):
        self.sent = []

    def publish(self, TopicArn, Subject, Message):
        self.sent.append((TopicArn, Subject, Message))


def runs(*times):
    return [(f"daily-executor/stage/run-{i}.log", t(s)) for i, s in enumerate(times)]


class EvaluateTest(unittest.TestCase):
    def ev(self, rs, now):
        return lf.evaluate(rs, t(now), MAX, REP, WIN)

    def test_fresh_is_quiet(self):
        self.assertIsNone(self.ev(runs("2026-10-08 06:20"), "2026-10-08 20:05"))
        self.assertIsNone(self.ev(runs("2026-10-08 06:20"), "2026-10-09 12:05"))  # 29 h 45

    def test_first_alert_once_per_crossing(self):
        rs = runs("2026-10-08 06:20")  # crosses 30 h at 2026-10-09 12:20
        self.assertIsNone(self.ev(rs, "2026-10-09 12:05"))
        kind, d = self.ev(rs, "2026-10-09 13:05")
        self.assertEqual(kind, "stale")
        self.assertTrue(d["first"])
        self.assertEqual(d["age"], dt.timedelta(hours=30, minutes=45))
        self.assertIsNone(self.ev(rs, "2026-10-09 14:05"))

    def test_hourly_invocations_never_miss_and_remind_every_12h(self):
        rs = runs("2026-10-08 06:20")
        now, sends = t("2026-10-08 07:05"), []
        for _ in range(72):  # three days of hourly invocations, with a few seconds of jitter
            res = lf.evaluate(rs, now + dt.timedelta(seconds=7), MAX, REP, WIN)
            if res:
                sends.append((now, res[1]["first"]))
            now += H
        self.assertEqual([s[0] for s in sends],
                         [t("2026-10-09 13:05"), t("2026-10-10 01:05"), t("2026-10-10 13:05"),
                          t("2026-10-11 01:05")])
        self.assertEqual([s[1] for s in sends], [True, False, False, False])

    def test_resolved_after_a_gap_only(self):
        gap = runs("2026-10-05 22:53", "2026-10-08 06:40")
        kind, d = self.ev(gap, "2026-10-08 07:05")
        self.assertEqual(kind, "resolved")
        self.assertEqual(d["gap"], dt.timedelta(days=2, hours=7, minutes=47))
        self.assertIsNone(self.ev(gap, "2026-10-08 08:05"))
        # a normal daily cadence never "resolves"
        self.assertIsNone(self.ev(runs("2026-10-07 06:30", "2026-10-08 06:40"), "2026-10-08 07:05"))
        # a single run has nothing to resolve
        self.assertIsNone(self.ev(runs("2026-10-08 06:40"), "2026-10-08 07:05"))

    def test_order_does_not_matter(self):
        rs = list(reversed(runs("2026-10-01 06:20", "2026-10-08 06:20")))
        self.assertEqual(self.ev(rs, "2026-10-09 13:05")[0], "stale")

    def test_no_runs_reminds_at_00_and_12_only(self):
        self.assertEqual(self.ev([], "2026-10-08 12:05")[0], "no-runs")
        self.assertEqual(self.ev([], "2026-10-09 00:05")[0], "no-runs")
        self.assertIsNone(self.ev([], "2026-10-08 13:05"))


class ParseTest(unittest.TestCase):
    def test_ledgers(self):
        self.assertEqual(lf.parse_ledgers("stage=daily-executor/stage/, dry=x/y"),
                         [("stage", "daily-executor/stage", "daily"), ("dry", "x/y", "daily")])
        self.assertEqual(lf.parse_ledgers("stage=daily-executor/stage,etoro-demo=daily-executor/etoro-demo:nyse"),
                         [("stage", "daily-executor/stage", "daily"),
                          ("etoro-demo", "daily-executor/etoro-demo", "nyse")])
        self.assertEqual(lf.parse_ledgers("e=daily-executor/etoro-demo/:nyse"),
                         [("e", "daily-executor/etoro-demo", "nyse")])
        for bad in ["", "stage", "=x", "stage=", "stage=/", "stage=:nyse", "e=p:lunar", "e=p:"]:
            with self.assertRaises(ValueError, msg=bad):
                lf.parse_ledgers(bad)

    def test_human(self):
        self.assertEqual(lf.human(dt.timedelta(minutes=45)), "45 min")
        self.assertEqual(lf.human(dt.timedelta(hours=31, minutes=5)), "31 h")
        self.assertEqual(lf.human(dt.timedelta(hours=52)), "2 d 4 h")


class NyseCalendarTest(unittest.TestCase):
    """The published lists in shared/pkg/mktcal/mktcal_test.go."""

    HOLIDAYS = {
        2026: ["2026-01-01", "2026-01-19", "2026-02-16", "2026-04-03", "2026-05-25", "2026-06-19",
               "2026-07-03", "2026-09-07", "2026-11-26", "2026-12-25"],
        2027: ["2027-01-01", "2027-01-18", "2027-02-15", "2027-03-26", "2027-05-31", "2027-06-18",
               "2027-07-05", "2027-09-06", "2027-11-25", "2027-12-24"],
    }

    def test_holiday_lists_match_mktcal(self):
        for y, want in self.HOLIDAYS.items():
            got = sorted(d.isoformat() for d in lf.nyse_holidays(y))
            self.assertEqual(got, want, y)
            for d in want:
                self.assertFalse(lf.is_trading_day(dt.date.fromisoformat(d)), d)

    def test_observance_names(self):
        self.assertEqual(lf.nyse_holidays(2026)[dt.date(2026, 7, 3)], "Independence Day (observed)")
        self.assertEqual(lf.nyse_holidays(2027)[dt.date(2027, 12, 24)], "Christmas Day (observed)")
        self.assertEqual(lf.nyse_holidays(2027)[dt.date(2027, 7, 5)], "Independence Day (observed)")
        # A Saturday Jan 1 (2022) is not observed on Friday Dec 31.
        self.assertNotIn(dt.date(2021, 12, 31), lf.nyse_holidays(2021))
        self.assertNotIn(dt.date(2022, 1, 1), lf.nyse_holidays(2022))

    def test_easter(self):
        for y, want in {2000: "2000-04-23", 2019: "2019-04-21", 2024: "2024-03-31", 2026: "2026-04-05",
                        2027: "2027-03-28", 2038: "2038-04-25"}.items():
            self.assertEqual(lf.easter(y).isoformat(), want, y)

    def test_trading_days(self):
        self.assertTrue(lf.is_trading_day(dt.date(2026, 10, 16)))  # Friday
        self.assertFalse(lf.is_trading_day(dt.date(2026, 10, 17)))  # Saturday
        self.assertFalse(lf.is_trading_day(dt.date(2026, 10, 18)))  # Sunday
        self.assertTrue(lf.is_trading_day(dt.date(2026, 10, 12)))  # Columbus Day: NYSE is open
        self.assertTrue(lf.is_trading_day(dt.date(2026, 11, 27)))  # early close is still a session
        self.assertFalse(lf.is_trading_day(dt.date(2025, 1, 9)))  # special closure (Carter)
        self.assertEqual(sum(lf.is_trading_day(dt.date(2026, 1, 1) + dt.timedelta(days=i)) for i in range(365)), 251)

    def test_new_york_offset_and_run_instants(self):
        # EDT until Sat 2026-10-31, EST from Sun 2026-11-01 (first Sunday of November); EDT again
        # from Sun 2027-03-14 (second Sunday of March).
        self.assertEqual(lf.scheduled_run(dt.date(2026, 10, 30)), t("2026-10-30 13:35"))
        self.assertEqual(lf.scheduled_run(dt.date(2026, 11, 2)), t("2026-11-02 14:35"))
        self.assertEqual(lf.scheduled_run(dt.date(2027, 3, 12)), t("2027-03-12 14:35"))
        self.assertEqual(lf.scheduled_run(dt.date(2027, 3, 15)), t("2027-03-15 13:35"))
        self.assertEqual(lf.ny_date(t("2026-10-17 03:30")), dt.date(2026, 10, 16))  # 23:30 EDT Friday
        self.assertEqual(lf.ny_date(t("2026-10-17 04:30")), dt.date(2026, 10, 17))  # 00:30 EDT Saturday
        self.assertEqual(lf.ny_date(t("2026-11-02 04:30")), dt.date(2026, 11, 1))  # 23:30 EST Sunday

    def test_last_scheduled(self):
        fri = t("2026-10-16 13:35")
        self.assertEqual(lf.last_scheduled(t("2026-10-16 13:35")), fri)
        self.assertEqual(lf.last_scheduled(t("2026-10-16 13:34")), t("2026-10-15 13:35"))
        self.assertEqual(lf.last_scheduled(t("2026-10-18 20:00")), fri)  # Sunday
        self.assertEqual(lf.last_scheduled(t("2026-10-19 13:34")), fri)  # Monday before the run
        self.assertEqual(lf.last_scheduled(t("2026-10-19 13:35")), t("2026-10-19 13:35"))
        self.assertEqual(lf.prev_scheduled(t("2026-10-19 13:35")), fri)
        # Thanksgiving (Thu 11-26) and Good Friday (04-03) are skipped.
        self.assertEqual(lf.last_scheduled(t("2026-11-26 20:00")), t("2026-11-25 14:35"))
        self.assertEqual(lf.last_scheduled(t("2026-04-03 20:00")), t("2026-04-02 13:35"))


NYSE_MAX = 6 * H


def nyse_runs(*times):
    return [(f"daily-executor/etoro-demo/run-{i}.log", t(s)) for i, s in enumerate(times)]


class EvaluateNyseTest(unittest.TestCase):
    """The eToro ledger: runs at 09:35 New York on trading days, uploaded a minute later."""

    def ev(self, rs, now):
        return lf.evaluate_nyse(rs, t(now), NYSE_MAX, REP, WIN)

    FRIDAY = nyse_runs("2026-10-15 13:36", "2026-10-16 13:36")  # Thu and Fri 2026-10-16 (EDT)

    def test_weekend_and_monday_morning_are_quiet_with_fridays_run(self):
        for now in ["2026-10-16 23:05",  # Friday 19:05 New York
                    "2026-10-17 16:05",  # Saturday
                    "2026-10-18 16:05",  # Sunday
                    "2026-10-19 12:05",  # Monday 08:05 New York
                    "2026-10-19 19:05"]:  # Monday 15:05 New York: the deadline is 15:35
            self.assertIsNone(self.ev(self.FRIDAY, now), now)

    def test_monday_afternoon_pages_without_mondays_run(self):
        kind, d = self.ev(self.FRIDAY, "2026-10-19 20:05")  # Monday 16:05 New York
        self.assertEqual(kind, "stale")
        self.assertTrue(d["first"])
        self.assertEqual(d["due"], t("2026-10-19 13:35"))
        self.assertEqual(d["age"], dt.timedelta(days=3, hours=6, minutes=29))
        self.assertIsNone(self.ev(self.FRIDAY, "2026-10-19 21:05"))  # outside the window
        # Reminder 12 h after the deadline (Tue 03:35 NY -> the 04:05 invocation), then Tuesday's
        # own deadline (15:35 NY) restarts the cycle as a reminder: the run before it was missed too.
        kind, d = self.ev(self.FRIDAY, "2026-10-20 08:05")
        self.assertEqual((kind, d["first"]), ("stale", False))
        self.assertIsNone(self.ev(self.FRIDAY, "2026-10-20 19:05"))
        kind, d = self.ev(self.FRIDAY, "2026-10-20 20:05")
        self.assertEqual((kind, d["first"], d["due"]), ("stale", False, t("2026-10-20 13:35")))

    def test_mondays_run_keeps_it_quiet(self):
        rs = self.FRIDAY + nyse_runs("2026-10-19 13:36")
        self.assertIsNone(self.ev(rs, "2026-10-19 20:05"))
        self.assertIsNone(self.ev(rs, "2026-10-20 12:05"))

    def test_hourly_invocations_over_a_dead_week(self):
        now, sends = t("2026-10-16 14:05"), []
        for _ in range(24 * 5):  # Friday 14:05 UTC through Wednesday
            res = lf.evaluate_nyse(self.FRIDAY, now + dt.timedelta(seconds=7), NYSE_MAX, REP, WIN)
            if res:
                sends.append((now, res[1]["first"]))
            now += H
        self.assertEqual(sends, [
            (t("2026-10-19 20:05"), True),   # Monday 16:05 NY: first
            (t("2026-10-20 08:05"), False),  # Tuesday 04:05 NY: reminder
            (t("2026-10-20 20:05"), False),  # Tuesday 16:05 NY: Tuesday's run missed too
            (t("2026-10-21 08:05"), False),
        ])

    def test_thanksgiving_is_quiet(self):
        # Wed 2026-11-25 run (EST: 09:36 NY = 14:36 UTC); Thursday is Thanksgiving, no run expected.
        rs = nyse_runs("2026-11-24 14:36", "2026-11-25 14:36")
        for now in ["2026-11-26 21:05",  # Thursday 16:05 New York
                    "2026-11-27 13:05",  # Friday 08:05 New York
                    "2026-11-27 20:05"]:  # Friday 15:05 New York (early close day, run still at 09:35)
            self.assertIsNone(self.ev(rs, now), now)
        kind, d = self.ev(rs, "2026-11-27 21:05")  # Friday 16:05 New York: Friday's run is missing
        self.assertEqual((kind, d["first"], d["due"]), ("stale", True, t("2026-11-27 14:35")))

    def test_good_friday_and_a_long_weekend(self):
        rs = nyse_runs("2026-04-02 13:36")  # Thursday before Good Friday 2026-04-03
        for now in ["2026-04-03 20:05", "2026-04-04 20:05", "2026-04-05 20:05", "2026-04-06 19:05"]:
            self.assertIsNone(self.ev(rs, now), now)
        self.assertEqual(self.ev(rs, "2026-04-06 20:05")[0], "stale")

    def test_resolved(self):
        # Monday missed, Tuesday's run arrives: resolved once, in the window after it.
        rs = self.FRIDAY + nyse_runs("2026-10-20 13:36")
        kind, d = self.ev(rs, "2026-10-20 14:05")
        self.assertEqual(kind, "resolved")
        self.assertEqual(d["gap"], dt.timedelta(days=4))
        self.assertIsNone(self.ev(rs, "2026-10-20 15:05"))
        # A late Monday run (after the 15:35 deadline that paged) resolves too.
        late = self.FRIDAY + nyse_runs("2026-10-19 20:30")
        self.assertEqual(self.ev(late, "2026-10-19 21:05")[0], "resolved")
        # The normal cadence (Fri -> Mon) never resolves; a single run has nothing to resolve.
        self.assertIsNone(self.ev(self.FRIDAY + nyse_runs("2026-10-19 13:36"), "2026-10-19 14:05"))
        self.assertIsNone(self.ev(nyse_runs("2026-10-19 13:36"), "2026-10-19 14:05"))

    def test_no_runs_reminds_at_00_and_12_only(self):
        self.assertEqual(self.ev([], "2026-10-19 12:05")[0], "no-runs")
        self.assertIsNone(self.ev([], "2026-10-19 13:05"))

    def test_daily_ledger_is_unchanged_by_the_dispatch(self):
        ages = {"daily": MAX, "nyse": NYSE_MAX}
        rs = runs("2026-10-08 06:20")
        self.assertEqual(lf.evaluate_for("daily", rs, t("2026-10-09 13:05"), ages, REP, WIN),
                         lf.evaluate(rs, t("2026-10-09 13:05"), MAX, REP, WIN))
        self.assertIsNone(lf.evaluate_for("daily", rs, t("2026-10-09 12:05"), ages, REP, WIN))
        # Friday's 06:20 run is stale on Saturday for a daily ledger (no calendar).
        self.assertEqual(lf.evaluate_for("daily", runs("2026-10-16 06:20"), t("2026-10-17 13:05"), ages, REP, WIN)[0], "stale")


ENV = {"BUCKET": "b", "TOPIC_ARN": "arn:aws:sns:us-east-1:1:t", "LEDGERS": "stage=daily-executor/stage"}


@mock.patch.dict(os.environ, ENV, clear=True)
class HandlerTest(unittest.TestCase):
    def s3(self, **kw):
        return FakeS3({
            "daily-executor/stage/run-20261005T225300Z.log": (t("2026-10-05 22:53"), "...\nupload=ok s3://b/x\nexit=0\n"),
            "daily-executor/stage/run-20261008T061500Z.log": (t("2026-10-08 06:20"), "...\nupload=failed s3://b/x\nexit=1\n"),
            "daily-executor/stage/ledger.jsonl": (t("2026-10-08 06:19"), "{}\n"),
            "daily-executor/stage/candles/btc_mxn_daily_2026-10-07.csv": (t("2026-10-08 06:19"), ""),
            "daily-executor/stage/run-notes.txt": (t("2026-10-08 06:19"), ""),
        }, **kw)

    def test_quiet_when_fresh(self):
        sns = FakeSNS()
        out = lf.lambda_handler({}, None, s3=self.s3(), sns=sns, now=t("2026-10-08 20:05"))
        self.assertFalse(out["sent"])
        self.assertEqual(sns.sent, [])
        self.assertEqual(out["status"]["stage"]["runs"], 2)
        # The run time is the stamp in the key name (06:15), not the upload time (06:20).
        self.assertEqual(out["status"]["stage"]["age_hours"], 13.83)

    def test_run_time_comes_from_the_key_name(self):
        lm = t("2026-10-16 23:47")
        self.assertEqual(lf.run_time("daily-executor/etoro-demo/run-20261016T133502Z.log", lm), t("2026-10-16 13:35").replace(second=2))
        self.assertEqual(lf.run_time("daily-executor/x/odd.log", lm), lm)
        # A bulk re-send (--upload-only) gives every log the same LastModified: the names still
        # order the runs and the newest is the newest run, not an arbitrary one.
        same = t("2026-10-16 23:47")
        s3 = FakeS3({
            f"daily-executor/etoro-demo/run-202610{d}T133502Z.log": (same, "exit=0\n") for d in ("12", "13", "14", "15", "16")
        } | {"daily-executor/etoro-demo/run-20261010T031824Z.log": (same, "exit=1\n")})
        runs = lf.list_runs(s3, "b", "daily-executor/etoro-demo")
        key, newest = max(runs, key=lambda r: r[1])
        self.assertTrue(key.endswith("run-20261016T133502Z.log"))
        self.assertEqual(newest, t("2026-10-16 13:35").replace(second=2))

    def test_stale_publishes_ascii_subject_and_details(self):
        sns = FakeSNS()
        out = lf.lambda_handler({}, None, s3=self.s3(), sns=sns, now=t("2026-10-09 13:05"))
        self.assertTrue(out["sent"])
        (topic, subject, body), = sns.sent
        self.assertEqual(topic, ENV["TOPIC_ARN"])
        self.assertEqual(subject, "[mtb-ops] watchdog: 1 stale - stage: no run for 30 h")
        subject.encode("ascii")
        self.assertLess(len(subject), 100)
        self.assertIn("STALE: last run log run-20261008T061500Z.log, 30 h ago", body)
        self.assertIn("last run: exit=1, upload=failed", body)
        self.assertIn("ledger.jsonl: 2026-10-08 06:19 UTC (30 h ago)", body)

    def test_resolved(self):
        sns = FakeSNS()
        lf.lambda_handler({}, None, s3=self.s3(), sns=sns, now=t("2026-10-08 07:05"))
        (_, subject, body), = sns.sent
        self.assertEqual(subject, "[mtb-ops] watchdog: 1 resolved - stage: runs resumed")
        self.assertIn("after a 2 d 7 h gap", body)

    def test_dry_run_publishes_nothing(self):
        sns = FakeSNS()
        out = lf.lambda_handler({"dry_run": True}, None, s3=self.s3(), sns=sns, now=t("2026-10-09 13:05"))
        self.assertFalse(out["sent"])
        self.assertIn("STALE", out["body"])
        self.assertEqual(sns.sent, [])

    def test_unreadable_log_still_alerts(self):
        sns = FakeSNS()
        lf.lambda_handler({}, None, s3=self.s3(fail_get=True), sns=sns, now=t("2026-10-09 13:05"))
        (_, _, body), = sns.sent
        self.assertNotIn("last run:", body)

    def test_pagination(self):
        s3 = self.s3(page=1)
        out = lf.lambda_handler({}, None, s3=s3, sns=FakeSNS(), now=t("2026-10-08 20:05"))
        self.assertEqual(out["status"]["stage"]["runs"], 2)
        self.assertGreater(s3.lists, 2)

    def test_no_runs_and_two_ledgers(self):
        sns = FakeSNS()
        with mock.patch.dict(os.environ, {"LEDGERS": "stage=daily-executor/stage,dry=daily-executor/dry-run"}):
            lf.lambda_handler({}, None, s3=self.s3(), sns=sns, now=t("2026-10-09 12:05"))
        (_, subject, body), = sns.sent
        self.assertEqual(subject, "[mtb-ops] watchdog: 1 stale - dry: no run logs in S3")
        self.assertIn("NO RUN LOGS", body)
        self.assertIn("ledger.jsonl: missing or unreadable", body)

    def test_test_event(self):
        sns = FakeSNS()
        out = lf.lambda_handler({"test": True}, None, s3=self.s3(), sns=sns, now=t("2026-10-08 00:00"))
        self.assertTrue(out["sent"])
        self.assertIn("stage=s3://b/daily-executor/stage", sns.sent[0][2])

    def test_missing_topic_is_an_error(self):
        with mock.patch.dict(os.environ, {"TOPIC_ARN": ""}):
            with self.assertRaises(RuntimeError):
                lf.lambda_handler({}, None, s3=self.s3(), sns=FakeSNS(), now=t("2026-10-09 13:05"))


BOTH = "stage=daily-executor/stage,etoro-demo=daily-executor/etoro-demo:nyse"


@mock.patch.dict(os.environ, dict(ENV, LEDGERS=BOTH), clear=True)
class HandlerNyseTest(unittest.TestCase):
    """Both ledgers as deployed: the stage run at 06:15 UTC daily, the eToro run at 09:35 NY."""

    def s3(self):
        return FakeS3({
            "daily-executor/stage/run-20261016T061500Z.log": (t("2026-10-16 06:20"), "upload=ok s3://b/x\nexit=0\n"),
            "daily-executor/stage/run-20261017T061500Z.log": (t("2026-10-17 06:20"), "upload=ok s3://b/x\nexit=0\n"),
            "daily-executor/stage/run-20261018T061500Z.log": (t("2026-10-18 06:20"), "upload=ok s3://b/x\nexit=0\n"),
            "daily-executor/stage/run-20261019T061500Z.log": (t("2026-10-19 06:20"), "upload=ok s3://b/x\nexit=0\n"),
            "daily-executor/stage/ledger.jsonl": (t("2026-10-19 06:19"), "{}\n"),
            "daily-executor/etoro-demo/run-20261015T133502Z.log": (t("2026-10-15 13:36"), "reconcile=clean\nupload=ok s3://b/e\nexit=0\n"),
            "daily-executor/etoro-demo/run-20261016T133502Z.log": (t("2026-10-16 13:36"), "reconcile=clean\nupload=ok s3://b/e\nexit=0\n"),
            "daily-executor/etoro-demo/ledger.jsonl": (t("2026-10-16 13:35"), "{}\n"),
            "daily-executor/etoro-demo/reconcile/2026-10-16.json": (t("2026-10-16 13:36"), "{}"),
        })

    def test_weekend_is_quiet_for_the_nyse_ledger_only(self):
        sns = FakeSNS()
        out = lf.lambda_handler({}, None, s3=self.s3(), sns=sns, now=t("2026-10-18 16:05"))  # Sunday
        self.assertEqual(sns.sent, [])
        self.assertEqual(out["status"]["etoro-demo"]["calendar"], "nyse")
        self.assertEqual(out["status"]["etoro-demo"]["due"], "2026-10-16T13:35Z")
        self.assertEqual(out["status"]["stage"]["calendar"], "daily")

    def test_missed_monday_pages_at_1605_new_york(self):
        sns = FakeSNS()
        out = lf.lambda_handler({}, None, s3=self.s3(), sns=sns, now=t("2026-10-19 20:05"))
        self.assertTrue(out["sent"])
        (_, subject, body), = sns.sent
        self.assertEqual(subject, "[mtb-ops] watchdog: 1 stale - etoro-demo: no run for 3 d 6 h")
        self.assertIn("== etoro-demo (s3://…/daily-executor/etoro-demo, NYSE trading days)", body)
        self.assertIn("STALE: last run log run-20261016T133502Z.log, 3 d 6 h ago; expected run 2026-10-19 13:35 UTC", body)
        self.assertIn("mtb-ops/etoro.log", body)
        self.assertIn("last run: exit=0, upload=ok", body)
        self.assertIn("within 6 h", body)
        self.assertNotIn("== stage", body)

    def test_max_age_hours_nyse_env(self):
        with mock.patch.dict(os.environ, {"MAX_AGE_HOURS_NYSE": "9"}):
            out = lf.lambda_handler({"dry_run": True}, None, s3=self.s3(), sns=FakeSNS(), now=t("2026-10-19 20:05"))
            self.assertNotIn("subject", out)  # the 18:35 UTC deadline has not passed
            out = lf.lambda_handler({"dry_run": True}, None, s3=self.s3(), sns=FakeSNS(), now=t("2026-10-19 23:05"))
            self.assertIn("etoro-demo: no run for", out["subject"])


if __name__ == "__main__":
    unittest.main()
