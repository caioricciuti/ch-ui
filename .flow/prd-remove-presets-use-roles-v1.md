---
title: Remove Presets Feature, Use Roles Instead
status: approved
priority: P2
type: refactor
created: 2026-02-12
related_issues: []
---

# PRD: Remove Presets Feature, Use Roles Instead

## Summary

Remove the client-side privilege presets system and rely entirely on ClickHouse's native roles system. The presets feature duplicates what roles already provide (named sets of privileges), but stores them in localStorage instead of the server, creating a confusing dual system.

## Problem

The current Create/Edit User screen has two overlapping concepts:
1. **Presets** - Client-side (localStorage) named privilege templates managed via `PresetToolbar`
2. **Roles** - Server-side (ClickHouse `system.roles`) named privilege sets with native GRANT/REVOKE

Both map a name to a set of privileges. Presets are browser-local and connection-specific, while roles are server-side and shared across all clients. Having both confuses the UX and adds maintenance burden.

## Solution

Remove all preset-related code and UI. The existing **Database and Roles** section (which assigns ClickHouse roles) and the **Roles** tab in the Admin page (which manages role definitions) are sufficient.

## Scope

### Files to DELETE (complete removal)
1. `src/features/admin/components/CreateUser/PrivilegesSection/presetTypes.ts` - Preset type definitions
2. `src/features/admin/components/CreateUser/PrivilegesSection/usePresetStore.ts` - Zustand preset store
3. `src/features/admin/components/CreateUser/PrivilegesSection/PresetToolbar.tsx` - Preset UI toolbar
4. `src/features/admin/components/CreateUser/PrivilegesSection/SavePresetDialog.tsx` - Save preset dialog
5. `src/features/admin/components/CreateUser/PrivilegesSection/grantUtils.ts` - `grantsMatch()` utility (only used by PresetToolbar)

### Files to MODIFY
1. `src/features/admin/components/CreateUser/PrivilegesSection/index.tsx` - Remove PresetToolbar import/usage, remove usePresetStore import, remove auto-apply logic, update description text
2. Any other files importing from the deleted files (to be verified)

### Files to KEEP (no changes)
- `src/features/admin/components/CreateUser/DatabaseRolesSection.tsx` - Already handles role assignment
- `src/features/admin/components/RoleManagement/` - Already handles role CRUD
- `src/features/admin/components/CreateUser/PrivilegesSection/permissions.ts` - Permission hierarchy (still used)
- `src/features/admin/components/CreateUser/PrivilegesSection/types.ts` - Role/grant types (still used)
- `src/features/admin/components/CreateUser/PrivilegesSection/PrivilegesPanel.tsx` - Privilege editor (still used)

## UI Changes

### Before (Create User - Privileges Card)
```
[Privileges Card]
  "Select a preset or manually configure privileges..."
  [PresetToolbar: dropdown + save/create/delete/export/import buttons]
  [PrivilegesPanel: database/table/privilege selector]
```

### After (Create User - Privileges Card)
```
[Privileges Card]
  "Manually configure direct privileges, or assign roles in the Database and Roles section above."
  [PrivilegesPanel: database/table/privilege selector]
```

## Out of Scope

- Modifying the DatabaseRolesSection (it already works)
- Modifying the Roles tab in Admin page (it already works)
- Creating default roles in ClickHouse (user decided against this)
- Import/export functionality for roles (user decided against this)
- Cleaning up localStorage data (old preset data will be ignored naturally)

## Acceptance Criteria

1. All preset-related files are deleted
2. PrivilegesSection renders without PresetToolbar
3. No references to deleted modules remain in the codebase
4. The PrivilegesPanel still works for manual privilege configuration
5. The DatabaseRolesSection still works for role assignment
6. Build passes with no errors
7. No runtime errors when creating/editing users
