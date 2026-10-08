package api

// Strategies page and its controls:
//   - GET  /api/ui/strategies: every ledger with its halt file, plus the
//     intraday strategy-executor's strategies when ui-api is given its URL
//     (-strategy-executor-url), and the strategy audit log;
//   - POST /api/ui/strategies/{name}/start|stop: operator start/stop sent to
//     strategy-executor. A stop is a *hold*: the executor persists it
//     (STRATEGY_HOLD_FILE) so nothing but an operator start runs the
//     strategy again, across restarts;
//   - POST /api/ui/risk/halt-all: the kill switch, writing a halt to every
//     local ledger's halt file.
//
// All of them use the R4 gates (operator token, loopback Origin, JSON body,
// reason/by/confirm) and the same audit order: a "requested" line before
// anything changes, then "done" or "failed".

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"bitso-trading-platform/ui-api/internal/audit"
	"bitso-trading-platform/ui-api/internal/executor"
)

// HaltAllConfirm must be typed to use the kill switch.
const HaltAllConfirm = "HALT ALL"

// StrategyAuditFile is the default strategy audit log name, kept next to the
// default local ledger.
const StrategyAuditFile = "ui-strategy-audit.jsonl"

var strategyNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// LedgerControl is one ledger on the Strategies page.
type LedgerControl struct {
	Name   string `json:"name"`
	Remote bool   `json:"remote"`
	// ControlsEnabled says whether halt/resume (and the kill switch) can
	// act on this ledger.
	ControlsEnabled bool         `json:"controls_enabled"`
	DisabledReason  string       `json:"disabled_reason,omitempty"`
	HaltFile        HaltFileInfo `json:"halt_file"`
}

// KillSwitchInfo describes POST /api/ui/risk/halt-all.
type KillSwitchInfo struct {
	Enabled        bool     `json:"enabled"`
	DisabledReason string   `json:"disabled_reason,omitempty"`
	Confirm        string   `json:"confirm"`
	Targets        []string `json:"targets"`
	// AlreadyHalted are targets whose halt file already halts them.
	AlreadyHalted []string `json:"already_halted"`
}

// ExecutorInfo is the strategy-executor connection.
type ExecutorInfo struct {
	Configured      bool   `json:"configured"`
	URL             string `json:"url,omitempty"`
	Reachable       bool   `json:"reachable"`
	Error           string `json:"error,omitempty"`
	ControlsEnabled bool   `json:"controls_enabled"`
	DisabledReason  string `json:"disabled_reason,omitempty"`
	// HoldsSupported is false for an executor without the hold list (its
	// list has no "holds"): stops would not survive a restart.
	HoldsSupported bool   `json:"holds_supported"`
	FetchedAt      string `json:"fetched_at,omitempty"`
}

// HoldView is an operator hold.
type HoldView struct {
	Name   string `json:"name,omitempty"`
	Reason string `json:"reason"`
	By     string `json:"by"`
	At     string `json:"at"`
}

// StrategyView is one strategy-executor strategy.
type StrategyView struct {
	Name          string    `json:"name"`
	Type          string    `json:"type"`
	Version       string    `json:"version"`
	Book          string    `json:"book"`
	Running       bool      `json:"running"`
	Enabled       bool      `json:"enabled"`
	DryRun        *bool     `json:"dry_run,omitempty"`
	HasPosition   bool      `json:"has_position"`
	PositionSide  string    `json:"position_side,omitempty"`
	PositionSize  float64   `json:"position_size"`
	EntryPrice    float64   `json:"entry_price,omitempty"`
	UnrealizedPnL float64   `json:"unrealized_pnl"`
	PendingBuy    bool      `json:"pending_buy"`
	PendingSell   bool      `json:"pending_sell"`
	SignalCount   int64     `json:"signal_count"`
	TradeCount    int64     `json:"trade_count"`
	LastSignalAt  string    `json:"last_signal_at,omitempty"`
	TotalPnL      float64   `json:"total_pnl"`
	DailyPnL      float64   `json:"daily_pnl"`
	WinRate       float64   `json:"win_rate"`
	Hold          *HoldView `json:"hold,omitempty"`
}

// StrategiesInfo is GET /api/ui/strategies.
type StrategiesInfo struct {
	Ledgers    []LedgerControl `json:"ledgers"`
	KillSwitch KillSwitchInfo  `json:"kill_switch"`
	Executor   ExecutorInfo    `json:"executor"`
	Strategies []StrategyView  `json:"strategies"`
	// Holds are holds on names the executor does not have registered now;
	// they apply again when the strategy is registered.
	Holds      []HoldView    `json:"holds"`
	AuditPath  string        `json:"audit_path,omitempty"`
	Audit      []audit.Entry `json:"audit"`
	AuditError string        `json:"audit_error,omitempty"`
}

