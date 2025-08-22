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
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Card, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import FieldManagement, { Field } from "./FieldManagement";
import hljs from "highlight.js/lib/core";
import sqlLang from "highlight.js/lib/languages/sql";
import "highlight.js/styles/a11y-dark.css";

hljs.registerLanguage("sql", sqlLang);

interface DatabaseItem {
  name: string;
  type: string;
}

interface ManualCreationFormProps {
  database: string;
  tableName: string;
  engine: string;
  fields: Field[];
  primaryKeyFields: string[];
  orderByFields: string[];
  partitionByField: string | null;
  comment: string;
  onCluster?: boolean;
  clusterName?: string;
  errors: Record<string, string>;
  onChange: (field: string, value: any) => void;
  onAddField: () => void;
  onRemoveField: (index: number) => void;
  onUpdateField: (index: number, key: keyof Field, value: any) => void;
  onValidateAndGenerateSQL: () => string | null;
  onCreateManual: () => Promise<void>;
  createTableError?: string;
  isLoading?: boolean;
  databaseData: DatabaseItem[];
}

const TABLE_ENGINES = [
  "MergeTree",
  "ReplacingMergeTree",
  "ReplicatedMergeTree",
  "SummingMergeTree",
  "AggregatingMergeTree",
  "CollapsingMergeTree",
  "VersionedCollapsingMergeTree",
  "Memory",
  "Distributed",
] as const;

