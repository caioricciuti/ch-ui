import React, { useState, useEffect } from "react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { toast } from "sonner";
import { Role } from "../CreateUser/PrivilegesSection/types";
import { GrantedPermission } from "../CreateUser/PrivilegesSection/permissions";
import PrivilegesPanel from "../CreateUser/PrivilegesSection/PrivilegesPanel";
import { useGrants } from "../PermissionsConfig/hooks/useGrants";
import { useSqlGenerator } from "../PermissionsConfig/hooks/useSqlGenerator";
import useMetadata from "../CreateUser/hooks/useMetadata";
import { PendingChange } from "../PermissionsConfig/types";
import { escapeIdentifier } from "@/features/admin/utils/sqlEscape";

interface RoleEditorProps {
  role: Role | null;
  isCreating: boolean;
  onClose: () => void;
  onAddChange: (change: Omit<PendingChange, "id" | "createdAt">) => void;
}

const RoleEditor: React.FC<RoleEditorProps> = ({ role, isCreating, onClose, onAddChange }) => {
  const [roleName, setRoleName] = useState(role?.name || "");
  const [grants, setGrants] = useState<GrantedPermission[]>([]);
  const [loading, setLoading] = useState(false);

  const metadata = useMetadata(true);
  const { grants: existingGrants, loading: grantsLoading } = useGrants({
    roleName: role?.name,
  });
  const { generateGrant, generateRevoke } = useSqlGenerator();

  // Load existing grants when editing
  useEffect(() => {
    if (existingGrants && !isCreating) {
      setGrants(existingGrants);
    }
  }, [existingGrants, isCreating]);

  const handleSave = async () => {
    if (!roleName.trim()) {
      toast.error("Role name is required");
      return;
    }

    setLoading(true);

    try {
      const sqlStatements: string[] = [];

      if (isCreating) {
        // Create role
        sqlStatements.push(`CREATE ROLE IF NOT EXISTS ${escapeIdentifier(roleName)}`);

        // Grant privileges
        for (const grant of grants) {
          const grantSql = generateGrant(grant, roleName);
          if (grantSql) {
            sqlStatements.push(...grantSql);
          }
        }

        onAddChange({
          type: "CREATE",
          entityType: "ROLE",
          entityName: roleName,
          description: `Create role: ${roleName} with ${grants.length} privileges`,
          sqlStatements,
        });

        toast.success(`Role ${roleName} staged for creation`);
      } else {
        // Compare grants and generate GRANT/REVOKE statements
        const existingGrantIds = new Set(
          existingGrants.map((g) => `${g.permissionId}:${JSON.stringify(g.scope)}`)
        );
        const newGrantIds = new Set(
          grants.map((g) => `${g.permissionId}:${JSON.stringify(g.scope)}`)
        );

        // Revoke removed grants
        for (const grant of existingGrants) {
          const grantId = `${grant.permissionId}:${JSON.stringify(grant.scope)}`;
          if (!newGrantIds.has(grantId)) {
            const revokeSql = generateRevoke(grant, roleName);
            if (revokeSql) {
              sqlStatements.push(...revokeSql);
            }
          }
        }

        // Grant new grants
        for (const grant of grants) {
          const grantId = `${grant.permissionId}:${JSON.stringify(grant.scope)}`;
          if (!existingGrantIds.has(grantId)) {
            const grantSql = generateGrant(grant, roleName);
            if (grantSql) {
              sqlStatements.push(...grantSql);
            }
          }
        }

        if (sqlStatements.length > 0) {
          onAddChange({
            type: "ALTER",
            entityType: "ROLE",
            entityName: roleName,
            description: `Update privileges for role: ${roleName}`,
            sqlStatements,
          });

          toast.success(`Changes to role ${roleName} staged for review`);
        } else {
          toast.info("No changes to apply");
        }
      }

      onClose();
    } catch (err: any) {
      toast.error(err.message || "Failed to save role");
    } finally {
      setLoading(false);
    }
  };

  return (
    <Dialog open={true} onOpenChange={onClose}>
      <DialogContent className="max-w-6xl max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{isCreating ? "Create New Role" : `Edit Role: ${role?.name}`}</DialogTitle>
          <DialogDescription>
            {isCreating
              ? "Create a new role and configure its privileges."
              : "Modify the privileges granted to this role."}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-6 py-4">
          {isCreating && (
            <div className="space-y-2">
              <Label htmlFor="roleName">Role Name</Label>
              <Input
                id="roleName"
                value={roleName}
                onChange={(e) => setRoleName(e.target.value)}
                placeholder="Enter role name (e.g., analyst, readonly)"
                disabled={!isCreating}
              />
            </div>
          )}

          <Tabs defaultValue="privileges" className="w-full">
            <TabsList>
              <TabsTrigger value="privileges">Privileges</TabsTrigger>
            </TabsList>

            <TabsContent value="privileges" className="space-y-4">
              <p className="text-sm text-muted-foreground">
                Configure the privileges that will be granted to users assigned this role.
              </p>

              {grantsLoading ? (
                <p className="text-sm text-muted-foreground">Loading privileges...</p>
              ) : (
                <PrivilegesPanel
                  databases={metadata.databases}
                  tables={metadata.tables}
                  grants={grants}
                  onChange={setGrants}
                />
              )}
            </TabsContent>
          </Tabs>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={onClose} disabled={loading}>
            Cancel
          </Button>
          <Button onClick={handleSave} disabled={loading}>
            {loading ? "Saving..." : isCreating ? "Create Role" : "Save Changes"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};

export default RoleEditor;
