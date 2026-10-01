//go:build stageintegration

// Integration test against Bitso STAGE. Skipped by normal `go test`; run with:
//
//	set -a; . ~/.config/microservices-trading-bot/bitso-stage.env; set +a
//	go test -tags stageintegration -run TestStage -v ./internal/bitsostage
//
// It places one post-only BUY of 0.0001 BTC at 10% below the best bid, so it
// cannot fill, checks that the order is visible with its origin_id and a
// parseable created_at, that trades-by-origin_id answers (empty), then
// cancels it and checks it is gone.
package bitsostage

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestStageOrderLifecycle(t *testing.T) {
	c, err := New(os.Getenv("BITSO_API_BASE_URL"), os.Getenv("STAGE_BITSO_API_KEY"), os.Getenv("STAGE_BITSO_API_SECRET"))
	if err != nil {
		t.Fatal(err)
	}
	q, err := c.Ticker("btc_usd")
	if err != nil {
		t.Fatal(err)
	}
	price := math.Floor(q.Bid * 0.9) // btc_usd tick size is 1
	origin := fmt.Sprintf("itest-%d", time.Now().Unix())
	oid, err := c.PlaceOrder(OrderRequest{Book: "btc_usd", Side: "buy", Type: "limit", TimeInForce: "postonly",
		Major: "0.00010000", Price: strconv.FormatFloat(price, 'f', -1, 64), OriginID: origin})
	if err != nil {
		t.Fatalf("place: %v", err)
	}
	t.Logf("placed %s origin %s at %v (bid %v)", oid, origin, price, q.Bid)
	defer c.CancelOrder(oid) // belt and braces

	time.Sleep(2 * time.Second)
	open, err := c.OpenOrders("btc_usd")
	if err != nil {
		t.Fatal(err)
	}
	var found *OpenOrder
	for i := range open {
		if open[i].Oid == oid {
			found = &open[i]
		}
	}
	if found == nil {
		t.Fatalf("order %s not in open orders: %+v", oid, open)
	}
	t.Logf("open order: %+v", *found)
	if found.OriginID != origin {
		t.Fatalf("origin_id not returned: %q", found.OriginID)
	}
	if found.CreatedAt.IsZero() || time.Since(found.CreatedAt) > 5*time.Minute || time.Since(found.CreatedAt) < -time.Minute {
		t.Fatalf("created_at not parsed sensibly: %v", found.CreatedAt)
	}
	if found.Unfilled != 0.0001 || found.Price != price {
		t.Fatalf("amount/price: %+v", *found)
	}
	if _, err := c.PlaceOrder(OrderRequest{Book: "btc_usd", Side: "buy", Type: "limit", TimeInForce: "postonly",
		Major: "0.00010000", Price: strconv.FormatFloat(price, 'f', -1, 64), OriginID: origin}); err == nil {
		t.Errorf("Bitso accepted a second active order with the same origin_id")
	} else {
		t.Logf("duplicate origin_id rejected as documented: %v", err)
	}

	trades, err := c.TradesByOrigin(origin)
	if err != nil {
		t.Fatalf("trades by origin: %v", err)
	}
	if len(trades) != 0 {
		t.Fatalf("unfillable order has trades: %+v", trades)
	}

	if err := c.CancelOrder(oid); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	time.Sleep(2 * time.Second)
	open, err = c.OpenOrders("btc_usd")
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range open {
		if o.Oid == oid || o.OriginID == origin {
			t.Fatalf("order still open after cancel: %+v", o)
		}
	}

	// A post-only order priced through the market must not execute as taker.
	cross := strconv.FormatFloat(math.Ceil(q.Ask*1.01), 'f', -1, 64)
	xo := origin + "-x"
	xoid, err := c.PlaceOrder(OrderRequest{Book: "btc_usd", Side: "buy", Type: "limit", TimeInForce: "postonly",
		Major: "0.00010000", Price: cross, OriginID: xo})
	t.Logf("crossing post-only order: oid=%q err=%v", xoid, err)
	time.Sleep(2 * time.Second)
	if xoid != "" {
		defer c.CancelOrder(xoid)
	}
	xt, err := c.TradesByOrigin(xo)
	if err != nil {
		t.Fatal(err)
	}
	if len(xt) != 0 {
		t.Fatalf("post-only order crossed and traded: %+v", xt)
	}
}
