# Confirmation: Redis stores trades from the Bitso WebSocket

**Yes.** The Redis database is used to store trades that originate from the Bitso WebSocket. The flow is implemented end-to-end in code.

---

## Data flow (code path)

1. **Bitso WebSocket**  
   - `shared/pkg/bitso/websocket.go`: connection receives messages.  
   - `services/market-data/internal/websocket/manager.go`: `messageLoop()` reads from `wsConn.Receive()`, then `routeMessage(msg)` sends `bitso.WebSocketTrade` into `m.tradesStream`.

2. **Trade processor**  
   - `cmd/main.go`: processor input is `wsManager.GetTradesStream()` (same as `m.tradesStream`).  
   - `internal/processor/trade_processor.go`: `ProcessTrade(wsTrade)` converts via `models.FromBitsoWebSocketTrade(wsTrade)` to `*models.TradeEvent` and sends it to `tradesOutput`.  
   - `shared/pkg/models/events.go`: `FromBitsoWebSocketTrade` maps Bitso payload (TID, Book, Price, Amount, Value, Side, CreationTimestamp) into `TradeEvent`.

3. **Redis trade writer**  
   - `cmd/main.go`: writer input is `tradeProcessor.GetProcessedTradesStream()` (processor output).  
   - `internal/writer/redis_trade_writer.go`: `writeLoop()` reads from that channel; for each trade it calls `writeAndForward(ctx, trade)`.

4. **Writes to Redis**  
   - In `writeAndForward()`:
     - **Cache:** `w.cache.SetTrade(writeCtx, trade.Book, trade)`  
       - `internal/cache/redis.go` `SetTrade()`: `client.Set(ctx, "trade:{book}:{id}", json.Marshal(trade), TTL)` (Redis **DB 0**) and `addToRecentTrades()` → `LPUSH recent_trades:{book} {id}`.
     - **Storage:** `w.storage.StoreTrade(writeCtx, trade)`  
       - `internal/historical/storage.go` `StoreTrade()`: `client.Set(ctx, "trade:{book}:{timestamp_unix}:{id}", json.Marshal(trade), ttl)` (Redis **DB 1**). Optionally `addToTimeIndex()` for range queries.

So every trade that comes from the Bitso WebSocket and is successfully processed is written to Redis in two places: cache (DB 0) and historical storage (DB 1).

---

## Startup order (so trades reach Redis)

In `cmd/main.go` `Start()`:

1. WebSocket manager: `Connect()` → `Subscribe(books, channels)` → `Start()` (starts `messageLoop`).  
2. Trade processor: `Start()` (consumes `GetTradesStream()`, produces processed trades).  
3. Redis trade writer: `Start()` (consumes processor output, calls `SetTrade` and `StoreTrade`).

So when market-data is running and the WebSocket is connected and subscribed (e.g. to `trades` for `btc_mxn`), incoming Bitso trade messages are converted and then stored in Redis by this writer.

---

## Summary

| Question | Answer |
|----------|--------|
| Does Redis store trades from the Bitso WebSocket? | **Yes.** |
| Where in code? | WebSocket manager → trade processor → `internal/writer/redis_trade_writer.go` → `cache.SetTrade()` (DB 0) and `storage.StoreTrade()` (DB 1). |
| When does it happen? | For every trade message received on the Bitso WebSocket and successfully processed, as long as market-data is running and the writer’s Redis calls succeed. |

If you see no trades in Redis, the cause is not “trades aren’t written by code” but rather one of: market-data not running, WebSocket not connected/subscribed, different Redis instance than the one you’re checking, or Redis errors (check market-data logs for `SetTrade`/`StoreTrade` errors).
