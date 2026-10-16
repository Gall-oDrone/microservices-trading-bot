package api

// Assisted trades on the eToro DEMO account (porting plan §4.4; P5 design
// §3 P5d, option 1 of §4): the operator prepares a trade, reads the preview
// (quote, costs, risk verdict with its reasons) and places it within two
// minutes, typing the confirmation phrase. Both steps are POSTs under
// /api/ui/etoro/trades/ and share the controls' gates (loopback Origin,
// operator token, JSON body, audit of every attempt).
//
// Safety model, on top of those gates:
//   - off unless ui-api runs with -etoro-trading AND an operator token AND a
//     local eToro ledger AND demo credentials; the broker is built for the
//     demo environment only and NewEtoroTrading refuses anything else;
//   - prepare never sends anything: it runs the checks in order (halt file,
//     risk.Check with EtoroDemoPolicy, eligibility against the ledger and the
//     account, live quote, cost preview, cash) and answers a verdict with
//     its reasons; only an allow verdict carries a token;
//   - the token is HMAC-SHA256 over the preview (client_ref, request id,
//     book, side, amount or position, units, quote, expiry) with a
//     per-process random secret keyed by SHA-256(operator token): bound to
//     this process and this operator session, 120 s to live, accepted once;
//   - place re-verifies the token, the phrase, the halt file, the session,
//     the ledger and the account, writes the intent FIRST (the executor's
//     journal, same x-request-id = UUIDv5(client_ref)), audits "requested",
//     sends through the broker adapter (which polls up to 30 s and resolves
//     a duplicate reference), appends the ledger line with source "ui" and
//     audits "done" or "failed". A failed place leaves the intent on disk,
//     so etoro-reconcile and the data-health page report it until it is
//     resolved by hand (the executor only resumes its own sma50-* intents);
//   - one mutex with the controls, plus the executor's own ledger lock
//     (<ledger>.lock, flock) for the duration of a place, so a run and a
//     console trade never interleave.
//
// The executor plans from the ledger: a console position against the rule
// is brought back to the rule's target at the next 09:35 run unless the
// ledger is halted (docs/etoro/ETORO-DAILY-EXECUTOR.md §8).

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"bitso-trading-platform/shared/pkg/broker"
	"bitso-trading-platform/shared/pkg/broker/etorobroker"
	"bitso-trading-platform/shared/pkg/etoro"
	"bitso-trading-platform/shared/pkg/etoroledger"
	"bitso-trading-platform/shared/pkg/mktcal"
	"bitso-trading-platform/shared/pkg/risk"
	"bitso-trading-platform/ui-api/internal/audit"
	"bitso-trading-platform/ui-api/internal/etorolive"
)

const (
	// TradeTokenTTL is how long a prepared trade may be placed.
	TradeTokenTTL = 120 * time.Second
	// TradeMinAmount is eToro's minimum exposure for the index CFDs (P1
	// evidence; the executor's floor); TradeMaxAmount EtoroDemoPolicy's
	// order notional cap; TradeDefaultAmount the forward-test size.
	TradeMinAmount     = 1000
	TradeMaxAmount     = 1500
	TradeDefaultAmount = 1100

	// Verdicts.
	VerdictAllow = "allow"
	VerdictBlock = "block"

	// Sides of a prepare request.
	TradeOpen  = "open"
	TradeClose = "close"

	tradeOffDefault = "assisted trading is off: start ui-api with -etoro-trading (UI_API_ETORO_TRADING=1), " +
		"-operator-token-file and -etoro-env-file naming a 0600 file with ETORO_ENV=demo"
)

// tradePaths accept POST (isControlPath).
var tradePaths = map[string]bool{
	"/api/ui/etoro/trades/prepare": true,
	"/api/ui/etoro/trades/place":   true,
}

// EtoroTrading is the Server's assisted-trade dependency: the demo broker,
// the per-process token secret and the set of tokens already used.
type EtoroTrading struct {
	Broker broker.Broker
	secret []byte
	mu     sync.Mutex
	used   map[string]int64 // token id -> expiry (unix s), pruned as they expire
}

// NewEtoroTrading builds the demo broker for the assisted trades. It refuses
// any environment but demo: nothing for the real account is reachable from
// the console.
func NewEtoroTrading(cfg etoro.Config) (*EtoroTrading, error) {
	if cfg.Env != etoro.EnvDemo {
		return nil, fmt.Errorf("assisted trading: ETORO_ENV must be demo (got %q); the real account is refused in code", cfg.Env)
	}
	c, err := etoro.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("assisted trading: %w", err)
	}
	return newEtoroTrading(etorobroker.New(c))
}

// newEtoroTrading wraps a broker (tests pass a fake; it must still be an
// eToro venue).
func newEtoroTrading(b broker.Broker) (*EtoroTrading, error) {
	if b == nil || b.Venue() != etorobroker.Venue {
		return nil, errors.New("assisted trading: the broker must be the eToro adapter")
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("assisted trading: token secret: %w", err)
	}
	return &EtoroTrading{Broker: b, secret: secret, used: map[string]int64{}}, nil
}

