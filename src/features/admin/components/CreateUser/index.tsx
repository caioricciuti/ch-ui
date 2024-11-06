// components/CreateNewUser/index.tsx
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Form } from "@/components/ui/form";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from "@/components/ui/sheet";
import { Plus } from "lucide-react";
import React, { useState } from "react";
import { useForm } from "react-hook-form";
import { toast } from "sonner";
import useAppStore from "@/store";
import { format } from "date-fns";
import { generateRandomPassword } from "@/lib/utils"

import useMetadata from "./hooks/useMetadata";
import AccessControlSection from "./AccessControlSection";
import AuthenticationSection from "./AuthenticationSection";
import DatabaseRolesSection from "./DatabaseRolesSection";
import PrivilegesSection from "./PrivilegesSection";
import SettingsSection from "./SettingsSection";

interface CreateNewUserProps {
  onUserCreated: () => void;
}

const CreateNewUser: React.FC<CreateNewUserProps> = ({ onUserCreated }) => {
  const [isOpen, setIsOpen] = useState(false);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const { runQuery } = useAppStore();

  const form = useForm({
    defaultValues: {
      username: "",
      password: "",
      hostType: "ANY",
      hostValue: "",
      validUntil: undefined,
      defaultRole: "",
      defaultDatabase: "",
      grantDatabases: [],
      grantees: "NONE",
      settings: {
        profile: "",
        readonly: false,
      },
      privileges: {
        isAdmin: false,
        allowDDL: false,
        allowInsert: false,
        allowSelect: false,
        allowAlter: false,
        allowCreate: false,
        allowDrop: false,
        allowTruncate: false,
      },
    },
  });

  const metadata = useMetadata(isOpen);

  const onSubmit = async (data: any) => {
    try {
      setError("");
      setLoading(true);

      // Validate database grants
      if (!data.privileges.isAdmin && data.grantDatabases.length === 0) {
        setError("Please select at least one database to grant access to");
        return;
      }

      // Create user
      const createUserQuery = buildUserCreationQuery(data);
      const createResult = await runQuery(createUserQuery);

      if (createResult.error) {
        setError(createResult.error);
        return;
      }

      // Grant privileges
      const grantQueries = buildGrantQueries(data.username, data);

      for (const query of grantQueries) {
        const result = await runQuery(query);
        if (result.error) {
          // If there's an error, try to clean up by dropping the user
          await runQuery(`DROP USER IF EXISTS ${data.username}`);
          setError(`Failed to grant privileges: ${result.error}`);
          return;
        }
      }

      // If readonly mode is enabled, add additional restrictions
      if (data.settings.readonly) {
        await runQuery(`ALTER USER ${data.username} SETTINGS READONLY=1`);
      }

      toast.success(`User ${data.username} created successfully`);
      onUserCreated();
      setIsOpen(false);
      form.reset();
    } catch (err: any) {
      setError(err.message || "Failed to create user");
    } finally {
      setLoading(false);
    }
  };

  const handleGeneratePassword = () => {
    const newPassword = generateRandomPassword();
    form.setValue("password", newPassword);
  };

  const buildUserCreationQuery = (data: any) => {
    let query = `CREATE USER IF NOT EXISTS ${data.username}`;

    // Add authentication
    query += ` IDENTIFIED WITH sha256_password BY '${data.password}'`;

    // Add host restrictions if specified
    if (data.hostType !== "ANY") {
      query += ` HOST ${data.hostType} '${data.hostValue}'`;
    }

    // Add validity period if specified
    if (data.validUntil) {
      query += ` VALID UNTIL '${formatDate(data.validUntil)}'`;
    }

    // Add default role if specified
    if (data.defaultRole) {
      query += ` DEFAULT ROLE ${data.defaultRole}`;
    }

    // Add default database if specified
    if (data.defaultDatabase) {
      query += ` DEFAULT DATABASE ${data.defaultDatabase}`;
    }

    // Add grantees setting
    query += ` GRANTEES ${data.grantees}`;

    // Add settings profile and readonly mode if specified
    if (data.settings.profile) {
      query += ` SETTINGS PROFILE '${data.settings.profile}'`;
    }
    if (data.settings.readonly) {
      query += ` SETTINGS READONLY=1`;
    }

    return query;
  };

  const buildGrantQueries = (username: string, data: any) => {
    const queries = [];

    if (data.privileges.isAdmin) {
      queries.push(`GRANT ALL ON *.* TO ${username} WITH GRANT OPTION`);
      return queries;
    }

    for (const db of data.grantDatabases) {
      const privileges = [];

      if (data.privileges.allowSelect) privileges.push("SELECT");
      if (data.privileges.allowInsert) privileges.push("INSERT");
      if (data.privileges.allowAlter) privileges.push("ALTER");
      if (data.privileges.allowCreate) privileges.push("CREATE");
      if (data.privileges.allowDrop) privileges.push("DROP");
      if (data.privileges.allowTruncate) privileges.push("TRUNCATE");

      if (privileges.length > 0) {
        queries.push(
          `GRANT ${privileges.join(", ")} ON ${db}.* TO ${username}`
        );
      }

      // If DDL is allowed, grant additional schema modification privileges
      if (data.privileges.allowDDL) {
        queries.push(
          `GRANT CREATE, DROP, ALTER, CREATE DATABASE ON ${db}.* TO ${username}`
        );
      }
    }

    return queries;
  };

  const formatDate = (date: Date) => {
    return format(date, "yyyy-MM-dd HH:mm:ss");
  };

  return (
    <Sheet open={isOpen} onOpenChange={setIsOpen}>
      <SheetTrigger asChild>
        <Button className="gap-2 max-w-fit" variant="outline">
          <Plus className="h-4 w-4" />
          Create New User
        </Button>
      </SheetTrigger>
      <SheetContent className="sm:max-w-xl overflow-y-auto">
        <SheetHeader>
          <SheetTitle>Create New ClickHouse User</SheetTitle>
        </SheetHeader>

        <Form {...form}>
          <form
            onSubmit={form.handleSubmit(onSubmit)}
            className="space-y-4 pt-6"
          >
            <AuthenticationSection
              form={form}
              handleGeneratePassword={handleGeneratePassword}
            />
            <AccessControlSection form={form} />
            <DatabaseRolesSection form={form} />
            <PrivilegesSection form={form} />
            <SettingsSection form={form} profiles={metadata.profiles} />

            {error && (
              <Alert variant="destructive">
                <AlertDescription>{error}</AlertDescription>
              </Alert>
            )}

            <Button type="submit" className="w-full" disabled={loading}>
              {loading ? "Creating..." : "Create User"}
            </Button>
          </form>
        </Form>
      </SheetContent>
    </Sheet>
  );
};

export default CreateNewUser;
