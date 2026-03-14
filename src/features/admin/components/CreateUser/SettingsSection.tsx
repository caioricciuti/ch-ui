import React, { useMemo } from "react";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import {
  FormField,
  FormItem,
  FormLabel,
  FormControl,
  FormDescription,
  FormMessage,
} from "@/components/ui/form";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Checkbox } from "@/components/ui/checkbox";

const GRANTEES_LABELS: Record<string, string> = {
  ANY: "Any",
  NONE: "None",
};

interface SettingsSectionProps {
  form: any;
  profiles: string[];
}

function SettingsProfileField({
  field,
  profiles,
}: {
  field: any;
  profiles: string[];
}) {
  const options = useMemo(() => {
    const val = field.state.value;
    if (val && !profiles.includes(val)) {
      return [val, ...profiles];
    }
    return profiles;
  }, [field.state.value, profiles]);

  return (
    <FormItem>
      <FormLabel>Settings Profile</FormLabel>
      <Select
        onValueChange={(v) => field.handleChange(v)}
        value={field.state.value}
      >
        <FormControl>
          <SelectTrigger>
            <SelectValue placeholder="Select settings profile">
              {field.state.value || undefined}
            </SelectValue>
          </SelectTrigger>
        </FormControl>
        <SelectContent>
          {options.map((profile) => (
            <SelectItem key={profile} value={profile}>
              {profile}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <FormMessage />
    </FormItem>
  );
}

const SettingsSection: React.FC<SettingsSectionProps> = ({
  form,
  profiles,
}) => {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Settings</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <FormField
          form={form}
          name="settings.profile"
          render={({ field }) => (
            <SettingsProfileField field={field} profiles={profiles} />
          )}
        />

        <FormField
          form={form}
          name="settings.readonly"
          render={({ field }) => (
            <FormItem className="flex flex-row items-start space-x-3 space-y-0 rounded-md border p-4">
              <FormControl>
                <Checkbox
                  checked={field.state.value}
                  onCheckedChange={(checked) => field.handleChange(!!checked)}
                />
              </FormControl>
              <div className="space-y-1 leading-none">
                <FormLabel>Read-only Mode</FormLabel>
                <FormDescription>
                  Restrict user to read-only operations
                </FormDescription>
              </div>
            </FormItem>
          )}
        />

        <FormField
          form={form}
          name="grantees"
          render={({ field }) => (
            <FormItem>
              <FormLabel>Grantees</FormLabel>
              <Select
                onValueChange={(v) => field.handleChange(v)}
                value={field.state.value}
              >
                <FormControl>
                  <SelectTrigger>
                    <SelectValue placeholder="Select grantees">
                      {field.state.value ? GRANTEES_LABELS[field.state.value] : undefined}
                    </SelectValue>
                  </SelectTrigger>
                </FormControl>
                <SelectContent>
                  <SelectItem value="ANY">Any</SelectItem>
                  <SelectItem value="NONE">None</SelectItem>
                </SelectContent>
              </Select>
              <FormDescription>
                Specify if this user can grant their privileges to others
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
      </CardContent>
    </Card>
  );
};

export default SettingsSection;
