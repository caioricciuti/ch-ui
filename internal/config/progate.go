package config

import (
	"log/slog"
	"sync"
)

// ProGate lets a background Pro worker follow the license at runtime instead
// of deciding once at boot. Allow reports whether Pro work may run right now
// (ProActive or ProGrace: work keeps running through the grace window) and
// logs only when the worker pauses or resumes, never on every tick. Pausing
// deletes nothing; the worker picks up again as soon as a license is active.
type ProGate struct {
	name   string
	access func() ProAccess

	mu     sync.Mutex
	paused bool
}

// NewProGate returns a gate for the named worker. access is usually
// cfg.ProAccess.
func NewProGate(name string, access func() ProAccess) *ProGate {
	return &ProGate{name: name, access: access}
}

// Allow reports whether the worker may do Pro work now. A nil gate or a gate
// without an access function denies.
func (g *ProGate) Allow() bool {
	if g == nil || g.access == nil {
		return false
	}
	allowed := g.access() != ProNone
	g.mu.Lock()
	defer g.mu.Unlock()
	switch {
	case !allowed && !g.paused:
		g.paused = true
		slog.Info(g.name+" paused: no active Pro license. Nothing is deleted; it resumes when a license is activated", "worker", g.name)
	case allowed && g.paused:
		g.paused = false
		slog.Info(g.name+" resumed: Pro license active", "worker", g.name)
	}
	return allowed
}
