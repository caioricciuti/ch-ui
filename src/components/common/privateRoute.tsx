import { Navigate } from "react-router-dom";
import useAppStore from "@/store";
import { ReactNode, useEffect } from "react";

export function PrivateRoute({ children }: { children: ReactNode }) {
  const {
    isServerAvailable,
    credentialSource,
    isSessionValid,
    logout
  } = useAppStore();

  useEffect(() => {
    // Check if session has expired for session-based authentication
    if (credentialSource === "session" && !isSessionValid()) {
      logout();
    }
  }, [credentialSource, isSessionValid, logout]);

  // Redirect to login if not authenticated
  if (!isServerAvailable || (credentialSource === "session" && !isSessionValid())) {
    return <Navigate to="/login" replace />;
  }

  return <>{children}</>;
}
