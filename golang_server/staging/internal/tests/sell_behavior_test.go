package tests

import (
	"bitso_trading_bot/internal/behaviors"
	"bitso_trading_bot/internal/database"
	"bitso_trading_bot/internal/models"
	"bitso_trading_bot/internal/order"
	"bitso_trading_bot/internal/trading_bot"
	"bitso_trading_bot/pkg/bitso"
	"testing"
)

// setupTestSellBehavior creates a new SellBehavior instance with test data
func setupTestSellBehavior(t *testing.T) (*behaviors.SellBehavior, *trading_bot.TradingBot) {
	// Create test balance
	testBalance := bitso.Balance{
		Currency:  bitso.ToCurrency("btc"),
		Total:     bitso.ToMonetary(1.0),
		Locked:    bitso.ToMonetary(0.0),
		Available: bitso.ToMonetary(1.0),
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

	// Create sell behavior
	sellBehavior := behaviors.NewSellBehavior(testBalance, bitsoClient, orderManager)

	return sellBehavior, tb
}

func TestSellBehavior_CheckFunds(t *testing.T) {
	sellBehavior, _ := setupTestSellBehavior(t)

	// Create test balance for this test
	testBalance := bitso.Balance{
		Currency:  bitso.ToCurrency("btc"),
		Total:     bitso.ToMonetary(1.0),
		Locked:    bitso.ToMonetary(0.0),
		Available: bitso.ToMonetary(1.0),
	}

	tests := []struct {
		name    string
		min     float64
		wantErr bool
	}{
		{
			name:    "Sufficient funds",
			min:     0.1,
			wantErr: false,
		},
		{
			name:    "Insufficient funds",
			min:     2.0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := sellBehavior.CheckFunds(tt.min, &testBalance)
			if (err != nil) != tt.wantErr {
				t.Errorf("CheckFunds() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSellBehavior_CalculateOptimalRate(t *testing.T) {
	sellBehavior, _ := setupTestSellBehavior(t)

	// Create test ticker
	testTicker := &bitso.Ticker{
		Ask: bitso.ToMonetary(50000.0),
		Bid: bitso.ToMonetary(49900.0),
	}

	// Create test fee
	testFee := bitso.Fee{
		TakerFeeDecimal: bitso.ToMonetary(0.00650), // 0.65%
		MakerFeeDecimal: bitso.ToMonetary(0.00500), // 0.50%
	}

	tests := []struct {
		name        string
		ticker      *bitso.Ticker
		limit       float64
		amount      float64
		fee         bitso.Fee
		marketTrade string
		wantRate    float64
		wantErr     bool
	}{
		{
			name:        "Normal calculation",
			ticker:      testTicker,
			limit:       0,
			amount:      0.1,
			fee:         testFee,
			marketTrade: "",
			wantRate:    50224.35, // Expected rate after fee adjustment (49900 * 1.0065)
			wantErr:     false,
		},
		{
			name:        "With limit",
			ticker:      testTicker,
			limit:       51000.0,
			amount:      0.1,
			fee:         testFee,
			marketTrade: "",
			wantRate:    51000.0, // Should use limit
			wantErr:     false,
		},
		{
			name:        "Nil ticker",
			ticker:      nil,
			limit:       0,
			amount:      0.1,
			fee:         testFee,
			marketTrade: "",
			wantRate:    0,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotRate, err := sellBehavior.CalculateOptimalRate(tt.ticker, tt.limit, tt.amount, tt.fee, tt.marketTrade)
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

func TestSellBehavior_ConfigureOrder(t *testing.T) {
	sellBehavior, _ := setupTestSellBehavior(t)

	testBook := bitso.NewBook(bitso.ToCurrency("btc"), bitso.ToCurrency("mxn"))
	testOrderType := bitso.OrderTypeLimit
	testAmount := 0.1
	testRate := 50000.0

	sellBehavior.ConfigureOrder(*testBook, testOrderType, testAmount, testRate)
}
