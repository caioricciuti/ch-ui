import React from "react";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  MoreHorizontal,
  KeyRound,
  Database,
  Network,
  Users,
  Lock,
  Settings,
  AlertTriangle,
} from "lucide-react";
import useAppStore from "@/store";
import { toast } from "sonner";
import CreateNewUser from "./CreateNewUser";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { generateRandomPassword } from "@/lib/utils";

interface UserData {
  name: string;
  id: string;
  auth_type: string[];
  host_ip: string[];
  host_names: string[];
  host_names_regexp: string[];
  host_names_like: string[];
  default_roles_all: number;
  default_roles_list: string[];
  default_database: string;
  grantees_any: number;
  grantees_list: string[];
  grants?: string[];
  settings_profile?: string;
  readonly?: boolean;
}

interface CreateNewUserProps {
  onUserCreated: () => void;
}

const UserData: React.FC = () => {
  const { runQuery, isAdmin } = useAppStore();
  const [users, setUsers] = React.useState<UserData[]>([]);
  const [loading, setLoading] = React.useState(true);
  const [error, setError] = React.useState("");
  const [selectedUser, setSelectedUser] = React.useState<string | null>(null);
  const [showDeleteDialog, setShowDeleteDialog] = React.useState(false);
  const [deleting, setDeleting] = React.useState(false);
  const [refreshTrigger, setRefreshTrigger] = React.useState(0);

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

  const fetchUsers = React.useCallback(async () => {
    try {
      setLoading(true);
      setError("");

      // Fetch basic user information
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

      // Enhance user data with grants and settings
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

  React.useEffect(() => {
    fetchUsers();
  }, [fetchUsers, refreshTrigger]);

  const getHostAccess = (user: UserData) => {
    if (user.host_ip?.length) return `IP: ${user.host_ip.join(", ")}`;
    if (user.host_names?.length) return `Hosts: ${user.host_names.join(", ")}`;
    if (user.host_names_regexp?.length)
      return `Regexp: ${user.host_names_regexp.join(", ")}`;
    if (user.host_names_like?.length)
      return `Like: ${user.host_names_like.join(", ")}`;
    return "Any Host";
  };

  const getAuthType = (types: string[]) => {
    if (!types) return [];
    return types.map((type) =>
      type.replace(/'([^']+)'.*/, "$1").replace(/_/g, " ")
    );
  };

  const handleDeleteUser = async (username: string) => {
    if (!isAdmin) {
      toast.error("You need administrator privileges to delete users");
      return;
    }

    setDeleting(true);
    try {
      // First revoke all privileges
      await runQuery(`REVOKE ALL PRIVILEGES ON *.* FROM ${username}`);
      // Then drop the user
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

  const refreshUserPassword = async (username: string) => {
    try {
      const password = generateRandomPassword();
      await runQuery(`ALTER USER ${username} SET PASSWORD = '${password}'`);
      toast.success(`Password reset for ${username}`);
      setRefreshTrigger((prev) => prev + 1);
    } catch (error: any) {
      const errorMessage = error.message || "Failed to reset password";
      setError(errorMessage);
      toast.error(errorMessage);
    }
  };

  if (loading) {
    return (
      <>
        <Skeleton className="w-full h-20" />
        <Skeleton className="w-full h-20" />
        <Skeleton className="w-full h-20" />
        <Skeleton className="w-full h-20" />
      </>
    );
  }

  return (
    <Card className="space-y-4 p-4 h-96 overflow-auto">
      {error && (
        <div className="flex items-center gap-2 bg-destructive/15 text-destructive px-4 py-2 rounded-md">
          <AlertTriangle className="h-4 w-4" />
          <span>{error}</span>
        </div>
      )}

      <div className="flex justify-between items-center">
        <h2 className="text-lg font-semibold">User Management</h2>
        {isAdmin && (
          <CreateNewUser
            onUserCreated={() => setRefreshTrigger((prev) => prev + 1)}
          />
        )}
      </div>

      <Table className="min-w-[1000px]">
        <TableHeader>
          <TableRow>
            <TableHead className="w-[200px]">User</TableHead>
            <TableHead>Authentication</TableHead>
            <TableHead>Roles & Database</TableHead>
            <TableHead>Access</TableHead>
            <TableHead>Status</TableHead>
            <TableHead className="w-[100px]">Actions</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {users.map((user) => (
            <TableRow key={user.name}>
              <TableCell className="font-medium">
                <div className="flex items-center space-x-2">
                  <KeyRound className="h-4 w-4" />
                  <span>{user.name}</span>
                </div>
              </TableCell>
              <TableCell>
                <div className="flex flex-wrap gap-1">
                  {getAuthType(user.auth_type).map((type, idx) => (
                    <Badge
                      key={idx}
                      variant="outline"
                      className="flex items-center"
                    >
                      {type}
                    </Badge>
                  ))}
                </div>
              </TableCell>
              <TableCell>
                <div className="space-y-1">
                  <div className="flex flex-wrap gap-1">
                    {user.default_roles_all === 1 ? (
                      <Badge variant="default" className="flex items-center">
                        All Roles
                      </Badge>
                    ) : user.default_roles_list?.length > 0 ? (
                      user.default_roles_list.map((role, idx) => (
                        <Badge
                          key={idx}
                          variant="outline"
                          className="flex items-center"
                        >
                          {role}
                        </Badge>
                      ))
                    ) : (
                      <span className="text-muted-foreground">No roles</span>
                    )}
                  </div>
                  <div className="flex items-center gap-2 text-sm text-muted-foreground">
                    <Database className="h-3 w-3" />
                    {user.default_database || "No default database"}
                  </div>
                </div>
              </TableCell>
              <TableCell>
                <div className="flex items-center space-x-2">
                  <Network className="h-4 w-4" />
                  <span className="text-sm">{getHostAccess(user)}</span>
                </div>
              </TableCell>
              <TableCell>
                <div className="flex flex-wrap gap-2">
                  {user.readonly && (
                    <Badge variant="secondary" className="flex items-center">
                      <Lock className="h-3 w-3 mr-1" />
                      Read-only
                    </Badge>
                  )}
                  {user.settings_profile && (
                    <Badge variant="outline" className="flex items-center">
                      <Settings className="h-3 w-3 mr-1" />
                      {user.settings_profile}
                    </Badge>
                  )}
                  {user.grantees_any === 1 && (
                    <Badge variant="secondary" className="flex items-center">
                      <Users className="h-3 w-3 mr-1" />
                      Can Grant
                    </Badge>
                  )}
                </div>
              </TableCell>
              <TableCell>
                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <Button variant="ghost" className="h-8 w-8 p-0">
                      <MoreHorizontal className="h-4 w-4" />
                    </Button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end">
                    <DropdownMenuItem
                      onClick={() => {
                        setSelectedUser(user.name);
                        setShowDeleteDialog(true);
                      }}
                      className="text-destructive"
                    >
                      <AlertTriangle className="h-4 w-4 mr-2" />
                      Delete User
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>

      <Dialog open={showDeleteDialog} onOpenChange={setShowDeleteDialog}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete User</DialogTitle>
            <DialogDescription>
              Are you sure you want to delete the user "{selectedUser}"? This
              action cannot be undone and will revoke all privileges.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setShowDeleteDialog(false)}
              disabled={deleting}
            >
              Cancel
            </Button>
            <Button
              variant="destructive"
              onClick={() => selectedUser && handleDeleteUser(selectedUser)}
              disabled={deleting}
            >
              {deleting ? "Deleting..." : "Delete User"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Card>
  );
};

export default UserData;
