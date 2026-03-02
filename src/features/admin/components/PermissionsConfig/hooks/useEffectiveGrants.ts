import { useState, useEffect, useCallback } from "react";
import useAppStore from "@/store";
import {
  GrantedPermission,
  PermissionScope,
} from "../../CreateUser/PrivilegesSection/permissions";
import {
  ExtendedGrantedPermission,
  RoleAssignment,
} from "../../CreateUser/PrivilegesSection/types";

interface SystemGrantRow {
  access_type: string;
  database: string | null;
  table: string | null;
  column: string | null;
  is_partial_revoke: number;
  grant_option: number;
  user_name?: string | null;
  role_name?: string | null;
}

interface SystemRoleGrantRow {
  granted_role_name: string;
  with_admin_option: number;
}

interface UseEffectiveGrantsResult {
  /** Direct grants to the user */
  directGrants: GrantedPermission[];

  /** Roles assigned to the user */
  assignedRoles: RoleAssignment[];

  /** Grants organized by role name */
  roleGrants: Map<string, GrantedPermission[]>;

  /** Combined grants with source information */
  effectiveGrants: ExtendedGrantedPermission[];

  /** Loading state */
  loading: boolean;

  /** Error message if any */
  error: string | null;

  /** Refetch all data */
  refetch: () => void;
}

/**
 * Hook to fetch effective grants for a user including both direct grants
 * and grants inherited from assigned roles.
 *
 * @param userName - User name to fetch grants for
 * @returns Combined direct and role-inherited grants with source information
 */
