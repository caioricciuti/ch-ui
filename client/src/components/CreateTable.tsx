import { useState, useEffect } from "react";
import { z } from "zod";
import { Check, ChevronsUpDown, X, Trash2, Plus, Info } from "lucide-react";
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

// Validation Schema using Zod
const tableSchema = z.object({
  database: z.string().nonempty("Database is required"),
  tableName: z.string().nonempty("Table name is required"),
  fields: z
    .array(
      z.object({
        name: z.string().nonempty("Field name is required"),
        type: z.string(),
        nullable: z.boolean(),
        isPrimaryKey: z.boolean(),
      })
    )
    .min(1, "At least one field is required"),
});

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

const DATABASES = [
  { value: "default", label: "Default" },
  { value: "system", label: "System" },
  { value: "information_schema", label: "Information Schema" },
  { value: "your_custom_db", label: "Your Custom DB" },
];

const DEFAULT_PARTITION_OPTIONS = ["toYYYYMM", "toYYYYMMDD", "toYear"];

const CreateTable = ({ runQuery, fetchDatabaseData }) => {
  const [open, setOpen] = useState(false);
  const [database, setDatabase] = useState("");
  const [tableName, setTableName] = useState("");
  const [engine, setEngine] = useState("MergeTree");
  const [fields, setFields] = useState([
    { name: "", type: "String", nullable: false, isPrimaryKey: false },
  ]);
  const [primaryKeyFields, setPrimaryKeyFields] = useState([]);
  const [orderBy, setOrderBy] = useState("");
  const [partitionBy, setPartitionBy] = useState("");
  const [comment, setComment] = useState("");
  const [sql, setSql] = useState("");
  const [loading, setLoading] = useState(false);

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

  const removeField = (index) => {
    setFields(fields.filter((_, i) => i !== index));
  };

  const updateField = (index, key, value) => {
    const updatedFields = [...fields];
    updatedFields[index][key] = value;
    setFields(updatedFields);
  };

  const validateAndGenerateSQL = async () => {
    try {
      // Validate using Zod schema
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
      return true;
    } catch (error) {
      // Show Zod validation errors
      if (error instanceof z.ZodError) {
        toast.error(error.errors.map((err) => err.message).join(", "));
      } else {
        toast.error("Unknown error occurred");
      }
      return false;
    }
  };

  const handleCreate = async () => {
    if (!(await validateAndGenerateSQL())) return;

    setLoading(true);
    try {
      await runQuery("", sql);
      fetchDatabaseData();
      toast.success("Table created successfully!");
    } catch (error) {
      toast.error("Failed to create table");
    } finally {
      setLoading(false);
    }
  };

  const handleShowStatement = async () => {
    await validateAndGenerateSQL();
  };

  const handlePrimaryKeyToggle = (index) => {
    const updatedFields = fields.map((field, i) =>
      i === index ? { ...field, isPrimaryKey: !field.isPrimaryKey } : field
    );
    setFields(updatedFields);
  };

  const removePrimaryKeyField = (fieldName) => {
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
          {/* First row: Database, Table Name, Engine */}
          <div className="flex space-x-4">
            <Popover open={open} onOpenChange={setOpen}>
              <PopoverTrigger asChild>
                <Button
                  variant="outline"
                  role="combobox"
                  aria-expanded={open}
                  className="w-full justify-between"
                >
                  {database
                    ? DATABASES.find((db) => db.value === database)?.label
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
                      {DATABASES.map((db) => (
                        <CommandItem
                          key={db.value}
                          value={db.value}
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
                      ))}
                    </CommandGroup>
                  </CommandList>
                </Command>
              </PopoverContent>
            </Popover>

            <Input
              placeholder="Table Name"
              value={tableName}
              onChange={(e) => setTableName(e.target.value)}
            />

            <Select value={engine} onValueChange={setEngine}>
              <SelectTrigger>
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

          <div>
            <p className="text-lg font-semibold">Table Schema:</p>
          </div>

          {fields.map((field, index) => (
            <div key={index} className="flex space-x-2 items-center">
              <Input
                placeholder="Field Name"
                value={field.name}
                onChange={(e) => updateField(index, "name", e.target.value)}
              />
              <Select
                value={field.type}
                onValueChange={(value) => updateField(index, "type", value)}
              >
                <SelectTrigger className="w-[350px]">
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
              <Select
                value={field.nullable ? "NULL" : "NOT NULL"}
                onValueChange={(value) =>
                  updateField(index, "nullable", value === "NULL")
                }
              >
                <SelectTrigger className="w-[350px]">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="NULL">NULL</SelectItem>
                  <SelectItem value="NOT NULL">NOT NULL</SelectItem>
                </SelectContent>
              </Select>

              <div className="flex items-center space-x-1">
                <Checkbox
                  id="pk-check-box"
                  checked={field.isPrimaryKey}
                  onCheckedChange={() => handlePrimaryKeyToggle(index)}
                  disabled={!field.name}
                  aria-label="Primary Key"
                />
                <Label
                  htmlFor="pk-check-box"
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
              </div>

              <Button
                variant="link"
                size="icon"
                onClick={() => removeField(index)}
              >
                <Trash2 className="h-4 w-4 text-red-500" />
              </Button>
            </div>
          ))}

          <Button onClick={addField} size="sm" className="w-full bg-green-600">
            <Plus className="mr-2 h-4 w-4" /> Add Column
          </Button>

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

                {/* Clear Button */}
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

                {/* Clear Button */}
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

          {/* Primary Key Badges */}
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

        {sql && (
          <div className="mt-4">
            <h3 className="text-lg font-semibold">Generated SQL:</h3>
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
