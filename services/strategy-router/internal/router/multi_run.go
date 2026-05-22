package router

import (
	"context"
	"time"
)

// RunAll runs the evaluation loop for every engine in the slice (used by MultiEngine).
func RunAll(ctx context.Context, engines []*Engine) {
	if len(engines) == 0 {
		return
	}
	interval := engines[0].cfg.EvaluationInterval
	for _, e := range engines {
		if _, err := e.RunOnce(ctx); err != nil {
			_ = err
		}
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			for _, e := range engines {
				if _, err := e.RunOnce(ctx); err != nil {
					_ = err
				}
			}
		}
	}
}
