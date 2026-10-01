// Package dailyexec executes one leg (a buy or a sell of a fixed BTC amount)
// for the daily executor, the way the execution check measured it:
//
//  1. rest a post-only limit order at the best bid (buy) or ask (sell);
//  2. if it has not filled after MakerTimeout, cancel it and send a market
//     order for whatever is left.
//
// It is idempotent and resumable. Every order of a leg carries one of two
// client ids derived from (book, fill date, side): one for maker orders, one
// for the market fallback. Before acting, the leg asks Bitso for all trades
// ever made under those ids and only trades the remainder. A re-run after a
// crash therefore resumes instead of buying twice, and once a market fallback
// has traded, no second one is ever sent.
package dailyexec

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"bitso-trading-platform/strategy-executor/internal/bitsostage"
)

// Exchange is the subset of the Bitso API a leg needs.
type Exchange interface {
	Ticker(book string) (bitsostage.Quote, error)
	Balances() (map[string]float64, error)
	PlaceOrder(o bitsostage.OrderRequest) (string, error)
	OpenOrders(book string) ([]bitsostage.OpenOrder, error)
	CancelOrder(oid string) error
	TradesByOrigin(originID string) ([]bitsostage.Trade, error)
}

// Clock lets tests run a 60-minute leg instantly.
type Clock interface {
	Now() time.Time
	Sleep(time.Duration)
}

// RealClock is the wall clock.
type RealClock struct{}

func (RealClock) Now() time.Time        { return time.Now().UTC() }
func (RealClock) Sleep(d time.Duration) { time.Sleep(d) }

// Config tunes a leg.
type Config struct {
	MakerTimeout       time.Duration // rest this long before falling back to market (60m, as measured)
	Poll               time.Duration // how often to check fills
	MaxMakerPlacements int           // post-only placements (rejections/reprices) before falling back
	MinQuoteValue      float64       // below this remaining value, don't send a fallback (Bitso minimum)
	FundsBuffer        float64       // extra quote balance required on buys, e.g. 0.02 = 2%
}

// DefaultConfig matches the approved plan.
func DefaultConfig() Config {
	return Config{MakerTimeout: 60 * time.Minute, Poll: 30 * time.Second, MaxMakerPlacements: 5, MinQuoteValue: 10, FundsBuffer: 0.02}
}

// Leg is one buy or sell.
type Leg struct {
	Book     string  // e.g. btc_usd
	Side     string  // "buy" | "sell"
	Qty      float64 // BTC
	FillDate string  // YYYY-MM-DD, the day whose open the paper account fills at
}

// Result is what happened, suitable for the ledger.
type Result struct {
	Side        string             `json:"side"`
	Target      float64            `json:"target_btc"`
	Filled      float64            `json:"filled_btc"`
	MakerFilled float64            `json:"maker_btc"`
	TakerFilled float64            `json:"taker_btc"`
	AvgPrice    float64            `json:"avg_price"`
	Notional    float64            `json:"notional"` // quote currency, before fees
	Fees        map[string]float64 `json:"fees"`
	MakerOrigin string             `json:"maker_origin_id"`
	TakerOrigin string             `json:"taker_origin_id"`
	Oids        []string           `json:"oids"`
	Placements  int                `json:"maker_placements"`
	Fallback    bool               `json:"market_fallback"`
	Shortfall   float64            `json:"shortfall_btc"`
	Notes       []string           `json:"notes,omitempty"`
	Started     time.Time          `json:"started"`
	Finished    time.Time          `json:"finished"`
}

const dust = 1e-8

// OriginIDs returns the maker and market client ids for a leg. Bitso allows
// [A-Za-z0-9_-], at most 40 characters.
func OriginIDs(book, fillDate, side string) (maker, taker string) {
	base := fmt.Sprintf("sma50-%s-%s-%c", book, strings.ReplaceAll(fillDate, "-", ""), side[0])
	return base + "-m", base + "-t"
}

