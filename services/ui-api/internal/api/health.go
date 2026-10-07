package api

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"bitso-trading-platform/shared/pkg/dailyledger"
	"bitso-trading-platform/ui-api/internal/datahealth"
	"bitso-trading-platform/ui-api/internal/objstore"
	"bitso-trading-platform/ui-api/internal/store"
)

// HealthCheck is one row of the data-health summary.
type HealthCheck struct {
	ID      string `json:"id"`   // e.g. "archive.raw.btc_mxn"
	Area    string `json:"area"` // collector | archive | executor
	Label   string `json:"label"`
	Status  string `json:"status"` // ok | off | unknown | warn | fail
	Message string `json:"message"`
}

// ExecutorBook is one book's ledger coverage.
type ExecutorBook struct {
	Book string    `json:"book"`
	Run  RunStatus `json:"run"`
}

// UploadHealth says whether the last run copied the ledger to S3.
type UploadHealth struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Target  string `json:"target"`
	At      string `json:"at"`
}

// ExecutorHealth is the daily-executor section, for one ledger.
type ExecutorHealth struct {
	Status           string              `json:"status"`
	Message          string              `json:"message"`
	LedgerPath       string              `json:"ledger_path"`
	LedgerFound      bool                `json:"ledger_found"`
	LedgerModifiedAt string              `json:"ledger_modified_at"`
	Records          int                 `json:"records"`
	LastRecordedAt   string              `json:"last_recorded_at"`
	Books            []ExecutorBook      `json:"books"`
	LastRun          *datahealth.RunLog  `json:"last_run"`
	Runs             []datahealth.RunLog `json:"runs"`
	Upload           UploadHealth        `json:"upload"`
}

// CollectorHealth is what ui-api can tell about the data collector: its
// flushes to S3. Its own /healthz, the WebSocket gap table and Postgres row
// counts are inside the VPC and not reachable from here.
type CollectorHealth struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Note    string `json:"note"`
}

// DataHealthResponse is GET /api/ui/health/data.
type DataHealthResponse struct {
	Ledger      string             `json:"ledger"`
	GeneratedAt string             `json:"generated_at"`
	Status      string             `json:"status"` // worst of checks, ignoring "off"
	Checks      []HealthCheck      `json:"checks"`
	Collector   CollectorHealth    `json:"collector"`
	Archive     datahealth.Archive `json:"archive"`
	Executor    ExecutorHealth     `json:"executor"`
	Thresholds  map[string]string  `json:"thresholds"`
}

// archiveCache keeps the last archive check so the page's polling does not
// list S3 on every request.
type archiveCache struct {
	mu   sync.Mutex
	at   time.Time
	last *datahealth.Archive
}

const (
	archiveTTL      = 60 * time.Second
	archiveErrTTL   = 15 * time.Second
	archiveDeadline = 12 * time.Second
	runLogsShown    = 7
)

func (s *Server) archive(ctx context.Context, books []string) datahealth.Archive {
	if s.Archive == nil {
		return datahealth.CheckArchive(ctx, nil, books, s.Now())
	}
	c := &s.archiveCache
	c.mu.Lock()
	defer c.mu.Unlock()
	now := s.Now()
	if c.last != nil {
		ttl := archiveTTL
		if c.last.Error != "" {
			ttl = archiveErrTTL
		}
		if now.Sub(c.at) < ttl {
			return *c.last
		}
	}
	ctx, cancel := context.WithTimeout(ctx, archiveDeadline)
	defer cancel()
	a := datahealth.CheckArchive(ctx, s.Archive, books, now)
	if a.Error != "" {
		s.Log.Printf("archive check: %s", a.Error)
	}
	c.at, c.last = now, &a
	return a
}

