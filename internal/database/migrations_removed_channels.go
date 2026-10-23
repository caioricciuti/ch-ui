package database

import "fmt"

// migrateRemovedChannelTypes turns off alert channels of the email API
// providers CH-UI no longer sends through (Resend, Brevo). The rows stay,
// with their type and encrypted config, so admins see them as unsupported
// and delete them or recreate them as SMTP or webhook. Queued jobs on these
// channels are cancelled by the dispatcher because the channel is off.
//
// Idempotent: safe to run on every start.
func (db *DB) migrateRemovedChannelTypes() error {
	if _, err := db.conn.Exec(
		`UPDATE alert_channels SET is_active = 0, updated_at = CURRENT_TIMESTAMP
		 WHERE LOWER(TRIM(channel_type)) IN ('resend', 'brevo') AND is_active != 0`,
	); err != nil {
		return fmt.Errorf("removed channel types migration: %w", err)
	}
	return nil
}
