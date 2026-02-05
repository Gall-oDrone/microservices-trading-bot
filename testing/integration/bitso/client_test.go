// Integration tests for Bitso API (balance, ticker, optional order placement).
// Skipped unless STAGE_BITSO_API_KEY and STAGE_BITSO_APISECRET are set.
//
// Run from repo root:
//
//	STAGE_BITSO_API_KEY=xxx STAGE_BITSO_APISECRET=yyy go test -v ./testing/integration/bitso/... -run TestBitsoClient_Integration
package bitso_test

import (
	"net/url"
	"os"
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/bitso"
)

func TestBitsoClient_Integration_BalanceAndTicker(t *testing.T) {
	key := os.Getenv("STAGE_BITSO_API_KEY")
	secret := os.Getenv("STAGE_BITSO_APISECRET")
	if key == "" || secret == "" {
		t.Skip("STAGE_BITSO_API_KEY and STAGE_BITSO_APISECRET required for integration test")
	}

	client := bitso.NewClient()
	client.SetAuth(key, secret)
	client.SetAPIBaseURL("https://stage.bitso.com/api")
	client.SetBurstRate(200 * time.Millisecond)

	// Balances
	balances, err := client.Balances(url.Values{})
	if err != nil {
		t.Fatalf("Balances: %v", err)
	}
	if len(balances) == 0 {
		t.Log("Balances: no balances returned (empty account?)")
	}
	for _, b := range balances {
		t.Logf("Balance %s: total=%s available=%s", b.Currency, b.Total, b.Available)
	}

	// Ticker btc_mxn
	book := bitso.NewBook(bitso.BTC, bitso.MXN)
	ticker, err := client.Ticker(book)
	if err != nil {
		t.Fatalf("Ticker: %v", err)
	}
	if ticker.Bid.Float64() <= 0 || ticker.Ask.Float64() <= 0 {
		t.Errorf("Ticker bid/ask should be positive: bid=%s ask=%s", ticker.Bid, ticker.Ask)
	}
	t.Logf("btc_mxn bid=%s ask=%s", ticker.Bid, ticker.Ask)
}

func TestBitsoClient_Integration_PlaceSmallOrder(t *testing.T) {
	key := os.Getenv("STAGE_BITSO_API_KEY")
	secret := os.Getenv("STAGE_BITSO_APISECRET")
	if key == "" || secret == "" {
		t.Skip("STAGE_BITSO_API_KEY and STAGE_BITSO_APISECRET required for integration test")
	}
	if os.Getenv("BITSO_INTEGRATION_PLACE_ORDER") != "1" {
		t.Skip("Set BITSO_INTEGRATION_PLACE_ORDER=1 to run order placement test")
	}

	client := bitso.NewClient()
	client.SetAuth(key, secret)
	client.SetAPIBaseURL("https://stage.bitso.com/api")
	client.SetBurstRate(200 * time.Millisecond)

	book := bitso.NewBook(bitso.BTC, bitso.MXN)
	ticker, err := client.Ticker(book)
	if err != nil {
		t.Fatalf("Ticker: %v", err)
	}

	// Place a tiny limit buy at ask (executes like market)
	op := &bitso.OrderPlacement{
		Book:  *book,
		Side:  bitso.OrderSideBuy,
		Type:  bitso.OrderTypeLimit,
		Major: bitso.ToMonetary(0.0001),
		Price: ticker.Ask,
	}
	oid, err := client.PlaceOrder(op)
	if err != nil {
		t.Fatalf("PlaceOrder: %v", err)
	}
	t.Logf("Order placed oid=%s", oid)
	if oid == "" {
		t.Error("PlaceOrder returned empty oid")
	}
}
