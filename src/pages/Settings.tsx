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
  LogOut,
  Server,
  User,
  Lock,
  Cog,
  FileClock,
  Share2,
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

const formSchema = z.object({
  url: z.string().url("Invalid URL").min(1, "URL is required"),
  username: z.string().min(1, "Username is required"),
  password: z.string().optional(),
  useAdvanced: z.boolean().optional(),
  customPath: z.string().optional(),
  requestTimeout: z.coerce
    .number()
    .int("Request timeout must be a whole number")
    .min(1000, "Request timeout must be at least 1000 millisecond")
    .max(600000, "Request timeout must not exceed 600000 milliseconds"),
});

export default function SettingsPage() {
  document.title = "CH-UI | Settings";
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
  } = useAppStore();

  const [showPassword, setShowPassword] = useState(false);
  const [showAdvancedSettings, setShowAdvancedSettings] = useState(false);
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();

  const currentFormValues = {
    url: credential?.url,
    username: credential?.username,
    password: credential?.password,
    requestTimeout: credential?.requestTimeout,
  };

  const form = useForm<z.infer<typeof formSchema>>({
    resolver: zodResolver(formSchema),
    defaultValues: {
      url: searchParams.get("url") || credential?.url || "",
      username: searchParams.get("username") || credential?.username || "",
      password: searchParams.get("password") || credential?.password || "",
      requestTimeout:
        Number(searchParams.get("requestTimeout")) ||
        credential?.requestTimeout ||
        30000,
      useAdvanced: searchParams.get("useAdvanced") === "true" || false,
      customPath: searchParams.get("customPath") || "",
    },
  });

  useEffect(() => {
    form.reset({
      url: searchParams.get("url") || credential?.url || "",
      username: searchParams.get("username") || credential?.username || "",
      password: searchParams.get("password") || credential?.password || "",
      requestTimeout:
        Number(searchParams.get("requestTimeout")) ||
        credential?.requestTimeout ||
        30000,
      useAdvanced: searchParams.get("useAdvanced") === "true" || false,
      customPath: searchParams.get("customPath") || "",
    });

    if (searchParams.get("useAdvanced") === "true")
      setShowAdvancedSettings(true);
    else setShowAdvancedSettings(false);
  }, [searchParams, credential, form.reset]);

  const onSubmit = async (values: z.infer<typeof formSchema>) => {
    try {
      if (
        values.url === currentFormValues.url &&
        values.username === currentFormValues.username &&
        values.password === currentFormValues.password &&
        values.requestTimeout === currentFormValues.requestTimeout &&
        isServerAvailable
      ) {
        toast.info("No changes detected.");
        return;
      }

      let url = values.url;
      if (values.useAdvanced && values.customPath) {
        url = `${values.url}/${values.customPath}`;
      }

      await setCredential({
        ...values,
        url,
        password: values.password || "",
        useAdvanced: values.useAdvanced || false,
        customPath: values.customPath || "",
        requestTimeout: values.requestTimeout, // No need to convert, already a number
      });
      await checkServerStatus();
      setCredentialSource("app");
      await retryInitialization();
    } catch (error) {
      toast.error("Error saving credentials: " + (error as Error).message);
    }
  };

  const handleDisconnect = () => {
    clearCredentials();
    form.reset({
      url: "",
      username: "",
      password: "",
      useAdvanced: false,
      customPath: "",
      requestTimeout: 30000,
    });
    toast.success("Disconnected from ClickHouse server.");
    navigate("/settings");
  };

  const handleTestConnection = async () => {
    try {
      await checkServerStatus();
      if (isServerAvailable && !error) {
        toast.success("Connection successful!", {
          description: "Successfully connected to ClickHouse server."
        });
      } else if (error) {
        // Extract just the main error message for the toast (not the troubleshooting tips)
        const mainErrorMessage = error.split('\n\n')[0];
        toast.error(`Connection failed: ${mainErrorMessage}`, {
          description: "See the troubleshooting tips below for help resolving this issue."
        });
      }
    } catch (err) {
      toast.error("Error testing connection", {
        description: "An unexpected error occurred while testing the connection."
      });
    }
  };

  const handleShare = () => {
    const values = form.getValues();
    const params = new URLSearchParams();
    params.set("url", values.url);
    params.set("username", values.username);
    if (values.password) params.set("password", values.password);
    if (values.useAdvanced)
      params.set("useAdvanced", values.useAdvanced.toString());
    if (values.customPath) params.set("customPath", values.customPath);
    params.set("requestTimeout", String(values.requestTimeout));

    const url = `${window.location.origin}${
      window.location.pathname
    }?${params.toString()}`;

    navigator.clipboard.writeText(url);
    toast.success("URL copied to clipboard!");
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
                    Connection Settings
                  </CardTitle>
                </CardHeader>

                <CardContent>
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
                                <Input
                                  className="font-mono"
                                  disabled={isLoadingCredentials}
                                  placeholder="https://your-clickhouse-host:8123"
                                  {...field}
                                />
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
                                  disabled={isLoadingCredentials}
                                  placeholder="default"
                                  autoComplete="username"
                                  {...field}
                                />
                              </FormControl>
                              <FormMessage />
                            </FormItem>
                          )}
                        />

                        <FormField
                          control={form.control}
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
                                    className="font-mono pr-10"
                                    disabled={isLoadingCredentials}
                                    type={showPassword ? "text" : "password"}
                                    autoComplete="current-password"
                                    {...field}
                                  />
                                  <Tooltip>
                                    <TooltipTrigger asChild>
                                      <button
                                        type="button"
                                        onClick={() =>
                                          setShowPassword(!showPassword)
                                        }
                                        className="absolute inset-y-0 right-0 pr-3 flex items-center text-muted-foreground hover:text-foreground transition-colors"
                                      >
                                        {showPassword ? (
                                          <EyeOff className="h-4 w-4" />
                                        ) : (
                                          <Eye className="h-4 w-4" />
                                        )}
                                      </button>
                                    </TooltipTrigger>
                                    <TooltipContent>
                                      {showPassword
                                        ? "Hide password"
                                        : "Show password"}
                                    </TooltipContent>
                                  </Tooltip>
                                </div>
                              </FormControl>
                              <FormMessage />
                            </FormItem>
                          )}
                        />

                        <Separator className="my-6" />

                        <FormField
                          control={form.control}
                          name="useAdvanced"
                          render={({ field }) => (
                            <FormItem className="flex flex-row items-start space-x-3 space-y-0 rounded-md border p-4 shadow-sm">
                              <FormControl>
                                <Checkbox
                                  checked={field.value}
                                  onCheckedChange={(checked) => {
                                    setShowAdvancedSettings(checked as boolean);
                                    field.onChange(checked);
                                  }}
                                />
                              </FormControl>
                              <div className="space-y-1 leading-none">
                                <FormLabel className="flex items-center gap-2">
                                  <Cog className="h-4 w-4" />
                                  Advanced Settings
                                </FormLabel>
                                <FormDescription className="text-xs">
                                  Enable custom path handling for the ClickHouse
                                  URL
                                </FormDescription>
                              </div>
                            </FormItem>
                          )}
                        />

                        {showAdvancedSettings && (
                          <FormField
                            control={form.control}
                            name="customPath"
                            render={({ field }) => (
                              <FormItem>
                                <FormLabel>Custom Path</FormLabel>
                                <FormControl>
                                  <Input
                                    className="font-mono"
                                    disabled={isLoadingCredentials}
                                    placeholder="clickhouse-{cluster_name}"
                                    {...field}
                                  />
                                </FormControl>
                                <FormDescription className="text-xs">
                                  Specify the custom path if you're using
                                  path-based routing
                                </FormDescription>
                                <FormMessage />
                              </FormItem>
                            )}
                          />
                        )}

                        <FormField
                          control={form.control}
                          name="requestTimeout"
                          render={({ field }) => (
                            <FormItem>
                              <FormLabel className="flex items-center gap-2">
                                <FileClock className="h-4 w-4" />
                                Request Timeout (ms)
                              </FormLabel>
                              <FormControl>
                                <Input
                                  className="font-mono"
                                  disabled={isLoadingCredentials}
                                  type="number"
                                  min={1}
                                  max={600000}
                                  placeholder="30000"
                                  {...field}
                                  onChange={(e) =>
                                    field.onChange(e.target.value)
                                  }
                                />
                              </FormControl>
                              <FormDescription className="text-xs">
                                Set the request timeout in milliseconds. Must be
                                between 1000 and 600000. (Default: 30000)
                              </FormDescription>
                              <FormMessage />
                            </FormItem>
                          )}
                        />
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

                        {isServerAvailable && (
                          <Button
                            type="button"
                            variant="destructive"
                            onClick={handleDisconnect}
                            className="w-32"
                          >
                            <LogOut className="mr-2 h-4 w-4" />
                            Disconnect
                          </Button>
                        )}
                      </div>
                    </form>
                  </Form>
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
                            <p className="font-medium">{error.split('\n\n')[0]}</p>
                            
                            {error.includes('Troubleshooting tips:') && (
                              <div className="mt-4">
                                <h4 className="font-medium mb-2">Troubleshooting Tips:</h4>
                                <ul className="list-disc pl-5 space-y-1.5 text-sm">
                                  {error.split('Troubleshooting tips:\n')[1]?.split('\n').map((tip, index) => (
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
          </div>
        </div>
      </div>
    </TooltipProvider>
  );
}
