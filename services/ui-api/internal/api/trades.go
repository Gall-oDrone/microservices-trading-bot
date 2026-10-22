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
//
// Protection (2026-10-19, docs/etoro/ETORO-ASSISTED-TRADE-PROTECTION-PLAN-2026-10-19.md
// phase B): an open carries a stop loss (required by
// risk.EtoroDemoProtectedPolicy) and an optional take profit, chosen by a
// risk-aversion profile (ATR-based, shared/pkg/stops) or typed as custom
// distances, sent with the order in the same request; a third side,
// "protect", sets or moves the stop and target of the ledger's held
// position through the broker's ProtectionEditor (eToro PATCH position)
// with the same prepare -> place flow, phrase "PROTECT NSDQ100", intent and
// ledger line (action protect, not a leg). A request that names no stop
// gets the default profile and says so in a warning. protection.go holds
// the recommendation endpoint and the shared helpers.
//
// Cancel (2026-10-21): a third POST, /trades/cancel, lets the operator
// discard a prepared trade instead of waiting for the token to expire. It
// burns the token in the same used set place consults (so a cancel and a
// place of the same token cannot both succeed), touches neither the broker
// nor the ledger, and writes a trade.cancel audit line with the preview's
// client_ref so the decision not to trade is on record next to the
// decision to prepare it.
//
// Real-account canary (2026-10-22, docs/etoro/ETORO-REAL-CANARY-PLAN-2026-10-21.md):
// a SEPARATE ui-api process (its own port, env file, operator token, ledger
// directory and systemd unit) may trade the real account for the
// measurement-only canary when it was started with -etoro-real-canary and
// the six-part acknowledgement of shared/pkg/etoro/realack.go held at
// start. Nothing else changes in this file's flow: the same prepare ->
// place -> ledger line, with the canary policy (risk.EtoroRealCanaryPolicy:
// x20, 50-60 USD invested, one position, a stop just inside the venue's
// cap), the amount bounds and the leverage choices taken from that policy
// instead of the x1 constants, confirmation phrases carrying the word REAL
// ("PLACE REAL NSDQ100"), client refs prefixed ui-real-, ledger lines with
// mode "real" and demo.env "real", and TradingInfo.Environment "real" so
// the web app paints the account red. The demo process is unchanged:
// NewEtoroTrading still refuses anything but demo. -canary-rehearsal runs
// the canary policy on the demo account (its own ledger, REAL-less
// phrases) so the whole flow is exercised before the real key exists.

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
	"bitso-trading-platform/shared/pkg/stops"
	"bitso-trading-platform/ui-api/internal/audit"
	"bitso-trading-platform/ui-api/internal/etorolive"
)

const (
	// TradeTokenTTL is how long a prepared trade may be placed.
	TradeTokenTTL = 120 * time.Second
	// TradeMinAmount is eToro's minimum exposure for the index CFDs (P1
	// evidence; the executor's floor); TradeMaxAmount EtoroDemoPolicy's
	// order notional cap; TradeDefaultAmount the forward-test size. They
	// are the x1 form bounds of a policy that carries none of its own
	// (the demo policies); the canary policy carries its own (tradeBounds).
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
	"/api/ui/etoro/trades/prepare":   true,
	"/api/ui/etoro/trades/place":     true,
	"/api/ui/etoro/trades/recommend": true,
	"/api/ui/etoro/trades/cancel":    true,
}

// RealCanaryLedgerName is the only ledger name the canary process serves
// (plan §3.2: also the halt phrase and the ?ledger= value).
const RealCanaryLedgerName = "etoro-real-canary"

// EtoroTrading is the Server's assisted-trade dependency: the broker, the
// policy and environment it trades under, the per-process token secret and
// the set of tokens already used.
type EtoroTrading struct {
	Broker broker.Broker
	// Policy is what prepare and place check an order with and where the
	// form bounds come from: EtoroDemoProtectedPolicy for the demo console
	// (the zero value means it), EtoroRealCanaryPolicy for the real canary,
	// EtoroRealCanaryRehearsalPolicy under -canary-rehearsal.
	Policy risk.Policy
	// Env is the account the broker trades: demo (default) or real.
	Env etoro.Environment
	// Rehearsal marks the canary policy on the demo account.
	Rehearsal bool
	// Ack is the acknowledgement that let a real process start (nil on demo).
	Ack    *etoro.RealAck
	secret []byte
	mu     sync.Mutex
	used   map[string]int64 // token id -> expiry (unix s), pruned as they expire
}

// NewEtoroTrading builds the demo broker for the assisted trades. It refuses
// any environment but demo: nothing for the real account is reachable from
// the demo console (the canary is NewEtoroRealCanary, a different process).
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

// NewEtoroCanaryRehearsal builds the DEMO broker with the canary policy
// (plan §7's rehearsal): x20, 50-60 USD, the canary's stop bounds, on the
// demo account and a ledger of its own. Demo only, like NewEtoroTrading.
func NewEtoroCanaryRehearsal(cfg etoro.Config) (*EtoroTrading, error) {
	if cfg.Env != etoro.EnvDemo {
		return nil, fmt.Errorf("canary rehearsal: ETORO_ENV must be demo (got %q); the rehearsal runs on the demo account", cfg.Env)
	}
	c, err := etoro.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("canary rehearsal: %w", err)
	}
	return newEtoroTradingWith(etorobroker.New(c), risk.EtoroRealCanaryRehearsalPolicy(), etoro.EnvDemo, true, nil)
}

// NewEtoroRealCanary builds the REAL broker for the measurement canary. It
// is the only constructor that accepts ETORO_ENV=real, and only after
// etoro.CheckRealAck passed every one of the six conditions (the caller
// fills chk from its flags and files; PolicyVersion is forced to the canary
// policy's here so the ack is checked against what will actually run).
func NewEtoroRealCanary(cfg etoro.Config, ack etoro.RealAck, chk etoro.RealAckCheck) (*EtoroTrading, error) {
	pol := risk.EtoroRealCanaryPolicy()
	chk.Env, chk.PublicKey, chk.PolicyVersion = cfg.Env, cfg.PublicKey, pol.Version
	if err := etoro.CheckRealAck(ack, chk); err != nil {
		return nil, err
	}
	if cfg.Env != etoro.EnvReal {
		return nil, fmt.Errorf("%w (got %q)", etoro.ErrRealAckEnv, cfg.Env)
	}
	c, err := etoro.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("real canary: %w", err)
	}
	a := ack
	return newEtoroTradingWith(etorobroker.New(c), pol, etoro.EnvReal, false, &a)
}

