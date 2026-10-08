package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"bitso-trading-platform/shared/pkg/risk"
	"bitso-trading-platform/ui-api/internal/audit"
	"bitso-trading-platform/ui-api/internal/executor"
	"bitso-trading-platform/ui-api/internal/store"
)

// fakeExecutor mimics strategy-executor's strategy API with the hold list.
type fakeExecutor struct {
	mu         sync.Mutex
	strategies map[string]*executor.Strategy
	holds      map[string]executor.Hold
	calls      []executor.LifecycleRequest
	failNext   int // answer the next lifecycle call with this status
}

func newFakeExecutor() *fakeExecutor {
	return &fakeExecutor{
		strategies: map[string]*executor.Strategy{
			"mr_btc":  {Name: "mr_btc", Type: "mean_reversion", Version: "1.0.0", Book: "btc_mxn", Enabled: true, Running: true},
			"mom_eth": {Name: "mom_eth", Type: "momentum", Version: "1.0.0", Book: "eth_mxn", Enabled: true, Parameters: map[string]interface{}{"dry_run": true}},
			"lp_btc": {Name: "lp_btc", Type: "limit_profit", Version: "1.0.0", Book: "btc_mxn", Enabled: true, Running: true,
				State: executor.State{HasPosition: true, PositionSide: "long", PositionSize: 0.001}},
		},
		holds: map[string]executor.Hold{"old_one": {Reason: "retired", By: "ops", At: "2026-10-01T00:00:00Z"}},
	}
}

func (f *fakeExecutor) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/strategies")
	if path == "" && r.Method == http.MethodGet {
		list := executor.List{Strategies: []executor.Strategy{}, Holds: f.holds}
		for _, s := range f.strategies {
			list.Strategies = append(list.Strategies, *s)
		}
		list.Count = len(list.Strategies)
		_ = json.NewEncoder(w).Encode(list)
		return
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	s, ok := f.strategies[parts[0]]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "strategy not found", "code": "not_found"})
		return
	}
	if len(parts) == 1 {
		_ = json.NewEncoder(w).Encode(s)
		return
	}
	var req executor.LifecycleRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	f.calls = append(f.calls, req)
	if f.failNext != 0 {
		w.WriteHeader(f.failNext)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed: injected", "code": "held"})
		f.failNext = 0
		return
	}
	switch parts[1] {
	case "stop":
		was := s.Running
		s.Running = false
		h := executor.Hold{Reason: req.Reason, By: req.By, At: "2026-10-02T03:00:00Z"}
		f.holds[s.Name] = h
		s.Hold = &h
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"name": s.Name, "status": "stopped", "held": true, "was_running": was})
	case "start":
		delete(f.holds, s.Name)
		s.Hold = nil
		s.Running = true
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"name": s.Name, "status": "started"})
	}
}

func strategyServer(t *testing.T, token string, fake http.Handler) (ts *httptest.Server, dir string) {
	t.Helper()
	dir = t.TempDir()
	src, err := os.ReadFile("testdata/ledger.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"stage", "drill"} {
		if err := os.MkdirAll(filepath.Join(dir, n), 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(dir, n, "ledger.jsonl"), string(src))
	}
	var ex *executor.Client
	if fake != nil {
		up := httptest.NewServer(fake)
		t.Cleanup(up.Close)
		if ex, err = executor.New(up.URL); err != nil {
			t.Fatal(err)
		}
	}
	ts = newTestServer(t, fixedNow, func(s *Server) {
		s.Ledgers = []Ledger{
			{Name: "stage", Store: store.New(filepath.Join(dir, "stage", "ledger.jsonl"), "testdata/candles")},
			{Name: "drill", Store: store.New(filepath.Join(dir, "drill", "ledger.jsonl"), "testdata/candles")},
		}
		s.OperatorToken = token
		s.Executor = ex
	})
	return ts, dir
}

func getStrategies(t *testing.T, ts *httptest.Server) StrategiesInfo {
	t.Helper()
	res, err := http.Get(ts.URL + "/api/ui/strategies")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("GET /strategies = %d", res.StatusCode)
	}
	var info StrategiesInfo
	if err := json.NewDecoder(res.Body).Decode(&info); err != nil {
		t.Fatal(err)
	}
	return info
}

func sbody(reason, by, confirm string, ack bool) string {
	b, _ := json.Marshal(StrategyControlRequest{Reason: reason, By: by, Confirm: confirm, AckPosition: ack})
	return string(b)
}

