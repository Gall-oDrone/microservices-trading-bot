package api

// Protection of assisted trades (docs/etoro/ETORO-ASSISTED-TRADE-PROTECTION-PLAN-2026-10-19.md,
// phase B): the recommended stop-loss / take-profit levels per risk-aversion
// profile for an open or for an existing position, from the daily bars the
// executor saves (ATR), the live quote, the account and the venue's bounds.
//
//   - POST /api/ui/etoro/trades/recommend answers the levels only; it sends
//     nothing and is not audited unless the gates refuse it. It is a POST
//     so it shares the controls' gates (loopback Origin, operator token,
//     JSON body) without a second gate for GET;
//   - prepare (trades.go) computes the same levels for the side it is
//     given and binds them into the token; place sends them with the open
//     (atomic) or edits the held position (protect).
//
// Levels come from shared/pkg/stops; the policy's caps from
// risk.EtoroDemoProtectedPolicy; the venue bounds from the broker's
// ProtectionLimitsReader (eToro eligibility), with a conservative fallback
// when the broker cannot read them.

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"bitso-trading-platform/shared/pkg/broker"
	"bitso-trading-platform/shared/pkg/etoroledger"
	"bitso-trading-platform/shared/pkg/risk"
	"bitso-trading-platform/shared/pkg/stops"
	"bitso-trading-platform/ui-api/internal/audit"
)

// TradeProtect is the third side of a prepare request: set or move the
// stop loss / take profit of the ledger's position without trading it.
const TradeProtect = "protect"

// Fallback stop distance when no daily bars are available for an ATR: the
// disaster floor (8 %), inside the policy's 10 % cap, with a warning.
const fallbackStopPct = stops.DisasterFloorPct

// fallbackBounds are used when the broker cannot read the venue's rules:
// the evidence of 2026-10-10 for the x1 long index CFDs, with the stricter
// NSDQ100 minimum for both.
var fallbackBounds = stops.Bounds{MinStopLossPct: 0.01, MaxStopLossPct: 1, MaxTakeProfitPct: 10, Tick: 0.01}

// TradeRecommendRequest is the body of POST /api/ui/etoro/trades/recommend.
type TradeRecommendRequest struct {
	Book string `json:"book"`
	Side string `json:"side"` // TradeOpen | TradeProtect
	// AmountUSD sizes an open (default: the policy's default amount);
	// PositionID names the ledger's position for a protect (default: the
	// ledger's). Leverage (2026-10-22) is the open's leverage, default the
	// policy's (x1 on demo, x20 on the canary).
	AmountUSD  float64 `json:"amount_usd,omitempty"`
	PositionID string  `json:"position_id,omitempty"`
	Leverage   int     `json:"leverage,omitempty"`
}

func (r TradeRecommendRequest) validate(pol risk.Policy) error {
	if _, ok := tradeInstrument(r.Book); !ok {
		return fmt.Errorf("book: %q is not an eToro book of this executor (nsdq100, spx500)", r.Book)
	}
	b := boundsFor(pol, r.Book)
	switch r.Side {
	case TradeOpen:
		if r.AmountUSD != 0 && (math.IsNaN(r.AmountUSD) || r.AmountUSD < b.Min || r.AmountUSD > b.Max) {
			return fmt.Errorf("amount_usd: must be between %.0f and %.0f USD", b.Min, b.Max)
		}
		if r.Leverage != 0 && !b.allowsLeverage(r.Leverage) {
			return fmt.Errorf("leverage: must be one of %v (the policy)", b.Leverages)
		}
	case TradeProtect:
		if r.AmountUSD != 0 {
			return fmt.Errorf("amount_usd: not for a protect (the held position is sized already)")
		}
		if r.Leverage != 0 {
			return fmt.Errorf("leverage: not for a protect (the held position's applies)")
		}
		if r.PositionID != "" && (len(r.PositionID) > 32 || strings.Trim(r.PositionID, "0123456789") != "") {
			return fmt.Errorf("position_id: the numeric id of the ledger's position")
		}
	default:
		return fmt.Errorf(`side: "open" or "protect"`)
	}
	return nil
}

