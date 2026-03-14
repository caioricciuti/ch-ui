# PRD: ClickHouse Permission Management Fixes

## Status: Completed
## Priority: Critical
## Date: 2026-03-02

---

## Problem Statement

A thorough security and correctness audit of the ch-ui permission management system revealed 11 issues across Critical, High, Medium, and Low severity levels. The most severe is pervasive SQL injection via string interpolation in SQL generation code. Additionally, the system misrepresents effective permissions by ignoring partial revokes, doesn't track grant options per-permission, is missing many ClickHouse access types, and has several functional gaps.

---

## Goals

1. Eliminate all SQL injection vulnerabilities by adding proper identifier/string escaping
2. Fix grant fetching to include `is_partial_revoke`, `grant_option`, and `column` fields
3. Expand permission definitions to cover all ClickHouse access types
4. Fix individual privilege mapping (replace ACCESS MANAGEMENT umbrella)
5. Fix user deletion, role resolution, direct execution bypass, and GRANT OPTION modeling
6. Add unit tests for SQL generation and escaping

## Non-Goals

- Redesigning the UI/UX of the permissions management interface
- Adding new features beyond fixing existing issues
- Rewriting the entire permission system architecture

---

## Technical Design

### Epic 1: SQL Escaping Utilities + SQL Injection Fix

**New file: `src/features/admin/utils/sqlEscape.ts`**

Utility functions:
- `escapeIdentifier(name: string): string` — wraps in backticks, escapes internal backticks
- `escapeStringLiteral(value: string): string` — escapes single quotes
- `formatScopeEscaped(scope: PermissionScope): string` — scope formatting with escaping

**Apply to all SQL generation in:**
- `useSqlGenerator.ts` — all 11 generator functions
- `permissions.ts` — `generateGrantSql()`
- `CreateUser/index.tsx` — `buildUserCreationQuery()`, `buildGrantQueries()`, role assignments
- `EditUser.tsx` — all ALTER USER statements
- `UserManagement/index.tsx` — SHOW GRANTS, DROP USER, password reset
- `RoleEditor.tsx` — CREATE ROLE, grant/revoke
- `RoleList.tsx` — DROP ROLE
- `useRoleAssignments.ts` — GRANT/REVOKE role
- `useRoles.ts` — CREATE/DROP ROLE

**Unit tests:** Test escaping with special characters, backticks, single quotes, SQL injection payloads.

### Epic 2: Fix Grant Fetching (Partial Revokes, Grant Option, Columns)

**`useGrants.ts`:**
- Add `is_partial_revoke`, `grant_option`, `column` to SELECT query
- Update transform to include these fields in GrantedPermission
- Filter or mark partial revokes separately

**`useEffectiveGrants.ts`:**
- Add same columns to all three queries (direct, role assignments, role grants)
- Implement partial revoke logic: when merging grants, subtract partial revokes from broader grants
- Track grant_option per permission, not globally
- Add nested role resolution (recursive query or iterative fetching)

**Types update (`types.ts`):**
- Add `isPartialRevoke`, `grantOption`, `column` to GrantedPermission
- Add `columns?: string[]` to PermissionScope

**`useUserPrivileges.ts`:**
- Track grant_option per permission instead of global flag

### Epic 3: Expand Permission Definitions

**`permissions.ts`:**
Add missing permissions to PERMISSION_HIERARCHY:
- BACKUP, UNDROP_TABLE, KILL_TRANSACTION, MOVE_PARTITION_BETWEEN_SHARDS
- ROLE_ADMIN, SET_DEFINER, ALLOW_SQL_SECURITY_NONE
- SHOW_ACCESS (separate from SHOW)
- TABLE_ENGINE
- INTROSPECTION (addressToLine, demangle subtypes)
- dictGet, displaySecretsInShowAndSelect
- SOURCES group with all data source permissions (FILE, URL, REMOTE, CLUSTER, MONGO, REDIS, MYSQL, POSTGRES, SQLITE, ODBC, JDBC, HDFS, S3, HIVE, AZURE, KAFKA, NATS, RABBITMQ)

**`privilegeDefinitions.ts`:**
- Replace `"ACCESS MANAGEMENT"` umbrella with individual SQL privileges (CREATE USER, ALTER USER, DROP USER, CREATE ROLE, ALTER ROLE, DROP ROLE)
- Add missing OTHER_PRIVILEGES: ROLE_ADMIN, BACKUP, TABLE_ENGINE, INTROSPECTION, SOURCES
- Fix GRANT_OPTION — remove as standalone privilege, document it as a modifier
- Add CREATE/ALTER/DROP ROW POLICY, QUOTA, SETTINGS PROFILE

### Epic 4: Functional Fixes

**User deletion (`UserManagement/index.tsx`):**
- Add `REVOKE ALL ROLES FROM user` before DROP USER

**Direct execution bypass (`UserManagement/index.tsx`):**
- Route `handleRefreshPassword` through the staged review system

**Identifier quoting (`formatScope` in permissions.ts and useSqlGenerator.ts):**
- Use `escapeIdentifier()` for database/table names in scope formatting

**GRANT OPTION modeling (`privilegeDefinitions.ts`):**
- Remove GRANT_OPTION from TABLE_PRIVILEGES list
- Add `withGrantOption` flag to grant generation functions

---

## Implementation Order

1. **Epic 1** first (SQL escaping) — foundational, all other changes build on it
2. **Epic 3** next (permission definitions) — no runtime dependencies, purely declarative
3. **Epic 2** (grant fetching) — depends on updated types from Epic 3
4. **Epic 4** (functional fixes) — depends on escaping utilities from Epic 1

## Testing Strategy

- Unit tests for `sqlEscape.ts` (escaping functions)
- Unit tests for `useSqlGenerator.ts` (SQL generation correctness)
- Unit tests for updated permission hierarchy traversal
- All tests use Vitest with happy-dom

## Success Criteria

- Zero SQL injection vulnerabilities in permission management code
- All ClickHouse access types represented in permission definitions
- Partial revokes correctly reflected in effective grants display
- Grant option tracked per-permission
- User deletion properly revokes roles
- Password reset goes through staging
- All new code has unit test coverage