// newEtoroTrading wraps a broker (tests pass a fake; it must still be an
// eToro venue) with the demo console's policy.
func newEtoroTrading(b broker.Broker) (*EtoroTrading, error) {
	return newEtoroTradingWith(b, risk.EtoroDemoProtectedPolicy(), etoro.EnvDemo, false, nil)
}

func newEtoroTradingWith(b broker.Broker, pol risk.Policy, env etoro.Environment, rehearsal bool, ack *etoro.RealAck) (*EtoroTrading, error) {
	if b == nil || b.Venue() != etorobroker.Venue {
		return nil, errors.New("assisted trading: the broker must be the eToro adapter")
	}
	if env == etoro.EnvReal && (ack == nil || !strings.HasPrefix(pol.Version, etoro.RealAckPolicyPrefix)) {
		return nil, errors.New("assisted trading: a real broker needs the acknowledgement and the canary policy")
	}
	if err := pol.Validate(); err != nil {
		return nil, fmt.Errorf("assisted trading: policy: %w", err)
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("assisted trading: token secret: %w", err)
	}
	return &EtoroTrading{Broker: b, Policy: pol, Env: env, Rehearsal: rehearsal, Ack: ack, secret: secret, used: map[string]int64{}}, nil
}

// Real reports whether the broker trades the REAL account.
func (t *EtoroTrading) Real() bool { return t != nil && t.Env == etoro.EnvReal }

// Environment is "demo" or "real" (demo when unset).
func (t *EtoroTrading) Environment() string {
	if t.Real() {
		return string(etoro.EnvReal)
	}
	return string(etoro.EnvDemo)
}

// policy is the policy the trades are checked with (the demo console's
// when none was set, so older constructions behave as before).
func (t *EtoroTrading) policy() risk.Policy {
	if t == nil || t.Policy.Version == "" {
		return risk.EtoroDemoProtectedPolicy()
	}
	return t.Policy
}

// tradeBounds are the form's bounds for a book: the amount the operator
// invests (min, max, default) and the leverage values offered. A policy
// book that carries its own sizing (the canary) defines them; otherwise the
// x1 constants apply, as before 2026-10-22.
type tradeBounds struct {
	Min, Max, Default float64
	Leverages         []int
}

func (b tradeBounds) allowsLeverage(lev int) bool {
	for _, x := range b.Leverages {
		if x == lev {
			return true
		}
	}
	return false
}

// defaultLeverage is the leverage the form pre-selects (the only one on
// the canary; x1 on demo).
func (b tradeBounds) defaultLeverage() int {
	if len(b.Leverages) == 1 {
		return b.Leverages[0]
	}
	return 1
}

// boundsFor derives the bounds of book from the policy.
func boundsFor(pol risk.Policy, book string) tradeBounds {
	l := pol.For(book)
	b := tradeBounds{Min: TradeMinAmount, Max: TradeMaxAmount, Default: TradeDefaultAmount, Leverages: []int{1}}
	if l.MaxInvestedUSD > 0 {
		b.Max = l.MaxInvestedUSD
		b.Min, b.Default = l.MinInvestedUSD, l.DefaultInvestedUSD
		if b.Min <= 0 {
			b.Min = b.Max
		}
		if b.Default <= 0 {
			b.Default = b.Min
		}
	}
	if len(l.Leverages) > 0 {
		b.Leverages = append([]int(nil), l.Leverages...)
	}
	return b
}

// tradeBook is the book the form's bounds are reported for in TradingInfo:
// the first book of the policy (nsdq100 on every policy so far).
func tradeBook(pol risk.Policy) string {
	if names := pol.BookNames(); len(names) > 0 {
		return names[0]
	}
	return "nsdq100"
}

// TradingInfo is ControlsInfo.Trading: whether the assisted trades are
// available on a ledger, and how the page builds its form.
type TradingInfo struct {
	Enabled        bool   `json:"enabled"`
	DisabledReason string `json:"disabled_reason,omitempty"`
	// TokenTTLSeconds and the amount bounds let the page build its form.
	TokenTTLSeconds int     `json:"token_ttl_s"`
	MinAmountUSD    float64 `json:"min_amount_usd"`
	MaxAmountUSD    float64 `json:"max_amount_usd"`
	DefaultAmount   float64 `json:"default_amount_usd"`
	// Environment (2026-10-22) is "demo" or "real"; LeverageOptions the
	// leverage values the form offers and DefaultLeverage the one it
	// pre-selects (x1 on demo; x20 only on the canary); PolicyVersion the
	// policy the trades are checked with; Books the books it allows;
	// Rehearsal marks the canary policy on the demo account.
	Environment     string   `json:"environment"`
	LeverageOptions []int    `json:"leverage_options"`
	DefaultLeverage int      `json:"default_leverage"`
	PolicyVersion   string   `json:"policy_version"`
	Books           []string `json:"books"`
	Rehearsal       bool     `json:"rehearsal,omitempty"`
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
	pol := s.Trading.policy()
	b := boundsFor(pol, tradeBook(pol))
	info := TradingInfo{Enabled: enabled, DisabledReason: why, TokenTTLSeconds: int(TradeTokenTTL / time.Second),
		MinAmountUSD: b.Min, MaxAmountUSD: b.Max, DefaultAmount: b.Default,
		Environment: l.environment(), LeverageOptions: b.Leverages, DefaultLeverage: b.defaultLeverage(),
		PolicyVersion: pol.Version, Books: pol.BookNames()}
	if s.Trading != nil {
		info.Rehearsal = s.Trading.Rehearsal
	}
	return info
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
	Side string `json:"side"` // TradeOpen | TradeClose | TradeProtect
	// AmountUSD is what the operator invests in an open (the exposure at
	// x1; exposure / leverage on the canary); PositionID the ledger's
	// position to close or protect.
	AmountUSD  float64 `json:"amount_usd,omitempty"`
	PositionID string  `json:"position_id,omitempty"`
	// Leverage must be one of TradingInfo.LeverageOptions (x1 on demo; x20
	// only on the canary).
	Leverage int `json:"leverage"`
	// Protection of an open or a protect (2026-10-19). Profile is a
	// stops.Profile ("" = the default, moderate); "custom" takes
	// StopLossPct (required) and TakeProfitPct (0 = none) as fractions of
	// the entry. A named profile ignores the percentages.
	Profile       string  `json:"profile,omitempty"`
	StopLossPct   float64 `json:"stop_loss_pct,omitempty"`
	TakeProfitPct float64 `json:"take_profit_pct,omitempty"`
}