// Run executes the leg to completion (filled, fallen back, or failed).
// logf receives human-readable progress lines.
func Run(ex Exchange, clk Clock, cfg Config, leg Leg, logf func(string, ...any)) (Result, error) {
	if leg.Side != "buy" && leg.Side != "sell" {
		return Result{}, fmt.Errorf("side %q", leg.Side)
	}
	if leg.Qty <= dust {
		return Result{}, fmt.Errorf("qty %v", leg.Qty)
	}
	parts := strings.Split(leg.Book, "_")
	if len(parts) != 2 {
		return Result{}, fmt.Errorf("book %q", leg.Book)
	}
	base, quote := parts[0], parts[1]
	mk, tk := OriginIDs(leg.Book, leg.FillDate, leg.Side)
	res := Result{Side: leg.Side, Target: leg.Qty, MakerOrigin: mk, TakerOrigin: tk, Fees: map[string]float64{}, Started: clk.Now()}
	deadline := res.Started.Add(cfg.MakerTimeout)
	firstLoop := true
	oids := map[string]bool{}

	for {
		// Order matters: read open orders BEFORE trades. If an order fills
		// between the two calls, the trades then include it. The reverse
		// order would see neither the open order nor its fills and place a
		// second order.
		active, err := activeOrder(ex, leg.Book, mk)
		if err != nil {
			return res, err
		}
		mFills, err := ex.TradesByOrigin(mk)
		if err != nil {
			return res, fmt.Errorf("trades %s: %w", mk, err)
		}
		tFills, err := ex.TradesByOrigin(tk)
		if err != nil {
			return res, fmt.Errorf("trades %s: %w", tk, err)
		}
		summarize(&res, mFills, tFills, oids)
		remaining := floor8(leg.Qty - res.Filled)

		if active != nil {
			oids[active.Oid] = true
			if firstLoop && !active.CreatedAt.IsZero() {
				deadline = active.CreatedAt.Add(cfg.MakerTimeout) // resuming: keep the original clock
				logf("resuming maker order %s placed %s", active.Oid, active.CreatedAt.Format(time.RFC3339))
			}
		}
		firstLoop = false

		switch {
		case remaining <= dust:
			if active != nil { // filled but still listed: nothing left to cancel in practice
				_ = ex.CancelOrder(active.Oid)
			}
			return finish(&res, clk, oids, "filled"), nil

		case len(tFills) > 0:
			// A market fallback already traded under this leg. Never send another.
			if active != nil {
				_ = ex.CancelOrder(active.Oid)
			}
			res.Fallback = true
			res.Shortfall = remaining
			res.Notes = append(res.Notes, fmt.Sprintf("market fallback already traded; %.8f BTC left unfilled", remaining))
			return finish(&res, clk, oids, "fallback done earlier"), nil

		case !clk.Now().Before(deadline) || (active == nil && res.Placements >= cfg.MaxMakerPlacements):
			if active != nil {
				logf("maker timeout: cancelling %s", active.Oid)
				if err := ex.CancelOrder(active.Oid); err != nil {
					return res, fmt.Errorf("cancel %s: %w", active.Oid, err)
				}
				clk.Sleep(2 * time.Second)
				// Fills can land between the last check and the cancel.
				if mFills, err = ex.TradesByOrigin(mk); err != nil {
					return res, fmt.Errorf("trades %s: %w", mk, err)
				}
				summarize(&res, mFills, tFills, oids)
				if remaining = floor8(leg.Qty - res.Filled); remaining <= dust {
					return finish(&res, clk, oids, "filled at cancel"), nil
				}
			}
			return marketFallback(ex, clk, cfg, leg, quote, tk, mFills, remaining, &res, oids, logf)

		case active == nil:
			q, err := ex.Ticker(leg.Book)
			if err != nil {
				return res, fmt.Errorf("ticker: %w", err)
			}
			price := q.Bid
			if leg.Side == "sell" {
				price = q.Ask
			}
			if err := checkFunds(ex, leg.Side, base, quote, remaining, price, cfg.FundsBuffer); err != nil {
				return res, err
			}
			o := bitsostage.OrderRequest{
				Book: leg.Book, Side: leg.Side, Type: "limit", TimeInForce: "postonly",
				Major: fmt8(remaining), Price: strconv.FormatFloat(price, 'f', -1, 64), OriginID: mk,
			}
			res.Placements++
			oid, err := ex.PlaceOrder(o)
			if err != nil {
				var ae *bitsostage.APIError
				if errors.As(err, &ae) && (ae.HTTPStatus == 401 || ae.HTTPStatus == 403) {
					return res, fmt.Errorf("place maker order: %w", err)
				}
				res.Notes = append(res.Notes, fmt.Sprintf("maker placement %d rejected: %v", res.Placements, err))
				logf("maker placement %d rejected (%v); will retry", res.Placements, err)
			} else {
				oids[oid] = true
				logf("maker order %s: %s %s @ %s (post-only), deadline %s", oid, o.Side, o.Major, o.Price, deadline.Format("15:04:05Z"))
			}
		}
		clk.Sleep(cfg.Poll)
	}
}

