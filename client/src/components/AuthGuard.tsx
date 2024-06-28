// src/components/AuthGuard.tsx

import React, { useEffect, useState } from "react";
import { Outlet, useNavigate } from "react-router-dom";
import useAuthStore from "@/stores/user.store";

const AuthGuard: React.FC = () => {
  const { isAuthenticated, getCurrentUser } = useAuthStore();
  const navigate = useNavigate();
  const [isChecking, setIsChecking] = useState(true);

  useEffect(() => {
    const checkAuth = async () => {
      console.log("AuthGuard: Checking authentication");
      if (!isAuthenticated) {
        console.log("AuthGuard: Not authenticated, attempting to get current user");
        try {
          await getCurrentUser();
          console.log("AuthGuard: Successfully retrieved current user");
        } catch (error) {
          console.log("AuthGuard: Failed to get current user, redirecting to login");
          navigate("/login");
        }
      } else {
        console.log("AuthGuard: User is authenticated");
      }
      setIsChecking(false);
    };

    checkAuth();
  }, [isAuthenticated, getCurrentUser, navigate]);

  if (isChecking) {
    console.log("AuthGuard: Still checking authentication");
    return <div>Loading...</div>; // Or a proper loading component
  }

  if (!isAuthenticated) {
    console.log("AuthGuard: Not authenticated after check, redirecting to login");
    navigate("/login");
    return null;
  }

  console.log("AuthGuard: Authentication check passed, rendering protected content");
  return <Outlet />;
};

export default AuthGuard;