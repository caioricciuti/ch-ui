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
import { AlertCircle, RefreshCcw, Copy, ClipboardCopy, FileQuestion } from "lucide-react";
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
  const isNoDataState = errorMessage.toLowerCase().includes("no data returned");
  
  const copyToClipboard = (text: string, message: string) => {
    navigator.clipboard.writeText(text).then(() => {
      toast.success(message);
    }).catch(() => {
      toast.error("Failed to copy to clipboard");
    });
  };

  const cardStyles = isNoDataState
    ? "bg-gray-50 dark:bg-gray-900/30 border-gray-200 dark:border-gray-800"
    : "bg-red-50 dark:bg-red-900/30 border-red-200 dark:border-red-800";

  const buttonStyles = isNoDataState
    ? "text-gray-600 border-gray-300 hover:bg-gray-100 dark:border-gray-700 dark:hover:bg-gray-800/50"
    : "text-red-600 border-red-300 hover:bg-red-100 dark:border-red-700 dark:hover:bg-red-800/50";

  return (
    <Card className={`w-full ${cardStyles}`}>
      <CardHeader>
        <CardTitle 
          className={`flex items-center space-x-2 text-xl ${
            isNoDataState 
              ? "text-gray-700 dark:text-gray-300" 
              : "text-red-700 dark:text-red-300"
          }`}
        >
          {isNoDataState ? (
            <>
              <FileQuestion className="w-5 h-5" />
              <span>No Data Available for {item.title}</span>
            </>
          ) : (
            <>
              <AlertCircle className="w-5 h-5" />
              <span>Error loading "{item.title}" Metric</span>
            </>
          )}
        </CardTitle>
      </CardHeader>
      <CardContent>
        <p className={`text-sm mb-4 ${
          isNoDataState 
            ? "text-gray-600 dark:text-gray-400" 
            : "text-red-600 dark:text-red-400"
        }`}>
          {isNoDataState 
            ? "This query returned no results. This might be expected if there's no matching data for the current filters."
            : errorMessage
          }
        </p>
        <div className="flex justify-between">
          <div className="space-x-2">
            {!isNoDataState && (
              <TooltipProvider>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <Button
                      variant="outline"
                      size="sm"
                      className={buttonStyles}
                      onClick={() => copyToClipboard(errorMessage, "Error message copied to clipboard")}
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
            )}
            <TooltipProvider>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    variant="outline"
                    size="sm"
                    className={buttonStyles}
                    onClick={() => copyToClipboard(item.query, "Query copied to clipboard")}
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
                    className={buttonStyles}
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