func (r TradePrepareRequest) validate(pol risk.Policy) error {
	if _, ok := tradeInstrument(r.Book); !ok {
		return fmt.Errorf("book: %q is not an eToro book of this executor (nsdq100, spx500)", r.Book)
	}
	b := boundsFor(pol, r.Book)
	if !b.allowsLeverage(r.Leverage) {
		if len(b.Leverages) == 1 {
			return fmt.Errorf("leverage: must be %d (the policy allows x%d only)", b.Leverages[0], b.Leverages[0])
		}
		return fmt.Errorf("leverage: must be one of %v (the policy)", b.Leverages)
	}
	positionID := func() error {
		if r.PositionID == "" || len(r.PositionID) > 32 || strings.Trim(r.PositionID, "0123456789") != "" {
			return errors.New("position_id: the numeric id of the ledger's position")
		}
		return nil
	}
	protection := func() error {
		p, err := stops.ParseProfile(r.Profile)
		if err != nil {
			return errors.New("profile: " + err.Error())
		}
		if math.IsNaN(r.StopLossPct) || math.IsInf(r.StopLossPct, 0) || r.StopLossPct < 0 || r.StopLossPct >= 1 {
			return errors.New("stop_loss_pct: a fraction of the entry in [0, 1)")
		}
		if math.IsNaN(r.TakeProfitPct) || math.IsInf(r.TakeProfitPct, 0) || r.TakeProfitPct < 0 || r.TakeProfitPct > 10 {
			return errors.New("take_profit_pct: a fraction of the entry in [0, 10]")
		}
		if p == stops.Custom && r.StopLossPct == 0 {
			return errors.New("stop_loss_pct: required with the custom profile (the policy requires a stop)")
		}
		if p != stops.Custom && (r.StopLossPct != 0 || r.TakeProfitPct != 0) {
			return fmt.Errorf("stop_loss_pct / take_profit_pct: only with profile \"custom\" (got profile %q)", p)
		}
		return nil
	}
	switch r.Side {
	case TradeOpen:
		if r.PositionID != "" {
			return errors.New("position_id: not for an open")
		}
		if math.IsNaN(r.AmountUSD) || r.AmountUSD < b.Min || r.AmountUSD > b.Max {
			return fmt.Errorf("amount_usd: must be between %.0f and %.0f USD", b.Min, b.Max)
		}
		return protection()
	case TradeClose:
		if r.AmountUSD != 0 {
			return errors.New("amount_usd: not for a close (the whole position is closed)")
		}
		if r.Profile != "" || r.StopLossPct != 0 || r.TakeProfitPct != 0 {
			return errors.New("profile / stop_loss_pct / take_profit_pct: not for a close")
		}
		return positionID()
	case TradeProtect:
		if r.AmountUSD != 0 {
			return errors.New("amount_usd: not for a protect (the held position is sized already)")
		}
		if err := positionID(); err != nil {
			return err
		}
		return protection()
	default:
		return errors.New(`side: "open", "close" or "protect"`)
	}
}

