package etoro

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A fake key for the tests: the shape of nothing real.
const testPublicKey = "canarytest-0000-4000-8000-000000000000"

func writeAck(t *testing.T, dir string, mode os.FileMode, lines ...string) string {
	t.Helper()
	p := filepath.Join(dir, "REAL-ACCOUNT-ACK")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
	return p
}

func goodAckLines(ledger string) []string {
	return []string{
		"# written by hand",
		RealAckSentence,
		"key_prefix: canarytest-0",
		"ledger: " + ledger,
		"policy: etoro-real-canary-2026-10-22",
		"start: 2026-11-03",
		"end: 2026-12-05",
		"name: Operator",
	}
}

func goodCheck(ledger string) RealAckCheck {
	return RealAckCheck{
		Flag: true, Env: EnvReal, PublicKey: testPublicKey,
		LedgerPath:    ledger,
		OtherLedgers:  []string{"/srv/etoro-daily-data/demo/ledger.jsonl", "/srv/etoro-daily-data/demo-etf/ledger.jsonl"},
		PolicyVersion: "etoro-real-canary-2026-10-22",
		TokenFile:     "/home/op/.config/mtb/operator-token-real-canary",
		DemoTokenFile: "/home/op/.config/mtb/operator-token",
		TokenSHA256:   "aaaa", DemoTokenSHA256: "bbbb",
		Now: time.Date(2026, 11, 10, 15, 0, 0, 0, time.UTC),
	}
}

func TestLoadRealAckParsesAGoodFile(t *testing.T) {
	ledger := "/srv/etoro-daily-data/real-canary/ledger.jsonl"
	p := writeAck(t, t.TempDir(), 0o600, goodAckLines(ledger)...)
	ack, err := LoadRealAck(p)
	if err != nil {
		t.Fatal(err)
	}
	if ack.KeyPrefix != "canarytest-0" || ack.LedgerPath != ledger || ack.PolicyVersion != "etoro-real-canary-2026-10-22" ||
		ack.Start.Format("2006-01-02") != "2026-11-03" || ack.End.Format("2006-01-02") != "2026-12-05" || ack.Name != "Operator" {
		t.Fatalf("ack %+v", ack)
	}
	if err := CheckRealAck(ack, goodCheck(ledger)); err != nil {
		t.Fatalf("good check: %v", err)
	}
	facts := ack.Facts(goodCheck(ledger))
	if strings.Contains(facts, testPublicKey) || strings.Contains(facts, "canarytest-0") {
		t.Fatalf("facts leak the key or its prefix: %s", facts)
	}
	for _, want := range []string{"-etoro-real-canary", "ETORO_ENV=real", "12 chars", "Operator", "2026-12-05", ledger, "etoro-real-canary-2026-10-22", "hashes"} {
		if !strings.Contains(facts, want) {
			t.Errorf("facts miss %q: %s", want, facts)
		}
	}
}

func TestLoadRealAckRefusesBadFiles(t *testing.T) {
	ledger := "/srv/etoro-daily-data/real-canary/ledger.jsonl"
	good := goodAckLines(ledger)
	replace := func(i int, line string) []string {
		out := append([]string(nil), good...)
		out[i] = line
		return out
	}
	cases := map[string]struct {
		mode  os.FileMode
		lines []string
		want  string
	}{
		"group readable":    {0o640, good, "readable by group"},
		"sentence altered":  {0o600, replace(1, "I accept orders on the eToro real account for the measurement canary only"), "verbatim"},
		"no sentence":       {0o600, good[2:], "must be the sentence"},
		"missing name":      {0o600, good[:7], "name is missing"},
		"prefix too short":  {0o600, replace(2, "key_prefix: abc"), "8 to 16 characters"},
		"prefix is the key": {0o600, replace(2, "key_prefix: "+testPublicKey), "8 to 16 characters"},
		"relative ledger":   {0o600, replace(3, "ledger: etoro-daily-data/real-canary/ledger.jsonl"), "absolute"},
		"policy not canary": {0o600, replace(4, "policy: etoro-demo-2026-10-19"), "must start with"},
		"end before start":  {0o600, replace(6, "end: 2026-11-01"), "must be after"},
		"unknown field":     {0o600, append(append([]string(nil), good...), "budget: 60"), "unknown field"},
		"duplicate field":   {0o600, append(append([]string(nil), good...), "name: Twice"), "given twice"},
		"not key value":     {0o600, append(append([]string(nil), good...), "just words"), "not \"key: value\""},
		"bad date":          {0o600, replace(5, "start: 3 Nov 2026"), "start:"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			p := writeAck(t, t.TempDir(), tc.mode, tc.lines...)
			_, err := LoadRealAck(p)
			if !errors.Is(err, ErrRealAckFile) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want ErrRealAckFile with %q", err, tc.want)
			}
		})
	}
	if _, err := LoadRealAck(filepath.Join(t.TempDir(), "missing")); !errors.Is(err, ErrRealAckFile) {
		t.Fatalf("missing file: %v", err)
	}
}

