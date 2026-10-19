package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/broker"
	"bitso-trading-platform/shared/pkg/etoroledger"
	"bitso-trading-platform/shared/pkg/stops"
	"bitso-trading-platform/ui-api/internal/audit"
	"bitso-trading-platform/ui-api/internal/store"
)

var errBoom = errors.New("boom")

// Protection (plan phase B): the recommendation endpoint, opens that carry
// the stop the policy requires, the guards' clamps, and the protect flow
// on a held position.

func recommendBody(book, side string, amount float64, positionID string) string {
	m := map[string]any{"book": book, "side": side}
	if amount != 0 {
		m["amount_usd"] = amount
	}
	if positionID != "" {
		m["position_id"] = positionID
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func (h *tradeHarness) recommend(t *testing.T, body string) (int, TradeRecommendation, []byte) {
	t.Helper()
	code, b := post(t, h.ts, ctl{path: "/api/ui/etoro/trades/recommend?ledger=etoro-demo", token: h.token, origin: "http://127.0.0.1:5173", body: body})
	var rec TradeRecommendation
	if code == http.StatusOK {
		if err := json.Unmarshal(b, &rec); err != nil {
			t.Fatalf("recommend body: %v: %s", err, b)
		}
	}
	return code, rec, b
}

// prepareProtected is prepareBody plus the protection fields.
func prepareProtected(book, side string, amount float64, positionID, profile string, slPct, tpPct float64) string {
	m := map[string]any{}
	_ = json.Unmarshal([]byte(prepareBody(book, side, amount, positionID)), &m)
	if profile != "" {
		m["profile"] = profile
	}
	if slPct != 0 {
		m["stop_loss_pct"] = slPct
	}
	if tpPct != 0 {
		m["take_profit_pct"] = tpPct
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func TestTradeRecommend(t *testing.T) {
	h := tradeServer(t, tradeNow)

	// An open on NSDQ100: three profiles from the fixture's bars, moderate
	// by default, venue bounds from the broker (1 % minimum).
	code, rec, b := h.recommend(t, recommendBody("nsdq100", "open", 1100, ""))
	if code != 200 {
		t.Fatalf("%d %s", code, b)
	}
	if rec.DefaultProfile != "moderate" || len(rec.Profiles) != 3 || rec.AmountUSD != 1100 || rec.Units <= 0 || rec.Quote == nil || rec.Account == nil {
		t.Fatalf("recommendation %+v", rec)
	}
	if rec.Context.Volatility == nil || rec.Context.Volatility.ATR <= 0 || rec.Context.Volatility.Regime == stops.RegimeUnknown || rec.Context.Volatility.Bars != 120 {
		t.Fatalf("volatility %+v", rec.Context.Volatility)
	}
	if rec.Context.BoundsSource != "venue" || rec.Context.Bounds.MinStopLossPct != 0.01 || rec.Context.Limits.MaxStopLossPct != 0.10 || rec.Context.Limits.MaxLossPctEquity != 0.005 {
		t.Fatalf("bounds %+v limits %+v (%s)", rec.Context.Bounds, rec.Context.Limits, rec.Context.BoundsSource)
	}
	if rec.Context.Entry != 29851 || rec.Context.Mark != 29850 || rec.Context.SMA50 <= 0 || rec.PolicyVersion != "etoro-demo-2026-10-19" {
		t.Fatalf("context %+v policy %s", rec.Context, rec.PolicyVersion)
	}
	c, m, a := rec.Profiles["conservative"], rec.Profiles["moderate"], rec.Profiles["aggressive"]
	if !c.Feasible || !m.Feasible || !a.Feasible {
		t.Fatalf("feasible: %+v %+v %+v", c, m, a)
	}
	if !(c.SLRate > m.SLRate && m.SLRate > a.SLRate) || !(c.TPRate < m.TPRate && m.TPRate < a.TPRate) || m.SLRate >= 29850 || m.TPRate <= 29851 {
		t.Fatalf("ordering: sl %v %v %v tp %v %v %v", c.SLRate, m.SLRate, a.SLRate, c.TPRate, m.TPRate, a.TPRate)
	}
	if m.RR < 1.9 || m.RR > 2.1 || m.MaxLossUSD <= 0 || m.TargetUSD <= 0 || m.Profile != stops.Moderate {
		t.Fatalf("moderate %+v", m)
	}
	if len(rec.Warnings) != 0 {
		t.Fatalf("warnings %v", rec.Warnings)
	}
	if h.fb.limitsCalls != 1 {
		t.Fatalf("limits read %d times", h.fb.limitsCalls)
	}

	// A protect on SPX500: the held position, measured from its open rate
	// and the bid, with what the account holds (nothing yet).
	code, rec, b = h.recommend(t, recommendBody("spx500", "protect", 0, ""))
	if code != 200 {
		t.Fatalf("%d %s", code, b)
	}
	if rec.Position.PositionID != "3616683295" || rec.Units != 0.140735 || !near(rec.Context.Entry, 7816.07, 1e-6) || rec.Context.Mark != 7700 {
		t.Fatalf("protect recommendation %+v", rec)
	}
	if rec.Context.Before == nil || rec.Context.Before.Protected || rec.Context.Before.StopLossRate != 0 {
		t.Fatalf("before %+v", rec.Context.Before)
	}
	if rec.Context.Bounds.MinStopLossPct != 0 {
		t.Fatalf("spx500 has no venue minimum: %+v", rec.Context.Bounds)
	}
	for p, lv := range rec.Profiles {
		if !lv.Feasible || lv.SLRate >= 7700 || lv.TPRate <= 7700 {
			t.Fatalf("%s: %+v", p, lv)
		}
	}

	// Errors: nothing to protect on a flat book (409), a close is not a
	// recommendation (400), the gates (401, 403), the venue (502).
	if code, _, b := h.recommend(t, recommendBody("nsdq100", "protect", 0, "")); code != 409 || !strings.Contains(string(b), "no position") {
		t.Fatalf("flat protect: %d %s", code, b)
	}
	if code, _, b := h.recommend(t, recommendBody("spx500", "protect", 0, "999")); code != 409 {
		t.Fatalf("wrong position: %d %s", code, b)
	}
	if code, _, b := h.recommend(t, recommendBody("spx500", "close", 0, "")); code != 400 || !strings.Contains(string(b), "side") {
		t.Fatalf("close: %d %s", code, b)
	}
	if code, b := post(t, h.ts, ctl{path: "/api/ui/etoro/trades/recommend?ledger=etoro-demo", body: recommendBody("nsdq100", "open", 0, "")}); code != 401 {
		t.Fatalf("no token: %d %s", code, b)
	}
	if e := h.audit(t); len(e) == 0 || e[0].Action != "trade.recommend" || e[0].Outcome != audit.Denied {
		t.Fatalf("a denied recommend is audited: %+v", e)
	}
	h.fb.quoteErr = errBoom
	if code, _, b := h.recommend(t, recommendBody("nsdq100", "open", 0, "")); code != 502 {
		t.Fatalf("quote failure: %d %s", code, b)
	}
	h.fb.quoteErr = nil
	h.srv.Trading = nil
	if code, _, _ := h.recommend(t, recommendBody("nsdq100", "open", 0, "")); code != 403 {
		t.Fatalf("trading off: %d", code)
	}
}

func TestTradePrepareOpenCarriesStop(t *testing.T) {
	h := tradeServer(t, tradeNow)

	// No profile named: the default applies, with a warning; the risk check
	// saw the stop; the token binds it.
	code, pv, b := h.prepare(t, prepareBody("nsdq100", "open", 1100, ""))
	if code != 200 || pv.Verdict != VerdictAllow || pv.Token == "" {
		t.Fatalf("%d %+v %s", code, pv, b)
	}
	if pv.Protection == nil || pv.Protection.Profile != "moderate" || !pv.Protection.Levels.Feasible || pv.Protection.Volatility == nil {
		t.Fatalf("protection %+v", pv.Protection)
	}
	sl, tp := pv.Protection.Levels.SLRate, pv.Protection.Levels.TPRate
	if !(sl > 0 && sl < 29851) || !(tp > 29851) || pv.Risk.Order.StopLoss != sl || pv.Risk.Order.TakeProfit != tp || pv.Risk.PolicyVersion != "etoro-demo-2026-10-19" {
		t.Fatalf("risk order %+v (sl %v tp %v)", pv.Risk.Order, sl, tp)
	}
	if !hasReason(pv.Warnings, "no stop requested: the moderate profile") {
		t.Fatalf("warnings %v", pv.Warnings)
	}
	c, err := h.srv.Trading.verify(pv.Token, testToken)
	if err != nil || c.SLRate != sl || c.TPRate != tp || c.Profile != "moderate" || c.ATR <= 0 {
		t.Fatalf("claims %+v %v", c, err)
	}

	// Place: the broker gets the stop with the order, the ledger records it.
	code, resp, b := h.place(t, placeBody(pv.Token, "PLACE NSDQ100", "protected open from the console", "diego"))
	if code != 200 || resp.Status != "executed" || !resp.LedgerRecorded {
		t.Fatalf("%d %+v %s", code, resp, b)
	}
	if len(h.fb.opens) != 1 || h.fb.opens[0].StopLoss != sl || h.fb.opens[0].TakeProfit != tp {
		t.Fatalf("open request %+v", h.fb.opens)
	}
	if resp.PositionAfter.StopLossRate != sl || resp.PositionAfter.TakeProfitRate != tp || resp.PositionAfter.StopType != "fixed" || resp.Protection == nil || resp.Protection.Profile != "moderate" || resp.Protection.MaxLossUSD <= 0 {
		t.Fatalf("position after %+v protection %+v", resp.PositionAfter, resp.Protection)
	}
	recs := h.records(t)
	last := recs[len(recs)-1]
	var ui *etoroledger.Record
	for i := range recs {
		if recs[i].IsUI() && recs[i].Book == "nsdq100" {
			ui = &recs[i]
		}
	}
	if ui == nil || !ui.Demo.PositionAfter.Protected() || ui.Demo.PositionAfter.StopLossRate != sl || ui.Demo.Protection == nil || ui.Demo.Risk.Order.StopLoss != sl {
		t.Fatalf("ledger line %+v (last %s)", ui, last.Decision.BarDate)
	}
	if n := etoroledger.LegsOn(recs, "nsdq100", "2026-10-19"); n != 1 {
		t.Fatalf("legs %d", n)
	}
	if p := etoroledger.LastDemoPosition(recs, "nsdq100"); !p.Protected() {
		t.Fatalf("latest position %+v", p)
	}
}

func TestTradePrepareProfilesAndClamps(t *testing.T) {
	h := tradeServer(t, tradeNow)
	// Custom distances go through the guards: 0.5 % is widened to the 1 %
	// venue floor on NSDQ100; 15 % is tightened to the policy's 10 %.
	_, pv, _ := h.prepare(t, prepareProtected("nsdq100", "open", 1100, "", "custom", 0.005, 0.03))
	if pv.Verdict != VerdictAllow || pv.Protection == nil || pv.Protection.Profile != "custom" {
		t.Fatalf("%+v", pv)
	}
	if lv := pv.Protection.Levels; !near(lv.SLPct, 0.01, 1e-4) || len(lv.Clamps) != 1 || !strings.Contains(lv.Clamps[0], "floor") || !near(lv.TPPct, 0.03, 1e-4) {
		t.Fatalf("floor clamp %+v", lv)
	}
	_, pv, _ = h.prepare(t, prepareProtected("nsdq100", "open", 1100, "", "custom", 0.15, 0))
	if lv := pv.Protection.Levels; pv.Verdict != VerdictAllow || lv.SLPct < 0.0999 || lv.SLPct > 0.1 || len(lv.Clamps) != 1 || !strings.Contains(lv.Clamps[0], "10.0% maximum") || lv.TPRate != 0 {
		t.Fatalf("cap clamp %+v", lv)
	}
	// Named profiles: aggressive is wider than conservative; percentages
	// are refused with a named profile; an unknown profile is refused.
	_, cons, _ := h.prepare(t, prepareProtected("nsdq100", "open", 1100, "", "conservative", 0, 0))
	_, aggr, _ := h.prepare(t, prepareProtected("nsdq100", "open", 1100, "", "aggressive", 0, 0))
	if cons.Protection.Levels.SLRate <= aggr.Protection.Levels.SLRate || cons.Protection.Levels.TPRate >= aggr.Protection.Levels.TPRate {
		t.Fatalf("profiles: %+v vs %+v", cons.Protection.Levels, aggr.Protection.Levels)
	}
	if hasReason(cons.Warnings, "no stop requested") {
		t.Fatalf("a named profile is not a default: %v", cons.Warnings)
	}
	for _, body := range []string{
		prepareProtected("nsdq100", "open", 1100, "", "aggressive", 0.02, 0),
		prepareProtected("nsdq100", "open", 1100, "", "yolo", 0, 0),
		prepareProtected("nsdq100", "open", 1100, "", "custom", 0, 0.02),
		prepareProtected("spx500", "close", 0, "3616683295", "moderate", 0, 0),
	} {
		if code, b := post(t, h.ts, ctl{path: "/api/ui/etoro/trades/prepare?ledger=etoro-demo", token: testToken, origin: "http://127.0.0.1:5173", body: body}); code != 400 {
			t.Fatalf("%s: %d %s", body, code, b)
		}
	}

	// The loss cap against equity can make a stop infeasible: with 1,000
	// USD of equity, 0.5 % is 5 USD, a 0.45 % stop, under NSDQ100's 1 %
	// floor. The verdict blocks: nothing goes out without a feasible stop.
	h.fb.equity = 1000
	_, pv, _ = h.prepare(t, prepareBody("nsdq100", "open", 1100, ""))
	if pv.Verdict != VerdictBlock || !hasReason(pv.Reasons, "protection: no stop satisfies") || pv.Token != "" {
		t.Fatalf("infeasible: %+v", pv)
	}
	h.fb.equity = 99949.9

	// Without daily bars there is no ATR: the fallback 8 % stop applies,
	// with warnings, and the open is still allowed (and protected).
	h.srv.Ledgers[1].Store = store.New(filepath.Join(h.dir, "ledger.jsonl"), t.TempDir())
	_, pv, _ = h.prepare(t, prepareBody("nsdq100", "open", 1100, ""))
	if pv.Verdict != VerdictAllow || pv.Protection == nil || pv.Protection.Volatility != nil || !hasReason(pv.Warnings, "no daily bars") {
		t.Fatalf("fallback %+v %v", pv.Protection, pv.Warnings)
	}
	if lv := pv.Protection.Levels; !near(lv.SLPct, 0.08, 1e-4) || lv.TPRate != 0 || !hasReason(lv.Clamps, "no ATR available") {
		t.Fatalf("fallback levels %+v", lv)
	}
	// And without venue bounds the fallback bounds apply, with a warning.
	h.fb.limitsErr = errBoom
	_, pv, _ = h.prepare(t, prepareBody("nsdq100", "open", 1100, ""))
	if pv.Protection.BoundsSource != "fallback" || !hasReason(pv.Warnings, "venue bounds") || pv.Verdict != VerdictAllow {
		t.Fatalf("fallback bounds %+v %v", pv.Protection, pv.Warnings)
	}
}

func TestTradeProtectFlow(t *testing.T) {
	h := tradeServer(t, tradeNow)
	// SPX500 is held (3616683295) without a stop.
	code, pv, b := h.prepare(t, prepareProtected("spx500", "protect", 0, "3616683295", "", 0, 0))
	if code != 200 || pv.Verdict != VerdictAllow || pv.Token == "" || pv.Confirmation != "PROTECT SPX500" {
		t.Fatalf("%d %+v %s", code, pv, b)
	}
	if pv.Protection == nil || !near(pv.Protection.Entry, 7816.07, 1e-6) || pv.Protection.Mark != 7700 || pv.Protection.Before == nil || pv.Protection.Before.Protected {
		t.Fatalf("protection %+v", pv.Protection)
	}
	sl, tp := pv.Protection.Levels.SLRate, pv.Protection.Levels.TPRate
	if !(sl > 0 && sl < 7700) || tp <= 7700 || pv.Units != 0.140735 || pv.Risk == nil || !pv.Risk.Allowed || pv.Risk.Order.Side != "protect" {
		t.Fatalf("levels %v %v risk %+v", sl, tp, pv.Risk)
	}
	if !strings.HasPrefix(pv.ClientRef, "ui-spx500-2026-10-19-protect-") {
		t.Fatalf("ref %s", pv.ClientRef)
	}
	if hasReason(pv.Warnings, "no stop requested") {
		t.Fatalf("a protect never says it defaulted: %v", pv.Warnings)
	}

	// The phrase must match; then the edit goes out and is recorded.
	if code, _, b := h.place(t, placeBody(pv.Token, "PLACE SPX500", "wrong phrase on purpose", "diego")); code != 400 {
		t.Fatalf("wrong phrase: %d %s", code, b)
	}
	code, resp, b := h.place(t, placeBody(pv.Token, "PROTECT SPX500", "stop on the open position", "diego"))
	if code != 200 || resp.Status != "executed" || !resp.LedgerRecorded || resp.Side != "protect" || resp.OrderID != "op-"+pv.ClientRef {
		t.Fatalf("%d %+v %s", code, resp, b)
	}
	if len(h.fb.protects) != 1 || h.fb.protects[0].StopLoss != sl || h.fb.protects[0].TakeProfit != tp || h.fb.protects[0].PositionID != "3616683295" || len(h.fb.opens) != 0 {
		t.Fatalf("protect request %+v opens %d", h.fb.protects, len(h.fb.opens))
	}
	if resp.PositionAfter.StopLossRate != sl || resp.PositionAfter.TakeProfitRate != tp || resp.PositionAfter.PositionID != "3616683295" || resp.Protection == nil || resp.Protection.StopLossBefore != 0 || len(resp.Warnings) != 0 {
		t.Fatalf("after %+v protection %+v warnings %v", resp.PositionAfter, resp.Protection, resp.Warnings)
	}
	recs := h.records(t)
	var prot *etoroledger.Record
	for i := range recs {
		if recs[i].IsUI() && recs[i].Demo.Action == etoroledger.ActionProtect {
			prot = &recs[i]
		}
	}
	if prot == nil || prot.Book != "spx500" || prot.Demo.Order.Kind != etoroledger.ActionProtect || prot.Demo.PositionBefore.StopLossRate != 0 ||
		prot.Demo.PositionAfter.StopLossRate != sl || prot.Demo.Protection == nil || prot.Demo.Protection.Profile != "moderate" || prot.Decision.Action != "protect" || prot.Operator.By != "diego" {
		t.Fatalf("protect line %+v", prot)
	}
	if n := etoroledger.LegsOn(recs, "spx500", "2026-10-19"); n != 0 {
		t.Fatalf("a protect is not a leg: %d", n)
	}
	if p := etoroledger.LastDemoPosition(recs, "spx500"); p.State != "long" || !p.Protected() || p.StopLossRate != sl {
		t.Fatalf("latest position %+v", p)
	}
	if e := h.audit(t); e[0].Action != "trade.place" || e[0].Outcome != audit.Done || !strings.Contains(e[0].Detail, "protect spx500") || !strings.Contains(e[0].Detail, "stop 0.00 ->") {
		t.Fatalf("audit %+v", e[0])
	}

	// A second prepare sees the stop in place and warns when a custom
	// level would loosen it.
	_, pv2, _ := h.prepare(t, prepareProtected("spx500", "protect", 0, "3616683295", "custom", 0.09, 0))
	if pv2.Protection.Before == nil || !pv2.Protection.Before.Protected || pv2.Protection.Before.StopLossRate != sl || !hasReason(pv2.Warnings, "loosens the protection") || pv2.Verdict != VerdictAllow {
		t.Fatalf("second prepare %+v %v", pv2.Protection.Before, pv2.Warnings)
	}

	// Outside the cash session a protect is still allowed (an edit, not an
	// order); the open on the same page is not.
	h.now = time.Date(2026, 10, 19, 2, 0, 0, 0, time.UTC)
	_, pv3, _ := h.prepare(t, prepareProtected("spx500", "protect", 0, "3616683295", "", 0, 0))
	if pv3.Verdict != VerdictAllow {
		t.Fatalf("protect outside the session: %+v", pv3.Reasons)
	}
	if code, resp, b := h.place(t, placeBody(pv3.Token, "PROTECT SPX500", "overnight stop before the open", "diego")); code != 200 || resp.Status != "executed" {
		t.Fatalf("place outside the session: %d %+v %s", code, resp, b)
	}
	if _, pvo, _ := h.prepare(t, prepareBody("nsdq100", "open", 1100, "")); pvo.Verdict != VerdictBlock || !hasReason(pvo.Reasons, "market_closed") {
		t.Fatalf("open outside the session must still block: %+v", pvo.Reasons)
	}
	h.now = tradeNow

	// Halted: prepare blocks, place is refused at the re-check.
	_, pv4, _ := h.prepare(t, prepareProtected("spx500", "protect", 0, "3616683295", "", 0, 0))
	writeFile(t, filepath.Join(h.dir, "risk-state.json"), `{"halted":true,"reason":"drill","by":"diego","at":"2026-10-19T15:30:00Z"}`)
	if code, _, b := h.place(t, placeBody(pv4.Token, "PROTECT SPX500", "halted, must be refused", "diego")); code != 422 || !strings.Contains(string(b), "halted") {
		t.Fatalf("halted place: %d %s", code, b)
	}
	if _, pv5, _ := h.prepare(t, prepareProtected("spx500", "protect", 0, "3616683295", "", 0, 0)); pv5.Verdict != VerdictBlock || !hasReason(pv5.Reasons, "halted") {
		t.Fatalf("halted prepare %+v", pv5.Reasons)
	}
	writeFile(t, filepath.Join(h.dir, "risk-state.json"), `{"halted":false}`)

	// A flat book has nothing to protect: a block, not an error.
	if _, pv6, _ := h.prepare(t, prepareProtected("nsdq100", "protect", 0, "1", "", 0, 0)); pv6.Verdict != VerdictBlock || !hasReason(pv6.Reasons, "no position on NSDQ100 to protect") {
		t.Fatalf("flat protect %+v", pv6.Reasons)
	}
}

func TestTradeProtectPendingAndFailure(t *testing.T) {
	h := tradeServer(t, tradeNow)
	// Pending: eToro accepted, the portfolio has not shown it. Recorded as
	// sent, with a warning; 200.
	h.fb.protectPending = true
	_, pv, _ := h.prepare(t, prepareProtected("spx500", "protect", 0, "3616683295", "", 0, 0))
	code, resp, b := h.place(t, placeBody(pv.Token, "PROTECT SPX500", "pending edit case", "diego"))
	if code != 200 || resp.Status != "pending" || !resp.LedgerRecorded || len(resp.Warnings) != 1 || !strings.Contains(resp.Warnings[0], "did not show") {
		t.Fatalf("%d %+v %s", code, resp, b)
	}
	if resp.PositionAfter.StopLossRate != pv.Protection.Levels.SLRate {
		t.Fatalf("pending records the rates sent: %+v", resp.PositionAfter)
	}
	recs := h.records(t)
	if p := etoroledger.LastDemoPosition(recs, "spx500"); p.StopLossRate != pv.Protection.Levels.SLRate {
		t.Fatalf("ledger %+v", p)
	}

	// A rejection: failed place, the intent stays, no ledger line.
	h.fb.protectPending, h.fb.protectErr = false, broker.ErrPositionNotOpen
	_, pv, _ = h.prepare(t, prepareProtected("spx500", "protect", 0, "3616683295", "", 0, 0))
	code, resp, b = h.place(t, placeBody(pv.Token, "PROTECT SPX500", "rejected edit case", "diego"))
	if code != 502 || resp.Status != "rejected" || resp.LedgerRecorded || !strings.Contains(resp.Error, "stays on disk") {
		t.Fatalf("%d %+v %s", code, resp, b)
	}
	if _, ok, _ := etoroledger.LoadIntent(etoroledger.IntentDir(filepath.Join(h.dir, "ledger.jsonl")), pv.ClientRef); !ok {
		t.Fatal("the intent must stay on disk")
	}
	if n := len(h.records(t)); n != len(recs) {
		t.Fatalf("a failed protect must not write a ledger line: %d vs %d", n, len(recs))
	}
	if e := h.audit(t); e[0].Outcome != audit.Failed || e[1].Outcome != audit.Requested {
		t.Fatalf("audit %+v %+v", e[0], e[1])
	}

	// A broker without the editor: prepare blocks before anything is sent.
	h.fb.protectErr = nil
	plain := &plainBroker{h.fb}
	tr, err := newEtoroTrading(plain)
	if err != nil {
		t.Fatal(err)
	}
	h.srv.Trading = tr
	if _, pv, _ := h.prepare(t, prepareProtected("spx500", "protect", 0, "3616683295", "", 0, 0)); pv.Verdict != VerdictBlock || !hasReason(pv.Reasons, "cannot edit") {
		t.Fatalf("no editor %+v", pv.Reasons)
	}
	// An open through it still gets a stop (sent with the order), with the
	// fallback bounds since it cannot read the venue's.
	if _, pv, _ := h.prepare(t, prepareBody("nsdq100", "open", 1100, "")); pv.Verdict != VerdictAllow || pv.Protection.BoundsSource != "fallback" {
		t.Fatalf("plain open %+v %v", pv.Protection, pv.Reasons)
	}
}

// plainBroker is the fake without the optional protection interfaces.
type plainBroker struct{ f *fakeTradeBroker }

func (p *plainBroker) Venue() string { return p.f.Venue() }
func (p *plainBroker) Quote(ctx context.Context, in broker.Instrument) (broker.Quote, error) {
	return p.f.Quote(ctx, in)
}
func (p *plainBroker) Open(ctx context.Context, req broker.OpenRequest) (broker.Order, error) {
	return p.f.Open(ctx, req)
}
func (p *plainBroker) Close(ctx context.Context, req broker.CloseRequest) (broker.Order, error) {
	return p.f.Close(ctx, req)
}
func (p *plainBroker) Positions(ctx context.Context) ([]broker.Position, error) {
	return p.f.Positions(ctx)
}
func (p *plainBroker) Account(ctx context.Context) (broker.Account, error) { return p.f.Account(ctx) }
func (p *plainBroker) PreviewOpen(ctx context.Context, req broker.OpenRequest) (broker.Costs, error) {
	return p.f.PreviewOpen(ctx, req)
}
