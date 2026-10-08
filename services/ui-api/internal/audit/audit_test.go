package audit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppendAndTail(t *testing.T) {
	p := filepath.Join(t.TempDir(), FileName)
	if e, err := Tail(p, 10); err != nil || len(e) != 0 {
		t.Fatalf("missing file: %v %v", e, err)
	}
	for _, a := range []string{"halt", "resume", "halt"} {
		if err := Append(p, Entry{At: "2026-10-08T00:00:00Z", Action: a, Outcome: Done, Ledger: "stage"}); err != nil {
			t.Fatal(err)
		}
	}
	e, err := Tail(p, 2)
	if err != nil || len(e) != 2 || e[0].Action != "halt" || e[1].Action != "resume" {
		t.Fatalf("tail: %+v %v", e, err)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}

	// A damaged line is skipped and reported; the rest still reads.
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.WriteString("{not json\n")
	f.Close()
	if err := Append(p, Entry{At: "2026-10-08T01:00:00Z", Action: "resume", Outcome: Refused, Ledger: "stage"}); err != nil {
		t.Fatal(err)
	}
	e, err = Tail(p, 10)
	if len(e) != 4 || e[0].Outcome != Refused || err == nil || !strings.Contains(err.Error(), "line 4") {
		t.Fatalf("damaged: %d entries, %v", len(e), err)
	}
}