// TradeProtection is the protection block of a preview and of a
// recommendation: the levels the trade will carry and where they came from.
type TradeProtection struct {
	// Profile is the stops.Profile the levels follow; Levels the stop and
	// target after the guards (rates, fractions, money, R:R, clamps).
	Profile string       `json:"profile"`
	Levels  stops.Levels `json:"levels"`
	// Volatility is the ATR the levels came from (nil when no bars were
	// available and the fallback distance was used).
	Volatility *stops.Volatility `json:"volatility,omitempty"`
	// Entry / Mark are the prices the levels are measured against (the ask
	// for an open; the open rate and the bid for a protect).
	Entry float64 `json:"entry"`
	Mark  float64 `json:"mark"`
	// Bounds are the venue's rules; Limits the policy's caps.
	Bounds stops.Bounds `json:"bounds"`
	Limits stops.Limits `json:"limits"`
	// BoundsSource says where Bounds came from: "venue" or "fallback".
	BoundsSource string `json:"bounds_source"`
	// SMA50 is the rule's latest moving average (0 without a rule record):
	// the rule's own invalidation level, for context.
	SMA50 float64 `json:"sma50,omitempty"`
	// Before is what the position holds now (protect only).
	Before *ProtectionBefore `json:"before,omitempty"`
}

// ProtectionBefore is the position's protection as the account reports it.
type ProtectionBefore struct {
	StopLossRate   float64 `json:"stop_loss_rate,omitempty"`
	TakeProfitRate float64 `json:"take_profit_rate,omitempty"`
	StopType       string  `json:"stop_type,omitempty"`
	Protected      bool    `json:"protected"`
}

// TradeRecommendation is the answer to recommend.
type TradeRecommendation struct {
	Ledger     string                 `json:"ledger"`
	Book       string                 `json:"book"`
	Side       string                 `json:"side"`
	Instrument etoroledger.Instrument `json:"instrument"`
	Quote      *etoroledger.Quote     `json:"quote"`
	Account    *etoroledger.Account   `json:"account"`
	Position   etoroledger.Position   `json:"position"`
	RuleSignal string                 `json:"rule_signal"`
	AmountUSD  float64                `json:"amount_usd,omitempty"`
	Units      float64                `json:"units"`
	// Leverage, ExposureUSD and Environment (2026-10-22): the leverage the
	// levels were sized for, units x ask, and "demo" | "real".
	Leverage    int     `json:"leverage"`
	ExposureUSD float64 `json:"exposure_usd"`
	Environment string  `json:"environment"`
	// Profiles holds the three named profiles; DefaultProfile which one the
	// console pre-selects; Custom is nil here (the console sends its own
	// distances to prepare).
	Profiles       map[string]stops.Levels `json:"profiles"`
	DefaultProfile string                  `json:"default_profile"`
	Context        TradeProtection         `json:"context"`
	PolicyVersion  string                  `json:"policy_version"`
	Warnings       []string                `json:"warnings"`
	PreparedAt     string                  `json:"prepared_at"`
}

// protectionCtx is what preparing a protected trade needs beyond the
// preview's own reads.
type protectionCtx struct {
	in       stops.Input
	vol      *stops.Volatility
	bounds   stops.Bounds
	src      string
	limits   stops.Limits
	sma      float64
	warnings []string
}

