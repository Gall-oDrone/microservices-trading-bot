package ingest

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"time"

	"bitso-trading-platform/news-publisher/internal/config"
	"bitso-trading-platform/news-publisher/internal/s3news"
	"bitso-trading-platform/news-publisher/internal/state"
	"bitso-trading-platform/shared/pkg/kafka"
	"bitso-trading-platform/shared/pkg/models"
)

type Runner struct {
	cfg      config.Config
	reader   *s3news.Reader
	producer *kafka.Producer
	store    *state.Store
}

func NewRunner(cfg config.Config, reader *s3news.Reader, producer *kafka.Producer, store *state.Store) *Runner {
	return &Runner{cfg: cfg, reader: reader, producer: producer, store: store}
}

func (r *Runner) RunOnce(ctx context.Context) error {
	key, err := r.reader.LatestETLKey(ctx)
	if err != nil {
		return err
	}
	r.store.SetLastObjectKey(key)
	log.Printf("[news-publisher] ingesting s3://%s/%s", r.cfg.S3Bucket, key)

	var published int
	err = r.reader.StreamJSONLEvents(ctx, key, r.cfg.MaxArticlesPerRun, func(evt models.NewsAgenticEvent) error {
		r.store.Upsert(evt)
		if r.producer == nil {
			published++
			return nil
		}
		payload, err := json.Marshal(evt)
		if err != nil {
			return err
		}
		if err := r.producer.Produce(ctx, []byte(evt.Symbol), payload); err != nil {
			return err
		}
		published++
		return nil
	})
	if err != nil {
		return err
	}
	log.Printf("[news-publisher] published %d events to topic %s", published, r.cfg.KafkaTopic)
	return nil
}

func (r *Runner) Loop(ctx context.Context) {
	ticker := time.NewTicker(r.cfg.PublishInterval)
	defer ticker.Stop()

	if err := r.RunOnce(ctx); err != nil {
		log.Printf("[news-publisher] initial ingest error: %v", err)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := r.RunOnce(ctx); err != nil {
				log.Printf("[news-publisher] ingest error: %v", err)
			}
		}
	}
}

// SentimentForSymbol returns the latest cached event for a symbol (BTC, AAPL, etc.).
func SentimentForSymbol(store *state.Store, symbol string) (models.NewsAgenticEvent, bool) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if evt, ok := store.Get(symbol); ok {
		return evt, true
	}
	return store.Get("GENERAL")
}
