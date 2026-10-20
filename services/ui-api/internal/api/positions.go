package api

// Positions and round trips (2026-10-19): GET /api/ui/etoro/positions answers
// one row per position the ledger has seen, paired from its open, protect,
// close and venue_close lines (whoever wrote them: the 09:35 run or the
// console), so the operator can read where a trade stands without walking
// the ledger table: open with the account's live unrealised P&L, or closed
// with the close price, who closed it and eToro's realised figures.
//
// Two layers, kept apart so the view is honest about its sources:
//
//   - the ledger (always): open / close rates and times as the orders
//     filled, units, amount, the stop and target recorded, the source
//     (rule | manual) and the operator, the gross P&L = units x (close -
//     open) and the financing eToro had charged at the last run;
//   - the account (when ui-api runs with -etoro-trading, i.e. has the demo
//     broker): the open positions' unrealised P&L and fees to date, and the
//     trading history's net profit and fees for the closed ones. A ledger
//     position the account no longer holds is looked up in the history: a
//     close at the stop or the target is reported as the venue's, anything
//     else as closed outside the ledger (resolve by hand, runbook §9.4);
//     not in the history yet is "unknown" (eToro's history lags).
//
// The live reads (account, positions, history: three reads) are cached
// for positionsCacheTTL per ledger so a page polling every 60 s and
// several tabs cost one set of reads a minute, far inside the 60 reads/min
// the key shares with the rates poller and the trades.

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"bitso-trading-platform/shared/pkg/broker"
	"bitso-trading-platform/shared/pkg/etoroledger"
)

// Trade statuses, sources and closers.
const (
	TradeStatusOpen    = "open"
	TradeStatusClosed  = "closed"
	TradeStatusUnknown = "unknown" // the account does not hold it and the history does not list it yet

	TradeSourceRule    = "rule"
	TradeSourceManual  = "manual"
	TradeSourceUnknown = "unknown" // seen held before the ledger recorded its open

	ClosedByRule     = "rule"
	ClosedByOperator = "operator"
	ClosedByVenue    = "venue"   // at the stop or the target
	ClosedByOutside  = "outside" // closed on the portal or by a margin call, not through the ledger
	ClosedByUnknown  = "unknown"
)

// stopOutTolerance says how close a history close rate must be to the stop
// or target (fraction of the rate) to be called the venue's close (plan §6).
const stopOutTolerance = 0.0005

// positionsCacheTTL bounds the live reads behind the view.
const positionsCacheTTL = 45 * time.Second

