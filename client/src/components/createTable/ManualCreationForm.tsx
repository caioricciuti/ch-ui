// components/CreateTable/ManualCreationForm.tsx
import React from "react";
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
import { Textarea } from "@/components/ui/textarea";
import { AlertCircle, X } from "lucide-react";
import FieldManagement from "@/components/createTable/FieldManagement";
import { Alert, AlertDescription } from "../ui/alert";
import { Badge } from "@/components/ui/badge";

interface ManualCreationFormProps {
  database: string;
  tableName: string;
  engine: string;
  fields: any[];
  primaryKeyFields: string[];
  orderByFields: string[];
  partitionByField: string | null;
  comment: string;
  errors: Record<string, string>;
  onChange: (field: string, value: any) => void;
  onAddField: () => void;
  onRemoveField: (index: number) => void;
  onUpdateField: (index: number, key: string, value: any) => void;
  onValidateAndGenerateSQL: () => void;
  onCreateManual: () => void;
  sql: string;
  onCopySQL: () => void;
  createTableError: string;
  statementCopiedToClipBoard: boolean;
  fieldTypes: string[];
  databaseData: any[]; // Add this line
}

const ManualCreationForm: React.FC<ManualCreationFormProps> = ({
  database,
  tableName,
  engine,
  fields,
  primaryKeyFields,
  orderByFields,
  partitionByField,
  comment,
  errors,
  onChange,
  onAddField,
  onRemoveField,
  onUpdateField,
  onValidateAndGenerateSQL,
  onCreateManual,
  sql,
  onCopySQL,
  createTableError,
  databaseData, // Add this line
}) => {
  return (
    <div className="space-y-4">
      {/* Database Selector */}
      <div className="space-y-2">
        <Label htmlFor="database-select">Database</Label>
        <Select
          value={database}
          onValueChange={(value) => onChange("database", value)}
        >
          <SelectTrigger id="database-select">
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
        <Label htmlFor="table-name">Table Name</Label>
        <Input
          id="table-name"
          value={tableName}
          onChange={(e) => onChange("tableName", e.target.value)}
          placeholder="Enter table name"
          className={errors.tableName ? "border-red-500" : ""}
        />
        {errors.tableName && (
          <p className="text-xs text-red-500">{errors.tableName}</p>
        )}
      </div>

      {/* Table Engine Selector */}
      <div className="space-y-2">
        <Label htmlFor="engine-select">Database Engine</Label>
        <Select
          value={engine}
          onValueChange={(value) => onChange("engine", value)}
        >
          <SelectTrigger id="engine-select">
            <SelectValue placeholder="Select engine" />
          </SelectTrigger>
          <SelectContent>
            {[
              "MergeTree",
              "ReplacingMergeTree",
              "SummingMergeTree",
              "AggregatingMergeTree",
              "CollapsingMergeTree",
              "VersionedCollapsingMergeTree",
              "Memory",
            ].map((eng) => (
              <SelectItem key={eng} value={eng}>
                {eng}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {/* Field Management */}
      <FieldManagement
        fields={fields}
        onAddField={onAddField}
        onRemoveField={onRemoveField}
        onUpdateField={onUpdateField}
        errors={errors}
      />

      {/* Primary Key(s) Display */}
      <div className="flex flex-wrap gap-2 items-center">
        <span className="text-sm text-primary/70">Primary Key(s):</span>
        {primaryKeyFields.length > 0 ? (
          primaryKeyFields.map((field) => (
            <Badge key={field} variant="secondary" className="text-sm">
              {field}
              <Button
                variant="ghost"
                size="sm"
                className="ml-1 h-4 w-4 p-0"
                onClick={() =>
                  onUpdateField(
                    fields.findIndex((f) => f.name === field),
                    "isPrimaryKey",
                    false
                  )
                }
                aria-label={`Remove primary key from ${field}`}
              >
                <X className="h-3 w-3" />
              </Button>
            </Badge>
          ))
        ) : (
          <span className="text-xs text-gray-500">
            No column selected as primary key
          </span>
        )}
      </div>

      {/* Order By Fields Display */}
      <div className="flex flex-wrap gap-2 items-center">
        <span className="text-sm text-primary/70">Order By Field(s):</span>
        {orderByFields.length > 0 ? (
          orderByFields.map((field) => (
            <Badge key={field} variant="secondary" className="text-sm">
              {field}
              <Button
                variant="ghost"
                size="sm"
                className="ml-1 h-4 w-4 p-0"
                onClick={() =>
                  onUpdateField(
                    fields.findIndex((f) => f.name === field),
                    "isOrderBy",
                    false
                  )
                }
                aria-label={`Remove order by from ${field}`}
              >
                <X className="h-3 w-3" />
              </Button>
            </Badge>
          ))
        ) : (
          <span className="text-xs text-gray-500">
            No column selected as order by
          </span>
        )}
      </div>

      {/* Partition By Field Display */}
      <div className="flex flex-wrap gap-2 items-center">
        <span className="text-sm text-primary/70">Partition By Field:</span>
        {partitionByField ? (
          <Badge variant="secondary" className="text-sm">
            {partitionByField}
            <Button
              variant="ghost"
              size="sm"
              className="ml-1 h-4 w-4 p-0"
              onClick={() =>
                onUpdateField(
                  fields.findIndex((f) => f.name === partitionByField),
                  "isPartitionBy",
                  false
                )
              }
              aria-label={`Remove partition by from ${partitionByField}`}
            >
              <X className="h-3 w-3" />
            </Button>
          </Badge>
        ) : (
          <span className="text-xs text-gray-500">
            No column selected as partition by
          </span>
        )}
      </div>

      {/* Table Comment */}
      <div className="space-y-2">
        <Label htmlFor="table-comment">Table Comment</Label>
        <Textarea
          id="table-comment"
          placeholder="Enter table comment"
          value={comment}
          onChange={(e) => onChange("comment", e.target.value)}
        />
      </div>

      {/* Action Buttons */}
      <div className="flex gap-4">
        <Button
          onClick={onValidateAndGenerateSQL}
          variant="outline"
          disabled={!database || !tableName || fields.length === 0}
        >
          Show Statement
        </Button>

        <Button
          onClick={onCreateManual}
          disabled={false /* Handle loading state in parent */}
        >
          Create Table
        </Button>
      </div>

      {/* Generated SQL */}
      {sql && (
        <div className="mt-4">
          <div className="flex items-center">
            <h3 className="text-lg font-semibold">Generated SQL:</h3>
            <Button
              size="icon"
              variant="link"
              onClick={onCopySQL}
              className="ml-2"
              aria-label="Copy SQL Statement"
            >
              {/* Replace with your CopyIcon and CopyCheck logic */}
            </Button>
          </div>
          <pre className="bg-primary/20 p-2 rounded mt-2 text-sm overflow-x-auto">
            {sql}
          </pre>
        </div>
      )}

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

export default ManualCreationForm;
