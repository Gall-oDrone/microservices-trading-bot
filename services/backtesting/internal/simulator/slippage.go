package simulator

import (
	sharedModels "bitso-trading-platform/shared/pkg/models"
)

// SlippageModel defines the interface for slippage calculation
type SlippageModel interface {
	Calculate(order *sharedModels.Order, marketPrice float64) float64
}

// NoSlippage implements zero slippage
type NoSlippage struct{}

// FixedSlippage implements fixed slippage in currency units
type FixedSlippage struct {
	Value float64
}

// PercentageSlippage implements percentage-based slippage
type PercentageSlippage struct {
	Percentage float64
}

// VolumeBasedSlippage implements volume-dependent slippage
type VolumeBasedSlippage struct {
	BasePercentage float64
}

// NewSlippageModel creates a slippage model based on type and value
func NewSlippageModel(modelType string, value float64) SlippageModel {
	switch modelType {
	case "none":
		return &NoSlippage{}
	case "fixed":
		return &FixedSlippage{Value: value}
	case "percentage":
		return &PercentageSlippage{Percentage: value}
	case "volume":
		return &VolumeBasedSlippage{BasePercentage: value}
	default:
		return &NoSlippage{}
	}
}

// Calculate implementations

// Calculate for NoSlippage (always returns 0)
func (n *NoSlippage) Calculate(order *sharedModels.Order, marketPrice float64) float64 {
	return 0
}

// Calculate for FixedSlippage (returns fixed value)
func (f *FixedSlippage) Calculate(order *sharedModels.Order, marketPrice float64) float64 {
	return f.Value
}

// Calculate for PercentageSlippage (percentage of market price)
func (p *PercentageSlippage) Calculate(order *sharedModels.Order, marketPrice float64) float64 {
	return marketPrice * p.Percentage
}

// Calculate for VolumeBasedSlippage (scales with order size)
func (v *VolumeBasedSlippage) Calculate(order *sharedModels.Order, marketPrice float64) float64 {
	// Slippage increases with order size
	// Base slippage * (1 + log(amount))
	// For simplicity, use linear scaling for now
	volumeFactor := 1.0
	if order.Amount > 1.0 {
		volumeFactor = 1.0 + (order.Amount * 0.1)
	}
	
	return marketPrice * v.BasePercentage * volumeFactor
}

