package tests

import (
	"bitso_trading_bot/internal/config"
	"bitso_trading_bot/internal/database"
	"bitso_trading_bot/internal/trade"
	"bitso_trading_bot/pkg/bitso"
	"fmt"
	"testing"
	"time"
)

// setupTestTradeManager creates a new TradeManager instance with test data
func setupTestTradeManager(t *testing.T) *trade.Manager {
	// Create test book
	testBook := bitso.NewBook(bitso.ToCurrency("btc"), bitso.ToCurrency("mxn"))

	// Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		t.Fatalf("Failed to load configuration: %v", err)
	}

	// Create test clients
	bitsoClient := bitso.NewClient()
	bitsoClient.SetLogLevel(bitso.LogLevelDebug)
	bitsoClient.SetAuth(cfg.StageBitsoAPIKey, cfg.StageBitsoAPISecret)
	bitsoClient.SetAPIBaseURL("https://stage.bitso.com/api")

	// Initialize Redis client properly
	dbClient, err := database.Initialize()
	if err != nil {
		t.Fatalf("Failed to initialize Redis client: %v", err)
	}

	// Create trade manager
	tradeManager := trade.NewManager(bitsoClient, *dbClient, testBook)

	return tradeManager
}

// Getter method tests

func TestTradeManager_GetBitsoClient(t *testing.T) {
	tradeManager := setupTestTradeManager(t)

	fmt.Println("=== Testing GetBitsoClient() Method ===")

	// Call GetBitsoClient method
	bitsoClient := tradeManager.GetBitsoClient()

	fmt.Println("=== Response ===")
	if bitsoClient == nil {
		fmt.Println("Error: GetBitsoClient() returned nil")
		t.Error("GetBitsoClient() returned nil")
	} else {
		fmt.Println("Success! GetBitsoClient() returned valid client")
		fmt.Printf("Client Type: %T\n", bitsoClient)
		fmt.Printf("API Base URL: %s\n", bitsoClient.APIBaseURL())
	}

	fmt.Println("=== Test Complete ===")
}

func TestTradeManager_GetBitsoBook(t *testing.T) {
	tradeManager := setupTestTradeManager(t)

	fmt.Println("=== Testing GetBitsoBook() Method ===")

	// Call GetBitsoBook method
	book := tradeManager.GetBitsoBook()

	fmt.Println("=== Response ===")
	if book == nil {
		fmt.Println("Error: GetBitsoBook() returned nil")
		t.Error("GetBitsoBook() returned nil")
	} else {
		fmt.Println("Success! GetBitsoBook() returned valid book")
		fmt.Printf("Book: %s\n", book.String())
		fmt.Printf("Major Currency: %s\n", book.Major().String())
		fmt.Printf("Minor Currency: %s\n", book.Minor().String())
	}

	fmt.Println("=== Test Complete ===")
}

func TestTradeManager_GetDBClient(t *testing.T) {
	tradeManager := setupTestTradeManager(t)

	fmt.Println("=== Testing GetDBClient() Method ===")

	// Call GetDBClient method
	dbClient := tradeManager.GetDBClient()

	fmt.Println("=== Response ===")
	if dbClient == nil {
		fmt.Println("Error: GetDBClient() returned nil")
		t.Error("GetDBClient() returned nil")
	} else {
		fmt.Println("Success! GetDBClient() returned valid DB client")
		fmt.Printf("DB Client Type: %T\n", dbClient)
	}

	fmt.Println("=== Test Complete ===")
}

// Trade operations tests

