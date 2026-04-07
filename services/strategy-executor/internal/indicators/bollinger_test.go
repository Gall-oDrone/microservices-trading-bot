package indicators

import (
	"math"
	"testing"
)

func TestNewBollinger(t *testing.T) {
	bb := NewBollinger(20, 2.0)

	if bb == nil {
		t.Fatal("NewBollinger() returned nil")
	}

	if bb.Name() != "bollinger" {
		t.Errorf("Expected name 'bollinger', got '%s'", bb.Name())
	}

	if bb.Period() != 20 {
		t.Errorf("Expected period 20, got %d", bb.Period())
	}
}

func TestBollinger_ComputeBands(t *testing.T) {
	bb := NewBollinger(5, 2.0)

	prices := []float64{100, 102, 101, 103, 102}

	bands, err := bb.ComputeBands(prices)
	if err != nil {
		t.Fatalf("ComputeBands() error: %v", err)
	}

	expectedMiddle := 101.6
	if math.Abs(bands.Middle-expectedMiddle) > 0.01 {
		t.Errorf("Expected middle band ~%.2f, got %.2f", expectedMiddle, bands.Middle)
	}

	if bands.Upper <= bands.Middle {
		t.Errorf("Upper band (%.2f) should be > middle band (%.2f)", bands.Upper, bands.Middle)
	}

	if bands.Lower >= bands.Middle {
		t.Errorf("Lower band (%.2f) should be < middle band (%.2f)", bands.Lower, bands.Middle)
	}

	if bands.StdDev <= 0 {
		t.Errorf("StdDev should be > 0, got %.4f", bands.StdDev)
	}

	expectedWidth := 2 * 2.0 * bands.StdDev
	actualWidth := bands.Upper - bands.Lower
	if math.Abs(actualWidth-expectedWidth) > 0.0001 {
		t.Errorf("Band width mismatch: expected %.4f, got %.4f", expectedWidth, actualWidth)
	}
}

func TestBollinger_ComputeBands_InsufficientData(t *testing.T) {
	bb := NewBollinger(20, 2.0)

	prices := []float64{100, 101, 102}

	_, err := bb.ComputeBands(prices)
	if err == nil {
		t.Error("Expected error for insufficient data")
	}
}

func TestBollinger_Compute(t *testing.T) {
	bb := NewBollinger(5, 2.0)

	prices := []float64{100, 102, 101, 103, 102}

	middle, err := bb.Compute(prices)
	if err != nil {
		t.Fatalf("Compute() error: %v", err)
	}

	if middle <= 0 {
		t.Errorf("Expected positive middle value, got %.2f", middle)
	}
}

func TestBollinger_ComputeFromTrades(t *testing.T) {
	bb := NewBollinger(5, 2.0)

	trades := []Trade{
		{Price: 100},
		{Price: 102},
		{Price: 101},
		{Price: 103},
		{Price: 102},
	}

	middle, err := bb.ComputeFromTrades(trades)
	if err != nil {
		t.Fatalf("ComputeFromTrades() error: %v", err)
	}

	if middle <= 0 {
		t.Errorf("Expected positive middle value, got %.2f", middle)
	}
}

func TestBollinger_ComputeBandsFromTrades(t *testing.T) {
	bb := NewBollinger(5, 2.0)

	trades := []Trade{
		{Price: 100},
		{Price: 102},
		{Price: 101},
		{Price: 103},
		{Price: 102},
	}

	bands, err := bb.ComputeBandsFromTrades(trades)
	if err != nil {
		t.Fatalf("ComputeBandsFromTrades() error: %v", err)
	}

	if bands.Upper <= bands.Middle || bands.Lower >= bands.Middle {
		t.Error("Band ordering incorrect")
	}
}

func TestBollinger_ComputeFromBars(t *testing.T) {
	bb := NewBollinger(5, 2.0)

	bars := []OHLCV{
		{Close: 100},
		{Close: 102},
		{Close: 101},
		{Close: 103},
		{Close: 102},
	}

	middle, err := bb.ComputeFromBars(bars)
	if err != nil {
		t.Fatalf("ComputeFromBars() error: %v", err)
	}

	if middle <= 0 {
		t.Errorf("Expected positive middle value, got %.2f", middle)
	}
}

func TestBollinger_GetBandWidth(t *testing.T) {
	bb := NewBollinger(20, 2.0)

	bands := &BollingerBands{
		Upper:  110,
		Middle: 100,
		Lower:  90,
	}

	width := bb.GetBandWidth(bands)
	expectedWidth := 20.0

	if math.Abs(width-expectedWidth) > 0.0001 {
		t.Errorf("Expected band width %.4f, got %.4f", expectedWidth, width)
	}

	bandsZeroMiddle := &BollingerBands{
		Upper:  10,
		Middle: 0,
		Lower:  -10,
	}

	widthZero := bb.GetBandWidth(bandsZeroMiddle)
	if widthZero != 0 {
		t.Errorf("Expected 0 width for zero middle, got %.4f", widthZero)
	}
}

