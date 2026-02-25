package database

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
)

func (db *DB) runMigrations() error {
	slog.Info("Running database migrations...")

	stmts := []string{
		// Installation settings (key-value store)
		`CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`,

		// Connections (replaces tunnel_connections, no org_id)
		`CREATE TABLE IF NOT EXISTS connections (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			tunnel_token TEXT UNIQUE NOT NULL,
			is_embedded INTEGER DEFAULT 0,
			status TEXT DEFAULT 'disconnected',
			last_seen_at TEXT,
			host_info TEXT,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_conn_token ON connections(tunnel_token)`,

		// Sessions (no org_id)
		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			connection_id TEXT NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
			clickhouse_user TEXT NOT NULL,
			encrypted_password TEXT NOT NULL,
			token TEXT UNIQUE NOT NULL,
			expires_at TEXT NOT NULL,
			user_role TEXT DEFAULT 'viewer',
			created_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_session_token ON sessions(token)`,
		`CREATE INDEX IF NOT EXISTS idx_session_conn ON sessions(connection_id)`,
		`CREATE INDEX IF NOT EXISTS idx_session_expires ON sessions(expires_at)`,

		// Rate limits
		`CREATE TABLE IF NOT EXISTS rate_limits (
			identifier TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			attempts INTEGER NOT NULL DEFAULT 0,
			first_attempt_at TEXT NOT NULL,
			locked_until TEXT,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_rate_limit_type ON rate_limits(type)`,
		`CREATE INDEX IF NOT EXISTS idx_rate_limit_locked ON rate_limits(locked_until)`,

		// User role overrides
		`CREATE TABLE IF NOT EXISTS user_roles (
			username TEXT PRIMARY KEY,
			role TEXT NOT NULL DEFAULT 'viewer',
			created_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`,

		// Saved queries (was in ClickHouse, now SQLite)
		`CREATE TABLE IF NOT EXISTS saved_queries (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT,
			query TEXT NOT NULL,
			connection_id TEXT REFERENCES connections(id) ON DELETE SET NULL,
			created_by TEXT,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`,

		// Dashboards (was in ClickHouse, now SQLite)
		`CREATE TABLE IF NOT EXISTS dashboards (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT,
			created_by TEXT,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`,

		// Dashboard panels
		`CREATE TABLE IF NOT EXISTS panels (
			id TEXT PRIMARY KEY,
			dashboard_id TEXT NOT NULL REFERENCES dashboards(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			panel_type TEXT NOT NULL DEFAULT 'table',
			query TEXT NOT NULL,
			connection_id TEXT REFERENCES connections(id) ON DELETE SET NULL,
			config TEXT DEFAULT '{}',
			layout_x INTEGER DEFAULT 0,
			layout_y INTEGER DEFAULT 0,
			layout_w INTEGER DEFAULT 6,
			layout_h INTEGER DEFAULT 4,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_panel_dashboard ON panels(dashboard_id)`,

		// Schedules (was in ClickHouse, now SQLite)
		`CREATE TABLE IF NOT EXISTS schedules (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			saved_query_id TEXT REFERENCES saved_queries(id) ON DELETE CASCADE,
			connection_id TEXT REFERENCES connections(id) ON DELETE SET NULL,
			cron TEXT NOT NULL,
			timezone TEXT DEFAULT 'UTC',
			enabled INTEGER DEFAULT 1,
			timeout_ms INTEGER DEFAULT 60000,
			last_run_at TEXT,
			next_run_at TEXT,
			last_status TEXT,
			last_error TEXT,
			created_by TEXT,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`,

		// Schedule runs
		`CREATE TABLE IF NOT EXISTS schedule_runs (
			id TEXT PRIMARY KEY,
			schedule_id TEXT NOT NULL REFERENCES schedules(id) ON DELETE CASCADE,
			started_at TEXT NOT NULL,
			finished_at TEXT,
			status TEXT NOT NULL,
			rows_affected INTEGER DEFAULT 0,
			elapsed_ms INTEGER DEFAULT 0,
			error TEXT,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sched_run_schedule ON schedule_runs(schedule_id)`,

		// Audit logs (was stub, now real)
		`CREATE TABLE IF NOT EXISTS audit_logs (
			id TEXT PRIMARY KEY,
			action TEXT NOT NULL,
			username TEXT,
			connection_id TEXT,
			details TEXT,
			ip_address TEXT,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_created ON audit_logs(created_at)`,

		// Brain providers (admin-managed)
		`CREATE TABLE IF NOT EXISTS brain_providers (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			kind TEXT NOT NULL,
			base_url TEXT,
			encrypted_api_key TEXT,
			is_active INTEGER DEFAULT 1,
			is_default INTEGER DEFAULT 0,
			created_by TEXT,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_brain_provider_active ON brain_providers(is_active)`,

		// Brain models by provider
		`CREATE TABLE IF NOT EXISTS brain_models (
			id TEXT PRIMARY KEY,
			provider_id TEXT NOT NULL REFERENCES brain_providers(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			display_name TEXT,
			is_active INTEGER DEFAULT 1,
			is_default INTEGER DEFAULT 0,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(provider_id, name)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_brain_model_provider ON brain_models(provider_id)`,
		`CREATE INDEX IF NOT EXISTS idx_brain_model_active ON brain_models(is_active)`,

		// Brain chats
		`CREATE TABLE IF NOT EXISTS brain_chats (
			id TEXT PRIMARY KEY,
			connection_id TEXT NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
			username TEXT NOT NULL,
			title TEXT NOT NULL,
			provider_id TEXT REFERENCES brain_providers(id) ON DELETE SET NULL,
			model_id TEXT REFERENCES brain_models(id) ON DELETE SET NULL,
			archived INTEGER DEFAULT 0,
			last_message_at TEXT,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_brain_chat_user ON brain_chats(username, connection_id)`,
		`CREATE INDEX IF NOT EXISTS idx_brain_chat_lastmsg ON brain_chats(last_message_at)`,

		// Brain messages
		`CREATE TABLE IF NOT EXISTS brain_messages (
			id TEXT PRIMARY KEY,
			chat_id TEXT NOT NULL REFERENCES brain_chats(id) ON DELETE CASCADE,
			role TEXT NOT NULL,
			content TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'complete',
			error TEXT,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_brain_msg_chat ON brain_messages(chat_id, created_at)`,

		// Brain artifacts
		`CREATE TABLE IF NOT EXISTS brain_artifacts (
			id TEXT PRIMARY KEY,
			chat_id TEXT NOT NULL REFERENCES brain_chats(id) ON DELETE CASCADE,
			message_id TEXT REFERENCES brain_messages(id) ON DELETE SET NULL,
			artifact_type TEXT NOT NULL,
			title TEXT NOT NULL,
			content TEXT NOT NULL,
			created_by TEXT,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_brain_artifact_chat ON brain_artifacts(chat_id, created_at)`,

		// Brain tool call traces
		`CREATE TABLE IF NOT EXISTS brain_tool_calls (
			id TEXT PRIMARY KEY,
			chat_id TEXT NOT NULL REFERENCES brain_chats(id) ON DELETE CASCADE,
			message_id TEXT NOT NULL REFERENCES brain_messages(id) ON DELETE CASCADE,
			tool_name TEXT NOT NULL,
			input_json TEXT NOT NULL,
			output_json TEXT NOT NULL,
			status TEXT NOT NULL,
			error TEXT,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_brain_tool_chat ON brain_tool_calls(chat_id, created_at)`,

		// Brain skills (admin-managed system prompts)
		`CREATE TABLE IF NOT EXISTS brain_skills (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			content TEXT NOT NULL,
			is_active INTEGER DEFAULT 1,
			is_default INTEGER DEFAULT 0,
			created_by TEXT,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_brain_skill_active ON brain_skills(is_active)`,

		// ══════════════════════════════════════════════════════════════
		// Governance tables (Pro feature)
		// ══════════════════════════════════════════════════════════════

		// Governance sync state (watermark tracking per connection)
		`CREATE TABLE IF NOT EXISTS gov_sync_state (
			id TEXT PRIMARY KEY,
			connection_id TEXT NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
			sync_type TEXT NOT NULL,
			last_synced_at TEXT,
			watermark TEXT,
			status TEXT DEFAULT 'idle',
			last_error TEXT,
			row_count INTEGER DEFAULT 0,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(connection_id, sync_type)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_gov_sync_conn ON gov_sync_state(connection_id)`,

		// Governance databases
		`CREATE TABLE IF NOT EXISTS gov_databases (
			id TEXT PRIMARY KEY,
			connection_id TEXT NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			engine TEXT,
			first_seen TEXT NOT NULL,
			last_updated TEXT NOT NULL,
			is_deleted INTEGER DEFAULT 0,
			UNIQUE(connection_id, name)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_gov_db_conn ON gov_databases(connection_id)`,

		// Governance tables
		`CREATE TABLE IF NOT EXISTS gov_tables (
			id TEXT PRIMARY KEY,
			connection_id TEXT NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
			database_name TEXT NOT NULL,
			table_name TEXT NOT NULL,
			engine TEXT,
			table_uuid TEXT,
			total_rows INTEGER DEFAULT 0,
			total_bytes INTEGER DEFAULT 0,
			partition_count INTEGER DEFAULT 0,
			first_seen TEXT NOT NULL,
			last_updated TEXT NOT NULL,
			is_deleted INTEGER DEFAULT 0,
			UNIQUE(connection_id, database_name, table_name)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_gov_tbl_conn ON gov_tables(connection_id)`,
		`CREATE INDEX IF NOT EXISTS idx_gov_tbl_db ON gov_tables(connection_id, database_name)`,

		// Governance columns
		`CREATE TABLE IF NOT EXISTS gov_columns (
			id TEXT PRIMARY KEY,
			connection_id TEXT NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
			database_name TEXT NOT NULL,
			table_name TEXT NOT NULL,
			column_name TEXT NOT NULL,
			column_type TEXT NOT NULL,
			column_position INTEGER DEFAULT 0,
			default_kind TEXT,
			default_expression TEXT,
			comment TEXT,
			first_seen TEXT NOT NULL,
			last_updated TEXT NOT NULL,
			is_deleted INTEGER DEFAULT 0,
			UNIQUE(connection_id, database_name, table_name, column_name)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_gov_col_conn ON gov_columns(connection_id)`,
		`CREATE INDEX IF NOT EXISTS idx_gov_col_tbl ON gov_columns(connection_id, database_name, table_name)`,

		// Governance schema changes
		`CREATE TABLE IF NOT EXISTS gov_schema_changes (
			id TEXT PRIMARY KEY,
			connection_id TEXT NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
			change_type TEXT NOT NULL,
			database_name TEXT NOT NULL,
			table_name TEXT,
			column_name TEXT,
			old_value TEXT,
			new_value TEXT,
			detected_at TEXT NOT NULL,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_gov_schema_conn ON gov_schema_changes(connection_id)`,
		`CREATE INDEX IF NOT EXISTS idx_gov_schema_time ON gov_schema_changes(connection_id, detected_at)`,

		// Governance query log
		`CREATE TABLE IF NOT EXISTS gov_query_log (
			id TEXT PRIMARY KEY,
			connection_id TEXT NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
			query_id TEXT NOT NULL,
			ch_user TEXT NOT NULL,
			query_text TEXT NOT NULL,
			normalized_hash TEXT,
			query_kind TEXT,
			event_time TEXT NOT NULL,
			duration_ms INTEGER DEFAULT 0,
			read_rows INTEGER DEFAULT 0,
			read_bytes INTEGER DEFAULT 0,
			result_rows INTEGER DEFAULT 0,
			written_rows INTEGER DEFAULT 0,
			written_bytes INTEGER DEFAULT 0,
			memory_usage INTEGER DEFAULT 0,
			tables_used TEXT,
			is_error INTEGER DEFAULT 0,
			error_message TEXT,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(connection_id, query_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_gov_qlog_conn ON gov_query_log(connection_id)`,
		`CREATE INDEX IF NOT EXISTS idx_gov_qlog_time ON gov_query_log(connection_id, event_time)`,
		`CREATE INDEX IF NOT EXISTS idx_gov_qlog_user ON gov_query_log(connection_id, ch_user)`,
		`CREATE INDEX IF NOT EXISTS idx_gov_qlog_hash ON gov_query_log(connection_id, normalized_hash)`,

		// Governance lineage edges
		`CREATE TABLE IF NOT EXISTS gov_lineage_edges (
			id TEXT PRIMARY KEY,
			connection_id TEXT NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
			source_database TEXT NOT NULL,
			source_table TEXT NOT NULL,
			target_database TEXT NOT NULL,
			target_table TEXT NOT NULL,
			query_id TEXT,
			ch_user TEXT,
			edge_type TEXT NOT NULL,
			detected_at TEXT NOT NULL,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_gov_lineage_conn ON gov_lineage_edges(connection_id)`,
		`CREATE INDEX IF NOT EXISTS idx_gov_lineage_src ON gov_lineage_edges(connection_id, source_database, source_table)`,
		`CREATE INDEX IF NOT EXISTS idx_gov_lineage_tgt ON gov_lineage_edges(connection_id, target_database, target_table)`,

		// Governance sensitivity tags
		`CREATE TABLE IF NOT EXISTS gov_tags (
			id TEXT PRIMARY KEY,
			connection_id TEXT NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
			object_type TEXT NOT NULL,
			database_name TEXT NOT NULL,
			table_name TEXT NOT NULL,
			column_name TEXT NOT NULL DEFAULT '',
			tag TEXT NOT NULL,
			tagged_by TEXT,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(connection_id, object_type, database_name, table_name, column_name, tag)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_gov_tag_conn ON gov_tags(connection_id)`,
		`CREATE INDEX IF NOT EXISTS idx_gov_tag_obj ON gov_tags(connection_id, database_name, table_name)`,

		// Governance ClickHouse users
		`CREATE TABLE IF NOT EXISTS gov_ch_users (
			id TEXT PRIMARY KEY,
			connection_id TEXT NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			auth_type TEXT,
			host_ip TEXT,
			default_roles TEXT,
			first_seen TEXT NOT NULL,
			last_updated TEXT NOT NULL,
			UNIQUE(connection_id, name)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_gov_chuser_conn ON gov_ch_users(connection_id)`,

		// Governance ClickHouse roles
		`CREATE TABLE IF NOT EXISTS gov_ch_roles (
			id TEXT PRIMARY KEY,
			connection_id TEXT NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			first_seen TEXT NOT NULL,
			last_updated TEXT NOT NULL,
			UNIQUE(connection_id, name)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_gov_chrole_conn ON gov_ch_roles(connection_id)`,

		// Governance role grants
		`CREATE TABLE IF NOT EXISTS gov_role_grants (
			id TEXT PRIMARY KEY,
			connection_id TEXT NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
			user_name TEXT NOT NULL,
			granted_role_name TEXT NOT NULL,
			is_default INTEGER DEFAULT 0,
			with_admin_option INTEGER DEFAULT 0,
			first_seen TEXT NOT NULL,
			last_updated TEXT NOT NULL,
			UNIQUE(connection_id, user_name, granted_role_name)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_gov_rolegrant_conn ON gov_role_grants(connection_id)`,
		`CREATE INDEX IF NOT EXISTS idx_gov_rolegrant_user ON gov_role_grants(connection_id, user_name)`,
		`CREATE INDEX IF NOT EXISTS idx_gov_rolegrant_role ON gov_role_grants(connection_id, granted_role_name)`,

		// Governance grants
		`CREATE TABLE IF NOT EXISTS gov_grants (
			id TEXT PRIMARY KEY,
			connection_id TEXT NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
			user_name TEXT,
			role_name TEXT,
			access_type TEXT NOT NULL,
			grant_database TEXT,
			grant_table TEXT,
			grant_column TEXT,
			is_partial_revoke INTEGER DEFAULT 0,
			grant_option INTEGER DEFAULT 0,
			first_seen TEXT NOT NULL,
			last_updated TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_gov_grant_conn ON gov_grants(connection_id)`,
		`CREATE INDEX IF NOT EXISTS idx_gov_grant_user ON gov_grants(connection_id, user_name)`,
		`CREATE INDEX IF NOT EXISTS idx_gov_grant_role ON gov_grants(connection_id, role_name)`,

		// Governance access matrix (materialized)
		`CREATE TABLE IF NOT EXISTS gov_access_matrix (
			id TEXT PRIMARY KEY,
			connection_id TEXT NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
			user_name TEXT NOT NULL,
			role_name TEXT,
			database_name TEXT,
			table_name TEXT,
			privilege TEXT NOT NULL,
			is_direct_grant INTEGER DEFAULT 0,
			last_query_time TEXT,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_gov_matrix_conn ON gov_access_matrix(connection_id)`,
		`CREATE INDEX IF NOT EXISTS idx_gov_matrix_user ON gov_access_matrix(connection_id, user_name)`,

		// Governance policies
		`CREATE TABLE IF NOT EXISTS gov_policies (
			id TEXT PRIMARY KEY,
			connection_id TEXT NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			description TEXT,
			object_type TEXT NOT NULL,
			object_database TEXT,
			object_table TEXT,
			object_column TEXT,
			required_role TEXT,
			severity TEXT DEFAULT 'warn',
			enforcement_mode TEXT NOT NULL DEFAULT 'warn',
			enabled INTEGER DEFAULT 1,
			created_by TEXT,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_gov_policy_conn ON gov_policies(connection_id)`,

		// Governance policy violations
		`CREATE TABLE IF NOT EXISTS gov_policy_violations (
			id TEXT PRIMARY KEY,
			connection_id TEXT NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
			policy_id TEXT NOT NULL REFERENCES gov_policies(id) ON DELETE CASCADE,
			query_log_id TEXT,
			ch_user TEXT NOT NULL,
			violation_detail TEXT,
			severity TEXT NOT NULL,
			detection_phase TEXT NOT NULL DEFAULT 'post_exec',
			request_endpoint TEXT,
			detected_at TEXT NOT NULL,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_gov_violation_conn ON gov_policy_violations(connection_id)`,
		`CREATE INDEX IF NOT EXISTS idx_gov_violation_policy ON gov_policy_violations(policy_id)`,
		`CREATE INDEX IF NOT EXISTS idx_gov_violation_time ON gov_policy_violations(connection_id, detected_at)`,

		// Governance object notes/comments (table/column level)
		`CREATE TABLE IF NOT EXISTS gov_object_comments (
			id TEXT PRIMARY KEY,
			connection_id TEXT NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
			object_type TEXT NOT NULL,
			database_name TEXT NOT NULL,
			table_name TEXT NOT NULL,
			column_name TEXT NOT NULL DEFAULT '',
			comment_text TEXT NOT NULL,
			created_by TEXT,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_gov_comment_obj ON gov_object_comments(connection_id, object_type, database_name, table_name, column_name, created_at)`,

		// Governance incidents (Collibra-style workflow, simplified)
		`CREATE TABLE IF NOT EXISTS gov_incidents (
			id TEXT PRIMARY KEY,
			connection_id TEXT NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
			source_type TEXT NOT NULL DEFAULT 'manual',
			source_ref TEXT,
			dedupe_key TEXT,
			title TEXT NOT NULL,
			severity TEXT NOT NULL DEFAULT 'warn',
			status TEXT NOT NULL DEFAULT 'open',
			assignee TEXT,
			details TEXT,
			resolution_note TEXT,
			occurrence_count INTEGER NOT NULL DEFAULT 1,
			first_seen_at TEXT NOT NULL,
			last_seen_at TEXT NOT NULL,
			resolved_at TEXT,
			created_by TEXT,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_gov_incident_conn_status ON gov_incidents(connection_id, status, severity, last_seen_at)`,
		`CREATE INDEX IF NOT EXISTS idx_gov_incident_source ON gov_incidents(connection_id, source_type, source_ref)`,
		`CREATE INDEX IF NOT EXISTS idx_gov_incident_dedupe ON gov_incidents(connection_id, dedupe_key, status)`,

		`CREATE TABLE IF NOT EXISTS gov_incident_comments (
			id TEXT PRIMARY KEY,
			incident_id TEXT NOT NULL REFERENCES gov_incidents(id) ON DELETE CASCADE,
			comment_text TEXT NOT NULL,
			created_by TEXT,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_gov_incident_comment_incident ON gov_incident_comments(incident_id, created_at)`,

		// Alerting channels (SMTP/Resend/Brevo)
		`CREATE TABLE IF NOT EXISTS alert_channels (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			channel_type TEXT NOT NULL,
			config_encrypted TEXT NOT NULL,
			is_active INTEGER DEFAULT 1,
			created_by TEXT,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_alert_channel_active ON alert_channels(is_active)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_alert_channel_name_unique ON alert_channels(name)`,

		// Alert rules
		`CREATE TABLE IF NOT EXISTS alert_rules (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			event_type TEXT NOT NULL,
			severity_min TEXT NOT NULL DEFAULT 'warn',
			enabled INTEGER DEFAULT 1,
			cooldown_seconds INTEGER DEFAULT 300,
			max_attempts INTEGER DEFAULT 5,
			subject_template TEXT,
			body_template TEXT,
			created_by TEXT,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_alert_rule_enabled ON alert_rules(enabled)`,
		`CREATE INDEX IF NOT EXISTS idx_alert_rule_event ON alert_rules(event_type, enabled)`,

		// Rule routes map rules to channels and recipients
		`CREATE TABLE IF NOT EXISTS alert_rule_routes (
			id TEXT PRIMARY KEY,
			rule_id TEXT NOT NULL REFERENCES alert_rules(id) ON DELETE CASCADE,
			channel_id TEXT NOT NULL REFERENCES alert_channels(id) ON DELETE CASCADE,
			recipients_json TEXT NOT NULL,
			is_active INTEGER DEFAULT 1,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_alert_route_rule ON alert_rule_routes(rule_id)`,
		`CREATE INDEX IF NOT EXISTS idx_alert_route_channel ON alert_rule_routes(channel_id)`,

		// Per-route delivery policy (digest/escalation metadata)
		`CREATE TABLE IF NOT EXISTS alert_route_policies (
			route_id TEXT PRIMARY KEY REFERENCES alert_rule_routes(id) ON DELETE CASCADE,
			delivery_mode TEXT NOT NULL DEFAULT 'immediate',
			digest_window_minutes INTEGER NOT NULL DEFAULT 0,
			escalation_channel_id TEXT REFERENCES alert_channels(id) ON DELETE SET NULL,
			escalation_recipients_json TEXT,
			escalation_after_failures INTEGER NOT NULL DEFAULT 0,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_alert_route_policy_delivery ON alert_route_policies(delivery_mode, digest_window_minutes)`,

		// Alert events emitted by governance/scheduler/other subsystems
		`CREATE TABLE IF NOT EXISTS alert_events (
			id TEXT PRIMARY KEY,
			connection_id TEXT,
			event_type TEXT NOT NULL,
			severity TEXT NOT NULL,
			title TEXT NOT NULL,
			message TEXT NOT NULL,
			payload_json TEXT,
			fingerprint TEXT,
			source_ref TEXT,
			status TEXT NOT NULL DEFAULT 'new',
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			processed_at TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_alert_event_status ON alert_events(status, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_alert_event_type ON alert_events(event_type, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_alert_event_fingerprint ON alert_events(fingerprint, created_at)`,

		// Dispatch jobs generated from events and routes
		`CREATE TABLE IF NOT EXISTS alert_dispatch_jobs (
			id TEXT PRIMARY KEY,
			event_id TEXT NOT NULL REFERENCES alert_events(id) ON DELETE CASCADE,
			rule_id TEXT NOT NULL REFERENCES alert_rules(id) ON DELETE CASCADE,
			route_id TEXT NOT NULL REFERENCES alert_rule_routes(id) ON DELETE CASCADE,
			channel_id TEXT NOT NULL REFERENCES alert_channels(id) ON DELETE CASCADE,
			status TEXT NOT NULL DEFAULT 'queued',
			attempt_count INTEGER DEFAULT 0,
			max_attempts INTEGER DEFAULT 5,
			next_attempt_at TEXT NOT NULL,
			last_error TEXT,
			provider_message_id TEXT,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT DEFAULT CURRENT_TIMESTAMP,
			sent_at TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_alert_job_due ON alert_dispatch_jobs(status, next_attempt_at)`,
		`CREATE INDEX IF NOT EXISTS idx_alert_job_event ON alert_dispatch_jobs(event_id)`,
		`CREATE INDEX IF NOT EXISTS idx_alert_job_route ON alert_dispatch_jobs(route_id)`,

		// Digest windows for routes configured in digest mode
		`CREATE TABLE IF NOT EXISTS alert_route_digests (
			id TEXT PRIMARY KEY,
			route_id TEXT NOT NULL REFERENCES alert_rule_routes(id) ON DELETE CASCADE,
			rule_id TEXT NOT NULL REFERENCES alert_rules(id) ON DELETE CASCADE,
			channel_id TEXT NOT NULL REFERENCES alert_channels(id) ON DELETE CASCADE,
			bucket_start TEXT NOT NULL,
			bucket_end TEXT NOT NULL,
			event_type TEXT NOT NULL,
			severity TEXT NOT NULL,
			event_count INTEGER NOT NULL DEFAULT 0,
			event_ids_json TEXT NOT NULL,
			titles_json TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'collecting',
			attempt_count INTEGER NOT NULL DEFAULT 0,
			max_attempts INTEGER NOT NULL DEFAULT 5,
			next_attempt_at TEXT NOT NULL,
			last_error TEXT,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT DEFAULT CURRENT_TIMESTAMP,
			sent_at TEXT,
			UNIQUE(route_id, bucket_start, event_type, severity)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_alert_digest_due ON alert_route_digests(status, next_attempt_at, bucket_end)`,
		`CREATE INDEX IF NOT EXISTS idx_alert_digest_route ON alert_route_digests(route_id, bucket_start)`,
	}

	for _, stmt := range stmts {
		if _, err := db.conn.Exec(stmt); err != nil {
			return err
		}
	}

	if err := db.ensureColumn("gov_policies", "enforcement_mode", "TEXT NOT NULL DEFAULT 'warn'"); err != nil {
		return err
	}
	if err := db.ensureColumn("gov_policy_violations", "detection_phase", "TEXT NOT NULL DEFAULT 'post_exec'"); err != nil {
		return err
	}
	if err := db.ensureColumn("gov_policy_violations", "request_endpoint", "TEXT"); err != nil {
		return err
	}
	if err := db.ensureColumn("brain_chats", "context_database", "TEXT"); err != nil {
		return err
	}
	if err := db.ensureColumn("brain_chats", "context_table", "TEXT"); err != nil {
		return err
	}
	if err := db.ensureColumn("brain_chats", "context_tables", "TEXT"); err != nil {
		return err
	}

	// Drop legacy tables from the old SaaS schema
	dropLegacy := []string{
		"DROP TABLE IF EXISTS organizations",
		"DROP TABLE IF EXISTS tunnel_connections",
		"DROP TABLE IF EXISTS cloud_sessions",
		"DROP TABLE IF EXISTS scheduled_runs",
		"DROP TABLE IF EXISTS scheduled_jobs",
		"DROP TABLE IF EXISTS cloud_saved_queries",
		"DROP TABLE IF EXISTS cloud_panels",
		"DROP TABLE IF EXISTS cloud_dashboards",
		"DROP TABLE IF EXISTS cloud_user_roles",
		"DROP TABLE IF EXISTS beta_applications",
		"DROP TABLE IF EXISTS cloud_audit_logs",
	}
	for _, stmt := range dropLegacy {
		if _, err := db.conn.Exec(stmt); err != nil {
			slog.Warn("Failed to drop legacy table", "error", err)
		}
	}

	// Seed installation_id if not present
	var count int
	if err := db.conn.QueryRow("SELECT COUNT(*) FROM settings WHERE key = 'installation_id'").Scan(&count); err == nil && count == 0 {
		db.conn.Exec("INSERT INTO settings (key, value) VALUES ('installation_id', ?)", uuid.NewString())
		slog.Info("Generated new installation ID")
	}

	// Seed default Brain skill if not present.
	if err := db.conn.QueryRow("SELECT COUNT(*) FROM brain_skills").Scan(&count); err == nil && count == 0 {
		now := "CURRENT_TIMESTAMP"
		db.conn.Exec(`INSERT INTO brain_skills (id, name, content, is_active, is_default, created_by, created_at, updated_at)
			VALUES (?, ?, ?, 1, 1, 'system', `+now+`, `+now+`)`,
			uuid.NewString(),
			"Default Brain Skill",
			`You are Brain, a senior ClickHouse copilot.

Priorities:
- Give correct SQL first, concise explanation second.
- Keep queries safe and cost-aware: start with LIMIT 100 unless user asks otherwise.
- Prefer explicit columns over SELECT * on large tables.
- Use only schema fields known in context; if missing, ask a short clarifying question.
- When uncertain, provide assumptions clearly.

Artifacts:
- When sharing SQL, return a runnable SQL block.
- If a query result artifact exists, reference it by title and summarize key findings in bullets.
- For follow-ups, reuse prior artifacts/chats when relevant.

Tool behavior:
- Read-only queries by default.
- Never execute DDL/DROP/TRUNCATE/ALTER unless user explicitly asks and confirms.
- For expensive requests, propose a lightweight preview query first.

Formatting:
1) One-line intent acknowledgment.
2) SQL in a fenced sql block.
3) Short explanation and optional next-step variants.`,
		)
	}

	slog.Info("Database migrations completed")
	return nil
}

func (db *DB) ensureColumn(tableName, columnName, definition string) error {
	rows, err := db.conn.Query(fmt.Sprintf("PRAGMA table_info(%s)", tableName))
	if err != nil {
		return fmt.Errorf("inspect table %s columns: %w", tableName, err)
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var dfltValue interface{}
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk); err != nil {
			return fmt.Errorf("scan table info for %s: %w", tableName, err)
		}
		if strings.EqualFold(strings.TrimSpace(name), strings.TrimSpace(columnName)) {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate table info for %s: %w", tableName, err)
	}

	if _, err := db.conn.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", tableName, columnName, definition)); err != nil {
		return fmt.Errorf("add column %s.%s: %w", tableName, columnName, err)
	}

	return nil
}