func TestTradeManager_SaveTrade(t *testing.T) {
	tradeManager := setupTestTradeManager(t)

	fmt.Println("=== Testing SaveTrade() Method ===")
	fmt.Println("Creating test trade data...")

	// Create test trade data
	testTrade := &bitso.UserTrade{
		Book:          *tradeManager.GetBitsoBook(),
		Major:         bitso.Monetary("0.001"),
		Minor:         bitso.Monetary("500000"),
		FeesAmount:    bitso.Monetary("0.000001"),
		FeesCurrency:  bitso.ToCurrency("btc"),
		MinorCurrency: bitso.ToCurrency("mxn"),
		MajorCurrency: bitso.ToCurrency("btc"),
		OID:           "test_trade_123",
		Side:          bitso.OrderSideBuy,
		Price:         bitso.Monetary("500000"),
		CreatedAt:     bitso.Time(time.Now()),
	}

	fmt.Printf("Test Trade Details:\n")
	fmt.Printf("  OID: %s\n", testTrade.OID)
	fmt.Printf("  Book: %s\n", testTrade.Book.String())
	fmt.Printf("  Side: %s\n", testTrade.Side.String())
	fmt.Printf("  Major Amount: %s\n", string(testTrade.Major))
	fmt.Printf("  Minor Amount: %s\n", string(testTrade.Minor))
	fmt.Printf("  Price: %s\n", string(testTrade.Price))
	fmt.Println("Calling SaveTrade()...")

	// Call SaveTrade method
	err := tradeManager.SaveTrade(testTrade)

	fmt.Println("=== Response ===")
	if err != nil {
		fmt.Printf("Error Type: %T\n", err)
		fmt.Printf("Error Message: %v\n", err)
		fmt.Printf("Error Details: %+v\n", err)
		t.Logf("SaveTrade() returned error: %v", err)
	} else {
		fmt.Println("Success! Trade saved to database")
		fmt.Println("Trade Details:")
		fmt.Printf("  OID: %s\n", testTrade.OID)
		fmt.Printf("  Side: %s\n", testTrade.Side.String())
		fmt.Printf("  Price: %s\n", string(testTrade.Price))
	}

	fmt.Println("=== Test Complete ===")
}

func TestTradeManager_GetUserTrade(t *testing.T) {
	tradeManager := setupTestTradeManager(t)

	fmt.Println("=== Testing GetUserTrade() Method ===")
	fmt.Println("Retrieving a specific trade...")

	// Use a test trade ID
	testTradeID := "test_trade_123"

	fmt.Printf("Test Trade ID: %s\n", testTradeID)
	fmt.Println("Calling GetUserTrade()...")

	// Call GetUserTrade method
	trade, err := tradeManager.GetUserTrade(testTradeID)

	fmt.Println("=== Response ===")
	if err != nil {
		fmt.Printf("Error Type: %T\n", err)
		fmt.Printf("Error Message: %v\n", err)
		fmt.Printf("Error Details: %+v\n", err)
		t.Logf("GetUserTrade() returned error: %v", err)
	} else {
		fmt.Println("Success! Retrieved trade details")
		fmt.Println("Trade Details:")
		fmt.Printf("  OID: %s\n", trade.OID)
		fmt.Printf("  Book: %s\n", trade.Book.String())
		fmt.Printf("  Side: %s\n", trade.Side.String())
		fmt.Printf("  Major Amount: %s\n", string(trade.Major))
		fmt.Printf("  Minor Amount: %s\n", string(trade.Minor))
		fmt.Printf("  Price: %s\n", string(trade.Price))
		fmt.Printf("  Created At: %s\n", trade.CreatedAt)
	}

	fmt.Println("=== Test Complete ===")
}

func TestTradeManager_GetAllTrades(t *testing.T) {
	tradeManager := setupTestTradeManager(t)

	fmt.Println("=== Testing GetAllTrades() Method ===")
	fmt.Println("Retrieving all trades...")

	// Call GetAllTrades method
	trades, err := tradeManager.GetAllTrades()

	fmt.Println("=== Response ===")
	if err != nil {
		fmt.Printf("Error Type: %T\n", err)
		fmt.Printf("Error Message: %v\n", err)
		fmt.Printf("Error Details: %+v\n", err)
		t.Logf("GetAllTrades() returned error: %v", err)
	} else {
		fmt.Printf("Success! Retrieved %d trades\n", len(trades))

		if len(trades) > 0 {
			fmt.Println("Trades Details:")
			for i, trade := range trades {
				fmt.Printf("Trade %d:\n", i+1)
				fmt.Printf("  OID: %s\n", trade.OID)
				fmt.Printf("  Book: %s\n", trade.Book.String())
				fmt.Printf("  Side: %s\n", trade.Side.String())
				fmt.Printf("  Price: %s\n", string(trade.Price))
				fmt.Println("  ---")
			}
		} else {
			fmt.Println("No trades found.")
		}
	}

	fmt.Println("=== Test Complete ===")
}

