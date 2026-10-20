#!/usr/bin/env python3
"""Render the trading-operations SLOs (frontend plan §11 q4) from their spec.

    scripts/gen-slo.py            # write the rules file and the dashboard
    scripts/gen-slo.py --check    # exit 1 if either is out of date (CI)

Source:  monitoring/prometheus/slo/trading-slos.yaml
Outputs: monitoring/prometheus/rules/trading-slo-rules.yml
         monitoring/grafana/dashboards/domain/trading-slo.json

Recording rules, per SLO and window W (5m .. 30d):
  slo:error_ratio:W{slo,team,kind}   event: 1 - good/total     time: 1 - avg_over_time(good bool)
  slo:events:W{slo}                  event only: the number of events in the window
  slo:good:bool{slo,...}             time only: the condition, 1 or 0, every interval
Constants (one series per SLO): slo:target, slo:min_events:W (event SLOs with a guard).
Derived, generic over the slo label:
  slo:burn_rate:W = error ratio / (1 - target);  slo:sli:30d;  slo:budget_remaining:30d.
Alerts: multi-window burn rates per the spec's burn_alerts, plus the exhausted budget.

Stdlib + PyYAML only. Output is byte-stable so --check can diff it.
"""
import json
import os
import sys

import yaml

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SPEC = os.path.join(ROOT, "monitoring/prometheus/slo/trading-slos.yaml")
RULES = os.path.join(ROOT, "monitoring/prometheus/rules/trading-slo-rules.yml")
DASH = os.path.join(ROOT, "monitoring/grafana/dashboards/domain/trading-slo.json")
RUNBOOK = "docs/runbooks/TRADING-SLO.md"


def load_spec():
    with open(SPEC) as f:
        return yaml.safe_load(f)


def sub(expr, window):
    return " ".join(expr.replace("$W", window).split())


# --------------------------------------------------------------------------- rules

def recording_rules(spec):
    groups = []
    windows = spec["windows"]

    rec = []
    for s in spec["slos"]:
        labels = {"slo": s["name"], "team": s["team"], "kind": s["kind"]}
        rec.append({"record": "slo:target", "expr": f"vector({s['target']})", "labels": dict(labels)})
        if s["kind"] == "time":
            rec.append({"record": "slo:good:bool", "expr": sub(s["good_bool"], ""), "labels": dict(labels)})
        for w in windows:
            if s["kind"] == "event":
                good, total = sub(s["good"], w), sub(s["total"], w)
                expr = f"clamp(1 - ({good}) / ({total}), 0, 1)"
                rec.append({"record": f"slo:error_ratio:{w}", "expr": expr, "labels": dict(labels)})
                rec.append({"record": f"slo:events:{w}", "expr": sub(s["events"], w), "labels": {"slo": s["name"]}})
                if w in (s.get("min_events") or {}):
                    rec.append({"record": f"slo:min_events:{w}", "expr": f"vector({s['min_events'][w]})", "labels": {"slo": s["name"]}})
            else:
                expr = f'clamp(1 - avg_over_time(slo:good:bool{{slo="{s["name"]}"}}[{w}]), 0, 1)'
                rec.append({"record": f"slo:error_ratio:{w}", "expr": expr, "labels": dict(labels)})
    groups.append({"name": "trading-slo-indicators", "interval": "1m", "rules": rec})

    derived = []
    for w in windows:
        derived.append({"record": f"slo:burn_rate:{w}",
                        "expr": f"slo:error_ratio:{w} / on (slo) group_left () (1 - slo:target)"})
    W = spec["window"]
    derived.append({"record": f"slo:sli:{W}", "expr": f"1 - slo:error_ratio:{W}"})
    derived.append({"record": f"slo:budget_remaining:{W}", "expr": f"1 - slo:burn_rate:{W}"})
    groups.append({"name": "trading-slo-derived", "interval": "1m", "rules": derived})
    return groups


