package strategies

import (
	"time"
)

// ParamReader reads strategy parameters from a map (e.g. config.Parameters).
// Used for config-driven and intraday-tunable strategies.
type ParamReader struct {
	m map[string]interface{}
}

// NewParamReader returns a reader for the given parameters map.
// Nil or empty map is allowed; Get methods return defaults.
func NewParamReader(params map[string]interface{}) *ParamReader {
	if params == nil {
		params = make(map[string]interface{})
	}
	return &ParamReader{m: params}
}

// Float64 returns a float64 parameter. key is the parameter name (e.g. "profit_target_pct").
// If missing or invalid, returns defaultVal.
func (r *ParamReader) Float64(key string, defaultVal float64) float64 {
	v, ok := r.m[key]
	if !ok {
		return defaultVal
	}
	switch val := v.(type) {
	case float64:
		return val
	case int:
		return float64(val)
	case int64:
		return float64(val)
	default:
		return defaultVal
	}
}

// Int returns an int parameter. If missing or invalid, returns defaultVal.
func (r *ParamReader) Int(key string, defaultVal int) int {
	v, ok := r.m[key]
	if !ok {
		return defaultVal
	}
	switch val := v.(type) {
	case float64:
		return int(val)
	case int:
		return val
	case int64:
		return int(val)
	default:
		return defaultVal
	}
}

// DurationMinutes interprets a parameter as minutes and returns time.Duration.
// Key can be numeric (minutes) or a string like "5m", "1h". If missing or invalid, returns defaultVal.
func (r *ParamReader) DurationMinutes(key string, defaultVal time.Duration) time.Duration {
	v, ok := r.m[key]
	if !ok {
		return defaultVal
	}
	switch val := v.(type) {
	case float64:
		return time.Duration(val) * time.Minute
	case int:
		return time.Duration(val) * time.Minute
	case int64:
		return time.Duration(val) * time.Minute
	case string:
		d, err := time.ParseDuration(val)
		if err != nil {
			return defaultVal
		}
		return d
	default:
		return defaultVal
	}
}