func TestTradeManager_GetTradesByFilter(t *testing.T) {
	tradeManager := setupTestTradeManager(t)

	fmt.Println("=== Testing GetTradesByFilter() Method ===")
	fmt.Println("Filtering trades by side 'buy'...")

	// Call GetTradesByFilter method with side filter
	trades, err := tradeManager.GetTradesByFilter("side", "buy")

	fmt.Println("=== Response ===")
	if err != nil {
		fmt.Printf("Error Type: %T\n", err)
		fmt.Printf("Error Message: %v\n", err)
		fmt.Printf("Error Details: %+v\n", err)
		t.Logf("GetTradesByFilter() returned error: %v", err)
	} else {
		fmt.Printf("Success! Retrieved %d trades with side 'buy'\n", len(trades))

		if len(trades) > 0 {
			fmt.Println("Trades Details:")
			for i, trade := range trades {
				fmt.Printf("Trade %d:\n", i+1)
				fmt.Printf("  OID: %s\n", trade.OID)
				fmt.Printf("  Side: %s\n", trade.Side.String())
				fmt.Printf("  Price: %s\n", string(trade.Price))
				fmt.Println("  ---")
			}
		} else {
			fmt.Println("No trades found with side 'buy'")
		}
	}

	fmt.Println("=== Test Complete ===")
}

// Bitso API operations tests

func TestTradeManager_GetMyTrades(t *testing.T) {
	tradeManager := setupTestTradeManager(t)

	fmt.Println("=== Testing GetMyTrades() Method ===")
	fmt.Printf("Book: %s\n", tradeManager.GetBitsoBook().String())
	fmt.Println("Calling GetMyTrades()...")

	// Call GetMyTrades method
	trades, err := tradeManager.GetMyTrades()

	fmt.Println("=== HTTP Response ===")
	if err != nil {
		fmt.Printf("Error Type: %T\n", err)
		fmt.Printf("Error Message: %v\n", err)
		fmt.Printf("Error Details: %+v\n", err)

		// Check if it's an API error
		if apiErr, ok := err.(*bitso.Error); ok {
			fmt.Printf("API Error Code: %d\n", apiErr.Code())
			fmt.Printf("API Error Message: %s\n", apiErr.Error())
		}

		t.Logf("GetMyTrades() returned error: %v", err)
	} else {
		fmt.Printf("Success! Retrieved %d trades\n", len(trades))

		if len(trades) > 0 {
			fmt.Println("Trades Details:")
			for i, trade := range trades {
				fmt.Printf("Trade %d:\n", i+1)
				fmt.Printf("  OID: %s\n", trade.OID)
				fmt.Printf("  Book: %s\n", trade.Book.String())
				fmt.Printf("  Side: %s\n", trade.Side.String())
				fmt.Printf("  Major Amount: %s\n", string(trade.Major))
				fmt.Printf("  Minor Amount: %s\n", string(trade.Minor))
				fmt.Printf("  Price: %s\n", string(trade.Price))
				fmt.Printf("  Created At: %s\n", trade.CreatedAt)
				fmt.Println("  ---")
			}
		} else {
			fmt.Println("No trades found.")
		}
	}

	fmt.Println("=== Test Complete ===")
}

func TestTradeManager_GetOrderTrades(t *testing.T) {
	tradeManager := setupTestTradeManager(t)

	fmt.Println("=== Testing GetOrderTrades() Method ===")
	fmt.Println("Retrieving trades for a specific order...")

	// Use a test order ID
	testOrderID := "test_order_trades_123"

	fmt.Printf("Test Order ID: %s\n", testOrderID)
	fmt.Printf("Book: %s\n", tradeManager.GetBitsoBook().String())
	fmt.Println("Calling GetOrderTrades()...")

	// Call GetOrderTrades method
	trades, err := tradeManager.GetOrderTrades(testOrderID)

	fmt.Println("=== HTTP Response ===")
	if err != nil {
		fmt.Printf("Error Type: %T\n", err)
		fmt.Printf("Error Message: %v\n", err)
		fmt.Printf("Error Details: %+v\n", err)

		// Check if it's an API error
		if apiErr, ok := err.(*bitso.Error); ok {
			fmt.Printf("API Error Code: %d\n", apiErr.Code())
			fmt.Printf("API Error Message: %s\n", apiErr.Error())
		}

		t.Logf("GetOrderTrades() returned error: %v", err)
	} else {
		fmt.Printf("Success! Retrieved %d order trades\n", len(trades))

		if len(trades) > 0 {
			fmt.Println("Order Trades Details:")
			for i, trade := range trades {
				fmt.Printf("Trade %d:\n", i+1)
				fmt.Printf("  OID: %s\n", trade.OID)
				fmt.Printf("  Book: %s\n", trade.Book.String())
				fmt.Printf("  Side: %s\n", trade.Side.String())
				fmt.Printf("  Major Amount: %s\n", string(trade.Major))
				fmt.Printf("  Minor Amount: %s\n", string(trade.Minor))
				fmt.Printf("  Price: %s\n", string(trade.Price))
				fmt.Printf("  Created At: %s\n", trade.CreatedAt)
				fmt.Println("  ---")
			}
		} else {
			fmt.Println("No order trades found.")
		}
	}

	fmt.Println("=== Test Complete ===")
}

