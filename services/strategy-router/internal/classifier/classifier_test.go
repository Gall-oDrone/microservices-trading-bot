package classifier

import "testing"

func defaultThresholds() Thresholds {
	return Thresholds{
		ATRHighVolPct: 1.5,
		ATRLowVolPct:  0.30,
		RSIOverbought: 70,
		RSIOversold:   30,
		BBUpper:       0.85,
		BBLower:       0.15,
		EMADistEntry:  0.10,
	}
}

func TestClassify_HighVol(t *testing.T) {
	// ATR is 2% of price → high_vol regardless of other inputs.
	s := Snapshot{
		Price:    1_000_000,
		ATR:      20_000, // 2.0%
		EMA:      999_000,
		RSI:      55,
		BBUpper:  1_010_000,
		BBLower:  990_000,
		BBMiddle: 1_000_000,
	}
	d := Classify(s, defaultThresholds())
	if d.Regime != "high_vol" {
		t.Fatalf("regime = %q, want high_vol (atr_pct=%.3f)", d.Regime, d.ATRPct)
	}
}

func TestClassify_LowVolRange(t *testing.T) {
	// Tight ATR (0.1%) and price in the middle of the band → low_vol_range.
	s := Snapshot{
		Price:    1_000_000,
		ATR:      1_000, // 0.1%
		EMA:      1_000_000,
		RSI:      50,
		BBUpper:  1_005_000,
		BBLower:  995_000,
		BBMiddle: 1_000_000,
	}
	d := Classify(s, defaultThresholds())
	if d.Regime != "low_vol_range" {
		t.Fatalf("regime = %q, want low_vol_range (atr_pct=%.3f pb=%.3f)", d.Regime, d.ATRPct, d.BollingerPB)
	}
}

func TestClassify_TrendingUp(t *testing.T) {
	// Price 0.5% above EMA, RSI 60 (below overbought 70).
	s := Snapshot{
		Price:    1_000_000,
		ATR:      6_000, // 0.6% — between low and high vol
		EMA:      995_000,
		RSI:      60,
		BBUpper:  1_010_000,
		BBLower:  990_000,
		BBMiddle: 1_000_000,
	}
	d := Classify(s, defaultThresholds())
	if d.Regime != "trending_up" {
		t.Fatalf("regime = %q, want trending_up (ema_dist=%.3f)", d.Regime, d.EMADistPct)
	}
}

func TestClassify_TrendingDown(t *testing.T) {
	// Price 0.5% below EMA, RSI 40 (above oversold 30).
	s := Snapshot{
		Price:    1_000_000,
		ATR:      6_000,
		EMA:      1_005_000,
		RSI:      40,
		BBUpper:  1_010_000,
		BBLower:  990_000,
		BBMiddle: 1_000_000,
	}
	d := Classify(s, defaultThresholds())
	if d.Regime != "trending_down" {
		t.Fatalf("regime = %q, want trending_down (ema_dist=%.3f)", d.Regime, d.EMADistPct)
	}
}

func TestClassify_NeutralWhenRSIExhausted(t *testing.T) {
	// Trending up by EMA distance but RSI overbought → falls through to neutral.
	s := Snapshot{
		Price:    1_000_000,
		ATR:      6_000,
		EMA:      995_000,
		RSI:      80, // above 70 → blocks trending_up
		BBUpper:  1_010_000,
		BBLower:  990_000,
		BBMiddle: 1_000_000,
	}
	d := Classify(s, defaultThresholds())
	if d.Regime != "neutral" {
		t.Fatalf("regime = %q, want neutral when RSI exhausted (rsi=%.1f)", d.Regime, d.RSI)
	}
}

func TestClassify_MissingPriceIsNeutral(t *testing.T) {
	d := Classify(Snapshot{}, defaultThresholds())
	if d.Regime != "neutral" {
		t.Fatalf("empty snapshot regime = %q, want neutral", d.Regime)
	}
}

func TestClassify_BollingerOutsideBandIsNotRange(t *testing.T) {
	// Tight ATR but price hugging upper band → not low_vol_range.
	s := Snapshot{
		Price:    1_010_000,
		ATR:      1_000, // 0.1%
		EMA:      1_000_000,
		RSI:      60,
		BBUpper:  1_010_000,
		BBLower:  990_000,
		BBMiddle: 1_000_000,
	}
	d := Classify(s, defaultThresholds())
	if d.Regime == "low_vol_range" {
		t.Fatalf("regime should not be low_vol_range when %%B=%f hits band edge", d.BollingerPB)
	}
}
