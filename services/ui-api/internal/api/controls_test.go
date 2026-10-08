package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bitso-trading-platform/shared/pkg/risk"
	"bitso-trading-platform/ui-api/internal/audit"
	"bitso-trading-platform/ui-api/internal/objstore"
	"bitso-trading-platform/ui-api/internal/store"
)

const testToken = "0123456789abcdef0123456789abcdef-test"

// controlServer serves a copy of the test ledger in a temp dir with controls on.
func controlServer(t *testing.T, token string) (*httptest.Server, string) {
	t.Helper()
	dir := t.TempDir()
	src, err := os.ReadFile("testdata/ledger.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir+"/ledger.jsonl", string(src))
	ts := newTestServer(t, fixedNow, func(s *Server) {
		s.Store = store.New(dir+"/ledger.jsonl", "testdata/candles")
		s.OperatorToken = token
	})
	return ts, dir
}

type ctl struct {
	path, token, origin, ctype, body string
}

func post(t *testing.T, ts *httptest.Server, c ctl) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, ts.URL+c.path, strings.NewReader(c.body))
	if err != nil {
		t.Fatal(err)
	}
	if c.ctype == "" {
		c.ctype = "application/json"
	}
	req.Header.Set("Content-Type", c.ctype)
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if c.origin != "" {
		req.Header.Set("Origin", c.origin)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, b
}

func body(reason, by, confirm string) string {
	b, _ := json.Marshal(ControlRequest{Reason: reason, By: by, Confirm: confirm})
	return string(b)
}

func readAudit(t *testing.T, dir string) []audit.Entry {
	t.Helper()
	e, err := audit.Tail(filepath.Join(dir, audit.FileName), 100)
	if err != nil {
		t.Fatal(err)
	}
	return e // newest first
}

func TestControlsOffByDefault(t *testing.T) {
	ts, dir := controlServer(t, "")
	info := get[ControlsInfo](t, ts, "/api/ui/controls", 200)
	if info.Enabled || !strings.Contains(info.DisabledReason, "-operator-token-file") || len(info.Audit) != 0 {
		t.Fatalf("controls must be off without a token: %+v", info)
	}
	code, _ := post(t, ts, ctl{path: "/api/ui/risk/halt", token: "anything", body: body("exchange incident", "diego", "stage")})
	if code != http.StatusForbidden {
		t.Fatalf("halt with controls off: %d", code)
	}
	if _, err := os.Stat(dir + "/risk-state.json"); !os.IsNotExist(err) {
		t.Fatal("no halt file may be written with controls off")
	}
	// Every other POST stays read-only.
	if code, _ := post(t, ts, ctl{path: "/api/ui/risk", token: testToken, body: "{}"}); code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /risk: %d", code)
	}
}

