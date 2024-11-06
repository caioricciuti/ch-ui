import React from "react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { AlertCircle, AlertTriangle, CheckCircle, Info } from "lucide-react";

type Variant = "danger" | "warning" | "info" | "success";

interface ConfirmationDialogProps {
  isOpen: boolean;
  onClose: () => void;
  onConfirm: () => void;
  title: string;
  description: string;
  confirmText?: string;
  cancelText?: string;
  variant?: Variant;
  onConfirmAction?: () => void;
}

const variantStyles: Record<Variant, { icon: React.ReactNode; color: string }> =
  {
    danger: {
      icon: <AlertCircle className="h-5 w-5" />,
      color: "text-red-500",
    },
    warning: {
      icon: <AlertTriangle className="h-5 w-5" />,
      color: "text-yellow-500",
    },
    info: {
      icon: <Info className="h-5 w-5" />,
      color: "text-blue-500",
    },
    success: {
      icon: <CheckCircle className="h-5 w-5" />,
      color: "text-green-500",
    },
  };

const ConfirmationDialog: React.FC<ConfirmationDialogProps> = ({
  isOpen,
  onClose,
  onConfirm,
  title,
  description,
  confirmText = "Confirm",
  cancelText = "Cancel",
  variant = "danger",
}) => {
  const { icon, color } = variantStyles[variant];

  return (
    <Dialog open={isOpen} onOpenChange={onClose}>
      <DialogContent className="sm:max-w-[425px] text-white border-gray-800">
        <DialogHeader className="space-y-3">
          <div className="flex items-center justify-between">
            <DialogTitle
              className={`text-lg font-semibold ${color} flex items-center space-x-2`}
            >
              {icon}
              <span>{title}</span>
            </DialogTitle>
          </div>
          <DialogDescription className="text-gray-400">
            <div dangerouslySetInnerHTML={{ __html: description }} />
          </DialogDescription>
        </DialogHeader>
        <DialogFooter className="flex justify-end space-x-2 mt-6">
          <Button
            variant="outline"
            onClick={onClose}
            className="bg-gray-800 text-white border-gray-700 hover:bg-gray-700"
          >
            {cancelText}
          </Button>
          <Button
            onClick={onConfirm}
            className={`
              ${variant === "danger" ? "bg-red-600 hover:bg-red-700" : ""}
              ${
                variant === "warning" ? "bg-yellow-600 hover:bg-yellow-700" : ""
              }
              ${variant === "info" ? "bg-blue-600 hover:bg-blue-700" : ""}
              ${variant === "success" ? "bg-green-600 hover:bg-green-700" : ""}
              text-white
            `}
          >
            {confirmText}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};

export default ConfirmationDialog;
