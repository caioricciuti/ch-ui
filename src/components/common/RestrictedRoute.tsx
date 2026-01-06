import { Navigate } from "react-router-dom";
import useAppStore from "@/store";

interface RestrictedRouteProps {
    children: React.ReactNode;
}

export const RestrictedRoute = ({ children }: RestrictedRouteProps) => {
    const { isAdmin, isInitialized } = useAppStore();

    // If app not initialized, maybe wait? But usually this is inside MainLayout so Init is done.
    // If not admin, redirect to Explorer
    if (isInitialized && !isAdmin) {
        return <Navigate to="/explorer" replace />;
    }

    return <>{children}</>;
};
