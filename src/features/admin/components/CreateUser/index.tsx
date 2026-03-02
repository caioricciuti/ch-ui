// components/CreateNewUser/index.tsx
import React, { useState } from "react";
import { Button } from "@/components/ui/button";
import { ArrowLeft } from "lucide-react";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { toast } from "sonner";
import { useForm } from "@tanstack/react-form";
import useAppStore from "@/store";
import { format } from "date-fns";
import { generateRandomPassword } from "@/lib/utils";
import AuthenticationSection from "./AuthenticationSection";
import AccessControlSection from "./AccessControlSection";
import DatabaseRolesSection from "./DatabaseRolesSection";
import PrivilegesSection from "./PrivilegesSection";
import SettingsSection from "./SettingsSection";
import useMetadata from "./hooks/useMetadata";
import {
  GrantedPermission,
  findPermissionById,
  findParentId,
  formatScope,
} from "./PrivilegesSection/permissions";
import { PendingChange } from "../PermissionsConfig/types";
import { escapeIdentifier, escapeStringLiteral, formatScopeSQL } from "@/features/admin/utils/sqlEscape";

interface CreateNewUserProps {
  onBack: () => void;
  onUserCreated: () => void;
  onAddChange: (change: Omit<PendingChange, "id" | "createdAt">) => void;
}

interface CreateUserFormValues {
  username: string;
  password: string;
  hostType: string;
  hostValue: string;
  validUntil: Date | undefined;
  roles: string[];
  defaultDatabase: string;
  grantees: string;
  settings: {
    profile: string;
    readonly: boolean;
  };
  privileges: {
    grants: GrantedPermission[];
  };
}