export function useEffectiveGrants(userName?: string): UseEffectiveGrantsResult {
  const { clickHouseClient } = useAppStore();
  const [directGrants, setDirectGrants] = useState<GrantedPermission[]>([]);
  const [assignedRoles, setAssignedRoles] = useState<RoleAssignment[]>([]);
  const [roleGrants, setRoleGrants] = useState<Map<string, GrantedPermission[]>>(new Map());
  const [effectiveGrants, setEffectiveGrants] = useState<ExtendedGrantedPermission[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [refetchTrigger, setRefetchTrigger] = useState(0);

  const refetch = useCallback(() => {
    setRefetchTrigger((prev) => prev + 1);
  }, []);

  useEffect(() => {
    async function fetchEffectiveGrants() {
      if (!clickHouseClient) {
        setError("ClickHouse client not available");
        return;
      }

      if (!userName) {
        setDirectGrants([]);
        setAssignedRoles([]);
        setRoleGrants(new Map());
        setEffectiveGrants([]);
        return;
      }

      setLoading(true);
      setError(null);

      try {
        // 1. Fetch direct grants for the user
        const directGrantsQuery = `
          SELECT
            access_type,
            database,
            table,
            column,
            is_partial_revoke,
            grant_option
          FROM system.grants
          WHERE user_name = {userName:String}
          ORDER BY access_type, database, table
        `;

        const directResult = await clickHouseClient.query({
          query: directGrantsQuery,
          query_params: {
            userName,
          },
        });

        const directResponse = await directResult.json<{ data: SystemGrantRow[] }>();
        const transformedDirectGrants = transformGrantsToPermissions(directResponse.data);
        setDirectGrants(transformedDirectGrants);

        // 2. Fetch role assignments for the user
        const roleAssignmentsQuery = `
          SELECT
            granted_role_name,
            with_admin_option
          FROM system.role_grants
          WHERE user_name = {userName:String}
          ORDER BY granted_role_name
        `;

        const roleAssignmentsResult = await clickHouseClient.query({
          query: roleAssignmentsQuery,
          query_params: {
            userName,
          },
        });

        const roleAssignmentsResponse = await roleAssignmentsResult.json<{
          data: SystemRoleGrantRow[];
        }>();
        const transformedRoleAssignments: RoleAssignment[] = roleAssignmentsResponse.data.map(
          (row) => ({
            roleName: row.granted_role_name,
            adminOption: row.with_admin_option === 1,
          })
        );
        setAssignedRoles(transformedRoleAssignments);

        // 3. Fetch grants for each assigned role
        const roleGrantsMap = new Map<string, GrantedPermission[]>();

        if (transformedRoleAssignments.length > 0) {
          const roleNames = transformedRoleAssignments.map((r) => r.roleName);

          // Build IN clause for role names
          const roleNamesPlaceholders = roleNames.map((_, i) => `{role_${i}:String}`).join(", ");

          const roleGrantsQuery = `
            SELECT
              role_name,
              access_type,
              database,
              table,
              column,
              is_partial_revoke,
              grant_option
            FROM system.grants
            WHERE role_name IN (${roleNamesPlaceholders})
            ORDER BY role_name, access_type, database, table
          `;

          // Build query params for role names
          const roleQueryParams: Record<string, string> = {};
          roleNames.forEach((roleName, i) => {
            roleQueryParams[`role_${i}`] = roleName;
          });

          const roleGrantsResult = await clickHouseClient.query({
            query: roleGrantsQuery,
            query_params: roleQueryParams,
          });

          const roleGrantsResponse = await roleGrantsResult.json<{ data: SystemGrantRow[] }>();

          // Group grants by role name
          for (const row of roleGrantsResponse.data) {
            const roleName = row.role_name!;
            if (!roleGrantsMap.has(roleName)) {
              roleGrantsMap.set(roleName, []);
            }
            const permission = transformGrantToPermission(row);
            roleGrantsMap.get(roleName)!.push(permission);
          }
        }

        setRoleGrants(roleGrantsMap);

        // 4. Combine into effective grants with source information
        const combined: ExtendedGrantedPermission[] = [];

        // Add direct grants (excluding partial revokes)
        for (const grant of transformedDirectGrants) {
          combined.push({
            ...grant,
            source: "direct",
          });
        }

        // Add role-inherited grants (excluding partial revokes)
        for (const [roleName, grants] of roleGrantsMap.entries()) {
          for (const grant of grants) {
            combined.push({
              ...grant,
              source: "role",
              sourceRole: roleName,
            });
          }
        }

        setEffectiveGrants(combined);
      } catch (err) {
        const errorMessage = err instanceof Error ? err.message : "Failed to fetch effective grants";
        setError(errorMessage);
        console.error("Error fetching effective grants:", err);
      } finally {
        setLoading(false);
      }
    }

    fetchEffectiveGrants();
  }, [clickHouseClient, userName, refetchTrigger]);

  return {
    directGrants,
    assignedRoles,
    roleGrants,
    effectiveGrants,
    loading,
    error,
    refetch,
  };
}

/**
 * Transform system.grants rows to GrantedPermission format
 */
function transformGrantsToPermissions(rows: SystemGrantRow[]): GrantedPermission[] {
  return rows.map(transformGrantToPermission);
}

/**
 * Transform a single grant row to GrantedPermission
 */
function transformGrantToPermission(row: SystemGrantRow): GrantedPermission {
  // Convert access_type to permissionId by replacing spaces with underscores
  // e.g., "ALTER TABLE" → "ALTER_TABLE", "SELECT" → "SELECT"
  const permissionId = row.access_type.replace(/\s+/g, "_");

  // Determine scope from database/table columns
  const scope = determineScope(row.database, row.table);

  return {
    permissionId,
    scope,
    isPartialRevoke: row.is_partial_revoke === 1,
    grantOption: row.grant_option === 1,
    ...(row.column ? { columns: [row.column] } : {}),
  };
}

/**
 * Determine permission scope from database and table values
 */
function determineScope(database: string | null, table: string | null): PermissionScope {
  // Both empty → global scope
  if (!database && !table) {
    return { type: "global" };
  }

  // Only database set → database scope
  if (database && !table) {
    return {
      type: "database",
      database,
    };
  }

  // Both set → table scope
  if (database && table) {
    return {
      type: "table",
      database,
      table,
    };
  }

  // Fallback to global if only table is set (shouldn't happen in practice)
  return { type: "global" };
}