func TestStrategiesWithoutExecutor(t *testing.T) {
	ts, _ := strategyServer(t, "", nil)
	info := getStrategies(t, ts)
	if len(info.Ledgers) != 2 || info.Ledgers[0].Name != "stage" || info.Ledgers[1].Name != "drill" {
		t.Fatalf("ledgers = %+v", info.Ledgers)
	}
	if info.Executor.Configured || info.Executor.ControlsEnabled || !strings.Contains(info.Executor.DisabledReason, "-strategy-executor-url") {
		t.Fatalf("executor = %+v", info.Executor)
	}
	if info.KillSwitch.Enabled || !strings.Contains(info.KillSwitch.DisabledReason, "-operator-token-file") ||
		info.KillSwitch.Confirm != HaltAllConfirm || len(info.KillSwitch.Targets) != 2 {
		t.Fatalf("kill switch = %+v", info.KillSwitch)
	}
	if len(info.Strategies) != 0 || len(info.Audit) != 0 || info.AuditPath != "" {
		t.Fatalf("strategies/audit = %+v / %+v", info.Strategies, info.Audit)
	}
	// Strategy controls are refused before any audit when off.
	code, _ := post(t, ts, ctl{path: "/api/ui/strategies/mr_btc/stop", token: testToken, body: sbody("investigating drift", "diego", "mr_btc", false)})
	if code != http.StatusForbidden {
		t.Fatalf("stop without executor = %d", code)
	}
}

func TestStrategiesListFromExecutor(t *testing.T) {
	fake := newFakeExecutor()
	ts, dir := strategyServer(t, testToken, fake)
	info := getStrategies(t, ts)
	if !info.Executor.Configured || !info.Executor.Reachable || !info.Executor.ControlsEnabled || !info.Executor.HoldsSupported {
		t.Fatalf("executor = %+v", info.Executor)
	}
	names := []string{}
	for _, s := range info.Strategies {
		names = append(names, s.Name)
	}
	if strings.Join(names, ",") != "lp_btc,mom_eth,mr_btc" {
		t.Fatalf("names (sorted) = %v", names)
	}
	if !info.Strategies[0].HasPosition || info.Strategies[1].DryRun == nil || !*info.Strategies[1].DryRun || info.Strategies[2].DryRun != nil {
		t.Fatalf("views = %+v", info.Strategies)
	}
	if len(info.Holds) != 1 || info.Holds[0].Name != "old_one" {
		t.Fatalf("orphan holds = %+v", info.Holds)
	}
	if want := filepath.Join(dir, "stage", StrategyAuditFile); info.AuditPath != want {
		t.Fatalf("audit path = %q, want %q", info.AuditPath, want)
	}
	if !info.KillSwitch.Enabled || len(info.KillSwitch.AlreadyHalted) != 0 {
		t.Fatalf("kill switch = %+v", info.KillSwitch)
	}
}

func TestStrategyStopStartAudited(t *testing.T) {
	fake := newFakeExecutor()
	ts, dir := strategyServer(t, testToken, fake)
	ap := filepath.Join(dir, "stage", StrategyAuditFile)

	// Gates: token, confirm.
	code, _ := post(t, ts, ctl{path: "/api/ui/strategies/mr_btc/stop", body: sbody("investigating drift", "diego", "mr_btc", false)})
	if code != http.StatusUnauthorized {
		t.Fatalf("no token = %d", code)
	}
	code, b := post(t, ts, ctl{path: "/api/ui/strategies/mr_btc/stop", token: testToken, body: sbody("investigating drift", "diego", "mr", false)})
	if code != http.StatusBadRequest || !strings.Contains(string(b), "type the strategy name") {
		t.Fatalf("wrong confirm = %d %s", code, b)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("executor called before the gates passed: %+v", fake.calls)
	}

	code, b = post(t, ts, ctl{path: "/api/ui/strategies/mr_btc/stop", token: testToken, origin: "http://127.0.0.1:5173",
		body: sbody("investigating drift", "diego", "mr_btc", false)})
	if code != http.StatusOK {
		t.Fatalf("stop = %d %s", code, b)
	}
	var resp StrategyControlResponse
	_ = json.Unmarshal(b, &resp)
	if resp.Action != "stop" || resp.Audit.Outcome != audit.Done || resp.Audit.Strategy != "mr_btc" || resp.Upstream["held"] != true {
		t.Fatalf("resp = %+v", resp)
	}
	if c := fake.calls[0]; !c.Hold || c.ReleaseHold || c.By != "diego" || c.Reason != "investigating drift" {
		t.Fatalf("executor got %+v, want an operator hold", c)
	}

	// Stopping a held strategy again is refused by ui-api.
	code, b = post(t, ts, ctl{path: "/api/ui/strategies/mr_btc/stop", token: testToken, body: sbody("investigating drift", "diego", "mr_btc", false)})
	if code != http.StatusConflict || !strings.Contains(string(b), "already stopped and held by diego") {
		t.Fatalf("second stop = %d %s", code, b)
	}

	code, b = post(t, ts, ctl{path: "/api/ui/strategies/mr_btc/start", token: testToken, body: sbody("drift explained", "diego", "mr_btc", false)})
	if code != http.StatusOK {
		t.Fatalf("start = %d %s", code, b)
	}
	if c := fake.calls[1]; !c.ReleaseHold || c.Hold {
		t.Fatalf("executor got %+v, want release_hold", c)
	}
	code, _ = post(t, ts, ctl{path: "/api/ui/strategies/mr_btc/start", token: testToken, body: sbody("drift explained", "diego", "mr_btc", false)})
	if code != http.StatusConflict {
		t.Fatalf("start while running = %d", code)
	}

	entries, err := audit.Tail(ap, 50)
	if err != nil {
		t.Fatal(err)
	}
	// newest first: refused start, done start, requested start, refused stop,
	// done stop, requested stop, refused (confirm), denied (token).
	want := []string{"refused", "done", "requested", "refused", "done", "requested", "refused", "denied"}
	if len(entries) != len(want) {
		t.Fatalf("audit has %d entries: %+v", len(entries), entries)
	}
	for i, o := range want {
		if entries[i].Outcome != o || entries[i].Strategy != "mr_btc" || entries[i].Ledger != "" {
			t.Fatalf("entry %d = %+v, want outcome %s", i, entries[i], o)
		}
	}
	if entries[1].ID == "" || entries[1].ID != entries[2].ID || entries[1].Action != "strategy_start" || entries[1].UpstreamStatus != 200 {
		t.Fatalf("start pair = %+v / %+v", entries[1], entries[2])
	}
	// The ledger audit logs are untouched.
	if _, err := os.Stat(filepath.Join(dir, "stage", audit.FileName)); !os.IsNotExist(err) {
		t.Fatalf("ledger audit log was written: %v", err)
	}
}

