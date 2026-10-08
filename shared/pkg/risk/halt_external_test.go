package risk

import (
	"os"
	"strings"
	"testing"
)

// TestExternalHaltFiles validates halt files written outside Go with the real
// parser: scripts/tests/k8s-halt-test.sh passes the files scripts/k8s-halt.sh
// produced and the k8s ConfigMap default. Skipped unless RISK_HALT_FILES_UNDER_TEST
// is set (comma-separated paths). RISK_HALT_EXPECT is the comma-separated
// expected halted value for each file ("true"/"false").
func TestExternalHaltFiles(t *testing.T) {
	list := os.Getenv("RISK_HALT_FILES_UNDER_TEST")
	if list == "" {
		t.Skip("RISK_HALT_FILES_UNDER_TEST not set")
	}
	files := strings.Split(list, ",")
	want := strings.Split(os.Getenv("RISK_HALT_EXPECT"), ",")
	if len(want) != len(files) {
		t.Fatalf("RISK_HALT_EXPECT has %d values for %d files", len(want), len(files))
	}
	for i, f := range files {
		h, found, err := LoadHaltState(f)
		if err != nil || !found {
			t.Errorf("%s: found=%v err=%v", f, found, err)
			continue
		}
		if got := map[bool]string{true: "true", false: "false"}[h.Halted]; got != want[i] {
			t.Errorf("%s: halted=%s, want %s", f, got, want[i])
		}
	}
}
