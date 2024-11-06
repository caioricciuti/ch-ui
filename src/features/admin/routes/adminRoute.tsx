import { Navigate } from "react-router-dom";
import useAppStore from "@/store";
import { ReactNode, useEffect } from "react";
import { toast } from "sonner";

export function AdminRoute({ children }: { children: ReactNode }) {
  const { isAdmin } = useAppStore();

  if (!isAdmin) {
    toast.warning(`You don't have permission to access this page.`);
    return <Navigate to="/" replace />;
  }
  return <>{children}</>;
}
