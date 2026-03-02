import { useState, useEffect, useCallback } from "react";
import useAppStore from "@/store";
import { Role } from "../../CreateUser/PrivilegesSection/types";
import { escapeIdentifier } from "@/features/admin/utils/sqlEscape";

interface SystemRoleRow {
  name: string;
  id: string;
  storage: string | null;
}

interface UseRolesOptions {
  /** Optional external trigger to force refetch */
  refreshTrigger?: number;
}

interface UseRolesResult {
  /** All roles in the system */
  roles: Role[];

  /** Loading state */
  loading: boolean;

  /** Error message if any */
  error: string | null;

  /** Refetch roles */
  refetch: () => void;

  /** Create a new role */
  createRole: (roleName: string) => Promise<void>;

  /** Drop a role */
  dropRole: (roleName: string) => Promise<void>;
}

/**
 * Hook to fetch and manage roles from system.roles.
 * Provides methods to create and drop roles.
 *
 * @param options - Options for the hook
 * @returns All system roles and management methods
 */
export function useRoles(options: UseRolesOptions = {}): UseRolesResult {
  const { refreshTrigger: externalRefreshTrigger } = options;
  const { clickHouseClient } = useAppStore();
  const [roles, setRoles] = useState<Role[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [refetchTrigger, setRefetchTrigger] = useState(0);

  const refetch = useCallback(() => {
    setRefetchTrigger((prev) => prev + 1);
  }, []);

  useEffect(() => {
    async function fetchRoles() {
      if (!clickHouseClient) {
        setError("ClickHouse client not available");
        return;
      }

      setLoading(true);
      setError(null);

      try {
        const query = `
          SELECT
            name,
            id,
            storage
          FROM system.roles
          ORDER BY name
        `;

        const result = await clickHouseClient.query({
          query,
        });

        const response = await result.json<{ data: SystemRoleRow[] }>();
        const transformedRoles: Role[] = response.data.map((row) => ({
          name: row.name,
          id: row.id,
          storage: row.storage || undefined,
        }));

        setRoles(transformedRoles);
      } catch (err) {
        const errorMessage = err instanceof Error ? err.message : "Failed to fetch roles";
        setError(errorMessage);
        console.error("Error fetching roles:", err);
      } finally {
        setLoading(false);
      }
    }

    fetchRoles();
  }, [clickHouseClient, refetchTrigger, externalRefreshTrigger]);

  const createRole = useCallback(
    async (roleName: string) => {
      if (!clickHouseClient) {
        throw new Error("ClickHouse client not available");
      }

      const query = `CREATE ROLE IF NOT EXISTS ${escapeIdentifier(roleName)}`;

      try {
        await clickHouseClient.command({
          query,
        });

        // Refetch to update the list
        refetch();
      } catch (err) {
        const errorMessage = err instanceof Error ? err.message : "Failed to create role";
        console.error("Error creating role:", err);
        throw new Error(errorMessage);
      }
    },
    [clickHouseClient, refetch]
  );

  const dropRole = useCallback(
    async (roleName: string) => {
      if (!clickHouseClient) {
        throw new Error("ClickHouse client not available");
      }

      const query = `DROP ROLE IF EXISTS ${escapeIdentifier(roleName)}`;

      try {
        await clickHouseClient.command({
          query,
        });

        // Refetch to update the list
        refetch();
      } catch (err) {
        const errorMessage = err instanceof Error ? err.message : "Failed to drop role";
        console.error("Error dropping role:", err);
        throw new Error(errorMessage);
      }
    },
    [clickHouseClient, refetch]
  );

  return {
    roles,
    loading,
    error,
    refetch,
    createRole,
    dropRole,
  };
}
