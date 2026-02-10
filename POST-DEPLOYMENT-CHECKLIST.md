# Post-Deployment Checklist

This document outlines the steps to complete after Kubernetes pods are running, including service verification, monitoring setup, security hardening, and testing phases.

## Table of Contents

1. [Phase 1: Service Health Verification](#phase-1-service-health-verification)
2. [Phase 2: External Secrets Configuration](#phase-2-external-secrets-configuration)
3. [Phase 3: Initial Service Testing](#phase-3-initial-service-testing)
4. [Phase 4: Monitoring Stack Deployment](#phase-4-monitoring-stack-deployment)
5. [Phase 5: Security Policies](#phase-5-security-policies)
6. [Phase 6: Integration Testing](#phase-6-integration-testing)
7. [Phase 7: External Access Setup](#phase-7-external-access-setup)
8. [Phase 8: End-to-End Testing](#phase-8-end-to-end-testing)
9. [Phase 9: Load Testing](#phase-9-load-testing)
10. [Phase 10: Production Readiness](#phase-10-production-readiness)

---

## Phase 1: Service Health Verification

**Goal:** Ensure all pods are running and services are healthy.

### 1.1 Check Pod Status

```bash
# List all pods in the development namespace
kubectl get pods -n bitso-trading-dev

# Check pod details if any are not running
kubectl describe pod <pod-name> -n bitso-trading-dev

# View recent events
kubectl get events -n bitso-trading-dev --sort-by='.lastTimestamp'
```

### 1.2 Verify Service Endpoints

```bash
# List all services
kubectl get svc -n bitso-trading-dev

# Check endpoints are populated
kubectl get endpoints -n bitso-trading-dev
```

### 1.3 Check Container Logs

```bash
# Check logs for each service
kubectl logs -f deployment/api-gateway -n bitso-trading-dev
kubectl logs -f deployment/trading-engine -n bitso-trading-dev
kubectl logs -f deployment/market-data -n bitso-trading-dev
kubectl logs -f deployment/strategy-executor -n bitso-trading-dev
kubectl logs -f deployment/order-management -n bitso-trading-dev
kubectl logs -f deployment/backtesting -n bitso-trading-dev

# Check Kafka and Redis
kubectl logs -f deployment/kafka -n bitso-trading-dev
kubectl logs -f deployment/redis -n bitso-trading-dev
```

### 1.4 Verify Resource Usage

```bash
# Check CPU and memory usage
kubectl top pods -n bitso-trading-dev

# Check node resources
kubectl top nodes
```

### Checklist

- [ ] All pods showing `Running` status
- [ ] All pods have `1/1` or expected ready containers
- [ ] No restart loops (check RESTARTS column)
- [ ] Logs show successful startup messages
- [ ] No error messages in logs
- [ ] Resource usage within expected limits

---

## Phase 2: External Secrets Configuration

**Goal:** Configure secure secret management using AWS Secrets Manager.

### 2.1 Install External Secrets Operator

```bash
# Add the External Secrets Helm repository
helm repo add external-secrets https://charts.external-secrets.io
helm repo update

# Install the operator
helm install external-secrets \
  external-secrets/external-secrets \
  -n external-secrets \
  --create-namespace \
  --set installCRDs=true
```

### 2.2 Create Secrets in AWS Secrets Manager

```bash
# Create the required secrets in AWS Secrets Manager
aws secretsmanager create-secret \
  --name trading-bot/bitso-api-key \
  --secret-string "your-api-key-here" \
  --region us-east-1

aws secretsmanager create-secret \
  --name trading-bot/bitso-api-secret \
  --secret-string "your-api-secret-here" \
  --region us-east-1

aws secretsmanager create-secret \
  --name trading-bot/redis-password \
  --secret-string "your-redis-password-here" \
  --region us-east-1
```

### 2.3 Configure IAM for External Secrets

Ensure the EKS service account has permissions to access Secrets Manager:

```bash
# Create IAM policy for secrets access
cat <<EOF > secrets-policy.json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "secretsmanager:GetSecretValue",
        "secretsmanager:DescribeSecret"
      ],
      "Resource": "arn:aws:secretsmanager:us-east-1:*:secret:trading-bot/*"
    }
  ]
}
EOF

# Attach policy to the EKS node role or create IRSA
aws iam create-policy \
  --policy-name TradingBotSecretsAccess \
  --policy-document file://secrets-policy.json
```

### 2.4 Apply External Secret Resources

```bash
# Apply the SecretStore and ExternalSecret
kubectl apply -f k8s/base/service-account-external-secrets.yaml -n bitso-trading-dev
kubectl apply -f k8s/base/external-secret.yaml -n bitso-trading-dev

# Verify secrets are synced
kubectl get externalsecrets -n bitso-trading-dev
kubectl get secrets trading-secrets -n bitso-trading-dev
```

### Checklist

- [ ] External Secrets Operator installed
- [ ] Secrets created in AWS Secrets Manager
- [ ] IAM permissions configured
- [ ] SecretStore created and healthy
- [ ] ExternalSecret synced successfully
- [ ] Kubernetes secret `trading-secrets` exists

---

## Phase 3: Initial Service Testing

**Goal:** Verify individual services are responding correctly.

### 3.1 Test Health Endpoints

```bash
# Port-forward and test each service

# API Gateway
kubectl port-forward svc/api-gateway 8085:8085 -n bitso-trading-dev &
curl http://localhost:8085/health
curl http://localhost:8085/api/v1/status

# Trading Engine
kubectl port-forward svc/trading-engine 8080:8080 -n bitso-trading-dev &
curl http://localhost:8080/health

# Market Data
kubectl port-forward svc/market-data 8083:8083 -n bitso-trading-dev &
curl http://localhost:8083/health

# Strategy Executor
kubectl port-forward svc/strategy-executor 8082:8082 -n bitso-trading-dev &
curl http://localhost:8082/health
curl http://localhost:8082/api/v1/strategies

# Order Management
kubectl port-forward svc/order-management 8084:8084 -n bitso-trading-dev &
curl http://localhost:8084/health

# Backtesting
kubectl port-forward svc/backtesting 8081:8081 -n bitso-trading-dev &
curl http://localhost:8081/health

# Kill port-forward processes when done
pkill -f "kubectl port-forward"
```

### 3.2 Test Kafka Connectivity

```bash
# Exec into a pod to test Kafka
kubectl exec -it deployment/trading-engine -n bitso-trading-dev -- /bin/sh

# Inside the pod, test Kafka connection
# (if kafka tools are available)
kafka-topics.sh --list --bootstrap-server kafka:9092
```

### 3.3 Test Redis Connectivity

```bash
# Exec into a pod to test Redis
kubectl exec -it deployment/trading-engine -n bitso-trading-dev -- /bin/sh

# Inside the pod
redis-cli -h redis -p 6379 ping
```

### 3.4 Verify Metrics Endpoints

```bash
# Test metrics endpoints for each service
kubectl port-forward svc/trading-engine 8080:8080 -n bitso-trading-dev &
curl http://localhost:8080/metrics

kubectl port-forward svc/strategy-executor 8082:8082 -n bitso-trading-dev &
curl http://localhost:8082/metrics
```

### Checklist

- [ ] All health endpoints return 200 OK
- [ ] API Gateway routes are responding
- [ ] Kafka is accessible from pods
- [ ] Redis is accessible from pods
- [ ] Metrics endpoints exposing data
- [ ] No connection errors in logs

---

## Phase 4: Monitoring Stack Deployment

**Goal:** Deploy Prometheus and Grafana for observability.

### 4.1 Deploy Prometheus Stack

```bash
# Add Prometheus community Helm repo
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update

# Install kube-prometheus-stack
helm install prometheus prometheus-community/kube-prometheus-stack \
  -n monitoring \
  --create-namespace \
  --set prometheus.prometheusSpec.serviceMonitorSelectorNilUsesHelmValues=false \
  --set prometheus.prometheusSpec.podMonitorSelectorNilUsesHelmValues=false
```

### 4.2 Configure ServiceMonitors

Create ServiceMonitors for trading services:

```yaml
# Create file: k8s/monitoring/service-monitors.yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: trading-services
  namespace: monitoring
  labels:
    release: prometheus
spec:
  namespaceSelector:
    matchNames:
      - bitso-trading-dev
  selector:
    matchLabels:
      app.kubernetes.io/part-of: bitso-trading-platform
  endpoints:
    - port: http
      path: /metrics
      interval: 15s
```

```bash
# Apply the ServiceMonitor
kubectl apply -f k8s/monitoring/service-monitors.yaml
```

### 4.3 Import Grafana Dashboards

```bash
# Port-forward to Grafana (kube-prometheus-stack)
kubectl port-forward svc/kube-prometheus-stack-grafana 3000:80 -n monitoring

# Or use the helper script (keeps forwarding in foreground)
./scripts/grafana-port-forward.sh

# Access Grafana at http://localhost:3000
# Default credentials: admin / admin

# When using the IDE behind CloudFront (e.g. https://ddeyf7tq41v1l.cloudfront.net/proxy/3000):
# 1) Caddy must route /proxy/3000 to 127.0.0.1:3000 (this is in the IDE CloudFormation; existing IDEs
#    can apply it by hand: edit /etc/caddy/Caddyfile, add the handle /proxy/3000* block, then sudo systemctl restart caddy).
# 2) Run the port-forward on the IDE so Grafana is reachable: ./scripts/grafana-port-forward.sh (in a terminal).

# Import dashboard from monitoring/grafana/dashboards/trading-metrics.json
```

### 4.4 Configure Alert Rules

```bash
# Apply Prometheus alert rules
kubectl create configmap prometheus-alerts \
  --from-file=monitoring/prometheus/rules/trading-alerts.yml \
  -n monitoring
```

### 4.5 Set Up Alertmanager (Optional)

```bash
# Configure Alertmanager for notifications (Slack, email, etc.)
# Edit the alertmanager secret or values in Helm
```

### Checklist

- [ ] Prometheus deployed and running
- [ ] Grafana deployed and accessible
- [ ] ServiceMonitors created for trading services
- [ ] Targets visible in Prometheus (Status > Targets)
- [ ] Trading dashboard imported in Grafana
- [ ] Alert rules configured
- [ ] Alertmanager configured (optional)

---

## Phase 5: Security Policies

**Goal:** Apply network policies and RBAC for security hardening.

### 5.1 Apply Network Policies

```bash
# Apply default deny policy
kubectl apply -f security/network-policies/default-deny.yml -n bitso-trading-dev

# Apply service-specific policies
kubectl apply -f security/network-policies/service-policies.yml -n bitso-trading-dev

# Verify policies are applied
kubectl get networkpolicies -n bitso-trading-dev
```

### 5.2 Apply RBAC Roles

```bash
# Apply RBAC configuration
kubectl apply -f security/rbac/roles.yml

# Verify roles and bindings
kubectl get roles,rolebindings -n bitso-trading-dev
```

### 5.3 Verify Network Policies Work

```bash
# Test that unauthorized connections are blocked
# From a pod that shouldn't have access:
kubectl exec -it deployment/backtesting -n bitso-trading-dev -- \
  curl -s --connect-timeout 5 http://trading-engine:8080/health

# This should timeout or be blocked based on your policies
```

### 5.4 Enable Pod Security Standards (Optional)

```bash
# Label namespace for pod security
kubectl label namespace bitso-trading-dev \
  pod-security.kubernetes.io/enforce=restricted \
  pod-security.kubernetes.io/warn=restricted
```

### Checklist

- [ ] Default deny network policy applied
- [ ] Service-specific network policies applied
- [ ] RBAC roles and bindings created
- [ ] Unauthorized connections blocked
- [ ] Pod security standards enabled (optional)

---

## Phase 6: Integration Testing

**Goal:** Test service-to-service communication and data flow.

### 6.1 Test Market Data Flow

```bash
# 1. Verify market-data service is receiving data from Bitso API
kubectl logs -f deployment/market-data -n bitso-trading-dev | grep -i "ticker\|trade"

# 2. Verify market-data is publishing to Kafka
kubectl exec -it deployment/kafka -n bitso-trading-dev -- \
  kafka-console-consumer.sh --bootstrap-server localhost:9092 \
  --topic market-data.tickers --from-beginning --max-messages 5
```

### 6.2 Test Strategy Execution Flow

```bash
# 1. Verify strategy-executor is consuming market data
kubectl logs -f deployment/strategy-executor -n bitso-trading-dev | grep -i "signal\|strategy"

# 2. Check strategy signals are being published
kubectl exec -it deployment/kafka -n bitso-trading-dev -- \
  kafka-console-consumer.sh --bootstrap-server localhost:9092 \
  --topic strategy.signals --from-beginning --max-messages 5
```

### 6.3 Test Order Flow

```bash
# 1. Verify trading-engine receives signals
kubectl logs -f deployment/trading-engine -n bitso-trading-dev | grep -i "order\|signal"

# 2. Verify order-management receives orders
kubectl logs -f deployment/order-management -n bitso-trading-dev | grep -i "order"
```

### 6.4 Test API Gateway Routing

```bash
kubectl port-forward svc/api-gateway 8085:8085 -n bitso-trading-dev &

# Test routes to different services
curl http://localhost:8085/api/v1/market-data/btc-mxn
curl http://localhost:8085/api/v1/strategies
curl http://localhost:8085/api/v1/orders
curl http://localhost:8085/api/v1/backtest/status
```

### 6.5 Run Automated Integration Tests

```bash
# If you have integration test files in testing/
cd testing/
go test -v -tags=integration ./...
```

### Checklist

- [ ] Market data flowing from Bitso API
- [ ] Market data published to Kafka topics
- [ ] Strategy executor consuming market data
- [ ] Signals being generated and published
- [ ] Trading engine processing signals
- [ ] Order management receiving orders
- [ ] API Gateway routing correctly
- [ ] All integration tests passing

---

## Phase 7: External Access Setup

**Goal:** Configure ingress and external access to the API.

### 7.1 Install AWS Load Balancer Controller

```bash
# Install AWS Load Balancer Controller
helm repo add eks https://aws.github.io/eks-charts
helm repo update

helm install aws-load-balancer-controller eks/aws-load-balancer-controller \
  -n kube-system \
  --set clusterName=<your-cluster-name> \
  --set serviceAccount.create=false \
  --set serviceAccount.name=aws-load-balancer-controller
```

### 7.2 Create Ingress Resource

```yaml
# Create file: k8s/base/ingress.yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: trading-api-ingress
  namespace: bitso-trading-dev
  annotations:
    kubernetes.io/ingress.class: alb
    alb.ingress.kubernetes.io/scheme: internet-facing
    alb.ingress.kubernetes.io/target-type: ip
    alb.ingress.kubernetes.io/healthcheck-path: /health
spec:
  rules:
    - http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: api-gateway
                port:
                  number: 8085
```

```bash
kubectl apply -f k8s/base/ingress.yaml -n bitso-trading-dev
```

### 7.3 Configure TLS/SSL

```bash
# Option 1: Use AWS ACM certificate
# Add annotation to ingress:
# alb.ingress.kubernetes.io/certificate-arn: arn:aws:acm:...

# Option 2: Use cert-manager with Let's Encrypt
helm repo add jetstack https://charts.jetstack.io
helm install cert-manager jetstack/cert-manager \
  -n cert-manager \
  --create-namespace \
  --set installCRDs=true
```

### 7.4 Configure DNS

```bash
# Get the ALB DNS name
kubectl get ingress trading-api-ingress -n bitso-trading-dev

# Create Route53 record (or your DNS provider)
# Point your domain to the ALB DNS name
```

### 7.5 Verify External Access

```bash
# Get the external URL
EXTERNAL_URL=$(kubectl get ingress trading-api-ingress -n bitso-trading-dev \
  -o jsonpath='{.status.loadBalancer.ingress[0].hostname}')

# Test external access
curl http://$EXTERNAL_URL/health
curl http://$EXTERNAL_URL/api/v1/status
```

### Checklist

- [ ] Load balancer controller installed
- [ ] Ingress resource created
- [ ] ALB provisioned and healthy
- [ ] TLS/SSL configured
- [ ] DNS configured
- [ ] External health check passing
- [ ] API accessible from internet

---

## Phase 8: End-to-End Testing

**Goal:** Validate complete trading workflows.

### 8.1 Test Complete Trading Flow

```bash
# 1. Start monitoring logs from all services
kubectl logs -f deployment/market-data -n bitso-trading-dev &
kubectl logs -f deployment/strategy-executor -n bitso-trading-dev &
kubectl logs -f deployment/trading-engine -n bitso-trading-dev &
kubectl logs -f deployment/order-management -n bitso-trading-dev &

# 2. Trigger a test trade via API (if test mode is available)
curl -X POST http://$EXTERNAL_URL/api/v1/test/trigger-signal \
  -H "Content-Type: application/json" \
  -d '{"book": "btc_mxn", "signal": "buy", "amount": 0.001}'

# 3. Observe the flow through logs
```

### 8.2 Test Backtesting Flow

```bash
# Run a backtest
curl -X POST http://$EXTERNAL_URL/api/v1/backtest/run \
  -H "Content-Type: application/json" \
  -d '{
    "strategy": "basic",
    "book": "btc_mxn",
    "start_date": "2025-01-01",
    "end_date": "2025-01-15"
  }'

# Check backtest status
curl http://$EXTERNAL_URL/api/v1/backtest/status/{backtest_id}
```

### 8.3 Test Error Handling

```bash
# Test invalid requests
curl -X POST http://$EXTERNAL_URL/api/v1/orders \
  -H "Content-Type: application/json" \
  -d '{"invalid": "data"}'

# Should return proper error response

# Test rate limiting (if configured)
for i in {1..100}; do
  curl -s http://$EXTERNAL_URL/health &
done
wait
```

### 8.4 Test Failover Scenarios

```bash
# Kill a pod and verify recovery
kubectl delete pod -l app=strategy-executor -n bitso-trading-dev

# Watch pod recreation
kubectl get pods -n bitso-trading-dev -w

# Verify service continues working
curl http://$EXTERNAL_URL/api/v1/strategies
```

### Checklist

- [ ] Complete trading flow works end-to-end
- [ ] Backtesting produces results
- [ ] Error handling returns proper responses
- [ ] Rate limiting works (if configured)
- [ ] Services recover from pod failures
- [ ] No data loss during failures

---

## Phase 9: Load Testing

**Goal:** Verify system performance under load.

### 9.1 Install Load Testing Tool

```bash
# Install k6 for load testing
# On macOS
brew install k6

# On Linux
sudo gpg -k
sudo gpg --no-default-keyring --keyring /usr/share/keyrings/k6-archive-keyring.gpg \
  --keyserver hkp://keyserver.ubuntu.com:80 --recv-keys C5AD17C747E3415A3642D57D77C6C491D6AC1D69
echo "deb [signed-by=/usr/share/keyrings/k6-archive-keyring.gpg] https://dl.k6.io/deb stable main" | \
  sudo tee /etc/apt/sources.list.d/k6.list
sudo apt-get update && sudo apt-get install k6
```

### 9.2 Create Load Test Script

```javascript
// Create file: testing/load/api-load-test.js
import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
  stages: [
    { duration: '1m', target: 10 },   // Ramp up to 10 users
    { duration: '3m', target: 10 },   // Stay at 10 users
    { duration: '1m', target: 50 },   // Ramp up to 50 users
    { duration: '3m', target: 50 },   // Stay at 50 users
    { duration: '1m', target: 0 },    // Ramp down
  ],
  thresholds: {
    http_req_duration: ['p(95)<500'],  // 95% of requests under 500ms
    http_req_failed: ['rate<0.01'],    // Less than 1% failures
  },
};

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8085';

export default function () {
  // Health check
  let healthRes = http.get(`${BASE_URL}/health`);
  check(healthRes, {
    'health status is 200': (r) => r.status === 200,
  });

  // Get market data
  let marketRes = http.get(`${BASE_URL}/api/v1/market-data/btc-mxn`);
  check(marketRes, {
    'market data status is 200': (r) => r.status === 200,
  });

  // Get strategies
  let strategyRes = http.get(`${BASE_URL}/api/v1/strategies`);
  check(strategyRes, {
    'strategies status is 200': (r) => r.status === 200,
  });

  sleep(1);
}
```

### 9.3 Run Load Tests

```bash
# Run load test
k6 run -e BASE_URL=http://$EXTERNAL_URL testing/load/api-load-test.js

# Run with HTML report
k6 run --out json=results.json -e BASE_URL=http://$EXTERNAL_URL testing/load/api-load-test.js
```

### 9.4 Monitor During Load Test

```bash
# Watch pod metrics during load test
kubectl top pods -n bitso-trading-dev --containers

# Watch HPA scaling (if configured)
kubectl get hpa -n bitso-trading-dev -w

# Check Grafana dashboards for real-time metrics
```

### 9.5 Analyze Results

Review load test results for:
- Response time percentiles (p50, p95, p99)
- Error rates
- Throughput (requests per second)
- Resource utilization

### Checklist

- [ ] Load test scripts created
- [ ] Baseline performance established
- [ ] System handles expected load
- [ ] Response times within SLA
- [ ] Error rate below threshold
- [ ] No memory leaks under load
- [ ] Auto-scaling triggers correctly (if configured)

---

## Phase 10: Production Readiness

**Goal:** Final verification before production deployment.

### 10.1 Documentation Review

- [ ] README updated with deployment instructions
- [ ] API documentation complete
- [ ] Runbook for common issues created
- [ ] Architecture diagram updated
- [ ] Environment variables documented

### 10.2 Security Audit

- [ ] No secrets in code or configs
- [ ] All secrets in AWS Secrets Manager
- [ ] Network policies enforced
- [ ] RBAC properly configured
- [ ] TLS enabled for all external traffic
- [ ] API authentication enabled (if required)

### 10.3 Monitoring Verification

- [ ] All services have health checks
- [ ] Metrics being collected
- [ ] Dashboards show key metrics
- [ ] Alerts configured for critical issues
- [ ] On-call rotation set up

### 10.4 Backup & Recovery

- [ ] Database backup strategy (if applicable)
- [ ] Configuration backup
- [ ] Disaster recovery plan documented
- [ ] Recovery procedures tested

### 10.5 CI/CD Verification

```bash
# Verify GitHub secrets are configured
# In GitHub repo settings, check for:
# - AWS_GITHUB_ACTIONS_ROLE_ARN
# - EKS_CLUSTER_NAME_STAGING
# - EKS_CLUSTER_NAME_PRODUCTION
```

- [ ] Build pipeline working
- [ ] Deploy pipeline tested
- [ ] Rollback procedure documented
- [ ] Blue-green or canary deployment ready

### 10.6 Final Sign-Off

| Item | Status | Verified By | Date |
|------|--------|-------------|------|
| All services healthy | ☐ | | |
| Monitoring operational | ☐ | | |
| Security hardened | ☐ | | |
| Load testing passed | ☐ | | |
| Documentation complete | ☐ | | |
| CI/CD verified | ☐ | | |

---

## Quick Reference Commands

```bash
# Check overall status
kubectl get all -n bitso-trading-dev

# Restart a deployment
kubectl rollout restart deployment/<service-name> -n bitso-trading-dev

# Scale a deployment
kubectl scale deployment/<service-name> --replicas=3 -n bitso-trading-dev

# View logs with timestamps
kubectl logs -f deployment/<service-name> -n bitso-trading-dev --timestamps

# Get shell in a pod
kubectl exec -it deployment/<service-name> -n bitso-trading-dev -- /bin/sh

# Port-forward for debugging
kubectl port-forward svc/<service-name> <local-port>:<service-port> -n bitso-trading-dev

# Rollback a deployment
kubectl rollout undo deployment/<service-name> -n bitso-trading-dev
```

---

## Troubleshooting

### Pod Not Starting

```bash
kubectl describe pod <pod-name> -n bitso-trading-dev
kubectl logs <pod-name> -n bitso-trading-dev --previous
```

### Service Not Accessible

```bash
kubectl get endpoints <service-name> -n bitso-trading-dev
kubectl get networkpolicies -n bitso-trading-dev
```

### High Latency

```bash
kubectl top pods -n bitso-trading-dev
# Check Grafana for bottlenecks
```

### Kafka Issues

```bash
kubectl logs deployment/kafka -n bitso-trading-dev
kubectl exec -it deployment/kafka -n bitso-trading-dev -- \
  kafka-topics.sh --describe --bootstrap-server localhost:9092
```

---

**Document Version:** 1.0  
**Last Updated:** January 24, 2026  
**Maintainer:** Platform Team