func TestHaltAndResume(t *testing.T) {
	ts, dir := controlServer(t, testToken)
	info := get[ControlsInfo](t, ts, "/api/ui/controls", 200)
	if !info.Enabled || info.AuditPath != filepath.Join(dir, audit.FileName) {
		t.Fatalf("controls info %+v", info)
	}

	code, b := post(t, ts, ctl{path: "/api/ui/risk/halt", token: testToken, origin: "http://127.0.0.1:5173",
		body: body("  exchange incident on Bitso  ", "diego", "stage")})
	if code != 200 {
		t.Fatalf("halt: %d %s", code, b)
	}
	var resp ControlResponse
	if err := json.Unmarshal(b, &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.HaltFile.Halted || resp.HaltFile.Reason != "exchange incident on Bitso" || resp.HaltFile.By != "diego" ||
		resp.Audit.Outcome != audit.Done || resp.AuditError != "" {
		t.Fatalf("halt response %+v", resp)
	}
	// The executor's own reader accepts the file.
	h, found, err := risk.LoadHaltState(dir + "/risk-state.json")
	if err != nil || !found || !h.Halted || h.At != "2026-10-02T03:00:00Z" {
		t.Fatalf("halt file: %+v %v %v", h, found, err)
	}
	if fi, _ := os.Stat(dir + "/risk-state.json"); fi.Mode().Perm() != 0o644 {
		t.Fatalf("halt file mode %v", fi.Mode().Perm())
	}
	r := get[RiskResponse](t, ts, "/api/ui/risk", 200)
	if !r.Halted || r.HaltSource != "file" {
		t.Fatalf("/risk after halt: halted=%v source=%s", r.Halted, r.HaltSource)
	}

	// Halting twice is refused and changes nothing.
	code, b = post(t, ts, ctl{path: "/api/ui/risk/halt", token: testToken, body: body("second halt reason", "diego", "stage")})
	if code != http.StatusConflict || !strings.Contains(string(b), "already halted by diego") {
		t.Fatalf("second halt: %d %s", code, b)
	}

	code, b = post(t, ts, ctl{path: "/api/ui/risk/resume", token: testToken, body: body("incident resolved", "diego", "stage")})
	if code != 200 {
		t.Fatalf("resume: %d %s", code, b)
	}
	h, _, err = risk.LoadHaltState(dir + "/risk-state.json")
	if err != nil || h.Halted || h.Reason != "incident resolved" {
		t.Fatalf("after resume: %+v %v", h, err)
	}
	if r := get[RiskResponse](t, ts, "/api/ui/risk", 200); r.Halted {
		t.Fatal("/risk must not be halted after resume")
	}
	code, _ = post(t, ts, ctl{path: "/api/ui/risk/resume", token: testToken, body: body("incident resolved", "diego", "stage")})
	if code != http.StatusConflict {
		t.Fatalf("resume when not halted: %d", code)
	}

	// Audit: requested+done per change, refusals in between; newest first.
	e := readAudit(t, dir)
	var got []string
	for i := len(e) - 1; i >= 0; i-- {
		got = append(got, e[i].Action+":"+e[i].Outcome)
	}
	want := "halt:requested halt:done halt:refused resume:requested resume:done resume:refused"
	if strings.Join(got, " ") != want {
		t.Fatalf("audit = %v\nwant %s", got, want)
	}
	last := len(e) - 1
	if e[last].ID == "" || e[last].ID != e[last-1].ID || e[last-1].After == nil || !e[last-1].After.Halted ||
		e[last].Before == nil || e[last].Before.Halted || e[last].By != "diego" {
		t.Fatalf("halt audit lines: %+v %+v", e[last], e[last-1])
	}
	if fi, _ := os.Stat(filepath.Join(dir, audit.FileName)); fi.Mode().Perm() != 0o600 {
		t.Fatalf("audit mode %v", fi.Mode().Perm())
	}
	info = get[ControlsInfo](t, ts, "/api/ui/controls", 200)
	if len(info.Audit) != 6 || info.Audit[0].Outcome != audit.Refused || info.HaltFile.Halted {
		t.Fatalf("controls info after: %+v", info)
	}
}

func TestControlRejections(t *testing.T) {
	ts, dir := controlServer(t, testToken)
	good := body("exchange incident", "diego", "stage")
	for _, c := range []struct {
		name string
		ctl
		code    int
		outcome string // audited outcome ("" = not audited)
		msg     string
	}{
		{"no token", ctl{body: good}, 401, audit.Denied, "operator token"},
		{"wrong token", ctl{token: testToken + "x", body: good}, 401, audit.Denied, "operator token"},
		{"foreign origin", ctl{token: testToken, origin: "https://evil.example", body: good}, 403, audit.Denied, "origin"},
		{"form post", ctl{token: testToken, ctype: "application/x-www-form-urlencoded", body: good}, 415, audit.Refused, "application/json"},
		{"unknown field", ctl{token: testToken, body: `{"reason":"exchange incident","by":"d","confirm":"stage","x":1}`}, 400, audit.Refused, "unknown field"},
		{"short reason", ctl{token: testToken, body: body("oops", "diego", "stage")}, 400, audit.Refused, "reason"},
		{"multi-line reason", ctl{token: testToken, body: body("exchange\nincident", "diego", "stage")}, 400, audit.Refused, "one line"},
		{"bad by", ctl{token: testToken, body: body("exchange incident", "<script>", "stage")}, 400, audit.Refused, "by:"},
		{"wrong confirm", ctl{token: testToken, body: body("exchange incident", "diego", "dry-run")}, 400, audit.Refused, "confirm"},
		{"too big", ctl{token: testToken, body: `{"reason":"` + strings.Repeat("a", 5000) + `"}`}, 400, audit.Refused, "body"},
	} {
		t.Run(c.name, func(t *testing.T) {
			c.path = "/api/ui/risk/halt"
			code, b := post(t, ts, c.ctl)
			if code != c.code || !strings.Contains(string(b), c.msg) {
				t.Fatalf("%d %s, want %d containing %q", code, b, c.code, c.msg)
			}
			if e := readAudit(t, dir); len(e) == 0 || e[0].Outcome != c.outcome {
				t.Fatalf("audit %+v, want %s", e, c.outcome)
			}
		})
	}
	if _, err := os.Stat(dir + "/risk-state.json"); !os.IsNotExist(err) {
		t.Fatal("rejected requests must not write a halt file")
	}
	if code, _ := post(t, ts, ctl{path: "/api/ui/risk/halt?ledger=nope", token: testToken, body: good}); code != 400 {
		t.Fatalf("unknown ledger: %d", code)
	}
}

func TestControlInvalidHaltFile(t *testing.T) {
	ts, dir := controlServer(t, testToken)
	writeFile(t, dir+"/risk-state.json", `{"halted":true}`)
	// Resume never lifts a fail-closed file.
	code, b := post(t, ts, ctl{path: "/api/ui/risk/resume", token: testToken, body: body("try to resume", "diego", "stage")})
	if code != http.StatusConflict || !strings.Contains(string(b), "invalid") {
		t.Fatalf("resume over an invalid file: %d %s", code, b)
	}
	// Halt replaces it with a valid halt (still fail closed), noted in the audit.
	code, b = post(t, ts, ctl{path: "/api/ui/risk/halt", token: testToken, body: body("fix the halt file", "diego", "stage")})
	if code != 200 {
		t.Fatalf("halt over an invalid file: %d %s", code, b)
	}
	if h, _, err := risk.LoadHaltState(dir + "/risk-state.json"); err != nil || !h.Halted {
		t.Fatalf("after: %+v %v", h, err)
	}
	e := readAudit(t, dir)
	if e[1].Outcome != audit.Requested || !strings.Contains(e[1].Error, "replacing an invalid halt file") || e[1].Before != nil {
		t.Fatalf("requested line %+v", e[1])
	}
}

func TestControlNeedsAuditFirst(t *testing.T) {
	ts, dir := controlServer(t, testToken)
	// An audit "file" that cannot be appended to: nothing may change.
	if err := os.Mkdir(filepath.Join(dir, audit.FileName), 0o700); err != nil {
		t.Fatal(err)
	}
	code, b := post(t, ts, ctl{path: "/api/ui/risk/halt", token: testToken, body: body("exchange incident", "diego", "stage")})
	if code != 500 || !strings.Contains(string(b), "nothing changed") {
		t.Fatalf("%d %s", code, b)
	}
	if _, err := os.Stat(dir + "/risk-state.json"); !os.IsNotExist(err) {
		t.Fatal("no halt without an audit line")
	}
}

func TestControlsRemoteLedger(t *testing.T) {
	m := &objstore.Mem{Name: "s3://bucket"}
	ts := newTestServer(t, fixedNow, func(s *Server) {
		s.Store = store.NewRemote(m, "daily-executor/stage")
		s.OperatorToken = testToken
	})
	info := get[ControlsInfo](t, ts, "/api/ui/controls", 200)
	if info.Enabled || !strings.Contains(info.DisabledReason, "S3") || info.AuditPath != "" {
		t.Fatalf("S3 ledger: %+v", info)
	}
	code, _ := post(t, ts, ctl{path: "/api/ui/risk/halt", token: testToken, body: body("exchange incident", "diego", "stage")})
	if code != http.StatusForbidden {
		t.Fatalf("halt on an S3 ledger: %d", code)
	}
}

func TestLoadOperatorToken(t *testing.T) {
	dir := t.TempDir()
	p := dir + "/tok"
	if err := os.WriteFile(p, []byte(testToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if tok, err := LoadOperatorToken(p); err != nil || tok != testToken {
		t.Fatalf("%q %v", tok, err)
	}
	if err := os.Chmod(p, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOperatorToken(p); err == nil || !strings.Contains(err.Error(), "chmod 600") {
		t.Fatalf("world-readable token must be refused: %v", err)
	}
	if err := os.WriteFile(dir+"/short", []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOperatorToken(dir + "/short"); err == nil {
		t.Fatal("short token must be refused")
	}
}
