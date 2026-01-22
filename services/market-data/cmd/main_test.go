package main

import (
	"context"
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/bitso"
)

// TestParseBook tests the parseBook function
func TestParseBook(t *testing.T) {
	tests := []struct {
		name        string
		bookStr     string
		expected    *bitso.Book
		expectError bool
	}{
		{
			name:        "Valid BTC/MXN book",
			bookStr:     "btc_mxn",
			expected:    bitso.ToBook("btc_mxn"),
			expectError: false,
		},
		{
			name:        "Valid ETH/MXN book",
			bookStr:     "eth_mxn",
			expected:    bitso.ToBook("eth_mxn"),
			expectError: false,
		},
		{
			name:        "Valid XRP/MXN book",
			bookStr:     "xrp_mxn",
			expected:    bitso.ToBook("xrp_mxn"),
			expectError: false,
		},
		{
			name:        "Invalid format - no underscore",
			bookStr:     "btcmxn",
			expected:    nil,
			expectError: true,
		},
		{
			name:        "Invalid format - multiple underscores",
			bookStr:     "btc_mxn_usd",
			expected:    nil,
			expectError: true,
		},
		{
			name:        "Invalid format - empty string",
			bookStr:     "",
			expected:    nil,
			expectError: true,
		},
		{
			name:        "Invalid format - only underscore",
			bookStr:     "_",
			expected:    nil,
			expectError: true,
		},
		{
			name:        "Invalid format - underscore at start",
			bookStr:     "_btc_mxn",
			expected:    nil,
			expectError: true,
		},
		{
			name:        "Invalid format - underscore at end",
			bookStr:     "btc_mxn_",
			expected:    nil,
			expectError: true,
		},
		{
			name:        "Invalid currency - unknown major",
			bookStr:     "unknown_mxn",
			expected:    nil,
			expectError: true,
		},
		{
			name:        "Invalid currency - unknown minor",
			bookStr:     "btc_unknown",
			expected:    nil,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseBook(tt.bookStr)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for input '%s', but got none", tt.bookStr)
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error for input '%s': %v", tt.bookStr, err)
				return
			}

			if result == nil {
				t.Errorf("Expected non-nil result for input '%s'", tt.bookStr)
				return
			}

			if result.String() != tt.expected.String() {
				t.Errorf("Expected book '%s', got '%s'", tt.expected.String(), result.String())
			}
		})
	}
}

// TestApplicationCreation tests application creation
func TestApplicationCreation(t *testing.T) {
	// This test is limited since we can't easily mock all dependencies
	// In a real scenario, we would use dependency injection or interfaces

	// Test that NewApplication returns an error when config loading fails
	// We can't easily test this without modifying the global environment
	// or using dependency injection, so we'll skip this for now

	t.Skip("Skipping application creation test - requires dependency injection for proper testing")
}

// TestApplicationLifecycle tests the application lifecycle
func TestApplicationLifecycle(t *testing.T) {
	// This test would require mocking all dependencies
	// In a real scenario, we would use interfaces and dependency injection

	t.Skip("Skipping application lifecycle test - requires dependency injection for proper testing")
}

// TestParseBookEdgeCases tests edge cases for parseBook
func TestParseBookEdgeCases(t *testing.T) {
	// Test with various edge cases
	edgeCases := []struct {
		name        string
		bookStr     string
		expectError bool
	}{
		{
			name:        "Single character major",
			bookStr:     "a_mxn",
			expectError: true, // 'a' is not a valid currency
		},
		{
			name:        "Single character minor",
			bookStr:     "btc_a",
			expectError: true, // 'a' is not a valid currency
		},
		{
			name:        "Numbers in currency",
			bookStr:     "btc1_mxn",
			expectError: true, // 'btc1' is not a valid currency
		},
		{
			name:        "Special characters",
			bookStr:     "btc@_mxn",
			expectError: true, // '@' is not valid in currency
		},
		{
			name:        "Whitespace",
			bookStr:     "btc _mxn",
			expectError: true, // whitespace is not valid
		},
		{
			name:        "Mixed case",
			bookStr:     "BTC_mxn",
			expectError: true, // uppercase is not valid
		},
	}

	for _, tt := range edgeCases {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseBook(tt.bookStr)
			if tt.expectError && err == nil {
				t.Errorf("Expected error for input '%s', but got none", tt.bookStr)
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error for input '%s': %v", tt.bookStr, err)
			}
		})
	}
}

