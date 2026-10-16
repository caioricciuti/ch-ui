package pipelines

import (
	"fmt"
	"log/slog"
	"sync"
	"time"
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
	lastMsg   string
	lastOK    time.Time
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
	msg := fmt.Sprintf("%s failing (%d attempts, still retrying): %v", where, n, err)
	if report {
		h.reported = true
	}
	if h.reported {
		h.lastMsg = msg
	}
	cb := h.onReport
	h.mu.Unlock()
	slog.Warn("Pipeline failure, retrying", "where", where, "consecutive", n, "error", err)
	if report && cb != nil {
		cb(msg)
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
	h.lastMsg = ""
	h.lastOK = time.Now()
	cb := h.onRecover
	h.mu.Unlock()
	if wasReported && cb != nil {
		cb()
	}
}

// State reports whether the failures were reported (healthThreshold
// consecutive ones), the latest failure message while they are, and when
// the last success was.
func (h *Health) State() (failing bool, msg string, lastOK time.Time) {
	if h == nil {
		return false, "", time.Time{}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.reported, h.lastMsg, h.lastOK
}