// StrategyControlRequest is the body of POST /api/ui/strategies/{name}/start|stop.
type StrategyControlRequest struct {
	Reason string `json:"reason"`
	By     string `json:"by"`
	// Confirm must equal the strategy name.
	Confirm string `json:"confirm"`
	// AckPosition must be true to stop a strategy with an open position or
	// a pending order: stopping it leaves that position unmanaged.
	AckPosition bool `json:"ack_position,omitempty"`
}

// StrategyControlResponse answers a successful start/stop.
type StrategyControlResponse struct {
	Strategy   string                 `json:"strategy"`
	Action     string                 `json:"action"`
	Upstream   map[string]interface{} `json:"upstream"`
	Audit      audit.Entry            `json:"audit"`
	AuditError string                 `json:"audit_error,omitempty"`
}

// HaltAllResult is one ledger's outcome in a halt-all.
type HaltAllResult struct {
	Ledger   string       `json:"ledger"`
	Outcome  string       `json:"outcome"` // halted | already_halted | failed
	Error    string       `json:"error,omitempty"`
	HaltFile HaltFileInfo `json:"halt_file"`
}

// HaltAllResponse answers POST /api/ui/risk/halt-all.
type HaltAllResponse struct {
	Action  string          `json:"action"`
	Group   string          `json:"group"`
	Results []HaltAllResult `json:"results"`
}

func zeroTime(s string) string {
	if s == "" || strings.HasPrefix(s, "0001-01-01") {
		return ""
	}
	return s
}

func viewStrategy(s executor.Strategy) StrategyView {
	v := StrategyView{
		Name: s.Name, Type: s.Type, Version: s.Version, Book: s.Book,
		Running: s.Running, Enabled: s.Enabled,
		HasPosition: s.State.HasPosition, PositionSide: s.State.PositionSide,
		PositionSize: s.State.PositionSize, EntryPrice: s.State.EntryPrice,
		UnrealizedPnL: s.State.UnrealizedPnL,
		PendingBuy:    s.State.PendingBuy, PendingSell: s.State.PendingSell,
		SignalCount: s.State.SignalCount, TradeCount: s.State.TradeCount,
		LastSignalAt: zeroTime(s.State.LastSignalTime),
		TotalPnL:     s.Metrics.TotalPnL, DailyPnL: s.Metrics.DailyPnL, WinRate: s.Metrics.WinRate,
	}
	if d, ok := s.Parameters["dry_run"].(bool); ok {
		v.DryRun = &d
	}
	if s.Hold != nil {
		v.Hold = &HoldView{Reason: s.Hold.Reason, By: s.Hold.By, At: s.Hold.At}
	}
	return v
}

// localLedgers are the ledgers whose halt file ui-api can write.
func (s *Server) localLedgers() []Ledger {
	var out []Ledger
	for _, l := range s.ledgers() {
		if !l.Store.Remote() {
			out = append(out, l)
		}
	}
	return out
}

// strategyAuditPath is where strategy actions are audited ("" if nowhere).
func (s *Server) strategyAuditPath() string {
	if s.StrategyAuditPath != "" {
		return s.StrategyAuditPath
	}
	if ls := s.localLedgers(); len(ls) > 0 {
		return filepath.Join(filepath.Dir(ls[0].Store.LedgerPath), StrategyAuditFile)
	}
	return ""
}

// strategyControlsState reports whether strategy start/stop is available.
func (s *Server) strategyControlsState() (bool, string) {
	switch {
	case s.Executor == nil:
		return false, "no strategy-executor: start ui-api with -strategy-executor-url (loopback)"
	case s.OperatorToken == "":
		return false, "controls are off: start ui-api with -operator-token-file (see the plan, §8.12)"
	case s.strategyAuditPath() == "":
		return false, "no local ledger to keep the strategy audit log next to: set -strategy-audit-file"
	}
	return true, ""
}

func (s *Server) killSwitchState(targets []Ledger) (bool, string) {
	switch {
	case s.OperatorToken == "":
		return false, "controls are off: start ui-api with -operator-token-file (see the plan, §8.12)"
	case len(targets) == 0:
		return false, "no local ledgers: S3 copies cannot be halted from here"
	}
	return true, ""
}

