package validator

import (
	"testing"

	"bitso-trading-platform/order-management/internal/config"
	"bitso-trading-platform/order-management/internal/logger"
	"bitso-trading-platform/order-management/internal/metrics"
	"bitso-trading-platform/order-management/internal/models"
	"bitso-trading-platform/order-management/internal/repository"
	sharedModels "bitso-trading-platform/shared/pkg/models"
)

func TestValidateSignal(t *testing.T) {
	validator := setupValidator()

	tests := []struct {
		name    string
		signal  *sharedModels.TradeSignalEvent
		wantErr bool
	}{
		{
			name: "valid BUY signal",
			signal: &sharedModels.TradeSignalEvent{
				EventID:   "evt-123",
				Timestamp: 1698160000000,
				Book:      "btc_mxn",
				Strategy:  "basic",
				Signal:    "BUY",
				Price:     500000.0,
				Amount:    0.01,
				Metadata:  make(map[string]interface{}),
			},
			wantErr: false,
		},
		{
			name: "valid SELL signal",
			signal: &sharedModels.TradeSignalEvent{
				EventID:   "evt-124",
				Timestamp: 1698160000000,
				Book:      "eth_mxn",
				Strategy:  "trend",
				Signal:    "SELL",
				Price:     25000.0,
				Amount:    0.5,
				Metadata:  make(map[string]interface{}),
			},
			wantErr: false,
		},
		{
			name: "invalid signal type",
			signal: &sharedModels.TradeSignalEvent{
				EventID:   "evt-125",
				Timestamp: 1698160000000,
				Book:      "btc_mxn",
				Strategy:  "basic",
				Signal:    "INVALID",
				Price:     500000.0,
				Amount:    0.01,
			},
			wantErr: true,
		},
		{
			name: "missing book",
			signal: &sharedModels.TradeSignalEvent{
				EventID:   "evt-126",
				Timestamp: 1698160000000,
				Book:      "",
				Strategy:  "basic",
				Signal:    "BUY",
				Price:     500000.0,
				Amount:    0.01,
			},
			wantErr: true,
		},
		{
			name: "zero price",
			signal: &sharedModels.TradeSignalEvent{
				EventID:   "evt-127",
				Timestamp: 1698160000000,
				Book:      "btc_mxn",
				Strategy:  "basic",
				Signal:    "BUY",
				Price:     0,
				Amount:    0.01,
			},
			wantErr: true,
		},
		{
			name: "zero amount",
			signal: &sharedModels.TradeSignalEvent{
				EventID:   "evt-128",
				Timestamp: 1698160000000,
				Book:      "btc_mxn",
				Strategy:  "basic",
				Signal:    "BUY",
				Price:     500000.0,
				Amount:    0,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateSignal(tt.signal)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateSignal() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateOrder(t *testing.T) {
	validator := setupValidator()

	tests := []struct {
		name    string
		order   *models.Order
		wantErr bool
	}{
		{
			name:    "valid order",
			order:   models.NewOrder("signal-1", "btc_mxn", "buy", "limit", "basic", 500000.0, 0.01),
			wantErr: false,
		},
		{
			name: "order too small",
			order: func() *models.Order {
				order := models.NewOrder("signal-2", "btc_mxn", "buy", "limit", "basic", 500000.0, 0.0001)
				return order
			}(),
			wantErr: true,
		},
		{
			name: "order value too large",
			order: func() *models.Order {
				order := models.NewOrder("signal-3", "btc_mxn", "buy", "limit", "basic", 5000000.0, 100.0)
				return order
			}(),
			wantErr: true,
		},
		{
			name: "invalid side",
			order: func() *models.Order {
				order := models.NewOrder("signal-4", "btc_mxn", "invalid", "limit", "basic", 500000.0, 0.01)
				return order
			}(),
			wantErr: true,
		},
		{
			name: "invalid type",
			order: func() *models.Order {
				order := models.NewOrder("signal-5", "btc_mxn", "buy", "invalid", "basic", 500000.0, 0.01)
				return order
			}(),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateOrder(tt.order)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateOrder() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateOrderSize(t *testing.T) {
	validator := setupValidator()

	tests := []struct {
		name    string
		amount  float64
		wantErr bool
	}{
		{"valid size", 0.01, false},
		{"minimum size", 0.001, false},
		{"too small", 0.0001, true},
		{"zero", 0, true},
		{"negative", -0.01, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			order := models.NewOrder("signal", "btc_mxn", "buy", "limit", "basic", 500000.0, tt.amount)
			err := validator.ValidateOrderSize(order)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateOrderSize() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateOrderValue(t *testing.T) {
	validator := setupValidator()

	tests := []struct {
		name    string
		price   float64
		amount  float64
		wantErr bool
	}{
		{"valid value", 500000.0, 0.01, false},
		{"max value", 500000.0, 0.2, false},
		{"too large", 5000000.0, 100.0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			order := models.NewOrder("signal", "btc_mxn", "buy", "limit", "basic", tt.price, tt.amount)
			err := validator.ValidateOrderValue(order)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateOrderValue() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateBook(t *testing.T) {
	validator := setupValidator()

	tests := []struct {
		name    string
		book    string
		wantErr bool
	}{
		{"valid book", "btc_mxn", false},
		{"valid book", "eth_mxn", false},
		{"invalid format - no underscore", "btcmxn", true},
		{"invalid format - empty", "", true},
		{"invalid format - only major", "btc_", true},
		{"invalid format - only minor", "_mxn", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateBook(tt.book)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateBook() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateSide(t *testing.T) {
	validator := setupValidator()

	tests := []struct {
		name    string
		side    string
		wantErr bool
	}{
		{"buy", "buy", false},
		{"sell", "sell", false},
		{"invalid", "invalid", true},
		{"empty", "", true},
		{"BUY uppercase", "BUY", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateSide(tt.side)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateSide() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateType(t *testing.T) {
	validator := setupValidator()

	tests := []struct {
		name    string
		typ     string
		wantErr bool
	}{
		{"market", "market", false},
		{"limit", "limit", false},
		{"invalid", "invalid", true},
		{"empty", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateType(tt.typ)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateType() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCheckDuplicates(t *testing.T) {
	validator := setupValidator()

	// Create an order in the repository
	order1 := models.NewOrder("signal-1", "btc_mxn", "buy", "limit", "basic", 500000.0, 0.01)
	validator.repo.Create(nil, order1)

	// Test duplicate check
	order2 := models.NewOrder("signal-1", "btc_mxn", "buy", "limit", "basic", 500000.0, 0.01)
	err := validator.CheckDuplicates(order2)
	if err == nil {
		t.Error("Expected duplicate error, got nil")
	}

	// Test non-duplicate
	order3 := models.NewOrder("signal-2", "btc_mxn", "buy", "limit", "basic", 500000.0, 0.01)
	err = validator.CheckDuplicates(order3)
	if err != nil {
		t.Errorf("Expected no error for non-duplicate, got: %v", err)
	}
}

func TestValidateStateTransition(t *testing.T) {
	validator := setupValidator()

	tests := []struct {
		name    string
		from    models.OrderStatus
		to      models.OrderStatus
		wantErr bool
	}{
		{"pending to validated", models.OrderStatusPending, models.OrderStatusValidated, false},
		{"validated to submitted", models.OrderStatusValidated, models.OrderStatusSubmitted, false},
		{"same state", models.OrderStatusPending, models.OrderStatusPending, true},
		{"from filled", models.OrderStatusFilled, models.OrderStatusCancelled, true},
		{"from cancelled", models.OrderStatusCancelled, models.OrderStatusPending, true},
		{"from rejected", models.OrderStatusRejected, models.OrderStatusPending, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateStateTransition(tt.from, tt.to)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateStateTransition() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// Helper functions

var (
	testValidatorLogger  *logger.Logger
	testValidatorMetrics *metrics.MetricsCollector
	testValidatorRepo    repository.OrderRepository
)

func init() {
	testValidatorLogger = logger.DefaultLogger()
	testValidatorMetrics = metrics.NewMetricsCollector("validator-test")
	testValidatorRepo = repository.NewInMemoryOrderRepository(testValidatorLogger, testValidatorMetrics)
}

func setupValidator() *Validator {
	cfg := &config.RiskConfig{
		MaxOpenOrders:        10,
		MaxOrderValue:        100000.0,
		MinOrderSize:         0.001,
		MaxPositionSize:      1.0,
		EnableDuplicateCheck: true,
		MaxOrdersPerMinute:   60,
	}

	return NewOrderValidator(cfg, testValidatorLogger, testValidatorRepo, testValidatorMetrics)
}
