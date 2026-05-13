package models

// LimitProfitPnLEstimate is a paper-trading snapshot add-on for open limit_profit positions.
// Threshold math matches strategy-executor limit_profit (exit ref "last"): take-profit when
// last >= breakeven_after_round_trip + min_profit + extra_fee (Fee).
type LimitProfitPnLEstimate struct {
	StrategyName string `json:"strategy_name"`
	Book         string `json:"book"`

	// FeeModel is bitso_api when rates came from Bitso GET /fees (or override decimals),
	// or manual_estimate when use_bitso_fees is false or Bitso rates were unavailable.
	FeeModel string `json:"fee_model"`
	// SkipReason is set when we cannot produce threshold / PnL numbers (e.g. no position).
	SkipReason string `json:"skip_reason,omitempty"`

	UseBitsoFees bool `json:"use_bitso_fees"`

	EntryPrice   float64 `json:"entry_price,omitempty"`
	PositionSize float64 `json:"position_size,omitempty"`
	MinProfit    float64 `json:"min_profit_effective,omitempty"`
	ExtraFee     float64 `json:"extra_fee_quote,omitempty"`

	BuyLiquidity  string `json:"buy_liquidity,omitempty"`
	SellLiquidity string `json:"sell_liquidity,omitempty"`

	BuyFeeRateDecimal  float64 `json:"buy_fee_rate_decimal,omitempty"`
	SellFeeRateDecimal float64 `json:"sell_fee_rate_decimal,omitempty"`

	BreakevenExitLast        float64 `json:"breakeven_exit_last,omitempty"`
	TakeProfitThresholdLast  float64 `json:"take_profit_threshold_last,omitempty"`
	EstGrossPnlQuoteAtThresh float64 `json:"est_gross_pnl_quote_at_threshold,omitempty"`
	EstNetPnlQuoteAtThresh   float64 `json:"est_net_pnl_quote_at_threshold,omitempty"`
}
