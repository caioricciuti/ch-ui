import { useState, useEffect } from "react";
import { Settings, Loader2, Eye, EyeOff, ContainerIcon } from "lucide-react";
import { useClickHouseState } from "@/providers/ClickHouseContext";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { toast } from "sonner";
import { Link } from "react-router-dom";
import { deleteDatabase } from "@/lib/tablesIndexedDB";
import { Progress } from "@/components/ui/progress";

export default function SettingsPage() {
  const {
    credentials,
    setCredentials,
    isLoading,
    setIsLoading,
    isServerAvailable,
    checkServerStatus,
  } = useClickHouseState();

  // Local state for form inputs
  const [clickHouseUrl, setClickHouseUrl] = useState(credentials.url || "");
  const [clickHouseUser, setClickHouseUser] = useState(
    credentials.username || ""
  );
  const [clickHousePassword, setClickHousePassword] = useState(
    credentials.password || ""
  );
  const [showPassword, setShowPassword] = useState(false);
  const [isResetting, setIsResetting] = useState(false);
  const [resetProgress, setResetProgress] = useState(0);
  const [resetStep, setResetStep] = useState("");
  const [credentialsSource, setCredentialsSource] = useState("manual");

  const handleDockerSubmit = async (url, user, pass) => {
    try {
      setClickHouseUrl(url);
      setClickHouseUser(user);
      setClickHousePassword(pass);
      setCredentialsSource("docker");
      await setCredentials({
        url,
        username: user,
        password: pass,
      });
      toast.success("Credentials were set from Docker environment variables");
    } catch (error) {
      toast.error("Error saving credentials: " + error);
    }
  };

  useEffect(() => {
    // Check if credentials are set from environment variables
    const envUrl = window.env?.VITE_CLICKHOUSE_URL;
    const envUser = window.env?.VITE_CLICKHOUSE_USER;
    const envPass = window.env?.VITE_CLICKHOUSE_PASS;

    if (envUrl && envUser) {
      setClickHouseUrl(envUrl);
      setClickHouseUser(envUser);
      setClickHousePassword(envPass || "");
      setCredentialsSource("docker");
      handleDockerSubmit(envUrl, envUser, envPass);
    }
  }, []);

  const handleSubmit = async () => {
    setIsLoading(true);
    try {
      if (!clickHouseUrl || !clickHouseUser) {
        toast.error("Please fill all fields");
        setIsLoading(false);
        return;
      }

      localStorage.setItem(
        "chCredentials",
        JSON.stringify({
          url: clickHouseUrl,
          username: clickHouseUser,
          password: clickHousePassword,
        })
      );

      await setCredentials({
        url: clickHouseUrl,
        username: clickHouseUser,
        password: clickHousePassword,
      });
    } catch (error) {
      toast.error("Error saving credentials: " + error);
      setIsLoading(false);
    }
  };

  const hardResetAll = async (e) => {
    setIsResetting(true);
    setResetProgress(0);
    setResetStep("Starting reset...");
    e.preventDefault();
    if (confirm("Are you sure you want to reset the app?")) {
      try {
        setIsLoading(true);
        setResetProgress(10);
        setResetStep("Removing chCredentials from localStorage...");
        localStorage.removeItem("chCredentials");
        setResetProgress(20);
        setResetStep("Removing availableDatabases from localStorage...");
        localStorage.removeItem("availableDatabases");
        setResetProgress(30);
        setResetStep("Removing selectedDatabase from localStorage...");
        localStorage.removeItem("selectedDatabase");
        setResetProgress(40);
        setResetStep("Removing activeTab from localStorage...");
        localStorage.removeItem("activeTab");
        setResetProgress(50);
        setResetStep("Resetting credentials...");
        setCredentials({
          url: "",
          username: "",
          password: "",
        });
        setResetProgress(60);
        setResetStep("Deleting tabs_indexed_db...");
        await deleteDatabase("tabs_indexed_db");
        setResetProgress(70);
        setResetStep("Deleting tables_indexed_db...");
        await deleteDatabase("tables_indexed_db");
        setResetProgress(80);
        setResetStep("Finalizing...");
        window.onbeforeunload = null;
        setResetProgress(100);
        setResetStep("Reloading...");
        window.location.reload();
        toast.success("All cache reset successfully");
      } catch (error) {
        toast.error("Error resetting app: " + error);
        setIsResetting(false);
      }
    } else {
      toast.info("Reset canceled");
      setIsResetting(false);
    }
  };

  return (
    <>
      {isResetting && (
        <div className="absolute w-[100vw] h-[100vh] bg-black/50 flex items-center justify-center">
          <div className="p-4 rounded-lg">
            <Progress value={resetProgress} className="w-[250px]" />
            <p className="text-xs font-thin mt-2 flex items-center">
              {resetStep || "reseting..."}{" "}
              <Loader2 size={18} className="ml-2 animate-spin" />
            </p>
            <p className="text-xs font-bold mt-2">
              Plase don't leave this page
            </p>
            <p className="text-xs mt-2">
              If it's the first time this is running it takes a while!{" "}
            </p>
          </div>
        </div>
      )}
      <div className="p-6 flex flex-col">
        <div className="flex items-center">
          <h1 className="text-2xl font-bold">Settings Page</h1>
          <Settings className="ml-4" size={24} />
        </div>

        {credentialsSource === "docker" && (
          <Alert variant="waring" className="mt-4 max-w-lg text-amber-500">
            <AlertTitle className="flex items-center">
              <ContainerIcon className="mr-4" /> Credentials Set from Docker (or
              Environment Variables)
            </AlertTitle>
            <AlertDescription className="text-xs mt-4 text-amber-500">
              Your credentials were set using Docker environment variables. You
              can't change them here. If you want to change them, you need to
              restart the app with new environment variables. Please note that
              if you don't set the environment variables, the app will use the
              manual credentials.
            </AlertDescription>
          </Alert>
        )}

        <div className="flex flex-col mt-4">
          <Label htmlFor="clickhouseUrl" className="text-sm font-semibold">
            ClickHouse URL
          </Label>
          <Input
            disabled={
              isLoading || isResetting || credentialsSource === "docker"
            }
            id="clickhouseUrl"
            type="url"
            className="mt-2 max-w-md"
            placeholder="http://localhost:8123 | https://myinstance.com:<PORT>"
            value={clickHouseUrl}
            onChange={(e) => setClickHouseUrl(e.target.value)}
          />
          <Label
            htmlFor="clickhouseUser"
            className="text-sm font-semibold mt-4"
          >
            Username
          </Label>
          <Input
            disabled={
              isLoading || isResetting || credentialsSource === "docker"
            }
            id="clickhouseUser"
            type="text"
            className="mt-2 max-w-md"
            placeholder="Click house user (default)"
            value={clickHouseUser}
            onChange={(e) => setClickHouseUser(e.target.value)}
          />
          <Label
            htmlFor="clickhousePassword"
            className="text-sm font-semibold mt-4"
          >
            Password
          </Label>
          <div className="relative flex items-center max-w-md">
            <Input
              disabled={
                isLoading || isResetting || credentialsSource === "docker"
              }
              id="clickhousePassword"
              type={showPassword ? "text" : "password"}
              autoComplete="password"
              className="mt-2 max-w-md pr-6" // Add padding to prevent text from overlapping with the icon
              placeholder="Password"
              value={clickHousePassword}
              onChange={(e) => setClickHousePassword(e.target.value)}
            />
            {showPassword ? (
              <EyeOff
                className="absolute cursor-pointer right-2 top-1/2 mt-1 transform -translate-y-1/2 text-red-400"
                size={24}
                onClick={() => setShowPassword(!showPassword)}
              />
            ) : (
              <Eye
                className="absolute cursor-pointer right-2 top-1/2 mt-1 transform -translate-y-1/2 text-gray-400"
                size={24}
                onClick={() => setShowPassword(!showPassword)}
              />
            )}
          </div>
          <Button
            disabled={
              isLoading || isResetting || credentialsSource === "docker"
            }
            type="submit"
            onClick={(e) => handleSubmit(e)}
            className="mt-4 max-w-md"
          >
            {isLoading ? (
              <>
                Loading <Loader2 className="ml-2 animate-spin" />
              </>
            ) : (
              "Submit"
            )}
          </Button>
        </div>

        <div className="mt-6">
          <p className="text-sm mt-2">
            {isServerAvailable ? (
              <span className="text-green-500">
                Server is available and ready to use. <br />
                <Link className="text-primary" to="/">
                  Go back to the home page.
                </Link>
              </span>
            ) : (
              "Server is not available"
            )}
          </p>
        </div>

        <div className="mt-6">
          <Button
            disabled={isLoading || isResetting}
            onClick={(e) => hardResetAll(e)}
            className="mt-4 max-w-md"
          >
            {isResetting ? (
              <div className="flex items-center">
                Reseting cache <Loader2 className="ml-2 animate-spin" />
              </div>
            ) : (
              "Hard Reset"
            )}
          </Button>
        </div>
      </div>
    </>
  );
}
