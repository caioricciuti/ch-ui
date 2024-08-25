import React, { useState, useEffect } from "react";
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
import { Download } from "lucide-react";
import Papa from "papaparse";
import { toast } from "sonner";

interface DownloadDialogProps {
  data: any[];
}

const DownloadDialog: React.FC<DownloadDialogProps> = ({ data }) => {
  const [downloadOption, setDownloadOption] = useState<string>("csv");
  const [estimatedSize, setEstimatedSize] = useState<string>("");
  const [open, setOpen] = useState(false);

  useEffect(() => {
    estimateSize();
  }, [downloadOption, data]);

  const estimateSize = () => {
    let size: number;
    switch (downloadOption) {
      case "csv":
        size = new Blob([Papa.unparse(data)]).size;
        break;
      case "json":
        size = new Blob([JSON.stringify(data)]).size;
        break;
      case "parquet":
        // Rough estimation, as actual Parquet size would be calculated server-side
        size = new Blob([JSON.stringify(data)]).size * 0.7;
        break;
      case "clipboard":
        size = new Blob([JSON.stringify(data)]).size;
        break;
      default:
        size = 0;
    }
    setEstimatedSize(formatBytes(size));
  };

  const formatBytes = (bytes: number, decimals = 2) => {
    if (bytes === 0) return "0 Bytes";
    const k = 1024;
    const dm = decimals < 0 ? 0 : decimals;
    const sizes = ["Bytes", "KB", "MB", "GB", "TB", "PB", "EB", "ZB", "YB"];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(dm)) + " " + sizes[i];
  };

  const handleDownload = () => {
    switch (downloadOption) {
      case "csv":
        downloadCSV();
        break;
      case "json":
        downloadJSON();
        break;
      case "parquet":
        alert(
          "Parquet download is not implemented in this client-side example."
        );
        break;
      case "clipboard":
        copyToClipboard();
        break;
    }
    setOpen(false);
  };

  const downloadCSV = () => {
    const csv = Papa.unparse(data);
    const blob = new Blob([csv], { type: "text/csv;charset=utf-8;" });
    downloadBlob(blob, "data.csv");
    toast.success("CSV downloaded successfully");
  };

  const downloadJSON = () => {
    const jsonString = JSON.stringify(data, null, 2);
    const blob = new Blob([jsonString], { type: "application/json" });
    downloadBlob(blob, "data.json");
    toast.success("JSON downloaded successfully");
  };

  const copyToClipboard = () => {
    const jsonString = JSON.stringify(data, null, 2);
    navigator.clipboard
      .writeText(jsonString)
      .then(() => {
        toast.success("Data copied to clipboard");
      })
      .catch((err) => {
        console.error("Failed to copy: ", err);
        toast.error("Failed to copy data to clipboard");
      });
  };

  const downloadBlob = (blob: Blob, fileName: string) => {
    const link = document.createElement("a");
    if (link.download !== undefined) {
      const url = URL.createObjectURL(blob);
      link.setAttribute("href", url);
      link.setAttribute("download", fileName);
      link.style.visibility = "hidden";
      document.body.appendChild(link);
      link.click();
      document.body.removeChild(link);
    }
  };

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button className="h-6 w-6 p-1 bg-transparent text-primary hover:bg-secondary">
          <Download className="h-4 w-4" />
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-[425px]">
        <DialogHeader>
          <DialogTitle>Download Options</DialogTitle>
        </DialogHeader>
        <div className="grid gap-4 py-4">
          <RadioGroup value={downloadOption} onValueChange={setDownloadOption}>
            <div className="flex items-center space-x-2">
              <RadioGroupItem value="csv" id="csv" />
              <Label htmlFor="csv">Download as CSV</Label>
            </div>
            <div className="flex items-center space-x-2">
              <RadioGroupItem value="json" id="json" />
              <Label htmlFor="json">Download as JSON</Label>
            </div>
            <div className="flex items-center space-x-2">
              <RadioGroupItem value="parquet" id="parquet" />
              <Label htmlFor="parquet">Download as Parquet</Label>
            </div>
            <div className="flex items-center space-x-2">
              <RadioGroupItem value="clipboard" id="clipboard" />
              <Label htmlFor="clipboard">Copy to Clipboard</Label>
            </div>
          </RadioGroup>
          <div className="text-sm text-gray-500">
            Estimated size: {estimatedSize}
          </div>
        </div>
        <Button onClick={handleDownload}>Download</Button>
      </DialogContent>
    </Dialog>
  );
};

export default DownloadDialog;
