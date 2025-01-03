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
import { FormProvider, useForm } from "react-hook-form";
import InputField from "@/components/common/form/InputField";
import { ClickHouseSettings } from "@clickhouse/client-common";
import { toast } from "sonner";

export default function ClickhouseDefaultConfiguration() {
  const { updateConfiguration, clickhouseSettings } = useAppStore();

  const methods = useForm<ClickHouseSettings>({
    defaultValues: {
      max_result_rows: clickhouseSettings.max_result_rows ?? "0",
      max_result_bytes: clickhouseSettings.max_result_bytes ?? "0",
      result_overflow_mode: clickhouseSettings.result_overflow_mode ?? "throw",
    },
  });

  const onSubmit = async (data: ClickHouseSettings) => {
    try {
      await updateConfiguration(data); // Assuming this is an async function
      toast.success("Configuration updated successfully");
    } catch (error) {
      toast.error(`Failed to update configuration: ${error}`);
    }
  };

  return (
    <Card className="w-2/8">
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
        <FormProvider {...methods}>
          <form onSubmit={methods.handleSubmit(onSubmit)}>
            <InputField
              name="max_result_rows"
              label="Max result rows"
              rules={{
                pattern: {
                  value: /^[0-9]+$/,
                  message: "Only numbers",
                },
              }}
              required
            />
            <InputField
              name="max_result_bytes"
              label="Max result bytes"
              rules={{
                pattern: {
                  value: /^[0-9]+$/,
                  message: "Only numbers",
                },
              }}
              required
            />
            <Button type="submit" className="w-full mt-2">
              Save
            </Button>
          </form>
        </FormProvider>
      </CardContent>
    </Card>
  );
}