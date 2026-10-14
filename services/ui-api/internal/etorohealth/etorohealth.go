// Package etorohealth checks the eToro index-CFD demo executor from its
// ledger directory alone (porting plan P3/P6; docs/etoro/ETORO-DAILY-EXECUTOR.md):
// did the 09:35 New York run record every book on every trading day, did the
// last run exit cleanly, is the last reconcile clean, is an order sent but not
// recorded, is a halt in force. ui-alerts evaluates it every 15 minutes next
// to the Bitso data-health and risk views, so a failed or missed eToro run
// reaches the operator the same way (SNS) without a running ui-api.
//
// It reads files only: the ledger, the intent journal, the run logs and the
// reconcile reports the wrapper leaves next to the ledger, and the halt file.
// It never calls eToro and never writes.
package etorohealth

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"bitso-trading-platform/shared/pkg/etoroledger"
	"bitso-trading-platform/shared/pkg/mktcal"
	"bitso-trading-platform/shared/pkg/risk"
	"bitso-trading-platform/ui-api/internal/datahealth"
)

// Check ids.
const (
	IDLedger    = "etoro.ledger"    // the ledger file itself
	IDHalt      = "etoro.halted"    // a halt in force (warning) or a corrupt halt file (fail)
	IDCoverage  = "etoro.coverage"  // every book recorded on every due trading day
	IDRun       = "etoro.run"       // the newest run log ended with exit=0
	IDReconcile = "etoro.reconcile" // the newest reconcile report is clean
	IDIntents   = "etoro.intents"   // no order sent without a ledger line
)

// Check is one condition, with the statuses of package datahealth.
type Check struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// Options describe one executor.
type Options struct {
	// LedgerPath is <ledger dir>/ledger.jsonl; the intents, run logs,
	// reconcile reports and halt file are found next to it.
	LedgerPath string
	// Books that must be recorded every trading day. Empty: the books seen
	// in the ledger, else DefaultBooks.
	Books []string
	// RunAt is the scheduled start in New York local time; the run is due
	// Grace later. Zero values: 09:35 and 25 minutes (due at 10:00).
	RunAt time.Duration
	Grace time.Duration
	// Days is how many due trading days back coverage is checked. Zero: 3.
	Days int
	// IntentGrace is how old an unrecorded intent must be before it alerts,
	// so a run in progress is not reported. Zero: 10 minutes.
	IntentGrace time.Duration
}

// DefaultBooks are the pre-registered instruments.
var DefaultBooks = []string{"nsdq100", "spx500"}

func (o Options) runAt() time.Duration {
	if o.RunAt == 0 {
		return 9*time.Hour + 35*time.Minute
	}
	return o.RunAt
}

func (o Options) grace() time.Duration {
	if o.Grace == 0 {
		return 25 * time.Minute
	}
	return o.Grace
}

func (o Options) days() int {
	if o.Days <= 0 {
		return 3
	}
	return o.Days
}

func (o Options) intentGrace() time.Duration {
	if o.IntentGrace == 0 {
		return 10 * time.Minute
	}
	return o.IntentGrace
}

// DueDate is the most recent NYSE trading day whose run is already due at
// now: today (New York) once RunAt+Grace has passed on a trading day, else
// the previous trading day.
func (o Options) DueDate(now time.Time) time.Time {
	ny := now.In(mktcal.NewYork)
	today := mktcal.Date(ny.Year(), ny.Month(), ny.Day())
	due := time.Date(ny.Year(), ny.Month(), ny.Day(), 0, 0, 0, 0, mktcal.NewYork).Add(o.runAt() + o.grace())
	if mktcal.IsTradingDay(today) && !ny.Before(due) {
		return today
	}
	return mktcal.PrevTradingDay(today)
}

