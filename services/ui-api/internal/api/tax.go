package api

import (
	"math"
	"sort"
	"strconv"

	"bitso-trading-platform/shared/pkg/dailyrule"
)

// Plan §6.4.14: tax lots. The stage fills as first-in, first-out lots: each
// sale is matched against the oldest open buys, giving realized gains per
// sale and per calendar year, plus the open lots at the last close. In the
// book's quote currency, and in MXN (for a USD book at the USD/MXN implied
// by the two Bitso series on the fill day). Reporting only; not tax advice.

// TaxLot is an open (or partly sold) buy.
type TaxLot struct {
	BuyDate      string  `json:"buy_date"`
	QtyBTC       float64 `json:"qty_btc"`       // received, net of a fee paid in BTC
	RemainingBTC float64 `json:"remaining_btc"` // not yet sold
	CostPerBTC   float64 `json:"cost_per_btc"`  // quote paid / BTC received (fees included)
	Unrealized   float64 `json:"unrealized"`    // remaining × (mark − cost)
	HoldingDays  int     `json:"holding_days"`  // to the mark date
}

// TaxSale is one sale matched against one lot.
type TaxSale struct {
	SellDate    string   `json:"sell_date"`
	BuyDate     string   `json:"buy_date"`
	QtyBTC      float64  `json:"qty_btc"`
	Proceeds    float64  `json:"proceeds"` // net of the sale's fee, pro rata
	Cost        float64  `json:"cost"`
	Gain        float64  `json:"gain"`
	HoldingDays int      `json:"holding_days"`
	GainMXN     *float64 `json:"gain_mxn"` // null when no rate is known for a USD book
}

// TaxYear sums a calendar year's sales (by sale date).
type TaxYear struct {
	Year     int      `json:"year"`
	Sales    int      `json:"sales"`
	Proceeds float64  `json:"proceeds"`
	Cost     float64  `json:"cost"`
	Gain     float64  `json:"gain"`
	GainMXN  *float64 `json:"gain_mxn"` // null if any sale lacks a rate
}

// TaxLots is performance.tax_lots.
type TaxLots struct {
	Method       string    `json:"method"` // FIFO
	Quote        string    `json:"quote"`
	MarkDate     string    `json:"mark_date"`
	Mark         float64   `json:"mark"`
	Open         []TaxLot  `json:"open"`
	Sales        []TaxSale `json:"sales"`
	Years        []TaxYear `json:"years"`
	Realized     float64   `json:"realized"`
	Unrealized   float64   `json:"unrealized"`
	UnmatchedBTC float64   `json:"unmatched_btc"` // sold with no open lot (should be 0)
	Note         string    `json:"note"`
}

const taxNote = "FIFO on the stage fills; buy cost includes the fee, sale proceeds are net of it. Reporting only, not tax advice: " +
	"Mexican ISR measures a sale of crypto assets in MXN with the cost adjusted for inflation (INPC) and USD at the Banxico FIX rate; " +
	"these figures use the Bitso-implied USD/MXN and no inflation adjustment."

// buildTaxLots matches fills (in order) FIFO. fxMXN returns the MXN per
// quote unit on a date (1 for an MXN book); ok false when unknown.
func buildTaxLots(quote string, fills []Fill, mark float64, markDate string, fxMXN func(date string) (float64, bool)) *TaxLots {
	if len(fills) == 0 {
		return nil
	}
	t := &TaxLots{Method: "FIFO", Quote: quote, Mark: mark, MarkDate: markDate, Open: []TaxLot{}, Sales: []TaxSale{}, Years: []TaxYear{}, Note: taxNote}
	var lots []TaxLot
	for _, f := range fills {
		q := math.Abs(f.NetBTC)
		if q <= 0 {
			continue
		}
		if f.Side == "buy" {
			lots = append(lots, TaxLot{BuyDate: f.FillDate, QtyBTC: q, RemainingBTC: q, CostPerBTC: f.Notional / q})
			continue
		}
		perBTC := (f.Notional - f.FeeQuote) / q
		left := q
		for i := range lots {
			if left <= btcDust {
				break
			}
			if lots[i].RemainingBTC <= btcDust {
				continue
			}
			take := math.Min(left, lots[i].RemainingBTC)
			s := TaxSale{SellDate: f.FillDate, BuyDate: lots[i].BuyDate, QtyBTC: take, Proceeds: take * perBTC,
				Cost: take * lots[i].CostPerBTC, HoldingDays: daysBetween(lots[i].BuyDate, f.FillDate)}
			s.Gain = s.Proceeds - s.Cost
			// In MXN: proceeds at the sale day's rate, cost at the buy day's.
			if sx, ok1 := fxMXN(s.SellDate); ok1 {
				if bx, ok2 := fxMXN(s.BuyDate); ok2 {
					g := s.Proceeds*sx - s.Cost*bx
					s.GainMXN = &g
				}
			}
			t.Sales = append(t.Sales, s)
			t.Realized += s.Gain
			lots[i].RemainingBTC -= take
			left -= take
		}
		if left > btcDust {
			t.UnmatchedBTC += left
		}
	}
	for _, l := range lots {
		if l.RemainingBTC <= btcDust {
			continue
		}
		l.HoldingDays = daysBetween(l.BuyDate, markDate)
		if mark > 0 {
			l.Unrealized = l.RemainingBTC * (mark - l.CostPerBTC)
		}
		t.Unrealized += l.Unrealized
		t.Open = append(t.Open, l)
	}
	years := map[int]*TaxYear{}
	missing := map[int]bool{}
	for _, s := range t.Sales {
		y, _ := strconv.Atoi(s.SellDate[:4])
		ty := years[y]
		if ty == nil {
			ty = &TaxYear{Year: y}
			years[y] = ty
		}
		ty.Sales++
		ty.Proceeds += s.Proceeds
		ty.Cost += s.Cost
		ty.Gain += s.Gain
		if s.GainMXN == nil {
			missing[y] = true
		} else {
			g := *s.GainMXN
			if ty.GainMXN != nil {
				g += *ty.GainMXN
			}
			ty.GainMXN = &g
		}
	}
	for y, ty := range years {
		if missing[y] {
			ty.GainMXN = nil
		}
		t.Years = append(t.Years, *ty)
	}
	sort.Slice(t.Years, func(i, j int) bool { return t.Years[i].Year < t.Years[j].Year })
	return t
}

// impliedFX returns MXN per USD at or before a date from the two Bitso
// series (btc_mxn close / btc_usd close on the same Mexico City day).
func impliedFX(usdBars, mxnBars []dailyrule.Bar) func(string) (float64, bool) {
	mx := map[string]float64{}
	for _, b := range mxnBars {
		mx[b.Date.Format("2006-01-02")] = b.Close
	}
	var days []string
	fx := map[string]float64{}
	for _, b := range usdBars {
		d := b.Date.Format("2006-01-02")
		if c, ok := mx[d]; ok && c > 0 && b.Close > 0 {
			fx[d] = c / b.Close
			days = append(days, d)
		}
	}
	sort.Strings(days)
	return func(d string) (float64, bool) {
		i := sort.SearchStrings(days, d)
		if i < len(days) && days[i] == d {
			return fx[d], true
		}
		if i == 0 {
			return 0, false
		}
		return fx[days[i-1]], true
	}
}
