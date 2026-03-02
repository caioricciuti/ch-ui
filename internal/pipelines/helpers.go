package pipelines

// intField extracts an int from a config map with a default fallback.
func intField(fields map[string]interface{}, key string, def int) int {
	v, ok := fields[key]
	if !ok {
		return def
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	default:
		return def
	}
}

// stringField extracts a string from a config map with a default fallback.
func stringField(fields map[string]interface{}, key, def string) string {
	v, ok := fields[key]
	if !ok {
		return def
	}
	s, ok := v.(string)
	if !ok {
		return def
	}
	return s
}

// boolField extracts a bool from a config map with a default fallback.
func boolField(fields map[string]interface{}, key string, def bool) bool {
	v, ok := fields[key]
	if !ok {
		return def
	}
	b, ok := v.(bool)
	if !ok {
		return def
	}
	return b
}
