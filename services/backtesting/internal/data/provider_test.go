package data

import (
	"testing"
	"time"

	"bitso-trading-platform/backtesting/internal/models"
)

func TestDataRequestValidation(t *testing.T) {
	now := time.Now()
	pastDate := now.AddDate(0, -1, 0)
	
	tests := []struct {
		name    string
		req     *DataRequest
		wantErr bool
	}{
		{
			name: "valid request",
			req: &DataRequest{
				Book:       "btc_mxn",
				StartDate:  pastDate,
				EndDate:    now,
				EventTypes: []models.MarketEventType{models.EventTypeTrade},
			},
			wantErr: false,
		},
		{
			name: "empty book",
			req: &DataRequest{
				Book:       "",
				StartDate:  pastDate,
				EndDate:    now,
				EventTypes: []models.MarketEventType{models.EventTypeTrade},
			},
			wantErr: true,
		},
		{
			name: "end before start",
			req: &DataRequest{
				Book:       "btc_mxn",
				StartDate:  now,
				EndDate:    pastDate,
				EventTypes: []models.MarketEventType{models.EventTypeTrade},
			},
			wantErr: true,
		},
		{
			name: "no event types",
			req: &DataRequest{
				Book:       "btc_mxn",
				StartDate:  pastDate,
				EndDate:    now,
				EventTypes: []models.MarketEventType{},
			},
			wantErr: true,
		},
		{
			name: "negative limit",
			req: &DataRequest{
				Book:       "btc_mxn",
				StartDate:  pastDate,
				EndDate:    now,
				EventTypes: []models.MarketEventType{models.EventTypeTrade},
				Limit:      -100,
			},
			wantErr: true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDataRequestMethods(t *testing.T) {
	now := time.Now()
	startDate := now.AddDate(0, 0, -7) // 7 days ago
	endDate := now
	
	req := NewDataRequest("btc_mxn", startDate, endDate)
	
	// Test NewDataRequest defaults
	if req.Book != "btc_mxn" {
		t.Errorf("Expected book 'btc_mxn', got '%s'", req.Book)
	}
	if len(req.EventTypes) != 1 || req.EventTypes[0] != models.EventTypeTrade {
		t.Error("Expected default event type to be trades")
	}
	if req.Granularity != "tick" {
		t.Errorf("Expected default granularity 'tick', got '%s'", req.Granularity)
	}
	
	// Test GetDuration
	duration := req.GetDuration()
	expectedDuration := 7 * 24 * time.Hour
	if duration < expectedDuration-time.Hour || duration > expectedDuration+time.Hour {
		t.Errorf("Expected duration ~7 days, got %v", duration)
	}
	
	// Test GetDays
	days := req.GetDays()
	if days != 7 {
		t.Errorf("Expected 7 days, got %d", days)
	}
	
	// Test builder methods
	req.WithEventTypes(models.EventTypeTrade, models.EventTypeTicker)
	if len(req.EventTypes) != 2 {
		t.Error("WithEventTypes failed")
	}
	
	req.WithGranularity("1m")
	if req.Granularity != "1m" {
		t.Error("WithGranularity failed")
	}
	
	req.WithLimit(1000)
	if req.Limit != 1000 {
		t.Error("WithLimit failed")
	}
	
	// Test String method
	str := req.String()
	if str == "" {
		t.Error("Expected non-empty string representation")
	}
}

func TestFileProviderBasics(t *testing.T) {
	// Create temp directory for testing
	tempDir := t.TempDir()
	
	provider := NewFileProvider(tempDir, nil)
	if provider == nil {
		t.Fatal("NewFileProvider returned nil")
	}
	
	if provider.basePath != tempDir {
		t.Errorf("Expected basePath '%s', got '%s'", tempDir, provider.basePath)
	}
	
	// Test Close
	if err := provider.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

func TestFileProviderBuildFileName(t *testing.T) {
	provider := NewFileProvider("/tmp", nil)
	
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC)
	
	fileName := provider.buildFileName("btc_mxn", models.EventTypeTrade, startDate, endDate)
	expected := "btc_mxn_trade_20240101_20241231.json"
	
	if fileName != expected {
		t.Errorf("Expected file name '%s', got '%s'", expected, fileName)
	}
}

func TestFileProviderParseDateFromFileName(t *testing.T) {
	provider := NewFileProvider("/tmp", nil)
	
	tests := []struct {
		name     string
		fileName string
		wantErr  bool
	}{
		{
			name:     "valid file name",
			fileName: "btc_mxn_trade_20240101_20241231.json",
			wantErr:  false,
		},
		{
			name:     "invalid format",
			fileName: "invalid.json",
			wantErr:  true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := provider.parseDateFromFileName(tt.fileName)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseDateFromFileName() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSortEventsByTimestamp(t *testing.T) {
	now := time.Now()
	
	events := []models.MarketEvent{
		{Timestamp: now.Add(2 * time.Hour)},
		{Timestamp: now},
		{Timestamp: now.Add(1 * time.Hour)},
	}
	
	sortEventsByTimestamp(events)
	
	// Verify sorted order
	if !events[0].Timestamp.Before(events[1].Timestamp) {
		t.Error("Events not sorted correctly")
	}
	if !events[1].Timestamp.Before(events[2].Timestamp) {
		t.Error("Events not sorted correctly")
	}
}

