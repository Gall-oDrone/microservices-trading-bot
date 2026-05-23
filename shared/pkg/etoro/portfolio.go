package etoro

import (
	"context"
	"fmt"
)

// ClientPortfolio is the account snapshot from GET /trading/info/{env}/pnl.
type ClientPortfolio struct {
	Credit    float64    `json:"credit"`
	Positions []Position `json:"positions"`
}

type pnlResponse struct {
	ClientPortfolio ClientPortfolio `json:"clientPortfolio"`
}

// Position is an open position in the portfolio.
type Position struct {
	PositionID   int64   `json:"positionID"`
	InstrumentID int64   `json:"instrumentID"`
	IsBuy        bool    `json:"isBuy"`
	Amount       float64 `json:"amount"`
	Units        float64 `json:"units"`
	OpenRate     float64 `json:"openRate"`
}

// GetPortfolio returns the full account snapshot (balances and open positions).
func (c *Client) GetPortfolio(ctx context.Context) (*ClientPortfolio, error) {
	path := fmt.Sprintf("/trading/info/%s/pnl", c.env)
	var resp pnlResponse
	if err := c.doJSON(ctx, httpMethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return &resp.ClientPortfolio, nil
}

// FindPositionByInstrument returns the first open position for an instrument (buy side preferred for sells).
func (p *ClientPortfolio) FindPositionByInstrument(instrumentID int64, isBuy bool) *Position {
	if p == nil {
		return nil
	}
	for i := range p.Positions {
		pos := &p.Positions[i]
		if pos.InstrumentID == instrumentID && pos.IsBuy == isBuy {
			return pos
		}
	}
	for i := range p.Positions {
		pos := &p.Positions[i]
		if pos.InstrumentID == instrumentID {
			return pos
		}
	}
	return nil
}