func TestStrategyStopWithPositionNeedsAck(t *testing.T) {
	fake := newFakeExecutor()
	ts, dir := strategyServer(t, testToken, fake)
	code, b := post(t, ts, ctl{path: "/api/ui/strategies/lp_btc/stop", token: testToken, body: sbody("exchange incident", "diego", "lp_btc", false)})
	if code != http.StatusConflict || !strings.Contains(string(b), "open position") {
		t.Fatalf("stop with position = %d %s", code, b)
	}
	code, b = post(t, ts, ctl{path: "/api/ui/strategies/lp_btc/stop", token: testToken, body: sbody("exchange incident", "diego", "lp_btc", true)})
	if code != http.StatusOK {
		t.Fatalf("acknowledged stop = %d %s", code, b)
	}
	entries, _ := audit.Tail(filepath.Join(dir, "stage", StrategyAuditFile), 1)
	if !strings.Contains(entries[0].Detail, "acknowledged") {
		t.Fatalf("detail = %q", entries[0].Detail)
	}
}

func TestStrategyControlErrors(t *testing.T) {
	fake := newFakeExecutor()
	ts, dir := strategyServer(t, testToken, fake)

	code, _ := post(t, ts, ctl{path: "/api/ui/strategies/ghost/stop", token: testToken, body: sbody("investigating drift", "diego", "ghost", false)})
	if code != http.StatusNotFound {
		t.Fatalf("unknown strategy = %d", code)
	}
	code, _ = post(t, ts, ctl{path: "/api/ui/strategies/bad%20name/stop", token: testToken, body: sbody("investigating drift", "diego", "bad name", false)})
	if code != http.StatusBadRequest {
		t.Fatalf("bad name = %d", code)
	}

	// An executor 409 is passed through and audited as failed.
	fake.failNext = http.StatusConflict
	code, b := post(t, ts, ctl{path: "/api/ui/strategies/mom_eth/start", token: testToken, body: sbody("start for a test", "diego", "mom_eth", false)})
	if code != http.StatusConflict || !strings.Contains(string(b), "injected") {
		t.Fatalf("upstream 409 = %d %s", code, b)
	}
	entries, _ := audit.Tail(filepath.Join(dir, "stage", StrategyAuditFile), 1)
	if entries[0].Outcome != audit.Failed || entries[0].UpstreamStatus != http.StatusConflict {
		t.Fatalf("failed entry = %+v", entries[0])
	}

	// Only start and stop accept POST.
	code, _ = post(t, ts, ctl{path: "/api/ui/strategies/mom_eth/delete", token: testToken, body: "{}"})
	if code != http.StatusMethodNotAllowed {
		t.Fatalf("POST delete = %d", code)
	}
}

