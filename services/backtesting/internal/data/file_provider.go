package data

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
	"bitso-trading-platform/shared/pkg/bitso"
)

// FileProvider implements DataProvider using local files
type FileProvider struct {
	basePath string
	logger   logger.Logger
}

// NewFileProvider creates a new file-based data provider
func NewFileProvider(basePath string, log logger.Logger) *FileProvider {
	return &FileProvider{
		basePath: basePath,
		logger:   log,
	}
}

// LoadHistoricalData loads historical data from files
func (p *FileProvider) LoadHistoricalData(ctx context.Context, req *DataRequest) ([]models.MarketEvent, error) {
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}
	
	p.logger.Info("Loading historical data from files", map[string]interface{}{
		"book":      req.Book,
		"base_path": p.basePath,
	})
	
	events := make([]models.MarketEvent, 0)
	
	for _, eventType := range req.EventTypes {
		// Build file path based on event type
		fileName := p.buildFileName(req.Book, eventType, req.StartDate, req.EndDate)
		filePath := filepath.Join(p.basePath, fileName)
		
		p.logger.Debug("Reading data file", map[string]interface{}{
			"file": filePath,
		})
		
		// Check if file exists
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			p.logger.Warn("Data file not found", map[string]interface{}{
				"file": filePath,
			})
			continue
		}
		
		// Read and parse file
		fileEvents, err := p.readDataFile(filePath, eventType)
		if err != nil {
			return nil, fmt.Errorf("failed to read file %s: %w", filePath, err)
		}
		
		// Filter by date range
		for _, event := range fileEvents {
			if event.Timestamp.After(req.StartDate) && event.Timestamp.Before(req.EndDate) {
				events = append(events, event)
			}
		}
	}
	
	// Sort by timestamp
	sortEventsByTimestamp(events)
	
	// Apply limit if specified
	if req.Limit > 0 && len(events) > req.Limit {
		events = events[:req.Limit]
	}
	
	p.logger.Info("Historical data loaded from files", map[string]interface{}{
		"count": len(events),
	})
	
	return events, nil
}

// StreamData streams historical data from files
func (p *FileProvider) StreamData(ctx context.Context, req *DataRequest) (<-chan models.MarketEvent, error) {
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}
	
	eventChan := make(chan models.MarketEvent, 100)
	
	go func() {
		defer close(eventChan)
		
		// Load all data
		events, err := p.LoadHistoricalData(ctx, req)
		if err != nil {
			p.logger.Error("Failed to load data for streaming", map[string]interface{}{"error": err})
			return
		}
		
		// Stream events
		for _, event := range events {
			select {
			case <-ctx.Done():
				return
			case eventChan <- event:
			}
		}
	}()
	
	return eventChan, nil
}

// GetDataRange returns the available date range for a book
func (p *FileProvider) GetDataRange(ctx context.Context, book string) (*DateRange, error) {
	// Scan directory for files matching the book
	pattern := filepath.Join(p.basePath, fmt.Sprintf("%s_*.json", book))
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to scan directory: %w", err)
	}
	
	if len(files) == 0 {
		return nil, fmt.Errorf("no data files found for book: %s", book)
	}
	
	// Parse dates from file names
	var firstDate, lastDate time.Time
	for _, file := range files {
		date, err := p.parseDateFromFileName(filepath.Base(file))
		if err != nil {
			continue
		}
		
		if firstDate.IsZero() || date.Before(firstDate) {
			firstDate = date
		}
		if lastDate.IsZero() || date.After(lastDate) {
			lastDate = date
		}
	}
	
	return &DateRange{
		FirstDate: firstDate,
		LastDate:  lastDate,
	}, nil
}

// readDataFile reads and parses a data file
func (p *FileProvider) readDataFile(filePath string, eventType models.MarketEventType) ([]models.MarketEvent, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	
	events := make([]models.MarketEvent, 0)
	
	switch eventType {
	case models.EventTypeTrade:
		var trades []bitso.Trade
		if err := json.Unmarshal(data, &trades); err != nil {
			return nil, fmt.Errorf("failed to parse trades: %w", err)
		}
		for _, trade := range trades {
			events = append(events, *models.NewTradeEvent(&trade))
		}
		
	case models.EventTypeTicker:
		var tickers []bitso.Ticker
		if err := json.Unmarshal(data, &tickers); err != nil {
			return nil, fmt.Errorf("failed to parse tickers: %w", err)
		}
		for _, ticker := range tickers {
			events = append(events, *models.NewTickerEvent(&ticker))
		}
	}
	
	return events, nil
}

// buildFileName builds a file name based on the request parameters
func (p *FileProvider) buildFileName(book string, eventType models.MarketEventType, startDate, endDate time.Time) string {
	// Format: {book}_{type}_{startdate}_{enddate}.json
	// Example: btc_mxn_trades_20240101_20241231.json
	return fmt.Sprintf("%s_%s_%s_%s.json",
		book,
		eventType,
		startDate.Format("20060102"),
		endDate.Format("20060102"))
}

// parseDateFromFileName extracts date from file name
func (p *FileProvider) parseDateFromFileName(fileName string) (time.Time, error) {
	// Remove extension
	name := strings.TrimSuffix(fileName, ".json")
	
	// Split by underscore
	parts := strings.Split(name, "_")
	if len(parts) < 4 {
		return time.Time{}, fmt.Errorf("invalid file name format: %s", fileName)
	}
	
	// Parse date (e.g., "20240101")
	dateStr := parts[len(parts)-2] // Second to last part is start date
	return time.Parse("20060102", dateStr)
}

// Close closes the provider
func (p *FileProvider) Close() error {
	return nil
}

// sortEventsByTimestamp sorts events by timestamp (helper function)
func sortEventsByTimestamp(events []models.MarketEvent) {
	n := len(events)
	for i := 0; i < n-1; i++ {
		for j := 0; j < n-i-1; j++ {
			if events[j].Timestamp.After(events[j+1].Timestamp) {
				events[j], events[j+1] = events[j+1], events[j]
			}
		}
	}
}

