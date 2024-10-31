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
  AlertTriangle,
  LogOut,
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
import { toast } from "sonner";
import { useNavigate } from "react-router-dom";
import useAppStore from "@/store";
import { retryInitialization } from "@/features/workspace/editor/monacoConfig";

const formSchema = z.object({
  host: z.string().url("Invalid URL").min(1, "URL is required"),
  username: z.string().min(1, "Username is required"),
  password: z.string().optional(),
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
    },
  });

  const onSubmit = async (values: z.infer<typeof formSchema>) => {
    try {
      // if current values are the same as the form values and it's connected, do nothing
      if (
        values.host === currentFormValues.host &&
        values.username === currentFormValues.username &&
        values.password === currentFormValues.password &&
        isServerAvailable
      ) {
        toast.info("No changes detected.");
        return;
      }
      await setCredential({
        ...values,
        password: values.password || "",
      });
      await checkServerStatus();
      setCredentialSource("app");
      // retry to load monacoconfig
      await retryInitialization();
    } catch (error) {
      toast.error("Error saving credentials: " + (error as Error).message);
    }
  };

  const handleDisconnect = () => {
    clearCredentials();
    // reset form values
    form.reset({
      host: "",
      username: "",
      password: "",
    });

    toast.success("Disconnected from ClickHouse server.");
    navigate("/settings");
  };

  return (
    <div className="max-w-3xl m-auto h-full lg:h-screen flex items-center">
      <div className="space-y-12">
        <Card>
          <CardHeader>
            {credentialSource === "env" ? (
              <Alert variant="info" className="mb-4">
                <AlertTitle className="flex items-center">
                  Using Environment Variables
                </AlertTitle>
                <AlertDescription>
                  Your ClickHouse credentials are set using environment
                  variables. Please update your environment variables to change
                  the ClickHouse connection settings.
                </AlertDescription>
              </Alert>
            ) : (
              <>
                <CardTitle className="text-2xl font-bold flex items-center">
                  <Settings className="mr-2" size={24} />
                  Connection Settings
                </CardTitle>
                <CardDescription>
                  Configure your connection to the ClickHouse server
                </CardDescription>
              </>
            )}
          </CardHeader>
          <CardContent>
            <Form {...form}>
              <form
                onSubmit={form.handleSubmit(onSubmit)}
                className="space-y-4"
              >
                <FormField
                  control={form.control}
                  name="host"
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>ClickHouse Host</FormLabel>
                      <FormControl>
                        <Input
                          disabled={
                            isLoadingCredentials || credentialSource === "env"
                          }
                          placeholder="https://your-clickhouse-host:<PORT>"
                          {...field}
                        />
                      </FormControl>
                      <FormDescription>
                        The URL of your ClickHouse server, including protocol
                        and port.
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
                      <FormLabel>Username</FormLabel>
                      <FormControl>
                        <Input
                          disabled={
                            isLoadingCredentials || credentialSource === "env"
                          }
                          placeholder="ClickHouse username"
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
                      <FormLabel>Password</FormLabel>
                      <FormControl>
                        <div className="relative">
                          <Input
                            disabled={
                              isLoadingCredentials || credentialSource === "env"
                            }
                            type={showPassword ? "text" : "password"}
                            autoComplete="current-password"
                            {...field}
                          />
                          <button
                            type="button"
                            onClick={() => setShowPassword(!showPassword)}
                            className="absolute inset-y-0 right-0 pr-3 flex items-center text-gray-400 cursor-pointer"
                          >
                            {showPassword ? (
                              <EyeOff size={20} />
                            ) : (
                              <Eye size={20} />
                            )}
                          </button>
                        </div>
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                {credentialSource !== "env" && (
                  <div className="flex space-x-4">
                    <Button type="submit" disabled={isLoadingCredentials}>
                      {isLoadingCredentials ? (
                        <>
                          Connecting{" "}
                          <Loader2 className="ml-2 h-4 w-4 animate-spin" />
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
          {isServerAvailable && (
            <CardFooter className="flex justify-between">
              <Button
                variant="outline"
                onClick={async () => {
                  await checkServerStatus();
                  if (isServerAvailable) {
                    toast.success("Connection successful!");
                  } else if (error) {
                    toast.error(`Connection failed: ${error}`);
                  }
                }}
                disabled={isLoadingCredentials}
              >
                <RefreshCw className="mr-2 h-4 w-4" />
                Test Connection
              </Button>
            </CardFooter>
          )}
        </Card>
        {isServerAvailable ? (
          <Alert
            variant="success"
            className="mt-2 flex items-start border-green-500 p-4 transition-all duration-300 ease-in-out transform hover:scale-105"
          >
            <div>
              <AlertTitle className="text-lg font-bold text-green-700 mb-2">
                Connection Successful!
              </AlertTitle>
              <AlertDescription className="flex items-center">
                <span className="mr-2">Server version: {version}</span>
                <Button
                  variant="ghost"
                  onClick={() => navigate("/")}
                  className="ml-2 px-4 py-2 text-green-600 border border-green-600 rounded-md hover:bg-green-500 transition-colors duration-200"
                >
                  Start Exploring
                </Button>
              </AlertDescription>
            </div>
          </Alert>
        ) : error ? (
          <Alert variant="destructive" className="mt-4">
            <AlertTitle className="flex items-center">
              <AlertTriangle className="mr-2" /> Connection Failed
            </AlertTitle>
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        ) : null}
      </div>
    </div>
  );
}