// EtoroTrade is one position's life as the ledger and the account tell it.
type EtoroTrade struct {
	Book       string `json:"book"`
	Symbol     string `json:"symbol"`
	PositionID string `json:"position_id"`
	Status     string `json:"status"` // TradeStatusOpen | TradeStatusClosed | TradeStatusUnknown
	// Source is who opened it; Operator the console's by / reason.
	Source   string                `json:"source"`
	Operator *etoroledger.Operator `json:"operator,omitempty"`

	// The open, from the ledger line that recorded it (RecordedOpen) or
	// from the first line that saw the position held (not recorded: the
	// ledger started after the open).
	RecordedOpen  bool    `json:"recorded_open"`
	OpenedAt      string  `json:"opened_at,omitempty"`
	OpenRate      float64 `json:"open_rate"`
	Units         float64 `json:"units"`
	Amount        float64 `json:"amount"`
	OpenClientRef string  `json:"open_client_ref,omitempty"`
	OpenBarDate   string  `json:"open_bar_date,omitempty"`
	// OpenFillVsMidBps and OpenCosts are the open order's fill quality and
	// eToro's cost preview at the time.
	OpenFillVsMidBps float64            `json:"open_fill_vs_mid_bps,omitempty"`
	OpenCosts        *etoroledger.Costs `json:"open_costs,omitempty"`

	// Protection as the ledger last recorded it, and how many protect
	// lines moved it.
	StopLossRate   float64 `json:"stop_loss_rate,omitempty"`
	TakeProfitRate float64 `json:"take_profit_rate,omitempty"`
	StopType       string  `json:"stop_type,omitempty"`
	ProtectEdits   int     `json:"protect_edits,omitempty"`

	// FinancingToDate is the financing eToro had charged at the last ledger
	// line that saw the position held (the account's figure supersedes it
	// in FeesToDate when live).
	FinancingToDate float64 `json:"financing_to_date,omitempty"`

	// Open positions, from the account when live (MarkSource "account"):
	// MarkRate is implied from the unrealised P&L (open + pnl / units).
	AccountHolds  *bool   `json:"account_holds,omitempty"`
	MarkSource    string  `json:"mark_source,omitempty"`
	MarkRate      float64 `json:"mark_rate,omitempty"`
	UnrealizedPnL float64 `json:"unrealized_pnl,omitempty"`
	UnrealizedPct float64 `json:"unrealized_pct,omitempty"`
	FeesToDate    float64 `json:"fees_to_date,omitempty"`
	// AccountStopLossRate / AccountTakeProfitRate are the rates the account
	// holds; ProtectionDrift says they differ from the ledger's.
	AccountStopLossRate   float64 `json:"account_stop_loss_rate,omitempty"`
	AccountTakeProfitRate float64 `json:"account_take_profit_rate,omitempty"`
	ProtectionDrift       bool    `json:"protection_drift,omitempty"`
	HeldDays              int     `json:"held_days,omitempty"`

	// Closed positions. RecordedClose is false when the close was found in
	// the history only. GrossPnL is units x (close - open) at the ledger's
	// (or the history's) rates; NetProfit and Fees are eToro's figures
	// when the history was read (VenueConfirmed), Net = NetProfit - Fees.
	RecordedClose     bool     `json:"recorded_close,omitempty"`
	ClosedAt          string   `json:"closed_at,omitempty"`
	CloseRate         float64  `json:"close_rate,omitempty"`
	ClosedBy          string   `json:"closed_by,omitempty"`
	CloseKind         string   `json:"close_kind,omitempty"` // etoroledger venue-close kinds
	CloseReason       string   `json:"close_reason,omitempty"`
	CloseClientRef    string   `json:"close_client_ref,omitempty"`
	CloseBarDate      string   `json:"close_bar_date,omitempty"`
	CloseFillVsMidBps float64  `json:"close_fill_vs_mid_bps,omitempty"`
	GrossPnL          *float64 `json:"gross_pnl,omitempty"`
	ReturnPct         *float64 `json:"return_pct,omitempty"`
	VenueConfirmed    bool     `json:"venue_confirmed,omitempty"`
	NetProfit         *float64 `json:"net_profit,omitempty"`
	Fees              *float64 `json:"fees,omitempty"`
	Net               *float64 `json:"net,omitempty"`
	VenueCloseRate    float64  `json:"venue_close_rate,omitempty"`

	Notes []string `json:"notes,omitempty"`
}

// EtoroTradeSummary totals the rows.
type EtoroTradeSummary struct {
	Open    int `json:"open"`
	Closed  int `json:"closed"`
	Unknown int `json:"unknown"`
	// OpenExposure is the open rows' amount; UnrealizedPnL and FeesToDate
	// their account figures (0 without the account).
	OpenExposure  float64 `json:"open_exposure"`
	UnrealizedPnL float64 `json:"unrealized_pnl"`
	FeesToDate    float64 `json:"fees_to_date"`
	// RealizedGross sums the closed rows' gross P&L; RealizedNet the net of
	// the ones eToro confirmed (RealizedConfirmed of them).
	RealizedGross     float64 `json:"realized_gross"`
	RealizedNet       float64 `json:"realized_net"`
	RealizedConfirmed int     `json:"realized_confirmed"`
}

// EtoroLiveAccount is the demo account as the broker reported it.
type EtoroLiveAccount struct {
	Currency      string  `json:"currency"`
	Cash          float64 `json:"cash"`
	Equity        float64 `json:"equity"`
	UnrealizedPnL float64 `json:"unrealized_pnl"`
	At            string  `json:"at"`
}

