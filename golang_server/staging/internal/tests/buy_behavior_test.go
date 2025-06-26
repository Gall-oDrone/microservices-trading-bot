package tests

import (
	"bitso_trading_bot/internal/behaviors"
	"bitso_trading_bot/internal/database"
	"bitso_trading_bot/internal/models"
	"bitso_trading_bot/internal/order"
	"bitso_trading_bot/internal/trading_bot"
	"bitso_trading_bot/pkg/bitso"
	"bitso_trading_bot/table"
	"testing"
)

// setupTestBuyBehavior creates a new BuyBehavior instance with test data
func setupTestBuyBehavior(t *testing.T) (*behaviors.BuyBehavior, *trading_bot.TradingBot) {
	// Create test balance
	testBalance := bitso.Balance{
		Currency:  bitso.ToCurrency("mxn"),
		Total:     bitso.ToMonetary(1000.0),
		Locked:    bitso.ToMonetary(0.0),
		Available: bitso.ToMonetary(1000.0),
	}

	// Create test book
	testBook := bitso.NewBook(bitso.ToCurrency("btc"), bitso.ToCurrency("mxn"))

	// Create test clients
	bitsoClient := bitso.NewClient()
	dbClient := &database.RedisClient{} // Mock DB client
	orderManager := order.NewManager(bitsoClient, *dbClient, testBook)

	// Create trading bot with proper configuration
	config := &models.TradingConfig{
		Book: testBook,
	}
	tb := trading_bot.NewTradingBot(config, bitsoClient, dbClient)

	// Create buy behavior
	buyBehavior := behaviors.NewBuyBehavior(testBalance, bitsoClient, orderManager)

	return buyBehavior, tb
}

func TestBuyBehavior_CheckFunds(t *testing.T) {
	buyBehavior, _ := setupTestBuyBehavior(t)

	// Create test balance for this test
	testBalance := bitso.Balance{
		Currency:  bitso.ToCurrency("mxn"),
		Total:     bitso.ToMonetary(1000.0),
		Locked:    bitso.ToMonetary(0.0),
		Available: bitso.ToMonetary(1000.0),
	}

	tests := []struct {
		name    string
		min     float64
		wantErr bool
	}{
		{
			name:    "Sufficient funds",
			min:     100.0,
			wantErr: false,
		},
		{
			name:    "Insufficient funds",
			min:     2000.0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := buyBehavior.CheckFunds(tt.min, &testBalance)
			if (err != nil) != tt.wantErr {
				t.Errorf("CheckFunds() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestBuyBehavior_CalculateOptimalRate(t *testing.T) {
	buyBehavior, _ := setupTestBuyBehavior(t)

	// Create test ticker
	testTicker := &bitso.Ticker{
		Ask: bitso.ToMonetary(50000.0),
		Bid: bitso.ToMonetary(49900.0),
	}

	// Create test fee
	testFee := bitso.Fee{
		TakerFeeDecimal: bitso.ToMonetary(0.001), // 0.1%
		MakerFeeDecimal: bitso.ToMonetary(0.001),
	}

	tests := []struct {
		name        string
		ticker      *bitso.Ticker
		limit       float64
		amount      float64
		fee         bitso.Fee
		marketTrade string
		tableData   *table.TableData
		wantRate    float64
		wantErr     bool
	}{
		{
			name:        "Normal calculation",
			ticker:      testTicker,
			limit:       0,
			amount:      1000.0,
			fee:         testFee,
			marketTrade: "",
			tableData:   table.NewTableData(),
			wantRate:    49950.0, // Expected rate after fee adjustment
			wantErr:     false,
		},
		{
			name:        "With limit",
			ticker:      testTicker,
			limit:       49900.0,
			amount:      1000.0,
			fee:         testFee,
			marketTrade: "",
			tableData:   table.NewTableData(),
			wantRate:    49900.0, // Should use limit
			wantErr:     false,
		},
		{
			name:        "Nil ticker",
			ticker:      nil,
			limit:       0,
			amount:      1000.0,
			fee:         testFee,
			marketTrade: "",
			tableData:   table.NewTableData(),
			wantRate:    0,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotRate, err := buyBehavior.CalculateOptimalRate(tt.ticker, tt.limit, tt.amount, tt.fee, tt.marketTrade, tt.tableData)
			if (err != nil) != tt.wantErr {
				t.Errorf("CalculateOptimalRate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && gotRate != tt.wantRate {
				t.Errorf("CalculateOptimalRate() = %v, want %v", gotRate, tt.wantRate)
			}
		})
	}
}

func TestBuyBehavior_ConfigureOrder(t *testing.T) {
	buyBehavior, _ := setupTestBuyBehavior(t)

	testBook := bitso.NewBook(bitso.ToCurrency("btc"), bitso.ToCurrency("mxn"))
	testOrderType := bitso.OrderTypeLimit
	testAmount := 1000.0
	testRate := 50000.0

	buyBehavior.ConfigureOrder(*testBook, testOrderType, testAmount, testRate)
}
