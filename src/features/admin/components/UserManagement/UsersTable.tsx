// components/UserTable/UsersTable.tsx
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
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  MoreHorizontal,
  Key,
  Trash,
  Lock,
  Settings,
  Users,
  Network,
  Database,
  KeyRound,
} from "lucide-react";
import TooltipWrapper from "./TooltipWrapper";
import { UserData } from "../../types";

interface UsersTableProps {
  users: UserData[];
  setSelectedUser: (username: string | null) => void;
  setShowDeleteDialog: (show: boolean) => void;
  setShowResetPasswordDialog: (show: boolean) => void;
}

const UsersTable: React.FC<UsersTableProps> = ({
  users,
  setSelectedUser,
  setShowDeleteDialog,
  setShowResetPasswordDialog,
}) => {
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

  return (
    <div className="rounded-md border">
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
                    <Badge key={idx} variant="outline" className="bg-primary/5">
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
                    ) : (user.default_roles_list && user.default_roles_list.length > 0) ? (
                      user.default_roles_list.map((role, idx) => (
                        <Badge className="" key={idx} variant="secondary">
                          {role}
                        </Badge>
                      ))
                    ) : (
                      <span className="text-muted-foreground">No roles</span>
                    )}
                  </div>
                  <TooltipWrapper content="Default Database">
                    <div className="flex items-center gap-2 text-sm text-muted-foreground cursor-help">
                      <Database className="h-3 w-3" />
                      {user.default_database || "No default database"}
                    </div>
                  </TooltipWrapper>
                </div>
              </TableCell>
              <TableCell>
                <TooltipWrapper content="Host Access Configuration">
                  <div className="flex items-center space-x-2">
                    <Network className="h-4 w-4 text-muted-foreground" />
                    <span className="text-sm">{getHostAccess(user)}</span>
                  </div>
                </TooltipWrapper>
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
    </div>
  );
};

export default UsersTable;
