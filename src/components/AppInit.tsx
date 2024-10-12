import { useEffect, useState, ReactNode } from "react";

declare global {
  interface Window {
    env?: {
      VITE_CLICKHOUSE_URL?: string;
      VITE_CLICKHOUSE_USER?: string;
      VITE_CLICKHOUSE_PASS?: string;
    };
  }
}
import useAppStore from "@/store/appStore";
import { Loader2 } from "lucide-react";
import { toast } from "sonner";

const AppInitializer = ({ children }: { children: ReactNode }) => {
  const {
    initializeApp,
    isInitialized,
    error,
    setCredential,
    credentialSource,
    setCredentialSource,
  } = useAppStore();
  const [isLoading, setIsLoading] = useState(true);

  // Effect to set credentials from environment variables
  useEffect(() => {
    // Check if credentials are set from environment variables
    const envUrl = window.env?.VITE_CLICKHOUSE_URL;
    const envUser = window.env?.VITE_CLICKHOUSE_USER;
    const envPass = window.env?.VITE_CLICKHOUSE_PASS;

    if (envUrl && envUser) {
      setCredential({
        host: envUrl,
        username: envUser,
        password: envPass || "",
      });
      setCredentialSource("env");
    }
  }, [setCredential]);

  // Effect to initialize the application
  useEffect(() => {
    const init = async () => {
      try {
        await initializeApp();
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
      <div className="flex items-center justify-center h-screen">
        <Loader2 className="w-8 h-8 animate-spin" />
        <span className="ml-2">Initializing application...</span>
      </div>
    );
  }

  return <>{children}</>;
};

export default AppInitializer;
