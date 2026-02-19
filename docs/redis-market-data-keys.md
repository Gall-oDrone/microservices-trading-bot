# Redis keys used by market-data (Bitso WebSocket)

The **market-data** service writes every trade (and optionally order book / ticker) from the Bitso WebSocket API into Redis. It uses two Redis databases on the same server.

## Redis databases

| DB  | Purpose           | Config (market-data)      |
|-----|-------------------|---------------------------|
| **0** | Cache (hot data)   | `REDIS_DB` (default 0)     |
| **1** | Historical storage| `REDIS_DB + 1` (default 1)|

---

## DB 0 – Cache

Used for latest state and quick lookups.

| Key pattern | Type | Description |
|-------------|------|-------------|
| `trade:{book}:{tradeID}` | String (JSON) | One trade. Example: `trade:btc_mxn:12345678` |
| `recent_trades:{book}` | List | Trade IDs, newest first (e.g. `recent_trades:btc_mxn`). Capped per book; TTL set. |
| `orderbook:{book}` | String (JSON) | Current order book snapshot (if diff-orders are processed). |
| `ticker:{book}` | String (JSON) | Current ticker (if ticker channel is used). |
| `trade_stats:{book}` | String (JSON) | Aggregated trade stats for the book. |

**Trade JSON (from Bitso WebSocket)** – stored at `trade:{book}:{tradeID}` and in historical DB:

```json
{
  "id": 12345678,
  "book": "btc_mxn",
  "price": 1234567.89,
  "amount": 0.001,
  "value": 1234.56,
  "side": "buy",
  "timestamp": "2025-02-19T12:00:00Z",
  "maker_side": "buy",
  "received_at": "2025-02-19T12:00:00.123Z",
  "created_at_millis": 1739966400123
}
```

- `id` = Bitso trade ID (`TID` from WebSocket payload).
- `timestamp` = exchange time (from `creation_timestamp`).
- `received_at` = when the market-data service received the message.

---

## DB 1 – Historical storage

Used for time-range queries (e.g. backtesting).

| Key pattern | Type | Description |
|-------------|------|-------------|
| `trade:{book}:{unix_ts}:{tradeID}` | String (JSON) | Same trade JSON as cache; timestamp in key for range scans. Example: `trade:btc_mxn:1739966400:12345678` |
| `time_index:trade:{book}:{unix_ts}` | String | Maps to trade ID for time-based lookup (if indexing enabled). |

---

## How to inspect

1. **Use the script** (requires `redis-cli` and Redis running):

   ```bash
   export REDIS_HOST=localhost REDIS_PORT=6379   # optional
   ./scripts/redis-market-data-inspect.sh
   ```

2. **Manual examples** (redis-cli):

   ```bash
   # List cache keys (DB 0)
   redis-cli -n 0 KEYS '*'

   # Recent trade IDs for btc_mxn (newest first)
   redis-cli -n 0 LRANGE recent_trades:btc_mxn 0 19

   # Get one trade (replace 12345678 with an ID from recent_trades)
   redis-cli -n 0 GET trade:btc_mxn:12345678

   # List historical trade keys (DB 1)
   redis-cli -n 1 KEYS 'trade:*'
   redis-cli -n 1 GET 'trade:btc_mxn:1739966400:12345678'
   ```

If no keys appear, ensure the **market-data** service is running and connected to the Bitso WebSocket; it writes to Redis on every trade received.
