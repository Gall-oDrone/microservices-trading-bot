package etoro

import "fmt"

// Route table. Demo and real differ, and not uniformly: v2 routes insert
// "/demo" for demo, the v1 pnl route uses "/demo" vs "/real", the v1 history
// route uses ".../trade/demo/history" vs ".../trade/history". Every path
// below was read from the portal's per-route OpenAPI (trading--demo and
// trading--real) on 2026-10-10; demo paths were also called successfully.

func (c *Client) isDemo() bool { return c.env != EnvReal }

func (c *Client) pick(demo, real string) string {
	if c.isDemo() {
		return demo
	}
	return real
}

func (c *Client) ordersPath() string {
	return c.pick("/api/v2/trading/execution/demo/orders", "/api/v2/trading/execution/orders")
}

func (c *Client) orderLookupPath() string {
	return c.pick("/api/v2/trading/info/demo/orders:lookup", "/api/v2/trading/info/orders:lookup")
}

func (c *Client) costsPath() string {
	return c.pick("/api/v2/trading/info/demo/costs", "/api/v2/trading/info/costs")
}

func (c *Client) eligibilityPath() string {
	return c.pick("/api/v2/trading/info/demo/eligibility", "/api/v2/trading/info/eligibility")
}

// closePositionPath: v2 orders do not support action=close yet ("reserved
// ... currently rejected"), so closes use the v1 market-close route.
func (c *Client) closePositionPath(positionID int64) string {
	return fmt.Sprintf(c.pick("/api/v1/trading/execution/demo/market-close-orders/positions/%d",
		"/api/v1/trading/execution/market-close-orders/positions/%d"), positionID)
}

func (c *Client) closeOrderInfoPath(orderID int64) string {
	return fmt.Sprintf(c.pick("/api/v1/trading/info/demo/close-orders/%d",
		"/api/v1/trading/info/real/close-orders/%d"), orderID)
}

func (c *Client) pnlPath() string {
	return c.pick("/api/v1/trading/info/demo/pnl", "/api/v1/trading/info/real/pnl")
}

func (c *Client) historyPath() string {
	return c.pick("/api/v1/trading/info/trade/demo/history", "/api/v1/trading/info/trade/history")
}

// Market-data routes are environment independent.
const (
	pathSearch      = "/api/v1/market-data/search"
	pathRates       = "/api/v2/market-data/rates"
	pathCandlesFmt  = "/api/v1/market-data/instruments/%d/history/candles/%s/%s/%d"
	pathHistoryFmt  = "/api/v1/data/instruments/%d/candles"
	pathClosingFull = "/api/v1/market-data/instruments/history/closing-price"
)
