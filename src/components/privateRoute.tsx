import { Navigate } from "react-router-dom";
import useAppStore from "@/store/appStore";
import { ReactNode } from "react";

export function PrivateRoute({ children }: { children: ReactNode }) {
  const { isServerAvailable } = useAppStore();
  if (!isServerAvailable) {
    return <Navigate to="/settings" replace />;
  }
  return <>{children}</>;
}
