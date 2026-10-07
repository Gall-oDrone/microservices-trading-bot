// Command ui-alerts is risk step R3: it evaluates the same data-health and
// risk views as ui-api, in process (it does not need ui-api running), and
// notifies the operator when something needs attention: a missed or failed
// executor run, a blocked order, a risk warning, a stale collector, a
// compaction lag or a failed ledger upload.
//
// Run it every 15 minutes (scripts/install-ops-cron.sh). Each alert is sent when
// it first appears or escalates, again every -repeat while it stays open, and
// once more when it resolves; the open alerts are kept in -state. Nothing is
// sent when nothing changed.
//
//	go run ./cmd/ui-alerts -dry-run                      # print what would be sent
//	go run ./cmd/ui-alerts -notify sns:arn:aws:sns:us-east-1:<account>:mtb-operator-alerts
//	go run ./cmd/ui-alerts -notify sns:<arn> -test       # check the subscription
//
// It reads the ledgers and the archive like ui-api (same flags and UI_API_*
// variables) and never writes them; it writes only its state file and
// publishes to the topic.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"bitso-trading-platform/shared/pkg/risk"
	"bitso-trading-platform/ui-api/internal/alerts"
	"bitso-trading-platform/ui-api/internal/api"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func defaultState() string {
	if d := os.Getenv("XDG_STATE_HOME"); d != "" {
		return filepath.Join(d, "mtb-ui-alerts", "state.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "mtb-ui-alerts", "state.json")
}

func main() {
	ledger := flag.String("ledger", env("UI_API_LEDGER", "../strategy-executor/daily-executor-data/stage/ledger.jsonl"), "daily-executor ledger, or s3://bucket/prefix")
	candles := flag.String("candles-dir", env("UI_API_CANDLES_DIR", ""), "candles dir (default: <ledger dir>/candles)")
	ledgerSpec := flag.String("ledgers", env("UI_API_LEDGERS", ""), "named ledgers name=path,… (overrides -ledger); every ledger is checked")
	policyPath := flag.String("risk-policy", env("UI_API_RISK_POLICY", ""), "risk policy JSON (default: built-in)")
	stageSize := flag.Float64("stage-size", envFloat("UI_API_STAGE_SIZE", 0.001), "BTC per stage entry")
	archiveURI := flag.String("archive", env("UI_API_ARCHIVE", ""), "collector archive s3://bucket (default: off)")
	statePath := flag.String("state", env("UI_ALERTS_STATE", defaultState()), "open alerts between runs (JSON)")
	notify := flag.String("notify", env("UI_ALERTS_NOTIFY", "stdout"), "stdout, or sns:<topic ARN>")
	repeat := flag.Duration("repeat", envDuration("UI_ALERTS_REPEAT", 12*time.Hour), "re-send an open alert this often (0: only when new, escalated or resolved)")
	uiURL := flag.String("ui-url", env("UI_ALERTS_UI_URL", "http://127.0.0.1:5173"), "operator UI, linked in the message")
	dryRun := flag.Bool("dry-run", false, "print what would be sent; do not notify or write the state")
	test := flag.Bool("test", false, "send one test message and exit")
	flag.Parse()
	logger := log.New(os.Stderr, "ui-alerts ", log.LstdFlags|log.LUTC|log.Lmsgprefix)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	var n alerts.Notifier = alerts.Writer{W: os.Stdout}
	if !*dryRun {
		var err error
		if n, err = notifier(ctx, *notify); err != nil {
			logger.Print(err)
			os.Exit(2)
		}
	}
	if *test {
		host, _ := os.Hostname()
		body := fmt.Sprintf("Test message from ui-alerts on %s at %s UTC.\nIf you can read this, R3 alerts reach you.\n", host, time.Now().UTC().Format("2006-01-02 15:04"))
		if err := n.Notify(ctx, "[mtb-ops] test: alerts reach you", body); err != nil {
			logger.Print(err)
			os.Exit(1)
		}
		logger.Printf("test message sent via %s", n.Describe())
		return
	}

	pol, err := risk.LoadPolicy(*policyPath)
	if err != nil {
		logger.Print(err)
		os.Exit(2)
	}
	ledgers, err := openLedgers(*ledger, *candles, *ledgerSpec)
	if err != nil {
		logger.Print(err)
		os.Exit(2)
	}
	srv := &api.Server{Ledgers: ledgers, Policy: pol, PolicySrc: *policyPath, StageSize: *stageSize,
		Version: "ui-alerts", Log: log.New(io.Discard, "", 0)}
	if srv.Archive, err = api.NewArchive(ctx, *archiveURI); err != nil {
		logger.Print("-archive: ", err)
		os.Exit(2)
	}

	cur := evaluate(srv.Handler(), ledgers)
	prev, err := loadState(*statePath)
	if err != nil {
		logger.Print(err) // a corrupt state re-sends everything once
	}
	now := time.Now().UTC()
	notice, next := alerts.Diff(prev, cur, now, *repeat)
	logger.Printf("%d open (%s); new %d, escalated %d, reminders %d, resolved %d",
		len(cur), counts(cur), len(notice.New), len(notice.Escalated), len(notice.Reminder), len(notice.Resolved))

	if !notice.Empty() {
		subject, body := alerts.Render(notice, *uiURL)
		if err := n.Notify(ctx, subject, body); err != nil {
			logger.Print(err) // state not saved: the next run retries
			os.Exit(1)
		}
		logger.Printf("sent via %s: %s", n.Describe(), subject)
	}
	if *dryRun {
		return
	}
	if err := saveState(*statePath, next); err != nil {
		logger.Print(err)
		os.Exit(1)
	}
}

func notifier(ctx context.Context, spec string) (alerts.Notifier, error) {
	switch {
	case spec == "stdout":
		return alerts.Writer{W: os.Stdout}, nil
	case strings.HasPrefix(spec, "sns:"):
		return alerts.NewSNS(ctx, strings.TrimPrefix(spec, "sns:"))
	}
	return nil, fmt.Errorf("-notify %q: want stdout or sns:<topic ARN>", spec)
}

func openLedgers(path, candles, spec string) ([]api.Ledger, error) {
	if spec != "" {
		return api.ParseLedgers(spec)
	}
	st, err := api.OpenStore(path, candles, nil)
	if err != nil {
		return nil, err
	}
	return []api.Ledger{{Name: "stage", Store: st}}, nil
}

// evaluate calls ui-api's own handlers in process for every ledger.
func evaluate(h http.Handler, ledgers []api.Ledger) []alerts.Alert {
	var groups [][]alerts.Alert
	for _, l := range ledgers {
		q := "?ledger=" + url.QueryEscape(l.Name)
		var dh api.DataHealthResponse
		if err := call(h, "/api/ui/health/data"+q, &dh); err != nil {
			groups = append(groups, []alerts.Alert{alerts.EvalError(l.Name, "data health", err)})
		} else {
			groups = append(groups, alerts.FromHealth(l.Name, dh))
		}
		var rr api.RiskResponse
		if err := call(h, "/api/ui/risk"+q, &rr); err != nil {
			groups = append(groups, []alerts.Alert{alerts.EvalError(l.Name, "risk", err)})
		} else {
			groups = append(groups, alerts.FromRisk(l.Name, rr))
		}
	}
	return alerts.Merge(groups...)
}

func call(h http.Handler, path string, v any) error {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &e)
		return fmt.Errorf("%s: HTTP %d: %s", path, rec.Code, e.Error)
	}
	return json.Unmarshal(rec.Body.Bytes(), v)
}

func counts(as []alerts.Alert) string {
	c, w := 0, 0
	for _, a := range as {
		if a.Severity == alerts.Critical {
			c++
		} else {
			w++
		}
	}
	return fmt.Sprintf("%d critical, %d warning", c, w)
}

func loadState(path string) (alerts.State, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return alerts.State{}, nil
	}
	if err != nil {
		return alerts.State{}, err
	}
	var s alerts.State
	if err := json.Unmarshal(b, &s); err != nil {
		return alerts.State{}, fmt.Errorf("state %s: %w", path, err)
	}
	return s, nil
}

// saveState writes atomically (temp file + rename).
func saveState(path string, s alerts.State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func envFloat(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
