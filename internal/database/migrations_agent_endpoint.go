package database

import "fmt"

// migrateAgentEndpoint adds the ClickHouse endpoint an agent connection's
// agent last reported (see chendpoint.Normalize). The gateway compares it on
// every agent sign-in and clears the old server's state when it changes.
// NULL means no agent has reported one yet: never connected, or an agent
// older than the field.
//
// server_reset_at is when the connection last moved to a different
// ClickHouse server (URL edit or agent report) and the old server's state
// was cleared, so the connections list can say so.
//
// Idempotent: safe to run on every start.
func (db *DB) migrateAgentEndpoint() error {
	for _, col := range []string{"agent_endpoint", "server_reset_at"} {
		if err := db.ensureColumn("connections", col, "TEXT"); err != nil {
			return fmt.Errorf("agent endpoint migration: %w", err)
		}
	}
	return nil
}
