package safe

import "sync"

// Changes remembers a condition per key (usually a connection ID) so a
// background worker that checks it on every tick logs it once when it
// starts, changes or clears, not on every tick. The zero value is ready to
// use.
//
//	if w.noCreds.Set(connID, err.Error()) {
//	    slog.Warn("No credentials", "connection", connID, "error", err)
//	}
type Changes struct {
	mu    sync.Mutex
	state map[string]string
}

// Set records the condition of key, "" for none, and reports whether it
// differs from the one recorded before. Clearing a key that has no
// condition is not a change.
func (c *Changes) Set(key, condition string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state[key] == condition {
		return false
	}
	if condition == "" {
		delete(c.state, key)
		return true
	}
	if c.state == nil {
		c.state = map[string]string{}
	}
	c.state[key] = condition
	return true
}
