package tests

import (
	"bitso_trading_bot/internal/database"
	"bitso_trading_bot/internal/order"
	"bitso_trading_bot/pkg/bitso"
	"fmt"
	"testing"
)

// setupTestOrderManager creates a new OrderManager instance with test data
func setupTestOrderManager(t *testing.T) *order.Manager {
	// Create test book
	testBook := bitso.NewBook(bitso.ToCurrency("btc"), bitso.ToCurrency("mxn"))

	// Create test clients
	bitsoClient := bitso.NewClient()
	dbClient := &database.RedisClient{} // Mock DB client

	// Create order manager
	orderManager := order.NewManager(bitsoClient, *dbClient, testBook)

	return orderManager
}

func TestOrderManager_GetOpenOrders(t *testing.T) {
	orderManager := setupTestOrderManager(t)

	fmt.Println("=== Testing GetOpenOrders() Method ===")
	fmt.Printf("Book: %s\n", orderManager.GetBitsoBook().String())
	fmt.Println("Calling GetOpenOrders()...")

	// Call GetOpenOrders method
	openOrders, err := orderManager.GetOpenOrders()

	fmt.Println("=== HTTP Response ===")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		t.Logf("GetOpenOrders() returned error: %v", err)
	} else {
		fmt.Printf("Success! Retrieved %d open orders\n", len(openOrders))

		if len(openOrders) > 0 {
			fmt.Println("Open Orders Details:")
			for i, order := range openOrders {
				fmt.Printf("Order %d:\n", i+1)
				fmt.Printf("  OID: %s\n", order.OID)
				fmt.Printf("  Book: %s\n", order.Book.String())
				fmt.Printf("  Side: %s\n", order.Side.String())
				fmt.Printf("  Status: %s\n", order.Status.String())
				fmt.Printf("  Type: %s\n", order.Type)
				fmt.Printf("  Price: %.8f\n", order.Price.Float64())
				fmt.Printf("  Original Amount: %.8f\n", order.OriginalAmount.Float64())
				fmt.Printf("  Unfilled Amount: %.8f\n", order.UnfilledAmount.Float64())
				fmt.Printf("  Original Value: %.8f\n", order.OriginalValue.Float64())
				fmt.Printf("  Created At: %s\n", order.CreatedAt)
				fmt.Printf("  Updated At: %s\n", order.UpdatedAt)
				fmt.Println("  ---")
			}
		} else {
			fmt.Println("No open orders found.")
		}
	}

	fmt.Println("=== Test Complete ===")
}

func TestOrderManager_GetOpenOrders_WithDummyData(t *testing.T) {
	orderManager := setupTestOrderManager(t)

	fmt.Println("=== Testing GetOpenOrders() with Dummy Data ===")
	fmt.Printf("Book: %s\n", orderManager.GetBitsoBook().String())
	fmt.Println("Calling GetOpenOrders()...")

	// Call GetOpenOrders method
	openOrders, err := orderManager.GetOpenOrders()

	fmt.Println("=== HTTP Response Analysis ===")
	if err != nil {
		fmt.Printf("Error Type: %T\n", err)
		fmt.Printf("Error Message: %v\n", err)
		fmt.Printf("Error Details: %+v\n", err)

		// Check if it's an API error
		if apiErr, ok := err.(*bitso.Error); ok {
			fmt.Printf("API Error Code: %d\n", apiErr.Code())
			fmt.Printf("API Error Message: %s\n", apiErr.Error())
		}

		t.Logf("GetOpenOrders() returned error: %v", err)
	} else {
		fmt.Printf("Success! Retrieved %d open orders\n", len(openOrders))

		// Print summary statistics
		fmt.Println("Summary:")
		fmt.Printf("  Total Orders: %d\n", len(openOrders))

		// Count by side
		buyCount := 0
		sellCount := 0
		for _, order := range openOrders {
			if order.Side == bitso.OrderSideBuy {
				buyCount++
			} else if order.Side == bitso.OrderSideSell {
				sellCount++
			}
		}
		fmt.Printf("  Buy Orders: %d\n", buyCount)
		fmt.Printf("  Sell Orders: %d\n", sellCount)

		// Count by status
		statusCount := make(map[string]int)
		for _, order := range openOrders {
			status := order.Status.String()
			statusCount[status]++
		}
		fmt.Println("  Orders by Status:")
		for status, count := range statusCount {
			fmt.Printf("    %s: %d\n", status, count)
		}
	}

	fmt.Println("=== Test Complete ===")
}

