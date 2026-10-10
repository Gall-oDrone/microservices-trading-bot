package news

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"bitso-trading-platform/shared/pkg/kafka"
	"bitso-trading-platform/shared/pkg/models"
)

// RunConsumer reads news.agentic events into the sentiment store until ctx is cancelled.
func RunConsumer(ctx context.Context, consumer *kafka.Consumer, store *Store, logger func(string, ...interface{})) {
	if consumer == nil || store == nil {
		return
	}
	if logger == nil {
		logger = func(format string, args ...interface{}) {
			log.Printf(format, args...)
		}
	}
	logger("news.agentic consumer started")
	for {
		msg, err := consumer.Consume(ctx)
		if err != nil {
			if ctx.Err() != nil {
				logger("news.agentic consumer stopping")
				return
			}
			logger("news consume: %v", err)
			time.Sleep(time.Second)
			continue
		}
		var evt models.NewsAgenticEvent
		if err := json.Unmarshal(msg, &evt); err != nil {
			logger("news unmarshal: %v", err)
			continue
		}
		store.Upsert(evt)
	}
}
