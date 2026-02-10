#!/usr/bin/env bash
# Start kubectl port-forward so Grafana is reachable on localhost:3000.
# When running in the IDE behind CloudFront, Caddy routes /proxy/3000 to this port,
# so after running this script you can use: https://<cloudfront-domain>/proxy/3000
#
# Prerequisites:
#   - kubectl and kubeconfig (e.g. aws eks update-kubeconfig --name mtb-development --region us-east-1)
#
# Usage: ./scripts/grafana-port-forward.sh [namespace]
# Default namespace: monitoring

set -e
NAMESPACE="${1:-monitoring}"
SVC="kube-prometheus-stack-grafana"

if ! kubectl get svc -n "$NAMESPACE" "$SVC" &>/dev/null; then
  echo "Service $SVC not found in namespace $NAMESPACE. Is kube-prometheus-stack installed?"
  exit 1
fi

echo "Forwarding localhost:3000 -> $NAMESPACE/$SVC:80 (Grafana). Use https://<cloudfront-domain>/proxy/3000 in the browser."
echo "Press Ctrl+C to stop."
exec kubectl port-forward -n "$NAMESPACE" "svc/$SVC" 3000:80
