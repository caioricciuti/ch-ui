import React from "react";
import { FormField, FormItem, FormLabel, FormControl, FormMessage, FormDescription } from "@/components/ui/form";
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
    <div className="space-y-6">
      {/* Allowed Databases (Primary Scope) */}
      <FormField
        control={form.control}
        name="grantDatabases"
        render={({ field }) => (
          <FormItem>
            <FormLabel className="text-white">Allowed Databases</FormLabel>
            <FormDescription className="text-gray-400">
              Select which databases this user can access.
            </FormDescription>
            <Select
              onValueChange={(value: string) => {
                if (!grantDatabases.includes(value)) {
                  form.setValue("grantDatabases", [...grantDatabases, value]);
                }
              }}
            >
              <FormControl>
                <SelectTrigger className="bg-white/5 border-white/10 text-white">
                  <SelectValue placeholder="Add database access..." />
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
            <div className="flex flex-wrap gap-2 mt-3">
              {grantDatabases.length === 0 && (
                <div className="text-sm text-yellow-500/80 italic">No databases selected. User may not be able to query data unless they are Admin.</div>
              )}
              {grantDatabases.map((db: string) => (
                <Badge
                  key={db}
                  variant="secondary"
                  className="bg-purple-500/20 text-purple-200 hover:bg-red-500/20 hover:text-red-200 cursor-pointer border border-purple-500/30 transition-colors"
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
    </div>
  );
};

export default DatabaseRolesSection;
