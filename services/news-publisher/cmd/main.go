package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"bitso-trading-platform/news-publisher/internal/config"
	"bitso-trading-platform/news-publisher/internal/ingest"
	"bitso-trading-platform/news-publisher/internal/s3news"
	"bitso-trading-platform/news-publisher/internal/server"
	"bitso-trading-platform/news-publisher/internal/state"
	"bitso-trading-platform/shared/pkg/kafka"
)

func main() {
	cfg := config.Load()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reader, err := s3news.NewReader(ctx, cfg.S3Bucket, cfg.S3Prefix, cfg.S3Format)
	if err != nil {
		log.Fatalf("s3 reader: %v", err)
	}

	var producer *kafka.Producer
	if cfg.KafkaBrokers != "" {
		brokers := strings.Split(cfg.KafkaBrokers, ",")
		for i := range brokers {
			brokers[i] = strings.TrimSpace(brokers[i])
		}
		producer, err = kafka.NewProducer(&kafka.ProducerConfig{
			Brokers: brokers,
			Topic:   cfg.KafkaTopic,
		})
		if err != nil {
			log.Fatalf("kafka producer: %v", err)
		}
		defer producer.Close()
	}

	store := state.New()
	runner := ingest.NewRunner(cfg, reader, producer, store)
	go runner.Loop(ctx)

	addr := net.JoinHostPort(cfg.Host, itoa(cfg.Port))
	srv := &http.Server{Addr: addr, Handler: server.New(store).Handler()}
	go func() {
		log.Printf("news-publisher listening on %s (bucket=%s)", addr, cfg.S3Bucket)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http: %v", err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()
	_ = srv.Shutdown(shutdownCtx)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
