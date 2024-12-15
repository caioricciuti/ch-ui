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
  } = useAppStore();
  const [isLoading, setIsLoading] = useState(true);

  // Effect to set credentials from environment variables
  useEffect(() => {
    // Check if credentials are set from environment variables
    const envUrl = window.env?.VITE_CLICKHOUSE_URL;
    const envUser = window.env?.VITE_CLICKHOUSE_USER;
    const envPass = window.env?.VITE_CLICKHOUSE_PASS;
    const envUseAdvanced = window.env?.VITE_CLICKHOUSE_USE_ADVANCED;
    const envCustomPath = window.env?.VITE_CLICKHOUSE_CUSTOM_PATH;

    if (envUrl && envUser) {
      setCredential({
        url: envUrl,
        username: envUser,
        password: envPass || "",
        useAdvanced: envUseAdvanced || false,
        customPath: envCustomPath || "",
      });
      setCredentialSource("env");
    }
  }, [setCredential]);

  // Effect to initialize the application
  useEffect(() => {
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
  }, [initializeApp]);

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
