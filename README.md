# Microservices Trading Bot

A microservices-based trading bot that integrates with the [Bitso](https://bitso.com) exchange. It consumes market data, runs configurable strategies, executes orders, and supports backtesting.

## Architecture

- **market-data** – Bitso WebSocket/REST → Kafka (trades, tickers, order book).
- **strategy-executor** – Consumes market data, runs strategies (basic, trend, arbitrage), publishes signals to Kafka.
- **trading-engine** – Consumes signals and places orders via the Bitso API.
- **order-management** – Orders, positions, and risk checks.
- **api-gateway** – HTTP API and routing to backend services.
- **backtesting** – Historical strategy evaluation and optimization.

Deployment targets Kubernetes (EKS) with **Kafka** (market data and signals), **Redis** (order-management orders/positions, backtesting cache and storage), and optional **centralized logging** (e.g. Loki or CloudWatch) for observability.

## Bitso Testing Environment

The bot supports Bitso’s **testing (stage) environment** so you can develop and test without using production. See [Bitso: Set up your testing environment](https://docs.bitso.com/bitso-api/docs/set-up-your-testing-environment).

- **Stage API base URL:** `https://stage.bitso.com/api`
- **Production API base URL:** `https://bitso.com/api`

The trading-engine uses **stage by default**. Set `BITSO_API_BASE_URL` to switch without code changes (see `shared/pkg/config`). Use stage credentials (`STAGE_BITSO_API_KEY`, `STAGE_BITSO_APISECRET`) with the stage URL and production keys only with the production URL.

## Planning: Intraday Strategies & Metrics

Before running new intraday strategies live, follow the steps in:

- **[INTRADAY-STRATEGY-IMPLEMENTATION-PLAN.md](./INTRADAY-STRATEGY-IMPLEMENTATION-PLAN.md)** – Phases for Bitso env config, paper trading, position sync, daily loss/drawdown limits, intraday metrics, and backtesting.

## Documentation

| Document | Description |
|----------|-------------|
| [DEVELOPMENT-ROADMAP.md](./DEVELOPMENT-ROADMAP.md) | Strategy enhancements, paper trading, position management, risk controls. |
| [REMAINING-PHASES-CHECKLIST.md](./REMAINING-PHASES-CHECKLIST.md) | Deployment phases (1–10), centralized logging/observability, Redis deployment, and verification. |
| [POST-DEPLOYMENT-CHECKLIST.md](./POST-DEPLOYMENT-CHECKLIST.md) | Post-deploy verification and hardening. |
| [docs/ORDER-FLOW-AND-BITSO-TESTING.md](./docs/ORDER-FLOW-AND-BITSO-TESTING.md) | When orders go to Bitso testing, how to validate the flow, and relevant env vars. |
| [docs/OPERATIONS-ENV-VARS.md](./docs/OPERATIONS-ENV-VARS.md) | Env vars for trading-engine, order-management, and session risk. |

## Quick Start

1. Set environment variables (see `.env.example` and service READMEs). For testing, use Bitso stage credentials and stage API URL.
2. Run dependencies: **Kafka** (market data and signals) and **Redis** (required by order-management and backtesting; set `REDIS_HOST` and optionally `REDIS_PORT` / `REDIS_PASSWORD`). Then run services locally or via `k8s/` and `config/`.
3. Use the API gateway for health, status, strategies, and backtest endpoints.

**Docker Compose:** If `docker compose build` fails with "compose build requires buildx 0.17.0 or later", build the image with plain Docker instead: `./scripts/docker-build-market-data.sh`, then `docker-compose up -d market-data`.

For full deployment (EKS, monitoring, centralized logging, Redis, and security), see [REMAINING-PHASES-CHECKLIST.md](./REMAINING-PHASES-CHECKLIST.md).
