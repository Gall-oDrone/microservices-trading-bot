package optimizer

import (
	"context"
	"fmt"
	"sync"
	"time"

	"bitso-trading-platform/backtesting/internal/engine"
	"bitso-trading-platform/backtesting/internal/logger"
	"bitso-trading-platform/backtesting/internal/models"
	"bitso-trading-platform/backtesting/internal/storage"
	
	"github.com/google/uuid"
)

// OptimizationConfig defines the configuration for parameter optimization
type OptimizationConfig struct {
	Name        string                            `json:"name"`
	Description string                            `json:"description"`
	BaseConfig  *models.BacktestConfig            `json:"base_config"`
	Parameters  map[string]*ParameterRange        `json:"parameters"`
	Metric      string                            `json:"metric"` // Which metric to optimize (sharpe_ratio, total_return, etc.)
	MaxWorkers  int                               `json:"max_workers"`
	TopN        int                               `json:"top_n"` // Number of top results to keep
}

// ParameterRange defines a range of values for a parameter
type ParameterRange struct {
	Name   string        `json:"name"`
	Min    float64       `json:"min"`
	Max    float64       `json:"max"`
	Step   float64       `json:"step"`
	Values []interface{} `json:"values,omitempty"` // For discrete values
}

// Optimization represents an optimization run
type Optimization struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Status      string                 `json:"status"` // pending, running, completed, failed, cancelled
	Config      *OptimizationConfig    `json:"config"`
	Progress    float64                `json:"progress"`
	TotalRuns   int                    `json:"total_runs"`
	CompletedRuns int                  `json:"completed_runs"`
	Results     []*OptimizationResult  `json:"results"`
	BestResult  *OptimizationResult    `json:"best_result,omitempty"`
	CreatedAt   time.Time              `json:"created_at"`
	StartedAt   *time.Time             `json:"started_at,omitempty"`
	CompletedAt *time.Time             `json:"completed_at,omitempty"`
	Error       string                 `json:"error,omitempty"`
	mu          sync.RWMutex           `json:"-"`
}

// OptimizationResult represents the result of a single parameter combination
type OptimizationResult struct {
	Parameters map[string]interface{}   `json:"parameters"`
	Result     *models.BacktestResult   `json:"result"`
	Score      float64                  `json:"score"`
	Rank       int                      `json:"rank"`
}

// Optimizer handles parameter optimization
type Optimizer interface {
	// Optimize runs parameter optimization
	Optimize(ctx context.Context, config *OptimizationConfig) (*Optimization, error)
	
	// GetOptimization retrieves an optimization by ID
	GetOptimization(id string) (*Optimization, error)
	
	// CancelOptimization cancels a running optimization
	CancelOptimization(id string) error
}

// optimizer implements the Optimizer interface
type optimizer struct {
	engine           engine.BacktestEngine
	storage          storage.ResultStorage
	logger           logger.Logger
	runningOpts      map[string]*Optimization
	mu               sync.RWMutex
}

// NewOptimizer creates a new optimizer
func NewOptimizer(
	engine engine.BacktestEngine,
	storage storage.ResultStorage,
	log logger.Logger,
) Optimizer {
	return &optimizer{
		engine:      engine,
		storage:     storage,
		logger:      log,
		runningOpts: make(map[string]*Optimization),
	}
}

// Optimize runs parameter optimization
func (o *optimizer) Optimize(ctx context.Context, config *OptimizationConfig) (*Optimization, error) {
	// Validate config
	if err := validateOptimizationConfig(config); err != nil {
		return nil, fmt.Errorf("invalid optimization config: %w", err)
	}
	
	// Create optimization
	opt := &Optimization{
		ID:          uuid.New().String(),
		Name:        config.Name,
		Description: config.Description,
		Status:      "pending",
		Config:      config,
		Progress:    0.0,
		Results:     make([]*OptimizationResult, 0),
		CreatedAt:   time.Now(),
	}
	
	// Register optimization
	o.mu.Lock()
	o.runningOpts[opt.ID] = opt
	o.mu.Unlock()
	
	// Start optimization in background
	go o.runOptimization(ctx, opt)
	
	return opt, nil
}

// GetOptimization retrieves an optimization by ID
func (o *optimizer) GetOptimization(id string) (*Optimization, error) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	
	opt, exists := o.runningOpts[id]
	if !exists {
		return nil, fmt.Errorf("optimization not found: %s", id)
	}
	
	return opt, nil
}

