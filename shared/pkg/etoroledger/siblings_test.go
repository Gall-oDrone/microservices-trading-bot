package etoroledger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The sibling ledgers (2026-10-25): a position a sibling's ledger holds is
// that sibling's; a closed one, a missing ledger and the cell's own path
// are not; a corrupt sibling is an error, never a guess.
func TestSiblingPositions(t *testing.T) {
	dir := t.TempDir()
	etf := filepath.Join(dir, "demo-etf", "ledger.jsonl")
	rot := filepath.Join(dir, "demo-rotation", "ledger.jsonl")
	l, err := Open(etf)
	if err != nil {
		t.Fatal(err)
	}
	spy := rec("spy", "2026-10-21", "long")
	spy.Demo.PositionAfter.PositionID, spy.Demo.PositionAfter.Units = "3623764792", 1.4
	qqq := rec("qqq", "2026-10-21", "long")
	qqq.Demo.PositionAfter.PositionID = "111"
	qqqClosed := rec("qqq", "2026-10-22", "flat")
	for _, r := range []Record{spy, qqq, qqqClosed} {
		if err := l.Append(r); err != nil {
			t.Fatal(err)
		}
	}
	paths := SplitSiblingPaths(" "+etf+", "+rot+" ,"+etf+",,"+rot, rot)
	if len(paths) != 1 || paths[0] != etf {
		t.Fatalf("paths %v", paths)
	}
	owners, err := SiblingPositions([]string{etf, rot})
	if err != nil {
		t.Fatal(err)
	}
	if len(owners) != 1 {
		t.Fatalf("owners %+v (the closed qqq must not be there, the missing rotation ledger is skipped)", owners)
	}
	o := owners["3623764792"]
	if o.Book != "spy" || o.Ledger != etf || o.Units != 1.4 {
		t.Fatalf("spy owner %+v", o)
	}
	if s := DescribeSiblings([]string{"3623764792"}, owners); !strings.Contains(s, "3623764792 on spy (ledger ") {
		t.Fatalf("describe %q", s)
	}
	// A corrupt sibling is an error.
	if err := os.MkdirAll(filepath.Dir(rot), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rot, []byte("not json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := SiblingPositions([]string{rot}); err == nil || !strings.Contains(err.Error(), "sibling ledger") {
		t.Fatalf("corrupt sibling: %v", err)
	}
}
