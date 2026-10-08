#!/usr/bin/env python3
"""Check that a rendered k8s overlay wires the cluster kill switch (§6.4.5).

Reads `kustomize build` / `kubectl kustomize` output on stdin and fails unless:
  - ConfigMap trading-halt exists and its risk-state.json is {"halted": false}
    (a deploy must never ship a halt, or a broken file that blocks everything);
  - trading-engine and order-management each mount it as a directory (no
    subPath: subPath mounts never receive ConfigMap updates) at /etc/trading-halt,
    read-only, and list /etc/trading-halt/risk-state.json in TRADING_HALT_FILES.

Usage: kustomize build k8s/overlays/staging | scripts/tests/check-k8s-halt-wiring.py staging
"""
import json
import sys

import yaml

MOUNT = "/etc/trading-halt"
FILE = MOUNT + "/risk-state.json"
SERVICES = ("trading-engine", "order-management")


def main() -> int:
    label = sys.argv[1] if len(sys.argv) > 1 else "overlay"
    docs = [d for d in yaml.safe_load_all(sys.stdin) if d]
    errors = []

    cms = [d for d in docs if d.get("kind") == "ConfigMap" and d["metadata"]["name"].startswith("trading-halt")]
    if len(cms) != 1:
        errors.append(f"want exactly one trading-halt ConfigMap, found {len(cms)}")
        cm_name = "trading-halt"
    else:
        cm_name = cms[0]["metadata"]["name"]  # may carry a kustomize name prefix/suffix
        try:
            state = json.loads(cms[0]["data"]["risk-state.json"])
            if state != {"halted": False}:
                errors.append(f"shipped halt state must be {{\"halted\": false}}, got {state}")
        except (KeyError, ValueError) as e:
            errors.append(f"trading-halt risk-state.json unreadable: {e}")

    deps = {d["metadata"]["name"]: d for d in docs if d.get("kind") == "Deployment"}
    for svc in SERVICES:
        dep = deps.get(svc)
        if dep is None:
            errors.append(f"{svc}: Deployment missing")
            continue
        spec = dep["spec"]["template"]["spec"]
        vols = {v["name"]: v for v in spec.get("volumes", [])}
        halt_vols = [n for n, v in vols.items() if v.get("configMap", {}).get("name") == cm_name]
        if not halt_vols:
            errors.append(f"{svc}: no volume from ConfigMap {cm_name}")
            continue
        c = next((c for c in spec["containers"] if c["name"] == svc), spec["containers"][0])
        mounts = [m for m in c.get("volumeMounts", []) if m["name"] in halt_vols]
        if not mounts:
            errors.append(f"{svc}: halt volume not mounted")
        for m in mounts:
            if m.get("mountPath") != MOUNT:
                errors.append(f"{svc}: mounted at {m.get('mountPath')}, want {MOUNT}")
            if m.get("subPath"):
                errors.append(f"{svc}: subPath mount never receives ConfigMap updates")
            if not m.get("readOnly"):
                errors.append(f"{svc}: halt mount must be readOnly")
        env = {e["name"]: e.get("value", "") for e in c.get("env", [])}
        files = [f.strip() for f in env.get("TRADING_HALT_FILES", "").split(",")]
        if FILE not in files:
            errors.append(f"{svc}: TRADING_HALT_FILES={env.get('TRADING_HALT_FILES')!r} does not list {FILE}")

    if errors:
        for e in errors:
            print(f"{label}: {e}", file=sys.stderr)
        return 1
    print(f"{label}: kill switch wired into {', '.join(SERVICES)}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