func (s *Server) dataHealth(w http.ResponseWriter, r *http.Request) {
	l, ok := s.pick(w, r)
	if !ok {
		return
	}
	now := s.Now()
	resp := DataHealthResponse{Ledger: l.Name, GeneratedAt: now.UTC().Format(time.RFC3339), Checks: []HealthCheck{},
		Thresholds: map[string]string{
			"raw_warn_after":       datahealth.RawWarnAfter.String(),
			"raw_fail_after":       datahealth.RawFailAfter.String(),
			"flush_gap_warn":       datahealth.FlushGapWarn.String(),
			"compaction_warn_days": fmt.Sprint(datahealth.CompactionWarnDays),
			"compaction_fail_days": fmt.Sprint(datahealth.CompactionFailDays),
		}}

	recs, recErr := l.Store.Records()
	by := dailyledger.ByBook(recs)
	books := s.books(by)

	// Archive and collector.
	resp.Archive = s.archive(r.Context(), books)
	resp.Collector = CollectorHealth{
		Note: "Inferred from the collector's hourly flushes to S3. Its /healthz, the ws_gaps table and Postgres row " +
			"counts are inside the VPC (audit via SSM, as in docs/backtest-readiness/evidence-2026-09-22).",
	}
	switch {
	case s.Archive == nil:
		resp.Collector.Status, resp.Collector.Message = datahealth.Off, "archive not configured (ui-api -archive s3://bucket)"
		resp.Checks = append(resp.Checks, HealthCheck{ID: "archive", Area: "archive", Label: "Trade archive",
			Status: datahealth.Off, Message: "not configured: start ui-api with -archive s3://<bucket>"})
	case resp.Archive.Status == datahealth.Unknown && resp.Archive.Error != "":
		resp.Collector.Status, resp.Collector.Message = datahealth.Unknown, "cannot list the archive: "+resp.Archive.Error
		resp.Checks = append(resp.Checks, HealthCheck{ID: "archive", Area: "archive", Label: "Trade archive",
			Status: datahealth.Unknown, Message: "cannot list " + resp.Archive.Source + ": " + resp.Archive.Error})
	default:
		var raw []string
		for _, b := range resp.Archive.Books {
			raw = append(raw, b.Raw.Status)
			resp.Checks = append(resp.Checks,
				HealthCheck{ID: "collector." + b.Book, Area: "collector", Label: "Collector flushes · " + b.Book,
					Status: b.Raw.Status, Message: b.Raw.Message},
				HealthCheck{ID: "compaction." + b.Book, Area: "archive", Label: "Compaction · " + b.Book,
					Status: b.Compacted.Status, Message: b.Compacted.Message})
		}
		resp.Collector.Status = datahealth.Worst(raw...)
		resp.Collector.Message = map[string]string{
			datahealth.OK:   "flushing trades to S3 on schedule for every book",
			datahealth.Warn: "late or irregular flushes: check the collector",
			datahealth.Fail: "no recent flushes: the collector may be down",
		}[resp.Collector.Status]
	}

	// Daily-executor.
	resp.Executor = s.executorHealth(l, by, books, recs, recErr, now)
	if resp.Executor.LastRun != nil {
		lr := resp.Executor.LastRun
		resp.Checks = append(resp.Checks, HealthCheck{ID: "executor.last_run", Area: "executor", Label: "Last run",
			Status: lr.Status, Message: lr.File + ": " + lr.Message})
	} else {
		resp.Checks = append(resp.Checks, HealthCheck{ID: "executor.last_run", Area: "executor", Label: "Last run",
			Status: datahealth.Unknown, Message: "no run-*.log next to the ledger (use scripts/daily-executor-run.sh)"})
	}
	for _, b := range resp.Executor.Books {
		msg := b.Run.Message
		if n := len(b.Run.MissingDays); n > 0 && b.Run.Status == "missed" {
			msg = fmt.Sprintf("%d closed day(s) not recorded: %s (last bar %s)", n, strings.Join(b.Run.MissingDays, ", "), b.Run.LastBarDate)
		}
		resp.Checks = append(resp.Checks, HealthCheck{ID: "executor.ledger." + b.Book, Area: "executor",
			Label: "Ledger coverage · " + b.Book, Status: runStatusHealth(b.Run.Status), Message: msg})
	}
	resp.Checks = append(resp.Checks, HealthCheck{ID: "executor.upload", Area: "executor", Label: "Ledger copy in S3",
		Status: resp.Executor.Upload.Status, Message: resp.Executor.Upload.Message})

	var all []string
	for _, c := range resp.Checks {
		if c.Status != datahealth.Off {
			all = append(all, c.Status)
		}
	}
	resp.Status = datahealth.Worst(all...)
	writeJSON(w, http.StatusOK, resp)
}

// runStatusHealth maps the ledger run status: a pending run (before 06:00
// Mexico City) is fine; a missed day is a failure.
func runStatusHealth(s string) string {
	switch s {
	case "ok", "pending":
		return datahealth.OK
	case "missed":
		return datahealth.Fail
	}
	return datahealth.Warn
}

