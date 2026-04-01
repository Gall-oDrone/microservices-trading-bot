package manager

import (
	"encoding/json"
)

const orderFillRealizedPnLMetaKey = "order_fill_realized_pnl_mx"

func metaFloat(m map[string]interface{}, key string) float64 {
	if m == nil {
		return 0
	}
	v, ok := m[key]
	if !ok {
		return 0
	}
	switch t := v.(type) {
	case float64:
		return t
	case float32:
		return float64(t)
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case uint64:
		return float64(t)
	case json.Number:
		f, _ := t.Float64()
		return f
	default:
		return 0
	}
}

func setMetaFloat(m map[string]interface{}, key string, v float64) {
	if m == nil {
		return
	}
	m[key] = v
}
