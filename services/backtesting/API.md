# Backtesting Service API Documentation

**Version**: 1.0.0  
**Base URL**: `http://localhost:8084/api/v1`  
**Content-Type**: `application/json`

---

## Table of Contents

- [Authentication](#authentication)
- [Health Endpoints](#health-endpoints)
- [Backtest Endpoints](#backtest-endpoints)
- [Optimization Endpoints](#optimization-endpoints)
- [Error Handling](#error-handling)
- [Rate Limiting](#rate-limiting)

---

## Authentication

Currently, the API does not require authentication. In production, authentication will be added via API Gateway.

---

## Health Endpoints

### GET /health

Full health check with all dependencies.

**Response** (200 OK):
```json
{
  "status": "healthy",
  "timestamp": "2025-10-28T12:00:00Z",
  "checks": {
    "service": {
      "name": "service",
      "status": "healthy",
      "duration": 641,
      "last_checked": "2025-10-28T12:00:00Z"
    },
    "redis": {
      "name": "redis",
      "status": "healthy",
      "duration": 2341,
      "last_checked": "2025-10-28T12:00:00Z"
    },
    "market-data": {
      "name": "market-data",
      "status": "healthy",
      "duration": 1523,
      "last_checked": "2025-10-28T12:00:00Z"
    }
  }
}
```

**Status Codes**:
- `200 OK` - Service is healthy
- `503 Service Unavailable` - One or more dependencies unhealthy

---

### GET /health/live

Liveness probe for Kubernetes.

**Response** (200 OK):
```json
{
  "status": "alive"
}
```

---

### GET /health/ready

Readiness probe for Kubernetes.

**Response** (200 OK):
```json
{
  "status": "ready"
}
```

**Response** (503 Service Unavailable):
```json
{
  "status": "not_ready",
  "reason": "Redis connection failed"
}
```

---

## Backtest Endpoints

### POST /api/v1/backtests

Create a new backtest.

**Request Body**:
```json
{
  "name": "Basic Strategy Test",
  "description": "Testing basic RSI strategy",
  "start_date": "2024-01-01T00:00:00Z",
  "end_date": "2024-12-31T23:59:59Z",
  "book": "btc_mxn",
  "initial_balance": 100000.0,
  "strategy": "basic",
  "strategy_params": {
    "rsi_period": 14,
    "rsi_oversold": 30,
    "rsi_overbought": 70
  },
  "slippage_model": "percentage",
  "slippage_value": 0.001,
  "commission_rate": 0.001,
  "data_source": "market-data",
  "data_granularity": "trades"
}
```

**Field Descriptions**:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Backtest name |
| `description` | string | No | Description |
| `start_date` | string | Yes | Start date (RFC3339) |
| `end_date` | string | Yes | End date (RFC3339) |
| `book` | string | Yes | Trading pair (e.g., "btc_mxn") |
| `initial_balance` | float64 | Yes | Starting balance |
| `strategy` | string | Yes | Strategy name |
| `strategy_params` | object | No | Strategy parameters |
| `slippage_model` | string | No | slippage, percentage, volume_based |
| `slippage_value` | float64 | No | Slippage value |
| `commission_rate` | float64 | No | Commission rate |
| `data_source` | string | No | Data source (default: "market-data") |
| `data_granularity` | string | No | Data granularity |

**Response** (201 Created):
```json
{
  "success": true,
  "data": {
    "id": "bt-a1b2c3d4",
    "status": "pending",
    "created_at": "2025-10-28T12:00:00Z"
  }
}
```

**Response** (400 Bad Request):
```json
{
  "success": false,
  "error": {
    "code": "INVALID_REQUEST",
    "message": "Invalid request body",
    "details": "start_date is required"
  }
}
```

---

### GET /api/v1/backtests

List all backtests with optional filters.

**Query Parameters**:

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `status` | string | - | Filter by status (pending, running, completed, failed, cancelled) |
| `strategy` | string | - | Filter by strategy name |
| `book` | string | - | Filter by trading pair |
| `limit` | int | 20 | Maximum results |
| `offset` | int | 0 | Pagination offset |

**Example**:
```
GET /api/v1/backtests?status=completed&limit=10&offset=0
```

**Response** (200 OK):
```json
{
  "success": true,
  "data": {
    "backtests": [
      {
        "id": "bt-a1b2c3d4",
        "status": "completed",
        "progress": 1.0,
        "created_at": "2025-10-28T12:00:00Z"
      }
    ],
    "total": 45,
    "limit": 10,
    "offset": 0
  }
}
```

---

### GET /api/v1/backtests/{id}

Get backtest status and metadata.

**Response** (200 OK):
```json
{
  "success": true,
  "data": {
    "id": "bt-a1b2c3d4",
    "status": "running",
    "progress": 0.45,
    "created_at": "2025-10-28T12:00:00Z",
    "started_at": "2025-10-28T12:00:05Z",
    "completed_at": null
  }
}
```

**Status Values**:
- `pending` - Queued, not started
- `running` - Currently executing
- `completed` - Finished successfully
- `failed` - Failed with error
- `cancelled` - Cancelled by user

---

### POST /api/v1/backtests/{id}/cancel

Cancel a running backtest.

**Response** (200 OK):
```json
{
  "success": true,
  "data": {
    "id": "bt-a1b2c3d4",
    "status": "cancelled",
    "message": "Backtest cancelled successfully"
  }
}
```

**Response** (400 Bad Request):
```json
{
  "success": false,
  "error": {
    "code": "CANCEL_FAILED",
    "message": "Backtest is not running"
  }
}
```

---

### DELETE /api/v1/backtests/{id}

Delete a backtest and its results.

**Response** (200 OK):
```json
{
  "success": true,
  "data": {
    "message": "Backtest deleted successfully"
  }
}
```

---

### GET /api/v1/backtests/{id}/results

Get complete backtest results.

**Response** (200 OK):
```json
{
  "success": true,
  "data": {
    "id": "bt-a1b2c3d4",
    "status": "completed",
    "summary": {
      "total_return": 15234.50,
      "total_return_percent": 15.23,
      "annualized_return": 15.85,
      "volatility": 0.23,
      "sharpe_ratio": 1.85,
      "sortino_ratio": 2.34,
      "max_drawdown": -5432.10,
      "max_drawdown_percent": -5.12,
      "total_trades": 127,
      "winning_trades": 83,
      "losing_trades": 44,
      "win_rate": 0.65,
      "profit_factor": 2.14,
      "average_win": 234.50,
      "average_loss": -156.30
    },
    "trades": [...],
    "equity_curve": [...],
    "completed_at": "2025-10-28T12:04:32Z",
    "duration_seconds": 272
  }
}
```

---

### GET /api/v1/backtests/{id}/summary

Get performance summary only (lightweight).

**Response** (200 OK):
```json
{
  "success": true,
  "data": {
    "total_return": 15234.50,
    "total_return_percent": 15.23,
    "sharpe_ratio": 1.85,
    "max_drawdown_percent": -5.12,
    "win_rate": 0.65,
    "total_trades": 127
  }
}
```

---

### GET /api/v1/backtests/{id}/trades

Get trade history with pagination.

**Query Parameters**:

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `limit` | int | 100 | Maximum trades |
| `offset` | int | 0 | Pagination offset |

**Response** (200 OK):
```json
{
  "success": true,
  "data": {
    "trades": [
      {
        "id": "trade-1",
        "entry_time": "2024-01-15T10:30:00Z",
        "exit_time": "2024-01-15T14:45:00Z",
        "book": "btc_mxn",
        "side": "buy",
        "entry_price": 500000.0,
        "exit_price": 505000.0,
        "amount": 0.01,
        "profit_loss": 50.0,
        "profit_loss_percent": 1.0,
        "commission": 5.0,
        "slippage": 0.5,
        "holding_time": "4h15m0s"
      }
    ],
    "total": 127,
    "limit": 100,
    "offset": 0
  }
}
```

---

### GET /api/v1/backtests/{id}/report

Download backtest report in various formats.

**Query Parameters**:

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `format` | string | json | Report format: json, text, html |

**Response** (200 OK):
- Content-Type varies by format
- `application/json` - JSON format
- `text/plain` - Text format
- `text/html` - HTML format

---

## Optimization Endpoints

### POST /api/v1/optimizations

Create a parameter optimization.

**Request Body**:
```json
{
  "name": "RSI Strategy Optimization",
  "description": "Find optimal RSI parameters",
  "start_date": "2024-01-01T00:00:00Z",
  "end_date": "2024-12-31T23:59:59Z",
  "book": "btc_mxn",
  "initial_balance": 100000.0,
  "strategy": "basic",
  "parameters": {
    "rsi_period": {
      "min": 10,
      "max": 20,
      "step": 2
    },
    "rsi_oversold": {
      "min": 20,
      "max": 40,
      "step": 5
    },
    "rsi_overbought": {
      "min": 60,
      "max": 80,
      "step": 5
    }
  },
  "metric": "sharpe_ratio",
  "max_workers": 4,
  "top_n": 10,
  "slippage_model": "percentage",
  "slippage_value": 0.001,
  "commission_rate": 0.001
}
```

**Field Descriptions**:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Optimization name |
| `description` | string | No | Description |
| `start_date` | string | Yes | Start date (RFC3339) |
| `end_date` | string | Yes | End date (RFC3339) |
| `book` | string | Yes | Trading pair |
| `initial_balance` | float64 | Yes | Starting balance |
| `strategy` | string | Yes | Strategy name |
| `parameters` | object | Yes | Parameter ranges |
| `metric` | string | No | Optimization metric (default: sharpe_ratio) |
| `max_workers` | int | No | Parallel workers (default: 4) |
| `top_n` | int | No | Keep top N results (default: 10) |

**Parameter Range**:
- Numeric range: `{"min": 10, "max": 20, "step": 2}`
- Discrete values: `{"values": [10, 14, 18, 22]}`

**Optimization Metrics**:
- `sharpe_ratio` - Risk-adjusted returns
- `sortino_ratio` - Downside risk-adjusted
- `total_return` - Total profit/loss
- `profit_factor` - Win $ / Loss $
- `win_rate` - Winning trades %
- `max_drawdown` - Maximum decline (lower is better)
- `composite` - Weighted combination

**Response** (201 Created):
```json
{
  "success": true,
  "data": {
    "id": "opt-789abc",
    "status": "pending",
    "total_runs": 150,
    "created_at": "2025-10-28T12:00:00Z"
  }
}
```

---

### GET /api/v1/optimizations/{id}

Get optimization status.

**Query Parameters**:

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `detailed` | bool | false | Return full optimization object |

**Response** (200 OK):
```json
{
  "success": true,
  "data": {
    "id": "opt-789abc",
    "name": "RSI Strategy Optimization",
    "status": "running",
    "progress": 0.67,
    "total_runs": 150,
    "completed_runs": 100,
    "created_at": "2025-10-28T12:00:00Z",
    "started_at": "2025-10-28T12:00:05Z"
  }
}
```

---

### GET /api/v1/optimizations/{id}/results

Get optimization results (top N).

**Response** (200 OK):
```json
{
  "success": true,
  "data": {
    "results": [
      {
        "rank": 1,
        "score": 2.87,
        "parameters": {
          "rsi_period": 14,
          "rsi_oversold": 30,
          "rsi_overbought": 70
        },
        "result": {
          "summary": {
            "sharpe_ratio": 2.87,
            "total_return": 0.45,
            "max_drawdown": -0.12,
            "win_rate": 0.62
          }
        }
      }
    ],
    "best_result": {
      "rank": 1,
      "score": 2.87,
      "parameters": {...},
      "result": {...}
    },
    "total": 10
  }
}
```

**Response** (400 Bad Request):
```json
{
  "success": false,
  "error": {
    "code": "NOT_COMPLETED",
    "message": "Optimization is not completed yet"
  }
}
```

---

### GET /api/v1/optimizations/{id}/best

Get only the best result.

**Response** (200 OK):
```json
{
  "success": true,
  "data": {
    "rank": 1,
    "score": 2.87,
    "parameters": {
      "rsi_period": 14,
      "rsi_oversold": 30,
      "rsi_overbought": 70
    },
    "result": {
      "summary": {
        "sharpe_ratio": 2.87,
        "total_return": 0.45,
        "max_drawdown": -0.12,
        "win_rate": 0.62
      }
    }
  }
}
```

---

### POST /api/v1/optimizations/{id}/cancel

Cancel a running optimization.

**Response** (200 OK):
```json
{
  "success": true,
  "data": {
    "id": "opt-789abc",
    "status": "cancelled",
    "message": "Optimization cancelled successfully"
  }
}
```

---

## Error Handling

### Error Response Format

All errors follow this format:

```json
{
  "success": false,
  "error": {
    "code": "ERROR_CODE",
    "message": "Human-readable error message",
    "details": "Optional detailed information"
  }
}
```

### Error Codes

| Code | HTTP Status | Description |
|------|-------------|-------------|
| `INVALID_REQUEST` | 400 | Invalid request body or parameters |
| `INVALID_DATE` | 400 | Invalid date format |
| `NOT_FOUND` | 404 | Resource not found |
| `METHOD_NOT_ALLOWED` | 405 | HTTP method not allowed |
| `CREATE_FAILED` | 400 | Failed to create resource |
| `CANCEL_FAILED` | 400 | Failed to cancel operation |
| `NOT_COMPLETED` | 400 | Operation not completed yet |
| `INTERNAL_ERROR` | 500 | Internal server error |

---

## Rate Limiting

Currently, there is no rate limiting implemented. In production, rate limiting will be handled by the API Gateway.

Recommended limits:
- **Backtest Creation**: 10 requests/minute
- **Optimization Creation**: 5 requests/minute
- **Results Queries**: 100 requests/minute

---

## Metrics

### GET /metrics

Prometheus metrics endpoint.

**Available Metrics**:

- `backtests_created_total` - Counter of backtests created
- `backtests_completed_total{status}` - Counter by status
- `backtest_duration_seconds` - Histogram of durations
- `active_backtests` - Gauge of running backtests
- `events_processed_total{type}` - Counter by event type
- `service_uptime_seconds` - Gauge of uptime

**Example Query**:
```
rate(backtests_created_total[5m])
```

---

**Last Updated**: October 28, 2025  
**API Version**: 1.0.0