// protectionInputs gathers the ATR from the ledger's candles, the venue's
// bounds from the broker and the policy's caps. side decides the entry and
// mark: an open measures from the ask (entry) and the bid (mark); a protect
// from the position's open rate (entry) and the bid (mark). exposure (the
// notional, invested x leverage) sizes the money figures of an open;
// leverage selects the venue's stop bounds (eToro states them as a share
// of the invested amount, so the adapter divides by the leverage: 50 % at
// x20 is 2.5 % of the price). It never fails: a missing input becomes a
// warning and a conservative default.
func (s *Server) protectionInputs(ctx context.Context, l Ledger, book, side string, q broker.Quote, acct broker.Account, pos etoroledger.Position, exposure float64, leverage int, pol risk.Policy) protectionCtx {
	pc := protectionCtx{warnings: []string{}}
	bl := pol.For(book)
	pc.limits = stops.Limits{MinStopLossPct: bl.MinStopLossPct, MaxStopLossPct: bl.MaxStopLossPct, MaxLossPctEquity: bl.MaxLossPctEquity}
	if leverage < 1 {
		leverage = 1
	}

	// Volatility from the executor's saved bars.
	if rows, _, err := l.Store.Candles(book); err != nil {
		pc.warnings = append(pc.warnings, fmt.Sprintf("no daily bars for %s in %s (%v): no ATR, the %.0f%% fallback stop applies", book, l.Store.Where(l.Store.CandlesDir), err, fallbackStopPct*100))
	} else {
		bars := make([]stops.Bar, 0, len(rows))
		for _, r := range rows {
			bars = append(bars, stops.Bar{High: r.High, Low: r.Low, Close: r.Close})
		}
		if v, err := stops.Measure(bars, stops.DefaultATRPeriod, stops.DefaultRegimeLookback); err != nil {
			pc.warnings = append(pc.warnings, fmt.Sprintf("ATR: %v: the %.0f%% fallback stop applies", err, fallbackStopPct*100))
		} else {
			pc.vol = &v
		}
	}

	// The venue's bounds for this leverage, or the fallback (x1 figures,
	// scaled by the leverage like the adapter does).
	in, _ := tradeInstrument(book)
	bin := broker.Instrument{Venue: s.Trading.Broker.Venue(), Symbol: in.Symbol, ID: in.ID}
	pc.bounds, pc.src = fallbackBounds, "fallback"
	if leverage > 1 {
		pc.bounds.MinStopLossPct /= float64(leverage)
		pc.bounds.MaxStopLossPct /= float64(leverage)
		pc.bounds.MaxTakeProfitPct /= float64(leverage)
	}
	if lr, ok := s.Trading.Broker.(broker.ProtectionLimitsReader); ok {
		if pl, err := lr.ProtectionLimits(ctx, bin, broker.Long, leverage); err != nil {
			pc.warnings = append(pc.warnings, fmt.Sprintf("venue bounds: %v; the fallback bounds (NSDQ100 x1 long, evidence 2026-10-10, scaled to x%d) apply", err, leverage))
		} else {
			pc.bounds = stops.Bounds{MinStopLossPct: pl.MinStopLossPct, MaxStopLossPct: pl.MaxStopLossPct, MaxTakeProfitPct: pl.MaxTakeProfitPct, Tick: pl.Tick}
			pc.src = "venue"
			if side == TradeProtect && !pl.AllowEdit {
				pc.warnings = append(pc.warnings, "the venue does not allow editing the stop of this position")
			}
		}
	} else {
		pc.warnings = append(pc.warnings, "this broker cannot read the venue's stop bounds; the fallback bounds apply")
	}

	// The rule's SMA50, for context.
	if recs, err := etoroledger.ReadFile(l.Store.LedgerPath); err == nil {
		for _, r := range recs {
			if r.Book == book && !r.IsUI() {
				pc.sma = r.Decision.SMA
			}
		}
	}

	pc.in = stops.Input{SpreadPct: spreadPct(q), Equity: acct.Equity, Bounds: pc.bounds, Limits: pc.limits}
	if pc.vol != nil {
		pc.in.ATR = pc.vol.ATR
	}
	switch side {
	case TradeProtect:
		pc.in.Entry, pc.in.Mark, pc.in.Units = pos.OpenRate, q.Bid, pos.Units
	default:
		pc.in.Entry, pc.in.Mark, pc.in.Amount = q.Ask, q.Bid, exposure
	}
	return pc
}

