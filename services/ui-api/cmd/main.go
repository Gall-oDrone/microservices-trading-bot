// Command ui-api is the read-only backend for the web UI (web/). It reads the
// daily-executor's ledger and candle files, evaluates the risk policy from
// shared/pkg/risk against them, and serves JSON under /api/ui/. With -static
// it also serves the built web app.
//
// Phase 1 runs on localhost only: it refuses a non-loopback address unless
// UI_API_ALLOW_REMOTE=1, because the API has no authentication yet
// (docs/frontend/FRONTEND-UI-PLAN-2026-10-03.md §6).
//
//	go run ./cmd -ledger ../strategy-executor/daily-executor-data/stage/ledger.jsonl
//	go run ./cmd -ledgers stage=../strategy-executor/daily-executor-data/stage/ledger.jsonl,dry-run=../strategy-executor/daily-executor-data/ledger.jsonl
//	go run ./cmd -static ../../web/dist   # also serve the built UI
//
// The Research page reads the study write-ups from -studies-dir
// (default ../../docs/backtest-readiness), read-only.
//
// The Data health page lists the collector's S3 archive when -archive is set
// (go run ./cmd -archive s3://mtb-development-data-archive-<account>), with
// the default AWS credentials; it only lists and reads.
//
// A ledger can also be read from the copy scripts/daily-executor-run.sh
// uploads (DAILY_EXECUTOR_S3_URI), list and get only:
//
//	go run ./cmd -ledgers stage=s3://<bucket>/daily-executor/stage,local=../strategy-executor/daily-executor-data/stage/ledger.jsonl
//
// Operator controls (R4: halt/resume from the Risk page, audited) are off
// unless -operator-token-file names a 0600 file with the token, and then
// only on a loopback address:
//
//	umask 077; openssl rand -hex 32 > ~/.config/mtb/operator-token
//	go run ./cmd -operator-token-file ~/.config/mtb/operator-token
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"syscall"
	"time"

	"bitso-trading-platform/shared/pkg/risk"
	"bitso-trading-platform/ui-api/internal/api"
	"bitso-trading-platform/ui-api/internal/live"
	"bitso-trading-platform/ui-api/internal/research"
	"bitso-trading-platform/ui-api/internal/store"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	addr := flag.String("addr", env("UI_API_ADDR", "127.0.0.1:8090"), "listen address (loopback only unless UI_API_ALLOW_REMOTE=1)")
	ledger := flag.String("ledger", env("UI_API_LEDGER", "../strategy-executor/daily-executor-data/stage/ledger.jsonl"), "daily-executor ledger (JSONL), or its S3 copy s3://bucket/prefix")
	candles := flag.String("candles-dir", env("UI_API_CANDLES_DIR", ""), "daily-executor candles dir (default: <ledger dir>/candles)")
	ledgerSpec := flag.String("ledgers", env("UI_API_LEDGERS", ""), "named ledgers name=path,name=s3://bucket/prefix (first is the default; overrides -ledger and -candles-dir)")
	policyPath := flag.String("risk-policy", env("UI_API_RISK_POLICY", ""), "risk policy JSON (default: built-in shared/pkg/risk.DefaultPolicy)")
	static := flag.String("static", env("UI_API_STATIC_DIR", ""), "serve the built web app from this dir (e.g. ../../web/dist)")
	stageSize := flag.Float64("stage-size", envFloat("UI_API_STAGE_SIZE", 0.001), "BTC per stage entry, as passed to daily-executor -size")
	printPolicy := flag.Bool("print-default-policy", false, "print the built-in risk policy as JSON and exit")
	liveOn := flag.Bool("live", env("UI_API_LIVE", "1") != "0", "stream display-only market data from Bitso's public WebSocket (UI_API_LIVE=0 disables)")
	liveURL := flag.String("live-url", env("UI_API_LIVE_URL", live.DefaultURL), "Bitso public WebSocket URL (production; no keys)")
	liveREST := flag.String("live-rest-url", env("UI_API_LIVE_REST_URL", live.DefaultRESTURL), "Bitso public REST API, for today's bar")
	studiesDir := flag.String("studies-dir", env("UI_API_STUDIES_DIR", "../../docs/backtest-readiness"), "study write-ups (markdown) for the Research page")
	archiveURI := flag.String("archive", env("UI_API_ARCHIVE", ""), "collector trade archive s3://bucket for the data-health page, read-only (default: off)")
	tokenFile := flag.String("operator-token-file", env("UI_API_OPERATOR_TOKEN_FILE", ""), "enable operator controls (halt/resume, audited) with the token in this 0600 file (default: off, read-only)")
	flag.Parse()

	if *printPolicy {
		b, _ := json.MarshalIndent(risk.DefaultPolicy(), "", "  ")
		fmt.Println(string(b))
		return
	}
	logger := log.New(os.Stdout, "ui-api ", log.LstdFlags|log.LUTC|log.Lmsgprefix)

	if err := checkLoopback(*addr); err != nil {
		logger.Fatal(err)
	}
	pol, err := risk.LoadPolicy(*policyPath)
	if err != nil {
		logger.Fatal(err)
	}
	src := *policyPath
	if src == "" {
		src = "built-in default (" + pol.Version + ")"
	}
	if *static != "" {
		if fi, err := os.Stat(*static); err != nil || !fi.IsDir() {
			logger.Fatalf("-static %s is not a directory (run `npm run build` in web/)", *static)
		}
	}

	st, err := api.OpenStore(*ledger, *candles, nil)
	if err != nil {
		logger.Fatal("-ledger: ", err)
	}
	ledgers := []api.Ledger{{Name: "stage", Store: st}}
	if *ledgerSpec != "" {
		if ledgers, err = api.ParseLedgers(*ledgerSpec); err != nil {
			logger.Fatal("-ledgers: ", err)
		}
	}
	srv := &api.Server{Ledgers: ledgers, Policy: pol, PolicySrc: src, StageSize: *stageSize, StaticDir: *static,
		Version: version(), Log: logger,
		Research: &research.Index{Dir: *studiesDir, RepoRel: repoRel(*studiesDir)}}
	if srv.Archive, err = api.NewArchive(context.Background(), *archiveURI); err != nil {
		logger.Fatal("-archive: ", err)
	}
	if *tokenFile != "" {
		if !isLoopback(*addr) {
			logger.Fatalf("-operator-token-file needs a loopback -addr (got %s): no TLS or OIDC yet", *addr)
		}
		if srv.OperatorToken, err = api.LoadOperatorToken(*tokenFile); err != nil {
			logger.Fatal("-operator-token-file: ", err)
		}
	}
	ctx, stopLive := context.WithCancel(context.Background())
	defer stopLive()
	if *liveOn {
		srv.Live = startLive(ctx, ledgers[0].Store, *liveURL, *liveREST, logger)
	}
	hs := &http.Server{
		Addr: *addr, Handler: srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		// /api/ui/stream clears this per request (http.ResponseController).
		WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second,
	}
	if srv.Live != nil {
		hs.RegisterOnShutdown(srv.Live.Close) // ends open SSE streams
	}

	go func() {
		for _, l := range ledgers {
			logger.Printf("ledger %s: %s | candles %s", l.Name, l.Store.Where(l.Store.LedgerPath), l.Store.Where(l.Store.CandlesDir))
			if l.Store.Remote() {
				// The first S3 call resolves credentials and connects (seconds);
				// do it now rather than on the first page load.
				go func(l api.Ledger) {
					if _, err := l.Store.Records(); err != nil {
						logger.Printf("ledger %s: %v", l.Name, err)
					}
				}(l)
			}
		}
		logger.Printf("studies: %s", *studiesDir)
		if srv.Archive != nil {
			logger.Printf("archive: %s (read-only)", srv.Archive.Describe())
		}
		logger.Printf("listening on http://%s | policy %s", *addr, src)
		if err := hs.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatal(err)
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	stopLive()
	sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := hs.Shutdown(sctx); err != nil {
		logger.Printf("shutdown: %v", err)
	}
}