def alert_rules(spec):
    W = spec["window"]
    rules = []
    for b in spec["burn_alerts"]:
        for kind in b["kinds"]:
            long_, short, factor, sev = b["long"], b["short"], b["factor"], b["severity"]
            name = f"TradingSLOBurn{'Fast' if sev == 'critical' else 'Slow'}{long_.upper()}{'Events' if kind == 'event' else ''}"
            expr = (f'slo:burn_rate:{long_}{{kind="{kind}"}} > {factor}\n'
                    f'  and on (slo) slo:burn_rate:{short}{{kind="{kind}"}} > {factor}')
            if kind == "event":
                expr += f"\n  and on (slo) slo:events:{long_} >= on (slo) slo:min_events:{long_}"
            budget = {"1h": "2 % of the 30-day budget in 1 h", "6h": "5 % of the 30-day budget in 6 h",
                      "1d": "10 % of the 30-day budget in 1 day", "3d": "10 % of the 30-day budget in 3 days"}[long_]
            desc = (f"Error budget burning at {factor}x over {long_} and {short}: {budget}. "
                    + ("Low-volume platform: this only fires with enough events in the window (slo:min_events). "
                       if kind == "event" else "")
                    + "The runbook lists the SLI's source metrics and the per-event alerts to read next.")
            rules.append({"alert": name,
                          "expr": expr,
                          "for": "2m" if sev == "critical" else "15m",
                          "labels": {"severity": sev},
                          "annotations": {
                              "summary": f"SLO {{{{ $labels.slo }}}}: burn rate {{{{ $value | printf \"%.1f\" }}}}x over {long_} (and {short})",
                              "description": desc,
                              "runbook_url": f"{RUNBOOK}#{name.lower()}"}})
    rules.append({"alert": "TradingSLOBudgetExhausted",
                  "expr": f"slo:budget_remaining:{W} <= 0",
                  "for": "1h",
                  "labels": {"severity": "warning"},
                  "annotations": {
                      "summary": f"SLO {{{{ $labels.slo }}}}: the {W} error budget is spent ({{{{ $value | humanizePercentage }}}} remaining)",
                      "description": f"The {W} SLI is below its target. Review the SLO's incidents in the dashboard before adding risk; a negative value says how far over.",
                      "runbook_url": f"{RUNBOOK}#tradingslobudgetexhausted"}})
    return [{"name": "trading-slo-alerts", "rules": rules}]


class Dumper(yaml.SafeDumper):
    def increase_indent(self, flow=False, indentless=False):
        return super().increase_indent(flow, False)


def str_presenter(dumper, data):
    if "\n" in data:
        return dumper.represent_scalar("tag:yaml.org,2002:str", data, style="|")
    return dumper.represent_scalar("tag:yaml.org,2002:str", data)


Dumper.add_representer(str, str_presenter)


def render_rules(spec):
    header = """# GENERATED by scripts/gen-slo.py from monitoring/prometheus/slo/trading-slos.yaml.
# Do not edit by hand: edit the spec and re-run the script (CI checks they match).
#
# Trading-operations SLOs (frontend plan §11 q4; runbook docs/runbooks/TRADING-SLO.md):
# recording rules for each SLI's error ratio over 5m..30d, the derived burn rates, the
# 30-day SLI and budget, and multi-window burn-rate alerts (Google SRE workbook factors).
# Event SLIs are gated on slo:min_events because this platform trades a few times a day.
# Kept in sync with k8s/monitoring/trading-slo-rules.yaml by scripts/sync-prometheus-rules.sh.
"""
    body = yaml.dump({"groups": recording_rules(spec) + alert_rules(spec)}, Dumper=Dumper,
                     sort_keys=False, width=1000, allow_unicode=True)
    return header + "\n" + body


# --------------------------------------------------------------------------- dashboard