// CancelOptimization cancels a running optimization
func (o *optimizer) CancelOptimization(id string) error {
	o.mu.RLock()
	opt, exists := o.runningOpts[id]
	o.mu.RUnlock()
	
	if !exists {
		return fmt.Errorf("optimization not found: %s", id)
	}
	
	opt.mu.Lock()
	defer opt.mu.Unlock()
	
	if opt.Status != "running" {
		return fmt.Errorf("optimization is not running: %s", opt.Status)
	}
	
	opt.Status = "cancelled"
	now := time.Now()
	opt.CompletedAt = &now
	
	o.logger.Info("Optimization cancelled", map[string]interface{}{
		"optimization_id": id,
	})
	
	return nil
}

// runOptimization executes the optimization
func (o *optimizer) runOptimization(ctx context.Context, opt *Optimization) {
	// Update status
	opt.mu.Lock()
	opt.Status = "running"
	now := time.Now()
	opt.StartedAt = &now
	opt.mu.Unlock()
	
	o.logger.Info("Starting optimization", map[string]interface{}{
		"optimization_id": opt.ID,
		"name":           opt.Name,
	})
	
	// Generate parameter combinations
	grid := NewParameterGrid(opt.Config.Parameters)
	combinations := grid.Generate()
	
	opt.mu.Lock()
	opt.TotalRuns = len(combinations)
	opt.mu.Unlock()
	
	o.logger.Info("Generated parameter combinations", map[string]interface{}{
		"optimization_id": opt.ID,
		"total_runs":     len(combinations),
	})
	
	// Run backtests for each combination
	runner := NewParallelRunner(o.engine, opt.Config.MaxWorkers, o.logger)
	results, err := runner.RunAll(ctx, opt, combinations)
	
	if err != nil {
		opt.mu.Lock()
		opt.Status = "failed"
		opt.Error = err.Error()
		now := time.Now()
		opt.CompletedAt = &now
		opt.mu.Unlock()
		
		o.logger.Error("Optimization failed", map[string]interface{}{
			"optimization_id": opt.ID,
			"error":          err,
		})
		return
	}
	
	// Check if cancelled
	opt.mu.RLock()
	cancelled := opt.Status == "cancelled"
	opt.mu.RUnlock()
	
	if cancelled {
		return
	}
	
	// Evaluate and rank results
	evaluator := NewEvaluator(opt.Config.Metric, o.logger)
	rankedResults := evaluator.EvaluateAndRank(results)
	
	// Keep only top N results
	topN := opt.Config.TopN
	if topN > 0 && len(rankedResults) > topN {
		rankedResults = rankedResults[:topN]
	}
	
	// Update optimization with results
	opt.mu.Lock()
	opt.Status = "completed"
	opt.Results = rankedResults
	if len(rankedResults) > 0 {
		opt.BestResult = rankedResults[0]
	}
	opt.Progress = 1.0
	now = time.Now()
	opt.CompletedAt = &now
	opt.mu.Unlock()
	
	o.logger.Info("Optimization completed", map[string]interface{}{
		"optimization_id": opt.ID,
		"total_runs":     len(results),
		"best_score":     opt.BestResult.Score,
	})
}

// validateOptimizationConfig validates the optimization configuration
func validateOptimizationConfig(config *OptimizationConfig) error {
	if config == nil {
		return fmt.Errorf("config is nil")
	}
	
	if config.Name == "" {
		return fmt.Errorf("name is required")
	}
	
	if config.BaseConfig == nil {
		return fmt.Errorf("base_config is required")
	}
	
	if len(config.Parameters) == 0 {
		return fmt.Errorf("at least one parameter is required")
	}
	
	if config.Metric == "" {
		config.Metric = "sharpe_ratio" // Default metric
	}
	
	if config.MaxWorkers <= 0 {
		config.MaxWorkers = 4 // Default workers
	}
	
	if config.TopN <= 0 {
		config.TopN = 10 // Default top N
	}
	
	// Validate parameter ranges
	for name, param := range config.Parameters {
		if param.Values == nil || len(param.Values) == 0 {
			// Numeric range
			if param.Max <= param.Min {
				return fmt.Errorf("parameter %s: max must be greater than min", name)
			}
			if param.Step <= 0 {
				return fmt.Errorf("parameter %s: step must be positive", name)
			}
		}
	}
	
	return nil
}