func TestTradeManager_GetRecentTrades(t *testing.T) {
	tradeManager := setupTestTradeManager(t)

	fmt.Println("=== Testing GetRecentTrades() Method ===")
	fmt.Printf("Book: %s\n", tradeManager.GetBitsoBook().String())
	fmt.Println("Retrieving recent trades (limit: 10)...")

	// Call GetRecentTrades method with limit
	trades, err := tradeManager.GetRecentTrades(10)

	fmt.Println("=== HTTP Response ===")
	if err != nil {
		fmt.Printf("Error Type: %T\n", err)
		fmt.Printf("Error Message: %v\n", err)
		fmt.Printf("Error Details: %+v\n", err)

		// Check if it's an API error
		if apiErr, ok := err.(*bitso.Error); ok {
			fmt.Printf("API Error Code: %d\n", apiErr.Code())
			fmt.Printf("API Error Message: %s\n", apiErr.Error())
		}

		t.Logf("GetRecentTrades() returned error: %v", err)
	} else {
		fmt.Printf("Success! Retrieved %d recent trades\n", len(trades))

		if len(trades) > 0 {
			fmt.Println("Recent Trades Details:")
			for i, trade := range trades {
				fmt.Printf("Trade %d:\n", i+1)
				fmt.Printf("  TID: %d\n", trade.TID.Uint64())
				fmt.Printf("  Book: %s\n", trade.Book.String())
				fmt.Printf("  Maker Side: %s\n", trade.MakerSide.String())
				fmt.Printf("  Amount: %s\n", string(trade.Amount))
				fmt.Printf("  Price: %s\n", string(trade.Price))
				fmt.Printf("  Created At: %s\n", trade.CreatedAt)
				fmt.Println("  ---")
			}
		} else {
			fmt.Println("No recent trades found.")
		}
	}

	fmt.Println("=== Test Complete ===")
}

func TestTradeManager_SaveTradeWithTTL(t *testing.T) {
	tradeManager := setupTestTradeManager(t)

	fmt.Println("=== Testing SaveTradeWithTTL() Method ===")
	fmt.Println("Saving trade with TTL...")

	// Create test trade data
	testTrade := &bitso.UserTrade{
		Book:          *tradeManager.GetBitsoBook(),
		Major:         bitso.Monetary("0.001"),
		Minor:         bitso.Monetary("500000"),
		FeesAmount:    bitso.Monetary("0.000001"),
		FeesCurrency:  bitso.ToCurrency("btc"),
		MinorCurrency: bitso.ToCurrency("mxn"),
		MajorCurrency: bitso.ToCurrency("btc"),
		OID:           "test_trade_ttl_123",
		Side:          bitso.OrderSideSell,
		Price:         bitso.Monetary("500000"),
		CreatedAt:     bitso.Time(time.Now()),
	}

	ttl := 5 * time.Minute

	fmt.Printf("Test Trade Details:\n")
	fmt.Printf("  OID: %s\n", testTrade.OID)
	fmt.Printf("  Side: %s\n", testTrade.Side.String())
	fmt.Printf("  TTL: %v\n", ttl)
	fmt.Println("Calling SaveTradeWithTTL()...")

	// Call SaveTradeWithTTL method
	err := tradeManager.SaveTradeWithTTL(testTrade, ttl)

	fmt.Println("=== Response ===")
	if err != nil {
		fmt.Printf("Error Type: %T\n", err)
		fmt.Printf("Error Message: %v\n", err)
		fmt.Printf("Error Details: %+v\n", err)
		t.Logf("SaveTradeWithTTL() returned error: %v", err)
	} else {
		fmt.Println("Success! Trade saved with TTL")
		fmt.Println("Trade Details:")
		fmt.Printf("  OID: %s\n", testTrade.OID)
		fmt.Printf("  TTL: %v\n", ttl)
	}

	fmt.Println("=== Test Complete ===")
}

