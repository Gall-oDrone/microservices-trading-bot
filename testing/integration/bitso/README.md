# Bitso API integration tests

Tests that hit the **Bitso stage API** using `shared/pkg/bitso`. They are skipped unless credentials are set.

| Test | Env required | Description |
|------|----------------|-------------|
| `TestBitsoClient_Integration_BalanceAndTicker` | `STAGE_BITSO_API_KEY`, `STAGE_BITSO_APISECRET` | Fetches balances and btc_mxn ticker |
| `TestBitsoClient_Integration_PlaceSmallOrder` | Above + `BITSO_INTEGRATION_PLACE_ORDER=1` | Places a tiny limit buy at ask |

Run from **repo root**:

```bash
go test -v ./testing/integration/bitso/... -run TestBitsoClient_Integration
```
