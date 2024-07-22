import React, { useEffect, useState } from "react";
import Routes from "@/routes";
import { ThemeProvider } from "@/components/theme-provider";
import useAuthStore from "@/stores/user.store";
import { FlipWords } from "./components/ui/flip-words";
import Logo from "/logo.png";

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

  if (isLoading) {
    return (
      <>
        {/* LOADING PAGE WITH SPINNER USING SHADCN AND TAILWIND */}
        <div
          className="fixed flex-col inset-0 text-4xl font-bold z-50 flex items-center justify-center"
          style={{ backdropFilter: "blur(5px)" }}
        >
          <img src={Logo} alt="Loading..." className="w-24 h-24 mb-6" />
          <FlipWords
            words={["Loading...", "Please wait...", "Just a moment..."]}
            duration={2000}
          />
        </div>
        {/* LOADING PAGE WITH SPINNER USING SHADCN AND TAILWIND */}
      </>
    ); // Or a proper loading component
  }

  return (
    <ThemeProvider defaultTheme="dark" storageKey="vite-ui-theme">
      <Routes />
    </ThemeProvider>
  );
};

export default App;
