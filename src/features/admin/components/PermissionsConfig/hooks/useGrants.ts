import { useState, useEffect } from "react";
import useAppStore from "@/store";
import { GrantedPermission, PermissionScope } from "../../CreateUser/PrivilegesSection/permissions";

interface SystemGrantRow {
  access_type: string;
  database: string | null;
  table: string | null;
  column: string | null;
  is_partial_revoke: number;
  grant_option: number;
}

interface UseGrantsOptions {
  userName?: string;
  roleName?: string;
}

/**
 * Hook to fetch and transform grants from system.grants table
 * @param userName - User name to fetch grants for
 * @param roleName - Role name to fetch grants for
 * @returns Array of GrantedPermission objects
 */
export function useGrants({ userName, roleName }: UseGrantsOptions) {
  const { clickHouseClient } = useAppStore();
  const [grants, setGrants] = useState<GrantedPermission[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    async function fetchGrants() {
      if (!clickHouseClient) {
        setError("ClickHouse client not available");
        return;
      }

      if (!userName && !roleName) {
        setGrants([]);
        return;
      }

      setLoading(true);
      setError(null);

      try {
        // Build query based on whether we're fetching for user or role
        const whereClause = userName
          ? "WHERE user_name = {entityName:String}"
          : "WHERE role_name = {entityName:String}";

        const query = `
          SELECT
            access_type,
            database,
            table,
            column,
            is_partial_revoke,
            grant_option
          FROM system.grants
          ${whereClause}
          ORDER BY access_type, database, table
        `;

        const result = await clickHouseClient.query({
          query,
          query_params: {
            entityName: userName || roleName || "",
          },
        });

        const response = await result.json<{ data: SystemGrantRow[] }>();
        const transformedGrants = transformGrantsToPermissions(response.data);
        setGrants(transformedGrants);
      } catch (err) {
        const errorMessage = err instanceof Error ? err.message : "Failed to fetch grants";
        setError(errorMessage);
        console.error("Error fetching grants:", err);
      } finally {
        setLoading(false);
      }
    }

    fetchGrants();
  }, [clickHouseClient, userName, roleName]);

  return { grants, loading, error };
}

/**
 * Transform system.grants rows to GrantedPermission format
 */
function transformGrantsToPermissions(rows: SystemGrantRow[]): GrantedPermission[] {
  return rows.map((row) => {
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
  });
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
