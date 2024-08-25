import React from "react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Info } from "lucide-react";
import { Link } from "react-router-dom";

interface HowToDialogProps {
  isOpen: boolean;
  onClose: () => void;
  title: string;
  description: string;
  steps: string[]; // Array of steps or points to explain the process
  confirmText?: string; // Optional text for closing button
  docsUrl?: string; // Optional URL for documentation
}

const InfoDialog: React.FC<HowToDialogProps> = ({
  isOpen,
  onClose,
  title,
  description,
  steps,
  confirmText = "Got it!",
  docsUrl = "",
}) => {
  return (
    <Dialog open={isOpen} onOpenChange={onClose}>
      <DialogContent className="sm:max-w-[450px] border-gray-800">
        <DialogHeader className="space-y-3">
          <div className="flex items-center space-x-2">
            <Info className="h-6 w-6 text-blue-500" />
            <DialogTitle className="text-lg font-semibold text-blue-500">
              {title}
            </DialogTitle>
          </div>
          <DialogDescription className="text-gray-400">
            {description}
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-4 mt-4 text-sm">
          {steps.map((step, index) => (
            <div key={index} className="flex items-start space-x-2">
              <span className="text-blue-500 font-semibold">{index + 1}.</span>
              <p className="">{step}</p>
            </div>
          ))}
        </div>
        <div className="flex justify-end mt-6">
          <Button
            onClick={onClose}
            className="bg-blue-600 hover:bg-blue-700 text-white"
          >
            {confirmText}
          </Button>
          {docsUrl && (
            <Link to={docsUrl + "?utm_source=ch-ui&utm_medium=organization-how-to"} className="ml-2" target="_blank">
              <Button variant="outline">Documentation</Button>
            </Link>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
};

export default InfoDialog;