def panel_base(pid, ptype, title, x, y, w, h, targets, unit=None, thresholds=None, description=None, extra=None):
    steps = [{"color": "green", "value": None}]
    for i, t in enumerate(thresholds or []):
        steps.append({"color": "orange" if i == 0 and len(thresholds) > 1 else "red", "value": t})
    p = {"id": pid, "type": ptype, "title": title, "gridPos": {"h": h, "w": w, "x": x, "y": y},
         "datasource": {"type": "prometheus", "uid": "${DS}"},
         "targets": [dict(t, refId=chr(65 + i), datasource={"type": "prometheus", "uid": "${DS}"}) for i, t in enumerate(targets)],
         "fieldConfig": {"defaults": {"color": {"mode": "palette-classic"},
                                      "thresholds": {"mode": "absolute", "steps": steps}},
                         "overrides": []},
         "options": {}}
    if unit:
        p["fieldConfig"]["defaults"]["unit"] = unit
    if description:
        p["description"] = description
    if ptype == "timeseries":
        p["fieldConfig"]["defaults"]["custom"] = {"lineWidth": 1, "fillOpacity": 8, "showPoints": "never",
                                                  "thresholdsStyle": {"mode": "line" if thresholds else "off"}}
        p["options"] = {"legend": {"displayMode": "list", "placement": "bottom", "showLegend": True},
                        "tooltip": {"mode": "multi", "sort": "desc"}}
    if ptype == "stat":
        p["fieldConfig"]["defaults"]["color"] = {"mode": "thresholds"}
        p["options"] = {"reduceOptions": {"calcs": ["lastNotNull"], "fields": "", "values": False},
                        "orientation": "auto", "textMode": "value", "colorMode": "background",
                        "graphMode": "none", "justifyMode": "center"}
    if extra:
        p.update(extra)
    return p


def sli_steps(target):
    # red below target, orange within a tenth of the budget above it, green otherwise
    budget = 1 - target
    return [{"color": "red", "value": None}, {"color": "orange", "value": target}, {"color": "green", "value": min(1, target + budget * 0.5)}]