func TestStrategiesExecutorDown(t *testing.T) {
	ex, err := executor.New("http://127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	ts2 := newTestServer(t, fixedNow, func(s *Server) {
		s.Store = store.New(t.TempDir()+"/ledger.jsonl", "")
		s.OperatorToken = testToken
		s.Executor = ex
	})
	info := getStrategies(t, ts2)
	if !info.Executor.Configured || info.Executor.Reachable || info.Executor.Error == "" || info.Executor.ControlsEnabled {
		t.Fatalf("executor = %+v", info.Executor)
	}
}

func TestExecutorURLMustBeLoopback(t *testing.T) {
	for _, u := range []string{"http://10.0.0.5:8081", "http://example.com", "ftp://127.0.0.1", "http://u:p@127.0.0.1:1"} {
		if _, err := executor.New(u); err == nil {
			t.Errorf("executor.New(%q) accepted", u)
		}
	}
	for _, u := range []string{"http://127.0.0.1:8081", "http://localhost:8081/", "http://[::1]:8081"} {
		if _, err := executor.New(u); err != nil {
			t.Errorf("executor.New(%q): %v", u, err)
		}
	}
}

func TestHaltAll(t *testing.T) {
	ts, dir := strategyServer(t, testToken, nil)
	drillHalt := filepath.Join(dir, "drill", "risk-state.json")
	writeFile(t, drillHalt, `{"halted":true,"reason":"drill already halted","by":"ops","at":"2026-10-01T00:00:00Z"}`)

	code, b := post(t, ts, ctl{path: "/api/ui/risk/halt-all", token: testToken, body: body("exchange incident", "diego", "halt all")})
	if code != http.StatusBadRequest || !strings.Contains(string(b), "type the phrase") {
		t.Fatalf("wrong phrase = %d %s", code, b)
	}
	code, _ = post(t, ts, ctl{path: "/api/ui/risk/halt-all", body: body("exchange incident", "diego", HaltAllConfirm)})
	if code != http.StatusUnauthorized {
		t.Fatalf("no token = %d", code)
	}

	code, b = post(t, ts, ctl{path: "/api/ui/risk/halt-all", token: testToken, body: body("exchange incident", "diego", HaltAllConfirm)})
	if code != http.StatusOK {
		t.Fatalf("halt-all = %d %s", code, b)
	}
	var resp HaltAllResponse
	_ = json.Unmarshal(b, &resp)
	if resp.Group == "" || len(resp.Results) != 2 ||
		resp.Results[0].Ledger != "stage" || resp.Results[0].Outcome != "halted" || !resp.Results[0].HaltFile.Halted ||
		resp.Results[1].Ledger != "drill" || resp.Results[1].Outcome != "already_halted" {
		t.Fatalf("resp = %+v", resp)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "stage", "risk-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	h, err := risk.ParseHaltState(raw)
	if err != nil || !h.Halted || h.By != "diego" || h.Reason != "exchange incident" {
		t.Fatalf("stage halt file = %+v, %v", h, err)
	}
	// The drill halt is untouched.
	if raw, _ := os.ReadFile(drillHalt); !strings.Contains(string(raw), "drill already halted") {
		t.Fatalf("drill halt file changed: %s", raw)
	}

	stage, _ := audit.Tail(filepath.Join(dir, "stage", audit.FileName), 10)
	drill, _ := audit.Tail(filepath.Join(dir, "drill", audit.FileName), 10)
	// stage: done, requested, refused (phrase), denied (token); drill: refused
	// (already halted), refused (phrase), denied (token).
	if len(stage) != 4 || stage[0].Outcome != audit.Done || stage[0].Group != resp.Group || stage[0].Action != "halt_all" || stage[0].Ledger != "stage" {
		t.Fatalf("stage audit = %+v", stage)
	}
	if len(drill) != 3 || drill[0].Outcome != audit.Refused || drill[0].Group != resp.Group || !strings.Contains(drill[0].Error, "already halted") {
		t.Fatalf("drill audit = %+v", drill)
	}

	info := getStrategies(t, ts)
	if len(info.KillSwitch.AlreadyHalted) != 2 {
		t.Fatalf("already halted = %v", info.KillSwitch.AlreadyHalted)
	}
}

func TestHaltAllNeedsToken(t *testing.T) {
	ts, _ := strategyServer(t, "", nil)
	code, b := post(t, ts, ctl{path: "/api/ui/risk/halt-all", token: testToken, body: body("exchange incident", "diego", HaltAllConfirm)})
	if code != http.StatusForbidden || !strings.Contains(string(b), "-operator-token-file") {
		t.Fatalf("halt-all without token = %d %s", code, b)
	}
}
