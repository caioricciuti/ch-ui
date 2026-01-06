import { useState, useEffect } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import * as z from "zod";
import {
  Settings,
  Loader2,
  Eye,
  EyeOff,
  RefreshCw,

  Server,
  User,
  Lock,
  FileClock,
  Share2,
  Trash2,
  ChevronRight,
} from "lucide-react";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@/components/ui/form";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { Checkbox } from "@/components/ui/checkbox";
import { Separator } from "@/components/ui/separator";
import { toast } from "sonner";
import { useNavigate, useSearchParams } from "react-router-dom";
import useAppStore from "@/store";
import { retryInitialization } from "@/features/workspace/editor/monacoConfig";

// Custom URL validator that accepts both standard URLs and IP addresses with ports
const isValidClickHouseUrl = (url: string): boolean => {
  if (!url) return false;

  try {
    // Try to parse as URL - this will work for both domain names and IP addresses
    const parsed = new URL(url);

    // Check if it has a valid protocol
    if (!["http:", "https:"].includes(parsed.protocol)) {
      return false;
    }

    // Check if it has a hostname (can be domain or IP)
    if (!parsed.hostname) {
      return false;
    }

    // Basic IP address pattern check (IPv4)
    const ipv4Pattern = /^(\d{1,3}\.){3}\d{1,3}$/;
    // Basic IPv6 pattern check (simplified)
    const ipv6Pattern = /^(\[)?[0-9a-fA-F:]+(\])?$/;

    // If it's an IP address, ensure it's valid
    if (ipv4Pattern.test(parsed.hostname)) {
      const parts = parsed.hostname.split(".");
      return parts.every((part) => {
        const num = parseInt(part, 10);
        return num >= 0 && num <= 255;
      });
    }

    // For IPv6, basic validation (URL constructor handles most of it)
    if (ipv6Pattern.test(parsed.hostname.replace(/[\[\]]/g, ""))) {
      return true;
    }

    // For domain names, just ensure it's not empty
    return parsed.hostname.length > 0;
  } catch {
    return false;
  }
};

const formSchema = z.object({
  url: z.string().min(1, "URL is required").refine(isValidClickHouseUrl, {
    message:
      "Invalid URL. Please use format: http://hostname:port or http://ip-address:port",
  }),

  username: z.string().optional(),
  password: z.string().optional(),
  isDistributed: z.boolean().optional(),
  clusterName: z.string().optional(),
});

