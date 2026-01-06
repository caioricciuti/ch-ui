import React from "react";
import useAppStore from "@/store";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";

interface PermissionGuardProps {
    requiredPermission: string;
    children: React.ReactNode;
    fallback?: React.ReactNode;
    showTooltip?: boolean;
}

const PermissionGuard: React.FC<PermissionGuardProps> = ({
    requiredPermission,
    children,
    fallback = null,
    showTooltip = false,
}) => {
    const { permissions, isAdmin } = useAppStore();

    const hasPermission = React.useMemo(() => {
        if (isAdmin) return true;
        if (!permissions) return false;

        // Check for exact permission or relevant wildcard
        // e.g., 'CREATE DATABASE' is covered by 'CREATE DATABASE', 'CREATE', or 'ALL'
        const normalizedReq = requiredPermission.toUpperCase();

        return permissions.some(p => {
            if (p === 'ALL' || p === 'ALL DATABASES' || p === 'ALL TABLES') return true;

            // Exact match
            if (p === normalizedReq) return true;

            // Partial match for wildcards (simplistic)
            // e.g. "CREATE" covers "CREATE DATABASE"
            if (normalizedReq.startsWith(p)) return true;

            return false;
        });
    }, [permissions, isAdmin, requiredPermission]);

    if (hasPermission) {
        return <>{children}</>;
    }

    if (showTooltip) {
        return (
            <TooltipProvider>
                <Tooltip>
                    <TooltipTrigger asChild>
                        <div className="opacity-50 pointer-events-none grayscale">
                            {children}
                        </div>
                    </TooltipTrigger>
                    <TooltipContent>
                        <p>You do not have permission to perform this action.</p>
                        <p className="text-xs text-gray-400">Required: {requiredPermission}</p>
                    </TooltipContent>
                </Tooltip>
            </TooltipProvider>
        )
    }

    return <>{fallback}</>;
};

export default PermissionGuard;
