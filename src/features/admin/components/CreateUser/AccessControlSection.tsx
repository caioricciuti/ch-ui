// components/CreateNewUser/AccessControlSection.tsx
import React from "react";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Form, FormField, FormItem, FormLabel, FormControl, FormMessage } from "@/components/ui/form";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Calendar } from "@/components/ui/calendar";
import { Button } from "@/components/ui/button";
import { CalendarIcon } from "lucide-react";
import { cn } from "@/lib/utils";
import { format } from "date-fns";

interface AccessControlSectionProps {
  form: any;
}

const AccessControlSection: React.FC<AccessControlSectionProps> = ({ form }) => {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Access Control</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <FormField
          control={form.control}
          name="hostType"
          render={({ field }) => (
            <FormItem>
              <FormLabel>Host Type</FormLabel>
              <Select onValueChange={field.onChange} defaultValue={field.value}>
                <FormControl>
                  <SelectTrigger>
                    <SelectValue placeholder="Select host type" />
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

        {form.watch("hostType") !== "ANY" && form.watch("hostType") !== "LOCAL" && (
          <FormField
            control={form.control}
            name="hostValue"
            rules={{ required: "Host value is required" }}
            render={({ field }) => (
              <FormItem>
                <FormLabel>Host Value</FormLabel>
                <FormControl>
                  <Input placeholder="Enter host value" {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
        )}

        <FormField
          control={form.control}
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
                        !field.value && "text-muted-foreground"
                      )}
                    >
                      {field.value ? format(field.value, "PPP") : "Pick a date"}
                      <CalendarIcon className="ml-auto h-4 w-4 opacity-50" />
                    </Button>
                  </FormControl>
                </PopoverTrigger>
                <PopoverContent className="w-auto p-0" align="start">
                  <Calendar
                    className="rounded-md border"
                    classNames='w-64'
                    mode="single"
                    selected={field.value}
                    onSelect={field.onChange}
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