// TradingInfo is ControlsInfo.Trading: whether the assisted trades are
// available on a ledger.
type TradingInfo struct {
	Enabled        bool   `json:"enabled"`
	DisabledReason string `json:"disabled_reason,omitempty"`
	// TokenTTLSeconds and the amount bounds let the page build its form.
	TokenTTLSeconds int     `json:"token_ttl_s"`
	MinAmountUSD    float64 `json:"min_amount_usd"`
	MaxAmountUSD    float64 `json:"max_amount_usd"`
	DefaultAmount   float64 `json:"default_amount_usd"`
}

// tradingState reports whether a ledger accepts assisted trades, and why not.
func (s *Server) tradingState(l Ledger) (bool, string) {
	if l.venue() != VenueEtoro {
		return false, "assisted trading exists for eToro ledgers only"
	}
	if enabled, why := s.controlsState(l); !enabled {
		return false, why
	}
	if s.Trading == nil {
		if s.TradingOffReason != "" {
			return false, s.TradingOffReason
		}
		return false, tradeOffDefault
	}
	return true, ""
}

func (s *Server) tradingInfo(l Ledger) TradingInfo {
	enabled, why := s.tradingState(l)
	return TradingInfo{Enabled: enabled, DisabledReason: why, TokenTTLSeconds: int(TradeTokenTTL / time.Second),
		MinAmountUSD: TradeMinAmount, MaxAmountUSD: TradeMaxAmount, DefaultAmount: TradeDefaultAmount}
}

// tradeInstrument maps a book to its eToro instrument (the executor's frozen
// specs; etorolive.DefaultBooks carries the same ids for the rates poll).
func tradeInstrument(book string) (etoroledger.Instrument, bool) {
	id, ok := etorolive.DefaultBooks[book]
	if !ok {
		return etoroledger.Instrument{}, false
	}
	return etoroledger.Instrument{Symbol: strings.ToUpper(book), ID: id}, true
}

// TradePrepareRequest is the body of POST /api/ui/etoro/trades/prepare.
type TradePrepareRequest struct {
	Book string `json:"book"`
	Side string `json:"side"` // TradeOpen | TradeClose
	// AmountUSD is the x1 exposure of an open; PositionID the ledger's
	// position to close.
	AmountUSD  float64 `json:"amount_usd,omitempty"`
	PositionID string  `json:"position_id,omitempty"`
	Leverage   int     `json:"leverage"`
}

func (r TradePrepareRequest) validate() error {
	if _, ok := tradeInstrument(r.Book); !ok {
		return fmt.Errorf("book: %q is not an eToro book of this executor (nsdq100, spx500)", r.Book)
	}
	if r.Leverage != 1 {
		return errors.New("leverage: must be 1 (the policy allows x1 only)")
	}
	switch r.Side {
	case TradeOpen:
		if r.PositionID != "" {
			return errors.New("position_id: not for an open")
		}
		if math.IsNaN(r.AmountUSD) || r.AmountUSD < TradeMinAmount || r.AmountUSD > TradeMaxAmount {
			return fmt.Errorf("amount_usd: must be between %d and %d USD", TradeMinAmount, TradeMaxAmount)
		}
	case TradeClose:
		if r.AmountUSD != 0 {
			return errors.New("amount_usd: not for a close (the whole position is closed)")
		}
		if r.PositionID == "" || len(r.PositionID) > 32 || strings.Trim(r.PositionID, "0123456789") != "" {
			return errors.New("position_id: the numeric id of the ledger's position")
		}
	default:
		return errors.New(`side: "open" or "close"`)
	}
	return nil
}

func (r TradePrepareRequest) kind() string {
	if r.Side == TradeOpen {
		return etoroledger.ActionOpen
	}
	return etoroledger.ActionClose
}

// TradePreview is the answer to prepare: the checks' outcome and, when
// allowed, the token to place it.
type TradePreview struct {
	Verdict    string                 `json:"verdict"` // VerdictAllow | VerdictBlock
	Reasons    []string               `json:"reasons"`
	Warnings   []string               `json:"warnings"`
	Ledger     string                 `json:"ledger"`
	Book       string                 `json:"book"`
	Side       string                 `json:"side"`
	Instrument etoroledger.Instrument `json:"instrument"`
	// Position is what the ledger holds before the trade; RuleSignal the
	// rule's latest signal for the book ("" without a rule record).
	Position   etoroledger.Position   `json:"position"`
	RuleSignal string                 `json:"rule_signal"`
	Quote      *etoroledger.Quote     `json:"quote"`
	Costs      *etoroledger.Costs     `json:"costs"`
	Account    *etoroledger.Account   `json:"account"`
	Risk       *etoroledger.RiskCheck `json:"risk"`
	Units      float64                `json:"units"`
	AmountUSD  float64                `json:"amount_usd"`
	PositionID string                 `json:"position_id,omitempty"`
	// RefPrice is the last close the rule recorded (the price-deviation
	// guard's reference and fill_vs_close's).
	RefPrice     float64 `json:"ref_price"`
	Confirmation string  `json:"confirmation"`
	ClientRef    string  `json:"client_ref"`
	RequestID    string  `json:"request_id"`
	PreparedAt   string  `json:"prepared_at"`
	// Token and ExpiresAt are set on an allow verdict only.
	Token      string `json:"token,omitempty"`
	ExpiresAt  string `json:"expires_at,omitempty"`
	ExpiresInS int    `json:"expires_in_s,omitempty"`
}

