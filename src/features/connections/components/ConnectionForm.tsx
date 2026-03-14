// src/features/connections/components/ConnectionForm.tsx
// Form for creating/editing connections (no auth required)

import { useState, useEffect } from "react";
import { useForm } from "@tanstack/react-form";
import { useStore } from "@tanstack/react-store";
import * as z from "zod";
import {
  Eye,
  EyeOff,
  Loader2,
  Save,
  X,
  Server,
  User,
  Lock,
  Cog,
  CheckCircle2,
  XCircle,
} from "lucide-react";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@/components/ui/form";
import { toast } from "sonner";
import { useConnectionStore } from "@/store/connectionStore";
import type { SavedConnection } from "@/lib/db";
import { createClient } from "@clickhouse/client-web";
import useAppStore, { ClickHouseError } from "@/store/index";

const isValidClickHouseUrl = (url: string): boolean => {
  if (!url) return false;
  try {
    const parsed = new URL(url);
    if (!["http:", "https:"].includes(parsed.protocol)) return false;
    return parsed.hostname.length > 0;
  } catch {
    return false;
  }
};

const formSchema = z.object({
  name: z.string().min(1, "Name is required").max(100, "Name is too long"),
  url: z.string().min(1, "URL is required").refine(isValidClickHouseUrl, {
    message: "Invalid URL. Please use format: http://hostname:port",
  }),
  username: z.string().min(1, "Username is required"),
  password: z.string().optional(),
  useAdvanced: z.boolean().optional(),
  customPath: z.string().optional(),
  requestTimeout: z.coerce
    .number()
    .int("Request timeout must be a whole number")
    .min(1000, "Request timeout must be at least 1000ms")
    .max(600000, "Request timeout must not exceed 600000ms"),
  isDistributed: z.boolean().optional(),
  clusterName: z.string().optional(),
  isDefault: z.boolean().optional(),
});

type FormData = z.infer<typeof formSchema>;
type FormInput = z.input<typeof formSchema>;

interface ConnectionFormProps {
  connection?: SavedConnection | null;
  onSuccess: () => void;
  onCancel: () => void;
}

