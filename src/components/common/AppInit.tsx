import { useEffect, useState, ReactNode } from "react";
import { MultiStepLoader as Loader } from "@/components/ui/multi-step-loader";

declare global {
  interface Window {
    env?: {
      VITE_CLICKHOUSE_URL?: string;
      VITE_CLICKHOUSE_USER?: string;
      VITE_CLICKHOUSE_PASS?: string;
      VITE_CLICKHOUSE_USE_ADVANCED?: boolean;
      VITE_CLICKHOUSE_CUSTOM_PATH?: string;
      VITE_CLICKHOUSE_REQUEST_TIMEOUT?: number;
      VITE_BASE_PATH?: string;
    };
  }
}
import useAppStore from "@/store";
import { toast } from "sonner";

const AppInitializer = ({ children }: { children: ReactNode }) => {
  const loadingStates = [
    {
      text: "Initializing application...",
    },
    {
      text: "Checking if you are an admin...",
    },
    {
      text: "Loading metrics...",
    },
    {
      text: "Loading settings...",
    },
  ];

  const {
    initializeApp,
    error,
    setCredential,
    setCredentialSource,
    checkIsAdmin,
    credentialSource,
    isSessionValid,
    logout,
  } = useAppStore();
  const [isLoading, setIsLoading] = useState(true);
  const [envChecked, setEnvChecked] = useState(false);

  // Effect to check for environment variables (Docker mode)
  useEffect(() => {
    // Check if credentials are set from environment variables
    const envUrl = window.env?.VITE_CLICKHOUSE_URL;
    const envUser = window.env?.VITE_CLICKHOUSE_USER;
    const envPass = window.env?.VITE_CLICKHOUSE_PASS;
    const envUseAdvanced = window.env?.VITE_CLICKHOUSE_USE_ADVANCED;
    const envCustomPath = window.env?.VITE_CLICKHOUSE_CUSTOM_PATH;
    const envRequestTimeout = window.env?.VITE_CLICKHOUSE_REQUEST_TIMEOUT;

    console.log("AppInit: Checking environment variables...");
    console.log("AppInit: envUrl:", envUrl ? "SET" : "NOT SET");
    console.log("AppInit: envUser:", envUser ? "SET" : "NOT SET");

    // Only auto-set credentials for Docker/env mode
    if (envUrl && envUser) {
      console.log("AppInit: Setting credentials from environment variables");
      setCredential({
        url: envUrl,
        username: envUser,
        password: envPass || "",
        useAdvanced: envUseAdvanced || false,
        customPath: envCustomPath || "",
        requestTimeout: envRequestTimeout || 30000,
      });
      setCredentialSource("env");
    }

    // Clear window.env after reading to reduce credential exposure
    if (window.env) {
      delete window.env;
    }

    setEnvChecked(true);
  }, [setCredential, setCredentialSource]);

  // Effect to validate existing sessions on app start
  useEffect(() => {
    if (credentialSource === "session" && !isSessionValid()) {
      console.log("AppInit: Session expired, clearing credentials");
      logout();
    }
  }, [credentialSource, isSessionValid, logout]);

  // Effect to initialize the application after env check
  useEffect(() => {
    if (!envChecked) return;
    
    const init = async () => {
      try {
        await initializeApp();
        await checkIsAdmin();
      } catch (err) {
        console.error("Initialization failed:", err);
      } finally {
        setIsLoading(false);
      }
    };
    init();
  }, [envChecked, initializeApp, checkIsAdmin]);

  // Effect to handle initialization errors
  useEffect(() => {
    if (error) {
      toast.error(`Failed to initialize application: ${error}`);
    }
  }, [error]);

  // Loading state
  if (isLoading) {
    return (
      <Loader
        loadingStates={loadingStates}
        loading={isLoading}
        duration={1000}
      />
    );
  }

  return <>{children}</>;
};

export default AppInitializer;
