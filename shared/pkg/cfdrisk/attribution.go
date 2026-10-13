package cfdrisk

import (
	"sort"
	"time"

	"bitso-trading-platform/shared/pkg/etoroledger"
)

// P&L attribution of the demo account from the ledger (M13): what the
// positions made or lost on price, what the spread, markup and fees took,
// what overnight financing costs under the frozen model (and what eToro
// has actually charged, when a reconcile report says), per lot, per book
// and in total, split realized (closed lots) / unrealized (open lots marked
// at the latest close). Then the paper-vs-demo tracking: the pre-registered
// paper account's equity (the ledger's paper block, cfdsim at the frozen
// costs, fills at the next open) against the demo account's equity from
// the same ledger's fills, in basis points.
//
// Financing is modelled as the pre-registration states it: the frozen rate
// per calendar night on the position's open notional, a weekend being three
// nights (the calendar-day difference between the open's fill date and the
// close's, or the as-of date for an open lot). eToro's own charge appears in
// its position's totalFees, which cmd/etoro-reconcile records as
// financing_to_date; it is an optional input because the report must also
// run without a reconcile report for the day.

// Mark is the latest close a book's open lot is valued at.
type Mark struct {
	Date  time.Time
	Close float64
}

// AttributionInput is everything the attribution reads.
type AttributionInput struct {
	Records []etoroledger.Record
	Marks   map[string]Mark // per book
	// AsOf is the New York date the open lots' nights are counted to.
	AsOf time.Time
	// OvernightBps is the frozen financing per calendar night, per book,
	// in basis points of the open notional.
	OvernightBps map[string]float64
	// FinancingCharged is eToro's financing to date per book (from the
	// newest reconcile report), when known.
	FinancingCharged map[string]float64
}

// Breakdown is one P&L decomposition, USD, gains positive. Costs are
// positive numbers that reduce Net.
type Breakdown struct {
	PricePnL          float64 `json:"price_pnl"`
	SpreadPaid        float64 `json:"spread_paid"`
	Markup            float64 `json:"markup"`
	Fees              float64 `json:"fees"`
	FinancingModelled float64 `json:"financing_modelled"`
	Net               float64 `json:"net"`
}

func (b *Breakdown) add(o Breakdown) {
	b.PricePnL += o.PricePnL
	b.SpreadPaid += o.SpreadPaid
	b.Markup += o.Markup
	b.Fees += o.Fees
	b.FinancingModelled += o.FinancingModelled
	b.Net += o.Net
}

// Lot is one demo position: an executed open and, when closed, its
// executed close.
type Lot struct {
	Book       string  `json:"book"`
	PositionID string  `json:"position_id"`
	OpenDate   string  `json:"open_date"` // fill date (New York)
	OpenPrice  float64 `json:"open_price"`
	Units      float64 `json:"units"`
	Amount     float64 `json:"amount"` // open notional, USD
	Open       bool    `json:"open"`
	CloseDate  string  `json:"close_date,omitempty"`
	ClosePrice float64 `json:"close_price,omitempty"` // the close fill, or the mark for an open lot
	Nights     int     `json:"nights"`                // calendar nights financed
	Breakdown
}

// BookAttribution is one book's lots and totals.
type BookAttribution struct {
	Book             string    `json:"book"`
	Lots             []Lot     `json:"lots"`
	Realized         Breakdown `json:"realized"`
	Unrealized       Breakdown `json:"unrealized"`
	Total            Breakdown `json:"total"`
	MarkDate         string    `json:"mark_date,omitempty"`
	MarkClose        float64   `json:"mark_close,omitempty"`
	FinancingCharged *float64  `json:"financing_charged,omitempty"` // eToro's figure, when known
	NetWithCharged   *float64  `json:"net_with_charged,omitempty"`  // Total.Net with the charged financing instead of the modelled
	// Tracking: the paper account (ledger paper block, latest record) against
	// the demo lots chained from 1.0.
	PaperEquity  float64 `json:"paper_equity"`
	PaperStarted bool    `json:"paper_started"`
	DemoEquity   float64 `json:"demo_equity"`
	TrackingBps  float64 `json:"tracking_bps"` // (demo - paper) x 1e4
}

// Attribution is the whole account.
type Attribution struct {
	AsOf       string            `json:"as_of"`
	Books      []BookAttribution `json:"books"`
	Realized   Breakdown         `json:"realized"`
	Unrealized Breakdown         `json:"unrealized"`
	Total      Breakdown         `json:"total"`
}

func nyDate(t time.Time) string { return t.Format("2006-01-02") }

