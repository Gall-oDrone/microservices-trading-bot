package storage

import (
	"context"
	"strings"
	"testing"

	"bitso-trading-platform/backtesting/internal/models"
)

func TestListFilters(t *testing.T) {
	// Test NewListFilters defaults
	filters := NewListFilters()

	if filters.Limit != 20 {
		t.Errorf("Expected default limit 20, got %d", filters.Limit)
	}
	if filters.Offset != 0 {
		t.Errorf("Expected default offset 0, got %d", filters.Offset)
	}
	if filters.SortBy != "created_at" {
		t.Errorf("Expected default sortBy 'created_at', got '%s'", filters.SortBy)
	}
	if filters.SortOrder != "desc" {
		t.Errorf("Expected default sortOrder 'desc', got '%s'", filters.SortOrder)
	}
}

func TestListFiltersBuilders(t *testing.T) {
	filters := NewListFilters()

	// Test builder methods
	filters.WithStatus("completed")
	if filters.Status != "completed" {
		t.Error("WithStatus failed")
	}

	filters.WithStrategy("basic")
	if filters.Strategy != "basic" {
		t.Error("WithStrategy failed")
	}

	filters.WithBook("btc_mxn")
	if filters.Book != "btc_mxn" {
		t.Error("WithBook failed")
	}

	filters.WithLimit(100)
	if filters.Limit != 100 {
		t.Error("WithLimit failed")
	}

	filters.WithOffset(20)
	if filters.Offset != 20 {
		t.Error("WithOffset failed")
	}
}

func TestListFiltersValidation(t *testing.T) {
	tests := []struct {
		name    string
		filters *ListFilters
		want    *ListFilters
	}{
		{
			name: "negative limit",
			filters: &ListFilters{
				Limit:     -10,
				Offset:    0,
				SortOrder: "desc",
			},
			want: &ListFilters{
				Limit:     20, // Should be set to default
				Offset:    0,
				SortOrder: "desc",
			},
		},
		{
			name: "limit too high",
			filters: &ListFilters{
				Limit:     2000,
				Offset:    0,
				SortOrder: "desc",
			},
			want: &ListFilters{
				Limit:     1000, // Should be capped at 1000
				Offset:    0,
				SortOrder: "desc",
			},
		},
		{
			name: "negative offset",
			filters: &ListFilters{
				Limit:     20,
				Offset:    -5,
				SortOrder: "desc",
			},
			want: &ListFilters{
				Limit:     20,
				Offset:    0, // Should be set to 0
				SortOrder: "desc",
			},
		},
		{
			name: "invalid sort order",
			filters: &ListFilters{
				Limit:     20,
				Offset:    0,
				SortOrder: "invalid",
			},
			want: &ListFilters{
				Limit:     20,
				Offset:    0,
				SortOrder: "desc", // Should be set to default
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.filters.Validate()

			if tt.filters.Limit != tt.want.Limit {
				t.Errorf("Limit = %d, want %d", tt.filters.Limit, tt.want.Limit)
			}
			if tt.filters.Offset != tt.want.Offset {
				t.Errorf("Offset = %d, want %d", tt.filters.Offset, tt.want.Offset)
			}
			if tt.filters.SortOrder != tt.want.SortOrder {
				t.Errorf("SortOrder = %s, want %s", tt.filters.SortOrder, tt.want.SortOrder)
			}
		})
	}
}

func TestFileStorageBasics(t *testing.T) {
	// Create temp directory for testing
	tempDir := t.TempDir()

	storage := NewFileStorage(tempDir, nil)
	if storage == nil {
		t.Fatal("NewFileStorage returned nil")
	}

	if storage.basePath != tempDir {
		t.Errorf("Expected basePath '%s', got '%s'", tempDir, storage.basePath)
	}

	// Test Close
	if err := storage.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

func TestFileStorageSaveAndGet(t *testing.T) {
	// Create temp directory
	tempDir := t.TempDir()
	storage := NewFileStorage(tempDir, nil)
	ctx := context.Background()

	// Create test result
	result := models.NewBacktestResult("bt-test-123", "cfg-test-456")
	result.Status = "completed"
	result.SetSummary(&models.PerformanceSummary{
		TotalReturn: 5000,
		WinRate:     0.65,
	})

	// Save
	if err := storage.Save(ctx, result); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Get
	retrieved, err := storage.Get(ctx, "bt-test-123")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	// Verify
	if retrieved.BacktestID != result.BacktestID {
		t.Errorf("BacktestID mismatch: expected %s, got %s", result.BacktestID, retrieved.BacktestID)
	}
	if retrieved.Status != result.Status {
		t.Errorf("Status mismatch: expected %s, got %s", result.Status, retrieved.Status)
	}
	if retrieved.Summary.TotalReturn != 5000 {
		t.Errorf("Summary mismatch: expected 5000, got %f", retrieved.Summary.TotalReturn)
	}
}

func TestFileStorageDelete(t *testing.T) {
	tempDir := t.TempDir()
	storage := NewFileStorage(tempDir, nil)
	ctx := context.Background()

	// Create and save result
	result := models.NewBacktestResult("bt-delete-test", "cfg-test")
	if err := storage.Save(ctx, result); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Verify it exists
	if _, err := storage.Get(ctx, "bt-delete-test"); err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	// Delete
	if err := storage.Delete(ctx, "bt-delete-test"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	// Verify it's deleted
	if _, err := storage.Get(ctx, "bt-delete-test"); err == nil {
		t.Error("Expected error when getting deleted result")
	}
}

func TestFileStorageUpdateStatus(t *testing.T) {
	tempDir := t.TempDir()
	storage := NewFileStorage(tempDir, nil)
	ctx := context.Background()

	// Create and save result
	result := models.NewBacktestResult("bt-update-test", "cfg-test")
	result.Status = "running"
	result.Progress = 0.0
	if err := storage.Save(ctx, result); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Update status
	if err := storage.UpdateStatus(ctx, "bt-update-test", "completed", 1.0); err != nil {
		t.Fatalf("UpdateStatus() error = %v", err)
	}

	// Verify update
	updated, err := storage.Get(ctx, "bt-update-test")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if updated.Status != "completed" {
		t.Errorf("Status not updated: expected 'completed', got '%s'", updated.Status)
	}
	if updated.Progress != 1.0 {
		t.Errorf("Progress not updated: expected 1.0, got %f", updated.Progress)
	}
}

func TestFileStorageExtractBacktestID(t *testing.T) {
	storage := NewFileStorage("/tmp", nil)

	tests := []struct {
		name     string
		filePath string
		want     string
	}{
		{
			name:     "simple path",
			filePath: "/tmp/bt-123.json",
			want:     "bt-123",
		},
		{
			name:     "nested path",
			filePath: "/tmp/2024/10/bt-456.json",
			want:     "bt-456",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := storage.extractBacktestID(tt.filePath)
			if got != tt.want {
				t.Errorf("extractBacktestID() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestFileStorageGeneratePath(t *testing.T) {
	storage := NewFileStorage("/var/lib/backtesting", nil)

	path := storage.generatePath("bt-test-123")

	// Should contain base path and backtest ID
	if !strings.Contains(path, "/var/lib/backtesting") {
		t.Error("Path should contain base path")
	}
	if !strings.Contains(path, "bt-test-123.json") {
		t.Error("Path should contain backtest ID")
	}
}
