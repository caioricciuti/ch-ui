import useAppStore from "@/store";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
} from "@/components/ui/card";
import { Database } from "lucide-react";
import { useForm } from "@tanstack/react-form";
import InputField from "@/components/common/form/InputField";
import type { ClickHouseSettings } from "@clickhouse/client-common";
import { toast } from "sonner";

const numericValidator = {
  onChange: ({ value }: { value: unknown }) =>
    !/^[0-9]+$/.test(String(value)) ? "Only numbers" : undefined,
};

export default function ClickhouseDefaultConfiguration() {
  const { updateConfiguration, clickhouseSettings } = useAppStore();

  const form = useForm({
    defaultValues: {
      max_result_rows: clickhouseSettings.max_result_rows ?? "0",
      max_result_bytes: clickhouseSettings.max_result_bytes ?? "0",
      result_overflow_mode: clickhouseSettings.result_overflow_mode ?? "throw",
    },
    onSubmit: async ({ value }) => {
      try {
        await updateConfiguration(value as ClickHouseSettings);
        toast.success("Configuration updated successfully");
      } catch (error) {
        toast.error(`Failed to update configuration: ${error}`);
      }
    },
  });

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center space-x-2">
          <Database className="h-5 w-5" />
          <CardTitle>Default values</CardTitle>
        </div>
        <CardDescription>
          Set default values when are doing the queries (only affects your
          session in ch-ui)
        </CardDescription>
      </CardHeader>

      <CardContent className="space-y-4">
        <form
          onSubmit={(e) => {
            e.preventDefault();
            e.stopPropagation();
            form.handleSubmit();
          }}
        >
          <InputField
            form={form}
            name="max_result_rows"
            label="Max result rows"
            validators={numericValidator}
          />
          <InputField
            form={form}
            name="max_result_bytes"
            label="Max result bytes"
            validators={numericValidator}
          />
          <Button type="submit" className="w-full mt-6">
            Save
          </Button>
        </form>
      </CardContent>
    </Card>
  );
}
