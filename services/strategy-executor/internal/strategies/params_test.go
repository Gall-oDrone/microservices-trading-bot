package strategies

import (
	"testing"
	"time"
)

func TestParamReader_Float64(t *testing.T) {
	p := NewParamReader(map[string]interface{}{
		"a": 1.5,
		"b": 2,
		"c": int64(3),
	})
	if got := p.Float64("a", 0); got != 1.5 {
		t.Errorf("Float64(a) = %v, want 1.5", got)
	}
	if got := p.Float64("b", 0); got != 2.0 {
		t.Errorf("Float64(b) = %v, want 2", got)
	}
	if got := p.Float64("missing", 99); got != 99 {
		t.Errorf("Float64(missing) = %v, want 99", got)
	}
}

func TestParamReader_Int(t *testing.T) {
	p := NewParamReader(map[string]interface{}{
		"n": float64(10),
		"m": 20,
	})
	if got := p.Int("n", 0); got != 10 {
		t.Errorf("Int(n) = %v, want 10", got)
	}
	if got := p.Int("missing", 5); got != 5 {
		t.Errorf("Int(missing) = %v, want 5", got)
	}
}

func TestParamReader_DurationMinutes(t *testing.T) {
	p := NewParamReader(map[string]interface{}{
		"mins": 10,
		"str":  "5m",
	})
	if got := p.DurationMinutes("mins", time.Minute); got != 10*time.Minute {
		t.Errorf("DurationMinutes(mins) = %v, want 10m", got)
	}
	if got := p.DurationMinutes("str", time.Minute); got != 5*time.Minute {
		t.Errorf("DurationMinutes(str) = %v, want 5m", got)
	}
	if got := p.DurationMinutes("missing", 3*time.Minute); got != 3*time.Minute {
		t.Errorf("DurationMinutes(missing) = %v, want 3m", got)
	}
}

func TestNewParamReader_NilMap(t *testing.T) {
	p := NewParamReader(nil)
	if got := p.Float64("x", 42); got != 42 {
		t.Errorf("nil map Float64(x) = %v, want 42", got)
	}
}
