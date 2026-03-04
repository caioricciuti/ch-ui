import React from "react";
import { useStore } from "@tanstack/react-store";
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
import { Input } from "@/components/ui/input";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { Calendar } from "@/components/ui/calendar";
import { Button } from "@/components/ui/button";
import { CalendarIcon } from "lucide-react";
import { cn } from "@/lib/utils";
import { format } from "date-fns";

const HOST_TYPE_LABELS: Record<string, string> = {
  ANY: "Any Host",
  LOCAL: "Local Only",
  IP: "Specific IP",
  NAME: "Host Name",
  REGEXP: "Regular Expression",
  LIKE: "Like Pattern",
};

interface AccessControlSectionProps {
  form: any;
}

const AccessControlSection: React.FC<AccessControlSectionProps> = ({
  form,
}) => {
  const hostType = useStore(form.store, (s: any) => s.values.hostType);

  return (
    <Card>
      <CardHeader>
        <CardTitle>Access Control</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <FormField
          form={form}
          name="hostType"
          render={({ field }) => (
            <FormItem>
              <FormLabel>Host Type</FormLabel>
              <Select
                onValueChange={(v) => field.handleChange(v)}
                value={field.state.value}
              >
                <FormControl>
                  <SelectTrigger>
                    <SelectValue placeholder="Select host type">
                      {field.state.value ? HOST_TYPE_LABELS[field.state.value] : undefined}
                    </SelectValue>
                  </SelectTrigger>
                </FormControl>
                <SelectContent>
                  <SelectItem value="ANY">Any Host</SelectItem>
                  <SelectItem value="LOCAL">Local Only</SelectItem>
                  <SelectItem value="IP">Specific IP</SelectItem>
                  <SelectItem value="NAME">Host Name</SelectItem>
                  <SelectItem value="REGEXP">Regular Expression</SelectItem>
                  <SelectItem value="LIKE">Like Pattern</SelectItem>
                </SelectContent>
              </Select>
            </FormItem>
          )}
        />

        {hostType !== "ANY" && hostType !== "LOCAL" && (
          <FormField
            form={form}
            name="hostValue"
            validators={{
              onChange: ({ value }: { value: string }) =>
                !value ? "Host value is required" : undefined,
            }}
            render={({ field }) => (
              <FormItem>
                <FormLabel>Host Value</FormLabel>
                <FormControl>
                  <Input
                    placeholder="Enter host value"
                    value={field.state.value}
                    onChange={(e) => field.handleChange(e.target.value)}
                    onBlur={field.handleBlur}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
        )}

        <FormField
          form={form}
          name="validUntil"
          render={({ field }) => (
            <FormItem className="flex flex-col">
              <FormLabel>Valid Until</FormLabel>
              <Popover>
                <PopoverTrigger asChild>
                  <FormControl>
                    <Button
                      variant="outline"
                      className={cn(
                        "w-full pl-3 text-left font-normal",
                        !field.state.value && "text-muted-foreground"
                      )}
                    >
                      {field.state.value
                        ? format(field.state.value, "PPP")
                        : "Pick a date"}
                      <CalendarIcon className="ml-auto h-4 w-4 opacity-50" />
                    </Button>
                  </FormControl>
                </PopoverTrigger>
                <PopoverContent className="w-auto p-0" align="start">
                  <Calendar
                    className="rounded-md border"
                    classNames="w-64"
                    mode="single"
                    selected={field.state.value}
                    onSelect={(date: Date | undefined) => field.handleChange(date)}
                    initialFocus
                  />
                </PopoverContent>
              </Popover>
              <FormMessage />
            </FormItem>
          )}
        />
      </CardContent>
    </Card>
  );
};

export default AccessControlSection;