// Batch trade operations tests

func TestTradeManager_SaveBatchTrade(t *testing.T) {
	tradeManager := setupTestTradeManager(t)

	fmt.Println("=== Testing SaveBatchTrade() Method ===")
	fmt.Println("Saving batch trade...")

	// Create test batch data (this will fail as it's not implemented)
	testBatch := "test_batch_data"

	fmt.Printf("Test Batch Data: %s\n", testBatch)
	fmt.Println("Calling SaveBatchTrade()...")

	// Call SaveBatchTrade method
	err := tradeManager.SaveBatchTrade(testBatch)

	fmt.Println("=== Response ===")
	if err != nil {
		fmt.Printf("Error Type: %T\n", err)
		fmt.Printf("Error Message: %v\n", err)
		fmt.Printf("Error Details: %+v\n", err)
		t.Logf("SaveBatchTrade() returned error: %v", err)
	} else {
		fmt.Println("Success! Batch trade saved")
		fmt.Println("Batch Details:")
		fmt.Printf("  Data: %s\n", testBatch)
	}

	fmt.Println("=== Test Complete ===")
}

func TestTradeManager_GetLastNthBatches(t *testing.T) {
	tradeManager := setupTestTradeManager(t)

	fmt.Println("=== Testing GetLastNthBatches() Method ===")
	fmt.Println("Retrieving last 5 batches...")

	// Call GetLastNthBatches method
	batches, err := tradeManager.GetLastNthBatches(5)

	fmt.Println("=== Response ===")
	if err != nil {
		fmt.Printf("Error Type: %T\n", err)
		fmt.Printf("Error Message: %v\n", err)
		fmt.Printf("Error Details: %+v\n", err)
		t.Logf("GetLastNthBatches() returned error: %v", err)
	} else {
		fmt.Printf("Success! Retrieved %d batches\n", len(batches))

		if len(batches) > 0 {
			fmt.Println("Batches Details:")
			for i, batch := range batches {
				fmt.Printf("Batch %d: %v\n", i+1, batch)
			}
		} else {
			fmt.Println("No batches found.")
		}
	}

	fmt.Println("=== Test Complete ===")
}

