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

interface FieldManagementProps {
  fields: any[];
  onAddField: () => void;
  onRemoveField: (index: number) => void;
  onUpdateField: (index: number, key: string, value: any) => void;
  errors: Record<string, string>;
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
];

const FieldManagement: React.FC<FieldManagementProps> = ({
  fields,
  onAddField,
  onRemoveField,
  onUpdateField,
  errors,
}) => {
  return (
    <div>
      <div className="flex items-center space-x-2 justify-between">
        <p className="text-lg font-semibold">Table Schema:</p>
        <Button
          size="sm"
          onClick={onAddField}
          variant="ghost"
          className="text-xs bg-transparent text-green-600 hover:text-green-400"
        >
          <Plus className="mr-2 h-4 w-4" /> Add Column
        </Button>
      </div>

      {fields.map((field, index) => (
        <div key={index} className="grid grid-cols-6 gap-4 items-center mt-2">
          {/* Field Name */}
          <div className="col-span-2">
            <TooltipProvider>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Label htmlFor={`field-name-${index}`}>Field Name</Label>
                </TooltipTrigger>
                <TooltipContent>
                  Enter the name of the column. No spaces allowed.
                </TooltipContent>
              </Tooltip>
            </TooltipProvider>
            <Input
              id={`field-name-${index}`}
              placeholder="Field Name"
              value={field.name}
              onChange={(e) => onUpdateField(index, "name", e.target.value)}
              className={errors[`fields.${index}.name`] ? "border-red-500" : ""}
            />
            {errors[`fields.${index}.name`] && (
              <p className="text-xs text-red-500">
                {errors[`fields.${index}.name`]}
              </p>
            )}
          </div>

          {/* Field Type */}
          <div className="col-span-1">
            <TooltipProvider>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Label htmlFor={`field-type-${index}`}>Type</Label>
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
              <SelectTrigger className="w-full">
                <SelectValue />
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
          <div className="col-span-1">
            <TooltipProvider>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Label htmlFor={`field-nullable-${index}`}>Nullable</Label>
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
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="NULL">NULL</SelectItem>
                <SelectItem value="NOT NULL">NOT NULL</SelectItem>
              </SelectContent>
            </Select>
          </div>

          {/* Field Options: PK, OB, PB */}
          <div className="col-span-2 flex items-center space-x-2">
            {/* Primary Key Checkbox */}
            <TooltipProvider>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Checkbox
                    id={`pk-check-box-${index}`}
                    checked={field.isPrimaryKey}
                    onCheckedChange={() =>
                      onUpdateField(index, "isPrimaryKey", !field.isPrimaryKey)
                    }
                    disabled={!field.name}
                    aria-label="Primary Key"
                  />
                </TooltipTrigger>
                <TooltipContent>
                  Mark this column as part of the primary key.
                </TooltipContent>
              </Tooltip>
            </TooltipProvider>
            <Label
              htmlFor={`pk-check-box-${index}`}
              className="whitespace-nowrap text-xs text-primary/70"
            >
              PK
            </Label>

            {/* Order By Checkbox */}
            <TooltipProvider>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Checkbox
                    id={`ob-check-box-${index}`}
                    checked={field.isOrderBy}
                    onCheckedChange={() =>
                      onUpdateField(index, "isOrderBy", !field.isOrderBy)
                    }
                    disabled={!field.name}
                    aria-label="Order By"
                  />
                </TooltipTrigger>
                <TooltipContent>
                  Include this column in the ORDER BY clause.
                </TooltipContent>
              </Tooltip>
            </TooltipProvider>
            <Label
              htmlFor={`ob-check-box-${index}`}
              className="whitespace-nowrap text-xs text-primary/70"
            >
              OB
            </Label>

            {/* Partition By Checkbox */}
            <TooltipProvider>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Checkbox
                    id={`pb-check-box-${index}`}
                    checked={field.isPartitionBy}
                    onCheckedChange={() =>
                      onUpdateField(
                        index,
                        "isPartitionBy",
                        !field.isPartitionBy
                      )
                    }
                    disabled={
                      !field.name || !["Date", "DateTime"].includes(field.type)
                    }
                    aria-label="Partition By"
                  />
                </TooltipTrigger>
                <TooltipContent>
                  Use this column for table partitioning.
                </TooltipContent>
              </Tooltip>
            </TooltipProvider>
            <Label
              htmlFor={`pb-check-box-${index}`}
              className="whitespace-nowrap text-xs text-primary/70"
            >
              PB
            </Label>

            {/* Remove Field Button */}
            <Button
              variant="link"
              size="icon"
              onClick={() => onRemoveField(index)}
              aria-label="Remove Field"
            >
              <Trash2 className="h-4 w-4 text-red-500" />
            </Button>
          </div>
        </div>
      ))}
    </div>
  );
};

export default FieldManagement;
