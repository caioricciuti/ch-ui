import React, { useState } from "react";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { MoreVertical, Edit, Trash2, Users } from "lucide-react";
import { Role } from "../CreateUser/PrivilegesSection/types";
import { PendingChange } from "../PermissionsConfig/types";
import { useGrants } from "../PermissionsConfig/hooks/useGrants";
import useAppStore from "@/store";
import { escapeIdentifier } from "@/features/admin/utils/sqlEscape";

interface RoleListProps {
  roles: Role[];
  onEditRole: (role: Role) => void;
  onAddChange: (change: Omit<PendingChange, "id" | "createdAt">) => void;
}

const RoleList: React.FC<RoleListProps> = ({ roles, onEditRole, onAddChange }) => {
  const { clickHouseClient } = useAppStore();
  const [roleToDelete, setRoleToDelete] = useState<Role | null>(null);
  const [userCounts, setUserCounts] = useState<Map<string, number>>(new Map());

  // Fetch user count for each role
  React.useEffect(() => {
    async function fetchUserCounts() {
      if (!clickHouseClient) return;

      const counts = new Map<string, number>();

      try {
        for (const role of roles) {
          const query = `
            SELECT COUNT(DISTINCT user_name) as count
            FROM system.role_grants
            WHERE granted_role_name = {roleName:String}
          `;

          const result = await clickHouseClient.query({
            query,
            query_params: {
              roleName: role.name,
            },
          });

          const response = await result.json<{ data: { count: string }[] }>();
          const count = parseInt(response.data[0]?.count || "0", 10);
          counts.set(role.name, count);
        }

        setUserCounts(counts);
      } catch (err) {
        console.error("Error fetching user counts:", err);
      }
    }

    fetchUserCounts();
  }, [roles, clickHouseClient]);

  const handleDeleteRole = async (role: Role) => {
    const sqlStatements = [`DROP ROLE IF EXISTS ${escapeIdentifier(role.name)}`];

    onAddChange({
      type: "DROP",
      entityType: "ROLE",
      entityName: role.name,
      description: `Drop role: ${role.name}`,
      sqlStatements,
    });

    setRoleToDelete(null);
  };

  if (roles.length === 0) {
    return (
      <div className="text-center py-8 text-muted-foreground">
        <p className="text-sm">No roles found. Create a role to get started.</p>
      </div>
    );
  }

  return (
    <>
      <div className="border rounded-lg">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Role Name</TableHead>
              <TableHead>ID</TableHead>
              <TableHead>Users Assigned</TableHead>
              <TableHead className="w-[100px]">Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {roles.map((role) => (
              <TableRow key={role.id}>
                <TableCell className="font-medium">{role.name}</TableCell>
                <TableCell className="font-mono text-xs text-muted-foreground">
                  {role.id}
                </TableCell>
                <TableCell>
                  <Badge variant="secondary" className="gap-1">
                    <Users className="h-3 w-3" />
                    {userCounts.get(role.name) || 0}
                  </Badge>
                </TableCell>
                <TableCell>
                  <DropdownMenu>
                    <DropdownMenuTrigger asChild>
                      <Button variant="ghost" size="sm">
                        <MoreVertical className="h-4 w-4" />
                      </Button>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end">
                      <DropdownMenuItem onClick={() => onEditRole(role)}>
                        <Edit className="mr-2 h-4 w-4" />
                        Edit Role
                      </DropdownMenuItem>
                      <DropdownMenuItem
                        onClick={() => setRoleToDelete(role)}
                        className="text-destructive"
                      >
                        <Trash2 className="mr-2 h-4 w-4" />
                        Delete Role
                      </DropdownMenuItem>
                    </DropdownMenuContent>
                  </DropdownMenu>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>

      {/* Delete Confirmation Dialog */}
      <AlertDialog open={!!roleToDelete} onOpenChange={() => setRoleToDelete(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete Role</AlertDialogTitle>
            <AlertDialogDescription>
              Are you sure you want to delete the role "{roleToDelete?.name}"? This action will
              remove all privileges granted to this role and unassign it from all users.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => roleToDelete && handleDeleteRole(roleToDelete)}
              className="bg-destructive hover:bg-destructive/90"
            >
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
};

export default RoleList;
