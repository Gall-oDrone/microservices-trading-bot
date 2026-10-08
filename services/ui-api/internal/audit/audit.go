// Package audit is the append-only operator audit log (R4): one JSON object
// per line, next to the ledger it is about (ui-audit.jsonl). Every control
// attempt is written, including denied and refused ones, and a change is
// written as "requested" before it is made and "done" or "failed" after.
package audit

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"bitso-trading-platform/shared/pkg/risk"
)

// FileName is the audit log, kept next to the ledger and its halt file.
const FileName = "ui-audit.jsonl"

// Outcomes of a control attempt.
const (
	Requested = "requested" // written before the change; nothing changed yet
	Done      = "done"      // the change was made
	Failed    = "failed"    // the change was attempted and did not happen
	Refused   = "refused"   // invalid request or wrong state; nothing attempted
	Denied    = "denied"    // missing or wrong operator token, or a foreign origin
)

// Entry is one audit line.
type Entry struct {
	// ID ties a "requested" line to its "done"/"failed" line.
	ID        string          `json:"id,omitempty"`
	At        string          `json:"at"` // RFC 3339, UTC
	Action    string          `json:"action"`
	Outcome   string          `json:"outcome"`
	Ledger    string          `json:"ledger"`
	By        string          `json:"by,omitempty"`
	Reason    string          `json:"reason,omitempty"`
	Remote    string          `json:"remote,omitempty"`
	UserAgent string          `json:"user_agent,omitempty"`
	Before    *risk.HaltState `json:"before,omitempty"`
	After     *risk.HaltState `json:"after,omitempty"`
	Error     string          `json:"error,omitempty"`
	// Group ties together the per-ledger lines of one halt-all (kill switch).
	Group string `json:"group,omitempty"`
	// Strategy, Executor and UpstreamStatus describe a strategy start/stop
	// sent to strategy-executor (Ledger is empty for those).
	Strategy       string `json:"strategy,omitempty"`
	Executor       string `json:"executor,omitempty"`
	UpstreamStatus int    `json:"upstream_status,omitempty"`
	// Detail is extra context, e.g. an acknowledged open position.
	Detail string `json:"detail,omitempty"`
}

// Append writes e as one line and syncs it to disk. The file is created
// with mode 0600 and only ever opened for appending.
func Append(path string, e Entry) error {
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// maxRead bounds what Tail reads (the log grows by a few lines per action).
const maxRead = 8 << 20

// Tail returns up to n entries, newest first. A missing file is no entries
// and no error. An unparseable line is skipped and reported in err (the
// other entries are still returned), so a damaged log is visible.
func Tail(path string, n int) ([]Entry, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return []Entry{}, nil
	}
	if err != nil {
		return []Entry{}, err
	}
	if len(b) > maxRead {
		b = b[len(b)-maxRead:]
		if i := bytes.IndexByte(b, '\n'); i >= 0 {
			b = b[i+1:]
		}
	}
	var all []Entry
	var bad []int
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	line := 0
	for sc.Scan() {
		line++
		if len(bytes.TrimSpace(sc.Bytes())) == 0 {
			continue
		}
		var e Entry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			bad = append(bad, line)
			continue
		}
		all = append(all, e)
	}
	if err := sc.Err(); err != nil {
		return []Entry{}, err
	}
	out := make([]Entry, 0, min(n, len(all)))
	for i := len(all) - 1; i >= 0 && len(out) < n; i-- {
		out = append(out, all[i])
	}
	if len(bad) > 0 {
		return out, fmt.Errorf("%s: %d unreadable line(s), first at line %d", path, len(bad), bad[0])
	}
	return out, nil
}