// EtoroPositionsResponse is GET /api/ui/etoro/positions.
type EtoroPositionsResponse struct {
	Ledger      string `json:"ledger"`
	Venue       string `json:"venue"`
	GeneratedAt string `json:"generated_at"`
	// Live says the account was read; LiveReason why not (or what failed
	// partially, see Warnings). Cached says the live figures came from the
	// cache (CachedAt when).
	Live       bool              `json:"live"`
	LiveReason string            `json:"live_reason,omitempty"`
	Cached     bool              `json:"cached,omitempty"`
	CachedAt   string            `json:"cached_at,omitempty"`
	Account    *EtoroLiveAccount `json:"account,omitempty"`
	Summary    EtoroTradeSummary `json:"summary"`
	Trades     []EtoroTrade      `json:"trades"`
	Warnings   []string          `json:"warnings"`
}

// positionsCache is the per-ledger live snapshot (Server.posCache).
type positionsCache struct {
	mu   sync.Mutex
	byLn map[string]cachedPositions
}

type cachedPositions struct {
	at   time.Time
	resp EtoroPositionsResponse
}

// buildEtoroTrades pairs the ledger lines of every book into trades, newest
// open first. Pure: the live layer is added by liveTrades.
func buildEtoroTrades(by map[string][]etoroledger.Record, now time.Time) []EtoroTrade {
	var out []EtoroTrade
	for _, book := range etoroBooks(by) {
		out = append(out, buildBookTrades(book, by[book], now)...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].OpenedAt != out[j].OpenedAt {
			return out[i].OpenedAt > out[j].OpenedAt
		}
		return out[i].PositionID > out[j].PositionID
	})
	if out == nil {
		out = []EtoroTrade{}
	}
	return out
}

