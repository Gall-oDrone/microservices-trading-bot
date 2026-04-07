// Package indicators provides technical indicator calculations for trading strategies.
package indicators

import (
	"context"
	"time"
)

// OHLCV represents a candlestick bar with Open, High, Low, Close, Volume
type OHLCV struct {
	Timestamp time.Time
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    float64
}

// Trade represents a single trade event
type Trade struct {
	Timestamp time.Time
	Price     float64
	Amount    float64
	Side      string
}

// IndicatorValue represents a computed indicator value with metadata
type IndicatorValue struct {
	Name      string
	Period    int
	Value     float64
	Timestamp time.Time
	Book      string
	Extra     map[string]float64
}

// BollingerBands represents Bollinger Bands indicator values
type BollingerBands struct {
	Upper  float64
	Middle float64
	Lower  float64
	StdDev float64
}

// Indicator defines the interface for all technical indicators
type Indicator interface {
	Name() string
	Period() int
	Compute(prices []float64) (float64, error)
	ComputeFromTrades(trades []Trade) (float64, error)
	ComputeFromBars(bars []OHLCV) (float64, error)
}

// IndicatorStore defines the interface for persisting indicator values
type IndicatorStore interface {
	Set(ctx context.Context, book, indicator string, period int, value *IndicatorValue) error
	Get(ctx context.Context, book, indicator string, period int) (*IndicatorValue, error)
	GetAll(ctx context.Context, book string) (map[string]*IndicatorValue, error)
	SetBollinger(ctx context.Context, book string, period int, bb *BollingerBands) error
	GetBollinger(ctx context.Context, book string, period int) (*BollingerBands, error)
}

// DataProvider provides market data for indicator computation
type DataProvider interface {
	GetRecentTrades(ctx context.Context, book string, limit int) ([]Trade, error)
	GetRecentBars(ctx context.Context, book string, interval string, limit int) ([]OHLCV, error)
}
