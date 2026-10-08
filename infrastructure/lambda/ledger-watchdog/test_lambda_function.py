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
                         [("stage", "daily-executor/stage"), ("dry", "x/y")])
        for bad in ["", "stage", "=x", "stage=", "stage=/"]:
            with self.assertRaises(ValueError, msg=bad):
                lf.parse_ledgers(bad)

    def test_human(self):
        self.assertEqual(lf.human(dt.timedelta(minutes=45)), "45 min")
        self.assertEqual(lf.human(dt.timedelta(hours=31, minutes=5)), "31 h")
        self.assertEqual(lf.human(dt.timedelta(hours=52)), "2 d 4 h")


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
        self.assertEqual(out["status"]["stage"]["age_hours"], 13.75)

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


if __name__ == "__main__":
    unittest.main()
