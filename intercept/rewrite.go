package intercept

import (
	"encoding/json"
	"reflect"
)

// NarrowingEdit recognizes only bounded numeric reductions. Free-form commands,
// paths, targets, credentials and arbitrary flags need explicit human review.
func NarrowingEdit(before, after json.RawMessage) bool {
	var a, b map[string]any
	if json.Unmarshal(before, &a) != nil || json.Unmarshal(after, &b) != nil || a == nil || b == nil {
		return false
	}
	if len(a) != len(b) {
		return false
	}
	for key, old := range a {
		value, ok := b[key]
		if !ok {
			return false
		}
		if reflect.DeepEqual(old, value) {
			continue
		}
		switch key {
		case "timeout", "timeout_seconds", "max_time", "max_rate", "rate", "threads", "concurrency", "limit":
		default:
			return false
		}
		x, xok := old.(float64)
		y, yok := value.(float64)
		if !xok || !yok || x <= 0 || y <= 0 || y > x {
			return false
		}
	}
	return true
}
