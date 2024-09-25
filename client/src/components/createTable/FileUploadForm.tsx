// components/CreateTable/FileUploadForm.tsx
import React, { useState } from "react";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Accordion,
  AccordionItem,
  AccordionTrigger,
  AccordionContent,
} from "@/components/ui/accordion";
import { Checkbox } from "@/components/ui/checkbox";
import { Upload, FileText, X } from "lucide-react";
import { Textarea } from "@/components/ui/textarea";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { toast } from "sonner";

import CHUITable from "@/components/CHUITable"; // Import your CHUITable component

interface FileUploadFormProps {
  database: string;
  tableName: string;
  fileType: "csv" | "json";
  file: File | null;
  uploadedFileName: string;
  csvDelimiter: string;
  csvQuoteChar: string;
  csvEscapeChar: string;
  csvHeaderRowsToSkip: number;
  flattenJSON: boolean;
  jsonNestedPaths: string[];
  errors: Record<string, string>;
  previewData: any[];
  fields: any[]; // Pass fields to reconstruct meta
  onChange: (field: string, value: any) => void;
  onFileChange: (file: File | null) => void;
  onFileTypeChange: (type: "csv" | "json") => void;
  onCsvDelimiterChange: (value: string) => void;
  onCsvQuoteCharChange: (value: string) => void;
  onCsvEscapeCharChange: (value: string) => void;
  onCsvHeaderRowsToSkipChange: (value: number) => void;
  onFlattenJSONChange: (value: boolean) => void;
  onJsonNestedPathsChange: (paths: string[]) => void;
  onRemoveFile: () => void;
  onCreateFromFile: () => void;
  createTableError: string;
  isProcessing: boolean;
  databaseData: any[]; // Add this prop
}

