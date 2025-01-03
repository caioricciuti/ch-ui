// components/CreateNewUser/DatabaseRolesSection.tsx
import React from "react";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { FormField, FormItem, FormLabel, FormControl, FormMessage } from "@/components/ui/form";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Badge } from "@/components/ui/badge";
import { X } from "lucide-react";

interface DatabaseRolesSectionProps {
  form: any;
  roles: string[];
  databases: string[];
}

const DatabaseRolesSection: React.FC<DatabaseRolesSectionProps> = ({ form, roles, databases }) => {
  const grantDatabases = form.watch("grantDatabases") || [];

  return (
    <Card>
      <CardHeader>
        <CardTitle>Database and Roles</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        {/* Default Role */}
        <FormField
          control={form.control}
          name="defaultRole"
          render={({ field }) => (
            <FormItem>
              <FormLabel>Default Role</FormLabel>
              <Select onValueChange={field.onChange} defaultValue={field.value}>
                <FormControl>
                  <SelectTrigger>
                    <SelectValue placeholder="Select default role" />
                  </SelectTrigger>
                </FormControl>
                <SelectContent>
                  {roles.map((role) => (
                    <SelectItem key={role} value={role}>
                      {role}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <FormMessage />
            </FormItem>
          )}
        />

        {/* Default Database */}
        <FormField
          control={form.control}
          name="defaultDatabase"
          render={({ field }) => (
            <FormItem>
              <FormLabel>Default Database</FormLabel>
              <Select onValueChange={field.onChange} defaultValue={field.value}>
                <FormControl>
                  <SelectTrigger>
                    <SelectValue placeholder="Select default database" />
                  </SelectTrigger>
                </FormControl>
                <SelectContent>
                  {databases.map((db) => (
                    <SelectItem key={db} value={db}>
                      {db}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <FormMessage />
            </FormItem>
          )}
        />

        {/* Grant Access to Databases */}
        <FormField
          control={form.control}
          name="grantDatabases"
          render={({ field }) => (
            <FormItem>
              <FormLabel>Grant Access to Databases</FormLabel>
              <Select
                onValueChange={(value: string) => {
                  if (!grantDatabases.includes(value)) {
                    form.setValue("grantDatabases", [...grantDatabases, value]);
                  }
                }}
              >
                <FormControl>
                  <SelectTrigger>
                    <SelectValue placeholder="Select databases to grant access" />
                  </SelectTrigger>
                </FormControl>
                <SelectContent>
                  {databases.map((db) => (
                    <SelectItem
                      key={db}
                      value={db}
                      disabled={grantDatabases.includes(db)}
                    >
                      {db}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <div className="flex flex-wrap gap-2 mt-2">
                {grantDatabases.map((db: string) => (
                  <Badge
                    key={db}
                    variant="secondary"
                    className="hover:bg-destructive hover:text-destructive-foreground cursor-pointer"
                    onClick={() =>
                      form.setValue(
                        "grantDatabases",
                        grantDatabases.filter((v: string) => v !== db)
                      )
                    }
                  >
                    {db}
                    <X className="ml-1 h-3 w-3" />
                  </Badge>
                ))}
              </div>
              <FormMessage />
            </FormItem>
          )}
        />
      </CardContent>
    </Card>
  );
};

export default DatabaseRolesSection;
