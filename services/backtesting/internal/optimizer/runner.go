package optimizer

import (
	"context"
	"fmt"
	"sync"

	"bitso-trading-platform/backtesting/internal/engine"
	"bitso-trading-platform/backtesting/internal/logger"
	"bitso-trading-platform/backtesting/internal/models"
)

// ParallelRunner runs backtests in parallel
type ParallelRunner struct {
	engine     engine.BacktestEngine
	maxWorkers int
	logger     logger.Logger
}

// NewParallelRunner creates a new parallel runner
func NewParallelRunner(
	engine engine.BacktestEngine,
	maxWorkers int,
	log logger.Logger,
) *ParallelRunner {
	if maxWorkers <= 0 {
		maxWorkers = 4
	}

	return &ParallelRunner{
		engine:     engine,
		maxWorkers: maxWorkers,
		logger:     log,
	}
}

// RunAll runs backtests for all parameter combinations in parallel
func (r *ParallelRunner) RunAll(
	ctx context.Context,
	opt *Optimization,
	combinations []map[string]interface{},
) ([]*OptimizationResult, error) {
	if len(combinations) == 0 {
		return []*OptimizationResult{}, nil
	}

	// Create channels
	jobs := make(chan *runJob, len(combinations))
	results := make(chan *OptimizationResult, len(combinations))
	errors := make(chan error, len(combinations))

	// Create jobs
	for _, params := range combinations {
		jobs <- &runJob{
			parameters: params,
			config:     opt.Config.BaseConfig.Clone(),
		}
	}
	close(jobs)

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < r.maxWorkers; i++ {
		wg.Add(1)
		go r.worker(ctx, opt, jobs, results, errors, &wg)
	}

	// Wait for completion in separate goroutine
	go func() {
		wg.Wait()
		close(results)
		close(errors)
	}()

	// Collect results
	optResults := make([]*OptimizationResult, 0, len(combinations))
	var lastErr error

	for {
		select {
		case result, ok := <-results:
			if !ok {
				// Channel closed, all done
				if lastErr != nil {
					return nil, lastErr
				}
				return optResults, nil
			}
			optResults = append(optResults, result)

		case err, ok := <-errors:
			if ok && err != nil {
				lastErr = err
				r.logger.Error("Backtest failed", map[string]interface{}{
					"error": err,
				})
			}

		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// runJob represents a single backtest job
type runJob struct {
	parameters map[string]interface{}
	config     *models.BacktestConfig
}

// worker processes backtest jobs
func (r *ParallelRunner) worker(
	ctx context.Context,
	opt *Optimization,
	jobs <-chan *runJob,
	results chan<- *OptimizationResult,
	errors chan<- error,
	wg *sync.WaitGroup,
) {
	defer wg.Done()

	for job := range jobs {
		// Check if optimization was cancelled
		opt.mu.RLock()
		cancelled := opt.Status == "cancelled"
		opt.mu.RUnlock()

		if cancelled {
			return
		}

		// Apply parameters to config
		if err := applyParameters(job.config, job.parameters); err != nil {
			errors <- fmt.Errorf("failed to apply parameters: %w", err)
			continue
		}

		// Run backtest
		result, err := r.engine.Run(ctx, job.config)
		if err != nil {
			errors <- fmt.Errorf("backtest failed: %w", err)

			// Update progress even on error
			opt.mu.Lock()
			opt.CompletedRuns++
			opt.Progress = float64(opt.CompletedRuns) / float64(opt.TotalRuns)
			opt.mu.Unlock()

			continue
		}

		// Create optimization result
		optResult := &OptimizationResult{
			Parameters: job.parameters,
			Result:     result,
			Score:      0.0, // Will be set by evaluator
		}

		results <- optResult

		// Update progress
		opt.mu.Lock()
		opt.CompletedRuns++
		opt.Progress = float64(opt.CompletedRuns) / float64(opt.TotalRuns)
		opt.mu.Unlock()

		r.logger.Debug("Backtest completed", map[string]interface{}{
			"optimization_id": opt.ID,
			"progress":        opt.Progress,
			"parameters":      job.parameters,
		})
	}
}

// applyParameters applies parameters to the backtest config
func applyParameters(config *models.BacktestConfig, params map[string]interface{}) error {
	// Apply parameters to strategy params
	for key, value := range params {
		config.StrategyParams[key] = value
	}

	return nil
}