func (r TradePrepareRequest) kind() string { return kindOf(r.Side) }

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
	// Leverage and ExposureUSD (2026-10-22): the leverage the order carries
	// and units x ask, the venue-side size (= AmountUSD at x1).
	// Environment is "demo" or "real"; MaxLossUSD the most the position
	// can lose at its stop (opens; 0 without a stop).
	Leverage    int     `json:"leverage"`
	ExposureUSD float64 `json:"exposure_usd"`
	Environment string  `json:"environment"`
	MaxLossUSD  float64 `json:"max_loss_usd,omitempty"`
	// RefPrice is the last close the rule recorded (the price-deviation
	// guard's reference and fill_vs_close's).
	RefPrice     float64 `json:"ref_price"`
	Confirmation string  `json:"confirmation"`
	ClientRef    string  `json:"client_ref"`
	RequestID    string  `json:"request_id"`
	PreparedAt   string  `json:"prepared_at"`
	// Protection is the stop and target the trade will carry (opens and
	// protects; nil for a close). 2026-10-19.
	Protection *TradeProtection `json:"protection,omitempty"`
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
	// The protection the preview showed, bound so place sends exactly it.
	SLRate   float64 `json:"sl_rate,omitempty"`
	TPRate   float64 `json:"tp_rate,omitempty"`
	Profile  string  `json:"profile,omitempty"`
	ATR      float64 `json:"atr,omitempty"`
	ATRPct   float64 `json:"atr_pct,omitempty"`
	SLBefore float64 `json:"sl_before,omitempty"`
	TPBefore float64 `json:"tp_before,omitempty"`
	OpenRate float64 `json:"open_rate,omitempty"`
	// Leverage of an open (0 on older tokens = x1); Env "real" binds a
	// canary token to the real process (never set on demo).
	Leverage int    `json:"leverage,omitempty"`
	Env      string `json:"env,omitempty"`
	Iat      int64  `json:"iat"`
	Exp      int64  `json:"exp"`
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
	if err := req.validate(s.Trading.policy()); err != nil {
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
		if pv.Side == TradeOpen {
			c.Leverage = pv.Leverage
		}
		if s.Trading.Real() {
			c.Env = string(etoro.EnvReal)
		}
		if p := pv.Protection; p != nil {
			c.SLRate, c.TPRate, c.Profile = p.Levels.SLRate, p.Levels.TPRate, p.Profile
			if p.Volatility != nil {
				c.ATR, c.ATRPct = p.Volatility.ATR, p.Volatility.ATRPct
			}
			if p.Before != nil {
				c.SLBefore, c.TPBefore = p.Before.StopLossRate, p.Before.TakeProfitRate
			}
			if pv.Side == TradeProtect {
				c.OpenRate = p.Entry
			}
		}
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
	e.Detail = tradeDetail(pv.Side, pv.Book, pv.AmountUSD, pv.PositionID, pv.Units, pv.Leverage, s.Trading.Real())
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

// tradeDetail is the trade in words for the audit line. The leverage is
// named on an open ("x1", "x20"); a real-account trade says REAL first.
func tradeDetail(side, book string, amount float64, positionID string, units float64, leverage int, real bool) string {
	if leverage < 1 {
		leverage = 1
	}
	prefix := ""
	if real {
		prefix = "REAL "
	}
	switch side {
	case TradeOpen:
		return fmt.Sprintf("%sopen %s %.0f USD x%d (%.6f units)", prefix, book, amount, leverage, units)
	case TradeProtect:
		return fmt.Sprintf("%sprotect %s position %s (%.6f units)", prefix, book, positionID, units)
	}
	return fmt.Sprintf("%sclose %s position %s (%.6f units)", prefix, book, positionID, units)
}

// preparePreview runs the checks. A local or venue failure is returned as
// an error with its HTTP status; a policy or eligibility failure is a block
// verdict with its reasons.
func (s *Server) preparePreview(ctx context.Context, l Ledger, req TradePrepareRequest) (TradePreview, int, error) {
	b := s.Trading.Broker
	real := s.Trading.Real()
	now := s.Now().UTC()
	today := now.In(mktcal.NewYork).Format(dateFmt)
	in, _ := tradeInstrument(req.Book)
	bin := broker.Instrument{Venue: b.Venue(), Symbol: in.Symbol, ID: in.ID}
	lev := req.Leverage
	if lev < 1 {
		lev = 1
	}
	pv := TradePreview{Verdict: VerdictBlock, Reasons: []string{}, Warnings: []string{}, Ledger: l.Name, Book: req.Book, Side: req.Side,
		Instrument: in, AmountUSD: req.AmountUSD, PositionID: req.PositionID, PreparedAt: now.Format(time.RFC3339),
		Leverage: lev, Environment: s.Trading.Environment()}
	pv.Confirmation = confirmationPhrase(req.Side, in.Symbol, real)

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
	// halted finding is the first the risk check reports). The policy is
	// the trading dependency's: the protected demo policy (an open must
	// carry a stop, 2026-10-19) or the canary's.
	pol := s.Trading.policy()
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

	// Protection levels (opens and protects), before the risk check so the
	// check sees the stop. A request that names nothing gets the default
	// profile; an infeasible level is a reason.
	profile, _ := stops.ParseProfile(req.Profile)
	var pc protectionCtx
	if req.Side == TradeOpen || req.Side == TradeProtect {
		plev := lev
		if req.Side == TradeProtect && pos.State == "long" {
			if held := heldPosition(all, in.ID, pos.PositionID); held != nil && held.Leverage > 0 {
				plev = held.Leverage
			}
		}
		pc = s.protectionInputs(ctx, l, req.Book, req.Side, q, acct, pos, req.AmountUSD*float64(lev), plev, pol)
		warnings = append(warnings, pc.warnings...)
		if req.Profile == "" && req.Side == TradeOpen {
			warnings = append(warnings, fmt.Sprintf("no stop requested: the %s profile's levels were applied (the policy requires a stop)", profile))
		}
		if req.Side == TradeProtect && pos.State != "long" {
			// No entry to measure from; the eligibility step below says why.
			pc.in.Entry, pc.in.Mark = q.Bid, q.Bid
		}
		lv, lerr := pc.levelsFor(profile, req.StopLossPct, req.TakeProfitPct)
		if lerr != nil {
			reasons = append(reasons, "protection: "+lerr.Error())
		} else {
			block := pc.block(string(profile), lv)
			pv.Protection = &block
			if !lv.Feasible {
				reasons = append(reasons, "protection: "+lv.Reason)
			}
		}
	}

	// 2. risk.Check with the policy, the executor's order and state. A
	// protect is not an order: only the halt applies to it. Units are the
	// exposure (invested x leverage) at the ask.
	ro := risk.Order{Book: req.Book, RefPrice: pv.RefPrice, Leverage: lev}
	rs := risk.State{OrdersToday: etoroledger.LegsOn(recs, req.Book, today), Equity: acct.Equity, Exposure: pv.Account.Exposure,
		MarketClosed: !mktcal.IsOpen(now), OpenPositions: len(all)}
	switch req.Side {
	case TradeOpen:
		ro.Side, ro.Price, ro.QtyBTC = "buy", q.Ask, req.AmountUSD*float64(lev)/q.Ask
		pv.ExposureUSD = ro.QtyBTC * q.Ask
		if pv.Protection != nil {
			ro.StopLoss, ro.TakeProfit = pv.Protection.Levels.SLRate, pv.Protection.Levels.TPRate
			pv.MaxLossUSD = pv.Protection.Levels.MaxLossUSD
		}
	case TradeClose:
		ro.Side, ro.Price, ro.QtyBTC = "sell", q.Bid, pos.Units
		rs.PositionBTC = pos.Units
		pv.ExposureUSD = pos.Units * q.Bid
	case TradeProtect:
		ro.Side, ro.Price, ro.QtyBTC = "protect", q.Bid, pos.Units
		rs.PositionBTC = pos.Units
		pv.ExposureUSD = pos.Units * q.Bid
	}
	pv.Units = ro.QtyBTC
	if req.Side == TradeProtect {
		dec := risk.Decision{Allowed: true, Findings: []risk.Finding{}}
		if pol.Halted {
			msg := "trading is halted"
			if pol.HaltReason != "" {
				msg += ": " + pol.HaltReason
			}
			dec = risk.Decision{Allowed: false, Findings: []risk.Finding{{Rule: risk.RuleHalted, Severity: risk.Block, Value: 1, Message: msg}}}
		}
		pv.Risk = &etoroledger.RiskCheck{PolicyVersion: pol.Version, Order: ro, State: rs, Allowed: dec.Allowed, Findings: dec.Findings, Halt: halt}
		for _, f := range dec.Findings {
			reasons = append(reasons, f.Rule+": "+f.Message)
		}
	} else {
		dec := risk.Check(pol, ro, rs)
		pv.Risk = &etoroledger.RiskCheck{PolicyVersion: pol.Version, Order: ro, State: rs, Allowed: dec.Allowed, Findings: dec.Findings, Halt: halt}
		for _, f := range dec.Findings {
			if f.Severity == risk.Block {
				reasons = append(reasons, f.Rule+": "+f.Message)
			} else {
				warnings = append(warnings, f.Rule+": "+f.Message)
			}
		}
	}

	// 3. Eligibility: the ledger and the account must agree, as the executor
	// requires before it acts. On the real account the whole account must
	// hold nothing the canary ledger does not know (plan §5.3), not only
	// the instrument: any other position blocks an open. The rehearsal on
	// demo shares the account with the forward-test cells, so there a
	// position on the instrument the rehearsal ledger does not know is a
	// warning, not a block (the close and protect checks are by position id
	// and unchanged).
	switch req.Side {
	case TradeOpen:
		switch {
		case pos.State == "long":
			reasons = append(reasons, fmt.Sprintf("the ledger already holds position %s on %s (%.6f units): close it before opening again", pos.PositionID, in.Symbol, pos.Units))
		case len(mine) > 0 && s.Trading.Rehearsal:
			warnings = append(warnings, fmt.Sprintf("rehearsal: the demo account holds %s on %s that this ledger does not (the demo cell's); the rehearsal position will sit next to it until you close it", tradeIDs(mine), in.Symbol))
		case len(mine) > 0:
			reasons = append(reasons, fmt.Sprintf("the account holds %s on %s that the ledger does not: resolve by hand, nothing sent", tradeIDs(mine), in.Symbol))
		}
		if real && len(all) > 0 {
			reasons = append(reasons, fmt.Sprintf("REAL: the account holds %s and the canary may be its only position: close or record them first, nothing sent", tradeIDs(all)))
		}
	default: // close, protect
		verb := "close"
		if req.Side == TradeProtect {
			verb = "protect"
		}
		switch {
		case pos.State != "long":
			reasons = append(reasons, fmt.Sprintf("the ledger holds no position on %s to %s", in.Symbol, verb))
		case req.PositionID != pos.PositionID:
			reasons = append(reasons, fmt.Sprintf("position %s is not the ledger's position on %s (%s)", req.PositionID, in.Symbol, pos.PositionID))
		default:
			held := heldPosition(all, in.ID, pos.PositionID)
			if held == nil {
				reasons = append(reasons, fmt.Sprintf("the account no longer holds position %s (closed outside the executor?): resolve by hand", pos.PositionID))
			} else if pos.Units > 0 && math.Abs(held.Units-pos.Units) > 1e-6*math.Max(1, pos.Units) {
				warnings = append(warnings, fmt.Sprintf("position %s holds %.6f units at eToro, the ledger says %.6f", pos.PositionID, held.Units, pos.Units))
			}
			if req.Side == TradeProtect && pv.Protection != nil {
				pv.Protection.Before = beforeOf(held)
				if _, ok := b.(broker.ProtectionEditor); !ok {
					reasons = append(reasons, "this broker cannot edit a position's stop loss / take profit")
				}
				if held != nil && held.StopLoss > 0 && pv.Protection.Levels.SLRate < held.StopLoss {
					warnings = append(warnings, fmt.Sprintf("the new stop %.2f is below the current stop %.2f: this loosens the protection", pv.Protection.Levels.SLRate, held.StopLoss))
				}
			}
		}
	}

	// 4. Cost preview (opens). 5. Cash (opens): what is invested must be
	// available.
	if req.Side == TradeOpen {
		c, err := b.PreviewOpen(ctx, broker.OpenRequest{Instrument: bin, Side: broker.Long, Amount: req.AmountUSD, Leverage: lev, ClientRef: "preview"})
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
	case req.Side == TradeProtect && pv.Protection != nil && pc.sma > 0 && pv.Protection.Levels.SLRate > pc.sma:
		warnings = append(warnings, fmt.Sprintf("the stop %.2f sits above the rule's SMA50 %.2f: tighter than the rule's own exit", pv.Protection.Levels.SLRate, pc.sma))
	}

	ref, err := clientRefFor(real, req.Book, today, req.kind())
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
	// Protection is what the position carries after the place (opens and
	// protects): the rates eToro reports when it could be read back, else
	// the ones sent. Warnings carry a pending edit. 2026-10-19.
	Protection *etoroledger.Protection `json:"protection,omitempty"`
	Warnings   []string                `json:"warnings,omitempty"`
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
	if (c.Env == string(etoro.EnvReal)) != s.Trading.Real() {
		refuse(http.StatusBadRequest, "trade token was prepared for another account environment")
		return
	}
	lev := c.Leverage
	if lev < 1 {
		lev = 1
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
	base.Detail = tradeDetail(c.Side, c.Book, c.AmountUSD, c.PositionID, c.Units, lev, s.Trading.Real())

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
	case c.Side != TradeProtect && !mktcal.IsOpen(now):
		// A protect edits a resting order and is accepted around the clock;
		// opens and closes trade in the cash session only.
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
	case c.Side != TradeOpen && (pos.State != "long" || pos.PositionID != c.PositionID):
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
	held := heldPosition(all, in.ID, pos.PositionID)
	switch {
	case c.Side == TradeOpen && s.Trading.Real() && len(all) > 0:
		blocked(fmt.Sprintf("REAL: the account holds %s and the canary may be its only position", tradeIDs(all)))
		return
	case c.Side == TradeOpen && len(mine) > 0 && !s.Trading.Rehearsal:
		blocked(fmt.Sprintf("the account holds %s on %s that the ledger does not: resolve by hand", tradeIDs(mine), in.Symbol))
		return
	case c.Side != TradeOpen && held == nil:
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

	if c.Side == TradeProtect {
		s.placeProtect(w, ctx, placeEnv{l: l, led: led, ap: ap, base: base, c: c, in: in, bin: bin, pos: pos, held: held, account: account, intent: intent, now: now, today: today})
		return
	}

	var bo broker.Order
	var oerr error
	if c.Side == TradeOpen {
		s.Log.Printf("trade.place ledger=%s %s open %s %.2f USD x%d sl=%.2f tp=%.2f ref=%s by=%q", l.Name, strings.ToUpper(s.Trading.Environment()), in.Symbol, c.AmountUSD, lev, c.SLRate, c.TPRate, c.ClientRef, base.By)
		bo, oerr = s.Trading.Broker.Open(ctx, broker.OpenRequest{Instrument: bin, Side: broker.Long, Amount: c.AmountUSD, Leverage: lev,
			StopLoss: c.SLRate, TakeProfit: c.TPRate, ClientRef: c.ClientRef, IntentAt: intent.IntentAt})
	} else {
		s.Log.Printf("trade.place ledger=%s %s close %s position %s ref=%s by=%q", l.Name, strings.ToUpper(s.Trading.Environment()), in.Symbol, c.PositionID, c.ClientRef, base.By)
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

	// The ledger line (the executor's shape, source ui; mode and env "real"
	// on the canary, schema unchanged).
	mid := (c.Bid + c.Ask) / 2
	d := &etoroledger.Demo{Env: s.Trading.Environment(), Action: kindOf(c.Side), PositionBefore: pos, Account: account,
		Quote: &etoroledger.Quote{Bid: c.Bid, Ask: c.Ask, At: time.Unix(c.Iat, 0).UTC().Format(time.RFC3339), SpreadBps: broker.Quote{Bid: c.Bid, Ask: c.Ask}.SpreadBps()},
		Order: &o,
		Notes: []string{ledgerNote(s.Trading)},
	}
	ro := risk.Order{Book: c.Book, RefPrice: c.RefPrice, Leverage: lev, QtyBTC: c.Units}
	rsState := risk.State{OrdersToday: etoroledger.LegsOn(led.Records(), c.Book, today), MarketClosed: false, OpenPositions: len(all)}
	if account != nil {
		rsState.Equity, rsState.Exposure = account.Equity, account.Exposure
	}
	if c.Side == TradeOpen {
		ro.Side, ro.Price, ro.StopLoss, ro.TakeProfit = "buy", c.Ask, c.SLRate, c.TPRate
		d.Target = "long"
		d.PositionAfter = etoroledger.Position{State: "long", PositionID: bo.PositionIDs[0], Units: bo.Units, Amount: bo.Amount,
			OpenRate: bo.AvgPrice, OpenedAt: bo.At.UTC().Format(time.RFC3339)}
		if c.SLRate > 0 {
			d.PositionAfter.StopLossRate, d.PositionAfter.StopType = c.SLRate, broker.StopFixed
		}
		d.PositionAfter.TakeProfitRate = c.TPRate
		// What eToro holds, when the portfolio already shows the position
		// (it may round the rates).
		if ps, err := s.Trading.Broker.Positions(ctx); err == nil {
			if hp := heldPosition(ps, in.ID, bo.PositionIDs[0]); hp != nil && hp.StopLoss > 0 {
				d.PositionAfter.StopLossRate, d.PositionAfter.TakeProfitRate, d.PositionAfter.StopType = hp.StopLoss, hp.TakeProfit, hp.StopType
			}
		}
		if c.SLRate > 0 {
			d.Protection = &etoroledger.Protection{Profile: c.Profile, ATR: c.ATR, ATRPct: c.ATRPct,
				MaxLossUSD: bo.Units * (bo.AvgPrice - d.PositionAfter.StopLossRate), Reason: "set with the open from the console"}
			resp.Protection = d.Protection
		} else {
			d.Notes = append(d.Notes, "opened without a stop loss")
		}
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
	d.Risk = &etoroledger.RiskCheck{PolicyVersion: s.Trading.policy().Version, Order: ro, State: rsState, Allowed: true}
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
		Mode: ledgerMode(s.Trading), Venue: etorobroker.Venue, Book: c.Book, Instrument: in,
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
	switch side {
	case TradeOpen:
		return etoroledger.ActionOpen
	case TradeProtect:
		return etoroledger.ActionProtect
	}
	return etoroledger.ActionClose
}

// confirmationPhrase is what the operator types to place: the verb and the
// symbol, with REAL between them on the real account ("PLACE REAL NSDQ100").
func confirmationPhrase(side, symbol string, real bool) string {
	verb := "CLOSE"
	switch side {
	case TradeOpen:
		verb = "PLACE"
	case TradeProtect:
		verb = "PROTECT"
	}
	if real {
		return verb + " REAL " + symbol
	}
	return verb + " " + symbol
}

// clientRefFor is the console's idempotency key: ui-real-… on the real
// account, ui-… otherwise.
func clientRefFor(real bool, book, nyDate, kind string) (string, error) {
	if real {
		return etoroledger.UIRealClientRef(book, nyDate, kind)
	}
	return etoroledger.UIClientRef(book, nyDate, kind)
}

// ledgerMode is the Record.Mode the process writes.
func ledgerMode(t *EtoroTrading) string {
	if t.Real() {
		return etoroledger.ModeReal
	}
	return etoroledger.ModeDemo
}

// ledgerNote is the first note of a console line: what the trade is and
// what happens to it next.
func ledgerNote(t *EtoroTrading) string {
	switch {
	case t.Real():
		return "REAL account: measurement canary trade from the console (ui-api -etoro-real-canary); no executor acts on this ledger"
	case t != nil && t.Rehearsal:
		return "canary rehearsal on the demo account (ui-api -canary-rehearsal); no executor acts on this ledger"
	}
	return "assisted trade from the console (ui-api); the executor brings the book back to the rule's target at its next run unless the ledger is halted"
}

// TradeCancelRequest is the body of POST /api/ui/etoro/trades/cancel
// (2026-10-21): the preview's token, and optionally who and why. A cancel
// changes nothing at the venue or in the ledger, so no phrase is required;
// Reason and By, when given, go to the audit line.
type TradeCancelRequest struct {
	Token  string `json:"token"`
	Reason string `json:"reason,omitempty"`
	By     string `json:"by,omitempty"`
}

// Cancel statuses.
const (
	// CancelCancelled: the token was live and is burned now.
	CancelCancelled = "cancelled"
	// CancelExpired: the token had already expired; nothing to burn, the
	// cancel is still audited so the operator's decision is on record.
	CancelExpired = "expired"
)

// TradeCancelResponse is the outcome of a cancel.
type TradeCancelResponse struct {
	Status     string      `json:"status"` // CancelCancelled | CancelExpired
	Ledger     string      `json:"ledger"`
	Book       string      `json:"book"`
	Side       string      `json:"side"`
	ClientRef  string      `json:"client_ref"`
	RequestID  string      `json:"request_id"`
	ExpiresAt  string      `json:"expires_at"`
	AuditPath  string      `json:"audit_path"`
	Audit      audit.Entry `json:"audit"`
	AuditError string      `json:"audit_error,omitempty"`
}

// tradeCancel burns a prepared trade's token. Gates as place: loopback
// Origin, operator token, JSON body, a token signed for this operator
// session and this ledger. Under the controls mutex so it serialises with
// a place of the same token: whichever consumes the id first wins and the
// other is a 409.
func (s *Server) tradeCancel(w http.ResponseWriter, r *http.Request) {
	l, ok := s.pickEtoro(w, r)
	if !ok {
		return
	}
	if enabled, why := s.tradingState(l); !enabled {
		writeErr(w, http.StatusForbidden, why)
		return
	}
	ap := auditPath(l)
	base := s.baseEntry(r, "trade.cancel")
	base.Ledger = l.Name
	var req TradeCancelRequest
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
	if strings.ContainsAny(base.Reason, "\r\n\x00") {
		refuse(http.StatusBadRequest, "reason: one line, no control characters")
		return
	}
	if base.By != "" && !byRe.MatchString(base.By) {
		refuse(http.StatusBadRequest, "by: 1-64 letters, digits, spaces or . _ @ -")
		return
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
	base.Detail = tradeDetail(c.Side, c.Book, c.AmountUSD, c.PositionID, c.Units, c.Leverage, s.Trading.Real())
	now := s.Now().UTC()

	s.controlsMu.Lock()
	defer s.controlsMu.Unlock()
	resp := TradeCancelResponse{Ledger: l.Name, Book: c.Book, Side: c.Side, ClientRef: c.ClientRef, RequestID: c.RequestID,
		ExpiresAt: time.Unix(c.Exp, 0).UTC().Format(time.RFC3339), AuditPath: ap}
	switch {
	case now.Unix() >= c.Exp:
		// Place refuses an expired token already; nothing to burn. Answered
		// 200 so the page lands in the same state as a live cancel.
		resp.Status = CancelExpired
		base.Detail += " -> token had expired at " + resp.ExpiresAt + "; nothing to cancel, nothing sent"
	case !s.Trading.consume(c.ID, c.Exp, now.Unix()):
		refuse(http.StatusConflict, "trade token already used: it was placed or cancelled")
		return
	default:
		resp.Status = CancelCancelled
		base.Detail += " -> cancelled: token burned, nothing sent"
	}
	done := base
	done.Outcome = audit.Done
	if err := audit.Append(ap, done); err != nil {
		s.Log.Printf("audit %s: %v", ap, err)
		resp.AuditError = err.Error()
	}
	resp.Audit = done
	s.Log.Printf("trade.cancel ledger=%s %s %s -> %s id=%s by=%q", l.Name, c.Side, c.Book, resp.Status, c.ClientRef, base.By)
	writeJSON(w, http.StatusOK, resp)
}

// placeEnv is what placeProtect needs from tradePlace after the shared
// checks, the intent and the "requested" audit line.
type placeEnv struct {
	l       Ledger
	led     *etoroledger.Ledger
	ap      string
	base    audit.Entry
	c       tradeClaims
	in      etoroledger.Instrument
	bin     broker.Instrument
	pos     etoroledger.Position
	held    *broker.Position
	account *etoroledger.Account
	intent  etoroledger.Intent
	now     time.Time
	today   string
}

// placeProtect sends the stop loss / take profit edit of a held position
// and records it: a ledger line with action protect (source ui, not a leg)
// whose position_after carries the rates eToro reports (executed) or the
// ones sent (pending: the portfolio had not shown them within the
// adapter's wait; a warning says so and reconcile's protection check
// confirms later). A rejection is a failed place like an order's: the
// intent stays on disk.
func (s *Server) placeProtect(w http.ResponseWriter, ctx context.Context, e placeEnv) {
	c, l, base := e.c, e.l, e.base
	resp := TradePlaceResponse{Book: c.Book, Side: c.Side, ClientRef: c.ClientRef, RequestID: c.RequestID, PositionIDs: []string{c.PositionID},
		Units: e.pos.Units, AmountUSD: e.pos.Amount, AvgPrice: e.pos.OpenRate, PositionAfter: e.pos, AuditPath: e.ap, Warnings: []string{}}
	fail := func(status int, msg string) {
		msg += fmt.Sprintf(" (the intent %s stays on disk; etoro-reconcile and the data-health page report it until it is resolved by hand)", c.ClientRef)
		ae := base
		ae.Outcome, ae.Error = audit.Failed, msg
		if err := audit.Append(e.ap, ae); err != nil {
			s.Log.Printf("audit %s: %v", e.ap, err)
			resp.AuditError = err.Error()
		}
		resp.Audit, resp.Error = ae, msg
		if resp.Status == "" {
			resp.Status = string(broker.StatusUnknown)
		}
		writeJSON(w, status, resp)
	}
	ed, ok := s.Trading.Broker.(broker.ProtectionEditor)
	if !ok {
		resp.Status = string(broker.StatusRejected)
		fail(http.StatusNotImplemented, "this broker cannot edit a position's stop loss / take profit")
		return
	}
	s.Log.Printf("trade.place ledger=%s protect %s position %s sl=%.2f tp=%.2f ref=%s by=%q", l.Name, e.in.Symbol, c.PositionID, c.SLRate, c.TPRate, c.ClientRef, base.By)
	res, perr := ed.SetProtection(ctx, broker.ProtectionRequest{Instrument: e.bin, PositionID: c.PositionID, StopLoss: c.SLRate, TakeProfit: c.TPRate, ClientRef: c.ClientRef})
	finished := s.Now().UTC()
	resp.Status, resp.OrderID = string(res.Status), res.OperationID
	switch {
	case perr != nil:
		fail(http.StatusBadGateway, fmt.Sprintf("protect %s: %v", c.ClientRef, perr))
		return
	case res.Status != broker.StatusExecuted && res.Status != broker.StatusPending:
		fail(http.StatusBadGateway, fmt.Sprintf("protect %s is %s at eToro: %s", c.ClientRef, res.Status, res.Error))
		return
	}

	// The ledger line: the position with its new rates.
	after := e.pos
	after.StopLossRate, after.TakeProfitRate, after.StopType = c.SLRate, c.TPRate, broker.StopFixed
	if c.SLRate == 0 && e.held != nil {
		after.StopLossRate, after.StopType = e.held.StopLoss, e.held.StopType
	}
	if c.TPRate == 0 && e.held != nil {
		after.TakeProfitRate = e.held.TakeProfit
	}
	if res.Status == broker.StatusExecuted && res.Position.ID == c.PositionID {
		after.StopLossRate, after.TakeProfitRate = res.Position.StopLoss, res.Position.TakeProfit
		if res.Position.StopType != "" {
			after.StopType = res.Position.StopType
		}
	} else {
		resp.Warnings = append(resp.Warnings, "eToro accepted the edit but the position did not show the new rates within the adapter's wait: recorded as sent; check the position (reconcile reports a protection drift if it never applies)")
	}
	before := e.pos
	if e.held != nil {
		before.StopLossRate, before.TakeProfitRate, before.StopType = e.held.StopLoss, e.held.TakeProfit, e.held.StopType
	}
	o := etoroledger.Order{Kind: etoroledger.ActionProtect, ClientRef: c.ClientRef, RequestID: c.RequestID, OrderID: res.OperationID, Status: string(res.Status),
		PositionIDs: []string{c.PositionID}, Units: e.pos.Units, Amount: e.pos.Amount, IntentAt: e.intent.IntentAt, Started: e.now, Finished: finished}
	prot := &etoroledger.Protection{Profile: c.Profile, ATR: c.ATR, ATRPct: c.ATRPct, StopLossBefore: before.StopLossRate, TakeProfitBefore: before.TakeProfitRate,
		OperationID: res.OperationID, Reason: "set from the console"}
	if after.StopLossRate > 0 && e.pos.OpenRate > 0 {
		prot.MaxLossUSD = e.pos.Units * (e.pos.OpenRate - after.StopLossRate)
	}
	d := &etoroledger.Demo{Env: s.Trading.Environment(), Target: "long", Action: etoroledger.ActionProtect, PositionBefore: before, PositionAfter: after, Account: e.account,
		Quote:      &etoroledger.Quote{Bid: c.Bid, Ask: c.Ask, At: time.Unix(c.Iat, 0).UTC().Format(time.RFC3339), SpreadBps: broker.Quote{Bid: c.Bid, Ask: c.Ask}.SpreadBps()},
		Order:      &o,
		Protection: prot,
		Notes:      []string{"protection set from the console (ui-api): the position's stop loss / take profit at eToro; not a leg"},
	}
	if e.held != nil {
		d.FinancingToDate = e.held.Fees
	}
	var ruleSignal string
	var ruleSMA float64
	for _, rr := range e.led.Records() {
		if rr.Book == c.Book && !rr.IsUI() {
			ruleSignal, ruleSMA = rr.Decision.Signal, rr.Decision.SMA
		}
	}
	rec := etoroledger.Record{
		Schema: etoroledger.Schema, RecordedAt: finished.Format(time.RFC3339), CodeVersion: "ui-api " + s.Version,
		Mode: ledgerMode(s.Trading), Venue: etorobroker.Venue, Book: c.Book, Instrument: e.in,
		Decision: etoroledger.Decision{BarDate: e.today, FillDate: e.today, Close: c.RefPrice, SMA: ruleSMA, Signal: etoroledger.SignalManual, PrevSignal: ruleSignal, Action: etoroledger.ActionProtect},
		Demo:     d, Source: etoroledger.SourceUI, Operator: &etoroledger.Operator{By: base.By, Reason: base.Reason},
	}
	resp.PositionAfter, resp.Protection = after, prot
	done := base
	done.Outcome = audit.Done
	done.Detail += fmt.Sprintf(" -> %s: stop %.2f -> %.2f, target %.2f -> %.2f (operation %s)", res.Status, before.StopLossRate, after.StopLossRate, before.TakeProfitRate, after.TakeProfitRate, res.OperationID)
	if err := e.led.Append(rec); err != nil {
		resp.LedgerError = err.Error()
		done.Error = "protection set but the ledger line could not be written: " + err.Error() + " (the intent is on disk; reconcile reports it)"
		s.Log.Printf("trade.place ledger=%s %s: %s", l.Name, c.ClientRef, done.Error)
	} else {
		resp.LedgerRecorded = true
	}
	if err := audit.Append(e.ap, done); err != nil {
		s.Log.Printf("audit %s: %v", e.ap, err)
		resp.AuditError = err.Error()
	}
	resp.Audit = done
	s.Log.Printf("trade.place ledger=%s protect %s %s: stop %.2f target %.2f (recorded=%v) id=%s", l.Name, e.in.Symbol, res.Status, after.StopLossRate, after.TakeProfitRate, resp.LedgerRecorded, done.ID)
	writeJSON(w, http.StatusOK, resp)
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
