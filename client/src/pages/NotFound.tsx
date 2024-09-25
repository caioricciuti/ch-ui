// src/pages/NotFound.tsx
import React, { useEffect, useState } from "react";
import Logo from "/logo.png";
import { Separator } from "@/components/ui/separator";
import { Button } from "@/components/ui/button";
import useAuthStore from "@/stores/user.store";
import { useNavigate } from "react-router-dom";
import { SignpostBig } from "lucide-react";

const NotFound: React.FC = () => {
  const { user } = useAuthStore();
  const navigate = useNavigate();
  const [navigationPath, setNavigationPath] = useState("");

  useEffect(() => {
    if (!user) {
      setNavigationPath("/account/login");
    }
    setNavigationPath("/");
  }, [user]);
  return (
    <div className="flex w-full h-screen">
      <div className="flex items-center m-auto">
        <img src={Logo} alt="logo" className="w-28 h-28" />
        <Separator orientation="vertical" className="p-0.5 h-32 mr-6 ml-4" />
        <div>
          <h1 className="font-semibold text-2xl">404 - Page Not Found</h1>
          <p className="text-primary/60">
            Sorry, the page you are looking for does not exist.
          </p>
          <Button
            variant="ghost"
            className="mt-4"
            onClick={() => {
              navigate(navigationPath);
            }}
          >
            <SignpostBig className="h-8" />
            <span className="ml-4">Show me the way...</span>
          </Button>
        </div>
      </div>
    </div>
  );
};

export default NotFound;
