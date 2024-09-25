// src/components/AuthGuard.tsx
import React, { useEffect, useState } from "react";
import { Outlet, useNavigate, useLocation } from "react-router-dom";
import useAuthStore from "@/stores/user.store";
import { toast } from "sonner";
//import LoadingScreen from "./LoadingScreen";

const AuthGuard: React.FC = () => {
  const { user, checkAuth } = useAuthStore();
  const navigate = useNavigate();
  const location = useLocation();
  const [isChecking, setIsChecking] = useState(true);

  useEffect(() => {
    const verifyAuth = async () => {
      const isAuthenticated = await checkAuth();
      setIsChecking(false);
      if (!isAuthenticated) {
        navigate("/account/login", {
          state: { from: location },
          replace: true,
        });
        toast.warning("Please Login to continue");
      }
    };

    verifyAuth();
  }, [checkAuth, navigate, location]);

  if (isChecking) {
    return null;
  }

  return user ? <Outlet /> : null;
};

export default AuthGuard;
