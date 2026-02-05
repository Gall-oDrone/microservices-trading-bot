# Order-Management Integration Tests

Component integration tests that wire the full order-management stack (manager, repos, validator, risk, intraday aggregator) and verify behavior across components.

## What they test

- **ProcessSignal → Filled flow:** Order created from signal, status transitions to filled, intraday P&L aggregator receives the trade (daily realized P&L and trade counts).
- **Realized P&L metadata:** When `UpdateOrderStatus(..., Filled, metadata)` is called with `metadata["realized_pnl"]`, the aggregator records it and updates win/loss counts.
- **GetPositionSummary:** Returns without error after manager start (and optionally after a full flow).

## Run

From the **order-management** service directory:

```bash
cd services/order-management
go test ./integration/... -v
```

From repo root (if you are in a monorepo that has this service):

```bash
go test ./services/order-management/integration/... -v
```

## Why here and not under `testing/integration/order-management/`?

These tests import `internal/` packages (manager, metrics, repository, etc.). Go allows `internal` to be imported only by code that lives in the same module. So the integration tests live inside the **order-management** module (`services/order-management/integration/`). The path `testing/integration/order-management/` at repo root contains a README that points here.
