import React, { useEffect, useState, ReactNode } from "react";
import { useNavigate } from "react-router-dom";
import useAppStore from "@/store/appStore";
import { Loader2 } from "lucide-react";
import { toast } from "sonner";

const AppInitializer = ({ children }: { children: ReactNode }) => {
  const { initializeApp, isInitialized, error } = useAppStore();
  const [isLoading, setIsLoading] = useState(true);

  useEffect(() => {
    const init = async () => {
      await initializeApp();
      setIsLoading(false);
    };
    init();
  }, [initializeApp]);

  useEffect(() => {
    if (error) {
      toast.error(`Failed to initialize application: ${error}`);
    }
  }, [error]);

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
