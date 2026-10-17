// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package ssoidentity

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/caioricciuti/ch-ui/internal/database"
)

// sweepInterval is how often the sweep looks for per-person users that
// stopped signing in. The thresholds are days, so hourly is plenty.
const sweepInterval = time.Hour

// sweepActor is the audit log actor of what the sweep locks and drops.
const sweepActor = "system"

// Sweeper retires per-person ClickHouse users that stopped signing in,
// following each connection's LockAfterDays and DropAfterDays. It runs for
// Pro licenses only and skips connections whose tunnel is offline, since
// nothing could reach ClickHouse there anyway.
type Sweeper struct {
	DB   *database.DB
	Prov *Provisioner

	pro    func() bool
	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
	// now is replaced in tests.
	now func() time.Time
}

// SweepResult counts what one tick did.
type SweepResult struct {
	Locked  int
	Dropped int
	// Failed counts users whose lock or drop failed, or whose last sign-in
	// could not be read. They are retried on the next tick.
	Failed int
}

// NewSweeper returns a sweeper over db and prov. pro reports whether a Pro
// license allows the sweep; a tick under a free license does nothing.
func NewSweeper(db *database.DB, prov *Provisioner, pro func() bool) *Sweeper {
	return &Sweeper{DB: db, Prov: prov, pro: pro, now: time.Now}
}

// Start runs one tick right away and then one every sweepInterval until
// Stop. Calling Start twice is a no-op.
func (s *Sweeper) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	done := make(chan struct{})
	s.done = done
	go func() {
		defer close(done)
		ticker := time.NewTicker(sweepInterval)
		defer ticker.Stop()
		s.Tick(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.Tick(ctx)
			}
		}
	}()
}

// Stop cancels the running tick, if any, and waits for the goroutine.
func (s *Sweeper) Stop() {
	s.mu.Lock()
	cancel, done := s.cancel, s.done
	s.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
		s.mu.Lock()
		if s.done == done {
			s.cancel = nil
			s.done = nil
		}
		s.mu.Unlock()
	}
}

// Tick sweeps every connection in per-person mode once. A user not seen for
// DropAfterDays is dropped; otherwise one not seen for LockAfterDays and not
// locked yet is locked. A threshold of 0 is off. Failures are logged and
// counted, never stop the tick, and are retried next time.
func (s *Sweeper) Tick(ctx context.Context) SweepResult {
	var res SweepResult
	if !s.pro() {
		return res
	}
	connections, err := s.DB.GetConnections()
	if err != nil {
		slog.Warn("SSO sweep: list connections", "error", err)
		return res
	}
	now := s.now()
	for _, conn := range connections {
		if ctx.Err() != nil {
			return res
		}
		cfg, err := s.DB.GetSSOIdentityConfig(conn.ID)
		if err != nil {
			slog.Warn("SSO sweep: read identity config", "connection", conn.ID, "error", err)
			continue
		}
		if !cfg.PerPerson() || (cfg.LockAfterDays == 0 && cfg.DropAfterDays == 0) {
			continue
		}
		if !s.Prov.Gateway.IsTunnelOnline(conn.ID) {
			slog.Debug("SSO sweep: connection offline, skipped", "connection", conn.ID)
			continue
		}
		users, err := s.DB.ListSSOClickHouseUsers(conn.ID)
		if err != nil {
			slog.Warn("SSO sweep: list per-person users", "connection", conn.ID, "error", err)
			continue
		}
		for _, u := range users {
			if ctx.Err() != nil {
				return res
			}
			s.sweepUser(ctx, cfg, u, now, &res)
		}
	}
	if res.Locked > 0 || res.Dropped > 0 || res.Failed > 0 {
		slog.Info("SSO sweep finished", "locked", res.Locked, "dropped", res.Dropped, "failed", res.Failed)
	}
	return res
}

func (s *Sweeper) sweepUser(ctx context.Context, cfg database.SSOIdentityConfig, u database.SSOPersonUser, now time.Time, res *SweepResult) {
	lastSeen, err := time.Parse(time.RFC3339, u.LastSeenAt)
	if err != nil {
		slog.Warn("SSO sweep: unreadable last sign-in", "connection", u.ConnectionID, "user", u.CHUser, "last_seen", u.LastSeenAt)
		res.Failed++
		return
	}
	idle := now.Sub(lastSeen)
	switch {
	case cfg.DropAfterDays > 0 && idle > days(cfg.DropAfterDays):
		if err := s.Prov.Drop(ctx, u, sweepActor); err != nil {
			slog.Warn("SSO sweep: drop user", "connection", u.ConnectionID, "user", u.CHUser, "error", err)
			res.Failed++
			return
		}
		res.Dropped++
	case cfg.LockAfterDays > 0 && !u.Locked() && idle > days(cfg.LockAfterDays):
		if err := s.Prov.Lock(ctx, u, sweepActor); err != nil {
			slog.Warn("SSO sweep: lock user", "connection", u.ConnectionID, "user", u.CHUser, "error", err)
			res.Failed++
			return
		}
		res.Locked++
	}
}

func days(n int) time.Duration {
	return time.Duration(n) * 24 * time.Hour
}
