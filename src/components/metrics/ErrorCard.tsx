import React from "react";
import {
  Card,
  CardHeader,
  CardTitle,
  CardContent,
} from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { AlertCircle, RefreshCcw, Copy, ClipboardCopy } from "lucide-react";
import { toast } from "sonner";

interface ErrorCardProps {
  item: { title: string; query: string };
  errorMessage: string;
  fetchData: () => void;
}

const ErrorCard: React.FC<ErrorCardProps> = ({
  item,
  errorMessage,
  fetchData,
}) => {
  const copyToClipboard = (text: string, message: string | undefined) => {
    navigator.clipboard.writeText(text);
    toast.info(message);
  };

  return (
    <Card className="w-full bg-red-50 dark:bg-red-900/30 border-red-200 dark:border-red-800">
      <CardHeader>
        <CardTitle className="text-red-700 dark:text-red-300 flex items-center space-x-2 text-xl">
          <AlertCircle className="w-5 h-5" />
          <span>Error loading "{item.title}" Metric</span>
        </CardTitle>
      </CardHeader>
      <CardContent>
        <p className="text-red-600 dark:text-red-400 text-sm mb-4 truncate">
          {errorMessage}
        </p>
        <div className="flex justify-between">
          <div className=" space-x-2">
            <TooltipProvider>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    variant="outline"
                    size="sm"
                    className="text-red-600 border-red-300 hover:bg-red-100 dark:border-red-700 dark:hover:bg-red-800/50"
                    onClick={() =>
                      copyToClipboard(
                        errorMessage,
                        "Error message copied to clipboard"
                      )
                    }
                  >
                    <Copy className="w-4 h-4 mr-2" />
                    Copy Error
                  </Button>
                </TooltipTrigger>
                <TooltipContent>
                  <p>Copy error message</p>
                </TooltipContent>
              </Tooltip>
            </TooltipProvider>
            <TooltipProvider>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    variant="outline"
                    size="sm"
                    className="text-red-600 border-red-300 hover:bg-red-100 dark:border-red-700 dark:hover:bg-red-800/50"
                    onClick={() =>
                      copyToClipboard(item.query, "Query copied to clipboard")
                    }
                  >
                    <ClipboardCopy className="w-4 h-4 mr-2" />
                    Copy Query
                  </Button>
                </TooltipTrigger>
                <TooltipContent>
                  <p>Copy associated query</p>
                </TooltipContent>
              </Tooltip>
            </TooltipProvider>
          </div>
          <div>
            <TooltipProvider>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    variant="ghost"
                    size="sm"
                    className="text-red-600 hover:bg-red-100 dark:hover:bg-red-800/50"
                    onClick={fetchData}
                  >
                    <RefreshCcw className="w-4 h-4 mr-2" />
                    Retry
                  </Button>
                </TooltipTrigger>
                <TooltipContent>
                  <p>Attempt to fetch data again</p>
                </TooltipContent>
              </Tooltip>
            </TooltipProvider>
          </div>
        </div>
      </CardContent>
    </Card>
  );
};

export default ErrorCard;