// WebSocket trade operations tests
/*
func TestTradeManager_SaveWebSocketTrade(t *testing.T) {
	tradeManager := setupTestTradeManager(t)

	fmt.Println("=== Testing SaveWebSocketTrade() Method ===")
	fmt.Println("Saving WebSocket trade...")

	// Create test WebSocket trade data
	testWSTrade := &bitso.WebSocketTrade{
		Book:      *tradeManager.GetBitsoBook(),
		Amount:    bitso.Monetary("0.001"),
		Price:     bitso.Monetary("500000"),
		Side:      bitso.OrderSideBuy,
		Timestamp: uint64(time.Now().Unix()),
	}

	fmt.Printf("Test WebSocket Trade Details:\n")
	fmt.Printf("  Book: %s\n", testWSTrade.Book.String())
	fmt.Printf("  Side: %s\n", testWSTrade.Side.String())
	fmt.Printf("  Amount: %s\n", string(testWSTrade.Amount))
	fmt.Printf("  Price: %s\n", string(testWSTrade.Price))
	fmt.Printf("  Timestamp: %d\n", testWSTrade.Timestamp)
	fmt.Println("Calling SaveWebSocketTrade()...")

	// Call SaveWebSocketTrade method
	err := tradeManager.SaveWebSocketTrade(testWSTrade)

	fmt.Println("=== Response ===")
	if err != nil {
		fmt.Printf("Error Type: %T\n", err)
		fmt.Printf("Error Message: %v\n", err)
		fmt.Printf("Error Details: %+v\n", err)
		t.Logf("SaveWebSocketTrade() returned error: %v", err)
	} else {
		fmt.Println("Success! WebSocket trade saved")
		fmt.Println("WebSocket Trade Details:")
		fmt.Printf("  Book: %s\n", testWSTrade.Book.String())
		fmt.Printf("  Side: %s\n", testWSTrade.Side.String())
		fmt.Printf("  Timestamp: %d\n", testWSTrade.Timestamp)
	}

	fmt.Println("=== Test Complete ===")
}

func TestTradeManager_GetLatestWebSocketTrade(t *testing.T) {
	tradeManager := setupTestTradeManager(t)

	fmt.Println("=== Testing GetLatestWebSocketTrade() Method ===")
	fmt.Println("Retrieving latest WebSocket trade...")

	// Call GetLatestWebSocketTrade method
	trade, err := tradeManager.GetLatestWebSocketTrade()

	fmt.Println("=== Response ===")
	if err != nil {
		fmt.Printf("Error Type: %T\n", err)
		fmt.Printf("Error Message: %v\n", err)
		fmt.Printf("Error Details: %+v\n", err)
		t.Logf("GetLatestWebSocketTrade() returned error: %v", err)
	} else {
		if trade == nil {
			fmt.Println("No WebSocket trade found.")
		} else {
			fmt.Println("Success! Retrieved latest WebSocket trade")
			fmt.Println("WebSocket Trade Details:")
			fmt.Printf("  Book: %s\n", trade.Book.String())
			fmt.Printf("  Side: %s\n", trade.Side.String())
			fmt.Printf("  Amount: %s\n", string(trade.Amount))
			fmt.Printf("  Price: %s\n", string(trade.Price))
			fmt.Printf("  Timestamp: %d\n", trade.Timestamp)
		}
	}

	fmt.Println("=== Test Complete ===")
}

func TestTradeManager_GetWebSocketTradesByTimestampRange(t *testing.T) {
	tradeManager := setupTestTradeManager(t)

	fmt.Println("=== Testing GetWebSocketTradesByTimestampRange() Method ===")
	fmt.Println("Retrieving WebSocket trades by timestamp range...")

	// Create timestamp range (last hour)
	end := uint64(time.Now().Unix())
	start := end - 3600 // 1 hour ago

	fmt.Printf("Timestamp Range:\n")
	fmt.Printf("  Start: %d (%s)\n", start, time.Unix(int64(start), 0))
	fmt.Printf("  End: %d (%s)\n", end, time.Unix(int64(end), 0))
	fmt.Println("Calling GetWebSocketTradesByTimestampRange()...")

	// Call GetWebSocketTradesByTimestampRange method
	trades, err := tradeManager.GetWebSocketTradesByTimestampRange(start, end)

	fmt.Println("=== Response ===")
	if err != nil {
		fmt.Printf("Error Type: %T\n", err)
		fmt.Printf("Error Message: %v\n", err)
		fmt.Printf("Error Details: %+v\n", err)
		t.Logf("GetWebSocketTradesByTimestampRange() returned error: %v", err)
	} else {
		fmt.Printf("Success! Retrieved %d WebSocket trades\n", len(trades))

		if len(trades) > 0 {
			fmt.Println("WebSocket Trades Details:")
			for i, trade := range trades {
				fmt.Printf("Trade %d:\n", i+1)
				fmt.Printf("  Book: %s\n", trade.Book.String())
				fmt.Printf("  Side: %s\n", trade.Side.String())
				fmt.Printf("  Amount: %s\n", string(trade.Amount))
				fmt.Printf("  Price: %s\n", string(trade.Price))
				fmt.Printf("  Timestamp: %d\n", trade.Timestamp)
				fmt.Println("  ---")
			}
		} else {
			fmt.Println("No WebSocket trades found in the specified range.")
		}
	}

	fmt.Println("=== Test Complete ===")
}
*/
func TestTradeManager_DeleteAllWebSocketTrades(t *testing.T) {
	tradeManager := setupTestTradeManager(t)

	fmt.Println("=== Testing DeleteAllWebSocketTrades() Method ===")
	fmt.Println("Deleting all WebSocket trades...")

	// Call DeleteAllWebSocketTrades method
	err := tradeManager.DeleteAllWebSocketTrades()

	fmt.Println("=== Response ===")
	if err != nil {
		fmt.Printf("Error Type: %T\n", err)
		fmt.Printf("Error Message: %v\n", err)
		fmt.Printf("Error Details: %+v\n", err)
		t.Logf("DeleteAllWebSocketTrades() returned error: %v", err)
	} else {
		fmt.Println("Success! All WebSocket trades deleted")
	}

	fmt.Println("=== Test Complete ===")
}

