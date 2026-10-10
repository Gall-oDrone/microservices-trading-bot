package etoro

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

// Portfolio is the account snapshot from GET /api/v1/trading/info/{demo|real}/pnl.
type Portfolio struct {
	Credit            float64           `json:"credit"` // available cash, account currency
	BonusCredit       float64           `json:"bonusCredit"`
	UnrealizedPnL     float64           `json:"unrealizedPnL"`
	AccountCurrencyID int               `json:"accountCurrencyId"` // 1 = USD
	Positions         []Position        `json:"positions"`
	OrdersForOpen     []OpenOrderState  `json:"ordersForOpen"`
	OrdersForClose    []CloseOrderState `json:"ordersForClose"`
}

type pnlResponse struct {
	ClientPortfolio Portfolio `json:"clientPortfolio"`
}

// Position is an open position. JSON names follow the schema; Go's decoder
// matches them case-insensitively, which also covers the examples' camelCase.
type Position struct {
	PositionID             int64   `json:"positionID"`
	InstrumentID           int64   `json:"instrumentID"`
	OrderID                int64   `json:"orderID"`
	MirrorID               int64   `json:"mirrorID"`
	IsBuy                  bool    `json:"isBuy"`
	Leverage               int     `json:"leverage"`
	Amount                 float64 `json:"amount"`
	Units                  float64 `json:"units"`
	InitialUnits           float64 `json:"initialUnits"`
	OpenRate               float64 `json:"openRate"`
	OpenConversionRate     float64 `json:"openConversionRate"`
	OpenDateTime           string  `json:"openDateTime"`
	StopLossRate           float64 `json:"stopLossRate"`
	TakeProfitRate         float64 `json:"takeProfitRate"`
	IsNoStopLoss           bool    `json:"isNoStopLoss"`
	IsNoTakeProfit         bool    `json:"isNoTakeProfit"`
	TotalFees              float64 `json:"totalFees"`
	InitialAmountInDollars float64 `json:"initialAmountInDollars"`
	SettlementTypeID       int     `json:"settlementTypeID"`
	// UnrealizedPnL is an object on the live API (verified on demo
	// 2026-10-10), not a number as the portfolio-level field is.
	UnrealizedPnL PositionPnL `json:"unrealizedPnL"`
}

// PositionPnL is a position's live mark-to-market block.
type PositionPnL struct {
	PnL                       float64 `json:"pnL"`
	ExposureInAccountCurrency float64 `json:"exposureInAccountCurrency"`
	MarginInAccountCurrency   float64 `json:"marginInAccountCurrency"`
	CloseRate                 float64 `json:"closeRate"`
	CloseConversionRate       float64 `json:"closeConversionRate"`
	Timestamp                 string  `json:"timestamp"`
}

// OpenedAt parses OpenDateTime (UTC).
func (p Position) OpenedAt() time.Time { return parseAPITime(p.OpenDateTime) }

// OpenOrderState is a pending open order in the portfolio snapshot.
type OpenOrderState struct {
	OrderID      int64   `json:"orderId"`
	StatusID     int     `json:"statusId"`
	InstrumentID int64   `json:"instrumentId"`
	Amount       float64 `json:"amount"`
	IsBuy        bool    `json:"isBuy"`
	Leverage     int     `json:"leverage"`
}

// CloseOrderState is a pending close order in the portfolio snapshot.
type CloseOrderState struct {
	OrderID       int64   `json:"orderId"`
	StatusID      int     `json:"statusId"`
	InstrumentID  int64   `json:"instrumentId"`
	PositionID    int64   `json:"positionId"`
	UnitsToDeduct float64 `json:"unitsToDeduct"`
}

// GetPortfolio returns the account snapshot (cash, open positions, pending orders).
func (c *Client) GetPortfolio(ctx context.Context) (*Portfolio, error) {
	var resp pnlResponse
	if err := c.do(ctx, call{method: "GET", path: c.pnlPath()}, &resp); err != nil {
		return nil, err
	}
	return &resp.ClientPortfolio, nil
}

// PositionsFor returns the open, directly held (non-copy) positions on an instrument.
func (p *Portfolio) PositionsFor(instrumentID int64) []Position {
	if p == nil {
		return nil
	}
	var out []Position
	for _, pos := range p.Positions {
		if pos.InstrumentID == instrumentID && pos.MirrorID == 0 {
			out = append(out, pos)
		}
	}
	return out
}

// PositionByID returns the open position with that id, or nil.
func (p *Portfolio) PositionByID(positionID int64) *Position {
	if p == nil {
		return nil
	}
	for i := range p.Positions {
		if p.Positions[i].PositionID == positionID {
			return &p.Positions[i]
		}
	}
	return nil
}

// Equity is credit plus the invested amount plus unrealized P&L, the
// formula of the portal guide "Calculate Equity" for a plain account.
func (p *Portfolio) Equity() float64 {
	if p == nil {
		return 0
	}
	invested := 0.0
	for _, pos := range p.Positions {
		invested += pos.Amount
	}
	return p.Credit + invested + p.UnrealizedPnL
}

// ClosedTrade is one row of the trading history.
type ClosedTrade struct {
	PositionID        int64   `json:"positionId"`
	InstrumentID      int64   `json:"instrumentId"`
	OrderID           int64   `json:"orderId"`
	IsBuy             bool    `json:"isBuy"`
	Leverage          int     `json:"leverage"`
	OpenRate          float64 `json:"openRate"`
	CloseRate         float64 `json:"closeRate"`
	OpenTimestamp     string  `json:"openTimestamp"`
	CloseTimestamp    string  `json:"closeTimestamp"`
	Units             float64 `json:"units"`
	Investment        float64 `json:"investment"`
	InitialInvestment float64 `json:"initialInvestment"`
	// NetProfit is realized P&L EXCLUDING Fees (fully loaded = NetProfit - Fees).
	NetProfit        float64 `json:"netProfit"`
	Fees             float64 `json:"fees"`
	StopLossRate     float64 `json:"stopLossRate"`
	TakeProfitRate   float64 `json:"takeProfitRate"`
	ParentPositionID int64   `json:"parentPositionId"`
}

// TradingHistory lists closed trades since minDate (a window under one
// year), one page of up to pageSize (≤ 200) rows.
func (c *Client) TradingHistory(ctx context.Context, minDate time.Time, page, pageSize int) ([]ClosedTrade, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 200
	}
	if minDate.IsZero() || time.Since(minDate) >= 365*24*time.Hour {
		return nil, fmt.Errorf("minDate must be set and less than one year ago")
	}
	q := url.Values{
		"minDate":  {minDate.UTC().Format("2006-01-02")},
		"page":     {strconv.Itoa(page)},
		"pageSize": {strconv.Itoa(pageSize)},
	}
	var rows []ClosedTrade
	err := c.do(ctx, call{method: "GET", path: c.historyPath(), query: q}, &rows)
	return rows, err
}