export default function SettingsPage() {
  document.title = "ClickHouse UI | Settings";
  const {
    credential,
    setCredential,
    checkServerStatus,
    isLoadingCredentials,
    isServerAvailable,
    version,
    error,
    clearCredentials,
    credentialSource,
    setCredentialSource,
    clearLocalData,
  } = useAppStore();

  const [showPassword, setShowPassword] = useState(false);
  const [showDistributedSettings, setShowDistributedSettings] = useState(
    credential?.isDistributed || false
  );
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();

  const currentFormValues = {
    url: credential?.url,
    username: credential?.username,
    password: credential?.password,
    isDistributed: credential?.isDistributed,
    clusterName: credential?.clusterName,
  };

  type FormData = {
    url: string;
    username?: string;
    password?: string;
    isDistributed?: boolean;
    clusterName?: string;
  };
  const form = useForm<FormData>({
    resolver: zodResolver(formSchema),
    defaultValues: {
      url: searchParams.get("url") || credential?.url || "",
      username: searchParams.get("username") || credential?.username || "",
      password: searchParams.get("password") || credential?.password || "",
      isDistributed:
        searchParams.get("isDistributed") === "true" ||
        credential?.isDistributed ||
        false,
      clusterName:
        searchParams.get("clusterName") || credential?.clusterName || "",
    },
  });

  useEffect(() => {
    form.reset({
      url: searchParams.get("url") || credential?.url || "",
      username: searchParams.get("username") || credential?.username || "",
      password: searchParams.get("password") || credential?.password || "",
      isDistributed:
        searchParams.get("isDistributed") === "true" ||
        credential?.isDistributed ||
        false,
      clusterName:
        searchParams.get("clusterName") || credential?.clusterName || "",
    });
  }, [searchParams, credential, form.reset]);

  const onSubmit = async (values: FormData) => {
    try {
      if (
        values.url === currentFormValues.url &&
        values.username === currentFormValues.username &&
        values.password === currentFormValues.password &&
        values.isDistributed === currentFormValues.isDistributed &&
        values.clusterName === currentFormValues.clusterName &&
        isServerAvailable
      ) {
        toast.info("No changes detected.");
        return;
      }

      await setCredential({
        ...values,
        username: credential?.username || "",
        password: credential?.password || "",
      });
      await checkServerStatus();
      setCredentialSource("app");
      await retryInitialization();
    } catch (error) {
      toast.error("Error saving credentials: " + (error as Error).message);
    }
  };



  const handleTestConnection = async () => {
    try {
      await checkServerStatus();
      if (isServerAvailable && !error) {
        toast.success("Connection successful!", {
          description: "Successfully connected to ClickHouse server.",
        });
      } else if (error) {
        // Extract just the main error message for the toast (not the troubleshooting tips)
        const mainErrorMessage = error.split("\n\n")[0];
        toast.error(`Connection failed: ${mainErrorMessage}`, {
          description:
            "See the troubleshooting tips below for help resolving this issue.",
        });
      }
    } catch (err) {
      toast.error("Error testing connection", {
        description:
          "An unexpected error occurred while testing the connection.",
      });
    }
  };

  const handleShare = () => {
    const values = form.getValues();
    const params = new URLSearchParams();
    params.set("url", values.url);
    params.set("username", values.username || "");
    if (values.password) params.set("password", values.password);

    const url = `${window.location.origin}${window.location.pathname
      }?${params.toString()}`;

    navigator.clipboard.writeText(url);
    toast.success("URL copied to clipboard!");
  };

  const handleClearLocal = () => {
    const confirmed = window.confirm(
      "This will clear tabs and metrics layouts saved locally. Credentials are kept. Continue?"
    );
    if (!confirmed) return;
    clearLocalData();
    toast.success("Local data cleared");
  };

  return (
    <TooltipProvider>
      <div className="max-h-screen w-full overflow-y-auto">
        <div className="max-w-2xl mx-auto py-8">
          <div className="space-y-8">
            {credentialSource === "env" && (
              <Alert variant="info" className="mb-8">
                <AlertTitle className="flex items-center font-semibold">
                  <Server className="mr-2 h-4 w-4" />
                  Using Environment Variables
                </AlertTitle>
                <AlertDescription>
                  Your ClickHouse credentials are set using environment
                  variables. Please update your environment variables to change
                  the connection settings.
                  <hr className="my-4" />
                  <p className="text-sm">
                    You are connected to: {credential?.url}
                    <br />
                    User: {credential?.username}
                    <br />
                  </p>
                </AlertDescription>
              </Alert>
            )}

            {credentialSource !== "env" && (
              <Card className="shadow-lg border-muted">
                <CardHeader>
                  <CardTitle className="text-2xl font-bold flex items-center gap-2">
                    <Settings className="h-6 w-6 text-primary" />
                    Cluster & Host Configuration
                  </CardTitle>
                  <CardDescription>
                    Configure your active server connection and enable distributed operations.
                  </CardDescription>
                </CardHeader>

                <CardContent>
                  {!useAppStore.getState().isAdmin ? (
                    <Alert variant="default" className="bg-yellow-500/10 border-yellow-500/20 text-yellow-200">
                      <Lock className="h-4 w-4" />
                      <AlertTitle>Admin Settings Locked</AlertTitle>
                      <AlertDescription>
                        You are logged in as a restricted user. Server configuration is read-only.
                        Please contact your administrator to change connection details.
                      </AlertDescription>
                    </Alert>
                  ) : (
                    <Form {...form}>
                      <form
                        onSubmit={form.handleSubmit(onSubmit)}
                        className="space-y-6"
                      >
                        <div className="space-y-4">
                          <FormField
                            control={form.control}
                            name="url"
                            render={({ field }) => (
                              <FormItem>
                                <FormLabel className="flex items-center gap-2">
                                  <Server className="h-4 w-4" />
                                  ClickHouse Host
                                </FormLabel>
                                <FormControl>
                                  {window.env?.VITE_CLICKHOUSE_URLS && window.env.VITE_CLICKHOUSE_URLS.length > 0 ? (
                                    <div className="relative">
                                      <select
                                        className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background file:border-0 file:bg-transparent file:text-sm file:font-medium placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50 appearance-none font-mono"
                                        value={field.value}
                                        onChange={field.onChange}
                                        disabled={isLoadingCredentials}
                                      >
                                        {window.env.VITE_CLICKHOUSE_URLS.map((url) => (
                                          <option key={url} value={url}>
                                            {url}
                                          </option>
                                        ))}
                                      </select>
                                      <ChevronRight className="absolute right-3 top-3 h-4 w-4 rotate-90 opacity-50 pointer-events-none" />
                                    </div>
                                  ) : (
                                    <Input
                                      className="font-mono"
                                      disabled={isLoadingCredentials}
                                      placeholder="https://your-clickhouse-host:8123"
                                      {...field}
                                    />
                                  )}
                                </FormControl>
                                <FormDescription className="text-xs">
                                  The URL of your ClickHouse server, including
                                  protocol and port
                                </FormDescription>
                                <FormMessage />
                              </FormItem>
                            )}
                          />







                          <FormField
                            control={form.control}
                            name="isDistributed"
                            render={({ field }) => (
                              <FormItem className="flex flex-row items-start space-x-3 space-y-0 rounded-md border p-4 shadow-sm">
                                <FormControl>
                                  <Checkbox
                                    checked={field.value}
                                    onCheckedChange={(checked) => {
                                      setShowDistributedSettings(
                                        checked as boolean
                                      );
                                      field.onChange(checked);
                                    }}
                                  />
                                </FormControl>
                                <div className="space-y-1 leading-none">
                                  <FormLabel>Distributed Mode</FormLabel>
                                  <FormDescription className="text-xs">
                                    Enable this if you're using a ClickHouse
                                    cluster
                                  </FormDescription>
                                </div>
                              </FormItem>
                            )}
                          />

                          {showDistributedSettings && (
                            <FormField
                              control={form.control}
                              name="clusterName"
                              render={({ field }) => (
                                <FormItem>
                                  <FormLabel>Cluster Name</FormLabel>
                                  <FormControl>
                                    <Input
                                      className="font-mono"
                                      disabled={isLoadingCredentials}
                                      placeholder="my_cluster"
                                      {...field}
                                    />
                                  </FormControl>
                                  <FormDescription className="text-xs">
                                    The name of your ClickHouse cluster
                                  </FormDescription>
                                  <FormMessage />
                                </FormItem>
                              )}
                            />
                          )}
                        </div>

                        <div className="flex gap-4 pt-4">
                          <Button
                            type="submit"
                            disabled={isLoadingCredentials}
                            className="w-40"
                          >
                            {isLoadingCredentials ? (
                              <>
                                <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                                Connecting
                              </>
                            ) : (
                              "Save and Connect"
                            )}
                          </Button>


                        </div>
                      </form>
                    </Form>
                  )}
                </CardContent>

                {isServerAvailable ? (
                  <CardFooter className="border-t bg-muted/50 rounded-b-lg pt-4">
                    <div className="flex w-full justify-between items-center ">
                      <div className="space-x-4">
                        <TooltipProvider>
                          <Tooltip>
                            <TooltipTrigger asChild>
                              <Button
                                variant="secondary"
                                onClick={handleShare}
                                disabled={isLoadingCredentials}
                                size="icon"
                              >
                                <Share2 className="h-4 w-4" />
                              </Button>
                            </TooltipTrigger>
                            <TooltipContent>
                              Share your current connection settings as a URL.
                            </TooltipContent>
                          </Tooltip>
                        </TooltipProvider>
                        <Button
                          variant="outline"
                          onClick={handleTestConnection}
                          disabled={isLoadingCredentials}
                          className="w-40"
                        >
                          <RefreshCw className="mr-2 h-4 w-4" />
                          Test Connection
                        </Button>
                      </div>
                      <span className="text-sm font-mono font-semibold text-green-500">
                        Server version: {version} - Connected
                      </span>
                    </div>
                  </CardFooter>
                ) : error ? (
                  <CardFooter className="border-t bg-muted/50 rounded-b-lg pt-4">
                    <div className="w-full">
                      <Alert variant="destructive" className="mb-4">
                        <AlertTitle className="flex items-center font-semibold text-red-500">
                          Connection Error
                        </AlertTitle>
                        <AlertDescription>
                          <div className="mt-2 space-y-4">
                            <p className="font-medium">
                              {error.split("\n\n")[0]}
                            </p>

                            {error.includes("Troubleshooting tips:") && (
                              <div className="mt-4">
                                <h4 className="font-medium mb-2">
                                  Troubleshooting Tips:
                                </h4>
                                <ul className="list-disc pl-5 space-y-1.5 text-sm">
                                  {error
                                    .split("Troubleshooting tips:\n")[1]
                                    ?.split("\n")
                                    .map((tip, index) => (
                                      <li key={index}>{tip}</li>
                                    ))}
                                </ul>
                              </div>
                            )}

                            <Button
                              variant="outline"
                              size="sm"
                              onClick={handleTestConnection}
                              className="mt-4"
                            >
                              <RefreshCw className="mr-2 h-4 w-4" />
                              Test Connection Again
                            </Button>
                          </div>
                        </AlertDescription>
                      </Alert>
                    </div>
                  </CardFooter>
                ) : null}
              </Card>
            )}

            {/* Local data management */}
            <Card className="shadow-lg border-muted">
              <CardHeader>
                <CardTitle className="text-2xl font-bold flex items-center gap-2">
                  <Trash2 className="h-6 w-6 text-primary" />
                  Interface Reset
                </CardTitle>
                <CardDescription>
                  Reset your workspace layout, including open tabs and dashboard customizations.
                </CardDescription>
              </CardHeader>
              <CardContent>
                <p className="text-sm text-muted-foreground">
                  This acts as a "Factory Reset" for the UI. It does not log you out or delete any data on the server.
                </p>
              </CardContent>
              <CardFooter className="border-t bg-muted/50 rounded-b-lg pt-4 flex justify-end">
                <Button variant="destructive" onClick={handleClearLocal}>
                  <Trash2 className="mr-2 h-4 w-4" /> Clear Local Data
                </Button>
              </CardFooter>
            </Card>
          </div>
        </div>
      </div>
    </TooltipProvider>
  );
}
