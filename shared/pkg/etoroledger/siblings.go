package etoroledger

// Sibling cells (2026-10-25). Three pre-registered cells trade on ONE demo
// account (the index CFDs, the SMA50 ETFs and the dual-momentum rotation),
// each with its own ledger, and two of them trade the same ETFs. Each cell's
// executor requires the account to agree with its ledger ("the executor is
// the only actor on its instruments"), and the reconcile reports every
// position on an instrument it does not trade. Both rules were written for a
// cell alone on its account; with three cells they fire on each other every
// day (2026-10-22: the rotation refused to run because the ETF cell held
// SPY; every reconcile warned about the other cells' positions).
//
// A sibling ledger is another cell's ledger on the same account. A position
// that a sibling's ledger currently holds is that sibling's, not foreign and
// not an orphan: this cell leaves it alone and says so. A position NO ledger
// holds is still foreign (the account was touched outside the executors),
// and the ledger isolation of checkLedgerPrereg is unchanged: siblings are
// read, never written.

import (
	"fmt"
	"sort"
	"strings"
)

// SiblingLedgersEnv names the environment variable both the executor and
// etoro-reconcile read as the default of -sibling-ledgers: comma-separated
// ledger paths of the other cells on the same account.
const SiblingLedgersEnv = "ETORO_SIBLING_LEDGERS"

// SiblingOwner says which sibling holds a position.
type SiblingOwner struct {
	Ledger string // the sibling ledger's path, as given
	Book   string
	Units  float64
}

// SiblingPositions reads the sibling ledgers and returns the positions
// their books currently hold, by position id. A path that does not exist
// yet is skipped (ReadFile answers nothing for it: a cell that has not run);
// an unreadable or corrupt ledger is an error, because a cell must not act
// on a guess about its siblings.
func SiblingPositions(paths []string) (map[string]SiblingOwner, error) {
	out := map[string]SiblingOwner{}
	for _, p := range paths {
		recs, err := ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("sibling ledger %s: %w", p, err)
		}
		books := map[string]bool{}
		for _, r := range recs {
			if r.Book != "" {
				books[r.Book] = true
			}
		}
		for b := range books {
			pos := LastDemoPosition(recs, b)
			if pos.State == "long" && pos.PositionID != "" {
				out[pos.PositionID] = SiblingOwner{Ledger: p, Book: b, Units: pos.Units}
			}
		}
	}
	return out, nil
}

// SplitSiblingPaths parses the flag / environment value: comma-separated,
// trimmed, empties dropped, duplicates and self removed.
func SplitSiblingPaths(v, self string) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range strings.Split(v, ",") {
		p = strings.TrimSpace(p)
		if p == "" || p == self || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// DescribeSiblings is the one-line note a run or a report writes about the
// sibling positions it saw and left alone: "3623764792 on spy (ledger …/demo-etf/ledger.jsonl)".
func DescribeSiblings(ids []string, owners map[string]SiblingOwner) string {
	sort.Strings(ids)
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		o := owners[id]
		parts = append(parts, fmt.Sprintf("%s on %s (ledger %s)", id, o.Book, o.Ledger))
	}
	return strings.Join(parts, ", ")
}
