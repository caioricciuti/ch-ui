// hooks/useUsers.ts
import { useState, useEffect, useCallback } from "react";
import useAppStore from "@/store";
import { toast } from "sonner";
import { UserData } from "@/features/admin/types";

const useUsers = (refreshTrigger: number) => {
  const { runQuery } = useAppStore();
  const [users, setUsers] = useState<UserData[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  const fetchUserGrants = async (username: string) => {
    try {
      const grantsResult = await runQuery(`SHOW GRANTS FOR ${username}`);
      return grantsResult.data || [];
    } catch (error) {
      console.error(`Failed to fetch grants for ${username}:`, error);
      return [];
    }
  };

  const fetchUserSettings = async (username: string) => {
    try {
      const settingsResult = await runQuery(`SHOW CREATE USER ${username}`);
      const createStatement = settingsResult.data?.[0]?.statement || "";
      return {
        profile: (createStatement.match(/SETTINGS PROFILE '([^']+)'/) || [])[1],
        readonly: createStatement.includes("READONLY=1"),
      };
    } catch (error) {
      console.error(`Failed to fetch settings for ${username}:`, error);
      return { profile: undefined, readonly: false };
    }
  };

  const fetchUsers = useCallback(async () => {
    try {
      setLoading(true);
      setError("");

      const usersResult = await runQuery(`
        SELECT 
          name, id, auth_type, host_ip, host_names,
          host_names_regexp, host_names_like, default_roles_all,
          default_roles_list, default_database, grantees_any, grantees_list
        FROM system.users
        ORDER BY name ASC
      `);

      if (usersResult.error) {
        throw new Error(usersResult.error);
      }

      const enhancedUsers = await Promise.all(
        usersResult.data.map(async (user: UserData) => {
          const [grants, settings] = await Promise.all([
            fetchUserGrants(user.name),
            fetchUserSettings(user.name),
          ]);

          return {
            ...user,
            grants,
            settings_profile: settings.profile,
            readonly: settings.readonly,
          };
        })
      );

      setUsers(enhancedUsers);
    } catch (err: any) {
      const errorMessage = err.message || "Failed to load users";
      setError(errorMessage);
      toast.error(errorMessage);
    } finally {
      setLoading(false);
    }
  }, [runQuery]);

  useEffect(() => {
    fetchUsers();
  }, [fetchUsers, refreshTrigger]);

  return { users, loading, error, refetch: fetchUsers };
};

export default useUsers;
