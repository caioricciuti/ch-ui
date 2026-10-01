// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

// Package audit forwards CH-UI audit events to external sinks (SIEM webhook,
// a file, or stdout) so security teams can ingest "who did what, when, from
// where" into their own tooling. Forwarding is best-effort and asynchronous:
// it never blocks or fails the request that produced the event, and the
// authoritative copy always remains in the SQLite audit_logs table.
package audit

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"
)

// Event is a single audit record forwarded to sinks.
type Event struct {
	Action   string `json:"action"`
	Username string `json:"username,omitempty"`
	// ClickhouseUser is the ClickHouse account the action ran as, when known.
	ClickhouseUser string `json:"ch_user,omitempty"`
	ConnectionID   string `json:"connection_id,omitempty"`
	Details        string `json:"details,omitempty"`
	IPAddress      string `json:"ip_address,omitempty"`
	Timestamp      string `json:"timestamp"`
}

// Sink delivers an event to one destination. Implementations must be safe for
// sequential calls from the forwarder worker and should return an error rather
// than panic on failure.
type Sink interface {
	Emit(e Event) error
	Name() string
}

// Forwarder fans audit events out to all configured sinks on a background
// worker. Emit is non-blocking; if the buffer is full, events are dropped with
// a warning rather than slowing down request handling.
type Forwarder struct {
	sinks []Sink
	ch    chan Event
	done  chan struct{}
	wg    sync.WaitGroup
}

// NewForwarder builds a forwarder for the given sinks and starts its worker.
// Returns nil if there are no sinks (callers should nil-check before use, or
// simply rely on Emit being a no-op on a nil forwarder).
func NewForwarder(sinks ...Sink) *Forwarder {
	if len(sinks) == 0 {
		return nil
	}
	f := &Forwarder{
		sinks: sinks,
		ch:    make(chan Event, 1024),
		done:  make(chan struct{}),
	}
	f.wg.Add(1)
	go f.run()
	names := make([]string, len(sinks))
	for i, s := range sinks {
		names[i] = s.Name()
	}
	slog.Info("Audit forwarding enabled", "sinks", names)
	return f
}

// Emit queues an event for delivery. Safe to call on a nil Forwarder.
func (f *Forwarder) Emit(e Event) {
	if f == nil {
		return
	}
	if e.Timestamp == "" {
		e.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}
	select {
	case f.ch <- e:
	default:
		slog.Warn("Audit forwarder buffer full; dropping forwarded event (SQLite copy retained)", "action", e.Action)
	}
}

// Close drains and stops the forwarder. Safe to call on a nil Forwarder.
func (f *Forwarder) Close() {
	if f == nil {
		return
	}
	close(f.done)
	f.wg.Wait()
}

func (f *Forwarder) run() {
	defer f.wg.Done()
	for {
		select {
		case e := <-f.ch:
			f.deliver(e)
		case <-f.done:
			// Drain anything already queued, then exit.
			for {
				select {
				case e := <-f.ch:
					f.deliver(e)
				default:
					return
				}
			}
		}
	}
}

func (f *Forwarder) deliver(e Event) {
	for _, s := range f.sinks {
		if err := s.Emit(e); err != nil {
			slog.Warn("Audit sink delivery failed", "sink", s.Name(), "error", err)
		}
	}
}

// ── Sinks ────────────────────────────────────────────────────────────────

// StdoutSink emits each event as a structured slog line so any log pipeline
// (Fluent Bit, Vector, Loki, CloudWatch, Datadog agent) can ingest it.
type StdoutSink struct{}

func (StdoutSink) Name() string { return "stdout" }
func (StdoutSink) Emit(e Event) error {
	slog.Info("audit",
		"action", e.Action,
		"username", e.Username,
		"ch_user", e.ClickhouseUser,
		"connection_id", e.ConnectionID,
		"ip_address", e.IPAddress,
		"details", e.Details,
		"ts", e.Timestamp,
	)
	return nil
}

// FileSink appends events as JSON lines (JSONL) to a file for log shippers.
type FileSink struct {
	mu   sync.Mutex
	path string
}

func NewFileSink(path string) *FileSink { return &FileSink{path: path} }
func (s *FileSink) Name() string        { return "file:" + s.path }
func (s *FileSink) Emit(e Event) error {
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(b, '\n'))
	return err
}

// WebhookSink POSTs each event as JSON to an HTTP endpoint (Splunk HEC,
// Datadog, Elastic, a custom collector, etc.).
type WebhookSink struct {
	url    string
	client *http.Client
}

func NewWebhookSink(url string) *WebhookSink {
	return &WebhookSink{url: url, client: &http.Client{Timeout: 10 * time.Second}}
}
func (s *WebhookSink) Name() string { return "webhook" }
func (s *WebhookSink) Emit(e Event) error {
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, s.url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "ch-ui-audit-forwarder")
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return &httpStatusError{code: resp.StatusCode}
	}
	return nil
}

type httpStatusError struct{ code int }

func (e *httpStatusError) Error() string {
	return "webhook returned status " + http.StatusText(e.code)
}