func TestOrderManager_GetBitsoClient_Ticker(t *testing.T) {
	orderManager := setupTestOrderManager(t)

	fmt.Println("=== Testing GetBitsoClient().Ticker() Method ===")
	fmt.Printf("Book: %s\n", orderManager.GetBitsoBook().String())
	fmt.Println("Calling GetBitsoClient().Ticker()...")

	// Get the Bitso client and call Ticker method
	bitsoClient := orderManager.GetBitsoClient()
	ticker, err := bitsoClient.Ticker(orderManager.GetBitsoBook())

	fmt.Println("=== HTTP Response ===")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		fmt.Printf("Error Type: %T\n", err)
		fmt.Printf("Error Details: %+v\n", err)
		// Check if it's an API error
		if apiErr, ok := err.(*bitso.Error); ok {
			fmt.Printf("API Error Code: %d\n", apiErr.Code())
			fmt.Printf("API Error Message: %s\n", apiErr.Error())
		}
		t.Logf("Ticker() returned error: %v", err)
	} else {
		fmt.Printf("Success! Retrieved ticker data\n")
		fmt.Println("Ticker Details:")
		fmt.Printf("  Book: %s\n", ticker.Book.String())
		fmt.Printf("  High: %.8f\n", ticker.High.Float64())
		fmt.Printf("  Last: %.8f\n", ticker.Last.Float64())
		fmt.Printf("  Created At: %s\n", ticker.CreatedAt)
		fmt.Printf("  Volume: %.8f\n", ticker.Volume.Float64())
		fmt.Printf("  Vwap: %.8f\n", ticker.Vwap.Float64())
		fmt.Printf("  Low: %.8f\n", ticker.Low.Float64())
		fmt.Printf("  Ask: %.8f\n", ticker.Ask.Float64())
		fmt.Printf("  Bid: %.8f\n", ticker.Bid.Float64())
	}

	fmt.Println("=== Test Complete ===")
}

func TestOrderManager_PlaceOrder(t *testing.T) {
	orderManager := setupTestOrderManager(t)

	fmt.Println("=== Testing PlaceOrder() Method ===")
	fmt.Printf("Book: %s\n", orderManager.GetBitsoBook().String())
	fmt.Println("Creating test order...")

	// Create a test order placement
	testOrder := &bitso.OrderPlacement{
		Book:  *orderManager.GetBitsoBook(),
		Side:  bitso.OrderSideBuy,
		Type:  bitso.OrderTypeLimit,
		Major: bitso.Monetary("0.001"),  // Small amount for testing
		Price: bitso.Monetary("500000"), // Price in MXN
	}

	fmt.Printf("Test Order Details:\n")
	fmt.Printf("  Book: %s\n", testOrder.Book.String())
	fmt.Printf("  Side: %s\n", testOrder.Side.String())
	fmt.Printf("  Type: %s\n", testOrder.Type.String())
	fmt.Printf("  Major Amount: %s\n", string(testOrder.Major))
	fmt.Printf("  Price: %s\n", string(testOrder.Price))
	fmt.Println("Calling PlaceOrder()...")

	// Call PlaceOrder method
	orderID, err := orderManager.PlaceOrder(testOrder)

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

		t.Logf("PlaceOrder() returned error: %v", err)
	} else {
		fmt.Printf("Success! Order placed with ID: %s\n", orderID)
		fmt.Println("Order Details:")
		fmt.Printf("  Order ID: %s\n", orderID)
		fmt.Printf("  Book: %s\n", testOrder.Book.String())
		fmt.Printf("  Side: %s\n", testOrder.Side.String())
		fmt.Printf("  Type: %s\n", testOrder.Type.String())
	}

	fmt.Println("=== Test Complete ===")
}

func TestOrderManager_PlaceOrder_MarketOrder(t *testing.T) {
	orderManager := setupTestOrderManager(t)

	fmt.Println("=== Testing PlaceOrder() with Market Order ===")
	fmt.Printf("Book: %s\n", orderManager.GetBitsoBook().String())
	fmt.Println("Creating market order...")

	// Create a market order placement (no price needed)
	testOrder := &bitso.OrderPlacement{
		Book:  *orderManager.GetBitsoBook(),
		Side:  bitso.OrderSideSell,
		Type:  bitso.OrderTypeMarket,
		Major: bitso.Monetary("0.0001"), // Very small amount for testing
	}

	fmt.Printf("Market Order Details:\n")
	fmt.Printf("  Book: %s\n", testOrder.Book.String())
	fmt.Printf("  Side: %s\n", testOrder.Side.String())
	fmt.Printf("  Type: %s\n", testOrder.Type.String())
	fmt.Printf("  Major Amount: %s\n", string(testOrder.Major))
	fmt.Println("Calling PlaceOrder()...")

	// Call PlaceOrder method
	orderID, err := orderManager.PlaceOrder(testOrder)

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

		t.Logf("PlaceOrder() returned error: %v", err)
	} else {
		fmt.Printf("Success! Market order placed with ID: %s\n", orderID)
		fmt.Println("Market Order Details:")
		fmt.Printf("  Order ID: %s\n", orderID)
		fmt.Printf("  Book: %s\n", testOrder.Book.String())
		fmt.Printf("  Side: %s\n", testOrder.Side.String())
		fmt.Printf("  Type: %s\n", testOrder.Type.String())
	}

	fmt.Println("=== Test Complete ===")
}
