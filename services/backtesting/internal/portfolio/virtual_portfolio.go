package portfolio

import (
	"fmt"
	"sync"

	"bitso-trading-platform/backtesting/internal/models"
)

// VirtualPortfolio represents a simulated trading portfolio
type VirtualPortfolio struct {
	ID               string
	InitialBalance   float64
	CurrentBalance   float64
	Positions        map[string]*models.Position
	TotalPL          float64
	TotalCommissions float64
	Trades           []models.Trade
	PeakBalance      float64
	DrawdownAmount   float64
	
	mu sync.RWMutex
}

// NewVirtualPortfolio creates a new virtual portfolio
func NewVirtualPortfolio(id string, initialBalance float64) *VirtualPortfolio {
	return &VirtualPortfolio{
		ID:               id,
		InitialBalance:   initialBalance,
		CurrentBalance:   initialBalance,
		Positions:        make(map[string]*models.Position),
		TotalPL:          0,
		TotalCommissions: 0,
		Trades:           make([]models.Trade, 0),
		PeakBalance:      initialBalance,
		DrawdownAmount:   0,
	}
}

// GetBalance returns the current balance (thread-safe)
func (p *VirtualPortfolio) GetBalance() float64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.CurrentBalance
}

// GetPosition returns the position for a book (thread-safe)
func (p *VirtualPortfolio) GetPosition(book string) (*models.Position, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	if pos, exists := p.Positions[book]; exists {
		return pos.Clone(), nil
	}
	
	// Return empty position if not found
	return models.NewPosition(book), nil
}

// GetAllPositions returns all positions (thread-safe)
func (p *VirtualPortfolio) GetAllPositions() []*models.Position {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	positions := make([]*models.Position, 0, len(p.Positions))
	for _, pos := range p.Positions {
		positions = append(positions, pos.Clone())
	}
	
	return positions
}

// ExecuteTrade executes a trade and updates the portfolio
func (p *VirtualPortfolio) ExecuteTrade(trade *models.Trade) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	// Validate trade
	if err := trade.Validate(); err != nil {
		return fmt.Errorf("invalid trade: %w", err)
	}
	
	// Get or create position
	pos, exists := p.Positions[trade.Book]
	if !exists {
		pos = models.NewPosition(trade.Book)
		p.Positions[trade.Book] = pos
	}
	
	// Execute based on side
	if trade.Side == "buy" {
		return p.executeBuyTrade(pos, trade)
	} else if trade.Side == "sell" {
		return p.executeSellTrade(pos, trade)
	}
	
	return fmt.Errorf("unknown trade side: %s", trade.Side)
}

// executeBuyTrade executes a buy trade
func (p *VirtualPortfolio) executeBuyTrade(pos *models.Position, trade *models.Trade) error {
	// Calculate cost
	cost := (trade.EntryPrice * trade.Amount) + trade.Commission
	
	// Check if we have enough balance
	if cost > p.CurrentBalance {
		return fmt.Errorf("insufficient balance: need %.2f, have %.2f", cost, p.CurrentBalance)
	}
	
	// Deduct from balance
	p.CurrentBalance -= cost
	
	// Add to position
	pos.AddSize(trade.Amount, trade.EntryPrice)
	
	// Record commission
	p.TotalCommissions += trade.Commission
	
	// Update peak balance and drawdown
	p.updatePeakAndDrawdown()
	
	// Add trade to history
	p.Trades = append(p.Trades, *trade)
	
	return nil
}

