import React, { useState, useEffect } from "react";
import { z } from "zod";
import {
  Check,
  ChevronsUpDown,
  X,
  Trash2,
  Plus,
  Info,
  CopyIcon,
  CopyCheck,
} from "lucide-react";
import { cn } from "@/lib/utils";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from "@/components/ui/sheet";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { Textarea } from "@/components/ui/textarea";
import { Checkbox } from "@/components/ui/checkbox";
import { Badge } from "@/components/ui/badge";
import { Label } from "@/components/ui/label";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import { toast } from "sonner";
import { json } from "stream/consumers";

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

const TIME_FIELDS = ["Date", "DateTime"];
const INT_FIELDS = ["UInt32", "UInt64", "Int32", "Int64"];
const ENGINES = [
  "MergeTree",
  "ReplacingMergeTree",
  "SummingMergeTree",
  "AggregatingMergeTree",
  "CollapsingMergeTree",
  "VersionedCollapsingMergeTree",
  "Memory",
];

const DEFAULT_PARTITION_OPTIONS = ["toYYYYMM", "toYYYYMMDD", "toYear"];

interface CreateTableProps {
  runQuery: (query: string, sql: string) => Promise<void>;
  fetchDatabaseData: () => void;
  databaseData: { name: string; children: { name: string }[] }[];
}

