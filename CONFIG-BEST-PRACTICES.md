# ==============================================================================
# Best Practices for Environment Configuration
# ==============================================================================

## Directory Structure

```
microservices-trading-bot/
├── .env                          # Global config (gitignored)
├── .env.example                  # Global template (committed)
├── .gitignore                    # Ensure .env is ignored
├── services/
│   ├── trading-engine/
│   │   ├── .env                 # Service-specific overrides (gitignored)
│   │   └── .env.example         # Service template (committed)
│   ├── market-data/
│   │   ├── .env
│   │   └── .env.example
│   └── ...
└── shared/pkg/config/
    └── config.go                # Smart config loader
```

## Configuration Hierarchy

### Priority Order (highest to lowest):
1. **System Environment Variables** - Highest priority
2. **Service-Level .env** - Service-specific overrides
3. **Root .env** - Shared configuration
4. **Default Values** - Fallbacks in code

### Example Flow:

When `trading-engine` starts:
```
1. Load shared/pkg/config
2. Search for .env files:
   - ./services/trading-engine/.env    (service-specific)
   - ./services/.env
   - ./.env                             (root - shared)
3. Merge with system environment
4. Apply defaults for missing values
```

## What Goes Where?

### Root .env (Shared Infrastructure)
```env
# Shared across ALL services

# API Keys & Secrets
BITSO_API_KEY=xxx
BITSO_API_SECRET=yyy
STAGE_BITSO_API_KEY=xxx
STAGE_BITSO_API_SECRET=yyy

# Database
REDIS_HOST=localhost
REDIS_PORT=6379
REDIS_PASSWORD=
REDIS_DB=0

# Message Broker
KAFKA_BROKERS=localhost:9092

# Environment
ENVIRONMENT=development
LOG_LEVEL=info
```

### Service .env (Service-Specific)
```env
# Trading Engine Only

# Service Identity
SERVICE_NAME=trading-engine
SERVICE_PORT=8080

# Trading Parameters
TRADING_BOOK=btc_mxn
MIN_TRADE_AMOUNT=0.001
MAX_TRADE_AMOUNT=0.1
MAX_OPEN_POSITIONS=3

# Kafka Topics (service-specific)
KAFKA_CONSUMER_GROUP=trading-engine-group
KAFKA_TOPIC_TRADE_SIGNALS=trade-signals
KAFKA_TOPIC_ORDER_EVENTS=order-events

# Feature Flags
ENABLE_PAPER_TRADING=true
ENABLE_METRICS=true
```

## Docker Compose Configuration

```yaml
version: '3.8'

services:
  trading-engine:
    build: ./services/trading-engine
    env_file:
      - .env                                    # Root (shared)
      - ./services/trading-engine/.env          # Service-specific
    environment:
      # Can also override specific vars here
      - SERVICE_NAME=trading-engine
      - LOG_LEVEL=${LOG_LEVEL:-info}
    networks:
      - trading-network
```

## Benefits of This Approach

### ✅ Advantages:

1. **DRY Principle**: Shared config in one place
2. **Service Autonomy**: Each service can override as needed
3. **Easy Local Development**: One root .env for basics
4. **Docker-Friendly**: Works seamlessly with docker-compose
5. **Security**: Secrets in gitignored files
6. **Flexibility**: Override per environment (dev/staging/prod)
7. **Documentation**: .env.example files serve as docs

### ✅ Scalability:

- Add new services easily
- Consistent configuration across services
- Easy to add new shared infrastructure
- Service-specific tuning without affecting others

## Migration Steps

If you already have .env files scattered:

1. **Consolidate shared config** to root .env
2. **Keep service-specific** in service folders
3. **Create .env.example** for both levels
4. **Update .gitignore** to exclude .env files
5. **Document** which vars go where
6. **Test** that services still load config correctly

## Security Notes

### Production Deployment:

1. **Never commit .env files**
2. **Use secret management** (AWS Secrets Manager, HashiCorp Vault)
3. **Inject via CI/CD** pipeline
4. **Rotate credentials** regularly
5. **Audit access** to secrets

### For Kubernetes:

```yaml
# Use ConfigMaps for non-sensitive
apiVersion: v1
kind: ConfigMap
metadata:
  name: trading-config
data:
  REDIS_HOST: redis-service
  KAFKA_BROKERS: kafka-service:9092

# Use Secrets for sensitive
apiVersion: v1
kind: Secret
metadata:
  name: api-credentials
type: Opaque
data:
  BITSO_API_KEY: <base64-encoded>
  BITSO_API_SECRET: <base64-encoded>
```

## Summary

**Recommendation for your project:**

✅ **Use BOTH:**
- **Root .env**: Shared infrastructure (Redis, Kafka, API keys)
- **Service .env**: Service-specific settings (ports, topics, features)

This gives you:
- Clean separation of concerns
- Easy local development
- Production-ready configuration management
- Flexibility for different environments

**Current Setup:** If you've added .env to trading-engine, that's good!
Now also create:
1. Root .env for shared config
2. .env.example files for documentation
3. Update .gitignore to protect secrets

