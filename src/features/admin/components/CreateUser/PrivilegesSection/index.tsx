import React, { useMemo, useCallback } from "react";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Info, Link2 } from "lucide-react";
import { useStore } from "@tanstack/react-form";
import { GrantedPermission } from "./permissions";
import { ExtendedGrantedPermission, RoleAssignment } from "./types";
import PrivilegesPanel from "./PrivilegesPanel";

const EMPTY_GRANTS: GrantedPermission[] = [];

interface PrivilegesSectionProps {
  form: any;
  databases?: string[];
  tables?: Map<string, string[]>;
  effectiveGrants?: ExtendedGrantedPermission[];
  assignedRoles?: RoleAssignment[];
  showRoleSource?: boolean;
}

const PrivilegesSection: React.FC<PrivilegesSectionProps> = ({
  form,
  databases = [],
  tables = new Map(),
  effectiveGrants,
  assignedRoles,
  showRoleSource = false,
}) => {
  const handleGrantsChange = useCallback((grants: GrantedPermission[]) => {
    form.setFieldValue("privileges.grants", grants);
  }, [form]);

  const watchedGrants = useStore(form.store, (s: any) => s.values.privileges.grants);
  const currentGrants = useMemo(() => {
    return watchedGrants && watchedGrants.length > 0 ? watchedGrants : EMPTY_GRANTS;
  }, [watchedGrants]);

  return (
    <Card>
      <CardHeader>
        <CardTitle>Privileges</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="text-sm text-muted-foreground">
          Manually configure direct privileges, or assign roles in the Database and Roles section above.
        </div>

        {/* Role Info Banner */}
        {assignedRoles && assignedRoles.length > 0 && (
          <Alert>
            <Info className="h-4 w-4" />
            <AlertDescription className="space-y-2">
              <div className="flex items-center gap-2">
                <span className="font-medium">Assigned Roles:</span>
                <div className="flex flex-wrap gap-2">
                  {assignedRoles.map((role) => (
                    <Badge key={role.roleName} variant="secondary" className="flex items-center gap-1">
                      {role.roleName}
                      {role.adminOption && <span className="text-xs">(admin)</span>}
                    </Badge>
                  ))}
                </div>
              </div>
              <p className="text-xs text-muted-foreground">
                Privileges from these roles are shown with <Link2 className="inline h-3 w-3" /> badge
                and cannot be modified here. Edit the role instead.
              </p>
              <div className="flex items-center gap-4 text-xs">
                <div className="flex items-center gap-2">
                  <div className="w-3 h-3 rounded bg-muted/50 border" />
                  <span>Direct grant</span>
                </div>
                <div className="flex items-center gap-2">
                  <div className="w-3 h-3 rounded bg-blue-50 dark:bg-blue-950/20 border" />
                  <span>Role-inherited</span>
                </div>
              </div>
            </AlertDescription>
          </Alert>
        )}

        <PrivilegesPanel
          databases={databases}
          tables={tables}
          grants={currentGrants}
          onChange={handleGrantsChange}
          effectiveGrants={effectiveGrants}
          showRoleSource={showRoleSource}
        />
      </CardContent>
    </Card>
  );
};

export default PrivilegesSection;