const ManualCreationForm: React.FC<ManualCreationFormProps> = ({
  database,
  tableName,
  engine,
  fields,
  primaryKeyFields,
  orderByFields,
  partitionByField,
  comment,
  onCluster = false,
  clusterName = "",
  errors,
  onChange,
  onAddField,
  onRemoveField,
  onUpdateField,
  onValidateAndGenerateSQL,
  onCreateManual,
  createTableError,
  isLoading = false,
  databaseData,
}) => {
  const [sql, setSql] = React.useState<string>("");
  const [isCopied, setIsCopied] = React.useState(false);

  const handleGenerateSQL = () => {
    const generatedSQL = onValidateAndGenerateSQL();
    if (generatedSQL) {
      setSql(generatedSQL);
    }
  };

  const handleCopySQL = async () => {
    await navigator.clipboard.writeText(sql);
    setIsCopied(true);
    setTimeout(() => setIsCopied(false), 2000);
  };

  const removeField = (type: "pk" | "ob" | "pb", fieldName: string) => {
    const fieldIndex = fields.findIndex((f) => f.name === fieldName);
    if (fieldIndex !== -1) {
      switch (type) {
        case "pk":
          onUpdateField(fieldIndex, "isPrimaryKey", false);
          break;
        case "ob":
          onUpdateField(fieldIndex, "isOrderBy", false);
          break;
        case "pb":
          onUpdateField(fieldIndex, "isPartitionBy", false);
          break;
      }
    }
  };

  return (
    <div className="space-y-8 w-full">
      {/* Database and Table Name */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
        <div className="space-y-2">
          <Label htmlFor="database">Database</Label>
          <Select
            value={database}
            onValueChange={(value) => onChange("database", value)}
          >
            <SelectTrigger id="database">
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
            <p className="text-sm text-destructive">{errors.database}</p>
          )}
        </div>

        <div className="space-y-2">
          <Label htmlFor="tableName">Table Name</Label>
          <Input
            id="tableName"
            value={tableName}
            onChange={(e) => onChange("tableName", e.target.value)}
            placeholder="Enter table name"
            className={errors.tableName ? "border-destructive" : ""}
          />
          {errors.tableName && (
            <p className="text-sm text-destructive">{errors.tableName}</p>
          )}
        </div>
      </div>

      {/* Engine and Comment */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
        <div className="space-y-2">
          <Label htmlFor="engine">Engine</Label>
          <Select
            value={engine}
            onValueChange={(value) => onChange("engine", value)}
          >
            <SelectTrigger id="engine">
              <SelectValue placeholder="Select engine" />
            </SelectTrigger>
            <SelectContent>
              {TABLE_ENGINES.map((eng) => (
                <SelectItem key={eng} value={eng}>
                  {eng}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        {/* ON CLUSTER Settings */}
        <div className="space-y-4">
          <div className="flex items-center space-x-2">
            <Checkbox
              id="onCluster"
              checked={onCluster}
              onCheckedChange={(checked) => onChange("onCluster", checked)}
            />
            <Label htmlFor="onCluster" className="text-sm font-medium leading-none peer-disabled:cursor-not-allowed peer-disabled:opacity-70">
              ON CLUSTER
            </Label>
          </div>
          
          {onCluster && (
            <div className="space-y-2">
              <Label htmlFor="clusterName">Cluster Name</Label>
              <Input
                id="clusterName"
                value={clusterName}
                onChange={(e) => onChange("clusterName", e.target.value)}
                placeholder="Enter cluster name"
                className={errors.clusterName ? "border-red-500" : ""}
              />
              {errors.clusterName && (
                <p className="text-xs text-red-500">{errors.clusterName}</p>
              )}
            </div>
          )}
        </div>

        <div className="space-y-2">
          <Label htmlFor="comment">Table Comment</Label>
          <Input
            id="comment"
            value={comment}
            onChange={(e) => onChange("comment", e.target.value)}
            placeholder="Enter table comment"
          />
        </div>
      </div>

      {/* Field Management */}
      <Card>
        <CardContent className="pt-6">
          <FieldManagement
            fields={fields}
            onAddField={onAddField}
            onRemoveField={onRemoveField}
            onUpdateField={onUpdateField}
            errors={errors}
          />
        </CardContent>
      </Card>

      {/* Field Summary */}
      <div className="space-y-4">
        {/* Primary Keys */}
        <div className="flex flex-wrap gap-2 items-center">
          <span className="text-sm font-medium">Primary Key(s):</span>
          <div className="flex flex-wrap gap-2">
            {primaryKeyFields.length > 0 ? (
              primaryKeyFields.map((field) => (
                <Badge
                  key={field}
                  variant="secondary"
                  className="flex items-center gap-1"
                >
                  {field}
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-4 w-4 p-0 hover:bg-transparent"
                    onClick={() => removeField("pk", field)}
                  >
                    <X className="h-3 w-3" />
                  </Button>
                </Badge>
              ))
            ) : (
              <span className="text-sm text-muted-foreground">
                None selected
              </span>
            )}
          </div>
        </div>

        {/* Order By */}
        <div className="flex flex-wrap gap-2 items-center">
          <span className="text-sm font-medium">Order By:</span>
          <div className="flex flex-wrap gap-2">
            {orderByFields.length > 0 ? (
              orderByFields.map((field) => (
                <Badge
                  key={field}
                  variant="secondary"
                  className="flex items-center gap-1"
                >
                  {field}
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-4 w-4 p-0 hover:bg-transparent"
                    onClick={() => removeField("ob", field)}
                  >
                    <X className="h-3 w-3" />
                  </Button>
                </Badge>
              ))
            ) : (
              <span className="text-sm text-muted-foreground">
                None selected
              </span>
            )}
          </div>
        </div>

        {/* Partition By */}
        <div className="flex flex-wrap gap-2 items-center">
          <span className="text-sm font-medium">Partition By:</span>
          <div className="flex flex-wrap gap-2">
            {partitionByField ? (
              <Badge variant="secondary" className="flex items-center gap-1">
                {partitionByField}
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-4 w-4 p-0 hover:bg-transparent"
                  onClick={() => removeField("pb", partitionByField)}
                >
                  <X className="h-3 w-3" />
                </Button>
              </Badge>
            ) : (
              <span className="text-sm text-muted-foreground">
                None selected
              </span>
            )}
          </div>
        </div>
      </div>

      {/* Action Buttons */}
      <div className="flex flex-col sm:flex-row gap-4">
        <Button
          variant="outline"
          onClick={handleGenerateSQL}
          disabled={!database || !tableName || fields.length === 0}
          className="flex-1"
        >
          Preview SQL
        </Button>
        <Button
          onClick={onCreateManual}
          disabled={!database || !tableName || fields.length === 0 || isLoading}
          className="flex-1"
        >
          {isLoading ? "Creating..." : "Create Table"}
        </Button>
      </div>

      {/* SQL Preview */}
      {sql && (
        <div className="space-y-2">
          <div className="flex items-center justify-between">
            <h3 className="text-lg font-semibold">Generated SQL:</h3>
            <Button
              variant="ghost"
              size="sm"
              onClick={handleCopySQL}
              className="flex items-center gap-2"
            >
              {isCopied ? (
                <CopyCheck className="h-4 w-4" />
              ) : (
                <Copy className="h-4 w-4" />
              )}
              {isCopied ? "Copied!" : "Copy"}
            </Button>
          </div>
          <pre
            className="bg-[#2d2d2d] rounded-md p-4 overflow-x-auto"
            dangerouslySetInnerHTML={{
              __html: hljs.highlight(sql, { language: "sql" }).value,
            }}
          />
        </div>
      )}

      {/* Error Display */}
      {createTableError && (
        <Alert variant="destructive">
          <AlertDescription>{createTableError}</AlertDescription>
        </Alert>
      )}
    </div>
  );
};

export default ManualCreationForm;
