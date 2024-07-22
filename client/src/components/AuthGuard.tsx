import React, { useEffect, useState } from "react";
import { Outlet, useNavigate, useLocation } from "react-router-dom";
import useAuthStore from "@/stores/user.store";
import { toast } from "sonner";

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
        navigate("/login", { state: { from: location }, replace: true });
        toast.warning("Please Login to continue");
      }
    };

    verifyAuth();
  }, [checkAuth, navigate, location]);

  if (isChecking) {
    return <div>Loading...</div>; // Or a proper loading component
  }

  return user ? <Outlet /> : null;
};

export default AuthGuard;