const FileUploadForm: React.FC<FileUploadFormProps> = ({
  database,
  tableName,
  fileType,
  file,
  uploadedFileName,
  csvDelimiter,
  csvQuoteChar,
  csvEscapeChar,
  csvHeaderRowsToSkip,
  flattenJSON,
  jsonNestedPaths,
  errors,
  previewData,
  fields,
  onChange,
  onFileChange,
  onFileTypeChange,
  onCsvDelimiterChange,
  onCsvQuoteCharChange,
  onCsvEscapeCharChange,
  onCsvHeaderRowsToSkipChange,
  onFlattenJSONChange,
  onJsonNestedPathsChange,
  onRemoveFile,
  onCreateFromFile,
  createTableError,
  isProcessing,
  databaseData,
}) => {
  const [showPreview, setShowPreview] = useState(false);

  // Function to construct meta for CHUITable from fields
  const constructMeta = () => {
    return fields.map((field, index) => ({
      name: field.name || `Column ${index + 1}`,
      type: field.type || "String",
    }));
  };

  // Prepare the result object for CHUITable
  const chuITableResult = {
    meta: constructMeta(),
    data: previewData.map((row) => {
      const rowObj: any = {};
      fields.forEach((field, index) => {
        rowObj[field.name || `Column ${index + 1}`] = row[index];
      });
      return rowObj;
    }),
    statistics: {
      elapsed: 0,
      rows_read: previewData.length,
      bytes_read: 0,
    },
    message: undefined,
    query_id: undefined,
  };

  return (
    <div className="space-y-4">
      {/* Database Selector */}
      <div className="space-y-2">
        <Label htmlFor="upload-database-select">Database</Label>
        <Select
          value={database}
          onValueChange={(value) => onChange("database", value)}
        >
          <SelectTrigger id="upload-database-select">
            <SelectValue placeholder="Select database" />
          </SelectTrigger>
          <SelectContent>
            {databaseData
              .filter((item: any) => item.type === "database")
              .map((db: any) => (
                <SelectItem key={db.name} value={db.name}>
                  {db.name}
                </SelectItem>
              ))}
          </SelectContent>
        </Select>
        {errors.database && (
          <p className="text-xs text-red-500">{errors.database}</p>
        )}
      </div>

      {/* Table Name Input */}
      <div className="space-y-2">
        <Label htmlFor="upload-table-name">Table Name</Label>
        <Input
          id="upload-table-name"
          value={tableName}
          onChange={(e) => onChange("tableName", e.target.value)}
          placeholder="Enter table name"
          className={errors.tableName ? "border-red-500" : ""}
        />
        {errors.tableName && (
          <p className="text-xs text-red-500">{errors.tableName}</p>
        )}
      </div>

      {/* File Type Selector */}
      <div className="space-y-2">
        <Label htmlFor="file-type-select">File Type</Label>
        <Select value={fileType} onValueChange={onFileTypeChange}>
          <SelectTrigger id="file-type-select">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="csv">CSV</SelectItem>
            <SelectItem value="json">JSON</SelectItem>
          </SelectContent>
        </Select>
      </div>

      {/* Advanced Options Accordion */}
      <Accordion type="single" collapsible className="w-full">
        <AccordionItem value="advanced-options">
          <AccordionTrigger>Advanced Options</AccordionTrigger>
          <AccordionContent>
            {/* Advanced CSV Options */}
            {fileType === "csv" && (
              <div className="space-y-4">
                <div className="space-y-2">
                  <Label htmlFor="csv-delimiter">Field Delimiter</Label>
                  <Input
                    id="csv-delimiter"
                    value={csvDelimiter}
                    onChange={(e) => onCsvDelimiterChange(e.target.value)}
                    placeholder="e.g., , ; |"
                    maxLength={1}
                  />
                  {errors.csvDelimiter && (
                    <p className="text-xs text-red-500">
                      {errors.csvDelimiter}
                    </p>
                  )}
                </div>

                <div className="space-y-2">
                  <Label htmlFor="csv-quote-char">Quote Character</Label>
                  <Input
                    id="csv-quote-char"
                    value={csvQuoteChar}
                    onChange={(e) => onCsvQuoteCharChange(e.target.value)}
                    placeholder='"'
                    maxLength={1}
                  />
                  {errors.csvQuoteChar && (
                    <p className="text-xs text-red-500">
                      {errors.csvQuoteChar}
                    </p>
                  )}
                </div>

                <div className="space-y-2">
                  <Label htmlFor="csv-escape-char">Escape Character</Label>
                  <Input
                    id="csv-escape-char"
                    value={csvEscapeChar}
                    onChange={(e) => onCsvEscapeCharChange(e.target.value)}
                    placeholder="\"
                    maxLength={1}
                  />
                  {errors.csvEscapeChar && (
                    <p className="text-xs text-red-500">
                      {errors.csvEscapeChar}
                    </p>
                  )}
                </div>

                <div className="space-y-2">
                  <Label htmlFor="csv-header-rows">Header Rows to Skip</Label>
                  <Input
                    id="csv-header-rows"
                    type="number"
                    min="0"
                    value={csvHeaderRowsToSkip}
                    onChange={(e) =>
                      onCsvHeaderRowsToSkipChange(Number(e.target.value))
                    }
                    placeholder="0"
                  />
                  {errors.csvHeaderRowsToSkip && (
                    <p className="text-xs text-red-500">
                      {errors.csvHeaderRowsToSkip}
                    </p>
                  )}
                </div>
              </div>
            )}

            {/* Advanced JSON Options */}
            {fileType === "json" && (
              <div className="space-y-4">
                <div className="flex items-center space-x-2">
                  <Checkbox
                    id="flatten-json"
                    checked={flattenJSON}
                    onCheckedChange={(checked) => onFlattenJSONChange(checked)}
                    aria-label="Flatten JSON"
                  />
                  <Label
                    htmlFor="flatten-json"
                    className="whitespace-nowrap text-xs text-primary/70"
                  >
                    Flatten JSON
                  </Label>
                </div>
                {!flattenJSON && (
                  <div className="space-y-2">
                    <Label htmlFor="json-nested-paths">Nested JSON Paths</Label>
                    <Textarea
                      id="json-nested-paths"
                      placeholder="Enter JSON paths separated by commas (e.g., address.street, user.age)"
                      value={jsonNestedPaths.join(", ")}
                      onChange={(e) =>
                        onJsonNestedPathsChange(
                          e.target.value
                            .split(",")
                            .map((path) => path.trim())
                            .filter((path) => path.length > 0)
                        )
                      }
                    />
                    {errors.jsonNestedPaths && (
                      <p className="text-xs text-red-500">
                        {errors.jsonNestedPaths}
                      </p>
                    )}
                  </div>
                )}
              </div>
            )}
          </AccordionContent>
        </AccordionItem>
      </Accordion>

      {/* File Upload */}
      <div className="space-y-2">
        <Label htmlFor="file-upload">Upload File</Label>
        <div className="flex items-center justify-center w-full">
          <label
            htmlFor="file-upload"
            className="flex flex-col items-center justify-center w-full h-64 border-2 border-gray-300 border-dashed rounded-lg cursor-pointer bg-gray-50 dark:hover:bg-bray-800 dark:bg-gray-700 hover:bg-gray-100 dark:border-gray-600 dark:hover:border-gray-500 dark:hover:bg-gray-600"
          >
            <div className="flex flex-col items-center justify-center pt-5 pb-6">
              <Upload className="w-8 h-8 mb-4 text-gray-500 dark:text-gray-400" />
              <p className="mb-2 text-sm text-gray-500 dark:text-gray-400">
                <span className="font-semibold">Click to upload</span> or drag
                and drop
              </p>
              <p className="text-xs text-gray-500 dark:text-gray-400">
                {fileType.toUpperCase()} (MAX. 100MB)
              </p>
            </div>
            <input
              id="file-upload"
              type="file"
              accept={fileType === "csv" ? ".csv" : ".json"}
              className="hidden"
              onChange={(e) => onFileChange(e.target.files?.[0] || null)}
            />
          </label>
        </div>
        {uploadedFileName && (
          <div className="flex items-center mt-2">
            <FileText className="w-4 h-4 mr-2" />
            <span className="text-sm">{uploadedFileName}</span>
            <Button
              variant="ghost"
              size="sm"
              className="ml-2"
              onClick={onRemoveFile}
              aria-label="Remove uploaded file"
            >
              <X className="w-4 h-4" />
            </Button>
          </div>
        )}
        {errors.file && <p className="text-xs text-red-500">{errors.file}</p>}
      </div>

      {/* Preview Data Button */}
      {previewData.length > 0 && (
        <div className="flex justify-end">
          <Button
            onClick={() => setShowPreview(!showPreview)}
            variant="outline"
            size="sm"
          >
            {showPreview ? "Hide Preview" : "Preview Data"}
          </Button>
        </div>
      )}

      {/* CHUITable for Data Preview */}
      {showPreview && (
        <div className="mt-4">
          <CHUITable result={chuITableResult} />
        </div>
      )}

      {/* Action Button */}
      <Button
        onClick={onCreateFromFile}
        disabled={
          isProcessing ||
          !file ||
          !database ||
          !tableName ||
          (fileType === "csv" &&
            (csvDelimiter.length !== 1 ||
              csvQuoteChar.length !== 1 ||
              csvEscapeChar.length !== 1 ||
              csvHeaderRowsToSkip < 0)) ||
          (fileType === "json" && !flattenJSON && jsonNestedPaths.length === 0)
        }
        className="w-full flex items-center justify-center"
      >
        {isProcessing ? "Processing..." : "Create Table from File"}
      </Button>

      {/* Error Message */}
      {createTableError && (
        <Alert variant="destructive" className="mt-4">
          <AlertCircle className="h-4 w-4" />
          <AlertDescription>{createTableError}</AlertDescription>
        </Alert>
      )}
    </div>
  );
};

export default FileUploadForm;
