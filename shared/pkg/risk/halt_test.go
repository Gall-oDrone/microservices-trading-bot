package risk

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadHaltState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, HaltFileName)
	if HaltPath(filepath.Join(dir, "ledger.jsonl")) != path {
		t.Fatalf("HaltPath")
	}
	if h, found, err := LoadHaltState(path); err != nil || found || h.Halted {
		t.Fatalf("missing file: %+v %v %v", h, found, err)
	}
	for _, c := range []struct {
		body, wantErr string
		halted        bool
	}{
		{`{"halted":false}`, "", false},
		{`{"halted":true,"reason":"exchange incident","by":"diego","at":"2026-10-06T01:00:00Z"}`, "", true},
		{`{"halted":true}`, "halted needs reason, by, at", false},
		{`{"halted":true,"reason":"x","by":"d","at":"yesterday"}`, "not RFC 3339", false},
		{`{"halted":true,"reason":"x","by":"d","at":"2026-10-06T01:00:00Z","until":"x"}`, "unknown field", false},
		{`{"halted":tru`, "unexpected EOF", false},
	} {
		if err := os.WriteFile(path, []byte(c.body), 0o600); err != nil {
			t.Fatal(err)
		}
		h, found, err := LoadHaltState(path)
		if !found {
			t.Fatalf("%s: found=false", c.body)
		}
		// ParseHaltState (ui-api's S3 copy) applies exactly the same rules.
		if ph, perr := ParseHaltState([]byte(c.body)); (perr == nil) != (err == nil) || ph != h {
			t.Fatalf("%s: ParseHaltState %+v %v, LoadHaltState %+v %v", c.body, ph, perr, h, err)
		}
		if c.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("%s: err %v, want %q", c.body, err, c.wantErr)
			}
			continue
		}
		if err != nil || h.Halted != c.halted {
			t.Fatalf("%s: %+v %v", c.body, h, err)
		}
	}
}

func TestApplyHaltBlocksOrders(t *testing.T) {
	p := DefaultPolicy()
	if got := ApplyHalt(p, HaltState{}); got.Halted {
		t.Fatal("not halted must not change the policy")
	}
	h := HaltState{Halted: true, Reason: "exchange incident", By: "diego", At: "2026-10-06T01:00:00Z"}
	hp := ApplyHalt(p, h)
	if !hp.Halted || !strings.Contains(hp.HaltReason, "exchange incident (halt file, by diego at 2026-10-06T01:00:00Z)") {
		t.Fatalf("%+v", hp)
	}
	d := Check(hp, Order{Book: "btc_usd", Side: "sell", QtyBTC: 0.001, Price: 100, RefPrice: 100}, State{PositionBTC: 0.001})
	if d.Allowed || len(d.Findings) == 0 || d.Findings[0].Rule != RuleHalted {
		t.Fatalf("a halt blocks even reducing sells: %+v", d)
	}
	p.Halted, p.HaltReason = true, "policy halt"
	if both := ApplyHalt(p, h); !strings.HasPrefix(both.HaltReason, "policy halt; exchange incident") {
		t.Fatalf("both reasons: %q", both.HaltReason)
	}
}