func buildBookTrades(book string, recs []etoroledger.Record, now time.Time) []EtoroTrade {
	rs := append([]etoroledger.Record(nil), recs...)
	sort.SliceStable(rs, func(i, j int) bool {
		if rs[i].Decision.BarDate != rs[j].Decision.BarDate {
			return rs[i].Decision.BarDate < rs[j].Decision.BarDate
		}
		return rs[i].RecordedAt < rs[j].RecordedAt
	})
	var order []string
	byID := map[string]*EtoroTrade{}
	symbol := strings.ToUpper(book)
	// seen returns the trade of a position id, creating an unrecorded one
	// from the position the line saw held when the ledger never recorded
	// its open.
	seen := func(p etoroledger.Position, rec etoroledger.Record) *EtoroTrade {
		if p.PositionID == "" {
			return nil
		}
		if t, ok := byID[p.PositionID]; ok {
			return t
		}
		t := &EtoroTrade{Book: book, Symbol: rec.Instrument.Symbol, PositionID: p.PositionID, Status: TradeStatusOpen, Source: TradeSourceUnknown,
			OpenedAt: p.OpenedAt, OpenRate: p.OpenRate, Units: p.Units, Amount: p.Amount,
			StopLossRate: p.StopLossRate, TakeProfitRate: p.TakeProfitRate, StopType: p.StopType,
			Notes: []string{fmt.Sprintf("held before the ledger recorded its open (first seen on the %s line)", rec.Decision.BarDate)}}
		if t.Symbol == "" {
			t.Symbol = symbol
		}
		byID[p.PositionID] = t
		order = append(order, p.PositionID)
		return t
	}
	for _, rec := range rs {
		d := rec.Demo
		if d == nil {
			continue
		}
		pb, pa := d.PositionBefore, d.PositionAfter
		if pb.State == "long" {
			seen(pb, rec)
		}
		o := d.Order
		executed := o != nil && o.Status == string(broker.StatusExecuted)
		switch d.Action {
		case etoroledger.ActionOpen:
			if !executed || pa.PositionID == "" {
				continue
			}
			t := &EtoroTrade{Book: book, Symbol: rec.Instrument.Symbol, PositionID: pa.PositionID, Status: TradeStatusOpen, Source: TradeSourceRule,
				RecordedOpen: true, OpenedAt: pa.OpenedAt, OpenRate: pa.OpenRate, Units: pa.Units, Amount: pa.Amount,
				OpenClientRef: o.ClientRef, OpenBarDate: rec.Decision.BarDate, OpenFillVsMidBps: o.FillVsMidBps, OpenCosts: d.Costs,
				StopLossRate: pa.StopLossRate, TakeProfitRate: pa.TakeProfitRate, StopType: pa.StopType}
			if t.Symbol == "" {
				t.Symbol = symbol
			}
			if t.OpenedAt == "" && !o.Finished.IsZero() {
				t.OpenedAt = o.Finished.UTC().Format(time.RFC3339)
			}
			if t.OpenRate == 0 {
				t.OpenRate = o.AvgPrice
			}
			if t.Units == 0 {
				t.Units = o.Units
			}
			if t.Amount == 0 {
				t.Amount = o.Amount
			}
			if rec.IsUI() {
				t.Source, t.Operator = TradeSourceManual, rec.Operator
			}
			byID[pa.PositionID] = t
			order = append(order, pa.PositionID)
		case etoroledger.ActionProtect:
			t := seen(pa, rec)
			if t == nil {
				continue
			}
			if executed || (o != nil && o.Status == string(broker.StatusPending)) {
				t.StopLossRate, t.TakeProfitRate, t.StopType = pa.StopLossRate, pa.TakeProfitRate, pa.StopType
				t.ProtectEdits++
			}
		case etoroledger.ActionClose:
			t := seen(pb, rec)
			if t == nil && o != nil && len(o.PositionIDs) > 0 {
				t = seen(etoroledger.Position{State: "long", PositionID: o.PositionIDs[0]}, rec)
			}
			if t == nil || !executed {
				continue
			}
			t.Status, t.RecordedClose = TradeStatusClosed, true
			t.CloseRate, t.CloseClientRef, t.CloseBarDate, t.CloseFillVsMidBps = o.AvgPrice, o.ClientRef, rec.Decision.BarDate, o.FillVsMidBps
			if !o.Finished.IsZero() {
				t.ClosedAt = o.Finished.UTC().Format(time.RFC3339)
			}
			if rec.IsUI() {
				t.ClosedBy = ClosedByOperator
				if rec.Operator != nil {
					t.CloseReason = rec.Operator.Reason
				}
			} else {
				t.ClosedBy = ClosedByRule
				t.CloseReason = fmt.Sprintf("the rule said %s at the %s close", rec.Decision.Signal, rec.Decision.BarDate)
			}
			if d.FinancingToDate != 0 {
				t.FinancingToDate = d.FinancingToDate
			}
			t.setGross()
		case etoroledger.ActionVenueClose:
			t := seen(pb, rec)
			if t == nil {
				continue
			}
			vc := d.VenueClose
			t.Status, t.RecordedClose, t.ClosedBy, t.VenueConfirmed = TradeStatusClosed, true, ClosedByVenue, true
			t.CloseBarDate = rec.Decision.BarDate
			if o != nil {
				t.CloseRate = o.AvgPrice
				if !o.Finished.IsZero() {
					t.ClosedAt = o.Finished.UTC().Format(time.RFC3339)
				}
			}
			if vc != nil {
				t.CloseKind = vc.Kind
				if vc.Kind == etoroledger.VenueCloseOther {
					t.ClosedBy = ClosedByOutside
				}
				if vc.CloseRate > 0 {
					t.CloseRate, t.VenueCloseRate = vc.CloseRate, vc.CloseRate
				}
				if vc.ClosedAt != "" {
					t.ClosedAt = vc.ClosedAt
				}
				t.setVenue(vc.NetProfit, vc.Fees)
				t.CloseReason = venueReason(vc.Kind)
			}
			t.setGross()
		default: // none, blocked: the position held through the line
			if pa.State == "long" {
				if t := seen(pa, rec); t != nil {
					if d.FinancingToDate != 0 {
						t.FinancingToDate = d.FinancingToDate
					}
					if pa.StopLossRate > 0 || pa.TakeProfitRate > 0 {
						t.StopLossRate, t.TakeProfitRate, t.StopType = pa.StopLossRate, pa.TakeProfitRate, pa.StopType
					}
				}
			}
		}
	}
	out := make([]EtoroTrade, 0, len(order))
	for _, id := range order {
		t := byID[id]
		if t.Status == TradeStatusOpen {
			t.HeldDays = heldDays(t.OpenedAt, now)
		} else if t.ClosedAt != "" {
			if at, err := time.Parse(time.RFC3339, t.ClosedAt); err == nil {
				t.HeldDays = heldDays(t.OpenedAt, at)
			}
		}
		out = append(out, *t)
	}
	return out
}

