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
import { Copy, CopyCheck, X } from "lucide-react";
import FieldManagement, { Field } from "@/components/explorer/FieldManagement";
import { Alert, AlertDescription } from "@/components/ui/alert";

interface ManualCreationFormProps {
  database: string;
  tableName: string;
  engine: string;
  fields: Array<{
    name: string;
    type: string;
    isPrimaryKey: boolean;
    isOrderBy: boolean;
    isPartitionBy: boolean;
  }>;
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
  databaseData: Array<{ name: string; type: string }>;
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
  statementCopiedToClipBoard,
  fieldTypes,
  databaseData,
}) => {
  return (
    <div className="flex w-full mt-6">
      <div className="space-y-6 w-full">
        {/* Database and Table Name */}
        <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
          {/* Database Selector */}
          <div className="flex flex-col">
            <Label htmlFor="database-select" className="mb-3">
              Database
            </Label>
            <Select
              value={database}
              onValueChange={(value) => onChange("database", value)}
            >
              <SelectTrigger id="database-select">
                <SelectValue placeholder="Select database" />
              </SelectTrigger>
              <SelectContent>
                {databaseData
                  .filter((item) => item.type === "database")
                  .map((db) => (
                    <SelectItem key={db.name} value={db.name}>
                      {db.name}
                    </SelectItem>
                  ))}
              </SelectContent>
            </Select>
            {errors.database && (
              <p className="mt-1 text-sm text-red-500">{errors.database}</p>
            )}
          </div>

          {/* Table Name Input */}
          <div className="flex flex-col">
            <Label htmlFor="table-name" className="mb-3">
              Table Name
            </Label>
            <Input
              id="table-name"
              value={tableName}
              onChange={(e) => onChange("tableName", e.target.value)}
              placeholder="Enter table name"
              className={`${errors.tableName ? "border-red-500" : ""} h-10`}
            />
            {errors.tableName && (
              <p className="mt-1 text-sm text-red-500">{errors.tableName}</p>
            )}
          </div>
        </div>

        <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
          {/* Table Comment */}
          <div className="flex flex-col">
            <Label htmlFor="table-comment" className="mb-3">
              Table Comment
            </Label>
            <Input
              id="table-comment"
              placeholder="Enter table comment"
              value={comment}
              onChange={(e) => onChange("comment", e.target.value)}
            />
          </div>

          {/* Engine Selector */}
          <div className="flex flex-col">
            <Label htmlFor="engine-select" className="mb-3">
              Database Engine
            </Label>
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
            {errors.engine && (
              <p className="mt-1 text-sm text-red-500">{errors.engine}</p>
            )}
          </div>
        </div>

        {/* Field Management */}
        <FieldManagement
          fields={fields as Field[]}
          onAddField={onAddField}
          onRemoveField={onRemoveField}
          onUpdateField={onUpdateField}
          errors={errors}
          fieldTypes={fieldTypes}
        />

        {/* Primary Key(s) Display */}
        <div className="flex flex-wrap gap-2 items-center">
          <span className="text-sm font-medium text-primary">
            Primary Key(s):
          </span>
          {primaryKeyFields.length > 0 ? (
            primaryKeyFields.map((field) => (
              <span
                key={field}
                className="inline-flex items-center px-2 py-0.5 rounded bg-secondary text-xs font-medium text-white"
              >
                {field}
                <button
                  type="button"
                  className="ml-1 text-white hover:text-red-300 focus:outline-none"
                  onClick={() => {
                    const fieldIndex = fields.findIndex(
                      (f) => f.name === field
                    );
                    if (fieldIndex !== -1) {
                      onUpdateField(fieldIndex, "isPrimaryKey", false);
                    }
                  }}
                  aria-label={`Remove primary key from ${field}`}
                >
                  <X className="h-3 w-3" />
                </button>
              </span>
            ))
          ) : (
            <span className="text-xs text-gray-500">
              No column selected as primary key
            </span>
          )}
        </div>

        {/* Order By Fields Display */}
        <div className="flex flex-wrap gap-2 items-center">
          <span className="text-sm font-medium text-primary">
            Order By Field(s):
          </span>
          {orderByFields.length > 0 ? (
            orderByFields.map((field) => (
              <span
                key={field}
                className="inline-flex items-center px-2 py-0.5 rounded bg-secondary text-xs font-medium text-white"
              >
                {field}
                <button
                  type="button"
                  className="ml-1 text-white hover:text-red-300 focus:outline-none"
                  onClick={() => {
                    const fieldIndex = fields.findIndex(
                      (f) => f.name === field
                    );
                    if (fieldIndex !== -1) {
                      onUpdateField(fieldIndex, "isOrderBy", false);
                    }
                  }}
                  aria-label={`Remove order by from ${field}`}
                >
                  <X className="h-3 w-3" />
                </button>
              </span>
            ))
          ) : (
            <span className="text-xs text-gray-500">
              No column selected as order by
            </span>
          )}
        </div>

        {/* Partition By Field Display */}
        <div className="flex flex-wrap gap-2 items-center">
          <span className="text-sm font-medium text-primary">
            Partition By Field:
          </span>
          {partitionByField ? (
            <span className="inline-flex items-center px-2 py-0.5 rounded bg-secondary text-xs font-medium text-white">
              {partitionByField}
              <button
                type="button"
                className="ml-1 text-white hover:text-red-300 focus:outline-none"
                onClick={() => {
                  const fieldIndex = fields.findIndex(
                    (f) => f.name === partitionByField
                  );
                  if (fieldIndex !== -1) {
                    onUpdateField(fieldIndex, "isPartitionBy", false);
                  }
                }}
                aria-label={`Remove partition by from ${partitionByField}`}
              >
                <X className="h-3 w-3" />
              </button>
            </span>
          ) : (
            <span className="text-xs text-gray-500">
              No column selected as partition by
            </span>
          )}
        </div>

        {/* Action Buttons */}
        <div className="flex flex-col sm:flex-row gap-4 justify-center">
          <Button
            onClick={onValidateAndGenerateSQL}
            variant="outline"
            disabled={!database || !tableName || fields.length === 0}
            className="w-full sm:w-auto"
          >
            Show Statement
          </Button>

          <Button
            onClick={onCreateManual}
            disabled={!database || !tableName || fields.length === 0}
            className="w-full sm:w-auto"
          >
            Create Table
          </Button>
        </div>

        {/* Generated SQL */}
        {sql && (
          <div className="mt-6">
            <div className="flex items-center justify-between">
              <h3 className="text-lg font-semibold">Generated SQL:</h3>
              <button
                type="button"
                onClick={onCopySQL}
                className="p-2 rounded hover:bg-gray-200"
                aria-label="Copy SQL Statement"
              >
                {!statementCopiedToClipBoard ? (
                  <Copy className="h-4 w-4" />
                ) : (
                  <CopyCheck className="h-4 w-4 text-green-500" />
                )}
              </button>
            </div>
            <pre className="bg-secondary p-4 rounded mt-2 text-sm overflow-x-auto">
              {sql}
            </pre>
          </div>
        )}

        {/* Error Message */}
        {createTableError && (
          <Alert variant="destructive" className="mt-4">
            <AlertDescription>{createTableError}</AlertDescription>
          </Alert>
        )}
      </div>
    </div>
  );
};

export default ManualCreationForm;
