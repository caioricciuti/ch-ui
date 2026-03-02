import React, { useRef } from "react";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import {
  FormField,
  FormItem,
  FormLabel,
  FormControl,
  FormMessage,
} from "@/components/ui/form";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Combobox,
  ComboboxChips,
  ComboboxChip,
  ComboboxChipsInput,
  ComboboxContent,
  ComboboxList,
  ComboboxItem,
  ComboboxEmpty,
} from "@/components/ui/combobox";

interface DatabaseRolesSectionProps {
  form: any;
  roles: string[];
  databases: string[];
}

function RolesField({
  field,
  roles,
}: {
  field: any;
  roles: string[];
}) {
  const chipsRef = useRef<HTMLDivElement | null>(null);
  const selectedRoles: string[] = field.state.value ?? [];

  return (
    <Combobox
      multiple
      value={selectedRoles}
      onValueChange={(values: string[]) => field.handleChange(values)}
    >
      <ComboboxChips
        ref={chipsRef}
        className="min-h-10 bg-background shadow-none ring-offset-background focus-within:ring-2 focus-within:ring-ring focus-within:ring-offset-2 focus-within:ring-ring/100"
      >
        {selectedRoles.map((role) => (
          <ComboboxChip key={role}>
            {role}
          </ComboboxChip>
        ))}
        <ComboboxChipsInput placeholder="Search roles..." />
      </ComboboxChips>
      <ComboboxContent anchor={chipsRef}>
        <ComboboxList>
          {roles.map((role) => (
            <ComboboxItem key={role} value={role}>
              {role}
            </ComboboxItem>
          ))}
          <ComboboxEmpty>No roles found</ComboboxEmpty>
        </ComboboxList>
      </ComboboxContent>
    </Combobox>
  );
}

const DatabaseRolesSection: React.FC<DatabaseRolesSectionProps> = ({
  form,
  roles,
  databases,
}) => {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Database and Roles</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <FormField
          form={form}
          name="roles"
          render={({ field }) => (
            <FormItem>
              <FormLabel>Roles</FormLabel>
              <FormControl>
                <RolesField field={field} roles={roles} />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />

        <FormField
          form={form}
          name="defaultDatabase"
          render={({ field }) => (
            <FormItem>
              <FormLabel>Default Database</FormLabel>
              <Select
                onValueChange={(v) => field.handleChange(v)}
                value={field.state.value}
              >
                <FormControl>
                  <SelectTrigger>
                    <SelectValue placeholder="Select default database">
                      {field.state.value || undefined}
                    </SelectValue>
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
      </CardContent>
    </Card>
  );
};

export default DatabaseRolesSection;