// tradeClaims is the signed payload of a trade token (field order is the
// canonical JSON).
type tradeClaims struct {
	ID           string  `json:"id"`
	Ledger       string  `json:"ledger"`
	ClientRef    string  `json:"client_ref"`
	RequestID    string  `json:"request_id"`
	Book         string  `json:"book"`
	Side         string  `json:"side"`
	AmountUSD    float64 `json:"amount_usd,omitempty"`
	PositionID   string  `json:"position_id,omitempty"`
	Units        float64 `json:"units"`
	Bid          float64 `json:"bid"`
	Ask          float64 `json:"ask"`
	RefPrice     float64 `json:"ref_price"`
	Confirmation string  `json:"confirmation"`
	Iat          int64   `json:"iat"`
	Exp          int64   `json:"exp"`
}

// key derives the signing key of an operator session.
func (t *EtoroTrading) key(operatorToken string) []byte {
	h := sha256.Sum256([]byte(operatorToken))
	m := hmac.New(sha256.New, t.secret)
	m.Write(h[:])
	return m.Sum(nil)
}

func (t *EtoroTrading) sign(c tradeClaims, operatorToken string) (string, error) {
	b, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	m := hmac.New(sha256.New, t.key(operatorToken))
	m.Write(b)
	enc := base64.RawURLEncoding
	return enc.EncodeToString(b) + "." + enc.EncodeToString(m.Sum(nil)), nil
}

var errTradeToken = errors.New("trade token invalid or issued for another operator session: prepare again")

func (t *EtoroTrading) verify(tok, operatorToken string) (tradeClaims, error) {
	payload, sig, ok := strings.Cut(strings.TrimSpace(tok), ".")
	if !ok || len(tok) > 4096 {
		return tradeClaims{}, errTradeToken
	}
	enc := base64.RawURLEncoding
	b, err := enc.DecodeString(payload)
	if err != nil {
		return tradeClaims{}, errTradeToken
	}
	want, err := enc.DecodeString(sig)
	if err != nil {
		return tradeClaims{}, errTradeToken
	}
	m := hmac.New(sha256.New, t.key(operatorToken))
	m.Write(b)
	if !hmac.Equal(m.Sum(nil), want) {
		return tradeClaims{}, errTradeToken
	}
	var c tradeClaims
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil || c.ID == "" || c.ClientRef == "" {
		return tradeClaims{}, errTradeToken
	}
	return c, nil
}

// consume marks a token used; false when it already was. Expired entries
// are dropped as they are seen.
func (t *EtoroTrading) consume(id string, exp, now int64) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	for k, e := range t.used {
		if e < now {
			delete(t.used, k)
		}
	}
	if _, dup := t.used[id]; dup {
		return false
	}
	t.used[id] = exp
	return true
}

