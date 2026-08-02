package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"bitso-trading-platform/data-collector/internal/clock"
	"bitso-trading-platform/data-collector/internal/collector"
	"bitso-trading-platform/data-collector/internal/config"
	"bitso-trading-platform/data-collector/internal/health"
	"bitso-trading-platform/data-collector/internal/metrics"
	"bitso-trading-platform/data-collector/internal/sink"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	logger := log.New(os.Stdout, "[data-collector] ", log.LstdFlags|log.Lshortfile)

	cfg, err := config.LoadConfig()
	if err != nil {
		logger.Fatalf("config: %v", err)
	}

	clk := clock.RealClock{}
	met := metrics.New()
	hc := health.NewChecker(clk, cfg.HealthStaleAfter)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	var objectSink sink.ObjectSink = sink.NewMemObjectSink()
	if cfg.EnableS3 {
		s3Sink, err := sink.NewS3ObjectSink(ctx, cfg.S3Region, cfg.S3Bucket)
		if err != nil {
			logger.Fatalf("s3 sink: %v", err)
		}
		objectSink = s3Sink
		logger.Printf("S3 archive enabled bucket=%s prefix=%s", cfg.S3Bucket, cfg.S3Prefix)
	} else {
		logger.Println("S3 disabled; using in-memory object sink (dev only)")
	}

	batcher := sink.NewParquetBatcher(
		objectSink,
		cfg.S3Prefix,
		cfg.FlushInterval,
		cfg.FlushMaxRows,
		clk,
		func(err error) {
			logger.Printf("S3 flush failure: %v", err)
			met.S3FlushFailures.Inc()
		},
	)

	var hotStore sink.TradeWriter = sink.NopTradeWriter{}
	if cfg.EnablePostgres {
		pg, err := sink.NewPostgresStore(ctx, cfg.PostgresDSN, cfg.HotRetentionDays)
		if err != nil {
			logger.Fatalf("postgres: %v", err)
		}
		hotStore = pg
		logger.Printf("Postgres hot store enabled retention_days=%d", cfg.HotRetentionDays)
	} else {
		logger.Println("Postgres disabled")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", hc.Handler())
	mux.Handle("/metrics", promhttp.Handler())

	httpServer := &http.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		logger.Printf("HTTP listening on :%s (/healthz, /metrics)", cfg.HTTPPort)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Printf("HTTP server error: %v", err)
			cancel()
		}
	}()

	svc := collector.New(cfg, logger, clk, met, hc, batcher, hotStore)
	err = svc.Start(ctx)
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()
	_ = httpServer.Shutdown(shutdownCtx)
	if err != nil && ctx.Err() == nil {
		logger.Fatalf("collector stopped with error: %v", err)
	}
	logger.Println("shutdown complete")
}