func marketFallback(ex Exchange, clk Clock, cfg Config, leg Leg, quote, tk string, mFills []bitsostage.Trade,
	remaining float64, res *Result, oids map[string]bool, logf func(string, ...any)) (Result, error) {
	q, err := ex.Ticker(leg.Book)
	if err != nil {
		return *res, fmt.Errorf("ticker: %w", err)
	}
	ref := q.Ask
	if leg.Side == "sell" {
		ref = q.Bid
	}
	if remaining*ref < cfg.MinQuoteValue {
		res.Shortfall = remaining
		res.Notes = append(res.Notes, fmt.Sprintf("remaining %.8f BTC (%.2f %s) is below the minimum order value; not sent", remaining, remaining*ref, quote))
		return finish(res, clk, oids, "remainder below minimum"), nil
	}
	parts := strings.Split(leg.Book, "_")
	if err := checkFunds(ex, leg.Side, parts[0], quote, remaining, ref, cfg.FundsBuffer); err != nil {
		return *res, err
	}
	o := bitsostage.OrderRequest{Book: leg.Book, Side: leg.Side, Type: "market", Major: fmt8(remaining), OriginID: tk}
	oid, err := ex.PlaceOrder(o)
	if err != nil {
		return *res, fmt.Errorf("market fallback: %w", err)
	}
	oids[oid] = true
	res.Fallback = true
	logf("market fallback %s: %s %s", oid, o.Side, o.Major)
	for i := 0; i < 10; i++ {
		clk.Sleep(2 * time.Second)
		tFills, err := ex.TradesByOrigin(tk)
		if err != nil {
			return *res, fmt.Errorf("trades %s: %w", tk, err)
		}
		summarize(res, mFills, tFills, oids)
		if floor8(leg.Qty-res.Filled) <= dust {
			break
		}
	}
	res.Shortfall = math.Max(0, floor8(leg.Qty-res.Filled))
	if res.Shortfall > dust {
		res.Notes = append(res.Notes, fmt.Sprintf("market fallback left %.8f BTC unfilled", res.Shortfall))
	}
	return finish(res, clk, oids, "market fallback"), nil
}

func activeOrder(ex Exchange, book, originID string) (*bitsostage.OpenOrder, error) {
	open, err := ex.OpenOrders(book)
	if err != nil {
		return nil, fmt.Errorf("open orders: %w", err)
	}
	var found *bitsostage.OpenOrder
	for i := range open {
		if open[i].OriginID == originID {
			if found != nil {
				return nil, fmt.Errorf("two open orders share origin id %s (%s, %s): refusing to continue", originID, found.Oid, open[i].Oid)
			}
			found = &open[i]
		}
	}
	return found, nil
}

func checkFunds(ex Exchange, side, base, quote string, qty, price, buffer float64) error {
	bal, err := ex.Balances()
	if err != nil {
		return fmt.Errorf("balances: %w", err)
	}
	if side == "buy" {
		if need := qty * price * (1 + buffer); bal[quote] < need {
			return fmt.Errorf("insufficient %s: need %.2f, available %.2f", quote, need, bal[quote])
		}
		return nil
	}
	if bal[base] < qty {
		return fmt.Errorf("insufficient %s: need %.8f, available %.8f", base, qty, bal[base])
	}
	return nil
}

func summarize(res *Result, maker, taker []bitsostage.Trade, oids map[string]bool) {
	res.MakerFilled, res.TakerFilled, res.Notional = 0, 0, 0
	res.Fees = map[string]float64{}
	for _, t := range maker {
		res.MakerFilled += t.Major
		res.Notional += t.Minor
		res.Fees[t.FeeCurrency] += t.Fee
		oids[t.Oid] = true
	}
	for _, t := range taker {
		res.TakerFilled += t.Major
		res.Notional += t.Minor
		res.Fees[t.FeeCurrency] += t.Fee
		oids[t.Oid] = true
	}
	res.Filled = res.MakerFilled + res.TakerFilled
	res.AvgPrice = 0
	if res.Filled > 0 {
		res.AvgPrice = res.Notional / res.Filled
	}
}

func finish(res *Result, clk Clock, oids map[string]bool, why string) Result {
	res.Finished = clk.Now()
	res.Oids = res.Oids[:0]
	for o := range oids {
		if o != "" {
			res.Oids = append(res.Oids, o)
		}
	}
	sort.Strings(res.Oids)
	res.Notes = append(res.Notes, "done: "+why)
	return *res
}

// floor8 rounds down to Bitso's 8-decimal BTC precision, so an order never
// asks for more than is left or available.
func floor8(v float64) float64 { return math.Floor(v*1e8+1e-6) / 1e8 }

func fmt8(v float64) string { return strconv.FormatFloat(floor8(v), 'f', 8, 64) }
