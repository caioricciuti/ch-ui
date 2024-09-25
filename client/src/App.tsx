// src/App.tsx
import React, { useEffect, useState } from "react";
import Routes from "@/routes";
import { ThemeProvider } from "@/components/theme-provider";
import useAuthStore from "@/stores/user.store";
import Logo from "/logo.png";
import { Toaster } from "sonner";

const App: React.FC = () => {
  const { checkAuth } = useAuthStore();
  const [isLoading, setIsLoading] = useState(true);

  useEffect(() => {
    const initAuth = async () => {
      await checkAuth();
      setIsLoading(false);
    };

    initAuth();
  }, [checkAuth]);

  return (
    <ThemeProvider defaultTheme="system" storageKey="vite-ui-theme">
      {isLoading && (
        <>
          <div
            className="fixed flex-col inset-0 text-4xl font-bold z-50 flex items-center justify-center"
            style={{ backdropFilter: "blur(5px)" }}
          >
            <img src={Logo} alt="Loading..." className="w-24 h-24 mb-6" />
            <div className="flex">
              <p>Loading</p>
              <p className="animate-bounce ml-2">...</p>
            </div>
          </div>
        </>
      )}
      <Routes />
    </ThemeProvider>
  );
};

export default App;
