// Package alerts turns ui-api's data-health and risk views into operator
// alerts (risk step R3, docs/frontend/FRONTEND-UI-PLAN-2026-10-03.md §8.9):
// a missed or failed executor run, a blocked order, a risk warning, a stale
// collector, a compaction lag or a failed ledger upload.
//
// It is pure: Evaluate maps responses to alerts, Diff decides what to send
// against the previous state (new, escalated, reminder, resolved), and
// Render formats one message. cmd/ui-alerts does the I/O.
package alerts

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"bitso-trading-platform/shared/pkg/dailyledger"
	"bitso-trading-platform/shared/pkg/risk"
	"bitso-trading-platform/ui-api/internal/api"
	"bitso-trading-platform/ui-api/internal/datahealth"
)

// Severities, worst first.
const (
	Critical = "critical"
	Warning  = "warning"
)

func rank(s string) int {
	if s == Critical {
		return 2
	}
	return 1
}

// Alert is one condition that needs the operator.
type Alert struct {
	// Key is stable across runs: "archive/<check id>" for the shared archive
	// checks, "<ledger>/<check id>" or "<ledger>/risk.<book>.<rule>" otherwise.
	Key      string `json:"key"`
	Severity string `json:"severity"`
	Title    string `json:"title"`
	Message  string `json:"message"`
}

// FromHealth maps the data-health checks of one ledger: fail is critical,
// warn is a warning. "unknown" only alerts for the archive (it cannot be
// listed, so a dead collector would go unseen); an executor run that is
// still in progress is not an alert, and "off" (not configured) never is.
func FromHealth(ledger string, h api.DataHealthResponse) []Alert {
	var out []Alert
	for _, c := range h.Checks {
		sev := ""
		switch {
		case c.Status == datahealth.Fail:
			sev = Critical
		case c.Status == datahealth.Warn:
			sev = Warning
		case c.Status == datahealth.Unknown && c.Area == "archive":
			sev = Warning
		}
		if sev == "" {
			continue
		}
		a := Alert{Key: "archive/" + c.ID, Severity: sev, Title: c.Label, Message: c.Message}
		if c.Area == "executor" {
			a.Key, a.Title = ledger+"/"+c.ID, c.Label+" ("+ledger+")"
		}
		out = append(out, a)
	}
	return out
}

// FromRisk maps the risk view of one ledger: an invalid halt file (the
// executor refuses to run) and block findings are critical, warn findings
// are warnings, and a halt is a warning while it lasts (so a halt or resume
// from the UI also reaches the operator by email). run_missed is skipped:
// the data-health ledger coverage check reports the same days.
func FromRisk(ledger string, r api.RiskResponse) []Alert {
	var out []Alert
	if r.HaltFile.Error != "" {
		out = append(out, Alert{Key: ledger + "/risk.halt_file", Severity: Critical,
			Title:   "Halt file invalid (" + ledger + ")",
			Message: r.HaltFile.Error + ": the executor refuses to run until it is fixed or removed"})
	}
	if r.Halted {
		msg := r.HaltReason
		if r.HaltSource == "file" || r.HaltSource == "both" {
			msg = fmt.Sprintf("%s (by %s at %s, halt file)", r.HaltFile.Reason, r.HaltFile.By, r.HaltFile.At)
		}
		out = append(out, Alert{Key: ledger + "/risk.halted", Severity: Warning,
			Title:   "Trading halted (" + ledger + ")",
			Message: msg + ": new stage orders are blocked and recorded"})
	}
	for _, b := range r.Books {
		for _, f := range b.Findings {
			if f.Rule == api.RuleRunMissed {
				continue
			}
			sev := Warning
			if f.Severity == risk.Block {
				sev = Critical
			}
			out = append(out, Alert{Key: fmt.Sprintf("%s/risk.%s.%s", ledger, b.Book, f.Rule), Severity: sev,
				Title: fmt.Sprintf("Risk %s · %s (%s)", f.Rule, b.Book, ledger), Message: f.Message})
		}
	}
	return out
}

// EvalError is the alert for a ledger ui-alerts could not evaluate at all.
func EvalError(ledger, what string, err error) Alert {
	return Alert{Key: ledger + "/eval." + what, Severity: Critical,
		Title: "Cannot evaluate " + what + " (" + ledger + ")", Message: err.Error()}
}

// Merge de-duplicates by key (the archive checks repeat per ledger), keeping
// the worst severity, and sorts worst first, then by key.
func Merge(groups ...[]Alert) []Alert {
	by := map[string]Alert{}
	for _, g := range groups {
		for _, a := range g {
			if old, ok := by[a.Key]; !ok || rank(a.Severity) > rank(old.Severity) {
				by[a.Key] = a
			}
		}
	}
	out := make([]Alert, 0, len(by))
	for _, a := range by {
		out = append(out, a)
	}
	sortAlerts(out)
	return out
}

func sortAlerts(as []Alert) {
	sort.Slice(as, func(i, j int) bool {
		if rank(as[i].Severity) != rank(as[j].Severity) {
			return rank(as[i].Severity) > rank(as[j].Severity)
		}
		return as[i].Key < as[j].Key
	})
}

// Entry is an open alert in the state file.
type Entry struct {
	Alert
	FirstSeen time.Time `json:"first_seen"`
	LastSent  time.Time `json:"last_sent"`
}

