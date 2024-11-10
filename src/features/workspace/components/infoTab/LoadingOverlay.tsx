// LoadingOverlay.tsx
import React from "react";
import { Loader2 } from "lucide-react";

interface LoadingOverlayProps {
  message?: string;
  className?: string;
}

const LoadingOverlay: React.FC<LoadingOverlayProps> = ({
  message = "Loading data...",
  className = "",
}) => (
  <div
    className={`absolute inset-0 bg-background/80 backdrop-blur-sm 
    flex flex-col items-center justify-center z-50 ${className}`}
  >
    <div className="flex flex-col items-center space-y-4">
      <Loader2 className="w-8 h-8 animate-spin text-primary" />
      <p className="text-sm text-muted-foreground">{message}</p>
    </div>
  </div>
);

export default LoadingOverlay;