func spreadPct(q broker.Quote) float64 {
	if m := q.Mid(); m > 0 && q.Ask >= q.Bid {
		return (q.Ask - q.Bid) / m
	}
	return 0
}

// levelsFor picks the levels a prepare asked for: a named profile from the
// ATR recommendation, custom distances through the guards, or (when the
// request named nothing) the default profile, with a warning that says so.
// Without an ATR a named profile falls back to a fixed stop and no target.
func (pc *protectionCtx) levelsFor(profile stops.Profile, slPct, tpPct float64) (stops.Levels, error) {
	if profile == stops.Custom {
		return stops.Build(pc.in, slPct, tpPct)
	}
	if pc.in.ATR > 0 {
		all, err := stops.Recommend(pc.in)
		if err != nil {
			return stops.Levels{}, err
		}
		return all[profile], nil
	}
	l, err := stops.Build(pc.in, fallbackStopPct, 0)
	if err != nil {
		return l, err
	}
	l.Profile = profile
	l.Clamps = append([]string{fmt.Sprintf("no ATR available: a fixed %.0f%% stop and no target stand in for the %s profile", fallbackStopPct*100, profile)}, l.Clamps...)
	return l, nil
}

// recommendAll builds the three named profiles (each falls back without an
// ATR).
func (pc *protectionCtx) recommendAll() map[string]stops.Levels {
	out := map[string]stops.Levels{}
	for _, p := range stops.Profiles {
		if l, err := pc.levelsFor(p, 0, 0); err == nil {
			out[string(p)] = l
		}
	}
	return out
}

func (pc *protectionCtx) block(profile string, lv stops.Levels) TradeProtection {
	return TradeProtection{Profile: profile, Levels: lv, Volatility: pc.vol, Entry: pc.in.Entry, Mark: pc.in.Mark,
		Bounds: pc.bounds, Limits: pc.limits, BoundsSource: pc.src, SMA50: pc.sma}
}

// beforeOf reads the position's protection from the account.
func beforeOf(held *broker.Position) *ProtectionBefore {
	if held == nil {
		return nil
	}
	return &ProtectionBefore{StopLossRate: held.StopLoss, TakeProfitRate: held.TakeProfit, StopType: held.StopType, Protected: held.Protected()}
}