func (s *Server) strategies(w http.ResponseWriter, r *http.Request) {
	info := StrategiesInfo{Ledgers: []LedgerControl{}, Strategies: []StrategyView{}, Holds: []HoldView{}, Audit: []audit.Entry{}}
	local := s.localLedgers()
	for _, l := range s.ledgers() {
		enabled, why := s.controlsState(l)
		_, hf := s.policyFor(l)
		info.Ledgers = append(info.Ledgers, LedgerControl{Name: l.Name, Remote: l.Store.Remote(),
			ControlsEnabled: enabled, DisabledReason: why, HaltFile: hf})
	}
	ks := KillSwitchInfo{Confirm: HaltAllConfirm, Targets: []string{}, AlreadyHalted: []string{}}
	ks.Enabled, ks.DisabledReason = s.killSwitchState(local)
	for _, lc := range info.Ledgers {
		if lc.Remote {
			continue
		}
		ks.Targets = append(ks.Targets, lc.Name)
		if lc.HaltFile.Halted && lc.HaltFile.Error == "" {
			ks.AlreadyHalted = append(ks.AlreadyHalted, lc.Name)
		}
	}
	info.KillSwitch = ks

	ex := ExecutorInfo{Configured: s.Executor != nil}
	ex.ControlsEnabled, ex.DisabledReason = s.strategyControlsState()
	if s.Executor != nil {
		ex.URL = s.Executor.URL()
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		list, err := s.Executor.List(ctx)
		cancel()
		ex.FetchedAt = s.Now().UTC().Format(time.RFC3339)
		if err != nil {
			ex.Error = err.Error()
			if ex.ControlsEnabled {
				ex.ControlsEnabled, ex.DisabledReason = false, "strategy-executor is unreachable"
			}
		} else {
			ex.Reachable = true
			ex.HoldsSupported = list.Holds != nil
			registered := map[string]bool{}
			for _, st := range list.Strategies {
				registered[st.Name] = true
				info.Strategies = append(info.Strategies, viewStrategy(st))
			}
			sort.Slice(info.Strategies, func(i, j int) bool { return info.Strategies[i].Name < info.Strategies[j].Name })
			for name, h := range list.Holds {
				if !registered[name] {
					info.Holds = append(info.Holds, HoldView{Name: name, Reason: h.Reason, By: h.By, At: h.At})
				}
			}
			sort.Slice(info.Holds, func(i, j int) bool { return info.Holds[i].Name < info.Holds[j].Name })
		}
	}
	info.Executor = ex

	if p := s.strategyAuditPath(); p != "" && s.Executor != nil {
		info.AuditPath = p
		entries, err := audit.Tail(p, auditTail)
		info.Audit = entries
		if err != nil {
			info.AuditError = err.Error()
		}
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) startStrategy(w http.ResponseWriter, r *http.Request) {
	s.strategyControl(w, r, "start")
}
func (s *Server) stopStrategy(w http.ResponseWriter, r *http.Request) {
	s.strategyControl(w, r, "stop")
}

func (s *Server) strategyControl(w http.ResponseWriter, r *http.Request, action string) {
	name := r.PathValue("name")
	if !strategyNameRe.MatchString(name) {
		writeErr(w, http.StatusBadRequest, "invalid strategy name")
		return
	}
	enabled, why := s.strategyControlsState()
	if !enabled {
		writeErr(w, http.StatusForbidden, why)
		return
	}
	ap := s.strategyAuditPath()
	sinks := []string{ap}
	base := s.baseEntry(r, "strategy_"+action)
	base.Strategy, base.Executor = name, s.Executor.URL()
	var req StrategyControlRequest
	if !s.admit(w, r, base, sinks, &req) {
		return
	}
	base.By, base.Reason = clip(strings.TrimSpace(req.By), 64), clip(strings.TrimSpace(req.Reason), maxReasonLen)
	refuse := func(e audit.Entry, status int, msg string) {
		e.Outcome, e.Error = audit.Refused, msg
		s.noteAll(sinks, e)
		writeErr(w, status, msg)
	}
	if err := validateControl(ControlRequest{Reason: req.Reason, By: req.By, Confirm: req.Confirm}, name, "the strategy name"); err != nil {
		refuse(base, http.StatusBadRequest, err.Error())
		return
	}

	s.controlsMu.Lock()
	defer s.controlsMu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	cur, err := s.Executor.Get(ctx, name)
	switch {
	case executor.StatusOf(err) == http.StatusNotFound:
		refuse(base, http.StatusNotFound, fmt.Sprintf("strategy-executor has no strategy %q", name))
		return
	case err != nil:
		refuse(base, http.StatusBadGateway, "cannot read the strategy from strategy-executor: "+err.Error())
		return
	}
	busy := cur.State.HasPosition || cur.State.PendingBuy || cur.State.PendingSell
	switch {
	case action == "start" && cur.Running:
		refuse(base, http.StatusConflict, "already running")
		return
	case action == "stop" && !cur.Running && cur.Hold != nil:
		refuse(base, http.StatusConflict, fmt.Sprintf("already stopped and held by %s at %s: %s", cur.Hold.By, cur.Hold.At, cur.Hold.Reason))
		return
	case action == "stop" && busy && !req.AckPosition:
		refuse(base, http.StatusConflict, "the strategy has an open position or pending order; stopping leaves it unmanaged. Confirm with ack_position to stop anyway")
		return
	}
	if action == "stop" && busy {
		base.Detail = fmt.Sprintf("stopped with an open position or pending order (acknowledged): side=%s size=%g pending_buy=%t pending_sell=%t",
			cur.State.PositionSide, cur.State.PositionSize, cur.State.PendingBuy, cur.State.PendingSell)
	}
	if action == "stop" && !cur.Running {
		base.Detail = strings.TrimSpace(base.Detail + " not running; recording a hold so it stays stopped")
	}

	base.ID = newID()
	req1 := base
	req1.Outcome = audit.Requested
	if err := audit.Append(ap, req1); err != nil {
		s.Log.Printf("audit %s: %v", ap, err)
		writeErr(w, http.StatusInternalServerError, "cannot write the audit log ("+err.Error()+"); nothing changed")
		return
	}
	var up map[string]interface{}
	if action == "start" {
		up, err = s.Executor.Start(ctx, name, base.By, base.Reason)
	} else {
		up, err = s.Executor.Stop(ctx, name, base.By, base.Reason)
	}
	done := base
	if err != nil {
		done.Outcome, done.Error, done.UpstreamStatus = audit.Failed, err.Error(), executor.StatusOf(err)
		s.noteAll(sinks, done)
		status := http.StatusBadGateway
		var ee *executor.Error
		if errors.As(err, &ee) && (ee.Status == http.StatusConflict || ee.Status == http.StatusNotFound) {
			status = ee.Status
		}
		writeErr(w, status, err.Error())
		return
	}
	done.Outcome, done.UpstreamStatus = audit.Done, http.StatusOK
	resp := StrategyControlResponse{Strategy: name, Action: action, Upstream: up, Audit: done}
	if err := audit.Append(ap, done); err != nil {
		s.Log.Printf("audit %s: %v", ap, err)
		resp.AuditError = err.Error()
	}
	s.Log.Printf("control strategy_%s strategy=%s by=%q id=%s", action, name, base.By, base.ID)
	writeJSON(w, http.StatusOK, resp)
}

// haltAll is the kill switch: halt every local ledger. Each ledger gets its
// own audited change (sharing a group id); ledgers already halted are left
// alone and reported as such.
func (s *Server) haltAll(w http.ResponseWriter, r *http.Request) {
	targets := s.localLedgers()
	if enabled, why := s.killSwitchState(targets); !enabled {
		writeErr(w, http.StatusForbidden, why)
		return
	}
	sinks := make([]string, 0, len(targets))
	for _, l := range targets {
		sinks = append(sinks, auditPath(l))
	}
	base := s.baseEntry(r, "halt_all")
	var req ControlRequest
	if !s.admit(w, r, base, sinks, &req) {
		return
	}
	base.By, base.Reason = clip(strings.TrimSpace(req.By), 64), clip(strings.TrimSpace(req.Reason), maxReasonLen)
	if err := validateControl(req, HaltAllConfirm, "the phrase"); err != nil {
		e := base
		e.Outcome, e.Error = audit.Refused, err.Error()
		s.noteAll(sinks, e)
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	s.controlsMu.Lock()
	defer s.controlsMu.Unlock()
	base.Group = newID()
	resp := HaltAllResponse{Action: "halt_all", Group: base.Group, Results: []HaltAllResult{}}
	failed := false
	for _, l := range targets {
		ap := auditPath(l)
		e := base
		e.Ledger = l.Name
		res := HaltAllResult{Ledger: l.Name}
		cur, _, curErr := l.Store.Halt()
		if curErr == nil {
			e.Before = &cur
		}
		if curErr == nil && cur.Halted {
			res.Outcome = "already_halted"
			e.Outcome, e.Error = audit.Refused, fmt.Sprintf("already halted by %s at %s: %s", cur.By, cur.At, cur.Reason)
			s.noteAll([]string{ap}, e)
		} else if _, auditErr, err := s.writeHaltAudited(l, ap, e, true, curErr); err != nil {
			res.Outcome, res.Error = "failed", err.Error()
			failed = true
		} else {
			res.Outcome, res.Error = "halted", auditErr
		}
		_, res.HaltFile = s.policyFor(l)
		resp.Results = append(resp.Results, res)
	}
	s.Log.Printf("control halt_all by=%q group=%s ledgers=%d failed=%t", base.By, base.Group, len(targets), failed)
	status := http.StatusOK
	if failed {
		status = http.StatusInternalServerError
	}
	writeJSON(w, status, resp)
}