const CreateNewUser: React.FC<CreateNewUserProps> = ({ onBack, onUserCreated, onAddChange }) => {
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const [onCluster, setOnCluster] = useState(false);
  const [clusterName, setClusterName] = useState("");

  const form = useForm({
    defaultValues: {
      username: "",
      password: "",
      hostType: "ANY",
      hostValue: "",
      validUntil: undefined,
      roles: [] as string[],
      defaultDatabase: "",
      grantees: "NONE",
      settings: {
        profile: "",
        readonly: false,
      },
      privileges: {
        grants: [] as GrantedPermission[],
      },
    },
    onSubmit: async ({ value }) => {
      try {
        setError("");
        setLoading(true);

        const statements: string[] = [];

        statements.push(buildUserCreationQuery(value));

        const grantQueries = buildGrantQueries(value.username, value);
        statements.push(...grantQueries);

        if (value.roles.length > 0) {
          const roleList = value.roles.map(r => escapeIdentifier(r)).join(", ");
          statements.push(`GRANT ${roleList} TO ${escapeIdentifier(value.username)}`);
          statements.push(`SET DEFAULT ROLE ${roleList} TO ${escapeIdentifier(value.username)}`);
        }

        if (value.settings.readonly) {
          statements.push(`ALTER USER ${escapeIdentifier(value.username)} SETTINGS READONLY=1`);
        }

        onAddChange({
          type: "CREATE",
          entityType: "USER",
          entityName: value.username,
          description: `Create user ${value.username}`,
          sqlStatements: statements,
          originalState: undefined,
          newState: { ...value },
        });

        toast.info(`User creation for ${value.username} staged for review`);
        form.reset();
        onBack();
      } catch (err: any) {
        setError(err.message || "Failed to stage user creation");
      } finally {
        setLoading(false);
      }
    },
  });

  const metadata = useMetadata(true);
  const { credential } = useAppStore();

  React.useEffect(() => {
    if (credential?.isDistributed && credential?.clusterName) {
      setOnCluster(true);
      setClusterName(credential.clusterName);
    }
  }, [credential]);

  const handleGeneratePassword = () => {
    const newPassword = generateRandomPassword();
    form.setFieldValue("password", newPassword);
  };

  const buildUserCreationQuery = (data: CreateUserFormValues) => {
    let query = `CREATE USER IF NOT EXISTS ${escapeIdentifier(data.username)}`;

    if (onCluster && clusterName) {
      query += ` ON CLUSTER ${escapeIdentifier(clusterName)}`;
    }

    query += ` IDENTIFIED WITH sha256_password BY '${escapeStringLiteral(data.password)}'`;

    if (data.hostType !== "ANY") {
      query += ` HOST ${data.hostType} '${escapeStringLiteral(data.hostValue)}'`;
    }

    if (data.validUntil) {
      query += ` VALID UNTIL '${formatDate(data.validUntil)}'`;
    }

    if (data.defaultDatabase) {
      query += ` DEFAULT DATABASE ${escapeIdentifier(data.defaultDatabase)}`;
    }

    query += ` GRANTEES ${data.grantees}`;

    if (data.settings.profile) {
      query += ` SETTINGS PROFILE '${escapeStringLiteral(data.settings.profile)}'`;
    }
    if (data.settings.readonly) {
      query += ` SETTINGS READONLY=1`;
    }

    return query;
  };

  const buildGrantQueries = (username: string, data: CreateUserFormValues) => {
    const queries: string[] = [];

    const grants: GrantedPermission[] = data.privileges.grants || [];
    if (grants.length > 0) {
      const grantedIds = new Set(grants.map(g => g.permissionId));
      const grantedSet = new Set<string>();

      for (const grant of grants) {
        const permission = findPermissionById(grant.permissionId);
        if (!permission) continue;

        const parentId = findParentId(grant.permissionId);
        if (parentId && grantedIds.has(parentId)) {
          continue;
        }

        const scopeStr = formatScopeSQL(grant.scope);
        const grantKey = `${permission.sqlPrivilege}:${scopeStr}`;

        if (grantedSet.has(grantKey)) continue;
        grantedSet.add(grantKey);

        queries.push(
          `GRANT ${permission.sqlPrivilege} ON ${scopeStr} TO ${escapeIdentifier(username)}`
        );
      }

      return queries;
    }

    return queries;
  };

  const formatDate = (date: Date) => {
    return format(date, "yyyy-MM-dd HH:mm:ss");
  };

  return (
    <div className="w-full max-w-7xl mx-auto px-3 pb-8">
      {/* Back Button */}
      <Button
        variant="ghost"
        onClick={onBack}
        className="mb-2 gap-2"
      >
        <ArrowLeft className="h-4 w-4" />
        Back to Users & Roles
      </Button>

      {/* Title */}
      <h1 className="text-3xl font-medium mb-2">Create New ClickHouse User</h1>
      <p className="text-gray-400 mb-4">
        Configure authentication, permissions, and settings for the new user.
      </p>

      {/* Form Container */}
      <form
        onSubmit={(e) => {
          e.preventDefault();
          e.stopPropagation();
          form.handleSubmit();
        }}
        className="space-y-6"
      >
        <Tabs defaultValue="general" className="w-full">
          <TabsList>
            <TabsTrigger value="general">General</TabsTrigger>
            <TabsTrigger value="privileges">Privileges</TabsTrigger>
          </TabsList>

          <TabsContent value="general" forceMount className="data-[state=inactive]:hidden">
            <div className="grid grid-cols-2 gap-6">
              {/* Authentication Section */}
              <AuthenticationSection
                form={form}
                handleGeneratePassword={handleGeneratePassword}
              />

              {/* Access Control Section */}
              <AccessControlSection form={form} />

              {/* Left column: Database & Roles + Cluster Settings */}
              <div className="space-y-6">
                {/* Database and Roles Section */}
                <DatabaseRolesSection
                  form={form}
                  roles={metadata.roles}
                  databases={metadata.databases}
                />

                {/* ON CLUSTER Settings */}
                <div className="space-y-4 border rounded-lg p-4">
                  <h3 className="text-lg font-semibold">Cluster Settings</h3>
                  <div className="flex items-center space-x-2">
                    <Checkbox
                      id="onCluster"
                      checked={onCluster}
                      onCheckedChange={(checked) => setOnCluster(!!checked)}
                    />
                    <Label htmlFor="onCluster">Create user on cluster</Label>
                  </div>

                  {onCluster && (
                    <div className="space-y-2">
                      <Label htmlFor="clusterName">Cluster Name</Label>
                      <Input
                        id="clusterName"
                        value={clusterName}
                        onChange={(e) => setClusterName(e.target.value)}
                        placeholder="Enter cluster name"
                      />
                    </div>
                  )}
                </div>
              </div>

              {/* Settings Section */}
              <SettingsSection form={form} profiles={metadata.profiles} />
            </div>
          </TabsContent>

          <TabsContent value="privileges" forceMount className="data-[state=inactive]:hidden">
            <PrivilegesSection
              form={form}
              databases={metadata.databases}
              tables={metadata.tables}
            />
          </TabsContent>
        </Tabs>

        {/* Error Alert */}
        {error && (
          <Alert variant="destructive">
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        )}

        {/* Submit Button */}
        <Button type="submit" className="w-full" disabled={loading}>
          {loading ? "Staging..." : "Stage User Creation"}
        </Button>
      </form>
    </div>
  );
};

export default CreateNewUser;