func venueReason(kind string) string {
	switch kind {
	case etoroledger.VenueCloseStoppedOut:
		return "eToro closed it at the stop loss"
	case etoroledger.VenueCloseTookProfit:
		return "eToro closed it at the take profit"
	default:
		return "closed at eToro outside the ledger (portal, margin call or unknown)"
	}
}

func heldDays(openedAt string, until time.Time) int {
	at, err := time.Parse(time.RFC3339, openedAt)
	if err != nil || until.Before(at) {
		return 0
	}
	return int(math.Round(until.Sub(at).Hours() / 24))
}

// setGross computes the price P&L and the return from the rates held.
func (t *EtoroTrade) setGross() {
	if t.Units <= 0 || t.OpenRate <= 0 || t.CloseRate <= 0 {
		return
	}
	g := t.Units * (t.CloseRate - t.OpenRate)
	t.GrossPnL = &g
	if t.Amount > 0 {
		r := g / t.Amount
		t.ReturnPct = &r
	}
}

// setVenue records eToro's realised figures (net profit before fees, the
// fees charged over the life; net after both).
func (t *EtoroTrade) setVenue(netProfit, fees float64) {
	np, f, n := netProfit, fees, netProfit-fees
	t.NetProfit, t.Fees, t.Net, t.VenueConfirmed = &np, &f, &n, true
}

// summarize totals the rows.
func summarize(ts []EtoroTrade) EtoroTradeSummary {
	var s EtoroTradeSummary
	for _, t := range ts {
		switch t.Status {
		case TradeStatusOpen:
			s.Open++
			s.OpenExposure += t.Amount
			s.UnrealizedPnL += t.UnrealizedPnL
			s.FeesToDate += t.FeesToDate
		case TradeStatusClosed:
			s.Closed++
			if t.GrossPnL != nil {
				s.RealizedGross += *t.GrossPnL
			}
			if t.Net != nil {
				s.RealizedNet += *t.Net
				s.RealizedConfirmed++
			}
		default:
			s.Unknown++
		}
	}
	return s
}

