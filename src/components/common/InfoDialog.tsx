import React from "react";
import { Info, AlertTriangle, ExternalLink } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

export default function InfoDialog({
  title,
  children,
  variant = "info",
  link,
  steps,
  isOpen,
  onClose,
}: {
  title: string;
  children: React.ReactNode;
  variant: "info" | "warning";
  link?: string;
  steps?: string[];
  isOpen: boolean;
  onClose: () => void;
}) {
  const variantIcon = {
    info: <Info className="w-6 h-6 text-blue-500 dark:text-blue-400" />,
    warning: (
      <AlertTriangle className="w-6 h-6 text-yellow-500 dark:text-yellow-400" />
    ),
  };

  return (
    <Dialog open={isOpen} onOpenChange={onClose}>
      <DialogContent className="max-w-md">
        <DialogHeader className="flex flex-row items-center gap-2">
          {variantIcon[variant]}
          <DialogTitle>{title}</DialogTitle>
        </DialogHeader>
        <div className="py-4">
          <div className="text-sm">{children}</div>
          {steps && (
            <ol className="mt-4 ml-4 list-decimal text-sm text-gray-700 dark:text-gray-300">
              {steps.map((step, index) => (
                <li key={index} className="mt-2">
                  {step}
                </li>
              ))}
            </ol>
          )}
          {link && (
            <a
              href={link}
              target="_blank"
              rel="noopener noreferrer"
              className="flex items-center mt-4 text-sm text-blue-600 hover:underline dark:text-blue-400"
            >
              Learn more
              <ExternalLink className="w-4 h-4 ml-1" />
            </a>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
