#!/usr/bin/env python3
"""Convert daily-research results on Bitso btc_usd into MXN and compare them
with holding btc_mxn, for an investor who starts and ends in pesos.

Usage:
  mxn-terms.py <btc_usd_daily.csv> <btc_mxn_daily.csv> <daily-research output on btc_usd> \
               <daily-research output on btc_mxn> <conversion bps per leg>

The USD/MXN rate is implied from the two Bitso series on the same Mexico-City
day: fx = btc_mxn close / btc_usd close. Both bars share the same day boundary,
so no other FX source (with a different close time) is mixed in.

MXN result of a btc_usd strategy over a window:
  (1 + r_usd) * fx(end) / fx(start) * (1 - conv)^2 - 1
where conv is the MXN->USD conversion cost paid once to enter and once to
leave the USD book. fx(start) is the close before the window (the strategy
enters at the window's first open); fx(end) is the window's last close.
"""
import csv
import re
import sys


def closes(path):
    return {r["date"]: float(r["close"]) for r in csv.DictReader(open(path))}


def results(path):
    """Map window-start -> {rule: return%} from daily-research output."""
    out, cur = {}, None
    for line in open(path):
        m = re.match(r"\s*(IN|OUT-OF)-SAMPLE\s+(\S+)\s+\.\.\s+(\S+)", line)
        if m:
            cur = (m.group(2), m.group(3))
            out[cur] = {}
            continue
        m = re.match(r"(buy_and_hold|trend_sma50)\s+(-?[\d.]+)\s+(\d+)", line)
        if m and cur:
            out[cur][m.group(1)] = (float(m.group(2)), int(m.group(3)))
    return out


usd, mxn = closes(sys.argv[1]), closes(sys.argv[2])
ru, rm = results(sys.argv[3]), results(sys.argv[4])
conv = float(sys.argv[5]) / 1e4
fx = {d: mxn[d] / usd[d] for d in usd if d in mxn}
days = sorted(fx)


def fx_before(d):
    prev = [x for x in days if x < d]
    return fx[prev[-1]]


def fx_at_or_before(d):
    prev = [x for x in days if x <= d]
    return fx[prev[-1]]


print(f"conversion MXN<->USD: {conv*1e4:.0f} bps per leg (paid twice)")
print(f"{'window':<25} {'USD/MXN chg':>11} {'hold btc_mxn':>13} {'trend btc_usd in MXN':>21} {'diff pp':>8} {'trips':>6}")
for w, r in ru.items():
    if w not in rm or "trend_sma50" not in r:
        continue
    f0, f1 = fx_before(w[0]), fx_at_or_before(w[1])
    tr_usd, trips = r["trend_sma50"]
    tr_mxn = ((1 + tr_usd / 100) * f1 / f0 * (1 - conv) ** 2 - 1) * 100
    hold = rm[w]["buy_and_hold"][0]
    print(f"{w[0]+'..'+w[1]:<25} {(f1/f0-1)*100:>10.1f}% {hold:>12.1f}% {tr_mxn:>20.1f}% {tr_mxn-hold:>8.1f} {trips:>6}")