// Evaluate runs every check. It never returns an empty slice: a ledger that
// cannot be read is itself a failing check.
func Evaluate(o Options, now time.Time) []Check {
	dir := filepath.Dir(o.LedgerPath)
	var out []Check

	// etoroledger.ReadFile reads a missing ledger as empty (the executor
	// creates it on the first append); say so explicitly here.
	_, statErr := os.Stat(o.LedgerPath)
	recs, err := etoroledger.ReadFile(o.LedgerPath)
	switch {
	case errors.Is(statErr, os.ErrNotExist):
		out = append(out, Check{IDLedger, "eToro ledger", datahealth.Warn, "no ledger yet at " + o.LedgerPath})
	case err != nil:
		out = append(out, Check{IDLedger, "eToro ledger", datahealth.Fail, err.Error()})
	case statErr != nil:
		out = append(out, Check{IDLedger, "eToro ledger", datahealth.Fail, statErr.Error()})
	default:
		out = append(out, Check{IDLedger, "eToro ledger", datahealth.OK, fmt.Sprintf("%d records", len(recs))})
	}
	demo := demoRecords(recs)
	books := o.Books
	if len(books) == 0 {
		books = booksIn(demo)
	}
	if len(books) == 0 {
		books = DefaultBooks
	}

	out = append(out, haltCheck(o.LedgerPath))
	due := o.DueDate(now)
	out = append(out, coverageCheck(demo, books, due, o.days()))
	out = append(out, runCheck(dir, now))
	out = append(out, reconcileCheck(dir, due))
	out = append(out, intentsCheck(o.LedgerPath, demo, now, o.intentGrace()))
	return out
}

func demoRecords(recs []etoroledger.Record) []etoroledger.Record {
	var out []etoroledger.Record
	for _, r := range recs {
		if r.Mode == etoroledger.ModeDemo && r.Demo != nil {
			out = append(out, r)
		}
	}
	return out
}

func booksIn(recs []etoroledger.Record) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range recs {
		if !seen[r.Book] {
			seen[r.Book] = true
			out = append(out, r.Book)
		}
	}
	sort.Strings(out)
	return out
}

// haltCheck mirrors the executor: a corrupt halt file makes it exit 2 before
// anything runs (fail); a valid halt is a warning while it lasts.
func haltCheck(ledgerPath string) Check {
	h, found, err := risk.LoadHaltState(risk.HaltPath(ledgerPath))
	switch {
	case err != nil:
		return Check{IDHalt, "eToro halt file", datahealth.Fail, err.Error() + ": the executor refuses to run until it is fixed or removed"}
	case found && h.Halted:
		return Check{IDHalt, "eToro trading halted", datahealth.Warn, fmt.Sprintf("%s (by %s at %s, halt file): planned demo orders are blocked and recorded", h.Reason, h.By, h.At)}
	}
	return Check{IDHalt, "eToro halt file", datahealth.OK, "not halted"}
}

// coverageCheck wants, for each of the last days due trading days and each
// book, a demo record whose fill date is that day. Every outcome of a run
// that reached the ledger counts (none, open, close, blocked, rejected); a
// run that stopped before recording (stale bars, outside the session, an
// unknown order outcome) leaves a hole, which is the point.
func coverageCheck(demo []etoroledger.Record, books []string, due time.Time, days int) Check {
	if len(demo) == 0 {
		return Check{IDCoverage, "eToro ledger coverage", datahealth.Warn, "no demo records yet"}
	}
	have := map[string]bool{}
	first := demo[0].Decision.FillDate
	for _, r := range demo {
		have[r.Book+" "+r.Decision.FillDate] = true
		if r.Decision.FillDate < first {
			first = r.Decision.FillDate
		}
	}
	var missing []string
	d := due
	for i := 0; i < days; i++ {
		ds := d.Format("2006-01-02")
		if ds < first {
			break // before the executor's first day
		}
		for _, b := range books {
			if !have[b+" "+ds] {
				missing = append(missing, b+" "+ds)
			}
		}
		d = mktcal.PrevTradingDay(d)
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return Check{IDCoverage, "eToro ledger coverage", datahealth.Fail,
			fmt.Sprintf("no ledger line for %s: the 09:35 New York run did not record it (missed, or stopped before recording)", strings.Join(missing, ", "))}
	}
	return Check{IDCoverage, "eToro ledger coverage", datahealth.OK, fmt.Sprintf("every book recorded through %s", due.Format("2006-01-02"))}
}

var runLogRe = regexp.MustCompile(`^run-(\d{8}T\d{6}Z)\.log$`)

