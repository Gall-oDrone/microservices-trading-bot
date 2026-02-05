// bitso-smoke-test verifies Bitso API: current account balance and optional small order at market price.
//
// Uses shared/pkg/bitso (stage.bitso.com by default). Requires stage API keys.
//
//   # Balance + btc_mxn ticker only
//   STAGE_BITSO_API_KEY=xxx STAGE_BITSO_APISECRET=yyy go run ./cmd/bitso-smoke-test
//
//   # Balance + place small limit buy at current ask (~0.0001 BTC)
//   STAGE_BITSO_API_KEY=xxx STAGE_BITSO_APISECRET=yyy go run ./cmd/bitso-smoke-test -order=buy
//
//   # Balance + place small limit sell at current bid
//   STAGE_BITSO_API_KEY=xxx STAGE_BITSO_APISECRET=yyy go run ./cmd/bitso-smoke-test -order=sell
//
// Production: BITSO_USE_STAGE=0 BITSO_API_KEY=... BITSO_APISECRET=... go run ./cmd/bitso-smoke-test
package main

import (
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"time"

	"bitso-trading-platform/shared/pkg/bitso"
)

func main() {
	useStage := os.Getenv("BITSO_USE_STAGE") != "0" // default true for safety
	orderSide := flag.String("order", "", "place a small order: 'buy' or 'sell' (optional)")
	flag.Parse()

	var key, secret, baseURL string
	if useStage {
		key = os.Getenv("STAGE_BITSO_API_KEY")
		secret = os.Getenv("STAGE_BITSO_APISECRET")
		baseURL = "https://stage.bitso.com/api"
	} else {
		key = os.Getenv("BITSO_API_KEY")
		secret = os.Getenv("BITSO_APISECRET")
		baseURL = "https://bitso.com/api"
	}
	if key == "" || secret == "" {
		log.Fatal("Set STAGE_BITSO_API_KEY and STAGE_BITSO_APISECRET (stage) or BITSO_API_KEY and BITSO_APISECRET (prod)")
	}

	client := bitso.NewClient()
	client.SetAuth(key, secret)
	client.SetAPIBaseURL(baseURL)
	client.SetBurstRate(200 * time.Millisecond)

	// 1) Balances
	fmt.Println("--- Balances ---")
	balances, err := client.Balances(url.Values{})
	if err != nil {
		log.Fatalf("Balances: %v", err)
	}
	for _, b := range balances {
		fmt.Printf("  %s: total=%s available=%s locked=%s\n",
			b.Currency, b.Total, b.Available, b.Locked)
	}

	// 2) Ticker for btc_mxn (market price)
	book := bitso.NewBook(bitso.BTC, bitso.MXN)
	ticker, err := client.Ticker(book)
	if err != nil {
		log.Fatalf("Ticker: %v", err)
	}
	fmt.Println("\n--- btc_mxn ticker ---")
	fmt.Printf("  bid=%s ask=%s last=%s\n", ticker.Bid, ticker.Ask, ticker.Last)

	// 3) Optional: place a small order at market (limit at best ask/bid so it fills immediately)
	if *orderSide != "" {
		var side bitso.OrderSide
		var price, amount float64
		switch *orderSide {
		case "buy":
			side = bitso.OrderSideBuy
			price = ticker.Ask.Float64()
			// Tiny amount: ~0.0001 BTC or min; use fixed small amount in BTC
			amount = 0.0001
		case "sell":
			side = bitso.OrderSideSell
			price = ticker.Bid.Float64()
			amount = 0.0001
		default:
			log.Fatalf("order must be 'buy' or 'sell', got %q", *orderSide)
		}
		op := &bitso.OrderPlacement{
			Book:  *book,
			Side:  side,
			Type:  bitso.OrderTypeLimit,
			Major: bitso.ToMonetary(amount),
			Price: bitso.ToMonetary(price),
		}
		oid, err := client.PlaceOrder(op)
		if err != nil {
			log.Fatalf("PlaceOrder: %v", err)
		}
		fmt.Printf("\n--- Order placed ---\n  oid=%s side=%s amount=%.8f price=%.2f\n", oid, *orderSide, amount, price)
	} else {
		fmt.Println("\n(No order placed; use -order=buy or -order=sell to place a small limit order at market price)")
	}
}
