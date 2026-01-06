import { useEffect, useState, ReactNode } from "react";
import { MultiStepLoader as Loader } from "@/components/ui/multi-step-loader";


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
  const [envChecked, setEnvChecked] = useState(false);

  // Effect to set credentials from environment variables - DEPRECATED in favor of Login page
  // Environment variables are now only used to populate defaults in the Login page
  useEffect(() => {
    // We only mark env as checked, we don't auto-login anymore
    setEnvChecked(true);
  }, []);

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
