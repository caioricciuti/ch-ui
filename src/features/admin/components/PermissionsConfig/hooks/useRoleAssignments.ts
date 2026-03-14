import { useState, useEffect, useCallback } from "react";
import useAppStore from "@/store";
import { RoleAssignment } from "../../CreateUser/PrivilegesSection/types";
import { escapeIdentifier } from "@/features/admin/utils/sqlEscape";

interface SystemRoleGrantRow {
  granted_role_name: string;
  with_admin_option: number;
}

interface UseRoleAssignmentsResult {
  /** Role assignments for the user */
  roleAssignments: RoleAssignment[];

  /** Loading state */
  loading: boolean;

  /** Error message if any */
  error: string | null;

  /** Refetch role assignments */
  refetch: () => void;

  /** Grant a role to the user */
  grantRole: (roleName: string, adminOption: boolean) => Promise<void>;

  /** Revoke a role from the user */
  revokeRole: (roleName: string) => Promise<void>;
}

/**
 * Hook to manage role assignments for a user.
 * Provides methods to grant and revoke roles.
 *
 * @param userName - User name to manage role assignments for
 * @returns Role assignments and management methods
 */
export function useRoleAssignments(userName?: string): UseRoleAssignmentsResult {
  const { clickHouseClient } = useAppStore();
  const [roleAssignments, setRoleAssignments] = useState<RoleAssignment[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [refetchTrigger, setRefetchTrigger] = useState(0);

  const refetch = useCallback(() => {
    setRefetchTrigger((prev) => prev + 1);
  }, []);

  useEffect(() => {
    async function fetchRoleAssignments() {
      if (!clickHouseClient) {
        setError("ClickHouse client not available");
        return;
      }

      if (!userName) {
        setRoleAssignments([]);
        return;
      }

      setLoading(true);
      setError(null);

      try {
        const query = `
          SELECT
            granted_role_name,
            with_admin_option
          FROM system.role_grants
          WHERE user_name = {userName:String}
          ORDER BY granted_role_name
        `;

        const result = await clickHouseClient.query({
          query,
          query_params: {
            userName,
          },
        });

        const response = await result.json<{ data: SystemRoleGrantRow[] }>();
        const assignments: RoleAssignment[] = response.data.map((row) => ({
          roleName: row.granted_role_name,
          adminOption: row.with_admin_option === 1,
        }));

        setRoleAssignments(assignments);
      } catch (err) {
        const errorMessage =
          err instanceof Error ? err.message : "Failed to fetch role assignments";
        setError(errorMessage);
        console.error("Error fetching role assignments:", err);
      } finally {
        setLoading(false);
      }
    }

    fetchRoleAssignments();
  }, [clickHouseClient, userName, refetchTrigger]);

  const grantRole = useCallback(
    async (roleName: string, adminOption: boolean) => {
      if (!clickHouseClient || !userName) {
        throw new Error("ClickHouse client or username not available");
      }

      const adminOptionClause = adminOption ? " WITH ADMIN OPTION" : "";
      const query = `GRANT ${escapeIdentifier(roleName)} TO ${escapeIdentifier(userName)}${adminOptionClause}`;

      try {
        await clickHouseClient.command({
          query,
        });

        // Refetch to update the list
        refetch();
      } catch (err) {
        const errorMessage = err instanceof Error ? err.message : "Failed to grant role";
        console.error("Error granting role:", err);
        throw new Error(errorMessage);
      }
    },
    [clickHouseClient, userName, refetch]
  );

  const revokeRole = useCallback(
    async (roleName: string) => {
      if (!clickHouseClient || !userName) {
        throw new Error("ClickHouse client or username not available");
      }

      const query = `REVOKE ${escapeIdentifier(roleName)} FROM ${escapeIdentifier(userName)}`;

      try {
        await clickHouseClient.command({
          query,
        });

        // Refetch to update the list
        refetch();
      } catch (err) {
        const errorMessage = err instanceof Error ? err.message : "Failed to revoke role";
        console.error("Error revoking role:", err);
        throw new Error(errorMessage);
      }
    },
    [clickHouseClient, userName, refetch]
  );

  return {
    roleAssignments,
    loading,
    error,
    refetch,
    grantRole,
    revokeRole,
  };
}