func TestBollinger_GetPercentB(t *testing.T) {
	bb := NewBollinger(20, 2.0)

	bands := &BollingerBands{
		Upper:  110,
		Middle: 100,
		Lower:  90,
	}

	tests := []struct {
		price    float64
		expected float64
		desc     string
	}{
		{100, 0.5, "at middle"},
		{110, 1.0, "at upper"},
		{90, 0.0, "at lower"},
		{120, 1.5, "above upper"},
		{80, -0.5, "below lower"},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			percentB := bb.GetPercentB(tt.price, bands)
			if math.Abs(percentB-tt.expected) > 0.0001 {
				t.Errorf("For price %.2f, expected %%B %.4f, got %.4f", tt.price, tt.expected, percentB)
			}
		})
	}

	bandsNoWidth := &BollingerBands{
		Upper:  100,
		Middle: 100,
		Lower:  100,
	}
	percentB := bb.GetPercentB(100, bandsNoWidth)
	if percentB != 0.5 {
		t.Errorf("Expected 0.5 for zero-width bands, got %.4f", percentB)
	}
}

func TestBollinger_IsPriceAboveUpper(t *testing.T) {
	bb := NewBollinger(20, 2.0)

	bands := &BollingerBands{
		Upper:  110,
		Middle: 100,
		Lower:  90,
	}

	if !bb.IsPriceAboveUpper(111, bands) {
		t.Error("Expected true for price 111 > upper 110")
	}

	if bb.IsPriceAboveUpper(109, bands) {
		t.Error("Expected false for price 109 < upper 110")
	}

	if bb.IsPriceAboveUpper(110, bands) {
		t.Error("Expected false for price 110 == upper 110")
	}
}

func TestBollinger_IsPriceBelowLower(t *testing.T) {
	bb := NewBollinger(20, 2.0)

	bands := &BollingerBands{
		Upper:  110,
		Middle: 100,
		Lower:  90,
	}

	if !bb.IsPriceBelowLower(89, bands) {
		t.Error("Expected true for price 89 < lower 90")
	}

	if bb.IsPriceBelowLower(91, bands) {
		t.Error("Expected false for price 91 > lower 90")
	}

	if bb.IsPriceBelowLower(90, bands) {
		t.Error("Expected false for price 90 == lower 90")
	}
}

func TestBollinger_ComputeBandsSeries(t *testing.T) {
	bb := NewBollinger(3, 2.0)

	prices := []float64{100, 101, 102, 103, 104}

	series, err := bb.ComputeBandsSeries(prices)
	if err != nil {
		t.Fatalf("ComputeBandsSeries() error: %v", err)
	}

	expectedLen := 3
	if len(series) != expectedLen {
		t.Errorf("Expected series length %d, got %d", expectedLen, len(series))
	}

	for i, bands := range series {
		if bands.Upper <= bands.Middle || bands.Lower >= bands.Middle {
			t.Errorf("Series[%d]: Band ordering incorrect", i)
		}
	}
}

func TestBollinger_ComputeBandsSeries_InsufficientData(t *testing.T) {
	bb := NewBollinger(10, 2.0)

	prices := []float64{100, 101, 102}

	_, err := bb.ComputeBandsSeries(prices)
	if err == nil {
		t.Error("Expected error for insufficient data")
	}
}

func TestBollinger_DifferentStdDevMultipliers(t *testing.T) {
	prices := []float64{100, 102, 101, 103, 102, 104, 103, 105, 104, 106}

	bb1 := NewBollinger(10, 1.0)
	bb2 := NewBollinger(10, 2.0)
	bb3 := NewBollinger(10, 3.0)

	bands1, _ := bb1.ComputeBands(prices)
	bands2, _ := bb2.ComputeBands(prices)
	bands3, _ := bb3.ComputeBands(prices)

	if bands1.Middle != bands2.Middle || bands2.Middle != bands3.Middle {
		t.Error("Middle bands should be equal regardless of stdDev multiplier")
	}

	width1 := bands1.Upper - bands1.Lower
	width2 := bands2.Upper - bands2.Lower
	width3 := bands3.Upper - bands3.Lower

	if width1 >= width2 || width2 >= width3 {
		t.Errorf("Band widths should increase with stdDev multiplier: %.4f < %.4f < %.4f", width1, width2, width3)
	}

	if math.Abs(width2/width1-2.0) > 0.0001 {
		t.Errorf("Width ratio 2x/1x should be 2.0, got %.4f", width2/width1)
	}
}
