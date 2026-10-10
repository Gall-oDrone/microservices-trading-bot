package etoro

import (
	"context"
	"fmt"
	"strconv"
)

// OpenByAmountRequest opens a market position using cash amount (USD on most instruments).
type OpenByAmountRequest struct {
	InstrumentID   int64   `json:"InstrumentID"`
	Amount         float64 `json:"Amount"`
	Leverage       int     `json:"Leverage"`
	IsBuy          bool    `json:"IsBuy"`
	StopLossRate   float64 `json:"StopLossRate"`
	TakeProfitRate float64 `json:"TakeProfitRate"`
}

// OpenOrderResponse is returned when a market open order is accepted.
type OpenOrderResponse struct {
	OrderID int64 `json:"orderId"`
}

// ClosePositionRequest closes all or part of a position.
type ClosePositionRequest struct {
	UnitsToDeduct *float64 `json:"UnitsToDeduct"`
}

// CloseOrderResponse is returned when a close order is accepted.
type CloseOrderResponse struct {
	OrderID int64 `json:"orderId"`
}

// DefaultSLTP returns placeholder stop-loss / take-profit rates required by the API.
func DefaultSLTP(referenceRate float64, isBuy bool) (stopLoss, takeProfit float64) {
	if referenceRate <= 0 {
		referenceRate = 1
	}
	stopLoss = 0.0001
	if isBuy {
		takeProfit = referenceRate * 100
	} else {
		takeProfit = referenceRate * 0.01
		if takeProfit < 0.0001 {
			takeProfit = 0.0001
		}
	}
	return stopLoss, takeProfit
}

// OpenMarketOrderByAmount places a market order using investment amount in account currency.
func (c *Client) OpenMarketOrderByAmount(ctx context.Context, req OpenByAmountRequest) (int64, error) {
	if req.Leverage == 0 {
		req.Leverage = 1
	}
	if req.StopLossRate <= 0 || req.TakeProfitRate <= 0 {
		sl, tp := DefaultSLTP(1, req.IsBuy)
		if req.StopLossRate <= 0 {
			req.StopLossRate = sl
		}
		if req.TakeProfitRate <= 0 {
			req.TakeProfitRate = tp
		}
	}
	path := fmt.Sprintf("/trading/execution/%s/market-open-orders/by-amount", c.env)
	var resp OpenOrderResponse
	if err := c.doJSON(ctx, "POST", path, req, &resp); err != nil {
		return 0, err
	}
	return resp.OrderID, nil
}

// ClosePosition closes an open position by positionID (full close when UnitsToDeduct is nil).
func (c *Client) ClosePosition(ctx context.Context, positionID int64, unitsToDeduct *float64) (int64, error) {
	path := fmt.Sprintf("/trading/execution/%s/market-close-orders/positions/%d", c.env, positionID)
	body := ClosePositionRequest{UnitsToDeduct: unitsToDeduct}
	var resp CloseOrderResponse
	if err := c.doJSON(ctx, "POST", path, body, &resp); err != nil {
		return 0, err
	}
	return resp.OrderID, nil
}

// FormatOrderID stringifies an eToro order id for Kafka / logging.
func FormatOrderID(id int64) string {
	return strconv.FormatInt(id, 10)
}
