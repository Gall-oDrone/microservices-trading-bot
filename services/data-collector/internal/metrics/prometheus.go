package metrics

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Collector holds Prometheus metrics for the data-collector service.
type Collector struct {
	TradesReceived     prometheus.Counter
	WSReconnects       prometheus.Counter
	S3FlushFailures    prometheus.Counter
	PostgresFailures   prometheus.Counter
	TimeSinceLastTrade prometheus.Gauge

	mu           sync.RWMutex
	lastTradeAt  time.Time
	hasTrade     bool
}

// New creates and registers Prometheus metrics.
func New() *Collector {
	c := &Collector{
		TradesReceived: promauto.NewCounter(prometheus.CounterOpts{
			Name: "data_collector_trades_received_total",
			Help: "Total number of trades received from Bitso WebSocket",
		}),
		WSReconnects: promauto.NewCounter(prometheus.CounterOpts{
			Name: "data_collector_ws_reconnects_total",
			Help: "Total number of WebSocket reconnects",
		}),
		S3FlushFailures: promauto.NewCounter(prometheus.CounterOpts{
			Name: "data_collector_s3_flush_failures_total",
			Help: "Total number of S3/Parquet flush failures",
		}),
		PostgresFailures: promauto.NewCounter(prometheus.CounterOpts{
			Name: "data_collector_postgres_write_failures_total",
			Help: "Total number of Postgres write failures",
		}),
		TimeSinceLastTrade: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "data_collector_seconds_since_last_trade",
			Help: "Seconds since the last trade was received",
		}),
	}
	return c
}

// ObserveTrade updates trade counters and last-trade timestamp.
func (c *Collector) ObserveTrade(at time.Time) {
	c.TradesReceived.Inc()
	c.mu.Lock()
	c.lastTradeAt = at.UTC()
	c.hasTrade = true
	c.mu.Unlock()
	c.TimeSinceLastTrade.Set(0)
}

// LastTradeAt returns the last trade timestamp and whether any trade was seen.
func (c *Collector) LastTradeAt() (time.Time, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastTradeAt, c.hasTrade
}

// UpdateStaleness refreshes the time-since-last-trade gauge.
func (c *Collector) UpdateStaleness(now time.Time) {
	c.mu.RLock()
	last := c.lastTradeAt
	has := c.hasTrade
	c.mu.RUnlock()
	if !has {
		c.TimeSinceLastTrade.Set(-1)
		return
	}
	c.TimeSinceLastTrade.Set(now.UTC().Sub(last).Seconds())
}