// State is what ui-alerts remembers between runs.
type State struct {
	Version int              `json:"version"`
	Open    map[string]Entry `json:"open"`
	// LastRun is when the last evaluation finished, sent or not.
	LastRun time.Time `json:"last_run"`
}

// Notice is what one run has to say. Empty means send nothing.
type Notice struct {
	At        time.Time
	New       []Entry // first seen now
	Escalated []Entry // warning → critical
	Reminder  []Entry // still open, last sent ≥ repeat ago
	Resolved  []Entry // open before, gone now
	Open      int     // open alerts after this run
}

// Empty reports whether there is nothing to send.
func (n Notice) Empty() bool {
	return len(n.New)+len(n.Escalated)+len(n.Reminder)+len(n.Resolved) == 0
}

// Diff compares the current alerts with the previous state. An alert is
// sent when it is new or escalates, again every repeat while it stays open
// (repeat ≤ 0: never), and once more when it resolves. A message change alone
// (e.g. one more missing day) waits for the next reminder.
func Diff(prev State, cur []Alert, now time.Time, repeat time.Duration) (Notice, State) {
	n := Notice{At: now}
	next := State{Version: 1, Open: map[string]Entry{}, LastRun: now}
	seen := map[string]bool{}
	for _, a := range cur {
		seen[a.Key] = true
		old, ok := prev.Open[a.Key]
		e := Entry{Alert: a, FirstSeen: now, LastSent: now}
		switch {
		case !ok:
			n.New = append(n.New, e)
		case rank(a.Severity) > rank(old.Severity):
			e.FirstSeen = old.FirstSeen
			n.Escalated = append(n.Escalated, e)
		case repeat > 0 && now.Sub(old.LastSent) >= repeat:
			e.FirstSeen = old.FirstSeen
			n.Reminder = append(n.Reminder, e)
		default:
			e.FirstSeen, e.LastSent = old.FirstSeen, old.LastSent
		}
		next.Open[a.Key] = e
	}
	for k, e := range prev.Open {
		if !seen[k] {
			n.Resolved = append(n.Resolved, e)
		}
	}
	sort.Slice(n.Resolved, func(i, j int) bool { return n.Resolved[i].Key < n.Resolved[j].Key })
	n.Open = len(next.Open)
	return n, next
}

// Render formats a notice as a subject (ASCII, at most 99 characters, as SNS
// requires) and a plain-text body.
func Render(n Notice, uiURL string) (subject, body string) {
	crit, warn := 0, 0
	for _, g := range [][]Entry{n.New, n.Escalated, n.Reminder} {
		for _, e := range g {
			if e.Severity == Critical {
				crit++
			} else {
				warn++
			}
		}
	}
	var parts []string
	if crit > 0 {
		parts = append(parts, fmt.Sprintf("%d critical", crit))
	}
	if warn > 0 {
		parts = append(parts, fmt.Sprintf("%d warning", warn))
	}
	if len(n.Resolved) > 0 {
		parts = append(parts, fmt.Sprintf("%d resolved", len(n.Resolved)))
	}
	first := ""
	for _, g := range [][]Entry{n.Escalated, n.New, n.Reminder, n.Resolved} {
		if len(g) > 0 {
			first = g[0].Title
			break
		}
	}
	subject = asciiLine("[mtb-ops] " + strings.Join(parts, ", ") + ": " + first)

	var b strings.Builder
	mx := n.At.In(dailyledger.Mexico)
	fmt.Fprintf(&b, "MTB operator alerts, %s UTC (%s Mexico City)\n", n.At.UTC().Format("2006-01-02 15:04"), mx.Format("2006-01-02 15:04"))
	section := func(name string, es []Entry, resolved bool) {
		if len(es) == 0 {
			return
		}
		fmt.Fprintf(&b, "\n%s\n", name)
		for _, e := range es {
			tag := strings.ToUpper(e.Severity)
			if resolved {
				tag = "OK"
			}
			fmt.Fprintf(&b, "  [%s] %s\n      %s\n", tag, e.Title, e.Message)
			if !resolved && !e.FirstSeen.Equal(n.At) {
				fmt.Fprintf(&b, "      open since %s UTC\n", e.FirstSeen.UTC().Format("2006-01-02 15:04"))
			}
		}
	}
	section("ESCALATED", n.Escalated, false)
	section("NEW", n.New, false)
	section("STILL OPEN (reminder)", n.Reminder, false)
	section("RESOLVED", n.Resolved, true)
	fmt.Fprintf(&b, "\nOpen alerts now: %d\n", n.Open)
	if uiURL != "" {
		fmt.Fprintf(&b, "Data health: %s/data-health\nRisk: %s/risk\n", strings.TrimRight(uiURL, "/"), strings.TrimRight(uiURL, "/"))
	}
	return subject, b.String()
}

// asciiLine keeps printable ASCII (· and → become - and >), one line, ≤ 99.
func asciiLine(s string) string {
	s = strings.NewReplacer("·", "-", "→", ">", "\n", " ", "\r", " ").Replace(s)
	var b strings.Builder
	for _, r := range s {
		if r >= 0x20 && r < 0x7f {
			b.WriteRune(r)
		}
	}
	out := b.String()
	if len(out) > 99 {
		out = out[:96] + "..."
	}
	return out
}
