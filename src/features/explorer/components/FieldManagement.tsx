// components/CreateTable/FieldManagement.tsx

import React from "react";
import { Label } from "@/components/ui/label";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Checkbox } from "@/components/ui/checkbox";
import { Button } from "@/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { Trash2, Plus } from "lucide-react";

// Define precise types for fields
export interface Field {
  name: string;
  type: string;
  nullable: boolean;
  description: string;
  isPrimaryKey: boolean;
  isOrderBy: boolean;
  isPartitionBy: boolean;
}

interface FieldManagementProps {
  fields: Field[];
  onAddField: () => void;
  onRemoveField: (index: number) => void;
  onUpdateField: (index: number, key: keyof Field, value: any) => void;
  errors: Record<string, string>;
  fieldTypes: string[];
}

const FIELD_TYPES = [
  "String",
  "UInt32",
  "UInt64",
  "Int32",
  "Int64",
  "Float32",
  "Float64",
  "Date",
  "DateTime",
  "Enum8",
  "Enum16",
  "Array(String)",
  "Array(UInt32)",
  "Other",
];

const FieldManagement: React.FC<FieldManagementProps> = ({
  fields,
  onAddField,
  onRemoveField,
  onUpdateField,
  errors,
}) => {
  return (
    <div className="w-full">
      {/* Header */}
      <div className="flex items-center justify-between mb-4 ">
        <p className="text-lg font-semibold">Table Schema</p>
        <Button
          aria-label="Add Column"
          size="sm"
          onClick={onAddField}
          className="flex items-center gap-2 text-sm text-green-500 hover:text-green-700"
          variant="outline"
        >
          <Plus className="h-4 w-4" />
          Add Column
        </Button>
      </div>

      {/* Fields List */}
      <div className="space-y-4 w-full">
        {fields.map((field, index) => (
          <div
            key={index}
            className="grid grid-cols-1 md:grid-cols-5 gap-4 items-center pb-5 border-b"
          >
            {/* Field Name */}
            <div className="flex flex-col">
              <TooltipProvider>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <Label htmlFor={`field-name-${index}`} className="mb-3">
                      Field Name
                    </Label>
                  </TooltipTrigger>
                  <TooltipContent>
                    Enter the name of the column. No spaces allowed.
                  </TooltipContent>
                </Tooltip>
              </TooltipProvider>
              <Input
                id={`field-name-${index}`}
                placeholder="e.g., user_id"
                value={field.name}
                onChange={(e) => onUpdateField(index, "name", e.target.value)}
                className={`h-10 ${
                  errors[`fields.${index}.name`] ? "border-red-500" : ""
                }`}
                aria-invalid={!!errors[`fields.${index}.name`]}
                aria-describedby={
                  errors[`fields.${index}.name`]
                    ? `error-field-name-${index}`
                    : undefined
                }
              />
              {errors[`fields.${index}.name`] && (
                <p
                  id={`error-field-name-${index}`}
                  className="mt-1 text-sm text-red-500"
                >
                  {errors[`fields.${index}.name`]}
                </p>
              )}
            </div>

            {/* Field Type */}
            <div className="flex flex-col">
              <TooltipProvider>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <Label htmlFor={`field-type-${index}`} className="mb-3">
                      Type
                    </Label>
                  </TooltipTrigger>
                  <TooltipContent>
                    Select the data type of the column.
                  </TooltipContent>
                </Tooltip>
              </TooltipProvider>
              <Select
                value={field.type}
                onValueChange={(value) => onUpdateField(index, "type", value)}
              >
                <SelectTrigger id={`field-type-${index}`} className="h-10">
                  <SelectValue placeholder="Select type" />
                </SelectTrigger>
                <SelectContent>
                  {FIELD_TYPES.map((type) => (
                    <SelectItem key={type} value={type}>
                      {type}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            {/* Nullable Selector */}
            <div className="flex flex-col">
              <TooltipProvider>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <Label htmlFor={`field-nullable-${index}`} className="mb-3">
                      Nullable
                    </Label>
                  </TooltipTrigger>
                  <TooltipContent>
                    Specify whether the column can contain NULL values.
                  </TooltipContent>
                </Tooltip>
              </TooltipProvider>
              <Select
                value={field.nullable ? "NULL" : "NOT NULL"}
                onValueChange={(value) =>
                  onUpdateField(index, "nullable", value === "NULL")
                }
              >
                <SelectTrigger id={`field-nullable-${index}`}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="NULL">NULL</SelectItem>
                  <SelectItem value="NOT NULL">NOT NULL</SelectItem>
                </SelectContent>
              </Select>
            </div>

            {/* Column Description */}
            <div className="flex flex-col">
              <TooltipProvider>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <Label
                      htmlFor={`field-description-${index}`}
                      className="mb-3"
                    >
                      Description
                    </Label>
                  </TooltipTrigger>
                  <TooltipContent>
                    Enter a description for the column.
                  </TooltipContent>
                </Tooltip>
              </TooltipProvider>
              <Input
                id={`field-description-${index}`}
                placeholder="e.g., Identifier for the user"
                value={field.description}
                onChange={(e) =>
                  onUpdateField(index, "description", e.target.value)
                }
                className="h-10"
              />
            </div>

            {/* Field Options: PK, OB, PB */}
            <div className="flex flex-col md:flex-row md:col-span-1 items-start md:items-center space-y-2 md:space-y-0 md:space-x-4 mt-6">
              {/* Primary Key Checkbox */}
              <div className="flex items-center space-x-1">
                <TooltipProvider>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Checkbox
                        id={`pk-checkbox-${index}`}
                        checked={field.isPrimaryKey}
                        onCheckedChange={() =>
                          onUpdateField(
                            index,
                            "isPrimaryKey",
                            !field.isPrimaryKey
                          )
                        }
                        disabled={!field.name}
                        aria-label={`Mark ${field.name} as Primary Key`}
                      />
                    </TooltipTrigger>
                    <TooltipContent>
                      <span className="text-xs text-gray-500">
                        Mark this column as part of the primary key.
                      </span>
                    </TooltipContent>
                  </Tooltip>
                </TooltipProvider>
                <Label
                  htmlFor={`pk-checkbox-${index}`}
                  className="text-sm text-gray-700"
                >
                  PK
                </Label>
              </div>

              {/* Order By Checkbox */}
              <div className="flex items-center space-x-1">
                <TooltipProvider>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Checkbox
                        id={`ob-checkbox-${index}`}
                        checked={field.isOrderBy}
                        onCheckedChange={() =>
                          onUpdateField(index, "isOrderBy", !field.isOrderBy)
                        }
                        disabled={!field.name}
                        aria-label={`Include ${field.name} in ORDER BY`}
                      />
                    </TooltipTrigger>
                    <TooltipContent>
                      <span className="text-xs text-gray-500">
                        Include this column in the ORDER BY clause.
                      </span>
                    </TooltipContent>
                  </Tooltip>
                </TooltipProvider>
                <Label
                  htmlFor={`ob-checkbox-${index}`}
                  className="text-sm text-gray-700"
                >
                  OB
                </Label>
              </div>

              {/* Partition By Checkbox */}
              <div className="flex items-center space-x-1 ">
                <TooltipProvider>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Checkbox
                        id={`pb-checkbox-${index}`}
                        checked={field.isPartitionBy}
                        onCheckedChange={() =>
                          onUpdateField(
                            index,
                            "isPartitionBy",
                            !field.isPartitionBy
                          )
                        }
                        disabled={
                          !field.name ||
                          !["Date", "DateTime"].includes(field.type)
                        }
                        aria-label={`Use ${field.name} for Partition By`}
                      />
                    </TooltipTrigger>
                    <TooltipContent>
                      <span className="text-xs text-gray-500">
                        Use this column for table partitioning. <br />
                        Only available for Date and DateTime types.
                      </span>
                    </TooltipContent>
                  </Tooltip>
                </TooltipProvider>
                <Label
                  htmlFor={`pb-checkbox-${index}`}
                  className="text-sm text-gray-700"
                >
                  PB
                </Label>
              </div>

              {/* Remove Field Button */}
              <Button
                variant="ghost"
                size="icon"
                onClick={() => onRemoveField(index)}
                aria-label={`Remove ${field.name} field`}
                className="text-red-500 hover:text-red-700"
              >
                <Trash2 className="h-4 w-4" />
              </Button>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
};

export default FieldManagement;