func (s *Server) executorHealth(l Ledger, by map[string][]dailyledger.Record, books []string,
	recs []dailyledger.Record, recErr error, now time.Time) ExecutorHealth {
	e := ExecutorHealth{LedgerPath: l.Store.Where(l.Store.LedgerPath), Records: len(recs), Books: []ExecutorBook{}, Runs: []datahealth.RunLog{}}
	if fi, found, err := l.Store.LedgerInfo(); err == nil && found {
		e.LedgerFound, e.LedgerModifiedAt = true, fi.ModTime.UTC().Format(time.RFC3339)
	}
	for _, rec := range recs {
		if rec.RecordedAt > e.LastRecordedAt {
			e.LastRecordedAt = rec.RecordedAt
		}
	}
	statuses := []string{}
	for _, b := range books {
		last := ""
		if rs := by[b]; len(rs) > 0 {
			last = rs[len(rs)-1].Decision.BarDate
		}
		rs := runStatus(last, now)
		e.Books = append(e.Books, ExecutorBook{Book: b, Run: rs})
		statuses = append(statuses, runStatusHealth(rs.Status))
	}
	runs, err := readRuns(l.Store, runLogsShown, now)
	if err != nil {
		s.Log.Printf("run logs %s: %v", l.Name, err)
	}
	if len(runs) > 0 {
		e.Runs = runs
		e.LastRun = &runs[0]
		statuses = append(statuses, runs[0].Status)
	}

	e.Upload = UploadHealth{Status: datahealth.Off,
		Message: "not uploaded: set DAILY_EXECUTOR_S3_URI for scripts/daily-executor-run.sh"}
	if e.LastRun != nil {
		switch e.LastRun.Upload {
		case "ok":
			e.Upload = UploadHealth{Status: datahealth.OK, Message: "the last run uploaded the ledger",
				Target: e.LastRun.UploadTarget, At: e.LastRun.ModifiedAt}
		case "failed":
			e.Upload = UploadHealth{Status: datahealth.Fail, Message: "the last run's upload failed: the S3 copy is stale",
				Target: e.LastRun.UploadTarget, At: e.LastRun.ModifiedAt}
		}
	}

	switch {
	case recErr != nil:
		e.Status, e.Message = datahealth.Fail, "cannot read the ledger: "+recErr.Error()
	case !e.LedgerFound:
		e.Status, e.Message = datahealth.Warn, "no ledger file yet"
	default:
		e.Status = datahealth.Worst(statuses...)
		e.Message = map[string]string{
			datahealth.OK:      "every closed day recorded; the last run exited cleanly",
			datahealth.Unknown: "a run may be in progress",
			datahealth.Warn:    "check the last run's log",
			datahealth.Fail:    "a closed day is missing or the last run failed",
		}[e.Status]
	}
	return e
}

// readRuns returns the newest n run-*.log files next to the ledger (on disk
// or in its S3 copy), newest first.
func readRuns(st *store.Store, n int, now time.Time) ([]datahealth.RunLog, error) {
	files, err := st.Files("run-", ".log")
	if err != nil {
		return nil, err
	}
	var logs []store.Info
	for _, f := range files {
		if datahealth.IsRunLog(f.Name) {
			logs = append(logs, f)
		}
	}
	// The UTC stamp sorts lexically.
	sort.Slice(logs, func(i, j int) bool { return logs[i].Name > logs[j].Name })
	if len(logs) > n {
		logs = logs[:n]
	}
	out := make([]datahealth.RunLog, 0, len(logs))
	for _, f := range logs {
		b, err := st.ReadFile(f.Path)
		if err != nil {
			return out, err
		}
		r, err := datahealth.ParseRun(f.Name, f.ModTime, bytes.NewReader(b), now)
		if err != nil {
			return out, err
		}
		out = append(out, r)
	}
	return out, nil
}

// NewArchive opens the S3 archive named by uri (s3://bucket). Empty disables it.
func NewArchive(ctx context.Context, uri string) (objstore.Store, error) {
	if uri == "" {
		return nil, nil
	}
	bucket, _, err := objstore.ParseURI(uri)
	if err != nil {
		return nil, err
	}
	return objstore.NewS3(ctx, bucket)
}