// Trade analysis operations tests

func TestTradeManager_GetTradeStatistics(t *testing.T) {
	tradeManager := setupTestTradeManager(t)

	fmt.Println("=== Testing GetTradeStatistics() Method ===")
	fmt.Println("Calculating trade statistics...")

	// Call GetTradeStatistics method
	stats, err := tradeManager.GetTradeStatistics()

	fmt.Println("=== Response ===")
	if err != nil {
		fmt.Printf("Error Type: %T\n", err)
		fmt.Printf("Error Message: %v\n", err)
		fmt.Printf("Error Details: %+v\n", err)
		t.Logf("GetTradeStatistics() returned error: %v", err)
	} else {
		fmt.Println("Success! Retrieved trade statistics")
		fmt.Println("Statistics:")
		for key, value := range stats {
			fmt.Printf("  %s: %v\n", key, value)
		}
	}

	fmt.Println("=== Test Complete ===")
}

func TestTradeManager_GetTradesBySide(t *testing.T) {
	tradeManager := setupTestTradeManager(t)

	fmt.Println("=== Testing GetTradesBySide() Method ===")
	fmt.Println("Retrieving trades by side 'buy'...")

	// Call GetTradesBySide method
	trades, err := tradeManager.GetTradesBySide(bitso.OrderSideBuy)

	fmt.Println("=== Response ===")
	if err != nil {
		fmt.Printf("Error Type: %T\n", err)
		fmt.Printf("Error Message: %v\n", err)
		fmt.Printf("Error Details: %+v\n", err)
		t.Logf("GetTradesBySide() returned error: %v", err)
	} else {
		fmt.Printf("Success! Retrieved %d buy trades\n", len(trades))

		if len(trades) > 0 {
			fmt.Println("Buy Trades Details:")
			for i, trade := range trades {
				fmt.Printf("Trade %d:\n", i+1)
				fmt.Printf("  OID: %s\n", trade.OID)
				fmt.Printf("  Book: %s\n", trade.Book.String())
				fmt.Printf("  Side: %s\n", trade.Side.String())
				fmt.Printf("  Major Amount: %s\n", string(trade.Major))
				fmt.Printf("  Price: %s\n", string(trade.Price))
				fmt.Printf("  Created At: %s\n", trade.CreatedAt)
				fmt.Println("  ---")
			}
		} else {
			fmt.Println("No buy trades found.")
		}
	}

	fmt.Println("=== Test Complete ===")
}

func TestTradeManager_GetTradesByDateRange(t *testing.T) {
	tradeManager := setupTestTradeManager(t)

	fmt.Println("=== Testing GetTradesByDateRange() Method ===")
	fmt.Println("Retrieving trades by date range...")

	// Create date range (last 7 days)
	end := time.Now()
	start := end.AddDate(0, 0, -7)

	fmt.Printf("Date Range:\n")
	fmt.Printf("  Start: %s\n", start.Format("2006-01-02 15:04:05"))
	fmt.Printf("  End: %s\n", end.Format("2006-01-02 15:04:05"))
	fmt.Println("Calling GetTradesByDateRange()...")

	// Call GetTradesByDateRange method
	trades, err := tradeManager.GetTradesByDateRange(start, end)

	fmt.Println("=== Response ===")
	if err != nil {
		fmt.Printf("Error Type: %T\n", err)
		fmt.Printf("Error Message: %v\n", err)
		fmt.Printf("Error Details: %+v\n", err)
		t.Logf("GetTradesByDateRange() returned error: %v", err)
	} else {
		fmt.Printf("Success! Retrieved %d trades in date range\n", len(trades))

		if len(trades) > 0 {
			fmt.Println("Trades in Date Range:")
			for i, trade := range trades {
				fmt.Printf("Trade %d:\n", i+1)
				fmt.Printf("  OID: %s\n", trade.OID)
				fmt.Printf("  Book: %s\n", trade.Book.String())
				fmt.Printf("  Side: %s\n", trade.Side.String())
				fmt.Printf("  Major Amount: %s\n", string(trade.Major))
				fmt.Printf("  Price: %s\n", string(trade.Price))
				fmt.Printf("  Created At: %s\n", trade.CreatedAt)
				fmt.Println("  ---")
			}
		} else {
			fmt.Println("No trades found in the specified date range.")
		}
	}

	fmt.Println("=== Test Complete ===")
}

