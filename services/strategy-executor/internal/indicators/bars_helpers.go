package indicators

// barCloses returns close prices from OHLCV bars in chronological order.
func barCloses(bars []OHLCV) []float64 {
	closes := make([]float64, len(bars))
	for i, b := range bars {
		closes[i] = b.Close
	}
	return closes
}

// minBarsRequired returns the minimum 1m bar count needed for the configured periods.
func minBarsRequired(cfg *ServiceConfig) int {
	need := cfg.SMAPeriod
	if cfg.BollingerPeriod > need {
		need = cfg.BollingerPeriod
	}
	if cfg.EMAPeriod > need {
		need = cfg.EMAPeriod
	}
	// RSI needs period+1 price points.
	if cfg.RSIPeriod+1 > need {
		need = cfg.RSIPeriod + 1
	}
	// ATR needs period+1 bars.
	if cfg.ATRPeriod+1 > need {
		need = cfg.ATRPeriod + 1
	}
	return need
}

// barFetchLimit returns how many bars to request from market-data for one compute cycle.
func barFetchLimit(cfg *ServiceConfig) int {
	limit := minBarsRequired(cfg) + cfg.BarLimitBuffer
	if limit < 30 {
		limit = 30
	}
	if limit > 500 {
		limit = 500
	}
	return limit
}
