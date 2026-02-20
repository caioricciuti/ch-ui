package governance

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

// Default number of inactive days to consider a permission as "over-granted".
const overPermissionInactiveDays = 30

// syncAccess harvests user, role, grant, and role_grant data from ClickHouse
// system tables, upserts them into SQLite, and rebuilds the access matrix.
func (s *Syncer) syncAccess(ctx context.Context, creds CHCredentials) (*AccessSyncResult, error) {
	connID := creds.ConnectionID
	now := time.Now().UTC().Format(time.RFC3339)

	// Update sync state to running
	if err := s.store.UpsertSyncState(connID, string(SyncAccess), "running", nil, nil, 0); err != nil {
		slog.Error("Failed to update sync state", "error", err)
	}

	result := &AccessSyncResult{}
	var syncErr error

	defer func() {
		status := "idle"
		var errMsg *string
		if syncErr != nil {
			status = "error"
			e := syncErr.Error()
			errMsg = &e
		}
		totalRows := result.UsersSynced + result.RolesSynced + result.GrantsSynced
		if err := s.store.UpsertSyncState(connID, string(SyncAccess), status, &now, errMsg, totalRows); err != nil {
			slog.Error("Failed to update sync state after access sync", "error", err)
		}
	}()

	// ── Phase 1: Users ──────────────────────────────────────────────────────
	userRows, err := s.executeQuery(creds,
		`SELECT
			name,
			toString(auth_type) AS auth_type,
			toString(host_ip) AS host_ip,
			default_roles_all,
			toString(default_roles_list) AS default_roles_list
		 FROM system.users
		 ORDER BY name`)
	if err != nil {
		slog.Warn("Access sync: failed to query system.users with role fields, trying fallback", "connection", connID, "error", err)
		userRows, err = s.executeQuery(creds,
			`SELECT
				name,
				toString(auth_type) AS auth_type,
				toString(host_ip) AS host_ip,
				0 AS default_roles_all,
				'' AS default_roles_list
			 FROM system.users
			 ORDER BY name`)
		if err != nil {
			slog.Warn("Access sync: fallback query for system.users failed", "connection", connID, "error", err)
			userRows = nil
		}
	} else {
		// no-op; rows handled below
	}

	usersFetched := err == nil
	if usersFetched {
		if err := s.store.DeleteChUsersForConnection(connID); err != nil {
			syncErr = err
			return nil, syncErr
		}

		for _, row := range userRows {
			name := fmt.Sprintf("%v", row["name"])

			var defaultRoles *string
			if allRoles, ok := row["default_roles_all"]; ok && fmt.Sprintf("%v", allRoles) == "1" {
				dr := "ALL"
				defaultRoles = &dr
			} else if roleList, ok := row["default_roles_list"]; ok {
				rl := fmt.Sprintf("%v", roleList)
				if rl != "" && rl != "<nil>" && rl != "[]" {
					defaultRoles = &rl
				}
			}

			authType := toStringPtr(row["auth_type"])
			hostIP := toStringPtr(row["host_ip"])

			if err := s.store.UpsertChUser(ChUser{
				ID:           uuid.NewString(),
				ConnectionID: connID,
				Name:         name,
				AuthType:     authType,
				HostIP:       hostIP,
				DefaultRoles: defaultRoles,
				FirstSeen:    now,
				LastUpdated:  now,
			}); err != nil {
				slog.Error("Failed to upsert CH user", "name", name, "error", err)
				continue
			}
			result.UsersSynced++
		}
	}

	if !usersFetched {
		slog.Warn("Access sync: keeping previous cached users because source query failed", "connection", connID)
	}

	// ── Phase 2: Roles ──────────────────────────────────────────────────────
	roleRows, err := s.executeQuery(creds,
		`SELECT name FROM system.roles ORDER BY name`)
	if err != nil {
		slog.Warn("Access sync: failed to query system.roles", "connection", connID, "error", err)
	} else {
		if err := s.store.DeleteChRolesForConnection(connID); err != nil {
			syncErr = err
			return nil, syncErr
		}

		for _, row := range roleRows {
			name := fmt.Sprintf("%v", row["name"])

			if err := s.store.UpsertChRole(ChRole{
				ID:           uuid.NewString(),
				ConnectionID: connID,
				Name:         name,
				FirstSeen:    now,
				LastUpdated:  now,
			}); err != nil {
				slog.Error("Failed to upsert CH role", "name", name, "error", err)
				continue
			}
			result.RolesSynced++
		}
	}

	// ── Phase 3: Role grants ────────────────────────────────────────────────
	roleGrantRows, err := s.executeQuery(creds,
		`SELECT
			user_name,
			granted_role_name,
			granted_role_is_default,
			with_admin_option
		 FROM system.role_grants
		 ORDER BY user_name, granted_role_name`)
	if err != nil {
		slog.Warn("Access sync: failed to query system.role_grants", "connection", connID, "error", err)
	} else {
		if err := s.store.DeleteRoleGrantsForConnection(connID); err != nil {
			syncErr = err
			return nil, syncErr
		}

		for _, row := range roleGrantRows {
			userName := fmt.Sprintf("%v", row["user_name"])
			roleName := fmt.Sprintf("%v", row["granted_role_name"])
			isDefault := toBool(row["granted_role_is_default"])
			withAdmin := toBool(row["with_admin_option"])

			if err := s.store.UpsertRoleGrant(RoleGrant{
				ID:              uuid.NewString(),
				ConnectionID:    connID,
				UserName:        userName,
				GrantedRoleName: roleName,
				IsDefault:       isDefault,
				WithAdminOption: withAdmin,
				FirstSeen:       now,
				LastUpdated:     now,
			}); err != nil {
				slog.Error("Failed to upsert role grant", "user", userName, "role", roleName, "error", err)
				continue
			}
		}
	}

	// ── Phase 4: Grants (privileges) ────────────────────────────────────────
	grantRows, err := s.executeQuery(creds,
		`SELECT
			user_name,
			role_name,
			access_type,
			database AS grant_database,
			table AS grant_table,
			column AS grant_column,
			is_partial_revoke,
			grant_option
		 FROM system.grants
		 ORDER BY user_name, role_name, access_type`)
	if err != nil {
		slog.Warn("Access sync: failed to query system.grants", "connection", connID, "error", err)
	} else {
		if err := s.store.DeleteGrantsForConnection(connID); err != nil {
			syncErr = err
			return nil, syncErr
		}

		for _, row := range grantRows {
			grant := Grant{
				ID:              uuid.NewString(),
				ConnectionID:    connID,
				UserName:        toStringPtr(row["user_name"]),
				RoleName:        toStringPtr(row["role_name"]),
				AccessType:      fmt.Sprintf("%v", row["access_type"]),
				GrantDatabase:   toStringPtr(row["grant_database"]),
				GrantTable:      toStringPtr(row["grant_table"]),
				GrantColumn:     toStringPtr(row["grant_column"]),
				IsPartialRevoke: toBool(row["is_partial_revoke"]),
				GrantOption:     toBool(row["grant_option"]),
				FirstSeen:       now,
				LastUpdated:     now,
			}

			if err := s.store.UpsertGrant(grant); err != nil {
				slog.Error("Failed to upsert grant",
					"user", grant.UserName,
					"role", grant.RoleName,
					"access_type", grant.AccessType,
					"error", err,
				)
				continue
			}
			result.GrantsSynced++
		}
	}

	// ── Phase 5: Rebuild access matrix ──────────────────────────────────────
	matrixCount, err := s.store.RebuildAccessMatrix(connID)
	if err != nil {
		slog.Error("Failed to rebuild access matrix", "connection", connID, "error", err)
	} else {
		result.MatrixEntries = matrixCount
	}

	// ── Phase 6: Count over-permissions ─────────────────────────────────────
	overPerms, err := s.store.GetOverPermissionsWithDays(connID, overPermissionInactiveDays)
	if err != nil {
		slog.Warn("Failed to count over-permissions", "connection", connID, "error", err)
	} else {
		result.OverPermissions = len(overPerms)
	}

	slog.Info("Access sync completed",
		"connection", connID,
		"users", result.UsersSynced,
		"roles", result.RolesSynced,
		"grants", result.GrantsSynced,
		"matrix_entries", result.MatrixEntries,
		"over_permissions", result.OverPermissions,
	)

	return result, nil
}

// toBool converts an interface{} to bool. Handles ClickHouse-style values:
// 0/1 (as float64 or string), true/false, etc.
func toBool(v interface{}) bool {
	if v == nil {
		return false
	}
	switch val := v.(type) {
	case bool:
		return val
	case float64:
		return val != 0
	case int64:
		return val != 0
	case int:
		return val != 0
	case string:
		return val == "1" || val == "true" || val == "True"
	default:
		return fmt.Sprintf("%v", v) == "1"
	}
}