// One test per condition of plan §5.2: each failure names its own sentinel.
func TestCheckRealAckNamesTheMissingCondition(t *testing.T) {
	ledger := "/srv/etoro-daily-data/real-canary/ledger.jsonl"
	p := writeAck(t, t.TempDir(), 0o600, goodAckLines(ledger)...)
	ack, err := LoadRealAck(p)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		mutate func(in *RealAckCheck)
		want   error
		msg    string
	}{
		"1 flag missing":             {func(in *RealAckCheck) { in.Flag = false }, ErrRealAckFlag, "-etoro-real-canary"},
		"2 env demo":                 {func(in *RealAckCheck) { in.Env = EnvDemo }, ErrRealAckEnv, `"demo"`},
		"2 key prefix differs":       {func(in *RealAckCheck) { in.PublicKey = "demokey-0000-4000-8000-000000000000" }, ErrRealAckKeyPrefix, "12 characters"},
		"2 key empty":                {func(in *RealAckCheck) { in.PublicKey = "" }, ErrRealAckKeyPrefix, ""},
		"3 before start":             {func(in *RealAckCheck) { in.Now = time.Date(2026, 11, 2, 23, 0, 0, 0, time.UTC) }, ErrRealAckWindow, "2026-11-02"},
		"3 after end":                {func(in *RealAckCheck) { in.Now = time.Date(2026, 12, 6, 0, 0, 0, 0, time.UTC) }, ErrRealAckWindow, "2026-12-06"},
		"4 ledger not real-canary":   {func(in *RealAckCheck) { in.LedgerPath = "/srv/etoro-daily-data/canary/ledger.jsonl" }, ErrRealAckLedger, "real-canary"},
		"4 ledger under demo":        {func(in *RealAckCheck) { in.LedgerPath = "/srv/demo-data/real-canary/ledger.jsonl" }, ErrRealAckLedger, "demo path element"},
		"4 ledger not the ack's":     {func(in *RealAckCheck) { in.LedgerPath = "/other/real-canary/ledger.jsonl" }, ErrRealAckLedger, "the ack names"},
		"4 ledger is a demo ledger":  {func(in *RealAckCheck) { in.OtherLedgers = append(in.OtherLedgers, ledger) }, ErrRealAckLedger, "also a demo ledger"},
		"5 policy demo":              {func(in *RealAckCheck) { in.PolicyVersion = "etoro-demo-2026-10-19" }, ErrRealAckPolicy, "etoro-demo-2026-10-19"},
		"5 policy other canary date": {func(in *RealAckCheck) { in.PolicyVersion = "etoro-real-canary-2026-10-01" }, ErrRealAckPolicy, "the ack names"},
		"6 no token file":            {func(in *RealAckCheck) { in.TokenFile = "" }, ErrRealAckToken, "no operator token"},
		"6 same token path":          {func(in *RealAckCheck) { in.TokenFile = in.DemoTokenFile }, ErrRealAckToken, "both are"},
		"6 same token value":         {func(in *RealAckCheck) { in.DemoTokenSHA256 = in.TokenSHA256 }, ErrRealAckToken, "same token"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			in := goodCheck(ledger)
			tc.mutate(&in)
			err := CheckRealAck(ack, in)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			if tc.msg != "" && !strings.Contains(err.Error(), tc.msg) {
				t.Fatalf("message %q lacks %q", err, tc.msg)
			}
			if strings.Contains(err.Error(), testPublicKey) {
				t.Fatalf("error leaks the key: %v", err)
			}
		})
	}
	// The demo token unreadable: the paths still must differ, the hashes
	// are not compared.
	in := goodCheck(ledger)
	in.DemoTokenSHA256 = ""
	if err := CheckRealAck(ack, in); err != nil {
		t.Fatalf("unreadable demo token: %v", err)
	}
	if f := ack.Facts(in); !strings.Contains(f, "(paths)") {
		t.Fatalf("facts should say the hashes were not compared: %s", f)
	}
	// The window is inclusive at both ends.
	for _, d := range []time.Time{time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC), time.Date(2026, 12, 5, 23, 59, 0, 0, time.UTC)} {
		in := goodCheck(ledger)
		in.Now = d
		if err := CheckRealAck(ack, in); err != nil {
			t.Fatalf("%s should be inside the window: %v", d, err)
		}
	}
}