func (s *Server) tradePrepare(w http.ResponseWriter, r *http.Request) {
	l, ok := s.pickEtoro(w, r)
	if !ok {
		return
	}
	if enabled, why := s.tradingState(l); !enabled {
		writeErr(w, http.StatusForbidden, why)
		return
	}
	ap := auditPath(l)
	base := s.baseEntry(r, "trade.prepare")
	base.Ledger = l.Name
	var req TradePrepareRequest
	if !s.admit(w, r, base, []string{ap}, &req) {
		return
	}
	note := func(outcome, msg string) {
		e := base
		e.Outcome, e.Error = outcome, msg
		s.noteAll([]string{ap}, e)
	}
	if err := req.validate(); err != nil {
		note(audit.Refused, err.Error())
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	pv, status, err := s.preparePreview(r.Context(), l, req)
	if err != nil {
		note(audit.Failed, err.Error())
		writeErr(w, status, err.Error())
		return
	}
	now := s.Now().UTC()
	if pv.Verdict == VerdictAllow {
		exp := now.Add(TradeTokenTTL)
		c := tradeClaims{ID: newID(), Ledger: l.Name, ClientRef: pv.ClientRef, RequestID: pv.RequestID, Book: pv.Book, Side: pv.Side,
			AmountUSD: pv.AmountUSD, PositionID: pv.PositionID, Units: pv.Units, Bid: pv.Quote.Bid, Ask: pv.Quote.Ask, RefPrice: pv.RefPrice,
			Confirmation: pv.Confirmation, Iat: now.Unix(), Exp: exp.Unix()}
		tok, err := s.Trading.sign(c, s.OperatorToken)
		if err != nil {
			note(audit.Failed, "token: "+err.Error())
			writeErr(w, http.StatusInternalServerError, "could not sign the trade token")
			return
		}
		pv.Token, pv.ExpiresAt, pv.ExpiresInS = tok, exp.Format(time.RFC3339), int(TradeTokenTTL/time.Second)
	}
	e := base
	e.Ref = pv.ClientRef
	e.Detail = tradeDetail(pv.Side, pv.Book, pv.AmountUSD, pv.PositionID, pv.Units)
	if pv.Verdict == VerdictAllow {
		e.Outcome = audit.Done
		e.Detail += " -> allow"
	} else {
		e.Outcome = audit.Refused
		e.Error = "blocked: " + strings.Join(pv.Reasons, " | ")
	}
	if len(pv.Warnings) > 0 {
		e.Detail += " (warnings: " + strings.Join(pv.Warnings, " | ") + ")"
	}
	s.noteAll([]string{ap}, e)
	s.Log.Printf("trade.prepare ledger=%s %s %s -> %s id=%s", l.Name, pv.Side, pv.Book, pv.Verdict, pv.ClientRef)
	writeJSON(w, http.StatusOK, pv)
}

func tradeDetail(side, book string, amount float64, positionID string, units float64) string {
	if side == TradeOpen {
		return fmt.Sprintf("open %s %.0f USD x1 (%.6f units)", book, amount, units)
	}
	return fmt.Sprintf("close %s position %s (%.6f units)", book, positionID, units)
}

// preparePreview runs the checks. A local or venue failure is returned as
// an error with its HTTP status; a policy or eligibility failure is a block
// verdict with its reasons.
func (s *Server) preparePreview(ctx context.Context, l Ledger, req TradePrepareRequest) (TradePreview, int, error) {
	b := s.Trading.Broker
	now := s.Now().UTC()
	today := now.In(mktcal.NewYork).Format(dateFmt)
	in, _ := tradeInstrument(req.Book)
	bin := broker.Instrument{Venue: b.Venue(), Symbol: in.Symbol, ID: in.ID}
	pv := TradePreview{Verdict: VerdictBlock, Reasons: []string{}, Warnings: []string{}, Ledger: l.Name, Book: req.Book, Side: req.Side,
		Instrument: in, AmountUSD: req.AmountUSD, PositionID: req.PositionID, PreparedAt: now.Format(time.RFC3339)}
	if req.Side == TradeOpen {
		pv.Confirmation = "PLACE " + in.Symbol
	} else {
		pv.Confirmation = "CLOSE " + in.Symbol
	}

	recs, err := etoroledger.ReadFile(l.Store.LedgerPath)
	if err != nil {
		return pv, http.StatusInternalServerError, fmt.Errorf("cannot read the %s ledger: %v", l.Name, err)
	}
	pos := etoroledger.LastDemoPosition(recs, req.Book)
	pv.Position = pos
	var ruleSignal string
	for _, r := range recs { // sorted: the last rule record of the book is its newest
		if r.Book == req.Book && !r.IsUI() {
			pv.RefPrice, ruleSignal = r.Decision.Close, r.Decision.Signal
		}
	}
	pv.RuleSignal = ruleSignal
	reasons, warnings := []string{}, []string{}

	// 1. Halt file (merged into the policy like the executor does, so the
	// halted finding is the first the risk check reports).
	pol := risk.EtoroDemoPolicy()
	var halt *risk.HaltState
	if h, found, herr := l.Store.Halt(); herr != nil {
		reasons = append(reasons, "halt file invalid: "+herr.Error()+" (the executor refuses to run; fix or remove it by hand)")
	} else if found && h.Halted {
		pol = risk.ApplyHalt(pol, h)
		halt = &h
	}

	// The account and the live quote (venue reads; a failure is a 502).
	all, err := b.Positions(ctx)
	if err != nil {
		return pv, http.StatusBadGateway, fmt.Errorf("positions: %v", err)
	}
	acct, err := b.Account(ctx)
	if err != nil {
		return pv, http.StatusBadGateway, fmt.Errorf("account: %v", err)
	}
	pv.Account = &etoroledger.Account{Currency: acct.Currency, Cash: acct.Cash, Equity: acct.Equity, Exposure: tradeExposure(all)}
	mine := tradeOnInstrument(all, in.ID)
	q, err := b.Quote(ctx, bin)
	if err != nil {
		return pv, http.StatusBadGateway, fmt.Errorf("quote: %v", err)
	}
	if !(q.Bid > 0) || !(q.Ask > 0) {
		return pv, http.StatusBadGateway, fmt.Errorf("quote: eToro returned bid %v ask %v", q.Bid, q.Ask)
	}
	pv.Quote = &etoroledger.Quote{Bid: q.Bid, Ask: q.Ask, At: q.At.UTC().Format(time.RFC3339), SpreadBps: q.SpreadBps()}

	// 2. risk.Check with EtoroDemoPolicy, the executor's order and state.
	ro := risk.Order{Book: req.Book, RefPrice: pv.RefPrice, Leverage: req.Leverage}
	rs := risk.State{OrdersToday: etoroledger.LegsOn(recs, req.Book, today), Equity: acct.Equity, Exposure: pv.Account.Exposure,
		MarketClosed: !mktcal.IsOpen(now)}
	if req.Side == TradeOpen {
		ro.Side, ro.Price, ro.QtyBTC = "buy", q.Ask, req.AmountUSD/q.Ask
	} else {
		ro.Side, ro.Price, ro.QtyBTC = "sell", q.Bid, pos.Units
		rs.PositionBTC = pos.Units
	}
	pv.Units = ro.QtyBTC
	dec := risk.Check(pol, ro, rs)
	pv.Risk = &etoroledger.RiskCheck{PolicyVersion: pol.Version, Order: ro, State: rs, Allowed: dec.Allowed, Findings: dec.Findings, Halt: halt}
	for _, f := range dec.Findings {
		if f.Severity == risk.Block {
			reasons = append(reasons, f.Rule+": "+f.Message)
		} else {
			warnings = append(warnings, f.Rule+": "+f.Message)
		}
	}

	// 3. Eligibility: the ledger and the account must agree, as the executor
	// requires before it acts.
	if req.Side == TradeOpen {
		if pos.State == "long" {
			reasons = append(reasons, fmt.Sprintf("the ledger already holds position %s on %s (%.6f units): close it before opening again", pos.PositionID, in.Symbol, pos.Units))
		} else if len(mine) > 0 {
			reasons = append(reasons, fmt.Sprintf("the account holds %s on %s that the ledger does not: resolve by hand, nothing sent", tradeIDs(mine), in.Symbol))
		}
	} else {
		switch {
		case pos.State != "long":
			reasons = append(reasons, fmt.Sprintf("the ledger holds no position on %s to close", in.Symbol))
		case req.PositionID != pos.PositionID:
			reasons = append(reasons, fmt.Sprintf("position %s is not the ledger's position on %s (%s)", req.PositionID, in.Symbol, pos.PositionID))
		default:
			var held *broker.Position
			for i := range mine {
				if mine[i].ID == pos.PositionID {
					held = &mine[i]
				}
			}
			if held == nil {
				reasons = append(reasons, fmt.Sprintf("the account no longer holds position %s (closed outside the executor?): resolve by hand", pos.PositionID))
			} else if pos.Units > 0 && math.Abs(held.Units-pos.Units) > 1e-6*math.Max(1, pos.Units) {
				warnings = append(warnings, fmt.Sprintf("position %s holds %.6f units at eToro, the ledger says %.6f (the whole position is closed)", pos.PositionID, held.Units, pos.Units))
			}
		}
	}

	// 4. Cost preview (opens). 5. Cash (opens).
	if req.Side == TradeOpen {
		c, err := b.PreviewOpen(ctx, broker.OpenRequest{Instrument: bin, Side: broker.Long, Amount: req.AmountUSD, Leverage: 1, ClientRef: "preview"})
		if err != nil {
			reasons = append(reasons, "cost preview failed: "+err.Error())
		} else {
			pv.Costs = &etoroledger.Costs{Spread: c.Spread, Markup: c.Markup, TransactionFee: c.TransactionFee, OvernightFee: c.OvernightFee, WeekendFee: c.WeekendFee}
		}
		if acct.Cash < req.AmountUSD {
			reasons = append(reasons, fmt.Sprintf("available cash %.2f %s is below the %.0f USD amount", acct.Cash, acct.Currency, req.AmountUSD))
		}
	}

	// Against the rule: allowed, but the operator must know what happens next.
	switch {
	case req.Side == TradeOpen && ruleSignal == "flat":
		warnings = append(warnings, "the rule says flat: the executor closes this position at its next 09:35 New York run unless the ledger is halted")
	case req.Side == TradeClose && ruleSignal == "long":
		warnings = append(warnings, "the rule says long: the executor re-opens at its next 09:35 New York run unless the ledger is halted")
	}

	ref, err := etoroledger.UIClientRef(req.Book, today, req.kind())
	if err != nil {
		return pv, http.StatusInternalServerError, err
	}
	pv.ClientRef, pv.RequestID = ref, etoro.RequestIDFor(ref)
	pv.Reasons, pv.Warnings = reasons, warnings
	if len(reasons) == 0 {
		pv.Verdict = VerdictAllow
	}
	return pv, http.StatusOK, nil
}

// TradePlaceRequest is the body of POST /api/ui/etoro/trades/place.
type TradePlaceRequest struct {
	Token string `json:"token"`
	// Confirm must equal the preview's confirmation phrase ("PLACE NSDQ100").
	Confirm string `json:"confirm"`
	Reason  string `json:"reason"`
	By      string `json:"by"`
}

// TradePlaceResponse is the outcome of place. A broker failure answers 502
// with the same shape (Status, Error) so the page can show what eToro said.
type TradePlaceResponse struct {
	Status         string               `json:"status"`
	Book           string               `json:"book"`
	Side           string               `json:"side"`
	ClientRef      string               `json:"client_ref"`
	RequestID      string               `json:"request_id"`
	OrderID        string               `json:"order_id,omitempty"`
	PositionIDs    []string             `json:"position_ids"`
	AvgPrice       float64              `json:"avg_price"`
	Units          float64              `json:"units"`
	AmountUSD      float64              `json:"amount_usd"`
	FillVsMidBps   float64              `json:"fill_vs_mid_bps"`
	FillVsCloseBps float64              `json:"fill_vs_close_bps"`
	PositionAfter  etoroledger.Position `json:"position_after"`
	LedgerRecorded bool                 `json:"ledger_recorded"`
	LedgerError    string               `json:"ledger_error,omitempty"`
	AuditPath      string               `json:"audit_path"`
	Audit          audit.Entry          `json:"audit"`
	AuditError     string               `json:"audit_error,omitempty"`
	Error          string               `json:"error,omitempty"`
}

func (s *Server) tradePlace(w http.ResponseWriter, r *http.Request) {
	l, ok := s.pickEtoro(w, r)
	if !ok {
		return
	}
	if enabled, why := s.tradingState(l); !enabled {
		writeErr(w, http.StatusForbidden, why)
		return
	}
	ap := auditPath(l)
	base := s.baseEntry(r, "trade.place")
	base.Ledger = l.Name
	var req TradePlaceRequest
	if !s.admit(w, r, base, []string{ap}, &req) {
		return
	}
	base.By, base.Reason = clip(strings.TrimSpace(req.By), 64), clip(strings.TrimSpace(req.Reason), maxReasonLen)
	note := func(outcome, msg string) {
		e := base
		e.Outcome, e.Error = outcome, msg
		s.noteAll([]string{ap}, e)
	}
	refuse := func(status int, msg string) {
		note(audit.Refused, msg)
		writeErr(w, status, msg)
	}
	c, err := s.Trading.verify(req.Token, s.OperatorToken)
	if err != nil {
		note(audit.Denied, err.Error())
		writeErr(w, http.StatusUnauthorized, err.Error())
		return
	}
	base.Ref = c.ClientRef
	if c.Ledger != l.Name {
		refuse(http.StatusBadRequest, "trade token was prepared for ledger "+c.Ledger)
		return
	}
	now := s.Now().UTC()
	if now.Unix() >= c.Exp {
		refuse(http.StatusGone, fmt.Sprintf("trade token expired at %s: prepare again", time.Unix(c.Exp, 0).UTC().Format(time.RFC3339)))
		return
	}
	if err := validateControl(ControlRequest{Reason: req.Reason, By: req.By, Confirm: req.Confirm}, c.Confirmation, "the confirmation phrase"); err != nil {
		refuse(http.StatusBadRequest, err.Error())
		return
	}
	in, _ := tradeInstrument(c.Book)
	bin := broker.Instrument{Venue: s.Trading.Broker.Venue(), Symbol: in.Symbol, ID: in.ID}
	today := now.In(mktcal.NewYork).Format(dateFmt)
	base.Detail = tradeDetail(c.Side, c.Book, c.AmountUSD, c.PositionID, c.Units)

	// One control at a time, and never while the executor runs.
	s.controlsMu.Lock()
	defer s.controlsMu.Unlock()
	if !s.Trading.consume(c.ID, c.Exp, now.Unix()) {
		refuse(http.StatusConflict, "trade token already used: prepare again")
		return
	}
	blocked := func(msg string) { refuse(http.StatusUnprocessableEntity, msg+"; nothing sent") }
	h, found, herr := l.Store.Halt()
	switch {
	case herr != nil:
		blocked("halt file invalid: " + herr.Error())
		return
	case found && h.Halted:
		blocked(fmt.Sprintf("trading is halted: %s (halt file, by %s at %s)", h.Reason, h.By, h.At))
		return
	case !mktcal.IsOpen(now):
		blocked("the NYSE cash session is closed")
		return
	}
	lock, err := lockLedger(l.Store.LedgerPath + ".lock")
	if err != nil {
		refuse(http.StatusConflict, err.Error())
		return
	}
	defer lock.Close()
	led, err := etoroledger.Open(l.Store.LedgerPath)
	if err != nil {
		note(audit.Failed, err.Error())
		writeErr(w, http.StatusInternalServerError, "cannot read the "+l.Name+" ledger: "+err.Error())
		return
	}
	pos := led.LastDemoPosition(c.Book)
	switch {
	case c.Side == TradeOpen && pos.State != "flat":
		blocked(fmt.Sprintf("the ledger changed since the preview: it holds position %s on %s", pos.PositionID, in.Symbol))
		return
	case c.Side == TradeClose && (pos.State != "long" || pos.PositionID != c.PositionID):
		blocked(fmt.Sprintf("the ledger changed since the preview: it no longer holds position %s on %s", c.PositionID, in.Symbol))
		return
	}
	ctx := r.Context()
	all, err := s.Trading.Broker.Positions(ctx)
	if err != nil {
		note(audit.Failed, "positions: "+err.Error())
		writeErr(w, http.StatusBadGateway, "positions: "+err.Error()+"; nothing sent")
		return
	}
	mine := tradeOnInstrument(all, in.ID)
	var held *broker.Position
	for i := range mine {
		if mine[i].ID == pos.PositionID {
			held = &mine[i]
		}
	}
	switch {
	case c.Side == TradeOpen && len(mine) > 0:
		blocked(fmt.Sprintf("the account holds %s on %s that the ledger does not: resolve by hand", tradeIDs(mine), in.Symbol))
		return
	case c.Side == TradeClose && held == nil:
		blocked(fmt.Sprintf("the account no longer holds position %s", c.PositionID))
		return
	}
	var account *etoroledger.Account
	if acct, err := s.Trading.Broker.Account(ctx); err == nil {
		account = &etoroledger.Account{Currency: acct.Currency, Cash: acct.Cash, Equity: acct.Equity, Exposure: tradeExposure(all)}
	}

	// The intent first (fsync), then the "requested" audit line, then the order.
	base.ID = newID()
	intent, existed, err := etoroledger.RecordIntent(etoroledger.IntentDir(l.Store.LedgerPath), etoroledger.Intent{
		ClientRef: c.ClientRef, RequestID: c.RequestID, Kind: kindOf(c.Side), Book: c.Book, Instrument: in.ID,
		BarDate: today, FillDate: today, Amount: c.AmountUSD, PositionID: c.PositionID, IntentAt: now,
	})
	if err != nil {
		note(audit.Failed, "intent: "+err.Error())
		writeErr(w, http.StatusInternalServerError, "cannot write the intent ("+err.Error()+"); nothing sent")
		return
	}
	if existed {
		refuse(http.StatusConflict, fmt.Sprintf("an intent for %s already exists (sent %s); nothing sent", c.ClientRef, intent.IntentAt.Format(time.RFC3339)))
		return
	}
	req1 := base
	req1.Outcome = audit.Requested
	if err := audit.Append(ap, req1); err != nil {
		s.Log.Printf("audit %s: %v", ap, err)
		writeErr(w, http.StatusInternalServerError, fmt.Sprintf("cannot write the audit log (%v); nothing sent. The intent %s is on disk and reconcile will list it: remove it by hand", err, c.ClientRef))
		return
	}

	var bo broker.Order
	var oerr error
	if c.Side == TradeOpen {
		s.Log.Printf("trade.place ledger=%s open %s %.2f USD x1 ref=%s by=%q", l.Name, in.Symbol, c.AmountUSD, c.ClientRef, base.By)
		bo, oerr = s.Trading.Broker.Open(ctx, broker.OpenRequest{Instrument: bin, Side: broker.Long, Amount: c.AmountUSD, Leverage: 1, ClientRef: c.ClientRef, IntentAt: intent.IntentAt})
	} else {
		s.Log.Printf("trade.place ledger=%s close %s position %s ref=%s by=%q", l.Name, in.Symbol, c.PositionID, c.ClientRef, base.By)
		bo, oerr = s.Trading.Broker.Close(ctx, broker.CloseRequest{Instrument: bin, PositionID: c.PositionID, ClientRef: c.ClientRef})
	}
	finished := s.Now().UTC()
	o := etoroledger.Order{Kind: kindOf(c.Side), ClientRef: c.ClientRef, RequestID: c.RequestID, IntentAt: intent.IntentAt, Started: now, Finished: finished}
	tradeFill(&o, bo)
	resp := TradePlaceResponse{Status: o.Status, Book: c.Book, Side: c.Side, ClientRef: c.ClientRef, RequestID: c.RequestID, OrderID: o.OrderID,
		PositionIDs: o.PositionIDs, AvgPrice: o.AvgPrice, Units: o.Units, AmountUSD: o.Amount, PositionAfter: pos, AuditPath: ap}
	if resp.PositionIDs == nil {
		resp.PositionIDs = []string{}
	}
	fail := func(msg string) {
		msg += fmt.Sprintf(" (the intent %s stays on disk; etoro-reconcile and the data-health page report it until it is resolved by hand)", c.ClientRef)
		e := base
		e.Outcome, e.Error = audit.Failed, msg
		if err := audit.Append(ap, e); err != nil {
			s.Log.Printf("audit %s: %v", ap, err)
			resp.AuditError = err.Error()
		}
		resp.Audit, resp.Error = e, msg
		writeJSON(w, http.StatusBadGateway, resp)
	}
	switch {
	case oerr != nil:
		fail(fmt.Sprintf("%s %s: %v", c.Side, c.ClientRef, oerr))
		return
	case bo.Status != broker.StatusExecuted:
		fail(fmt.Sprintf("%s %s is %s at eToro after the adapter's wait", c.Side, c.ClientRef, bo.Status))
		return
	case c.Side == TradeOpen && len(bo.PositionIDs) != 1:
		fail(fmt.Sprintf("open %s executed with %d positions %v: resolve by hand", c.ClientRef, len(bo.PositionIDs), bo.PositionIDs))
		return
	}

	// The ledger line (the executor's shape, source ui).
	mid := (c.Bid + c.Ask) / 2
	d := &etoroledger.Demo{Env: "demo", Action: kindOf(c.Side), PositionBefore: pos, Account: account,
		Quote: &etoroledger.Quote{Bid: c.Bid, Ask: c.Ask, At: time.Unix(c.Iat, 0).UTC().Format(time.RFC3339), SpreadBps: broker.Quote{Bid: c.Bid, Ask: c.Ask}.SpreadBps()},
		Order: &o,
		Notes: []string{"assisted trade from the console (ui-api); the executor brings the book back to the rule's target at its next run unless the ledger is halted"},
	}
	ro := risk.Order{Book: c.Book, RefPrice: c.RefPrice, Leverage: 1, QtyBTC: c.Units}
	rsState := risk.State{OrdersToday: etoroledger.LegsOn(led.Records(), c.Book, today), MarketClosed: false}
	if account != nil {
		rsState.Equity, rsState.Exposure = account.Equity, account.Exposure
	}
	if c.Side == TradeOpen {
		ro.Side, ro.Price = "buy", c.Ask
		d.Target = "long"
		d.PositionAfter = etoroledger.Position{State: "long", PositionID: bo.PositionIDs[0], Units: bo.Units, Amount: bo.Amount,
			OpenRate: bo.AvgPrice, OpenedAt: bo.At.UTC().Format(time.RFC3339)}
		o.FillVsMidBps, o.FillVsCloseBps = tradeCostBps("buy", bo.AvgPrice, mid), tradeCostBps("buy", bo.AvgPrice, c.RefPrice)
	} else {
		ro.Side, ro.Price = "sell", c.Bid
		rsState.PositionBTC = pos.Units
		d.Target = "flat"
		d.PositionAfter = etoroledger.Flat()
		o.FillVsMidBps, o.FillVsCloseBps = tradeCostBps("sell", bo.AvgPrice, mid), tradeCostBps("sell", bo.AvgPrice, c.RefPrice)
		if held != nil {
			d.FinancingToDate = held.Fees
		}
	}
	// The check as it was passed at prepare (same policy, state as of now).
	d.Risk = &etoroledger.RiskCheck{PolicyVersion: risk.EtoroDemoPolicy().Version, Order: ro, State: rsState, Allowed: true}
	var ruleSignal string
	var ruleSMA float64
	for _, rr := range led.Records() {
		if rr.Book == c.Book && !rr.IsUI() {
			ruleSignal, ruleSMA = rr.Decision.Signal, rr.Decision.SMA
		}
	}
	action := "buy"
	if c.Side == TradeClose {
		action = "sell"
	}
	rec := etoroledger.Record{
		Schema: etoroledger.Schema, RecordedAt: finished.Format(time.RFC3339), CodeVersion: "ui-api " + s.Version,
		Mode: etoroledger.ModeDemo, Venue: etorobroker.Venue, Book: c.Book, Instrument: in,
		Decision: etoroledger.Decision{BarDate: today, FillDate: today, Close: c.RefPrice, SMA: ruleSMA, Signal: etoroledger.SignalManual, PrevSignal: ruleSignal, Action: action},
		Demo:     d, Source: etoroledger.SourceUI, Operator: &etoroledger.Operator{By: base.By, Reason: base.Reason},
	}
	resp.FillVsMidBps, resp.FillVsCloseBps, resp.PositionAfter = o.FillVsMidBps, o.FillVsCloseBps, d.PositionAfter
	done := base
	done.Outcome = audit.Done
	done.Detail += fmt.Sprintf(" -> %s: order %s positions %v avg %.2f units %.6f (vs mid %+.1f bps, vs close %+.1f bps)",
		o.Status, o.OrderID, o.PositionIDs, o.AvgPrice, o.Units, o.FillVsMidBps, o.FillVsCloseBps)
	if err := led.Append(rec); err != nil {
		resp.LedgerError = err.Error()
		done.Error = "order executed but the ledger line could not be written: " + err.Error() + " (the intent is on disk; reconcile reports it)"
		s.Log.Printf("trade.place ledger=%s %s: %s", l.Name, c.ClientRef, done.Error)
	} else {
		resp.LedgerRecorded = true
	}
	if err := audit.Append(ap, done); err != nil {
		s.Log.Printf("audit %s: %v", ap, err)
		resp.AuditError = err.Error()
	}
	resp.Audit = done
	s.Log.Printf("trade.place ledger=%s %s %s %s: order %s positions %v avg %.2f (recorded=%v) id=%s", l.Name, c.Side, in.Symbol, o.Status, o.OrderID, o.PositionIDs, o.AvgPrice, resp.LedgerRecorded, done.ID)
	writeJSON(w, http.StatusOK, resp)
}

func kindOf(side string) string {
	if side == TradeOpen {
		return etoroledger.ActionOpen
	}
	return etoroledger.ActionClose
}

// lockLedger takes the executor's exclusive, non-blocking flock on the
// ledger's lock file so a console trade never interleaves with a run.
func lockLedger(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("the executor holds the ledger lock %s (a run is in progress): try again when it finishes; nothing sent", path)
	}
	return f, nil
}

