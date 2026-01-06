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
  Edit3,
} from "lucide-react";
import TooltipWrapper from "./TooltipWrapper";
import { UserData } from "../../types";

interface UsersTableProps {
  users: UserData[];
  setSelectedUser: (username: string | null) => void;
  setShowDeleteDialog: (show: boolean) => void;
  setShowResetPasswordDialog: (show: boolean) => void;
  onEditUser: (user: UserData) => void;
}

const UsersTable: React.FC<UsersTableProps> = ({
  users,
  setSelectedUser,
  setShowDeleteDialog,
  setShowResetPasswordDialog,
  onEditUser,
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

  const getEffectiveRole = (user: UserData) => {
    // If we have explicit RBAC roles, show them
    if (user.default_roles_list && user.default_roles_list.length > 0) {
      return user.default_roles_list.map((role) => ({ label: role, variant: "secondary" as const, tooltip: "RBAC Role" }));
    }

    // Infer from Grants
    const grantsArray = user.grants || [];
    const grantsString = grantsArray.join(" ").toUpperCase();

    // Check for Admin (ALL on *.* OR System-level privileges)
    // - GRANT ALL ...
    // - ROLE ADMIN
    // - CREATE USER (implies user management)
    const isAdmin =
      /GRANT\s+(ALL|ALL\s+PRIVILEGES)\s+ON\s+\*\.\*/.test(grantsString) ||
      grantsString.includes("ROLE ADMIN") ||
      (grantsString.includes("CREATE USER") && grantsString.includes("DROP USER"));

    if (isAdmin) {
      return [{ label: "Administrator", variant: "default" as const, tooltip: "Full System Access" }];
    }

    // Check for Developer / Read Write / Read Only patterns
    if (grantsString.includes("CREATE") && grantsString.includes("DROP")) {
      return [{ label: "Developer", variant: "secondary" as const, tooltip: "Can Create/Drop Tables" }];
    }
    if (grantsString.includes("INSERT")) {
      return [{ label: "Read-Write", variant: "outline" as const, tooltip: "SELECT + INSERT" }];
    }
    if (grantsString.includes("SELECT")) {
      return [{ label: "Read-Only", variant: "secondary" as const, tooltip: "SELECT Only" }];
    }

    // Default fallback
    const tooltipText = grantsArray.length > 0 ? grantsArray.join(", ").slice(0, 100) + "..." : "No Explicit Grants";
    return [{ label: "Custom Access", variant: "outline" as const, tooltip: tooltipText }];
  };

  return (
    <div className="rounded-md border">
      <Table className="min-w-[1000px]">
        <TableHeader>
          <TableRow>
            <TableHead className="w-[200px]">User</TableHead>
            <TableHead>Authentication</TableHead>
            <TableHead>Effective Access</TableHead>
            <TableHead>Host Access</TableHead>
            <TableHead className="w-[100px]">Actions</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {users.map((user) => {
            const effectiveRoles = getEffectiveRole(user);

            return (
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
                      {effectiveRoles.map((role, idx) => (
                        <TooltipWrapper key={idx} content={role.tooltip}>
                          <Badge variant={role.variant} className={`cursor-help ${role.label === "Administrator" ? "bg-purple-500 hover:bg-purple-600" : ""}`}>
                            {role.label}
                          </Badge>
                        </TooltipWrapper>
                      ))}
                    </div>

                    {/* Access Scope Display */}
                    <div className="flex items-center gap-2 text-sm text-muted-foreground">
                      <Database className="h-3 w-3" />
                      {(() => {
                        const grantsString = (user.grants || []).join(" ").toUpperCase();
                        // Admin Check
                        const isAdmin =
                          /GRANT\s+(ALL|ALL\s+PRIVILEGES)\s+ON\s+\*\.\*/.test(grantsString) ||
                          grantsString.includes("ROLE ADMIN") ||
                          (grantsString.includes("CREATE USER") && grantsString.includes("DROP USER"));

                        if (isAdmin) return "All Databases";

                        // Parse specific databases
                        const dbs = new Set<string>();
                        (user.grants || []).forEach(g => {
                          // Match ON db.* or ON "db".*
                          const match = g.match(/ON\s+"?([a-zA-Z0-9_]+)"?\.(\*|\S+)/);
                          if (match && match[1] && match[1] !== '*') {
                            dbs.add(match[1]);
                          }
                        });

                        const dbList = Array.from(dbs);
                        if (dbList.length > 0) return dbList.join(", ");

                        // Fallback to default DB if no explicit grants found (e.g. minimal user)
                        return user.default_database || "default";
                      })()}
                    </div>
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
                      <DropdownMenuItem
                        className="gap-2"
                        onClick={() => {
                          onEditUser(user);
                        }}
                      >
                        <Edit3 className="h-4 w-4" />
                        Edit User
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
            );
          })}
        </TableBody>
      </Table>
    </div>
  );
};

export default UsersTable;