// Attribute builds the attribution from demo records.
func Attribute(in AttributionInput) Attribution {
	out := Attribution{AsOf: nyDate(in.AsOf)}
	byBook := map[string][]etoroledger.Record{}
	for _, r := range in.Records {
		if r.Mode != etoroledger.ModeDemo || r.Demo == nil {
			continue
		}
		byBook[r.Book] = append(byBook[r.Book], r)
	}
	books := make([]string, 0, len(byBook))
	for b := range byBook {
		books = append(books, b)
	}
	sort.Strings(books)
	for _, book := range books {
		recs := byBook[book]
		sort.SliceStable(recs, func(i, j int) bool { return recs[i].Decision.BarDate < recs[j].Decision.BarDate })
		ba := BookAttribution{Book: book, PaperEquity: 1, DemoEquity: 1}
		mark, hasMark := in.Marks[book]
		if hasMark {
			ba.MarkDate, ba.MarkClose = nyDate(mark.Date), mark.Close
		}
		rate := in.OvernightBps[book] / 1e4
		var cur *Lot
		for _, r := range recs {
			d := r.Demo
			ba.PaperEquity, ba.PaperStarted = r.Paper.Equity, r.Paper.Started
			o := d.Order
			if o == nil || o.Status != "executed" {
				continue
			}
			switch o.Kind {
			case etoroledger.ActionOpen:
				lot := Lot{Book: book, PositionID: d.PositionAfter.PositionID, OpenDate: r.Decision.FillDate,
					OpenPrice: o.AvgPrice, Units: o.Units, Amount: o.Amount, Open: true}
				if lot.OpenPrice == 0 {
					lot.OpenPrice = d.PositionAfter.OpenRate
				}
				if lot.Units == 0 {
					lot.Units = d.PositionAfter.Units
				}
				if lot.Amount == 0 {
					lot.Amount = d.PositionAfter.Amount
				}
				lot.SpreadPaid += o.SpreadCost
				lot.Fees += o.Fees
				if d.Costs != nil {
					lot.Markup += d.Costs.Markup
					if o.SpreadCost == 0 {
						lot.SpreadPaid += d.Costs.Spread
					}
				}
				ba.Lots = append(ba.Lots, lot)
				cur = &ba.Lots[len(ba.Lots)-1]
			case etoroledger.ActionClose:
				if cur == nil || !cur.Open {
					continue // a close without a recorded open: nothing to attribute
				}
				cur.Open = false
				cur.CloseDate = r.Decision.FillDate
				cur.ClosePrice = o.AvgPrice
				cur.SpreadPaid += o.SpreadCost
				cur.Fees += o.Fees
				if d.Costs != nil {
					cur.Markup += d.Costs.Markup
				}
				cur = nil
			}
		}
		for i := range ba.Lots {
			lot := &ba.Lots[i]
			var closeDate time.Time
			if lot.Open {
				closeDate = in.AsOf
				if hasMark {
					lot.ClosePrice = mark.Close
				}
			} else {
				closeDate, _ = time.Parse("2006-01-02", lot.CloseDate)
			}
			openDate, _ := time.Parse("2006-01-02", lot.OpenDate)
			if !openDate.IsZero() && !closeDate.IsZero() && closeDate.After(openDate) {
				lot.Nights = int(closeDate.Sub(openDate).Hours() / 24)
			}
			lot.FinancingModelled = float64(lot.Nights) * rate * lot.Amount
			if lot.ClosePrice > 0 {
				lot.PricePnL = (lot.ClosePrice - lot.OpenPrice) * lot.Units
			}
			lot.Net = lot.PricePnL - lot.SpreadPaid - lot.Markup - lot.Fees - lot.FinancingModelled
			if lot.Open {
				ba.Unrealized.add(lot.Breakdown)
			} else {
				ba.Realized.add(lot.Breakdown)
			}
			ba.Total.add(lot.Breakdown)
			if lot.Amount > 0 {
				ba.DemoEquity *= 1 + lot.Net/lot.Amount
			}
		}
		if charged, ok := in.FinancingCharged[book]; ok {
			c := charged
			n := ba.Total.Net + ba.Total.FinancingModelled - charged
			ba.FinancingCharged, ba.NetWithCharged = &c, &n
		}
		ba.TrackingBps = (ba.DemoEquity - ba.PaperEquity) * 1e4
		out.Realized.add(ba.Realized)
		out.Unrealized.add(ba.Unrealized)
		out.Total.add(ba.Total)
		out.Books = append(out.Books, ba)
	}
	return out
}
