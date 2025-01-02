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
import { Badge } from "@/components/ui/badge";
import { Trash2, Plus, X, Info } from "lucide-react";

export interface Field {
  name: string;
  type: string;
  nullable: boolean;
  description: string;
  isPrimaryKey: boolean;
  isOrderBy: boolean;
  isPartitionBy: boolean;
  customType?: string;
}

interface FieldManagementProps {
  fields: Field[];
  onAddField: () => void;
  onRemoveField: (index: number) => void;
  onUpdateField: (index: number, key: keyof Field, value: any) => void;
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
  "Other",
] as const;

const FieldManagement: React.FC<FieldManagementProps> = ({
  fields,
  onAddField,
  onRemoveField,
  onUpdateField,
  errors,
}) => {
  const handleTypeChange = (index: number, value: string) => {
    if (value !== "Other") {
      onUpdateField(index, "customType", "");
    }
    onUpdateField(index, "type", value);

    // Reset partition by if type is changed and not compatible
    if (!["Date", "DateTime"].includes(value) && fields[index].isPartitionBy) {
      onUpdateField(index, "isPartitionBy", false);
    }
  };

  const handleResetCustomType = (index: number) => {
    onUpdateField(index, "type", "String");
    onUpdateField(index, "customType", "");
  };

  return (
    <div className="w-full space-y-6">
      <div className="flex items-center justify-between">
        <div className="space-y-1">
          <h3 className="text-2xl font-semibold tracking-tight">Table Schema</h3>
          <p className="text-sm text-muted-foreground">
            Define your table structure and constraints
          </p>
        </div>
        <Button
          onClick={onAddField}
          className="flex items-center gap-2"
          variant="outline"
        >
          <Plus className="h-4 w-4" />
          Add Column
        </Button>
      </div>

      <div className="space-y-6">
        {fields.map((field, index) => (
          <div
            key={index}
            className="grid grid-cols-1 md:grid-cols-6 gap-4 p-4 rounded-lg border bg-card"
          >
            {/* Field Name */}
            <div className="space-y-2">
              <Label 
                htmlFor={`field-name-${index}`}
                className="flex items-center gap-2"
              >
                Field Name
                <TooltipProvider>
                  <Tooltip>
                    <TooltipTrigger>
                      <Info className="h-4 w-4 text-muted-foreground" />
                    </TooltipTrigger>
                    <TooltipContent>
                      Enter the name of the column. No spaces allowed.
                    </TooltipContent>
                  </Tooltip>
                </TooltipProvider>
              </Label>
              <Input
                id={`field-name-${index}`}
                placeholder="e.g., user_id"
                value={field.name}
                onChange={(e) => onUpdateField(index, "name", e.target.value)}
                className={errors[`fields.${index}.name`] ? "border-destructive" : ""}
              />
              {errors[`fields.${index}.name`] && (
                <p className="text-sm text-destructive">
                  {errors[`fields.${index}.name`]}
                </p>
              )}
            </div>

            {/* Field Type */}
            <div className="space-y-2">
              <Label 
                htmlFor={`field-type-${index}`}
                className="flex items-center gap-2"
              >
                Type
                <TooltipProvider>
                  <Tooltip>
                    <TooltipTrigger>
                      <Info className="h-4 w-4 text-muted-foreground" />
                    </TooltipTrigger>
                    <TooltipContent>
                      Select the data type of the column
                    </TooltipContent>
                  </Tooltip>
                </TooltipProvider>
              </Label>
              {field.type !== "Other" ? (
                <Select
                  value={field.type}
                  onValueChange={(value) => handleTypeChange(index, value)}
                >
                  <SelectTrigger id={`field-type-${index}`}>
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
              ) : (
                <div className="relative">
                  <Input
                    id={`field-custom-type-${index}`}
                    placeholder="Enter custom type"
                    value={field.customType || ""}
                    onChange={(e) => onUpdateField(index, "customType", e.target.value)}
                    className={`pr-8 ${
                      errors[`fields.${index}.customType`] ? "border-destructive" : ""
                    }`}
                  />
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    className="absolute right-1 top-1 h-6 w-6 p-0"
                    onClick={() => handleResetCustomType(index)}
                  >
                    <X className="h-4 w-4" />
                  </Button>
                </div>
              )}
              {field.type === "Other" && errors[`fields.${index}.customType`] && (
                <p className="text-sm text-destructive">
                  {errors[`fields.${index}.customType`]}
                </p>
              )}
            </div>

            {/* Nullable Selector */}
            <div className="space-y-2">
              <Label 
                htmlFor={`field-nullable-${index}`}
                className="flex items-center gap-2"
              >
                Nullable
                <TooltipProvider>
                  <Tooltip>
                    <TooltipTrigger>
                      <Info className="h-4 w-4 text-muted-foreground" />
                    </TooltipTrigger>
                    <TooltipContent>
                      Specify whether the column can contain NULL values
                    </TooltipContent>
                  </Tooltip>
                </TooltipProvider>
              </Label>
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

            {/* Description */}
            <div className="space-y-2">
              <Label 
                htmlFor={`field-description-${index}`}
                className="flex items-center gap-2"
              >
                Description
                <TooltipProvider>
                  <Tooltip>
                    <TooltipTrigger>
                      <Info className="h-4 w-4 text-muted-foreground" />
                    </TooltipTrigger>
                    <TooltipContent>
                      Enter a description for the column
                    </TooltipContent>
                  </Tooltip>
                </TooltipProvider>
              </Label>
              <Input
                id={`field-description-${index}`}
                placeholder="e.g., Identifier for the user"
                value={field.description}
                onChange={(e) => onUpdateField(index, "description", e.target.value)}
              />
            </div>

            {/* Field Options */}
            <div className="flex flex-col space-y-4 md:space-y-0 md:flex-row md:items-end md:space-x-4 md:col-span-2">
              <div className="flex items-center space-x-4">
                <TooltipProvider>
                  <Tooltip>
                    <TooltipTrigger className="flex items-center space-x-2">
                      <Checkbox
                        id={`pk-checkbox-${index}`}
                        checked={field.isPrimaryKey}
                        onCheckedChange={() =>
                          onUpdateField(index, "isPrimaryKey", !field.isPrimaryKey)
                        }
                        disabled={!field.name}
                      />
                      <Label htmlFor={`pk-checkbox-${index}`}>
                        <Badge className="" variant={field.isPrimaryKey ? "default" : "outline"}>
                          PK
                        </Badge>
                      </Label>
                    </TooltipTrigger>
                    <TooltipContent>
                      Mark this column as part of the primary key
                    </TooltipContent>
                  </Tooltip>
                </TooltipProvider>

                <TooltipProvider>
                  <Tooltip>
                    <TooltipTrigger className="flex items-center space-x-2">
                      <Checkbox
                        id={`ob-checkbox-${index}`}
                        checked={field.isOrderBy}
                        onCheckedChange={() =>
                          onUpdateField(index, "isOrderBy", !field.isOrderBy)
                        }
                        disabled={!field.name}
                      />
                      <Label htmlFor={`ob-checkbox-${index}`}>
                        <Badge className="" variant={field.isOrderBy ? "default" : "outline"}>
                          OB
                        </Badge>
                      </Label>
                    </TooltipTrigger>
                    <TooltipContent>
                      Include this column in the ORDER BY clause
                    </TooltipContent>
                  </Tooltip>
                </TooltipProvider>

                <TooltipProvider>
                  <Tooltip>
                    <TooltipTrigger className="flex items-center space-x-2">
                      <Checkbox
                        id={`pb-checkbox-${index}`}
                        checked={field.isPartitionBy}
                        onCheckedChange={() => {
                          // Uncheck other partition by fields first
                          fields.forEach((_, i) => {
                            if (i !== index && fields[i].isPartitionBy) {
                              onUpdateField(i, "isPartitionBy", false);
                            }
                          });
                          onUpdateField(index, "isPartitionBy", !field.isPartitionBy);
                        }}
                        disabled={
                          !field.name ||
                          (!["Date", "DateTime"].includes(field.type) &&
                            field.type !== "Other")
                        }
                      />
                      <Label htmlFor={`pb-checkbox-${index}`}>
                        <Badge className="" variant={field.isPartitionBy ? "default" : "outline"}>
                          PB
                        </Badge>
                      </Label>
                    </TooltipTrigger>
                    <TooltipContent>
                      Use this column for table partitioning (Date/DateTime only)
                    </TooltipContent>
                  </Tooltip>
                </TooltipProvider>
              </div>

              <Button
                variant="ghost"
                size="icon"
                onClick={() => onRemoveField(index)}
                className="text-destructive hover:text-destructive/90 hover:bg-destructive/10"
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