func tradeFill(o *etoroledger.Order, bo broker.Order) {
	o.OrderID, o.Status, o.PositionIDs = bo.OrderID, string(bo.Status), bo.PositionIDs
	o.AvgPrice, o.Units, o.Amount, o.Fees, o.SpreadCost = bo.AvgPrice, bo.Units, bo.Amount, bo.Fees, bo.SpreadCost
	o.Error, o.ErrorCode = bo.Error, bo.ErrorCode
	if o.Status == "" {
		o.Status = string(broker.StatusUnknown)
	}
}

// tradeCostBps is the fill's cost against ref in bps (positive = paid), the
// executor's convention.
func tradeCostBps(side string, avg, ref float64) float64 {
	if avg <= 0 || ref <= 0 {
		return 0
	}
	if side == "buy" {
		return (avg - ref) / ref * 1e4
	}
	return (ref - avg) / ref * 1e4
}

func tradeOnInstrument(ps []broker.Position, id int64) []broker.Position {
	var out []broker.Position
	for _, p := range ps {
		if p.Instrument.ID == id {
			out = append(out, p)
		}
	}
	return out
}

// tradeExposure is the account's total open notional (account currency).
func tradeExposure(ps []broker.Position) float64 {
	t := 0.0
	for _, p := range ps {
		switch {
		case p.Exposure > 0:
			t += p.Exposure
		case p.Leverage > 1:
			t += p.Amount * float64(p.Leverage)
		default:
			t += p.Amount
		}
	}
	return t
}

func tradeIDs(ps []broker.Position) string {
	s := make([]string, len(ps))
	for i, p := range ps {
		s[i] = p.ID
	}
	return "position(s) " + strings.Join(s, ", ")
}
