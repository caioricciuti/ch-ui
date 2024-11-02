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
  DropdownMenuLabel,
  DropdownMenuSeparator,
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
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
} from "@/components/ui/card";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  MoreHorizontal,
  KeyRound,
  Database,
  Network,
  Users,
  Lock,
  Settings,
  AlertTriangle,
  Search,
  RefreshCcw,
  Key,
  Trash,
} from "lucide-react";
import useAppStore from "@/store";
import { toast } from "sonner";
import CreateNewUser from "./CreateNewUser";
import { Skeleton } from "@/components/ui/skeleton";
import { generateRandomPassword } from "@/lib/utils";
import { UserData } from "../types";

const UserTable: React.FC = () => {
  const { runQuery, isAdmin } = useAppStore();
  const [users, setUsers] = React.useState<UserData[]>([]);
  const [loading, setLoading] = React.useState(true);
  const [error, setError] = React.useState("");
  const [selectedUser, setSelectedUser] = React.useState<string | null>(null);
  const [showDeleteDialog, setShowDeleteDialog] = React.useState(false);
  const [showResetPasswordDialog, setShowResetPasswordDialog] =
    React.useState(false);
  const [deleting, setDeleting] = React.useState(false);
  const [refreshTrigger, setRefreshTrigger] = React.useState(0);
  const [searchQuery, setSearchQuery] = React.useState("");
  const [newPassword, setNewPassword] = React.useState<string | null>(null);

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
    if (!Array.isArray(types)) return [];
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
      setRefreshTrigger((prev) => prev + 1);
      setShowResetPasswordDialog(false);
    } catch (error: any) {
      const errorMessage = error.message || "Failed to reset password";
      setError(errorMessage);
      toast.error(errorMessage);
    }
  };

  const filteredUsers = React.useMemo(() => {
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
    <TooltipProvider>
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
          <div className="flex items-center space-x-2 mb-4">
            <div className="relative flex-1">
              <Search className="absolute left-2 top-2.5 h-4 w-4 text-muted-foreground" />
              <Input
                placeholder="Search users, roles, or databases..."
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                className="pl-8"
              />
            </div>
          </div>

          {error && (
            <div className="flex items-center gap-2 bg-destructive/15 text-destructive px-4 py-2 rounded-md mb-4">
              <AlertTriangle className="h-4 w-4" />
              <span>{error}</span>
            </div>
          )}

          <ScrollArea className="h-[calc(100vh-300px)] rounded-md border">
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
                {filteredUsers.map((user) => (
                  <TableRow key={user.name} className="group hover:bg-muted/50">
                    <TableCell className="font-medium">
                      <div className="flex items-center space-x-2">
                        <div className="bg-primary/10 p-2 rounded-full">
                          <KeyRound className="h-4 w-4 text-primary" />
                        </div>
                        <span>{user.name}</span>
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className="flex flex-wrap gap-1">
                        {getAuthType(user.auth_type).map((type, idx) => (
                          <Badge
                            key={idx}
                            variant="outline"
                            className="bg-primary/5"
                          >
                            {type}
                          </Badge>
                        ))}
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className="space-y-2">
                        <div className="flex flex-wrap gap-1">
                          {user.default_roles_all === 1 ? (
                            <Badge variant="default" className="bg-primary">
                              All Roles
                            </Badge>
                          ) : user.default_roles_list?.length > 0 ? (
                            user.default_roles_list.map((role, idx) => (
                              <Badge key={idx} variant="secondary">
                                {role}
                              </Badge>
                            ))
                          ) : (
                            <span className="text-muted-foreground">
                              No roles
                            </span>
                          )}
                        </div>
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <div className="flex items-center gap-2 text-sm text-muted-foreground cursor-help">
                              <Database className="h-3 w-3" />
                              {user.default_database || "No default database"}
                            </div>
                          </TooltipTrigger>
                          <TooltipContent>
                            <p>Default Database</p>
                          </TooltipContent>
                        </Tooltip>
                      </div>
                    </TableCell>
                    <TableCell>
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <div className="flex items-center space-x-2">
                            <Network className="h-4 w-4 text-muted-foreground" />
                            <span className="text-sm">
                              {getHostAccess(user)}
                            </span>
                          </div>
                        </TooltipTrigger>
                        <TooltipContent>
                          <p>Host Access Configuration</p>
                        </TooltipContent>
                      </Tooltip>
                    </TableCell>
                    <TableCell>
                      <div className="flex flex-wrap gap-2">
                        {user.readonly && (
                          <Badge variant="secondary" className="gap-1">
                            <Lock className="h-3 w-3" />
                            Read-only
                          </Badge>
                        )}
                        {user.settings_profile && (
                          <Badge variant="outline" className="gap-1">
                            <Settings className="h-3 w-3" />
                            {user.settings_profile}
                          </Badge>
                        )}
                        {user.grantees_any === 1 && (
                          <Badge variant="secondary" className="gap-1">
                            <Users className="h-3 w-3" />
                            Can Grant
                          </Badge>
                        )}
                      </div>
                    </TableCell>
                    <TableCell>
                      <DropdownMenu>
                        <DropdownMenuTrigger asChild>
                          <Button
                            variant="ghost"
                            className="h-8 w-8 p-0 opacity-0 group-hover:opacity-100"
                          >
                            <MoreHorizontal className="h-4 w-4" />
                          </Button>
                        </DropdownMenuTrigger>
                        <DropdownMenuContent align="end" className="w-48">
                          <DropdownMenuLabel>User Actions</DropdownMenuLabel>
                          <DropdownMenuSeparator />
                          <DropdownMenuItem
                            className="gap-2"
                            onClick={() => {
                              setSelectedUser(user.name);
                              setShowResetPasswordDialog(true);
                            }}
                          >
                            <Key className="h-4 w-4" />
                            Reset Password
                          </DropdownMenuItem>
                          <DropdownMenuSeparator />
                          <DropdownMenuItem
                            className="gap-2 text-destructive"
                            onClick={() => {
                              setSelectedUser(user.name);
                              setShowDeleteDialog(true);
                            }}
                          >
                            <Trash className="h-4 w-4" />
                            Delete User
                          </DropdownMenuItem>
                        </DropdownMenuContent>
                      </DropdownMenu>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </ScrollArea>
        </CardContent>
      </Card>

      {/* Reset Password Confirmation Dialog */}
      <Dialog
        open={showResetPasswordDialog}
        onOpenChange={setShowResetPasswordDialog}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Reset Password</DialogTitle>
            <DialogDescription>
              Are you sure you want to reset the password for "{selectedUser}"?
              This action will generate a new password.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setShowResetPasswordDialog(false)}
            >
              Cancel
            </Button>
            <Button
              variant="outline"
              onClick={() =>
                selectedUser && handleRefreshPassword(selectedUser)
              }
            >
              Reset Password
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* New Password Display Dialog */}
      <Dialog open={!!newPassword} onOpenChange={() => setNewPassword(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Password Reset Successful</DialogTitle>
            <DialogDescription>
              The new password for "{selectedUser}" is:{" "}
              <strong>{newPassword}</strong>
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setNewPassword(null)}>
              Close
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Delete Confirmation Dialog */}
      <Dialog open={showDeleteDialog} onOpenChange={setShowDeleteDialog}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete User</DialogTitle>
            <DialogDescription>
              Are you sure you want to delete the user "{selectedUser}"? This
              action cannot be undone.
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
    </TooltipProvider>
  );
};

export default UserTable;
