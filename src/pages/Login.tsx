import { useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import * as z from "zod";
import {
  Loader2,
  Eye,
  EyeOff,
  Server,
  User,
  Lock,
  Database,
  Shield,
  Cog,
  FileClock,
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
  username: z.string().min(1, "Username is required"),
  password: z.string().optional(),
  useAdvanced: z.boolean().optional(),
  customPath: z.string().optional(),
  requestTimeout: z.coerce
    .number()
    .int("Request timeout must be a whole number")
    .min(1000, "Request timeout must be at least 1000 milliseconds")
    .max(600000, "Request timeout must not exceed 600000 milliseconds"),
  rememberCredentials: z.boolean().optional(),
});

type FormData = z.infer<typeof formSchema>;

export default function LoginPage() {
  document.title = "CH-UI | Login";

  const {
    setCredential,
    isLoadingCredentials,
    error,
    setCredentialSource,
  } = useAppStore();

  const [showPassword, setShowPassword] = useState(false);
  const [showAdvancedSettings, setShowAdvancedSettings] = useState(false);
  const navigate = useNavigate();

  const form = useForm<FormData>({
    resolver: zodResolver(formSchema),
    defaultValues: {
      url: "",
      username: "default",
      password: "",
      requestTimeout: 30000,
      useAdvanced: false,
      customPath: "",
      rememberCredentials: false,
    },
  });

  const onSubmit = async (values: FormData) => {
    try {
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
        requestTimeout: Number(values.requestTimeout),
      });

      // Set credential source based on whether user wants to remember credentials
      setCredentialSource(values.rememberCredentials ? "app" : "session");

      await retryInitialization();

      // Navigate to home page after successful login
      navigate("/", { replace: true });

      toast.success("Successfully connected to ClickHouse!", {
        description: "Welcome to CH-UI dashboard.",
      });
    } catch (error) {
      toast.error("Login failed: " + (error as Error).message);
    }
  };

  return (
    <TooltipProvider>
      <div className="min-h-screen bg-gradient-to-br from-background via-background to-muted/20 flex items-center justify-center p-4">
        <div className="w-full max-w-md space-y-6">
          {/* Header */}
          <div className="text-center space-y-2">
            <div className="flex justify-center">
              <div className="bg-primary/10 p-3 rounded-full">
                <Database className="h-8 w-8 text-primary" />
              </div>
            </div>
            <h1 className="text-3xl font-bold">CH-UI</h1>
            <p className="text-muted-foreground">
              Connect to your ClickHouse database
            </p>
          </div>

          {/* Login Form */}
          <Card className="shadow-lg border-muted">
            <CardHeader className="space-y-1">
              <CardTitle className="text-2xl font-bold flex items-center gap-2">
                <Shield className="h-5 w-5 text-primary" />
                Database Login
              </CardTitle>
              <CardDescription>
                Enter your ClickHouse server credentials to continue
              </CardDescription>
            </CardHeader>

            <CardContent>
              <Form {...form}>
                <form
                  onSubmit={form.handleSubmit(onSubmit)}
                  className="space-y-4"
                >
                  <FormField
                    control={form.control}
                    name="url"
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel className="flex items-center gap-2">
                          <Server className="h-4 w-4" />
                          ClickHouse Server URL
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
                          The URL of your ClickHouse server, including protocol and port
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
                              placeholder="Enter password (optional)"
                              {...field}
                            />
                            <Tooltip>
                              <TooltipTrigger asChild>
                                <button
                                  type="button"
                                  onClick={() => setShowPassword(!showPassword)}
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
                                {showPassword ? "Hide password" : "Show password"}
                              </TooltipContent>
                            </Tooltip>
                          </div>
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />

                  {/* Advanced Settings */}
                  <Separator className="my-4" />

                  <FormField
                    control={form.control}
                    name="useAdvanced"
                    render={({ field }) => (
                      <FormItem className="flex flex-row items-start space-x-3 space-y-0 rounded-md border p-3 shadow-sm">
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
                          <FormLabel className="flex items-center gap-2 text-sm">
                            <Cog className="h-3 w-3" />
                            Advanced Settings
                          </FormLabel>
                          <FormDescription className="text-xs">
                            Configure custom path and timeout settings
                          </FormDescription>
                        </div>
                      </FormItem>
                    )}
                  />

                  {showAdvancedSettings && (
                    <div className="space-y-4 border-l-2 border-muted pl-4">
                      <FormField
                        control={form.control}
                        name="customPath"
                        render={({ field }) => (
                          <FormItem>
                            <FormLabel className="text-sm">Custom Path</FormLabel>
                            <FormControl>
                              <Input
                                className="font-mono"
                                disabled={isLoadingCredentials}
                                placeholder="clickhouse-{cluster_name}"
                                {...field}
                              />
                            </FormControl>
                            <FormDescription className="text-xs">
                              Specify custom path for proxy routing
                            </FormDescription>
                            <FormMessage />
                          </FormItem>
                        )}
                      />

                      <FormField
                        control={form.control}
                        name="requestTimeout"
                        render={({ field }) => (
                          <FormItem>
                            <FormLabel className="flex items-center gap-2 text-sm">
                              <FileClock className="h-3 w-3" />
                              Request Timeout (ms)
                            </FormLabel>
                            <FormControl>
                              <Input
                                className="font-mono"
                                disabled={isLoadingCredentials}
                                type="number"
                                min={1000}
                                max={600000}
                                placeholder="30000"
                                value={typeof field.value === "number" || typeof field.value === "string" ? field.value : ""}
                                onChange={(e) => field.onChange(e.target.value)}
                                onBlur={field.onBlur}
                                name={field.name}
                                ref={field.ref}
                              />
                            </FormControl>
                            <FormDescription className="text-xs">
                              Timeout between 1000-600000ms (Default: 30000)
                            </FormDescription>
                            <FormMessage />
                          </FormItem>
                        )}
                      />
                    </div>
                  )}

                  {/* Remember Credentials */}
                  <FormField
                    control={form.control}
                    name="rememberCredentials"
                    render={({ field }) => (
                      <FormItem className="flex flex-row items-start space-x-3 space-y-0 rounded-md border p-3 shadow-sm">
                        <FormControl>
                          <Checkbox
                            checked={field.value}
                            onCheckedChange={field.onChange}
                          />
                        </FormControl>
                        <div className="space-y-1 leading-none">
                          <FormLabel className="text-sm">Remember credentials</FormLabel>
                          <FormDescription className="text-xs">
                            Save credentials locally for future sessions (less secure)
                          </FormDescription>
                        </div>
                      </FormItem>
                    )}
                  />

                  <Button
                    type="submit"
                    disabled={isLoadingCredentials}
                    className="w-full"
                    size="lg"
                  >
                    {isLoadingCredentials ? (
                      <>
                        <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                        Connecting...
                      </>
                    ) : (
                      "Connect to Database"
                    )}
                  </Button>
                </form>
              </Form>
            </CardContent>

            {error && (
              <CardFooter className="border-t bg-muted/50 rounded-b-lg pt-4">
                <Alert variant="destructive" className="w-full">
                  <AlertTitle className="flex items-center font-semibold text-red-500">
                    Connection Error
                  </AlertTitle>
                  <AlertDescription>
                    <div className="mt-2 space-y-2">
                      <p className="font-medium">
                        {error.split("\n\n")[0]}
                      </p>

                      {error.includes("Troubleshooting tips:") && (
                        <div className="mt-4">
                          <h4 className="font-medium mb-2">
                            Troubleshooting Tips:
                          </h4>
                          <ul className="list-disc pl-5 space-y-1 text-sm">
                            {error
                              .split("Troubleshooting tips:\n")[1]
                              ?.split("\n")
                              .map((tip, index) => (
                                <li key={index}>{tip}</li>
                              ))}
                          </ul>
                        </div>
                      )}
                    </div>
                  </AlertDescription>
                </Alert>
              </CardFooter>
            )}
          </Card>

          {/* Footer */}
          <div className="text-center text-sm text-muted-foreground">
            <p>Secure runtime authentication for ClickHouse</p>
          </div>
        </div>
      </div>
    </TooltipProvider>
  );
}