// liveTrades adds the account's figures to the ledger's rows: the open
// positions' unrealised P&L, the history's realised figures, and the fate
// of a ledger position the account no longer holds. Every failure is a
// warning, never an error: the ledger's rows stand on their own.
func (s *Server) liveTrades(ctx context.Context, b broker.Broker, ts []EtoroTrade, resp *EtoroPositionsResponse) {
	now := s.Now().UTC()
	acct, err := b.Account(ctx)
	if err != nil {
		resp.Warnings = append(resp.Warnings, "account: "+err.Error()+"; the figures are the ledger's")
		resp.LiveReason = "the account could not be read"
		return
	}
	resp.Live = true
	resp.Account = &EtoroLiveAccount{Currency: acct.Currency, Cash: acct.Cash, Equity: acct.Equity, UnrealizedPnL: acct.UnrealizedPnL, At: now.Format(time.RFC3339)}

	held := map[string]broker.Position{}
	if ps, err := b.Positions(ctx); err != nil {
		resp.Warnings = append(resp.Warnings, "positions: "+err.Error()+"; the open rows carry the ledger's figures")
	} else {
		for _, p := range ps {
			held[p.ID] = p
		}
		for i := range ts {
			t := &ts[i]
			if t.Status != TradeStatusOpen {
				continue
			}
			p, ok := held[t.PositionID]
			holds := ok
			t.AccountHolds = &holds
			if !ok {
				continue
			}
			t.MarkSource = "account"
			t.UnrealizedPnL, t.FeesToDate = p.UnrealizedPnL, p.Fees
			if t.Units > 0 {
				t.MarkRate = t.OpenRate + p.UnrealizedPnL/t.Units
			}
			if t.Amount > 0 {
				t.UnrealizedPct = p.UnrealizedPnL / t.Amount
			}
			t.AccountStopLossRate, t.AccountTakeProfitRate = p.StopLoss, p.TakeProfit
			t.ProtectionDrift = ratesDiffer(p.StopLoss, t.StopLossRate) || ratesDiffer(p.TakeProfit, t.TakeProfitRate)
			if t.ProtectionDrift {
				t.Notes = append(t.Notes, fmt.Sprintf("the account holds stop %s / target %s, the ledger recorded %s / %s", rateText(p.StopLoss), rateText(p.TakeProfit), rateText(t.StopLossRate), rateText(t.TakeProfitRate)))
			}
		}
	}

	// The history: for open rows the account does not hold, and for closed
	// rows without eToro's figures.
	var need []int
	since := now
	for i := range ts {
		t := &ts[i]
		missing := t.Status == TradeStatusOpen && t.AccountHolds != nil && !*t.AccountHolds
		unconfirmed := t.Status == TradeStatusClosed && !t.VenueConfirmed
		if !missing && !unconfirmed {
			continue
		}
		need = append(need, i)
		if at, err := time.Parse(time.RFC3339, t.OpenedAt); err == nil && at.Before(since) {
			since = at
		}
	}
	if len(need) == 0 {
		return
	}
	hr, ok := b.(broker.HistoryReader)
	if !ok {
		resp.Warnings = append(resp.Warnings, "this broker cannot read the trading history: closed rows carry the ledger's figures")
		return
	}
	since = since.Add(-24 * time.Hour)
	if floor := now.Add(-364 * 24 * time.Hour); since.Before(floor) {
		since = floor
	}
	closed, err := hr.ClosedPositions(ctx, since)
	if err != nil {
		resp.Warnings = append(resp.Warnings, "trading history: "+err.Error()+"; closed rows carry the ledger's figures")
		return
	}
	hist := map[string]broker.ClosedPosition{}
	for _, c := range closed {
		hist[c.ID] = c
	}
	for _, i := range need {
		t := &ts[i]
		c, ok := hist[t.PositionID]
		switch {
		case t.Status == TradeStatusClosed && ok:
			t.setVenue(c.NetProfit, c.Fees)
			if c.CloseRate > 0 {
				t.VenueCloseRate = c.CloseRate
				if ratesDiffer(c.CloseRate, t.CloseRate) {
					t.Notes = append(t.Notes, fmt.Sprintf("eToro's history closes it at %s, the ledger recorded %s", rateText(c.CloseRate), rateText(t.CloseRate)))
				}
			}
		case t.Status == TradeStatusOpen && ok:
			// The ledger says open, the account does not hold it, the
			// history has it: closed outside the ledger.
			t.Status, t.RecordedClose = TradeStatusClosed, false
			t.CloseRate, t.VenueCloseRate = c.CloseRate, c.CloseRate
			if !c.ClosedAt.IsZero() {
				t.ClosedAt = c.ClosedAt.UTC().Format(time.RFC3339)
				t.HeldDays = heldDays(t.OpenedAt, c.ClosedAt)
			}
			t.setVenue(c.NetProfit, c.Fees)
			t.setGross()
			switch {
			case c.StopLoss > 0 && math.Abs(c.CloseRate-c.StopLoss) <= stopOutTolerance*c.StopLoss:
				t.ClosedBy, t.CloseKind = ClosedByVenue, etoroledger.VenueCloseStoppedOut
			case c.TakeProfit > 0 && math.Abs(c.CloseRate-c.TakeProfit) <= stopOutTolerance*c.TakeProfit:
				t.ClosedBy, t.CloseKind = ClosedByVenue, etoroledger.VenueCloseTookProfit
			default:
				t.ClosedBy, t.CloseKind = ClosedByOutside, etoroledger.VenueCloseOther
			}
			t.CloseReason = venueReason(t.CloseKind)
			t.Notes = append(t.Notes, "not in the ledger: the next 09:35 run reports it; until then resolve by hand (runbook §9.4)")
		case t.Status == TradeStatusOpen:
			t.Status = TradeStatusUnknown
			t.Notes = append(t.Notes, "the account does not hold it and the trading history does not list it yet (eToro's history lags, up to hours): check the portal")
		}
	}
}

