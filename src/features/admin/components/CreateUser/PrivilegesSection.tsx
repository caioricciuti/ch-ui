// components/CreateNewUser/PrivilegesSection.tsx
import React from "react";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Form, FormField, FormItem, FormLabel, FormControl, FormDescription } from "@/components/ui/form";
import { Checkbox } from "@/components/ui/checkbox";

interface PrivilegesSectionProps {
  form: any;
}

const PrivilegesSection: React.FC<PrivilegesSectionProps> = ({ form }) => {
  const isAdmin = form.watch("privileges.isAdmin");

  return (
    <Card>
      <CardHeader>
        <CardTitle>Privileges</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <FormField
          control={form.control}
          name="privileges.isAdmin"
          render={({ field }) => (
            <FormItem className="flex flex-row items-start space-x-3 space-y-0 rounded-md border p-4">
              <FormControl>
                <Checkbox checked={field.value} onCheckedChange={field.onChange} />
              </FormControl>
              <div className="space-y-1 leading-none">
                <FormLabel>Admin Privileges</FormLabel>
                <FormDescription>
                  Grant all privileges (INCLUDING GRANT OPTION) on all databases
                </FormDescription>
              </div>
            </FormItem>
          )}
        />

        {!isAdmin && (
          <>
            <FormField
              control={form.control}
              name="privileges.allowSelect"
              render={({ field }) => (
                <FormItem className="flex flex-row items-start space-x-3 space-y-0 rounded-md border p-4">
                  <FormControl>
                    <Checkbox checked={field.value} onCheckedChange={field.onChange} />
                  </FormControl>
                  <div className="space-y-1 leading-none">
                    <FormLabel>Select Permission</FormLabel>
                    <FormDescription>Allow reading data from tables</FormDescription>
                  </div>
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name="privileges.allowInsert"
              render={({ field }) => (
                <FormItem className="flex flex-row items-start space-x-3 space-y-0 rounded-md border p-4">
                  <FormControl>
                    <Checkbox checked={field.value} onCheckedChange={field.onChange} />
                  </FormControl>
                  <div className="space-y-1 leading-none">
                    <FormLabel>Insert Permission</FormLabel>
                    <FormDescription>Allow inserting data into tables</FormDescription>
                  </div>
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name="privileges.allowDDL"
              render={({ field }) => (
                <FormItem className="flex flex-row items-start space-x-3 space-y-0 rounded-md border p-4">
                  <FormControl>
                    <Checkbox checked={field.value} onCheckedChange={field.onChange} />
                  </FormControl>
                  <div className="space-y-1 leading-none">
                    <FormLabel>DDL Permission</FormLabel>
                    <FormDescription>Allow creating, dropping, and altering tables</FormDescription>
                  </div>
                </FormItem>
              )}
            />

            {/* Add more privileges as needed */}
          </>
        )}
      </CardContent>
    </Card>
  );
};

export default PrivilegesSection;
