# S3 archive compaction + flush-batching fix (2026-09-22)

**Bucket:** `s3://mtb-development-data-archive-326105557351` (confirmed from
`/etc/data-collector/env` on `i-00c71eec12a1c52d7`: `S3_BUCKET`, `S3_PREFIX=trades`,
`BITSO_BOOK=btc_mxn`)

## Root cause

The collector ran with `FLUSH_INTERVAL=60s` and `FLUSH_MAX_ROWS=500`. The time
trigger was a free-running 60s ticker that flushed whatever was buffered on
every tick. `btc_mxn` averages about 1.4 trades/minute (68,459 rows over 34
days), so the 500-row cap was never reached. Every minute that saw at least one
trade produced its own file: roughly 750–1,070 files/day, **2.6 rows and 2.3 KiB
per file**. Nothing was broken in the batching code itself. The time threshold
was simply about 100x too short for this book's volume.

A secondary bug: each flush was filed under the *first* trade's UTC day, so a
batch crossing midnight put the next day's first trades in the previous day's
partition. Compaction found 18 such rows across the archive.

## Fix (deployed 2026-09-22 18:25 UTC)

- The time trigger now measures the **age of the oldest buffered row**
  (`FLUSH_INTERVAL`, default `1h`) rather than firing on a fixed tick. The row
  trigger (`FLUSH_MAX_ROWS`, default `5000`) still caps memory during bursts.
  Whichever fires first wins.
- Each flush writes one object per UTC day partition, so batches spanning
  midnight are split correctly.
- A graceful shutdown flushes the buffer and retries up to 3 times, within a
  60s deadline (under systemd's 90s stop timeout).
- New metric `data_collector_s3_flush_rows` (rows per object), and a
  `S3 flush ok rows=N key=...` log line per object.
- Terraform module defaults (`flush_interval`, `flush_max_rows`) match the new
  code defaults. **Not applied**: the running instance's
  `/etc/data-collector/env` was edited in place over SSM. The next
  `terraform apply` will show an in-place `user_data` change on the instance,
  which stops and starts it.

Trade-off: a hard kill (OOM or SIGKILL) loses up to 1h of S3 data. Those rows
are still in the Postgres hot store for 7 days.

Rollback on the instance: `/opt/data-collector/data-collector.prev` and
`/etc/data-collector/env.prev`.

### Verified after deploy

The first object written by the fixed collector (2026-09-22 19:25:54 UTC,
exactly one hour after the first post-restart trade) held **764 rows in
15,362 bytes**, versus 2.6 rows / 2.3 KiB per file before. It was a busy
afternoon; quieter hours will be closer to 50–200 rows. `/metrics` showed
`data_collector_s3_flush_failures_total 0`, and `/healthz` was healthy.
Check later flushes with:

```bash
curl -s localhost:8085/metrics | grep -E '^data_collector_s3_flush_rows_(sum|count)'
grep 'S3 flush ok' /var/log/data-collector/stdout.log | tail
```

## One-time compaction (run 2026-09-22, non-destructive)

`services/data-collector/cmd/compact-archive` rebuilds each settled UTC day
from the source small files into
`trades_compacted/book=btc_mxn/year=YYYY/month=MM/day=DD/trades-YYYYMMDD.parquet`.
It then reads the output back and requires a row-for-row match with the
source before writing `_manifest.json` (source keys, sizes, fingerprint and
counts). Re-runs skip partitions whose source files are unchanged. A day is
eligible 2h after it ends (`-settle`), so the partition being written today
is never touched.

Result:

| | Before (`trades/`) | After (`trades_compacted/`) |
|---|---|---|
| Partitions | 34 (2026-08-19 → 2026-09-21) | 34 |
| Files | 25,914 | 34 |
| Rows | 68,459 | 68,459 (exact match, per partition and in total) |
| Size | 58.3 MiB | 1.7 MiB |
| Avg per file | 2.6 rows, 2.3 KiB | 2,013 rows, 51.9 KiB |

Day grain was used because a full day is only 600–4,600 rows (~17–116 KiB),
so hourly files would be needlessly small. Zero duplicate TIDs were found. The
18 rows filed under the previous day were kept where they were, so counts
reconcile.

Independent cross-check: the Postgres hot store's per-day counts (by exchange
timestamp) for 2026-09-16 → 09-21 match the compacted files exactly once
those 18 rows are accounted for.

**The original small files have not been deleted.** 2026-09-22 (today) was
skipped as not settled.

## Cutover (manual — not performed)

Cutover replaces a partition's small files under `trades/` with
`compacted-YYYYMMDD.parquet`. It only acts on partitions whose manifest is
validated **and** whose current source files still match the manifest's
fingerprint exactly. For each partition it writes and byte-verifies the
compacted file *before* deleting that partition's small files, and it asks
you to type `yes` first. If interrupted, re-running it resumes cleanly.

It needs credentials that can list, read, write and delete in the bucket
(e.g. the operator IAM user). The collector's instance role is write-only
and cannot run it.

```bash
cd services/data-collector
go build -o compact-archive ./cmd/compact-archive

# 1. Refresh: compacts any newly settled days, skips unchanged ones.
./compact-archive -bucket mtb-development-data-archive-326105557351

# 2. Review the cutover plan (no changes).
./compact-archive -bucket mtb-development-data-archive-326105557351 -cutover -dry-run

# 3. Cut over (prompts for "yes").
./compact-archive -bucket mtb-development-data-archive-326105557351 -cutover
```

After cutover, `trades_compacted/` still holds a validated copy of every
partition. Delete that prefix once backtest readers are confirmed working
against `trades/`.

## Going forward

With the fix, `trades/` gains about 24 files/day instead of about 800. Running
steps 1–3 periodically (e.g. weekly) keeps it at one file per day. This isn't
automated yet, because it needs a role with read and delete access to the
archive.