// runLog is one <ledger dir>/run-<UTC>.log left by the wrapper.
type runLog struct {
	Path   string
	At     time.Time
	Exit   int  // from the last "exit=N" line
	Ended  bool // an "exit=" line was found
	Failed []string
}

func readRunLogs(dir string) ([]runLog, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []runLog
	for _, e := range entries {
		m := runLogRe.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		at, err := time.Parse("20060102T150405Z", m[1])
		if err != nil {
			continue
		}
		rl := runLog{Path: filepath.Join(dir, e.Name()), At: at}
		f, err := os.Open(rl.Path)
		if err != nil {
			return nil, err
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			line := sc.Text()
			if strings.HasPrefix(line, "exit=") {
				if n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "exit="))); err == nil {
					rl.Exit, rl.Ended = n, true
				}
			}
			if strings.Contains(line, "FAILED: ") {
				rl.Failed = append(rl.Failed, strings.TrimSpace(line))
			}
		}
		f.Close()
		out = append(out, rl)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out, nil
}

// nyDate is the New York calendar date of a UTC instant.
func nyDate(t time.Time) time.Time {
	ny := t.In(mktcal.NewYork)
	return mktcal.Date(ny.Year(), ny.Month(), ny.Day())
}

// runCheck reads the newest run log. A run that ended with exit != 0 fails
// until a later run succeeds; a run with no exit line after unfinishedAfter
// fails too (killed, or hanging on eToro). A clean run, or none at all for
// the due day (coverage reports that), is ok.
func runCheck(dir string, now time.Time) Check {
	const unfinishedAfter = 15 * time.Minute
	logs, err := readRunLogs(dir)
	if err != nil {
		return Check{IDRun, "eToro executor run", datahealth.Fail, err.Error()}
	}
	if len(logs) == 0 {
		return Check{IDRun, "eToro executor run", datahealth.Warn, "no run log yet"}
	}
	last := logs[len(logs)-1]
	day := nyDate(last.At).Format("2006-01-02")
	at := last.At.UTC().Format("2006-01-02 15:04") + " UTC"
	switch {
	case !last.Ended && now.Sub(last.At) > unfinishedAfter:
		return Check{IDRun, "eToro executor run", datahealth.Fail, fmt.Sprintf("run of %s has no exit line after %s: killed or hanging; re-run (an intent on disk resumes)", at, now.Sub(last.At).Round(time.Minute))}
	case !last.Ended:
		return Check{IDRun, "eToro executor run", datahealth.OK, "run of " + at + " in progress"}
	case last.Exit != 0:
		msg := fmt.Sprintf("run of %s (New York %s) ended exit=%d", at, day, last.Exit)
		if len(last.Failed) > 0 {
			msg += ": " + strings.Join(last.Failed, " | ")
		}
		return Check{IDRun, "eToro executor run", datahealth.Fail, msg}
	}
	return Check{IDRun, "eToro executor run", datahealth.OK, fmt.Sprintf("run of %s ended exit=0", at)}
}

// reconcileReport is the part of etoro-reconcile's JSON the check reads.
type reconcileReport struct {
	Clean    bool `json:"clean"`
	Findings []struct {
		Severity string `json:"severity"`
		Book     string `json:"book"`
		Code     string `json:"code"`
		Message  string `json:"message"`
	} `json:"findings"`
}

func readReconcile(path string) (reconcileReport, error) {
	var r reconcileReport
	b, err := os.ReadFile(path)
	if err != nil {
		return r, err
	}
	return r, json.Unmarshal(b, &r)
}