// TestParseBookPerformance tests parseBook performance
func TestParseBookPerformance(t *testing.T) {
	validBooks := []string{"btc_mxn", "eth_mxn", "xrp_mxn", "ltc_mxn", "bch_mxn"}

	// Test parsing the same book multiple times
	start := time.Now()
	for i := 0; i < 1000; i++ {
		for _, bookStr := range validBooks {
			_, err := parseBook(bookStr)
			if err != nil {
				t.Errorf("Unexpected error parsing '%s': %v", bookStr, err)
			}
		}
	}
	duration := time.Since(start)

	t.Logf("Parsed %d books in %v", len(validBooks)*1000, duration)
	t.Logf("Average time per book: %v", duration/time.Duration(len(validBooks)*1000))

	// Ensure parsing is reasonably fast (less than 1ms per book)
	if duration/time.Duration(len(validBooks)*1000) > time.Millisecond {
		t.Errorf("Book parsing is too slow: %v per book", duration/time.Duration(len(validBooks)*1000))
	}
}

// TestParseBookConcurrency tests parseBook under concurrent access
func TestParseBookConcurrency(t *testing.T) {
	validBooks := []string{"btc_mxn", "eth_mxn", "xrp_mxn", "ltc_mxn", "bch_mxn"}

	// Test concurrent parsing
	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func() {
			defer func() { done <- true }()

			for j := 0; j < 100; j++ {
				for _, bookStr := range validBooks {
					_, err := parseBook(bookStr)
					if err != nil {
						t.Errorf("Unexpected error parsing '%s': %v", bookStr, err)
					}
				}
			}
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		select {
		case <-done:
			// Goroutine completed
		case <-time.After(5 * time.Second):
			t.Fatal("Timeout waiting for goroutines to complete")
		}
	}
}

// TestParseBookWithContext tests parseBook with context cancellation
func TestParseBookWithContext(t *testing.T) {
	// parseBook doesn't use context, but we can test that it's fast enough
	// to not be affected by context cancellation

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	// Start parsing in a goroutine
	resultChan := make(chan *bitso.Book, 1)
	errorChan := make(chan error, 1)

	go func() {
		book, err := parseBook("btc_mxn")
		if err != nil {
			errorChan <- err
		} else {
			resultChan <- book
		}
	}()

	// Wait for either result or context cancellation
	select {
	case book := <-resultChan:
		if book == nil {
			t.Error("Expected non-nil book")
		}
		if book.String() != "btc_mxn" {
			t.Errorf("Expected 'btc_mxn', got '%s'", book.String())
		}
	case err := <-errorChan:
		t.Errorf("Unexpected error: %v", err)
	case <-ctx.Done():
		t.Error("Context cancelled before parsing completed")
	}
}

// BenchmarkParseBook benchmarks the parseBook function
func BenchmarkParseBook(b *testing.B) {
	books := []string{"btc_mxn", "eth_mxn", "xrp_mxn", "ltc_mxn", "bch_mxn"}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			bookStr := books[i%len(books)]
			_, err := parseBook(bookStr)
			if err != nil {
				b.Errorf("Unexpected error parsing '%s': %v", bookStr, err)
			}
			i++
		}
	})
}

// BenchmarkParseBookSingle benchmarks parsing a single book
func BenchmarkParseBookSingle(b *testing.B) {
	bookStr := "btc_mxn"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := parseBook(bookStr)
		if err != nil {
			b.Errorf("Unexpected error parsing '%s': %v", bookStr, err)
		}
	}
}
