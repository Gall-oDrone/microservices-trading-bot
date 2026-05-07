// Tiny in-cluster admin CLI for ad-hoc Bitso REST cleanup.
// Reads STAGE_BITSO_API_KEY / STAGE_BITSO_API_SECRET / BITSO_API_BASE_URL from env
// (already mounted on trading-engine pod).
package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"bitso-trading-platform/shared/pkg/bitso"
)

func parseBook(s string) bitso.Book {
	parts := strings.Split(s, "_")
	if len(parts) != 2 {
		fmt.Fprintln(os.Stderr, "bad book:", s)
		os.Exit(2)
	}
	return *bitso.NewBook(bitso.ToCurrency(parts[0]), bitso.ToCurrency(parts[1]))
}

func parseMonetary(s string) bitso.Monetary {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bad amount:", err)
		os.Exit(2)
	}
	return bitso.ToMonetaryWithP(v)
}

func dump(v any) {
	b, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(b))
}

func mustClient() *bitso.Client {
	key := os.Getenv("STAGE_BITSO_API_KEY")
	if key == "" {
		key = os.Getenv("BITSO_API_KEY")
	}
	secret := os.Getenv("STAGE_BITSO_API_SECRET")
	if secret == "" {
		secret = os.Getenv("BITSO_API_SECRET")
	}
	if key == "" || secret == "" {
		fmt.Fprintln(os.Stderr, "missing BITSO API key/secret env")
		os.Exit(2)
	}
	c := bitso.NewClient()
	if base := os.Getenv("BITSO_API_BASE_URL"); base != "" {
		c.SetAPIBaseURL(base)
	}
	c.SetAuth(key, secret)
	return c
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: bitso-admin <balance|open|cancel|sell-market|sell-limit> [args]")
		os.Exit(2)
	}
	c := mustClient()
	switch os.Args[1] {
	case "balance":
		bs, err := c.Balances(nil)
		if err != nil {
			fmt.Fprintln(os.Stderr, "ERR:", err)
			os.Exit(1)
		}
		dump(bs)
	case "open":
		params := url.Values{}
		if len(os.Args) > 2 {
			params.Set("book", os.Args[2])
		}
		oo, err := c.MyOpenOrders(params)
		if err != nil {
			fmt.Fprintln(os.Stderr, "ERR:", err)
			os.Exit(1)
		}
		dump(oo)
	case "cancel":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "cancel <oid>")
			os.Exit(2)
		}
		ids, err := c.CancelOrder(os.Args[2])
		if err != nil {
			fmt.Fprintln(os.Stderr, "ERR:", err)
			os.Exit(1)
		}
		dump(map[string]any{"cancelled": ids})
	case "lookup":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "lookup <oid>")
			os.Exit(2)
		}
		o, err := c.LookupOrder(os.Args[2])
		if err != nil {
			fmt.Fprintln(os.Stderr, "ERR:", err)
			os.Exit(1)
		}
		dump(o)
	case "user-trades":
		params := url.Values{}
		if len(os.Args) > 2 {
			params.Set("book", os.Args[2])
		}
		params.Set("limit", "10")
		t, err := c.MyTrades(params)
		if err != nil {
			fmt.Fprintln(os.Stderr, "ERR:", err)
			os.Exit(1)
		}
		dump(t)
	case "ticker":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "ticker <book>")
			os.Exit(2)
		}
		book := parseBook(os.Args[2])
		t, err := c.Ticker(&book)
		if err != nil {
			fmt.Fprintln(os.Stderr, "ERR:", err)
			os.Exit(1)
		}
		dump(t)
	case "sell-market":
		if len(os.Args) < 4 {
			fmt.Fprintln(os.Stderr, "sell-market <book> <amount>")
			os.Exit(2)
		}
		op := &bitso.OrderPlacement{
			Book:  parseBook(os.Args[2]),
			Side:  bitso.OrderSide(2),
			Type:  bitso.OrderTypeMarket,
			Major: parseMonetary(os.Args[3]),
		}
		oid, err := c.PlaceOrder(op)
		if err != nil {
			fmt.Fprintln(os.Stderr, "ERR:", err)
			os.Exit(1)
		}
		dump(map[string]string{"oid": oid})
	case "sell-limit":
		if len(os.Args) < 5 {
			fmt.Fprintln(os.Stderr, "sell-limit <book> <amount> <price>")
			os.Exit(2)
		}
		op := &bitso.OrderPlacement{
			Book:  parseBook(os.Args[2]),
			Side:  bitso.OrderSide(2),
			Type:  bitso.OrderTypeLimit,
			Major: parseMonetary(os.Args[3]),
			Price: parseMonetary(os.Args[4]),
		}
		oid, err := c.PlaceOrder(op)
		if err != nil {
			fmt.Fprintln(os.Stderr, "ERR:", err)
			os.Exit(1)
		}
		dump(map[string]string{"oid": oid})
	default:
		fmt.Fprintln(os.Stderr, "unknown command")
		os.Exit(2)
	}
}
