# Bitso API smoke test

Simple check that your Bitso API keys work: **current account balance** and **btc_mxn** ticker, with an optional **small limit order at market price**.

- Uses **stage** by default (`https://stage.bitso.com/api`). Use [Bitso testing environment](https://docs.bitso.com/bitso-api/docs/set-up-your-testing-environment) keys.
- All logic uses `shared/pkg/bitso` (same client as the trading engine).

## Run (balance + ticker only)

```bash
export STAGE_BITSO_API_KEY=your_stage_key
export STAGE_BITSO_APISECRET=your_stage_secret
go run ./cmd/bitso-smoke-test
```

## Run and place a small order

Places a **limit** order at the current best ask (buy) or bid (sell), amount 0.0001 BTC, so it fills immediately like a market order:

```bash
go run ./cmd/bitso-smoke-test -order=buy   # buy 0.0001 BTC at ask
go run ./cmd/bitso-smoke-test -order=sell # sell 0.0001 BTC at bid
```

## Integration tests (testing/integration/bitso)

Integration tests live in the repo’s **testing** folder and run from the repo root:

```bash
# Skip unless credentials set
go test -v ./testing/integration/bitso/... -run TestBitsoClient_Integration

# With credentials: balance + ticker
STAGE_BITSO_API_KEY=xxx STAGE_BITSO_APISECRET=yyy go test -v ./testing/integration/bitso/... -run TestBitsoClient_Integration_BalanceAndTicker

# With credentials + place order (optional)
BITSO_INTEGRATION_PLACE_ORDER=1 STAGE_BITSO_API_KEY=xxx STAGE_BITSO_APISECRET=yyy go test -v ./testing/integration/bitso/... -run TestBitsoClient_Integration_PlaceSmallOrder
```