// executeSellTrade executes a sell trade
func (p *VirtualPortfolio) executeSellTrade(pos *models.Position, trade *models.Trade) error {
	// Check if we have enough position to sell
	if trade.Amount > pos.Size {
		return fmt.Errorf("insufficient position: need %.8f, have %.8f", trade.Amount, pos.Size)
	}
	
	// Calculate proceeds
	proceeds := (trade.ExitPrice * trade.Amount) - trade.Commission
	
	// Reduce position and get realized P&L
	realizedPL := pos.ReduceSize(trade.Amount, trade.ExitPrice)
	
	// Add proceeds to balance
	p.CurrentBalance += proceeds
	
	// Update total P&L (subtract commission from realized P&L)
	p.TotalPL += realizedPL - trade.Commission
	
	// Record commission
	p.TotalCommissions += trade.Commission
	
	// Update peak balance and drawdown
	p.updatePeakAndDrawdown()
	
	// Add trade to history
	p.Trades = append(p.Trades, *trade)
	
	return nil
}

// CalculateEquity calculates total equity (balance + unrealized P&L)
func (p *VirtualPortfolio) CalculateEquity(prices map[string]float64) float64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	equity := p.CurrentBalance
	
	// Add unrealized P&L from positions
	for book, pos := range p.Positions {
		if currentPrice, exists := prices[book]; exists {
			pos.UpdateCurrentPrice(currentPrice)
			equity += pos.UnrealizedPL
		}
	}
	
	return equity
}

// GetTrades returns all trades (thread-safe)
func (p *VirtualPortfolio) GetTrades() []models.Trade {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	trades := make([]models.Trade, len(p.Trades))
	copy(trades, p.Trades)
	return trades
}

// GetSummary returns a summary of the portfolio
func (p *VirtualPortfolio) GetSummary() *PortfolioSummary {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	return &PortfolioSummary{
		Balance:          p.CurrentBalance,
		InitialBalance:   p.InitialBalance,
		TotalPL:          p.TotalPL,
		TotalCommissions: p.TotalCommissions,
		PeakBalance:      p.PeakBalance,
		DrawdownAmount:   p.DrawdownAmount,
		PositionCount:    len(p.Positions),
		TradeCount:       len(p.Trades),
	}
}

// Clone creates a deep copy of the portfolio
func (p *VirtualPortfolio) Clone() *VirtualPortfolio {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	clone := &VirtualPortfolio{
		ID:               p.ID,
		InitialBalance:   p.InitialBalance,
		CurrentBalance:   p.CurrentBalance,
		TotalPL:          p.TotalPL,
		TotalCommissions: p.TotalCommissions,
		PeakBalance:      p.PeakBalance,
		DrawdownAmount:   p.DrawdownAmount,
		Positions:        make(map[string]*models.Position),
		Trades:           make([]models.Trade, len(p.Trades)),
	}
	
	// Clone positions
	for book, pos := range p.Positions {
		clone.Positions[book] = pos.Clone()
	}
	
	// Copy trades
	copy(clone.Trades, p.Trades)
	
	return clone
}

// Reset resets the portfolio to initial state
func (p *VirtualPortfolio) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	p.CurrentBalance = p.InitialBalance
	p.Positions = make(map[string]*models.Position)
	p.TotalPL = 0
	p.TotalCommissions = 0
	p.Trades = make([]models.Trade, 0)
	p.PeakBalance = p.InitialBalance
	p.DrawdownAmount = 0
}

// updatePeakAndDrawdown updates peak balance and drawdown
func (p *VirtualPortfolio) updatePeakAndDrawdown() {
	if p.CurrentBalance > p.PeakBalance {
		p.PeakBalance = p.CurrentBalance
	}
	
	drawdown := p.PeakBalance - p.CurrentBalance
	if drawdown > p.DrawdownAmount {
		p.DrawdownAmount = drawdown
	}
}

// PortfolioSummary represents a summary of portfolio metrics
type PortfolioSummary struct {
	Balance          float64 `json:"balance"`
	InitialBalance   float64 `json:"initial_balance"`
	TotalPL          float64 `json:"total_pl"`
	TotalCommissions float64 `json:"total_commissions"`
	PeakBalance      float64 `json:"peak_balance"`
	DrawdownAmount   float64 `json:"drawdown_amount"`
	PositionCount    int     `json:"position_count"`
	TradeCount       int     `json:"trade_count"`
}