// reconcileCheck reads the newest <ledger dir>/reconcile/<date>.json. Drift
// fails unless a <date>.recheck.json (the operator's later read-only
// re-run, runbook §3) is clean. Warnings other than portfolio_lag (normal,
// runbook §3) warn. No report for the due day while that day's run ended
// exit=0 is a warning: the wrapper's reconcile step failed.
func reconcileCheck(dir string, due time.Time) Check {
	rdir := filepath.Join(dir, "reconcile")
	entries, err := os.ReadDir(rdir)
	if errors.Is(err, os.ErrNotExist) {
		return Check{IDReconcile, "eToro reconcile", datahealth.Warn, "no reconcile reports yet"}
	}
	if err != nil {
		return Check{IDReconcile, "eToro reconcile", datahealth.Fail, err.Error()}
	}
	var dates []string
	rechecks := map[string]bool{}
	for _, e := range entries {
		n := e.Name()
		switch {
		case strings.HasSuffix(n, ".recheck.json"):
			rechecks[strings.TrimSuffix(n, ".recheck.json")] = true
		case strings.HasSuffix(n, ".json") && len(n) == len("2006-01-02.json"):
			dates = append(dates, strings.TrimSuffix(n, ".json"))
		}
	}
	if len(dates) == 0 {
		return Check{IDReconcile, "eToro reconcile", datahealth.Warn, "no reconcile reports yet"}
	}
	sort.Strings(dates)
	date := dates[len(dates)-1]
	rep, err := readReconcile(filepath.Join(rdir, date+".json"))
	if err != nil {
		return Check{IDReconcile, "eToro reconcile", datahealth.Fail, fmt.Sprintf("reconcile/%s.json: %v", date, err)}
	}
	var drift, warn []string
	for _, f := range rep.Findings {
		s := f.Code
		if f.Book != "" {
			s = f.Book + " " + f.Code
		}
		switch {
		case f.Severity == "drift":
			drift = append(drift, s+": "+f.Message)
		case f.Code == "portfolio_lag":
		default:
			warn = append(warn, s+": "+f.Message)
		}
	}
	if !rep.Clean {
		if rechecks[date] {
			if rc, err := readReconcile(filepath.Join(rdir, date+".recheck.json")); err == nil && rc.Clean {
				return Check{IDReconcile, "eToro reconcile", datahealth.OK, fmt.Sprintf("%s: drift on the run, clean on the operator's re-check", date)}
			}
		}
		return Check{IDReconcile, "eToro reconcile", datahealth.Fail, fmt.Sprintf("%s: %s (runbook §3: re-check by hand; unfinished_intent: re-run the executor)", date, strings.Join(drift, " | "))}
	}
	if len(warn) > 0 {
		return Check{IDReconcile, "eToro reconcile", datahealth.Warn, date + ": clean with warnings: " + strings.Join(warn, " | ")}
	}
	if dueStr := due.Format("2006-01-02"); date < dueStr {
		if logs, err := readRunLogs(dir); err == nil && len(logs) > 0 {
			if last := logs[len(logs)-1]; last.Ended && last.Exit == 0 && nyDate(last.At).Format("2006-01-02") == dueStr {
				return Check{IDReconcile, "eToro reconcile", datahealth.Warn, fmt.Sprintf("the run of %s ended exit=0 but left no reconcile/%s.json: the reconcile step failed", dueStr, dueStr)}
			}
		}
	}
	return Check{IDReconcile, "eToro reconcile", datahealth.OK, date + ": clean"}
}

// intentsCheck finds orders that were sent (an intent is on disk) but never
// reached the ledger: the run died between the order and the ledger line.
// The executor resumes them on the next run with the same reference; until
// then the account may hold a position the ledger does not know.
func intentsCheck(ledgerPath string, demo []etoroledger.Record, now time.Time, grace time.Duration) Check {
	intents, err := etoroledger.Intents(etoroledger.IntentDir(ledgerPath))
	if err != nil {
		return Check{IDIntents, "eToro intent journal", datahealth.Fail, err.Error()}
	}
	refs := map[string]bool{}
	for _, r := range demo {
		if o := r.Demo.Order; o != nil {
			refs[o.ClientRef] = true
		}
	}
	var open []string
	for _, in := range intents {
		if !refs[in.ClientRef] && now.Sub(in.IntentAt) > grace {
			open = append(open, fmt.Sprintf("%s (sent %s)", in.ClientRef, in.IntentAt.UTC().Format("2006-01-02 15:04 UTC")))
		}
	}
	if len(open) > 0 {
		sort.Strings(open)
		return Check{IDIntents, "eToro order sent but not recorded", datahealth.Fail,
			strings.Join(open, ", ") + ": re-run the executor today to resume it (it resends the same reference and cannot open twice)"}
	}
	return Check{IDIntents, "eToro intent journal", datahealth.OK, fmt.Sprintf("%d intents, all in the ledger", len(intents))}
}