func (s *Server) tradeRecommend(w http.ResponseWriter, r *http.Request) {
	l, ok := s.pickEtoro(w, r)
	if !ok {
		return
	}
	if enabled, why := s.tradingState(l); !enabled {
		writeErr(w, http.StatusForbidden, why)
		return
	}
	ap := auditPath(l)
	base := s.baseEntry(r, "trade.recommend")
	base.Ledger = l.Name
	var req TradeRecommendRequest
	if !s.admit(w, r, base, []string{ap}, &req) {
		return
	}
	if err := req.validate(s.Trading.policy()); err != nil {
		e := base
		e.Outcome, e.Error = audit.Refused, err.Error()
		s.noteAll([]string{ap}, e)
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	rec, status, err := s.recommend(r.Context(), l, req)
	if err != nil {
		writeErr(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

// recommend reads what it needs and builds the profiles. A venue failure is
// an error with its HTTP status; a protect on a book the ledger holds flat
// is a 409.
func (s *Server) recommend(ctx context.Context, l Ledger, req TradeRecommendRequest) (TradeRecommendation, int, error) {
	b := s.Trading.Broker
	now := s.Now().UTC()
	in, _ := tradeInstrument(req.Book)
	bin := broker.Instrument{Venue: b.Venue(), Symbol: in.Symbol, ID: in.ID}
	rec := TradeRecommendation{Ledger: l.Name, Book: req.Book, Side: req.Side, Instrument: in, Warnings: []string{},
		DefaultProfile: string(stops.DefaultProfile), PreparedAt: now.Format(time.RFC3339)}

	recs, err := etoroledger.ReadFile(l.Store.LedgerPath)
	if err != nil {
		return rec, http.StatusInternalServerError, fmt.Errorf("cannot read the %s ledger: %v", l.Name, err)
	}
	pos := etoroledger.LastDemoPosition(recs, req.Book)
	rec.Position = pos
	for _, r := range recs {
		if r.Book == req.Book && !r.IsUI() {
			rec.RuleSignal = r.Decision.Signal
		}
	}
	if req.Side == TradeProtect {
		if pos.State != "long" {
			return rec, http.StatusConflict, fmt.Errorf("the ledger holds no position on %s to protect", in.Symbol)
		}
		if req.PositionID != "" && req.PositionID != pos.PositionID {
			return rec, http.StatusConflict, fmt.Errorf("position %s is not the ledger's position on %s (%s)", req.PositionID, in.Symbol, pos.PositionID)
		}
	}

	all, err := b.Positions(ctx)
	if err != nil {
		return rec, http.StatusBadGateway, fmt.Errorf("positions: %v", err)
	}
	acct, err := b.Account(ctx)
	if err != nil {
		return rec, http.StatusBadGateway, fmt.Errorf("account: %v", err)
	}
	rec.Account = &etoroledger.Account{Currency: acct.Currency, Cash: acct.Cash, Equity: acct.Equity, Exposure: tradeExposure(all)}
	q, err := b.Quote(ctx, bin)
	if err != nil {
		return rec, http.StatusBadGateway, fmt.Errorf("quote: %v", err)
	}
	if !(q.Bid > 0) || !(q.Ask > 0) {
		return rec, http.StatusBadGateway, fmt.Errorf("quote: eToro returned bid %v ask %v", q.Bid, q.Ask)
	}
	rec.Quote = &etoroledger.Quote{Bid: q.Bid, Ask: q.Ask, At: q.At.UTC().Format(time.RFC3339), SpreadBps: q.SpreadBps()}

	pol := s.Trading.policy()
	bounds := boundsFor(pol, req.Book)
	amount, lev := req.AmountUSD, req.Leverage
	if lev < 1 {
		lev = bounds.defaultLeverage()
	}
	if req.Side == TradeOpen {
		if amount == 0 {
			amount = bounds.Default
		}
		rec.AmountUSD, rec.Units, rec.Leverage = amount, amount*float64(lev)/q.Ask, lev
	} else {
		rec.Units = pos.Units
		if held := heldPosition(all, in.ID, pos.PositionID); held != nil && held.Leverage > 0 {
			lev = held.Leverage
		}
		rec.Leverage = lev
	}
	rec.ExposureUSD, rec.Environment = rec.Units*q.Ask, s.Trading.Environment()
	pc := s.protectionInputs(ctx, l, req.Book, req.Side, q, acct, pos, amount*float64(lev), lev, pol)
	rec.Warnings = append(rec.Warnings, pc.warnings...)
	rec.Profiles = pc.recommendAll()
	rec.PolicyVersion = pol.Version
	def := rec.Profiles[rec.DefaultProfile]
	rec.Context = pc.block(rec.DefaultProfile, def)
	if req.Side == TradeProtect {
		held := heldPosition(all, in.ID, pos.PositionID)
		rec.Context.Before = beforeOf(held)
		if held == nil {
			rec.Warnings = append(rec.Warnings, fmt.Sprintf("the account does not hold position %s (closed outside the executor?): prepare will block", pos.PositionID))
		}
	}
	return rec, http.StatusOK, nil
}

// heldPosition is the account's position id on the instrument, or nil.
func heldPosition(all []broker.Position, instrumentID int64, positionID string) *broker.Position {
	for _, p := range tradeOnInstrument(all, instrumentID) {
		if p.ID == positionID {
			p := p
			return &p
		}
	}
	return nil
}
