package collector

import (
	"encoding/json"
	"strings"

	"bitso-trading-platform/paper-trading-reporter/internal/models"
	"bitso-trading-platform/shared/pkg/bitso"
)

// FeeEstimateOptions configures optional Bitso GET /fees or static fee overrides for snapshot estimates.
type FeeEstimateOptions struct {
	BitsoBaseURL string
	BitsoKey     string
	BitsoSecret  string
	// When both OverrideBuyFeeDecimal and OverrideSellFeeDecimal are > 0, they are used as
	// global buy/sell leg decimals for every limit_profit strategy (ignores per-book /fees).
	OverrideBuyFeeDecimal  float64
	OverrideSellFeeDecimal float64
}

func enrichLimitProfitEstimates(snap *models.PaperTradingSnapshot, feeOpts FeeEstimateOptions) {
	if snap == nil || len(snap.Strategies) == 0 {
		return
	}

	var cf *bitso.CustomerFees
	if feeOpts.OverrideBuyFeeDecimal <= 0 || feeOpts.OverrideSellFeeDecimal <= 0 {
		if feeOpts.BitsoKey != "" && feeOpts.BitsoSecret != "" && feeOpts.BitsoBaseURL != "" {
			c := bitso.NewClient()
			c.SetAPIBaseURL(feeOpts.BitsoBaseURL)
			c.SetAuth(feeOpts.BitsoKey, feeOpts.BitsoSecret)
			var err error
			cf, err = c.Fees(nil)
			if err != nil {
				snap.CollectionErrors["limit_profit_pnl_estimates:bitso_fees"] = err.Error()
			}
		}
	}

	for _, raw := range snap.Strategies {
		est := estimateOneLimitProfit(raw, cf, feeOpts)
		if est.StrategyName == "" {
			continue
		}
		snap.LimitProfitPnLEstimates = append(snap.LimitProfitPnLEstimates, est)
	}
}

func estimateOneLimitProfit(raw map[string]interface{}, cf *bitso.CustomerFees, feeOpts FeeEstimateOptions) models.LimitProfitPnLEstimate {
	typ, _ := raw["type"].(string)
	if typ != "limit_profit" {
		return models.LimitProfitPnLEstimate{}
	}
	name, _ := raw["name"].(string)
	book, _ := raw["book"].(string)
	st := nestedMap(raw, "state")
	params := nestedMap(raw, "parameters")
	if name == "" {
		return models.LimitProfitPnLEstimate{}
	}

	est := models.LimitProfitPnLEstimate{
		StrategyName: name,
		Book:         book,
	}

	if !truthy(st["has_position"]) {
		est.SkipReason = "no_open_position"
		est.UseBitsoFees = truthy(params["use_bitso_fees"])
		return est
	}

	entry := toFloat(st["entry_price"])
	size := toFloat(st["position_size"])
	if entry <= 0 || size <= 0 {
		est.SkipReason = "missing_entry_or_size"
		return est
	}

	useBitso := truthy(params["use_bitso_fees"])
	est.UseBitsoFees = useBitso
	buyLiq := stringField(params, "buy_liquidity", "maker")
	sellLiq := stringField(params, "sell_liquidity", "taker")
	est.BuyLiquidity = buyLiq
	est.SellLiquidity = sellLiq

	minProfit := computeMinProfit(entry, params)
	extraFee := toFloat(params["fee"])
	est.EntryPrice = entry
	est.PositionSize = size
	est.MinProfit = minProfit
	est.ExtraFee = extraFee

	var buyR, sellR float64
	feeModel := "manual_estimate"

	if useBitso {
		ok := false
		if feeOpts.OverrideBuyFeeDecimal > 0 && feeOpts.OverrideSellFeeDecimal > 0 {
			buyR, sellR = feeOpts.OverrideBuyFeeDecimal, feeOpts.OverrideSellFeeDecimal
			ok = true
			feeModel = "bitso_api"
		} else if cf != nil {
			f := bitso.LookupFeeByBook(cf, book)
			if f != nil {
				buyR = bitso.FeeDecimalForLiquidity(f, buyLiq)
				sellR = bitso.FeeDecimalForLiquidity(f, sellLiq)
				ok = true
				feeModel = "bitso_api"
			}
		}
		if !ok {
			feeModel = "manual_estimate"
			buyR, sellR = 0, 0
		}
	}

	est.FeeModel = feeModel
	if feeModel == "bitso_api" {
		if sellR >= 1 {
			est.SkipReason = "invalid_sell_fee_rate"
			return est
		}
		be := bitso.MinExitPriceAfterRoundTrip(entry, buyR, sellR)
		th := be + minProfit + extraFee
		est.BuyFeeRateDecimal = buyR
		est.SellFeeRateDecimal = sellR
		est.BreakevenExitLast = be
		est.TakeProfitThresholdLast = th
		est.EstGrossPnlQuoteAtThresh = (th - entry) * size
		est.EstNetPnlQuoteAtThresh = bitso.NetQuotePnLPerBase(entry, th, buyR, sellR) * size
		return est
	}

	manual := exitFeeAddon(entry, params)
	th := entry + minProfit + manual
	est.TakeProfitThresholdLast = th
	est.EstGrossPnlQuoteAtThresh = (th - entry) * size
	est.EstNetPnlQuoteAtThresh = est.EstGrossPnlQuoteAtThresh
	return est
}

func nestedMap(m map[string]interface{}, key string) map[string]interface{} {
	if m == nil {
		return nil
	}
	v, ok := m[key].(map[string]interface{})
	if !ok {
		return nil
	}
	return v
}

func truthy(v interface{}) bool {
	b, ok := v.(bool)
	return ok && b
}

func toFloat(v interface{}) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case json.Number:
		f, err := x.Float64()
		if err != nil {
			return 0
		}
		return f
	default:
		return 0
	}
}

func stringField(params map[string]interface{}, key, def string) string {
	if params == nil {
		return def
	}
	s, _ := params[key].(string)
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return def
	}
	return s
}

func computeMinProfit(entry float64, params map[string]interface{}) float64 {
	bps := toFloat(params["min_profit_bps"])
	if bps > 0 && entry > 0 {
		return entry * bps / 10000.0
	}
	return toFloat(params["min_profit"])
}

func exitFeeAddon(entry float64, params map[string]interface{}) float64 {
	addon := toFloat(params["fee"])
	feeBps := toFloat(params["fee_bps"])
	if feeBps > 0 && entry > 0 {
		addon += entry * 2.0 * feeBps / 10000.0
	}
	return addon
}
