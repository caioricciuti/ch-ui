// components/UserTable/index.tsx
import React, { useState, useEffect, useCallback, useMemo } from "react";
import { AnimatePresence, motion } from "framer-motion";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
} from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { RefreshCcw, AlertTriangle, Plus } from "lucide-react";
import { toast } from "sonner";
import useAppStore from "@/store";
import SearchBar from "./SearchBar";
import UserTableComponent from "./UsersTable";
import DeleteUserDialog from "./DeleteUserDialog";
import ResetPasswordDialog from "./ResetPasswordDialog";
import NewPasswordDialog from "./NewPasswordDialog";
import { Skeleton } from "@/components/ui/skeleton";
import { UserData } from "@/features/admin/types";
import CreateNewUser from "../CreateUser/index";
import EditUser from "../CreateUser/EditUser";
import { generateRandomPassword } from "@/lib/utils";
import { PendingChange } from "../PermissionsConfig/types";
import { escapeIdentifier, escapeStringLiteral } from "@/features/admin/utils/sqlEscape";

type ViewState =
  | { mode: "list" }
  | { mode: "create" }
  | { mode: "edit"; username: string };

interface UserTableProps {
  onAddChange: (change: Omit<PendingChange, "id" | "createdAt">) => void;
  refreshTrigger?: number;
}

const UserTable: React.FC<UserTableProps> = ({ onAddChange, refreshTrigger: externalRefreshTrigger = 0 }) => {
  const { runQuery, isAdmin } = useAppStore();
  const [viewState, setViewState] = useState<ViewState>({ mode: "list" });
  const [users, setUsers] = useState<UserData[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [selectedUser, setSelectedUser] = useState<string | null>(null);
  const [showDeleteDialog, setShowDeleteDialog] = useState(false);
  const [showResetPasswordDialog, setShowResetPasswordDialog] = useState(false);
  const [refreshTrigger, setRefreshTrigger] = useState(0);
  const [searchQuery, setSearchQuery] = useState("");
  const [newPassword, setNewPassword] = useState<string | null>(null);

  const fetchUserGrants = async (username: string) => {
    try {
      const grantsResult = await runQuery(`SHOW GRANTS FOR ${escapeIdentifier(username)}`);
      return grantsResult.data || [];
    } catch (error) {
      console.error(`Failed to fetch grants for ${username}:`, error);
      return [];
    }
  };

  const fetchUserSettings = async (username: string) => {
    try {
      const settingsResult = await runQuery(`SHOW CREATE USER ${escapeIdentifier(username)}`);
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
  }, [fetchUsers, refreshTrigger, externalRefreshTrigger]);

  const handleDeleteUser = (username: string) => {
    if (!isAdmin) {
      toast.error("You need administrator privileges to delete users");
      return;
    }

    // Stage deletion change instead of executing directly
    onAddChange({
      type: "DROP",
      entityType: "USER",
      entityName: username,
      description: `Delete user ${username}`,
      sqlStatements: [
        `REVOKE ALL PRIVILEGES ON *.* FROM ${escapeIdentifier(username)}`,
        `REVOKE ALL ROLES FROM ${escapeIdentifier(username)}`,
        `DROP USER IF EXISTS ${escapeIdentifier(username)}`,
      ],
      originalState: { username },
      newState: null,
    });

    setShowDeleteDialog(false);
    toast.info(`Deletion of user ${username} staged for review`);
  };

  const handleRefreshPassword = async (username: string) => {
    const password = generateRandomPassword();
    setNewPassword(password);

    onAddChange({
      type: "ALTER",
      entityType: "USER",
      entityName: username,
      description: `Reset password for ${username}`,
      sqlStatements: [
        `ALTER USER ${escapeIdentifier(username)} IDENTIFIED WITH sha256_password BY '${escapeStringLiteral(password)}'`,
      ],
      originalState: { username },
      newState: { passwordReset: true },
    });

    toast.info(`Password reset for ${username} staged for review`);
    setShowResetPasswordDialog(false);
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

  if (loading && viewState.mode === "list") {
    return <TableSkeletons />;
  }

  return (
    <AnimatePresence mode="wait">
      {viewState.mode === "list" && (
        <motion.div
          key="list"
          initial={{ x: -20, opacity: 0 }}
          animate={{ x: 0, opacity: 1 }}
          exit={{ x: -20, opacity: 0 }}
          transition={{ duration: 0.2 }}
        >
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
                    <Button
                      className="gap-2 max-w-fit"
                      variant="outline"
                      onClick={() => setViewState({ mode: "create" })}
                    >
                      <Plus className="h-4 w-4" />
                      Create New User
                    </Button>
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
                setShowEditDialog={(username: string) =>
                  setViewState({ mode: "edit", username })
                }
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
              deleting={false}
            />
          </Card>
        </motion.div>
      )}

      {viewState.mode === "create" && (
        <motion.div
          key="create"
          initial={{ x: 20, opacity: 0 }}
          animate={{ x: 0, opacity: 1 }}
          exit={{ x: 20, opacity: 0 }}
          transition={{ duration: 0.2 }}
        >
          <CreateNewUser
            onBack={() => setViewState({ mode: "list" })}
            onUserCreated={() => {
              setRefreshTrigger((prev) => prev + 1);
              setViewState({ mode: "list" });
            }}
            onAddChange={onAddChange}
          />
        </motion.div>
      )}

      {viewState.mode === "edit" && (
        <motion.div
          key="edit"
          initial={{ x: 20, opacity: 0 }}
          animate={{ x: 0, opacity: 1 }}
          exit={{ x: 20, opacity: 0 }}
          transition={{ duration: 0.2 }}
        >
          <EditUser
            username={viewState.username}
            onBack={() => setViewState({ mode: "list" })}
            onUserUpdated={() => {
              setRefreshTrigger((prev) => prev + 1);
              setViewState({ mode: "list" });
            }}
            onAddChange={onAddChange}
          />
        </motion.div>
      )}
    </AnimatePresence>
  );
};

export default UserTable;
