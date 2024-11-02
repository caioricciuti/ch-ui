// components/UserTable/index.tsx
import React, { useState, useEffect, useCallback, useMemo } from "react";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
} from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { RefreshCcw, AlertTriangle } from "lucide-react";
import { toast } from "sonner";
import useAppStore from "@/store";
import SearchBar from "./SearchBar";
import UserTableComponent from "./UsersTable";
import DeleteUserDialog from "./DeleteUserDialog";
import ResetPasswordDialog from "./ResetPasswordDialog";
import NewPasswordDialog from "./NewPasswordDialog";
import { Skeleton } from "@/components/ui/skeleton";
import { UserData } from "@/features/admin/types";
import CreateNewUser from "../CreateUser/index"
import { generateRandomPassword } from "@/lib/utils";

const UserTable: React.FC = () => {
  const { runQuery, isAdmin } = useAppStore();
  const [users, setUsers] = useState<UserData[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [selectedUser, setSelectedUser] = useState<string | null>(null);
  const [showDeleteDialog, setShowDeleteDialog] = useState(false);
  const [showResetPasswordDialog, setShowResetPasswordDialog] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [refreshTrigger, setRefreshTrigger] = useState(0);
  const [searchQuery, setSearchQuery] = useState("");
  const [newPassword, setNewPassword] = useState<string | null>(null);

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

  const handleDeleteUser = async (username: string) => {
    if (!isAdmin) {
      toast.error("You need administrator privileges to delete users");
      return;
    }

    setDeleting(true);
    try {
      await runQuery(`REVOKE ALL PRIVILEGES ON *.* FROM ${username}`);
      await runQuery(`DROP USER IF EXISTS ${username}`);
      toast.success(`User ${username} deleted successfully`);
      setRefreshTrigger((prev) => prev + 1);
    } catch (error: any) {
      const errorMessage = error.message || "Failed to delete user";
      setError(errorMessage);
      toast.error(errorMessage);
    } finally {
      setDeleting(false);
      setShowDeleteDialog(false);
    }
  };

  const handleRefreshPassword = async (username: string) => {
    const password = generateRandomPassword();
    setNewPassword(password); // Store the new password
    try {
      await runQuery(
        `ALTER USER ${username} IDENTIFIED WITH sha256_password BY '${password}'`
      );
      toast.success(`Password reset for ${username}`);
      setShowResetPasswordDialog(false);
    } catch (error: any) {
      const errorMessage = error.message || "Failed to reset password";
      setError(errorMessage);
      toast.error(errorMessage);
    }
  };

  const filteredUsers = useMemo(() => {
    return users.filter(
      (user) =>
        user.name.toLowerCase().includes(searchQuery.toLowerCase()) ||
        user.default_database
          ?.toLowerCase()
          .includes(searchQuery.toLowerCase()) ||
        user.default_roles_list?.some((role) =>
          role.toLowerCase().includes(searchQuery.toLowerCase())
        )
    );
  }, [users, searchQuery]);

  const TableSkeletons = () => (
    <div className="space-y-4 w-full h-[calc(100vh-300px)]">
      <Skeleton className="h-[calc(15vh-30px)] w-full" />
      <Skeleton className="h-[calc(15vh-30px)] w-full" />
      <Skeleton className="h-[calc(15vh-30px)] w-full" />
      <Skeleton className="h-[calc(15vh-30px)] w-full" />
    </div>
  );

  if (loading) {
    return <TableSkeletons />;
  }

  return (
    <Card className="border shadow-md">
      <CardHeader>
        <div className="flex items-center justify-between">
          <div>
            <CardTitle className="text-2xl font-bold">
              User Management
            </CardTitle>
            <CardDescription>
              Manage database users, roles, and permissions
            </CardDescription>
          </div>
          <div className="flex items-center gap-2">
            <Button
              variant="outline"
              size="icon"
              onClick={() => setRefreshTrigger((prev) => prev + 1)}
              className={loading ? "animate-spin" : ""}
            >
              <RefreshCcw className="h-4 w-4" />
            </Button>
            {isAdmin && (
              <CreateNewUser
                onUserCreated={() => setRefreshTrigger((prev) => prev + 1)}
              />
            )}
          </div>
        </div>
      </CardHeader>

      <CardContent>
        <SearchBar searchQuery={searchQuery} setSearchQuery={setSearchQuery} />

        {error && (
          <div className="flex items-center gap-2 bg-destructive/15 text-destructive px-4 py-2 rounded-md mb-4">
            <AlertTriangle className="h-4 w-4" />
            <span>{error}</span>
          </div>
        )}

        <UserTableComponent
          users={filteredUsers}
          setSelectedUser={setSelectedUser}
          setShowDeleteDialog={setShowDeleteDialog}
          setShowResetPasswordDialog={setShowResetPasswordDialog}
        />
      </CardContent>

      {/* Action Dialogs */}
      <ResetPasswordDialog
        open={showResetPasswordDialog}
        onOpenChange={setShowResetPasswordDialog}
        selectedUser={selectedUser}
        onResetPassword={handleRefreshPassword}
      />

      <NewPasswordDialog
        open={!!newPassword}
        onOpenChange={() => setNewPassword(null)}
        selectedUser={selectedUser}
        newPassword={newPassword}
      />

      <DeleteUserDialog
        open={showDeleteDialog}
        onOpenChange={setShowDeleteDialog}
        selectedUser={selectedUser}
        onDeleteUser={handleDeleteUser}
        deleting={deleting}
      />
    </Card>
  );
};

export default UserTable;
