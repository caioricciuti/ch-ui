package pipelines

import (
	"fmt"
	"log/slog"
	"sync"
)

// healthThreshold is the number of consecutive failures after which a
// pipeline's last_error is set, so the UI shows that it is struggling while it
// keeps retrying.
const healthThreshold = 3

// Health counts consecutive failures of a running pipeline (source polls,
// broker connections, sink writes). A nil *Health is valid and does nothing.
type Health struct {
	mu        sync.Mutex
	failures  int
	reported  bool
	onReport  func(msg string)
	onRecover func()
}

// Fail records a failure and returns the number of consecutive failures.
// When the count reaches healthThreshold the error is reported once.
func (h *Health) Fail(where string, err error) int {
	if h == nil {
		return 0
	}
	h.mu.Lock()
	h.failures++
	n := h.failures
	report := n >= healthThreshold && !h.reported
	if report {
		h.reported = true
	}
	cb := h.onReport
	h.mu.Unlock()
	slog.Warn("Pipeline failure, retrying", "where", where, "consecutive", n, "error", err)
	if report && cb != nil {
		cb(fmt.Sprintf("%s failing (%d attempts, still retrying): %v", where, n, err))
	}
	return n
}

// OK resets the failure count, clearing a reported error.
func (h *Health) OK() {
	if h == nil {
		return
	}
	h.mu.Lock()
	wasReported := h.reported
	h.failures = 0
	h.reported = false
	cb := h.onRecover
	h.mu.Unlock()
	if wasReported && cb != nil {
		cb()
	}
}
