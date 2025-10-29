# Backtesting Service - Deployment Guide

**Version**: 1.0.0  
**Last Updated**: October 28, 2025

---

## Table of Contents

- [Prerequisites](#prerequisites)
- [Docker Deployment](#docker-deployment)
- [Kubernetes Deployment](#kubernetes-deployment)
- [Production Configuration](#production-configuration)
- [Monitoring](#monitoring)
- [Scaling](#scaling)
- [Disaster Recovery](#disaster-recovery)

---

## Prerequisites

### Required Services

1. **Redis** (v6.0+)
   - Used for result storage and caching
   - Recommended: 2GB+ memory
   - Persistence enabled

2. **Market-Data Service**
   - Source of historical market data
   - Must be accessible via HTTP
   - Recommended: <100ms latency

### Optional Services

1. **Prometheus** (for metrics)
2. **Grafana** (for visualization)
3. **Jaeger** (for tracing)

### Resource Requirements

**Minimum (Development)**:
- CPU: 1 core
- Memory: 512 MB
- Storage: 1 GB

**Recommended (Production)**:
- CPU: 2-4 cores
- Memory: 2-4 GB
- Storage: 10 GB+

---

## Docker Deployment

### Build Docker Image

```bash
cd services/backtesting

# Build image
docker build -t backtesting:1.0.0 .

# Tag for registry
docker tag backtesting:1.0.0 registry.example.com/backtesting:1.0.0

# Push to registry
docker push registry.example.com/backtesting:1.0.0
```

### Docker Compose

```yaml
version: '3.8'

services:
  backtesting:
    image: backtesting:1.0.0
    container_name: backtesting
    ports:
      - "8084:8084"
    environment:
      - SERVICE_NAME=backtesting
      - SERVICE_PORT=8084
      - ENVIRONMENT=production
      
      # Redis
      - REDIS_HOST=redis
      - REDIS_PORT=6379
      - REDIS_PASSWORD=changeme
      - REDIS_DB=0
      
      # Market Data
      - MARKET_DATA_BASE_URL=http://market-data:8083
      - MARKET_DATA_TIMEOUT=30s
      
      # Storage
      - STORAGE_TYPE=redis
      - STORAGE_RETENTION_DAYS=90
      
      # Execution
      - MAX_CONCURRENT_BACKTESTS=10
      
      # Logging
      - LOG_LEVEL=info
      - LOG_FORMAT=json
    depends_on:
      - redis
    networks:
      - trading-network
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8084/health/live"]
      interval: 30s
      timeout: 10s
      retries: 3

  redis:
    image: redis:7-alpine
    container_name: redis-backtesting
    ports:
      - "6379:6379"
    volumes:
      - redis-data:/data
    command: redis-server --appendonly yes --requirepass changeme
    networks:
      - trading-network

volumes:
  redis-data:

networks:
  trading-network:
    external: true
```

### Run Container

```bash
docker-compose up -d

# Check logs
docker-compose logs -f backtesting

# Check health
curl http://localhost:8084/health
```

---

## Kubernetes Deployment

### Deployment YAML

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: backtesting
  namespace: trading
  labels:
    app: backtesting
    version: 1.0.0
spec:
  replicas: 3
  selector:
    matchLabels:
      app: backtesting
  template:
    metadata:
      labels:
        app: backtesting
        version: 1.0.0
    spec:
      containers:
      - name: backtesting
        image: registry.example.com/backtesting:1.0.0
        imagePullPolicy: Always
        ports:
        - name: http
          containerPort: 8084
          protocol: TCP
        env:
        - name: SERVICE_NAME
          value: "backtesting"
        - name: SERVICE_PORT
          value: "8084"
        - name: ENVIRONMENT
          value: "production"
        
        # Redis
        - name: REDIS_HOST
          valueFrom:
            configMapKeyRef:
              name: backtesting-config
              key: redis-host
        - name: REDIS_PORT
          value: "6379"
        - name: REDIS_PASSWORD
          valueFrom:
            secretKeyRef:
              name: redis-secret
              key: password
        
        # Market Data
        - name: MARKET_DATA_BASE_URL
          value: "http://market-data:8083"
        
        # Storage
        - name: STORAGE_TYPE
          value: "redis"
        
        # Execution
        - name: MAX_CONCURRENT_BACKTESTS
          value: "10"
        
        # Logging
        - name: LOG_LEVEL
          value: "info"
        
        resources:
          requests:
            cpu: "500m"
            memory: "512Mi"
          limits:
            cpu: "2"
            memory: "2Gi"
        
        livenessProbe:
          httpGet:
            path: /health/live
            port: 8084
          initialDelaySeconds: 30
          periodSeconds: 10
          timeoutSeconds: 5
          failureThreshold: 3
        
        readinessProbe:
          httpGet:
            path: /health/ready
            port: 8084
          initialDelaySeconds: 5
          periodSeconds: 5
          timeoutSeconds: 3
          failureThreshold: 3
        
        volumeMounts:
        - name: config
          mountPath: /etc/backtesting
          readOnly: true
      
      volumes:
      - name: config
        configMap:
          name: backtesting-config
```

### Service

```yaml
apiVersion: v1
kind: Service
metadata:
  name: backtesting
  namespace: trading
spec:
  type: ClusterIP
  ports:
  - port: 8084
    targetPort: 8084
    protocol: TCP
    name: http
  selector:
    app: backtesting
```

### ConfigMap

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: backtesting-config
  namespace: trading
data:
  redis-host: "redis.trading.svc.cluster.local"
  market-data-url: "http://market-data.trading.svc.cluster.local:8083"
  max-concurrent-backtests: "10"
  storage-retention-days: "90"
```

### Horizontal Pod Autoscaler

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: backtesting-hpa
  namespace: trading
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: backtesting
  minReplicas: 3
  maxReplicas: 10
  metrics:
  - type: Resource
    resource:
      name: cpu
      target:
        type: Utilization
        averageUtilization: 70
  - type: Resource
    resource:
      name: memory
      target:
        type: Utilization
        averageUtilization: 80
```

---

## Production Configuration

### Environment Variables

```bash
# Service
SERVICE_NAME=backtesting
SERVICE_VERSION=1.0.0
SERVICE_HOST=0.0.0.0
SERVICE_PORT=8084
ENVIRONMENT=production

# Redis
REDIS_HOST=redis.production.internal
REDIS_PORT=6379
REDIS_PASSWORD=<secret>
REDIS_DB=0
REDIS_POOL_SIZE=20

# Market Data
MARKET_DATA_BASE_URL=https://market-data.internal
MARKET_DATA_TIMEOUT=30s
MARKET_DATA_RETRY_COUNT=3
MARKET_DATA_RETRY_DELAY=1s

# Storage
STORAGE_TYPE=redis
STORAGE_PATH=/var/lib/backtesting/results
STORAGE_RETENTION_DAYS=90

# Execution
MAX_CONCURRENT_BACKTESTS=10
DEFAULT_SLIPPAGE_MODEL=percentage
DEFAULT_SLIPPAGE_VALUE=0.001
DEFAULT_COMMISSION_RATE=0.001

# Logging
LOG_LEVEL=info
LOG_FORMAT=json
LOG_OUTPUT=stdout

# Metrics
METRICS_ENABLED=true
METRICS_PATH=/metrics
```

### Security

1. **Network Policies**
   ```yaml
   apiVersion: networking.k8s.io/v1
   kind: NetworkPolicy
   metadata:
     name: backtesting-netpol
   spec:
     podSelector:
       matchLabels:
         app: backtesting
     policyTypes:
     - Ingress
     - Egress
     ingress:
     - from:
       - namespaceSelector:
           matchLabels:
             name: api-gateway
       ports:
       - protocol: TCP
         port: 8084
     egress:
     - to:
       - podSelector:
           matchLabels:
             app: redis
       ports:
       - protocol: TCP
         port: 6379
     - to:
       - podSelector:
           matchLabels:
             app: market-data
       ports:
       - protocol: TCP
         port: 8083
   ```

2. **RBAC**
   ```yaml
   apiVersion: v1
   kind: ServiceAccount
   metadata:
     name: backtesting
     namespace: trading
   ---
   apiVersion: rbac.authorization.k8s.io/v1
   kind: Role
   metadata:
     name: backtesting
   rules:
   - apiGroups: [""]
     resources: ["configmaps", "secrets"]
     verbs: ["get", "list"]
   ```

---

## Monitoring

### Prometheus Metrics

The service exposes metrics at `/metrics`:

```promql
# Backtest rate
rate(backtests_created_total[5m])

# Success rate
rate(backtests_completed_total{status="completed"}[5m]) 
/ 
rate(backtests_created_total[5m])

# Average duration
histogram_quantile(0.95, backtest_duration_seconds)

# Active backtests
active_backtests

# Error rate
rate(backtests_completed_total{status="failed"}[5m])
```

### Grafana Dashboard

```json
{
  "dashboard": {
    "title": "Backtesting Service",
    "panels": [
      {
        "title": "Backtest Rate",
        "targets": [
          "rate(backtests_created_total[5m])"
        ]
      },
      {
        "title": "Active Backtests",
        "targets": [
          "active_backtests"
        ]
      },
      {
        "title": "Success Rate",
        "targets": [
          "rate(backtests_completed_total{status=\"completed\"}[5m]) / rate(backtests_created_total[5m])"
        ]
      }
    ]
  }
}
```

### Alerts

```yaml
groups:
- name: backtesting
  rules:
  - alert: BacktestingHighErrorRate
    expr: rate(backtests_completed_total{status="failed"}[5m]) > 0.1
    for: 5m
    annotations:
      summary: "High backtest failure rate"
  
  - alert: BacktestingDown
    expr: up{job="backtesting"} == 0
    for: 1m
    annotations:
      summary: "Backtesting service is down"
  
  - alert: BacktestingHighLatency
    expr: histogram_quantile(0.95, backtest_duration_seconds) > 300
    for: 10m
    annotations:
      summary: "Backtest execution time > 5 minutes"
```

---

## Scaling

### Vertical Scaling

Increase resources for a single instance:

```yaml
resources:
  requests:
    cpu: "2"
    memory: "4Gi"
  limits:
    cpu: "4"
    memory: "8Gi"
```

### Horizontal Scaling

Use HPA (see Kubernetes Deployment section).

### Redis Scaling

For high volume, consider:
- Redis Cluster mode
- Separate Redis instances for cache vs storage
- Read replicas for queries

---

## Disaster Recovery

### Backup Strategy

1. **Redis Backup**
   ```bash
   # Scheduled Redis backup
   redis-cli --rdb /backup/redis-$(date +%Y%m%d).rdb
   ```

2. **Result Storage Backup**
   - Daily exports to object storage
   - Retention: 90 days

3. **Configuration Backup**
   - Git version control
   - ConfigMap snapshots

### Recovery Procedures

1. **Service Recovery**
   ```bash
   # Scale down
   kubectl scale deployment backtesting --replicas=0
   
   # Restore Redis
   redis-cli < /backup/redis-latest.rdb
   
   # Scale up
   kubectl scale deployment backtesting --replicas=3
   ```

2. **Data Recovery**
   - Restore from Redis backup
   - Re-run failed backtests if needed

---

## Troubleshooting

### Common Issues

**1. Redis Connection Fails**
```bash
# Check Redis connectivity
redis-cli -h <redis-host> ping

# Check network policies
kubectl get networkpolicies -n trading
```

**2. Out of Memory**
```bash
# Check memory usage
kubectl top pod -l app=backtesting

# Reduce concurrent backtests
kubectl set env deployment/backtesting MAX_CONCURRENT_BACKTESTS=5
```

**3. Slow Backtests**
```bash
# Check market-data latency
curl -w "%{time_total}" http://market-data:8083/health

# Check Redis performance
redis-cli --latency
```

---

**Last Updated**: October 28, 2025  
**Version**: 1.0.0


