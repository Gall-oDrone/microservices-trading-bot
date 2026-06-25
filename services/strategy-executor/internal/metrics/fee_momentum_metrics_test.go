package metrics

import (
	"strings"
	"testing"
)

func TestFeeDriftMetricsExport(t *testing.T) {
	m := NewPrometheusMetrics()
	m.RecordRealizedFeeRate("btc_mxn", "buy", "taker", 0.00741)
	m.SetAssumedFeeRate("btc_mxn", "taker", 0.0057)
	m.SetFeeDriftRatio("btc_mxn", "buy", "taker", 0.00741/0.0057)

	out := m.Export()
	for _, want := range []string{
		`strategy_executor_realized_fee_rate{book="btc_mxn",side="buy",liquidity="taker"}`,
		`strategy_executor_realized_fee_rate_observations_count{book="btc_mxn",side="buy",liquidity="taker"}`,
		`strategy_executor_assumed_fee_rate{book="btc_mxn",liquidity="taker"}`,
		`strategy_executor_fee_drift_ratio{book="btc_mxn",side="buy",liquidity="taker"}`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("export missing %q\n--- export ---\n%s", want, out)
		}
	}
}

func TestMomentumMetricsExport(t *testing.T) {
	m := NewPrometheusMetrics()
	m.IncMomentumEntrySignals("momentum_btc_mxn", "btc_mxn", "BUY")
	m.IncMomentumExitSignals("momentum_btc_mxn", "btc_mxn", "take_profit")
	m.RecordMomentumPositionHoldDuration("momentum_btc_mxn", "btc_mxn", 120)
	m.SetMomentumDailyRealizedPnL("momentum_btc_mxn", "btc_mxn", -42)
	m.SetMomentumCircuitBreakerActive("momentum_btc_mxn", "btc_mxn", true)

	out := m.Export()
	for _, want := range []string{
		`momentum_entry_signals_total{strategy="momentum_btc_mxn",book="btc_mxn",side="BUY"} 1`,
		`momentum_exit_signals_total{strategy="momentum_btc_mxn",book="btc_mxn",reason="take_profit"} 1`,
		`momentum_position_hold_duration_seconds_count{strategy="momentum_btc_mxn",book="btc_mxn"} 1`,
		`momentum_daily_realized_pnl_quote{strategy="momentum_btc_mxn",book="btc_mxn"} -42`,
		`momentum_circuit_breaker_active{strategy="momentum_btc_mxn",book="btc_mxn"} 1`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("export missing %q\n--- export ---\n%s", want, out)
		}
	}
}