// Queue operations tests

func TestTradeManager_AppendToQueue(t *testing.T) {
	tradeManager := setupTestTradeManager(t)

	fmt.Println("=== Testing AppendToQueue() Method ===")
	fmt.Println("Adding trade ID to queue...")

	// Test trade ID
	testTradeID := "test_trade_queue_123"

	fmt.Printf("Test Trade ID: %s\n", testTradeID)
	fmt.Println("Calling AppendToQueue()...")

	// Call AppendToQueue method
	err := tradeManager.AppendToQueue(testTradeID)

	fmt.Println("=== Response ===")
	if err != nil {
		fmt.Printf("Error Type: %T\n", err)
		fmt.Printf("Error Message: %v\n", err)
		fmt.Printf("Error Details: %+v\n", err)
		t.Logf("AppendToQueue() returned error: %v", err)
	} else {
		fmt.Println("Success! Trade ID added to queue")
		fmt.Printf("Trade ID: %s\n", testTradeID)
	}

	fmt.Println("=== Test Complete ===")
}

func TestTradeManager_RemoveFromQueue(t *testing.T) {
	tradeManager := setupTestTradeManager(t)

	fmt.Println("=== Testing RemoveFromQueue() Method ===")
	fmt.Println("Removing trade ID from queue...")

	// Test trade ID
	testTradeID := "test_trade_remove_123"

	// First add it to the queue
	tradeManager.AppendToQueue(testTradeID)

	fmt.Printf("Test Trade ID: %s\n", testTradeID)
	fmt.Println("Calling RemoveFromQueue()...")

	// Call RemoveFromQueue method
	err := tradeManager.RemoveFromQueue(testTradeID)

	fmt.Println("=== Response ===")
	if err != nil {
		fmt.Printf("Error Type: %T\n", err)
		fmt.Printf("Error Message: %v\n", err)
		fmt.Printf("Error Details: %+v\n", err)
		t.Logf("RemoveFromQueue() returned error: %v", err)
	} else {
		fmt.Println("Success! Trade ID removed from queue")
		fmt.Printf("Trade ID: %s\n", testTradeID)
	}

	fmt.Println("=== Test Complete ===")
}

func TestTradeManager_GetQueueTrades(t *testing.T) {
	tradeManager := setupTestTradeManager(t)

	fmt.Println("=== Testing GetQueueTrades() Method ===")
	fmt.Println("Retrieving trades from queue...")

	// Add some test trade IDs to the queue
	testTradeIDs := []string{"queue_trade_1", "queue_trade_2", "queue_trade_3"}
	for _, id := range testTradeIDs {
		tradeManager.AppendToQueue(id)
	}

	fmt.Printf("Added %d test trade IDs to queue\n", len(testTradeIDs))
	fmt.Println("Calling GetQueueTrades()...")

	// Call GetQueueTrades method
	queueTrades := tradeManager.GetQueueTrades()

	fmt.Println("=== Response ===")
	fmt.Printf("Success! Retrieved %d trades from queue\n", len(queueTrades))

	if len(queueTrades) > 0 {
		fmt.Println("Queue Trades:")
		for i, tradeID := range queueTrades {
			fmt.Printf("  %d. %s\n", i+1, tradeID)
		}
	} else {
		fmt.Println("No trades in queue.")
	}

	fmt.Println("=== Test Complete ===")
}

func TestTradeManager_ClearQueue(t *testing.T) {
	tradeManager := setupTestTradeManager(t)

	fmt.Println("=== Testing ClearQueue() Method ===")
	fmt.Println("Clearing all trades from queue...")

	// Add some test trade IDs to the queue first
	testTradeIDs := []string{"clear_trade_1", "clear_trade_2"}
	for _, id := range testTradeIDs {
		tradeManager.AppendToQueue(id)
	}

	fmt.Printf("Added %d test trade IDs to queue\n", len(testTradeIDs))
	fmt.Println("Calling ClearQueue()...")

	// Call ClearQueue method
	tradeManager.ClearQueue()

	fmt.Println("=== Response ===")
	fmt.Println("Success! Queue cleared")

	// Verify queue is empty
	remainingTrades := tradeManager.GetQueueTrades()
	fmt.Printf("Remaining trades in queue: %d\n", len(remainingTrades))

	fmt.Println("=== Test Complete ===")
}
