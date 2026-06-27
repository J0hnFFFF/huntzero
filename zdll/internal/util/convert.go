// Package util provides small, dependency-free helpers shared across zdll.
package util

// IntValue coerces v to an int when it holds a numeric type.
func IntValue(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case float32:
		return int(n)
	}
	return 0
}

// FloatValue coerces v to a float64 when it holds a numeric type.
func FloatValue(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	}
	return 0.0
}

// StringValue returns v as a string, or an empty string for non-string values.
func StringValue(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