func ratesDiffer(a, b float64) bool {
	if a == 0 && b == 0 {
		return false
	}
	if a == 0 || b == 0 {
		return true
	}
	return math.Abs(a-b) > stopOutTolerance*math.Max(a, b)
}

func rateText(r float64) string {
	if r <= 0 {
		return "none"
	}
	return fmt.Sprintf("%.2f", r)
}

// etoroPositions serves the view. The ledger is read every time (cheap,
// local); the live layer comes from the cache when fresh.
func (s *Server) etoroPositions(w http.ResponseWriter, r *http.Request) {
	l, by, ok := s.etoroLoad(w, r)
	if !ok {
		return
	}
	now := s.Now().UTC()
	resp := EtoroPositionsResponse{Ledger: l.Name, Venue: VenueEtoro, GeneratedAt: now.Format(time.RFC3339), Warnings: []string{}}
	resp.Trades = buildEtoroTrades(by, now)
	if book := r.URL.Query().Get("book"); book != "" {
		var f []EtoroTrade
		for _, t := range resp.Trades {
			if t.Book == book {
				f = append(f, t)
			}
		}
		if f == nil {
			f = []EtoroTrade{}
		}
		resp.Trades = f
	}
	switch {
	case s.Trading == nil:
		resp.LiveReason = s.TradingOffReason
		if resp.LiveReason == "" {
			resp.LiveReason = tradeOffDefault
		}
		resp.LiveReason = "the account is not read (" + resp.LiveReason + "): the figures are the ledger's"
	default:
		s.cachedLive(r.Context(), l.Name, by, &resp)
	}
	resp.Summary = summarize(resp.Trades)
	writeJSON(w, http.StatusOK, resp)
}

// cachedLive runs liveTrades through the per-ledger cache. The cache keeps
// the whole live response (every book) and the rows asked for are taken
// from it, so a book filter does not cost a second set of reads.
func (s *Server) cachedLive(ctx context.Context, ledger string, by map[string][]etoroledger.Record, resp *EtoroPositionsResponse) {
	s.posCache.mu.Lock()
	defer s.posCache.mu.Unlock()
	now := s.Now().UTC()
	if s.posCache.byLn == nil {
		s.posCache.byLn = map[string]cachedPositions{}
	}
	c, ok := s.posCache.byLn[ledger]
	if !ok || now.Sub(c.at) >= positionsCacheTTL || now.Before(c.at) {
		full := EtoroPositionsResponse{Warnings: []string{}}
		full.Trades = buildEtoroTrades(by, now)
		s.liveTrades(ctx, s.Trading.Broker, full.Trades, &full)
		c = cachedPositions{at: now, resp: full}
		s.posCache.byLn[ledger] = c
		resp.Cached = false
	} else {
		resp.Cached, resp.CachedAt = true, c.at.Format(time.RFC3339)
	}
	resp.Live, resp.LiveReason, resp.Account = c.resp.Live, c.resp.LiveReason, c.resp.Account
	resp.Warnings = append(resp.Warnings, c.resp.Warnings...)
	liveBy := map[string]EtoroTrade{}
	for _, t := range c.resp.Trades {
		liveBy[t.PositionID] = t
	}
	for i := range resp.Trades {
		if lt, ok := liveBy[resp.Trades[i].PositionID]; ok {
			resp.Trades[i] = lt
		}
	}
}