export default function ConnectionForm({
  connection,
  onSuccess,
  onCancel,
}: ConnectionFormProps) {
  const [showPassword, setShowPassword] = useState(false);
  const [showAdvanced, setShowAdvanced] = useState(false);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [testStatus, setTestStatus] = useState<{
    loading: boolean;
    success: boolean | null;
    message: string;
  }>({
    loading: false,
    success: null,
    message: "",
  });

  const { saveConnection, updateConnectionById, activeConnectionId } = useConnectionStore();
  const setCredential = useAppStore((s) => s.setCredential);

  const form = useForm({
    defaultValues: {
      name: connection?.name || "",
      url: connection?.url || "",
      username: connection?.username || "",
      password: connection?.password || "",
      useAdvanced: connection?.useAdvanced || false,
      customPath: connection?.customPath || "",
      requestTimeout: connection?.requestTimeout || 30000,
      isDistributed: connection?.isDistributed || false,
      clusterName: connection?.clusterName || "",
      isDefault: connection?.isDefault || false,
    },
    validators: {
      onMount: formSchema as any,
      onChange: formSchema as any,
      onSubmit: formSchema as any,
    },
    onSubmit: async ({ value }) => {
      await onSubmitHandler(value as FormData);
    },
  });

  const useAdvanced = useStore(form.store, (s) => s.values.useAdvanced);
  const isDistributed = useStore(form.store, (s) => s.values.isDistributed);
  const canSubmit = useStore(form.store, (s) => s.canSubmit);

  useEffect(() => {
    if (connection?.useAdvanced) {
      setShowAdvanced(true);
    }
  }, [connection]);

  const handleTestConnection = async () => {
    setTestStatus({ loading: true, success: null, message: "" });

    try {
      const formValues = form.state.values;

      const baseUrl = formValues.url.replace(/\/+$/, "");
      const pathname = formValues.useAdvanced && formValues.customPath
        ? formValues.customPath
        : undefined;

      const client = createClient({
        url: baseUrl,
        pathname,
        username: formValues.username,
        password: formValues.password || "",
        request_timeout: formValues.requestTimeout || 30000,
      });

      await client.ping();

      const versionResult = await client.query({
        query: "SELECT version()",
      });
      const versionData = (await versionResult.json()) as {
        data: { "version()": string }[];
      };
      const version = versionData.data[0]["version()"];

      setTestStatus({
        loading: false,
        success: true,
        message: `Connected successfully! Server version: ${version}`,
      });
      toast.success(`Connection successful! Server version: ${version}`);
    } catch (error: any) {
      const enhancedError = ClickHouseError.fromError(
        error,
        "Failed to connect to ClickHouse server"
      );

      const errorMessage = `${enhancedError.message}\n\nTroubleshooting tips:\n${enhancedError.troubleshootingTips.join("\n")}`;

      setTestStatus({
        loading: false,
        success: false,
        message: errorMessage,
      });
      toast.error(`Connection failed: ${enhancedError.message}`, {
        description: "Check the form for troubleshooting tips.",
      });
    }
  };

  const onSubmitHandler = async (data: FormData) => {
    setIsSubmitting(true);

    try {
      if (connection) {
        const success = await updateConnectionById(connection.id, {
          name: data.name,
          url: data.url,
          username: data.username,
          password: data.password,
          useAdvanced: data.useAdvanced,
          customPath: data.customPath,
          requestTimeout: data.requestTimeout,
          isDistributed: data.isDistributed,
          clusterName: data.clusterName,
          isDefault: data.isDefault,
        });

        if (success) {
          // Reconnect if the updated connection is currently active
          if (activeConnectionId === connection.id) {
            await setCredential({
              url: data.url,
              username: data.username,
              password: data.password || "",
              requestTimeout: data.requestTimeout,
              useAdvanced: data.useAdvanced || false,
              customPath: data.customPath || "",
              isDistributed: data.isDistributed,
              clusterName: data.clusterName,
            });
            toast.success("Connection updated and reconnected");
          } else {
            toast.success("Connection updated");
          }
          onSuccess();
        }
      } else {
        const result = await saveConnection({
          name: data.name,
          url: data.url,
          username: data.username,
          password: data.password || "",
          useAdvanced: data.useAdvanced,
          customPath: data.customPath,
          requestTimeout: data.requestTimeout,
          isDistributed: data.isDistributed,
          clusterName: data.clusterName,
          isDefault: data.isDefault,
        });

        if (result) {
          toast.success("Connection saved");
          onSuccess();
        }
      }
    } catch (err) {
      toast.error(
        "Failed to save: " +
          (err instanceof Error ? err.message : "Unknown error")
      );
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <Form>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          e.stopPropagation();
          form.handleSubmit();
        }}
        className="space-y-4"
      >
        <FormField
          form={form}
          name="name"
          render={({ field }) => (
            <FormItem>
              <FormLabel>Connection Name</FormLabel>
              <FormControl>
                <Input
                  placeholder="My ClickHouse Server"
                  disabled={isSubmitting}
                  value={field.state.value}
                  onChange={(e) => field.handleChange(e.target.value)}
                  onBlur={field.handleBlur}
                />
              </FormControl>
              <FormDescription>
                A friendly name to identify this connection
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />

        <FormField
          form={form}
          name="url"
          render={({ field }) => (
            <FormItem>
              <FormLabel className="flex items-center gap-2">
                <Server className="h-4 w-4" />
                ClickHouse Host
              </FormLabel>
              <FormControl>
                <Input
                  className="font-mono"
                  placeholder="https://your-clickhouse-host:8123"
                  disabled={isSubmitting}
                  value={field.state.value}
                  onChange={(e) => field.handleChange(e.target.value)}
                  onBlur={field.handleBlur}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />

        <div className="grid grid-cols-2 gap-4">
          <FormField
            form={form}
            name="username"
            render={({ field }) => (
              <FormItem>
                <FormLabel className="flex items-center gap-2">
                  <User className="h-4 w-4" />
                  Username
                </FormLabel>
                <FormControl>
                  <Input
                    className="font-mono"
                    placeholder="default"
                    disabled={isSubmitting}
                    value={field.state.value}
                    onChange={(e) => field.handleChange(e.target.value)}
                    onBlur={field.handleBlur}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            form={form}
            name="password"
            render={({ field }) => (
              <FormItem>
                <FormLabel className="flex items-center gap-2">
                  <Lock className="h-4 w-4" />
                  Password
                </FormLabel>
                <FormControl>
                  <div className="relative">
                    <Input
                      type={showPassword ? "text" : "password"}
                      placeholder="Enter password"
                      disabled={isSubmitting}
                      value={field.state.value}
                      onChange={(e) => field.handleChange(e.target.value)}
                      onBlur={field.handleBlur}
                    />
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      className="absolute right-0 top-0 h-full px-3"
                      onClick={() => setShowPassword(!showPassword)}
                    >
                      {showPassword ? (
                        <EyeOff className="h-4 w-4" />
                      ) : (
                        <Eye className="h-4 w-4" />
                      )}
                    </Button>
                  </div>
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
        </div>

        <FormField
          form={form}
          name="requestTimeout"
          render={({ field }) => (
            <FormItem>
              <FormLabel>Request Timeout (ms)</FormLabel>
              <FormControl>
                <Input
                  type="number"
                  disabled={isSubmitting}
                  value={field.state.value}
                  onChange={(e) => field.handleChange(Number(e.target.value))}
                  onBlur={field.handleBlur}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />

        <div className="flex items-center gap-4">
          <FormField
            form={form}
            name="isDefault"
            render={({ field }) => (
              <FormItem className="flex items-center gap-2">
                <FormControl>
                  <Checkbox
                    checked={field.state.value}
                    onCheckedChange={(checked) => field.handleChange(!!checked)}
                    disabled={isSubmitting}
                  />
                </FormControl>
                <FormLabel className="!mt-0 cursor-pointer">
                  Set as default connection
                </FormLabel>
              </FormItem>
            )}
          />
        </div>

        <Button
          type="button"
          variant="ghost"
          size="sm"
          onClick={() => setShowAdvanced(!showAdvanced)}
          className="w-full"
        >
          <Cog className="h-4 w-4 mr-2" />
          {showAdvanced ? "Hide" : "Show"} Advanced Settings
        </Button>

        {showAdvanced && (
          <div className="space-y-4 pt-2 border-t">
            <FormField
              form={form}
              name="useAdvanced"
              render={({ field }) => (
                <FormItem className="flex items-center gap-2">
                  <FormControl>
                    <Checkbox
                      checked={field.state.value}
                      onCheckedChange={(checked) => field.handleChange(!!checked)}
                      disabled={isSubmitting}
                    />
                  </FormControl>
                  <FormLabel className="!mt-0 cursor-pointer">
                    Use custom path
                  </FormLabel>
                </FormItem>
              )}
            />

            {useAdvanced && (
              <FormField
                form={form}
                name="customPath"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>Custom Path</FormLabel>
                    <FormControl>
                      <Input
                        placeholder="e.g., /clickhouse"
                        disabled={isSubmitting}
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
              name="isDistributed"
              render={({ field }) => (
                <FormItem className="flex items-center gap-2">
                  <FormControl>
                    <Checkbox
                      checked={field.state.value}
                      onCheckedChange={(checked) => field.handleChange(!!checked)}
                      disabled={isSubmitting}
                    />
                  </FormControl>
                  <FormLabel className="!mt-0 cursor-pointer">
                    Distributed cluster
                  </FormLabel>
                </FormItem>
              )}
            />

            {isDistributed && (
              <FormField
                form={form}
                name="clusterName"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>Cluster Name</FormLabel>
                    <FormControl>
                      <Input
                        placeholder="my_cluster"
                        disabled={isSubmitting}
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
          </div>
        )}

        <div className="space-y-3 pt-4">
          <div className="flex justify-end gap-2">
            <Button
              type="button"
              variant="outline"
              onClick={onCancel}
              disabled={isSubmitting}
            >
              <X className="h-4 w-4 mr-2" />
              Cancel
            </Button>
            <Button
              type="button"
              variant="outline"
              onClick={handleTestConnection}
              disabled={!canSubmit || testStatus.loading}
            >
              {testStatus.loading ? (
                <>
                  <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                  Testing...
                </>
              ) : (
                <>
                  <CheckCircle2 className="h-4 w-4 mr-2" />
                  Test Connection
                </>
              )}
            </Button>
            <Button type="submit" disabled={isSubmitting}>
              {isSubmitting ? (
                <>
                  <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                  Saving...
                </>
              ) : (
                <>
                  <Save className="h-4 w-4 mr-2" />
                  Save Connection
                </>
              )}
            </Button>
          </div>

          {testStatus.success === true && (
            <div className="flex items-start gap-2 p-3 rounded-md bg-green-50 dark:bg-green-950/20 border border-green-200 dark:border-green-900">
              <CheckCircle2 className="h-5 w-5 text-green-600 dark:text-green-500 flex-shrink-0 mt-0.5" />
              <div className="flex-1 text-sm text-green-700 dark:text-green-400">
                {testStatus.message}
              </div>
            </div>
          )}

          {testStatus.success === false && (
            <div className="flex items-start gap-2 p-3 rounded-md bg-red-50 dark:bg-red-950/20 border border-red-200 dark:border-red-900">
              <XCircle className="h-5 w-5 text-red-600 dark:text-red-500 flex-shrink-0 mt-0.5" />
              <div className="flex-1 text-sm text-red-700 dark:text-red-400 whitespace-pre-line">
                {testStatus.message}
              </div>
            </div>
          )}
        </div>
      </form>
    </Form>
  );
}
