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
//	go run ./cmd -static ../../web/dist   # also serve the built UI
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
	"runtime/debug"
	"strconv"
	"syscall"
	"time"

	"bitso-trading-platform/shared/pkg/risk"
	"bitso-trading-platform/ui-api/internal/api"
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
	ledger := flag.String("ledger", env("UI_API_LEDGER", "../strategy-executor/daily-executor-data/stage/ledger.jsonl"), "daily-executor ledger (JSONL)")
	candles := flag.String("candles-dir", env("UI_API_CANDLES_DIR", ""), "daily-executor candles dir (default: <ledger dir>/candles)")
	policyPath := flag.String("risk-policy", env("UI_API_RISK_POLICY", ""), "risk policy JSON (default: built-in shared/pkg/risk.DefaultPolicy)")
	static := flag.String("static", env("UI_API_STATIC_DIR", ""), "serve the built web app from this dir (e.g. ../../web/dist)")
	stageSize := flag.Float64("stage-size", envFloat("UI_API_STAGE_SIZE", 0.001), "BTC per stage entry, as passed to daily-executor -size")
	printPolicy := flag.Bool("print-default-policy", false, "print the built-in risk policy as JSON and exit")
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

	st := store.New(*ledger, *candles)
	srv := &api.Server{Store: st, Policy: pol, PolicySrc: src, StageSize: *stageSize, StaticDir: *static,
		Version: version(), Log: logger}
	hs := &http.Server{
		Addr: *addr, Handler: srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second,
	}

	go func() {
		logger.Printf("listening on http://%s | ledger %s | candles %s | policy %s", *addr, st.LedgerPath, st.CandlesDir, src)
		if err := hs.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatal(err)
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := hs.Shutdown(ctx); err != nil {
		logger.Printf("shutdown: %v", err)
	}
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
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("addr %q: %w", addr, err)
	}
	if host == "localhost" {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("refusing to listen on %q: the API has no auth yet; use 127.0.0.1 or set UI_API_ALLOW_REMOTE=1", addr)
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
