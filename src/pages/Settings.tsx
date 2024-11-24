import { useState } from "react";
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
import { useNavigate } from "react-router-dom";
import useAppStore from "@/store";
import { retryInitialization } from "@/features/workspace/editor/monacoConfig";

const formSchema = z.object({
  host: z.string().url("Invalid URL").min(1, "URL is required"),
  username: z.string().min(1, "Username is required"),
  password: z.string().optional(),
  useAdvanced: z.boolean().optional(),
  customPath: z.string().optional(),
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

  const currentFormValues = {
    host: credential?.host,
    username: credential?.username,
    password: credential?.password,
  };

  const form = useForm<z.infer<typeof formSchema>>({
    resolver: zodResolver(formSchema),
    defaultValues: {
      host: credential?.host || "",
      username: credential?.username || "",
      password: credential?.password || "",
      useAdvanced: false,
      customPath: "",
    },
  });

  const onSubmit = async (values: z.infer<typeof formSchema>) => {
    try {
      if (
        values.host === currentFormValues.host &&
        values.username === currentFormValues.username &&
        values.password === currentFormValues.password &&
        isServerAvailable
      ) {
        toast.info("No changes detected.");
        return;
      }

      let host = values.host;
      if (values.useAdvanced && values.customPath) {
        host = `${values.host}/${values.customPath}`;
      }

      await setCredential({
        ...values,
        host,
        password: values.password || "",
        useAdvanced: values.useAdvanced || false,
        customPath: values.customPath || "",
      });
      await checkServerStatus();
      setCredentialSource("app");
      await retryInitialization();
      toast.info("Settings saved successfully!");
    } catch (error) {
      toast.error("Error saving credentials: " + (error as Error).message);
    }
  };

  const handleDisconnect = () => {
    clearCredentials();
    form.reset({
      host: "",
      username: "",
      password: "",
      useAdvanced: false,
      customPath: "",
    });
    toast.success("Disconnected from ClickHouse server.");
    navigate("/settings");
  };

  const handleTestConnection = async () => {
    try {
      await checkServerStatus();
      if (isServerAvailable && !error) {
        toast.success("Connection successful!");
      } else if (error) {
        toast.error(`Connection failed: ${error}`);
      }
    } catch (err) {
      toast.error("Error testing connection");
    }
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
                </AlertDescription>
              </Alert>
            )}

            <Card className="shadow-lg border-muted">
              <CardHeader>
                <CardTitle className="text-2xl font-bold flex items-center gap-2">
                  <Settings className="h-6 w-6 text-primary" />
                  Connection Settings
                </CardTitle>
                <CardDescription>
                  Configure your connection to the ClickHouse server
                </CardDescription>
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
                        name="host"
                        render={({ field }) => (
                          <FormItem>
                            <FormLabel className="flex items-center gap-2">
                              <Server className="h-4 w-4" />
                              ClickHouse Host
                            </FormLabel>
                            <FormControl>
                              <Input
                                className="font-mono"
                                disabled={
                                  isLoadingCredentials ||
                                  credentialSource === "env"
                                }
                                placeholder="https://your-clickhouse-host:8123"
                                {...field}
                              />
                            </FormControl>
                            <FormDescription>
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
                                disabled={
                                  isLoadingCredentials ||
                                  credentialSource === "env"
                                }
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
                                  disabled={
                                    isLoadingCredentials ||
                                    credentialSource === "env"
                                  }
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
                              <FormDescription>
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
                              <FormDescription>
                                Specify the custom path if you're using
                                path-based routing
                              </FormDescription>
                              <FormMessage />
                            </FormItem>
                          )}
                        />
                      )}
                    </div>

                    {credentialSource !== "env" && (
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
                    )}
                  </form>
                </Form>
              </CardContent>

              {isServerAvailable ? (
                <CardFooter className="border-t bg-muted/50 rounded-b-lg pt-4">
                  <div className="flex w-full justify-between items-center ">
                    <Button
                      variant="outline"
                      onClick={handleTestConnection}
                      disabled={isLoadingCredentials}
                      className="w-40"
                    >
                      <RefreshCw className="mr-2 h-4 w-4" />
                      Test Connection
                    </Button>
                    <span className="text-sm font-mono font-semibold text-green-500">
                      Server version: {version} - Connected
                    </span>
                  </div>
                </CardFooter>
              ) : error ? (
                <CardFooter className="border-t bg-muted/50 rounded-b-lg pt-4">
                  <span className="text-sm font-mono font-semibold text-red-500">
                    {error} 
                  </span>
                </CardFooter>
              ) : null}
            </Card>
          </div>
        </div>
      </div>
    </TooltipProvider>
  );
}