const CreateTable: React.FC<CreateTableProps> = ({
  runQuery,
  fetchDatabaseData,
  databaseData,
}) => {
  const [open, setOpen] = useState(false);
  const [database, setDatabase] = useState("");
  const [tableName, setTableName] = useState("");
  const [engine, setEngine] = useState("MergeTree");
  const [fields, setFields] = useState([
    { name: "", type: "String", nullable: false, isPrimaryKey: false },
  ]);
  const [primaryKeyFields, setPrimaryKeyFields] = useState<string[]>([]);
  const [orderBy, setOrderBy] = useState("");
  const [partitionBy, setPartitionBy] = useState("");
  const [comment, setComment] = useState("");
  const [sql, setSql] = useState("");
  const [loading, setLoading] = useState(false);
  interface Errors {
    [key: string]: string | undefined;
  }

  const [errors, setErrors] = useState<Errors>({});

  const [createTableError, setCreateTableError] = useState<String>("");
  const [statementCopiedToClipBoard, setStatementCopiedToClipBoard] =
    useState<Boolean>(false);

  console.log(JSON.stringify(databaseData));

  let databases = databaseData
    .filter((item) => item.type === "database")
    .map((db) => ({
      value: db.name,
      label: db.name,
      children: db.children.map((child) => ({
        value: child.name,
        label: child.name,
        type: child.type,
      })),
    }));

  useEffect(() => {
    const newPrimaryKeyFields = fields
      .filter((field) => field.isPrimaryKey && field.name)
      .map((field) => field.name);
    setPrimaryKeyFields(newPrimaryKeyFields);
  }, [fields]);

  const addField = () => {
    setFields([
      ...fields,
      { name: "", type: "String", nullable: false, isPrimaryKey: false },
    ]);
  };

  const removeField = (index: number) => {
    setFields(fields.filter((_, i) => i !== index));
    setErrors((prev) => {
      const newErrors = { ...prev };
      delete newErrors[`fields.${index}.name`];
      return newErrors;
    });
  };

  type FieldKey = "name" | "type" | "nullable" | "isPrimaryKey";

  const updateField = (index: any, key: FieldKey, value: string | boolean) => {
    const updatedFields = [...fields];
    updatedFields[index][key] = value;
    setFields(updatedFields);
    if (key === "name") {
      setErrors((prev) => {
        const newErrors = { ...prev };
        delete newErrors[`fields.${index}.name`];
        return newErrors;
      });
    }
  };

  const validateTableName = (name: string) => {
    const selectedDb = databaseData.find(
      (db: { name: string }) => db.name === database
    );
    if (
      selectedDb &&
      selectedDb.children.some(
        (table: { name: string }) =>
          table.name.toLowerCase() === name.toLowerCase()
      )
    ) {
      return false;
    }
    return true;
  };

  const validateAndGenerateSQL = async () => {
    const tableSchema = z.object({
      database: z.string().min(1, "Database is required"),
      tableName: z
        .string()
        .min(1, "Table name is required")
        .refine(validateTableName, {
          message: "Table name already exists in this database",
        }),
      fields: z
        .array(
          z.object({
            name: z
              .string()
              .min(1, "Name is required")
              .refine((value) => !/\s/.test(value), {
                message: "Name cannot contain spaces",
              }),
            type: z.string(),
            nullable: z.boolean(),
            isPrimaryKey: z.boolean(),
          })
        )
        .min(1, "At least one field is required"),
    });
    try {
      tableSchema.parse({ database, tableName, fields });

      const fieldDefinitions = fields
        .map(
          (field) =>
            `${field.name} ${field.type}${
              field.nullable ? " NULL" : " NOT NULL"
            }`
        )
        .join(",\n    ");

      let sqlStatement = `CREATE TABLE ${database}.${tableName}\n(\n    ${fieldDefinitions}\n) ENGINE = ${engine}\n`;

      if (orderBy) sqlStatement += `ORDER BY (${orderBy})\n`;
      if (partitionBy) sqlStatement += `PARTITION BY ${partitionBy}\n`;
      if (primaryKeyFields.length)
        sqlStatement += `PRIMARY KEY (${primaryKeyFields.join(", ")})\n`;
      if (comment) sqlStatement += `COMMENT '${comment}'`;

      setSql(sqlStatement.trim());
      setErrors({});
      return true;
    } catch (error) {
      if (error instanceof z.ZodError) {
        const newErrors: { [key: string]: string } = {};
        error.errors.forEach((err) => {
          newErrors[err.path.join(".")] = err.message;
        });
        setErrors(newErrors);
        toast.error("Please correct the errors");
      } else {
        toast.error("Unknown error occurred");
      }
      return false;
    }
  };

  const handleCreate = async () => {
    if (!(await validateAndGenerateSQL())) return;

    setLoading(true);
    setCreateTableError("");
    try {
      await runQuery("", sql);
      fetchDatabaseData();
      toast.success("Table created successfully!");
      // Reset form after successful creation
      setTableName("");
      setFields([
        { name: "", type: "String", nullable: false, isPrimaryKey: false },
      ]);
      setOrderBy("");
      setPartitionBy("");
      setComment("");
      setSql("");
    } catch (error: any) {
      console.log(error);
      toast.error(`Failed to create table: ${error}`);
      setCreateTableError(error);
    } finally {
      setLoading(false);
    }
  };

  const handleShowStatement = async () => {
    setCreateTableError("");
    await validateAndGenerateSQL();
  };

  const handlePrimaryKeyToggle = (index: number) => {
    const updatedFields = fields.map((field, i) =>
      i === index ? { ...field, isPrimaryKey: !field.isPrimaryKey } : field
    );
    setFields(updatedFields);
  };

  const removePrimaryKeyField = (fieldName: string) => {
    const updatedFields = fields.map((field) =>
      field.name === fieldName ? { ...field, isPrimaryKey: false } : field
    );
    setFields(updatedFields);
  };

  return (
    <Sheet>
      <SheetTrigger asChild>
        <Button>Create Table</Button>
      </SheetTrigger>
      <SheetContent className="xl:w-[1000px] xl:max-w-none sm:w-[400px] sm:max-w-[540px] overflow-auto">
        <SheetHeader>
          <SheetTitle>
            Create Table - {database}.{tableName}
          </SheetTitle>
        </SheetHeader>
        <div className="mt-4 space-y-4">
          <div className=" space-x-4 justify-between grid grid-cols-3">
            <div className="space-y-2">
              <Label htmlFor="database-name">Database</Label>
              <Popover open={open} onOpenChange={setOpen}>
                <PopoverTrigger asChild>
                  <Button
                    id="database-name"
                    variant="outline"
                    role="combobox"
                    aria-expanded={open}
                    className={cn(
                      "w-full justify-between",
                      errors.database ? "border-red-500" : ""
                    )}
                  >
                    {database
                      ? databases.find(
                          (db: { value: string }) => db.value === database
                        )?.label
                      : "Select database..."}
                    <ChevronsUpDown className="ml-2 h-4 w-4 shrink-0 opacity-50" />
                  </Button>
                </PopoverTrigger>
                <PopoverContent className="w-[200px] p-0">
                  <Command>
                    <CommandInput placeholder="Search database..." />
                    <CommandList>
                      <CommandEmpty>No database found.</CommandEmpty>
                      <CommandGroup>
                        {databases.map(
                          (db: {
                            value: React.Key | null | undefined;
                            label:
                              | string
                              | number
                              | boolean
                              | React.ReactElement<
                                  any,
                                  string | React.JSXElementConstructor<any>
                                >
                              | Iterable<React.ReactNode>
                              | React.ReactPortal
                              | null
                              | undefined;
                          }) => (
                            <CommandItem
                              key={db.value}
                              value={db.value as string}
                              onSelect={(currentValue) => {
                                setDatabase(
                                  currentValue === database ? "" : currentValue
                                );
                                setOpen(false);
                              }}
                            >
                              <Check
                                className={cn(
                                  "mr-2 h-4 w-4",
                                  database === db.value
                                    ? "opacity-100"
                                    : "opacity-0"
                                )}
                              />
                              {db.label}
                            </CommandItem>
                          )
                        )}
                      </CommandGroup>
                    </CommandList>
                  </Command>
                </PopoverContent>
              </Popover>
              {errors.database && (
                <p className="text-xs text-red-500">{errors.database}</p>
              )}
            </div>
            <div className="space-y-2">
              <Label htmlFor="table-name">Table name</Label>
              <Input
                id="table-name"
                placeholder="Table Name"
                value={tableName}
                onChange={(e) => {
                  setTableName(e.target.value);
                  setErrors((prev) => ({ ...prev, tableName: undefined }));
                }}
                className={cn(errors.tableName ? "border-red-500" : "")}
              />
              {errors.tableName && (
                <p className="text-xs text-red-500">{errors.tableName}</p>
              )}
            </div>
            <div className="space-y-2">
              <Label htmlFor="engine-name">Database Engine</Label>

              <Select value={engine} onValueChange={setEngine}>
                <SelectTrigger id="engine-name">
                  <SelectValue placeholder="Select engine" />
                </SelectTrigger>
                <SelectContent>
                  {ENGINES.map((eng) => (
                    <SelectItem key={eng} value={eng}>
                      {eng}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>

          <div className="flex items-center space-x-2 justify-between">
            <p className="text-lg font-semibold">Table Schema:</p>
            <Button
              size="sm"
              onClick={addField}
              variant="ghost"
              className="text-xs bg-transparent text-green-600 hover:text-green-400"
            >
              <Plus className="mr-2 h-4 w-4" /> Add Column
            </Button>
          </div>

          {fields.map((field, index) => (
            <div key={index} className="grid grid-cols-4 gap-4 items-center">
              {/* Field Name */}
              <div className="col-span-1">
                <Input
                  placeholder="Field Name"
                  value={field.name}
                  onChange={(e) => updateField(index, "name", e.target.value)}
                  className={cn(
                    errors[`fields.${index}.name`] ? "border-red-500" : ""
                  )}
                />
                {errors[`fields.${index}.name`] && (
                  <p className="text-xs text-red-500">
                    {errors[`fields.${index}.name`]}
                  </p>
                )}
              </div>

              {/* Field Type */}
              <div className="col-span-1">
                <Select
                  value={field.type}
                  onValueChange={(value) => updateField(index, "type", value)}
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

              {/* Nullable */}
              <div className="col-span-1">
                <Select
                  value={field.nullable ? "NULL" : "NOT NULL"}
                  onValueChange={(value) =>
                    updateField(index, "nullable", value === "NULL")
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

              {/* Primary Key and Remove */}
              <div className="col-span-1 flex items-center space-x-2">
                <Checkbox
                  id={`pk-check-box-${index}`}
                  checked={field.isPrimaryKey}
                  onCheckedChange={() => handlePrimaryKeyToggle(index)}
                  disabled={!field.name}
                  aria-label="Primary Key"
                />
                <Label
                  htmlFor={`pk-check-box-${index}`}
                  className="whitespace-nowrap text-xs text-primary/70"
                >
                  Set as PK
                </Label>
                <TooltipProvider>
                  <Tooltip>
                    <TooltipTrigger>
                      <Info className="h-4 w-4 text-blue-500" />
                    </TooltipTrigger>
                    <TooltipContent>
                      <p className="p-2 max-w-[300px] text-xs text-primary/50">
                        Primary Key is a column or a set of columns that
                        uniquely identifies each row in the table. You can only
                        select a column as primary key if the given column has a
                        name.
                      </p>
                    </TooltipContent>
                  </Tooltip>
                </TooltipProvider>

                {/* Remove Button */}
                <Button
                  variant="link"
                  size="icon"
                  onClick={() => removeField(index)}
                >
                  <Trash2 className="h-4 w-4 text-red-500" />
                </Button>
              </div>
            </div>
          ))}

          <div className="flex gap-4 ">
            <div className="flex-col space-y-1">
              <Label htmlFor="order-by">Order by:</Label>

              <div className="flex items-center space-x-2">
                <Select
                  onValueChange={(value) => setOrderBy(value)}
                  value={orderBy}
                >
                  <SelectTrigger id="order-by" className="w-[300px]">
                    <SelectValue placeholder="Order By (Date/Time fields)" />
                  </SelectTrigger>
                  <SelectContent>
                    {fields
                      .filter(
                        (field) =>
                          TIME_FIELDS.includes(field.type) && field.name
                      )
                      .map((field) => (
                        <SelectItem key={field.name} value={field.name}>
                          {field.name}
                        </SelectItem>
                      ))}
                  </SelectContent>
                </Select>

                <Button
                  onClick={() => setOrderBy("")}
                  variant="outline"
                  size="sm"
                >
                  Clear
                </Button>
              </div>
            </div>

            <div className="flex-col space-y-1 w-full">
              <Label htmlFor="partition-by">Partition by:</Label>

              <div className="flex items-center space-x-2">
                <Select
                  onValueChange={(value) => setPartitionBy(value)}
                  value={partitionBy}
                >
                  <SelectTrigger id="partition-by" className="w-full">
                    <SelectValue placeholder="Partition By (Time/Integer fields)" />
                  </SelectTrigger>
                  <SelectContent>
                    {DEFAULT_PARTITION_OPTIONS.map((option) => (
                      <SelectItem key={option} value={option}>
                        {option}
                      </SelectItem>
                    ))}
                    {fields
                      .filter(
                        (field) =>
                          (TIME_FIELDS.includes(field.type) ||
                            INT_FIELDS.includes(field.type)) &&
                          field.name
                      )
                      .map((field) => (
                        <SelectItem key={field.name} value={field.name}>
                          {field.name}
                        </SelectItem>
                      ))}
                  </SelectContent>
                </Select>

                <Button
                  onClick={() => setPartitionBy("")}
                  variant="outline"
                  size="sm"
                >
                  Clear
                </Button>
              </div>
            </div>
          </div>

          <Textarea
            placeholder="Table Comment"
            value={comment}
            onChange={(e) => setComment(e.target.value)}
          />

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
                    onClick={() => removePrimaryKeyField(field)}
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

          <div className="flex gap-4">
            <Button
              onClick={handleShowStatement}
              variant="outline"
              disabled={!database || !tableName || fields.length === 0}
            >
              Show Statement
            </Button>

            <Button
              onClick={handleCreate}
              disabled={
                loading || !database || !tableName || fields.length === 0
              }
            >
              {loading ? "Creating..." : "Create Table"}
            </Button>
          </div>
        </div>

        {createTableError && (
          <div className="text-xs p-2">
            <div className="p-2 border rounded-md border-red-400 dark:text-red-300 text-red-700">
              {createTableError.toString()}
            </div>
          </div>
        )}

        {sql && (
          <div className="mt-4">
            <div className="flex items-center">
              <h3 className="text-lg font-semibold">Generated SQL:</h3>
              <Button
                size="icon"
                variant="link"
                onClick={() => {
                  // copy sql value to clip board
                  navigator.clipboard.writeText(sql);
                  toast.success("Statement coppied to clipboard!");
                  setStatementCopiedToClipBoard(true);
                  setTimeout(() => {
                    setStatementCopiedToClipBoard(false);
                  }, 4000);
                }}
              >
                {!statementCopiedToClipBoard ? (
                  <CopyIcon height={18} />
                ) : (
                  <CopyCheck height={18} className="text-green-400" />
                )}
              </Button>
            </div>
            <pre className="bg-primary/20 p-2 rounded mt-2 text-sm overflow-x-auto">
              {sql}
            </pre>
          </div>
        )}
      </SheetContent>
    </Sheet>
  );
};

export default CreateTable;
