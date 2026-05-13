package models

import (
	"encoding/json"
	"time"
)

// PaperTradingSnapshot is written to S3 as JSON (paper / stage organic runs).
type PaperTradingSnapshot struct {
	SchemaVersion       int                      `json:"schema_version"`
	Event               string                   `json:"event"`
	CollectedAt         time.Time                `json:"collected_at"`
	Environment         string                   `json:"environment"`
	StrategyExecutorURL string                   `json:"strategy_executor_url"`
	Strategies          []map[string]interface{} `json:"strategies"`
	StrategyCount       int                      `json:"strategy_count"`
	// LimitProfitPnLEstimates: per open limit_profit position, modeled exit-at-threshold P&L
	// (aligned with Bitso fee math when credentials or override decimals are provided).
	LimitProfitPnLEstimates []LimitProfitPnLEstimate   `json:"limit_profit_pnl_estimates,omitempty"`
	Indicators              map[string]json.RawMessage `json:"indicators,omitempty"`
	CollectionErrors        map[string]string          `json:"collection_errors,omitempty"`
}

const SchemaVersion = 1

func NewSnapshot(env, seURL string) *PaperTradingSnapshot {
	return &PaperTradingSnapshot{
		SchemaVersion:       SchemaVersion,
		Event:               "paper_trading_snapshot",
		CollectedAt:         time.Now().UTC(),
		Environment:         env,
		StrategyExecutorURL: seURL,
		Strategies:          nil,
		Indicators:          make(map[string]json.RawMessage),
		CollectionErrors:    make(map[string]string),
	}
}
