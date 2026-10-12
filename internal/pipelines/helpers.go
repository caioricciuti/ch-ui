package pipelines

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// Retry backoff bounds. Variables so tests can shrink them.
var (
	minBackoff = time.Second
	maxBackoff = 60 * time.Second
)

// nextBackoff doubles d, capped at maxBackoff.
func nextBackoff(d time.Duration) time.Duration {
	d *= 2
	if d > maxBackoff {
		d = maxBackoff
	}
	return d
}

// sleepCtx waits for d or until ctx is done. It reports whether the full
// duration elapsed.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

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

// checkMin rejects a numeric field that is present and below min. Absent
// fields use the connector default, which is always valid.
func checkMin(fields map[string]interface{}, key string, min int, what string) error {
	if _, ok := fields[key]; !ok {
		return nil
	}
	if v := intField(fields, key, min); v < min {
		return fmt.Errorf("%s must be at least %d (got %d)", what, min, v)
	}
	return nil
}

// validateCommon checks the batching and polling fields shared by sources.
// A zero interval would panic in time.NewTicker.
func validateCommon(fields map[string]interface{}) error {
	if err := checkMin(fields, "batch_size", 1, "batch_size"); err != nil {
		return err
	}
	if err := checkMin(fields, "batch_timeout_ms", 100, "batch_timeout_ms"); err != nil {
		return err
	}
	return checkMin(fields, "poll_interval", 1, "poll_interval (seconds)")
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

// dedupToken derives an insert_deduplication_token from a stable source
// position, so the same records always produce the same token.
func dedupToken(parts ...interface{}) string {
	h := sha256.New()
	for _, p := range parts {
		fmt.Fprintf(h, "%v\x00", p)
	}
	return "chui-" + hex.EncodeToString(h.Sum(nil))[:40]
}
