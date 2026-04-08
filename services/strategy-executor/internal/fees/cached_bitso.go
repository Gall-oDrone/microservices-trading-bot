package fees

import (
	"context"
	"sync"
	"time"

	"bitso-trading-platform/shared/pkg/bitso"
	"bitso-trading-platform/strategy-executor/internal/strategies"
)

// CachedBitsoFees loads and caches GET /fees (authenticated). Stale entries are refreshed on access;
// if refresh fails, the last successful payload is still used when present.
type CachedBitsoFees struct {
	client *bitso.Client
	ttl    time.Duration

	mu      sync.Mutex
	payload *bitso.CustomerFees
	fetched time.Time
}

// NewCachedBitsoFees returns a fee provider. ttl caps how long the full /fees payload is cached (per process).
func NewCachedBitsoFees(client *bitso.Client, ttl time.Duration) *CachedBitsoFees {
	if ttl <= 0 {
		ttl = time.Hour
	}
	return &CachedBitsoFees{client: client, ttl: ttl}
}

func (c *CachedBitsoFees) refreshLocked() {
	if c.payload == nil || time.Since(c.fetched) > c.ttl {
		cf, err := c.client.Fees(nil)
		if err == nil {
			c.payload = cf
			c.fetched = time.Now()
		}
	}
}

// MakerTakerRatesForBook implements strategies.MakerTakerFeeProvider.
func (c *CachedBitsoFees) MakerTakerRatesForBook(ctx context.Context, book string) (maker, taker float64, ok bool) {
	_ = ctx
	if c == nil || c.client == nil {
		return 0, 0, false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.refreshLocked()
	if c.payload == nil {
		return 0, 0, false
	}

	f := bitso.LookupFeeByBook(c.payload, book)
	if f == nil {
		return 0, 0, false
	}
	m, t := f.MakerTakerDecimalRates()
	return m, t, true
}

// FeeDecimalsForLegs implements strategies.BookFeeResolver using maker/taker columns from GET /fees.
func (c *CachedBitsoFees) FeeDecimalsForLegs(ctx context.Context, book, buyLiquidity, sellLiquidity string) (buyRate, sellRate float64, ok bool) {
	_ = ctx
	if c == nil || c.client == nil {
		return 0, 0, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.refreshLocked()
	if c.payload == nil {
		return 0, 0, false
	}
	f := bitso.LookupFeeByBook(c.payload, book)
	if f == nil {
		return 0, 0, false
	}
	return bitso.FeeDecimalForLiquidity(f, buyLiquidity), bitso.FeeDecimalForLiquidity(f, sellLiquidity), true
}

var _ strategies.MakerTakerFeeProvider = (*CachedBitsoFees)(nil)
var _ strategies.BookFeeResolver = (*CachedBitsoFees)(nil)
