// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti. See LICENSE.BSL.

// Package monitor runs opt-in hourly performance scans with dedicated accounts.
package monitor

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/performance"
	"github.com/caioricciuti/ch-ui/internal/safe"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

type Monitor struct {
	db      *database.DB
	gateway *tunnel.Gateway
	secret  string
	isPro   func() bool
	mu      sync.Mutex
	stopCh  chan struct{}
	doneCh  chan struct{}
}

// New requires a live license check; scheduled work never borrows human sessions.
func New(db *database.DB, gateway *tunnel.Gateway, secret string, isPro func() bool) *Monitor {
	return &Monitor{db: db, gateway: gateway, secret: secret, isPro: isPro}
}

func (m *Monitor) StartBackground() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopCh != nil {
		return
	}
	stop, done := make(chan struct{}), make(chan struct{})
	m.stopCh, m.doneCh = stop, done
	go func() {
		defer close(done)
		defer safe.Recover("performance-monitor")
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		m.tick(time.Now(), stop)
		for {
			select {
			case <-stop:
				return
			case now := <-ticker.C:
				m.tick(now, stop)
			}
		}
	}()
}

func (m *Monitor) Stop() {
	m.mu.Lock()
	stop, done := m.stopCh, m.doneCh
	if stop == nil {
		m.mu.Unlock()
		return
	}
	// Leave the channels installed until the goroutine has exited so a
	// concurrent StartBackground cannot overlap two scan loops.
	select {
	case <-stop:
	default:
		close(stop)
	}
	m.mu.Unlock()
	<-done
	m.mu.Lock()
	if m.doneCh == done {
		m.stopCh, m.doneCh = nil, nil
	}
	m.mu.Unlock()
}

func (m *Monitor) tick(now time.Time, stop <-chan struct{}) {
	if m.isPro == nil || !m.isPro() {
		return
	}
	ids, err := m.db.PerformanceMonitorConnections()
	if err != nil {
		slog.Warn("Performance: cannot load monitor configuration", "error", err)
		return
	}
	for _, id := range ids {
		select {
		case <-stop:
			return
		default:
		}
		if !m.isPro() {
			return
		}
		state, err := m.db.GetPerformanceMonitor(id)
		if err != nil || !state.Enabled {
			continue
		}
		if last, err := time.Parse(time.RFC3339Nano, state.LastScanAt); err == nil && now.Sub(last) < time.Hour {
			continue
		}
		report, err := m.collect(id, now)
		if err != nil {
			if storeErr := m.db.RecordPerformanceScan(id, nil, err.Error()); storeErr != nil {
				slog.Warn("Performance: cannot record scan failure", "connection", id, "error", storeErr)
			}
			continue
		}
		if err := m.db.RecordPerformanceScan(id, &report, ""); err != nil {
			slog.Warn("Performance: cannot store scan", "connection", id, "error", err)
		}
	}
}

func (m *Monitor) collect(id string, now time.Time) (performance.Report, error) {
	credential, err := m.db.GetBackgroundCredential(id, "performance")
	if err != nil {
		return performance.Report{}, err
	}
	if credential.Mode != "service_account" {
		return performance.Report{}, fmt.Errorf("a dedicated Performance service account is required; session borrowing is disabled")
	}
	user, password, err := m.db.BackgroundCredentials(id, "performance", m.secret)
	if err != nil {
		return performance.Report{}, err
	}
	if !m.gateway.IsTunnelOnline(id) {
		return performance.Report{}, fmt.Errorf("connection is offline")
	}
	return performance.Collect(func(sql string) ([]map[string]interface{}, error) {
		if !m.isPro() {
			return nil, fmt.Errorf("active Pro license required")
		}
		result, err := m.gateway.ExecuteQueryWithSettings(id, sql, user, password,
			map[string]string{"log_comment": performance.LogComment, "readonly": "1", "max_execution_time": "25", "max_memory_usage": "536870912", "max_threads": "2"}, 30*time.Second)
		if err != nil {
			return nil, err
		}
		if result == nil {
			return nil, fmt.Errorf("no query response")
		}
		var rows []map[string]interface{}
		if err := json.Unmarshal(result.Data, &rows); err != nil {
			return nil, fmt.Errorf("invalid query response: %w", err)
		}
		return rows, nil
	}, "", now, "24h")
}