// repoRel reports dir relative to the git repository root (the nearest parent
// with a .git entry), for display; it falls back to dir as given.
func repoRel(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return filepath.ToSlash(dir)
	}
	for root := abs; ; {
		if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
			if rel, err := filepath.Rel(root, abs); err == nil {
				return filepath.ToSlash(rel)
			}
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			break
		}
		root = parent
	}
	return filepath.ToSlash(dir)
}

// startLive connects one shared upstream for the forward-test books. Closed
// daily closes for the provisional flip level come from the default ledger's
// candle files (the executor's), never from the live feed.
func startLive(ctx context.Context, st *store.Store, wsURL, restURL string, logger *log.Logger) *live.Hub {
	books := make([]string, 0, len(api.PreregDates))
	for b := range api.PreregDates {
		books = append(books, b)
	}
	closes := func(book string) ([]float64, string, error) {
		rows, _, err := st.Candles(book)
		if err != nil {
			return nil, "", err
		}
		if len(rows) == 0 {
			return nil, "", errors.New("empty candle file")
		}
		cs := make([]float64, len(rows))
		for i, r := range rows {
			cs[i] = r.Close
		}
		return cs, rows[len(rows)-1].Date, nil
	}
	hub := live.NewHub(books, wsURL, live.RESTSeeder(restURL, nil), closes, logger)
	hub.Tape = live.RESTTapeSeeder(restURL, nil) // the Market page's recent trades
	feed := &live.Feed{URL: wsURL, Books: hub.Books, Handler: hub, Log: logger}
	go hub.Run(ctx)
	go feed.Run(ctx)
	logger.Printf("live: %s for %v (display only)", wsURL, hub.Books)
	return hub
}

func envFloat(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

// checkLoopback refuses to listen beyond localhost while there is no auth.
func checkLoopback(addr string) error {
	if os.Getenv("UI_API_ALLOW_REMOTE") == "1" {
		return nil
	}
	if _, _, err := net.SplitHostPort(addr); err != nil {
		return fmt.Errorf("addr %q: %w", addr, err)
	}
	if isLoopback(addr) {
		return nil
	}
	return fmt.Errorf("refusing to listen on %q: the API has no auth yet; use 127.0.0.1 or set UI_API_ALLOW_REMOTE=1", addr)
}

// isLoopback reports whether a listen address is localhost or a loopback IP.
func isLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func version() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" && len(s.Value) >= 12 {
				return s.Value[:12]
			}
		}
	}
	return "dev"
}
