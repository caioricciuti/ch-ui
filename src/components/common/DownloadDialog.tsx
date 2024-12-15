import React, { useState, useEffect, useCallback } from "react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Label } from "@/components/ui/label";
import { Download, AlertCircle } from "lucide-react";
import Papa from "papaparse";
import { toast } from "sonner";
import { Progress } from "@/components/ui/progress";
import { Alert, AlertDescription } from "@/components/ui/alert";

interface DownloadDialogProps {
  data: any[];
  onExport?: (format: string) => Promise<Blob>;
  filename?: string;
  maxRows?: number;
}

type ExportFormat = "csv" | "json" | "clipboard";

const CHUNK_SIZE = 10000; // Number of rows to process at once

const DownloadDialog: React.FC<DownloadDialogProps> = ({
  data,
  onExport,
  filename = "export",
  maxRows = 1000000,
}) => {
  const [downloadOption, setDownloadOption] = useState<ExportFormat>("csv");
  const [estimatedSize, setEstimatedSize] = useState<string>("");
  const [isProcessing, setIsProcessing] = useState(false);
  const [progress, setProgress] = useState(0);
  const [open, setOpen] = useState(false);

  const estimateSize = useCallback(async () => {
    if (data.length === 0) {
      setEstimatedSize("0 B");
      return;
    }

    const sampleSize = Math.min(100, data.length);
    const sample = data.slice(0, sampleSize);
    let size: number;

    switch (downloadOption) {
      case "csv":
        size =
          new Blob([Papa.unparse(sample)]).size * (data.length / sampleSize);
        break;
      case "json":
        size =
          new Blob([JSON.stringify(sample)]).size * (data.length / sampleSize);
        break;
      case "clipboard":
        size =
          new Blob([JSON.stringify(sample)]).size * (data.length / sampleSize);
        break;
      default:
        size = 0;
    }

    setEstimatedSize(formatBytes(size));
  }, [data, downloadOption]);

  useEffect(() => {
    estimateSize();
  }, [estimateSize]);

  const formatBytes = (bytes: number, decimals = 2) => {
    if (!bytes || bytes === 0) return "0 B";
    const k = 1024;
    const dm = decimals < 0 ? 0 : decimals;
    const sizes = ["B", "KB", "MB", "GB", "TB"];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return `${parseFloat((bytes / Math.pow(k, i)).toFixed(dm))} ${sizes[i]}`;
  };

  const processInChunks = async (
    data: any[],
    format: ExportFormat,
    chunkSize: number
  ): Promise<Blob> => {
    const chunks = Math.ceil(data.length / chunkSize);
    let result = "";

    for (let i = 0; i < chunks; i++) {
      const chunk = data.slice(i * chunkSize, (i + 1) * chunkSize);

      switch (format) {
        case "csv":
          result +=
            i === 0
              ? Papa.unparse(chunk)
              : Papa.unparse(chunk, { header: false });
          break;
        case "json":
        case "clipboard":
          result +=
            (i === 0 ? "[" : "") +
            chunk.map((item) => JSON.stringify(item)).join(",") +
            (i === chunks - 1 ? "]" : "");
          break;
        default:
          throw new Error(`Unsupported format: ${format}`);
      }

      setProgress(((i + 1) / chunks) * 100);
      // Allow UI to update
      await new Promise((resolve) => setTimeout(resolve, 0));
    }

    return new Blob([result], { type: getContentType(format) });
  };

  const getContentType = (format: ExportFormat): string => {
    switch (format) {
      case "csv":
        return "text/csv";
      case "json":
        return "application/json";
      default:
        return "text/plain";
    }
  };

  type AlertVariant = "destructive" | "warning" | "info" | "default" | "success" | "neutral";

  type SizeWarning = {
    message: string;
    severity: AlertVariant;
  } | null;

  const getSizeWarning = useCallback((): SizeWarning => {
    if (!estimatedSize) return null;

    const [size, unit] = estimatedSize.split(" ");
    const sizeNum = parseFloat(size);

    // Convert everything to MB for comparison
    let sizeInMB = sizeNum;
    switch (unit) {
      case "GB":
        sizeInMB = sizeNum * 1024;
        break;
      case "KB":
        sizeInMB = sizeNum / 1024;
        break;
      case "B":
        sizeInMB = sizeNum / (1024 * 1024);
        break;
      case "MB":
        sizeInMB = sizeNum;
        break;
    }

    if (sizeInMB >= 100) {
      return {
        message:
          "Warning: The export size is over 100MB. This might take a while and could impact browser performance.",
        severity: "destructive",
      };
    } else if (sizeInMB >= 50) {
      return {
        message:
          "Warning: The export size is over 50MB. This might take a while.",
        severity: "warning",
      };
    } else if (sizeInMB >= 20) {
      return {
        message: "The export size is over 20MB.",
        severity: "default",
      };
    }
    return null;
  }, [estimatedSize]);

  const handleDownload = async () => {
    try {
      setIsProcessing(true);
      setProgress(0);

      if (data.length > maxRows) {
        toast.error(`Cannot export more than ${maxRows.toLocaleString()} rows`);
        return;
      }

      let blob: Blob;

      if (onExport) {
        blob = await onExport(downloadOption);
      } else {
        blob = await processInChunks(data, downloadOption, CHUNK_SIZE);
      }

      const now = new Date().toISOString().split(".")[0].replace(/[:]/g, "-");
      const exportFilename = `ch_ui_export_${now}`;

      if (downloadOption === "clipboard") {
        const text = await blob.text();
        await navigator.clipboard.writeText(text);
        toast.success("Copied to clipboard!", { duration: 2000 });
      } else {
        const url = URL.createObjectURL(blob);
        const a = document.createElement("a");
        a.href = url;
        a.download = `${exportFilename}.${downloadOption}`;
        document.body.appendChild(a);
        a.click();
        URL.revokeObjectURL(url);
        document.body.removeChild(a);
        toast.success("Download started!", { duration: 2000 });
      }

      setOpen(false);
    } catch (error) {
      console.error("Export error:", error);
      toast.error("Failed to export data. Please try again.", {
        duration: 2000,
      });
    } finally {
      setIsProcessing(false);
      setProgress(0);
    }
  };

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button variant="outline" size="sm">
          <Download className="h-4 w-4" />
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Export Data</DialogTitle>
        </DialogHeader>

        <div className="space-y-4">
          {getSizeWarning() && (
            <Alert variant={getSizeWarning()?.severity || "neutral"}>
              <AlertDescription>{getSizeWarning()?.message}</AlertDescription>
            </Alert>
          )}

          <RadioGroup
            value={downloadOption}
            onValueChange={(value) => setDownloadOption(value as ExportFormat)}
          >
            <div className="flex items-center space-x-2">
              <RadioGroupItem value="csv" id="csv" />
              <Label htmlFor="csv">CSV</Label>
            </div>
            <div className="flex items-center space-x-2">
              <RadioGroupItem value="json" id="json" />
              <Label htmlFor="json">JSON</Label>
            </div>
            <div className="flex items-center space-x-2">
              <RadioGroupItem value="clipboard" id="clipboard" />
              <Label htmlFor="clipboard">Copy to Clipboard</Label>
            </div>
          </RadioGroup>

          <div className="text-sm text-gray-500">
            Estimated size: {estimatedSize}
            {data.length > maxRows && (
              <div className="flex items-center mt-2 text-amber-500">
                <AlertCircle className="h-4 w-4 mr-2" />
                Warning: Large dataset ({data.length.toLocaleString()} rows)
              </div>
            )}
          </div>

          {isProcessing && (
            <div className="space-y-2">
              <Progress value={progress} />
              <p className="text-sm text-gray-500 text-center">
                Processing... {Math.round(progress)}%
              </p>
            </div>
          )}

          <div className="flex justify-end">
            <Button
              variant="outline"
              onClick={handleDownload}
              disabled={isProcessing}
            >
              {isProcessing ? "Processing..." : "Export"}
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
};

export default DownloadDialog;