def dashboard(spec):
    W = spec["window"]
    panels = []
    pid = [0]

    def nid():
        pid[0] += 1
        return pid[0]

    y = 0
    # --- overview row: one line of stats per SLO
    panels.append({"id": nid(), "type": "row", "title": f"SLO overview ({W} rolling)", "collapsed": False,
                   "gridPos": {"h": 1, "w": 24, "x": 0, "y": y}, "panels": []})
    y += 1
    panels.append(panel_base(nid(), "text", "How to read this", 0, y, 24, 3, [], extra={
        "options": {"mode": "markdown", "content":
                    "Each SLO is a **30-day** objective. **SLI** is the measured ratio; **budget** is what is left of the allowed error "
                    "(1 − burn over 30 d); **burn 1h / 6h** is how fast the budget is being spent right now (1× = exactly on budget, "
                    "14.4× pages). Event SLIs (orders, decisions) are gated on a minimum number of events because this platform "
                    "trades a few times a day; the per-event alerts in *Trading Risk Operations* remain the guard below that volume. "
                    f"Definitions, sources and what to do: `{RUNBOOK}`."}}))
    y += 3
    for s in spec["slos"]:
        name, target = s["name"], s["target"]
        sel = f'{{slo="{name}"}}'
        panels.append(panel_base(nid(), "stat", s["title"], 0, y, 6, 3, [{"expr": f"slo:sli:{W}{sel}", "legendFormat": "SLI 30d"}],
                                 unit="percentunit", description=s["objective"],
                                 extra={"fieldConfig": {"defaults": {"unit": "percentunit", "decimals": 2, "color": {"mode": "thresholds"},
                                                                     "thresholds": {"mode": "absolute", "steps": sli_steps(target)}},
                                                        "overrides": []}}))
        panels.append(panel_base(nid(), "stat", "target", 6, y, 2, 3, [{"expr": f"slo:target{sel}", "legendFormat": "target"}],
                                 unit="percentunit", extra={"fieldConfig": {"defaults": {"unit": "percentunit", "decimals": 1, "color": {"mode": "fixed", "fixedColor": "text"},
                                                                                       "thresholds": {"mode": "absolute", "steps": [{"color": "text", "value": None}]}}, "overrides": []}}))
        panels.append(panel_base(nid(), "stat", "budget remaining", 8, y, 4, 3, [{"expr": f"slo:budget_remaining:{W}{sel}", "legendFormat": "budget"}],
                                 unit="percentunit", extra={"fieldConfig": {"defaults": {"unit": "percentunit", "decimals": 0, "color": {"mode": "thresholds"},
                                                                                       "thresholds": {"mode": "absolute", "steps": [{"color": "red", "value": None}, {"color": "orange", "value": 0.25}, {"color": "green", "value": 0.5}]}}, "overrides": []}}))
        for i, w in enumerate(("1h", "6h")):
            panels.append(panel_base(nid(), "stat", f"burn {w}", 12 + 3 * i, y, 3, 3, [{"expr": f"slo:burn_rate:{w}{sel}", "legendFormat": f"burn {w}"}],
                                     unit="none", extra={"fieldConfig": {"defaults": {"unit": "none", "decimals": 1, "color": {"mode": "thresholds"},
                                                                                     "thresholds": {"mode": "absolute", "steps": [{"color": "green", "value": None}, {"color": "orange", "value": 1}, {"color": "red", "value": 6 if w == "6h" else 14.4}]}}, "overrides": []}}))
        kind = "events in 1d" if s["kind"] == "event" else "kind"
        tgt = ({"expr": f"slo:events:1d{sel}", "legendFormat": "events 1d"} if s["kind"] == "event"
               else {"expr": f"slo:good:bool{sel}", "legendFormat": "good now"})
        panels.append(panel_base(nid(), "stat", kind, 18, y, 3, 3, [tgt], unit="short",
                                 extra={"fieldConfig": {"defaults": {"unit": "short", "decimals": 0, "color": {"mode": "fixed", "fixedColor": "text"},
                                                                     "thresholds": {"mode": "absolute", "steps": [{"color": "text", "value": None}]}}, "overrides": []}}))
        panels.append(panel_base(nid(), "stat", "team", 21, y, 3, 3, [{"expr": f"slo:target{sel}", "legendFormat": "{{team}}"}], unit="none",
                                 extra={"options": {"reduceOptions": {"calcs": ["lastNotNull"], "fields": "", "values": False}, "textMode": "name", "colorMode": "none", "graphMode": "none", "justifyMode": "center", "orientation": "auto"},
                                        "fieldConfig": {"defaults": {"color": {"mode": "fixed", "fixedColor": "text"}, "thresholds": {"mode": "absolute", "steps": [{"color": "text", "value": None}]}}, "overrides": []}}))
        y += 3

    # --- one row per SLO: error ratio vs budget, burn rates, source
    for s in spec["slos"]:
        name, target = s["name"], s["target"]
        sel = f'{{slo="{name}"}}'
        panels.append({"id": nid(), "type": "row", "title": f"{s['title']} — {s['objective']}", "collapsed": True,
                       "gridPos": {"h": 1, "w": 24, "x": 0, "y": y}, "panels": [
                           panel_base(nid(), "timeseries", "Error ratio by window vs the budget (1 − target)", 0, y + 1, 12, 8,
                                      [{"expr": f"slo:error_ratio:{w}{sel}", "legendFormat": w} for w in ("1h", "6h", "1d", "30d")]
                                      + [{"expr": f"1 - slo:target{sel}", "legendFormat": "budget (1 − target)"}],
                                      unit="percentunit", description=f"Source: {s['source']}"),
                           panel_base(nid(), "timeseries", "Burn rate (1× = on budget; 14.4× / 6× page, 3× / 1× ticket)", 12, y + 1, 12, 8,
                                      [{"expr": f"slo:burn_rate:{w}{sel}", "legendFormat": w} for w in ("5m", "1h", "6h", "1d", "3d")],
                                      unit="none", thresholds=[1, 6, 14.4]),
                           panel_base(nid(), "timeseries", "Events in the window" if s["kind"] == "event" else "Condition held (1) / failed (0)", 0, y + 9, 12, 6,
                                      ([{"expr": f"slo:events:{w}{sel}", "legendFormat": f"events {w}"} for w in ("6h", "1d", "3d")]
                                       + [{"expr": f"slo:min_events:{w}{sel}", "legendFormat": f"min {w}"} for w in sorted((s.get("min_events") or {}).keys())])
                                      if s["kind"] == "event" else [{"expr": f"slo:good:bool{sel}", "legendFormat": "good"}],
                                      unit="short"),
                           panel_base(nid(), "text", "Related per-event alerts", 12, y + 9, 12, 6, [], extra={
                               "options": {"mode": "markdown", "content":
                                           "**Source:** " + s["source"] + "\n\n**Threshold alerts on the same signal:** "
                                           + ", ".join(f"`{a}`" for a in s.get("related_alerts", [])) + f"\n\n**Runbook:** `{RUNBOOK}#{name}`"}}),
                       ]})
        y += 1

    # --- KPI rows
    for row in spec.get("kpis", []):
        panels.append({"id": nid(), "type": "row", "title": row["row"], "collapsed": False,
                       "gridPos": {"h": 1, "w": 24, "x": 0, "y": y}, "panels": []})
        y += 1
        n = len(row["panels"])
        w = 24 // n if n in (1, 2, 3, 4, 6) else 6
        for i, kp in enumerate(row["panels"]):
            targets = [{"expr": " ".join(kp["expr"].split()), "legendFormat": kp.get("legend", "")}]
            for e in kp.get("extra", []):
                targets.append({"expr": " ".join(e["expr"].split()), "legendFormat": e.get("legend", "")})
            panels.append(panel_base(nid(), "timeseries", kp["title"], (i % (24 // w)) * w, y + 8 * (i // (24 // w)), w, 8, targets,
                                     unit=kp.get("unit"), thresholds=kp.get("thresholds")))
        y += 8 * ((n + (24 // w) - 1) // (24 // w))

    return {
        "uid": "trading-slo",
        "title": "Trading Operations SLOs",
        "description": "Service-level objectives for the order path, freshness and the kill switch (frontend plan §11 q4). Generated by scripts/gen-slo.py from monitoring/prometheus/slo/trading-slos.yaml.",
        "tags": ["trading", "slo", "operations", "generated"],
        "timezone": "utc",
        "editable": True,
        "graphTooltip": 1,
        "schemaVersion": 39,
        "version": 1,
        "refresh": "1m",
        "time": {"from": "now-7d", "to": "now"},
        "templating": {"list": [{"name": "DS", "type": "datasource", "query": "prometheus", "label": "Prometheus",
                                 "current": {"text": "Prometheus", "value": "Prometheus"}, "hide": 0}]},
        "annotations": {"list": [{"name": "SLO alerts", "datasource": {"type": "prometheus", "uid": "${DS}"}, "enable": True,
                                  "expr": "ALERTS{alertstate=\"firing\", alertname=~\"TradingSLO.*\"}", "iconColor": "red",
                                  "titleFormat": "{{alertname}} {{slo}}", "step": "1m"}]},
        "links": [{"title": "Trading Risk Operations", "type": "link", "url": "/d/trading-risk-operations", "targetBlank": False},
                  {"title": "Runbook", "type": "link", "url": f"https://github.com/Gall-oDrone/microservices-trading-bot/blob/feat/etoro/{RUNBOOK}", "targetBlank": True}],
        "panels": panels,
    }


def render_dashboard(spec):
    return json.dumps(dashboard(spec), indent=2, ensure_ascii=False) + "\n"


# --------------------------------------------------------------------------- main

def main(argv):
    spec = load_spec()
    outputs = {RULES: render_rules(spec), DASH: render_dashboard(spec)}
    if "--check" in argv:
        stale = []
        for path, want in outputs.items():
            have = open(path).read() if os.path.exists(path) else None
            if have != want:
                stale.append(os.path.relpath(path, ROOT))
        if stale:
            print("out of date (run scripts/gen-slo.py): " + ", ".join(stale), file=sys.stderr)
            return 1
        print("trading-slo rules and dashboard are in sync with the spec")
        return 0
    for path, content in outputs.items():
        with open(path, "w") as f:
            f.write(content)
        print("wrote", os.path.relpath(path, ROOT))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
