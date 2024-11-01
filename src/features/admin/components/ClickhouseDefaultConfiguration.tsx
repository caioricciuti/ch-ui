import useAppStore from "@/store";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
} from "@/components/ui/card";
import { DevTool } from "@hookform/devtools";
import { Database } from "lucide-react";
import { FormProvider, useForm } from "react-hook-form";
import InputField from "@/components/common/form/InputField";
import { OverflowMode } from "@clickhouse/client-common/dist/settings"

export default function ClickhouseDefaultConfiguration() {
  const {
    updateConfiguration
  } = useAppStore();

  const methods = useForm({
    defaultValues: {
      max_result_rows: "0",
      max_result_bytes: "0",
      result_overflow_mode: "break" as OverflowMode
    },
  });

  return (
    <Card className="w-2/8">
      <CardHeader>
        <div className="flex items-center space-x-2">
          <Database className="h-5 w-5" />
          <CardTitle>Default values</CardTitle>
        </div>
        <CardDescription>
          Set default values when are doing the queries (only affects your session in ch-ui)
        </CardDescription>
      </CardHeader>

      <CardContent className="space-y-4">
        <FormProvider {...methods}>
          <form onSubmit={methods.handleSubmit(d => updateConfiguration(d))}>
            <InputField name="max_result_rows" label="Max result rows"
              rules={{
                pattern: {
                  value: /^[0-9]+$/,
                  message: "Only numbers",
                },
              }}
              required
            />
            <InputField name="max_result_bytes" label="Max result bytes"
              rules={{
                pattern: {
                  value: /^[0-9]+$/,
                  message: "Only numbers",
                },
              }}
              required
            />
            {/* <FormField control={form.control}
              name="maxResultRows"
              rules={{
                pattern: {
                  value: /^[0-9]+$/,
                  message: "Only numbers",
                },
              }}
              render={({ field }) => (
                <FormItem>
                  <FormLabel>Max result rows</FormLabel>
                  <FormControl>
                    <Input placeholder="Enter the max result rows" {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}>
            </FormField> */}
            <Button
              type="submit"
              className="w-full mt-2"
            >
              Save
            </Button>
          </form>
          <DevTool control={methods.control} />
        </FormProvider>
      </CardContent>


    </Card>
  );
}
