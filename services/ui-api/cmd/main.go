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
//
// The Strategies page also lists (and, with the token, starts and stops) the
// intraday strategy-executor's strategies when -strategy-executor-url points
// at it on a loopback address:
//
//	go run ./cmd -operator-token-file ~/.config/mtb/operator-token -strategy-executor-url http://127.0.0.1:8081
//
// The eToro index-CFD demo executor's ledger (porting plan P5a) is served
// under /api/ui/etoro/ and reached by the halt controls when -etoro-ledger
// names it (UI_API_ETORO_LEDGER; scripts/install-ops-cron.sh writes it to
// ops.env). It is a local file, never an S3 copy:
//
//	go run ./cmd -etoro-ledger ../strategy-executor/etoro-daily-data/demo/ledger.jsonl
//
// With an eToro ledger, ui-api also polls eToro's rates route for NSDQ100
// and SPX500 every 5 s (porting plan P5c; 12 reads a minute, display only,
// never a trading or portfolio route) with the demo keys in -etoro-env-file
// (UI_API_ETORO_ENV_FILE, default ~/.config/microservices-trading-bot/
// etoro-demo.env, which must say ETORO_ENV=demo); -etoro-live=false or a
// missing env file turns the feed off and the rest keeps serving.
//
// Assisted trades on the demo account (porting plan P5d, the Trade page) are
// OFF by default. -etoro-trading (UI_API_ETORO_TRADING=1) turns them on and
// needs all of: -operator-token-file, -etoro-ledger (a local file) and the
// env file with ETORO_ENV=demo; a missing prerequisite is fatal rather than
// a silent read-only start. The real account is refused in code:
//
//	go run ./cmd -operator-token-file ~/.config/mtb/operator-token \
//	  -etoro-ledger ../strategy-executor/etoro-daily-data/demo/ledger.jsonl -etoro-trading
//
// Real-account canary (2026-10-22, docs/etoro/ETORO-REAL-CANARY-PLAN-2026-10-21.md):
// a SEPARATE process on its own port, env file, operator token and ledger
// directory may trade the real account for the measurement canary. It
// needs -etoro-real-canary on the command line (visible in ps; there is
// no environment variable for it) and every one of the six conditions in
// shared/pkg/etoro/realack.go, or the start is fatal and names the missing
// one: never a silent read-only start. The executor keeps refusing real.
//
//	go run ./cmd -addr 127.0.0.1:8091 \
//	  -operator-token-file ~/.config/mtb/operator-token-real-canary \
//	  -etoro-env-file ~/.config/microservices-trading-bot/etoro-real-canary.env \
//	  -etoro-ledger etoro-real-canary=../strategy-executor/etoro-daily-data/real-canary/ledger.jsonl \
//	  -etoro-trading -etoro-real-canary
//
// -canary-rehearsal runs the same canary policy on the DEMO account with a
// ledger of its own (plan §7's rehearsal), demo keys and REAL-less phrases.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	"strings"
	"syscall"
	"time"

	"bitso-trading-platform/shared/pkg/etoro"
	"bitso-trading-platform/shared/pkg/risk"
	"bitso-trading-platform/ui-api/internal/api"
	"bitso-trading-platform/ui-api/internal/etorolive"
	"bitso-trading-platform/ui-api/internal/executor"
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
	executorURL := flag.String("strategy-executor-url", env("UI_API_STRATEGY_EXECUTOR_URL", ""), "intraday strategy-executor base URL (loopback) for the Strategies page; start/stop also needs -operator-token-file (default: off)")
	strategyAudit := flag.String("strategy-audit-file", env("UI_API_STRATEGY_AUDIT_FILE", ""), "strategy start/stop audit log (default: "+api.StrategyAuditFile+" next to the first local ledger)")
	bookSamples := flag.String("book-samples", env("UI_API_BOOK_SAMPLES", "../strategy-executor/daily-executor-data/book-samples"), "hourly order-book samples dir (<dir>/<book>.jsonl, cmd/book-sampler) for the capacity distribution; missing means none")
	preregDirs := flag.String("prereg-dirs", env("UI_API_PREREG_DIRS", "../strategy-executor/daily-executor-data/prereg,../../docs/backtest-readiness"), "where to look for the latest pre-registered verdict report (scripts/prereg-evaluation.sh output), comma-separated")
	etoroLedger := flag.String("etoro-ledger", env("UI_API_ETORO_LEDGER", ""), "eToro executor ledger(s), local disk, for /api/ui/etoro/ and the halt controls: a path (named by -etoro-name) or name=path,name=path,… (since 2026-10-21: the index-CFD, ETF and dual-momentum cells; ?ledger=name selects one, the first is the default) (default: off)")
	etoroName := flag.String("etoro-name", env("UI_API_ETORO_NAME", "etoro-demo"), "ledger name of a plain-path -etoro-ledger (?ledger=, the confirm phrase of its halt/resume)")
	etoroLiveOn := flag.Bool("etoro-live", env("UI_API_ETORO_LIVE", "1") != "0", "with -etoro-ledger, poll eToro's rates for the index CFDs every 5 s, display only (UI_API_ETORO_LIVE=0 disables)")
	etoroEnvFile := flag.String("etoro-env-file", env("UI_API_ETORO_ENV_FILE", etorolive.DefaultEnvFile()), "KEY=VALUE file with the eToro demo keys for the rates poll (ETORO_ENV must be demo); missing turns the feed off")
	etoroTrading := flag.Bool("etoro-trading", env("UI_API_ETORO_TRADING", "0") == "1", "enable the assisted trades (Trade page) on the eToro DEMO ledger; needs -operator-token-file, -etoro-ledger and -etoro-env-file with ETORO_ENV=demo (default: off)")
	realCanary := flag.Bool("etoro-real-canary", false, "REAL ACCOUNT: trade the measurement canary (one eToro ledger under a real-canary/ directory, -etoro-env-file with ETORO_ENV=real, its own -operator-token-file, the acknowledgement file); every condition of the acknowledgement must hold or the start is fatal; command line only, no env var")
	realAckFile := flag.String("real-ack-file", env("UI_API_REAL_ACK_FILE", defaultRealAckFile()), "the REAL-ACCOUNT-ACK file -etoro-real-canary checks")
	demoTokenFile := flag.String("demo-operator-token-file", env("UI_API_DEMO_OPERATOR_TOKEN_FILE", defaultDemoTokenFile()), "the demo console's operator token file; -etoro-real-canary refuses to start with the same token")
	rehearsal := flag.Bool("canary-rehearsal", env("UI_API_CANARY_REHEARSAL", "0") == "1", "run the canary policy (x20, 50-60 USD) on the DEMO account with its own ledger: the rehearsal before the real canary; needs -etoro-trading")
	flag.Parse()

	if *realCanary && *rehearsal {
		log.Fatal("ui-api: -etoro-real-canary and -canary-rehearsal exclude each other")
	}

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
	firstEtoro := -1
	if *etoroLedger != "" {
		// Appended after the Bitso ledgers: the first ledger stays the
		// default of the Bitso routes and of /healthz; the first eToro
		// ledger is the default of the eToro routes, the live feed and the
		// assisted trades.
		for _, nl := range api.ParseNamedLedgers(*etoroLedger, *etoroName) {
			if strings.HasPrefix(nl.Path, "s3://") {
				logger.Fatalf("-etoro-ledger %s: must be the local file the executor writes, not an S3 copy", nl.Name)
			}
			if !api.ValidLedgerName(nl.Name) {
				logger.Fatalf("-etoro-ledger name %q: use lowercase letters, digits, '-' or '_' (max 32)", nl.Name)
			}
			for _, l := range ledgers {
				if l.Name == nl.Name {
					logger.Fatalf("-etoro-ledger name %q is already a ledger name", nl.Name)
				}
			}
			if firstEtoro < 0 {
				firstEtoro = len(ledgers)
			}
			l := api.Ledger{Name: nl.Name, Store: store.New(nl.Path, ""), Venue: api.VenueEtoro}
			if *realCanary {
				l.Environment = string(etoro.EnvReal)
			}
			ledgers = append(ledgers, l)
		}
	}
	if *realCanary {
		// The canary process serves exactly one eToro ledger, the canary's,
		// named etoro-real-canary (the halt phrase of plan §3.2), and trades it.
		n := 0
		for _, l := range ledgers {
			if l.Venue == api.VenueEtoro {
				n++
				if l.Name != api.RealCanaryLedgerName {
					logger.Fatalf("-etoro-real-canary: the eToro ledger must be named %s (got %q): -etoro-ledger %s=<path>", api.RealCanaryLedgerName, l.Name, api.RealCanaryLedgerName)
				}
			}
		}
		if n != 1 {
			logger.Fatalf("-etoro-real-canary: exactly one eToro ledger (the canary's), got %d", n)
		}
		if !*etoroTrading {
			logger.Fatal("-etoro-real-canary without -etoro-trading makes no sense: the canary process exists to trade the canary ledger; for a read-only view of it start without -etoro-real-canary")
		}
	}
	srv := &api.Server{Ledgers: ledgers, Policy: pol, PolicySrc: src, StageSize: *stageSize, StaticDir: *static,
		Version: version(), Log: logger, BookSamplesDir: *bookSamples,
		PreregDirs: splitDirs(*preregDirs),
		Research:   &research.Index{Dir: *studiesDir, RepoRel: repoRel(*studiesDir)}}
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
	if *executorURL != "" {
		if srv.Executor, err = executor.New(*executorURL); err != nil {
			logger.Fatal("-strategy-executor-url: ", err)
		}
		srv.StrategyAuditPath = *strategyAudit
		logger.Printf("strategies: strategy-executor %s", srv.Executor.URL())
	}
	ctx, stopLive := context.WithCancel(context.Background())
	defer stopLive()
	if *liveOn {
		srv.Live = startLive(ctx, ledgers[0].Store, *liveURL, *liveREST, logger)
	}
	if firstEtoro >= 0 && *etoroLiveOn {
		// The feed polls the index CFDs and reads their closed bars from the
		// first eToro ledger (the CFD cell), not from whichever cell is last.
		// Market data is environment independent: the canary process polls
		// with its own (real) key, the demo process with the demo key.
		srv.EtoroLive = startEtoroLive(ctx, ledgers[firstEtoro], *etoroEnvFile, *realCanary, logger)
	}
	if *etoroTrading {
		switch {
		case *etoroLedger == "":
			logger.Fatal("-etoro-trading needs -etoro-ledger (the local demo ledger)")
		case srv.OperatorToken == "":
			logger.Fatal("-etoro-trading needs -operator-token-file: the trades are token-gated like the halt controls")
		}
		switch {
		case *realCanary:
			srv.Trading = startRealCanary(ledgers, ledgers[firstEtoro], *etoroEnvFile, *realAckFile, *tokenFile, *demoTokenFile, srv.OperatorToken, logger)
		case *rehearsal:
			cfg, err := etorolive.Credentials(*etoroEnvFile)
			if err != nil {
				logger.Fatalf("-canary-rehearsal: %v (the env file must exist, be 0600 and say ETORO_ENV=demo)", err)
			}
			if srv.Trading, err = api.NewEtoroCanaryRehearsal(cfg); err != nil {
				logger.Fatal("-canary-rehearsal: ", err)
			}
			for _, l := range ledgers {
				if l.Venue == api.VenueEtoro && strings.HasPrefix(l.Name, "etoro-demo") {
					logger.Fatalf("-canary-rehearsal: refusing to run the canary policy on the demo cell's ledger %s; give the rehearsal its own ledger (e.g. etoro-canary-rehearsal=…/etoro-daily-data/rehearsal-canary/ledger.jsonl)", l.Name)
				}
			}
			logger.Printf("etoro trading: CANARY REHEARSAL on the demo account (ledger %s, policy %s, x%v, %s tokens)", ledgers[firstEtoro].Name, srv.Trading.Policy.Version, srv.Trading.Policy.For("nsdq100").Leverages, api.TradeTokenTTL)
		default:
			cfg, err := etorolive.Credentials(*etoroEnvFile)
			if err != nil {
				logger.Fatalf("-etoro-trading: %v (the env file must exist, be 0600 and say ETORO_ENV=demo)", err)
			}
			if srv.Trading, err = api.NewEtoroTrading(cfg); err != nil {
				logger.Fatal("-etoro-trading: ", err)
			}
			logger.Printf("etoro trading: ASSISTED TRADES ENABLED on the demo account (ledger %s, token-gated, %s tokens)", ledgers[firstEtoro].Name, api.TradeTokenTTL)
		}
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
	if srv.EtoroLive != nil {
		hs.RegisterOnShutdown(srv.EtoroLive.Close)
	}

	go func() {
		for _, l := range ledgers {
			venue := ""
			if l.Venue != "" {
				venue = " (" + l.Venue + ")"
			}
			logger.Printf("ledger %s%s: %s | candles %s", l.Name, venue, l.Store.Where(l.Store.LedgerPath), l.Store.Where(l.Store.CandlesDir))
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

// startRealCanary builds the REAL broker for the measurement canary or
// dies naming the condition that failed (plan §5.2: never a silent
// read-only start). It reads the real env file (ETORO_ENV=real required),
// the acknowledgement file (0600, the exact sentence, the key prefix, the
// ledger, the policy, the window, a name), hashes the operator tokens and
// hands everything to api.NewEtoroRealCanary, which runs
// etoro.CheckRealAck before building anything. On success it logs one line
// with the six facts and no secret.
func startRealCanary(all []api.Ledger, canary api.Ledger, envFile, ackFile, tokenFile, demoTokenFile, token string, logger *log.Logger) *api.EtoroTrading {
	cfg, err := etorolive.RealCredentials(envFile)
	if err != nil {
		logger.Fatalf("-etoro-real-canary REFUSED (condition 2, the env file): %v (the env file must exist, be 0600 and say ETORO_ENV=real)", err)
	}
	ack, err := etoro.LoadRealAck(ackFile)
	if err != nil {
		logger.Fatalf("-etoro-real-canary REFUSED (condition 3, the acknowledgement): %v", err)
	}
	chk := etoro.RealAckCheck{Flag: true, LedgerPath: canary.Store.LedgerPath, TokenFile: tokenFile, DemoTokenFile: demoTokenFile, Now: time.Now()}
	if abs, err := filepath.Abs(canary.Store.LedgerPath); err == nil {
		chk.LedgerPath = abs
	}
	for _, l := range all {
		if l.Name != canary.Name && l.Store != nil && !l.Store.Remote() {
			p := l.Store.LedgerPath
			if abs, err := filepath.Abs(p); err == nil {
				p = abs
			}
			chk.OtherLedgers = append(chk.OtherLedgers, p)
		}
	}
	chk.TokenSHA256 = sha256Hex(token)
	if demo, err := api.LoadOperatorToken(demoTokenFile); err == nil {
		chk.DemoTokenSHA256 = sha256Hex(demo)
	} else {
		logger.Printf("real canary: the demo operator token %s could not be read (%v): comparing token paths only", demoTokenFile, err)
	}
	tr, err := api.NewEtoroRealCanary(cfg, ack, chk)
	if err != nil {
		logger.Fatalf("-etoro-real-canary REFUSED: %v", err)
	}
	chk.PolicyVersion = tr.Policy.Version
	logger.Printf("etoro trading: %s", ack.Facts(chk))
	logger.Printf("etoro trading: REAL ACCOUNT CANARY ENABLED on ledger %s (%s): policy %s, books %v, x%v, invested %.0f-%.0f USD, one position; phrases carry REAL; %s tokens",
		canary.Name, chk.LedgerPath, tr.Policy.Version, tr.Policy.BookNames(), tr.Policy.For("nsdq100").Leverages,
		tr.Policy.For("nsdq100").MinInvestedUSD, tr.Policy.For("nsdq100").MaxInvestedUSD, api.TradeTokenTTL)
	return tr
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func defaultRealAckFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "microservices-trading-bot", "REAL-ACCOUNT-ACK")
}

func defaultDemoTokenFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "mtb", "operator-token")
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

// startEtoroLive starts the eToro rates poller for the eToro ledger's books
// (porting plan P5c). The hub buckets the forming bar by UTC day like the
// executor's candle files, from which the provisional flip level is read.
// Without usable keys it logs why and returns nil: the console keeps
// serving everything else. GetRates is the only eToro call made here. The
// demo process accepts demo keys only; the canary process (real) its real
// key only: the rates route is the same for both.
func startEtoroLive(ctx context.Context, l api.Ledger, envFile string, real bool, logger *log.Logger) *live.Hub {
	if l.Venue != api.VenueEtoro {
		logger.Printf("etoro live: off (ledger %s is not an eToro ledger)", l.Name)
		return nil
	}
	load := etorolive.Credentials
	if real {
		load = etorolive.RealCredentials
	}
	cfg, err := load(envFile)
	switch {
	case etorolive.IsNotExist(err):
		logger.Printf("etoro live: off (no env file %s; -etoro-env-file)", envFile)
		return nil
	case errors.Is(err, etorolive.ErrNotDemo):
		logger.Printf("etoro live: REFUSED: %v; the demo console only ever uses demo keys, feed off", err)
		return nil
	case errors.Is(err, etorolive.ErrNotReal):
		logger.Printf("etoro live: REFUSED: %v; the canary process only ever uses its real key, feed off", err)
		return nil
	case err != nil:
		logger.Printf("etoro live: off (%v)", err)
		return nil
	}
	client, err := etoro.NewClient(cfg)
	if err != nil {
		logger.Printf("etoro live: off (%v)", err)
		return nil
	}
	books := make([]string, 0, len(etorolive.DefaultBooks))
	for b := range etorolive.DefaultBooks {
		books = append(books, b)
	}
	hub := api.NewEtoroHub(books, api.EtoroCloses(l.Store), logger)
	poller := &etorolive.Poller{Rates: client.GetRates, Books: etorolive.DefaultBooks, Interval: etorolive.DefaultInterval, Handler: hub, Log: logger}
	go hub.Run(ctx)
	go poller.Run(ctx)
	logger.Printf("etoro live: polling rates for %v every %s (display only)", hub.Books, poller.Interval)
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

func splitDirs(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
