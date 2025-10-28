package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"bitso-trading-platform/backtesting/internal/logger"
	"bitso-trading-platform/backtesting/internal/models"
)

// FileStorage implements ResultStorage using file system
type FileStorage struct {
	basePath string
	logger   logger.Logger
}

// NewFileStorage creates a new file-based storage instance
func NewFileStorage(basePath string, log logger.Logger) *FileStorage {
	// Ensure base path exists
	if err := os.MkdirAll(basePath, 0755); err != nil {
		if log != nil {
			log.Error("Failed to create storage directory", map[string]interface{}{
				"path":  basePath,
				"error": err,
			})
		}
	}

	return &FileStorage{
		basePath: basePath,
		logger:   log,
	}
}

// Save saves a backtest result to a file
func (s *FileStorage) Save(ctx context.Context, result *models.BacktestResult) error {
	// Generate file path
	filePath := s.generatePath(result.BacktestID)

	// Ensure directory exists
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Serialize result
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal result: %w", err)
	}

	// Write to file
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	if s.logger != nil {
		s.logger.Debug("Saved backtest result to file", map[string]interface{}{
			"backtest_id": result.BacktestID,
			"path":        filePath,
		})
	}

	return nil
}

// Get retrieves a backtest result from a file
func (s *FileStorage) Get(ctx context.Context, backtestID string) (*models.BacktestResult, error) {
	filePath := s.generatePath(backtestID)

	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil, fmt.Errorf("backtest not found: %s", backtestID)
	}

	// Read file
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	// Deserialize
	var result models.BacktestResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal result: %w", err)
	}

	return &result, nil
}

// List lists backtest results with optional filters
func (s *FileStorage) List(ctx context.Context, filters *ListFilters) ([]*models.BacktestResult, error) {
	if filters == nil {
		filters = NewListFilters()
	}
	filters.Validate()

	// Scan directory for result files
	pattern := filepath.Join(s.basePath, "**", "*.json")
	files, err := filepath.Glob(pattern)
	if err != nil {
		// Try simpler pattern
		pattern = filepath.Join(s.basePath, "*.json")
		files, err = filepath.Glob(pattern)
		if err != nil {
			return nil, fmt.Errorf("failed to scan directory: %w", err)
		}
	}

	// Load and filter results
	results := make([]*models.BacktestResult, 0)
	for _, file := range files {
		// Extract backtest ID from file name
		backtestID := s.extractBacktestID(file)

		// Load result
		result, err := s.Get(ctx, backtestID)
		if err != nil {
			if s.logger != nil {
				s.logger.Warn("Failed to load result", map[string]interface{}{
					"file":  file,
					"error": err,
				})
			}
			continue
		}

		// Apply filters
		if s.matchesFilters(result, filters) {
			results = append(results, result)
		}
	}

	// Sort results
	s.sortResults(results, filters.SortBy, filters.SortOrder)

	// Apply pagination
	start := filters.Offset
	if start > len(results) {
		start = len(results)
	}

	end := start + filters.Limit
	if end > len(results) {
		end = len(results)
	}

	return results[start:end], nil
}

// Delete deletes a backtest result file
func (s *FileStorage) Delete(ctx context.Context, backtestID string) error {
	filePath := s.generatePath(backtestID)

	if err := os.Remove(filePath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("backtest not found: %s", backtestID)
		}
		return fmt.Errorf("failed to delete file: %w", err)
	}

	if s.logger != nil {
		s.logger.Info("Deleted backtest result file", map[string]interface{}{
			"backtest_id": backtestID,
			"path":        filePath,
		})
	}

	return nil
}

// UpdateStatus updates the status and progress of a backtest
func (s *FileStorage) UpdateStatus(ctx context.Context, backtestID string, status string, progress float64) error {
	// Get existing result
	result, err := s.Get(ctx, backtestID)
	if err != nil {
		return fmt.Errorf("failed to get result: %w", err)
	}

	// Update fields
	result.Status = status
	result.Progress = progress

	// Save updated result
	return s.Save(ctx, result)
}

// Close closes the storage (no-op for file storage)
func (s *FileStorage) Close() error {
	return nil
}

// Helper methods

// generatePath generates a file path for a backtest result
// Format: {basePath}/YYYY/MM/{backtestID}.json
func (s *FileStorage) generatePath(backtestID string) string {
	now := time.Now()
	year := fmt.Sprintf("%04d", now.Year())
	month := fmt.Sprintf("%02d", now.Month())

	return filepath.Join(s.basePath, year, month, fmt.Sprintf("%s.json", backtestID))
}

// extractBacktestID extracts the backtest ID from a file path
func (s *FileStorage) extractBacktestID(filePath string) string {
	base := filepath.Base(filePath)
	return strings.TrimSuffix(base, ".json")
}

// matchesFilters checks if a result matches the given filters
func (s *FileStorage) matchesFilters(result *models.BacktestResult, filters *ListFilters) bool {
	// Status filter
	if filters.Status != "" && result.Status != filters.Status {
		return false
	}

	// Date range filters
	if filters.StartDate != nil && result.StartedAt.Before(*filters.StartDate) {
		return false
	}
	if filters.EndDate != nil && result.StartedAt.After(*filters.EndDate) {
		return false
	}

	return true
}

// sortResults sorts results by the specified field
func (s *FileStorage) sortResults(results []*models.BacktestResult, sortBy, sortOrder string) {
	// Simple bubble sort
	n := len(results)
	for i := 0; i < n-1; i++ {
		for j := 0; j < n-i-1; j++ {
			shouldSwap := false

			switch sortBy {
			case "created_at":
				if sortOrder == "asc" {
					shouldSwap = results[j].StartedAt.After(results[j+1].StartedAt)
				} else {
					shouldSwap = results[j].StartedAt.Before(results[j+1].StartedAt)
				}
			default:
				shouldSwap = results[j].StartedAt.Before(results[j+1].StartedAt)
			}

			if shouldSwap {
				results[j], results[j+1] = results[j+1], results[j]
			}
		}
	}
}
