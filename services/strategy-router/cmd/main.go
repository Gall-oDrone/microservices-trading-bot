// Command strategy-router is the Phase 2 in-cluster regime router.
// See docs/strategy-fee-accuracy/STRATEGY-REGIME-ROUTER-SERVICE-2026-05-22.md.
package main

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"bitso-trading-platform/strategy-router/internal/clients"
	"bitso-trading-platform/strategy-router/internal/config"
	"bitso-trading-platform/strategy-router/internal/metrics"
	"bitso-trading-platform/strategy-router/internal/router"
	"bitso-trading-platform/strategy-router/internal/server"
)

func main() {
	cfg := config.Load()

	httpClient := clients.NewHTTPClient(cfg.StrategyExecutorURL, cfg.HTTPTimeout)
	m := metrics.Get()
	audit := router.NewFileAudit(cfg.AuditLogPath)

	engines := make([]*router.Engine, 0, len(cfg.Books))
	for _, book := range cfg.Books {
		bookCfg := cfg.ForBook(book)
		engines = append(engines, router.New(bookCfg, httpClient, audit, m))
	}
	coord := router.NewBookCoordinator(engines)

	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	srv := server.New(addr, cfg, coord)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		log.Printf("strategy-router starting on %s (books=%v interval=%s cooldown=%ds dry_run=%t)",
			addr, cfg.Books, cfg.EvaluationInterval, cfg.CooldownSeconds, cfg.DryRun)
		if err := srv.Start(); err != nil && err.Error() != "http: Server closed" {
			log.Fatalf("strategy-router server error: %v", err)
		}
	}()

	if cfg.AutoStart {
		go coord.Run(ctx)
	} else {
		log.Printf("ROUTER_AUTOSTART=false: trigger via POST /api/v1/router/run")
	}

	waitForSignal()
	log.Printf("strategy-router shutting down")
	cancel()

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelShutdown()
	if err := srv.Stop(shutdownCtx); err != nil {
		log.Printf("shutdown error: %v", err)
	}
}

func waitForSignal() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
}
