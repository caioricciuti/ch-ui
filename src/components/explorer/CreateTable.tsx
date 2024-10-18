// components/CreateTable/CreateTable.tsx

import { useState, useEffect } from "react";
import { z } from "zod";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { toast } from "sonner";
import useAppStore from "@/store/appStore";

import ConfirmationDialog from "@/components/ConfirmationDialog";
import ManualCreationForm from "@/components/explorer/ManualCreationForm";
import FileUploadForm from "@/components/explorer/FileUploadForm";
const TIME_FIELDS = ["Date", "DateTime"];

const MAX_FILE_SIZE = 100 * 1024 * 1024; // 100MB in bytes
const PREVIEW_ROW_COUNT = 10; // Number of rows to preview

// Interfaces
interface Field {
  name: string;
  type: string;
  nullable: boolean;
  isPrimaryKey: boolean;
  isOrderBy: boolean;
  isPartitionBy: boolean;
}

const CreateTable = () => {
  const {
    isCreateTableModalOpen,
    selectedDatabaseForCreateTable,
    closeCreateTableModal,
    fetchDatabaseInfo,
    databaseData,
    runQuery,
    addTab,
  } = useAppStore();

  // State variables
  const [database, setDatabase] = useState("");
  const [tableName, setTableName] = useState("");
  const [engine, setEngine] = useState("MergeTree");
  const [fields, setFields] = useState<Field[]>([]);
  const [primaryKeyFields, setPrimaryKeyFields] = useState<string[]>([]);
  const [orderByFields, setOrderByFields] = useState<string[]>([]);
  const [partitionByField, setPartitionByField] = useState<string | null>(null);
  const [comment, setComment] = useState("");
  const [sql, setSql] = useState("");
  const [loading, setLoading] = useState(false);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [createTableError, setCreateTableError] = useState<string>("");
  const [statementCopiedToClipBoard, setStatementCopiedToClipBoard] =
    useState(false);

  // State variables for file upload
  const [file, setFile] = useState<File | null>(null);
  const [fileType, setFileType] = useState<"csv" | "json">("csv");
  const [uploadedFileName, setUploadedFileName] = useState("");

  // Advanced CSV options
  const [csvDelimiter, setCsvDelimiter] = useState(",");
  const [csvQuoteChar, setCsvQuoteChar] = useState('"');
  const [csvEscapeChar, setCsvEscapeChar] = useState("\\");
  const [csvHeaderRowsToSkip, setCsvHeaderRowsToSkip] = useState(0);

  // Nested JSON Handling
  const [flattenJSON, setFlattenJSON] = useState<boolean>(true);
  const [jsonNestedPaths, setJsonNestedPaths] = useState<string[]>([]);

  // State for confirmation dialog
  const [isConfirmDialogOpen, setIsConfirmDialogOpen] = useState(false);

  // State for data preview
  const [previewData, setPreviewData] = useState<any[]>([]);

  // State for processing progress
  const [isProcessing, setIsProcessing] = useState(false);

  // Effect to set default database if selectedDatabaseForCreateTable is provided
  useEffect(() => {
    if (selectedDatabaseForCreateTable) {
      setDatabase(selectedDatabaseForCreateTable);
    }
  }, [selectedDatabaseForCreateTable]);

  // Effect to update primaryKeyFields, orderByFields, and partitionByField when fields change
  useEffect(() => {
    const pkFields = fields
      .filter((field) => field.isPrimaryKey && field.name)
      .map((field) => field.name);
    setPrimaryKeyFields(pkFields);

    const obFields = fields
      .filter((field) => field.isOrderBy)
      .map((field) => field.name);
    setOrderByFields(obFields);

    const pbField = fields.find((field) => field.isPartitionBy)?.name || null;
    setPartitionByField(pbField);
  }, [fields]);

  // Function to add a new field
  const addField = () => {
    setFields([
      ...fields,
      {
        name: "",
        type: "String",
        nullable: false,
        isPrimaryKey: false,
        isOrderBy: false,
        isPartitionBy: false,
      },
    ]);
  };

  // Function to remove a field
  const removeField = (index: number) => {
    setFields(fields.filter((_, i) => i !== index));
    setErrors((prev) => {
      const newErrors = { ...prev };
      // Shift errors for fields after the removed index
      const updatedErrors: Record<string, string> = {};
      Object.keys(newErrors).forEach((key) => {
        const regex = /^fields\.(\d+)\.name$/;
        const match = key.match(regex);
        if (match) {
          const fieldIndex = parseInt(match[1], 10);
          if (fieldIndex > index) {
            updatedErrors[`fields.${fieldIndex - 1}.name`] = newErrors[key];
          } else if (fieldIndex < index) {
            updatedErrors[key] = newErrors[key];
          }
        } else {
          updatedErrors[key] = newErrors[key];
        }
      });
      return updatedErrors;
    });
  };

  // Function to update a field
  const updateField = (index: number, key: string, value: string | boolean) => {
    const updatedFields = [...fields];
    updatedFields[index] = { ...updatedFields[index], [key]: value };

    // Ensure only one Partition By field
    if (key === "isPartitionBy" && value === true) {
      updatedFields.forEach((field, i) => {
        if (i !== index && field.isPartitionBy) {
          updatedFields[i].isPartitionBy = false;
        }
      });
    }

    setFields(updatedFields);

    if (key === "name") {
      setErrors((prev) => {
        const newErrors = { ...prev };
        delete newErrors[`fields.${index}.name`];
        return newErrors;
      });
    }
  };

  // Validate if the table name is unique in the selected database
  const validateTableName = (name: string) => {
    const selectedDb = databaseData.find(
      (db: { name: string; children?: { name: string }[] }) =>
        db.name === database
    ) as { name: string; children?: { name: string }[] } | undefined;
    if (
      selectedDb &&
      selectedDb.children?.some(
        (table: { name: string }) =>
          table.name.toLowerCase() === name.toLowerCase()
      )
    ) {
      return false;
    }
    return true;
  };

  // Schema for manual table creation
  const manualTableSchema = z.object({
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
          isOrderBy: z.boolean(),
          isPartitionBy: z.boolean(),
        })
      )
      .min(1, "At least one field is required"),
  });

  // Function to infer column types based on data
  const inferColumnTypes = (headers: string[], data: any[]) => {
    const types: string[] = headers.map(() => "String"); // Default type

    headers.forEach((header, colIndex) => {
      let isNumber = true;
      let isInteger = true;
      let isDate = true;

      for (const row of data) {
        const value = row[colIndex];
        if (value === null || value === undefined || value === "") continue;

        // Check for number
        if (isNumber && isNaN(Number(value))) {
          isNumber = false;
        }

        // Check for integer
        if (isInteger && !Number.isInteger(Number(value))) {
          isInteger = false;
        }

        // Check for date
        if (isDate && isNaN(Date.parse(value))) {
          isDate = false;
        }

        // Early exit if all checks fail
        if (!isNumber && !isInteger && !isDate) {
          break;
        }
      }

      if (isInteger) {
        types[colIndex] = "Int64";
      } else if (isNumber) {
        types[colIndex] = "Float64";
      } else if (isDate) {
        types[colIndex] = "DateTime";
      } else {
        types[colIndex] = "String";
      }
    });

    return types;
  };

  // Function to flatten nested JSON objects
  const flattenObject = (
    obj: any,
    parentKey: string = "",
    result: any = {}
  ) => {
    for (const key in obj) {
      if (Object.prototype.hasOwnProperty.call(obj, key)) {
        const newKey = parentKey ? `${parentKey}.${key}` : key;
        if (
          typeof obj[key] === "object" &&
          obj[key] !== null &&
          !Array.isArray(obj[key])
        ) {
          flattenObject(obj[key], newKey, result);
        } else {
          result[newKey] = obj[key];
        }
      }
    }
    return result;
  };

  // Schema for file upload
  const uploadSchema = z.object({
    database: z.string().min(1, "Database is required"),
    tableName: z
      .string()
      .min(1, "Table name is required")
      .refine(validateTableName, {
        message: "Table name already exists in this database",
      }),
    file: z
      .instanceof(File)
      .refine(
        (f) => f.size <= MAX_FILE_SIZE,
        `File size should not exceed ${MAX_FILE_SIZE / (1024 * 1024)}MB`
      ),
    ...(fileType === "csv"
      ? {
          csvDelimiter: z
            .string()
            .min(1, "Delimiter is required")
            .max(1, "Delimiter must be a single character"),
          csvHeaderRowsToSkip: z.number().min(0, "Cannot skip negative rows"),
          csvQuoteChar: z
            .string()
            .length(1, "Quote character must be a single character"),
          csvEscapeChar: z
            .string()
            .length(1, "Escape character must be a single character"),
        }
      : {}),
    ...(fileType === "json"
      ? {
          flattenJSON: z.boolean(),
          jsonNestedPaths: z
            .array(z.string())
            .optional()
            .refine(
              (paths) => {
                if (!paths) return true;
                return paths.every(
                  (path) => typeof path === "string" && path.length > 0
                );
              },
              { message: "Each path must be a non-empty string" }
            ),
        }
      : {}),
  });

  // Function to validate and generate SQL for manual creation
  const validateAndGenerateSQL = () => {
    try {
      manualTableSchema.parse({ database, tableName, fields });

      const fieldDefinitions = fields
        .map(
          (field) =>
            `${field.name} ${field.type}${
              field.nullable ? " NULL" : " NOT NULL"
            }`
        )
        .join(",\n    ");

      let sqlStatement = `CREATE TABLE ${database}.${tableName}\n(\n    ${fieldDefinitions}\n) ENGINE = ${engine}\n`;

      if (orderByFields.length > 0) {
        sqlStatement += `ORDER BY (${orderByFields.join(", ")})\n`;
      }

      if (partitionByField) {
        const partitionField = fields.find((f) => f.name === partitionByField);
        if (partitionField && TIME_FIELDS.includes(partitionField.type)) {
          sqlStatement += `PARTITION BY toYYYYMM(${partitionByField})\n`;
        } else {
          sqlStatement += `PARTITION BY ${partitionByField}\n`;
        }
      }

      if (primaryKeyFields.length > 0) {
        sqlStatement += `PRIMARY KEY (${primaryKeyFields.join(", ")})\n`;
      }

      if (comment) {
        sqlStatement += `COMMENT '${comment}'`;
      }

      setSql(sqlStatement.trim());
      setErrors({});
      return sqlStatement.trim();
    } catch (error) {
      if (error instanceof z.ZodError) {
        const newErrors: { [key: string]: string } = {};
        error.errors.forEach((err) => {
          const path = err.path.join(".");
          newErrors[path] = err.message;
        });
        setErrors(newErrors);
      } else {
        toast.error("Unknown error occurred");
      }
      return null;
    }
  };

  // Function to handle file change
  const handleFileChange = (selectedFile: File | null) => {
    if (selectedFile) {
      if (selectedFile.size > MAX_FILE_SIZE) {
        setErrors((prev) => ({
          ...prev,
          file: "File size exceeds 100MB limit",
        }));
        return;
      }
      setFile(selectedFile);
      setUploadedFileName(selectedFile.name);
      setErrors((prev) => ({ ...prev, file: "" }));
    }
  };

  // Function to handle file type change
  const handleFileTypeChange = (value: "csv" | "json") => {
    setFileType(value);
    setFile(null);
    setUploadedFileName("");
    setErrors((prev) => ({ ...prev, file: "" }));

    // Reset advanced options when switching file types
    if (value !== "csv") {
      setCsvDelimiter(",");
      setCsvQuoteChar('"');
      setCsvEscapeChar("\\");
      setCsvHeaderRowsToSkip(0);
    }
    if (value !== "json") {
      setFlattenJSON(true);
      setJsonNestedPaths([]);
    }
  };

  // Function to generate preview data and infer fields
  const generatePreviewData = (headers: string[], data: any[]) => {
    const inferredTypes = inferColumnTypes(headers, data);
    setPreviewData(data.slice(0, PREVIEW_ROW_COUNT));
    // Update field types based on inference
    const updatedFields = headers.map((header, index) => ({
      name: header,
      type: inferredTypes[index],
      nullable: true,
      isPrimaryKey: false,
      isOrderBy: false,
      isPartitionBy: false,
    }));
    setFields(updatedFields);
  };

  // Function to handle file-based table creation
  const handleCreateFromFile = async () => {
    if (!file) {
      setErrors((prev) => ({ ...prev, file: "File is required" }));
      return;
    }

    const uploadData: any = {
      database,
      tableName,
      file,
    };

    if (fileType === "csv") {
      uploadData.csvDelimiter = csvDelimiter;
      uploadData.csvHeaderRowsToSkip = csvHeaderRowsToSkip;
      uploadData.csvQuoteChar = csvQuoteChar;
      uploadData.csvEscapeChar = csvEscapeChar;
    }

    if (fileType === "json") {
      uploadData.flattenJSON = flattenJSON;
      uploadData.jsonNestedPaths = jsonNestedPaths;
    }

    try {
      uploadSchema.parse(uploadData);
    } catch (error) {
      if (error instanceof z.ZodError) {
        const newErrors: { [key: string]: string } = {};
        error.errors.forEach((err) => {
          const path = err.path.join(".");
          newErrors[path] = err.message;
        });
        setErrors(newErrors);
      }
      return;
    }

    setLoading(true);
    setCreateTableError("");
    setIsProcessing(true);

    try {
      const reader = new FileReader();
      reader.onload = async (e) => {
        const content = e.target?.result;
        if (!content || typeof content !== "string") {
          setCreateTableError("Failed to read the file content.");
          setLoading(false);
          setIsProcessing(false);
          return;
        }

        let headers: string[] = [];
        let data: any[] = [];

        if (fileType === "csv") {
          const lines = content
            .split("\n")
            .filter((line) => line.trim() !== "");
          if (lines.length < 1) {
            setCreateTableError(
              "CSV file must have headers and at least one data row."
            );
            setLoading(false);
            setIsProcessing(false);
            return;
          }

          // Check if the CSV has headers
          const useHeaders = true; // Assuming CSV has headers
          if (useHeaders) {
            headers = lines[0]
              .split(csvDelimiter)
              .map((header) => header.trim());
            data = lines
              .slice(1 + csvHeaderRowsToSkip)
              .map((line) => line.split(csvDelimiter));
          } else {
            const columnCount = lines[0].split(csvDelimiter).length;
            headers = Array.from(
              { length: columnCount },
              (_, i) => `column_${i + 1}`
            );
            data = lines.map((line) => line.split(csvDelimiter));
          }

          // Generate preview data and infer types
          generatePreviewData(headers, data);
        } else if (fileType === "json") {
          try {
            const jsonData = JSON.parse(content);
            if (!Array.isArray(jsonData) || jsonData.length === 0) {
              setCreateTableError(
                "JSON file must contain an array of objects."
              );
              setLoading(false);
              setIsProcessing(false);
              return;
            }

            let processedData = jsonData;
            if (flattenJSON) {
              processedData = jsonData.map((item: any) => flattenObject(item));
            } else if (jsonNestedPaths.length > 0) {
              // Implement logic to extract specific nested paths
              processedData = jsonData.map((item: any) => {
                const newItem: any = {};
                jsonNestedPaths.forEach((path) => {
                  const keys = path.split(".");
                  let value = item;
                  for (const key of keys) {
                    value = value ? value[key] : undefined;
                  }
                  newItem[path] = value;
                });
                return newItem;
              });
            }

            headers = Object.keys(processedData[0]);
            data = processedData.map((obj: any) => Object.values(obj));

            // Generate preview data and infer types
            generatePreviewData(headers, data);
          } catch (jsonError) {
            setCreateTableError("Invalid JSON format.");
            setLoading(false);
            setIsProcessing(false);
            return;
          }
        }

        // Validate fields before generating SQL
        if (fields.length === 0 || fields.some((f) => !f.name || !f.type)) {
          setCreateTableError("Failed to infer table schema from the file.");
          setLoading(false);
          setIsProcessing(false);
          return;
        }

        // Generate SQL statement
        const fieldDefinitions = fields
          .map(
            (field) =>
              `${field.name} ${field.type}${
                field.nullable ? " NULL" : " NOT NULL"
              }`
          )
          .join(",\n    ");

        let sqlStatement = `CREATE TABLE ${database}.${tableName}\n(\n    ${fieldDefinitions}\n) ENGINE = ${engine}\n`;

        // For file uploads, set default ORDER BY and other clauses if not set
        if (orderByFields.length === 0) {
          sqlStatement += `ORDER BY tuple()\n`;
        } else {
          sqlStatement += `ORDER BY (${orderByFields.join(", ")})\n`;
        }

        if (partitionByField) {
          const partitionField = fields.find(
            (f) => f.name === partitionByField
          );
          if (partitionField && TIME_FIELDS.includes(partitionField.type)) {
            sqlStatement += `PARTITION BY toYYYYMM(${partitionByField})\n`;
          } else {
            sqlStatement += `PARTITION BY ${partitionByField}\n`;
          }
        }

        if (primaryKeyFields.length > 0) {
          sqlStatement += `PRIMARY KEY (${primaryKeyFields.join(", ")})\n`;
        }

        if (comment) {
          sqlStatement += `COMMENT '${comment}'`;
        }

        setSql(sqlStatement.trim());

        try {
          // Execute CREATE TABLE
          await runQuery(sqlStatement);
        } catch (createError: any) {
          setCreateTableError(createError.toString());
          setLoading(false);
          setIsProcessing(false);
          return;
        }

        // Generate INSERT statements
        let insertSQL = "";
        if (fileType === "csv") {
          insertSQL = `INSERT INTO ${database}.${tableName} FORMAT CSVWithNames\n${content}`;
        } else if (fileType === "json") {
          insertSQL = `INSERT INTO ${database}.${tableName} FORMAT JSONEachRow\n${content}`;
        }

        try {
          await runQuery(insertSQL);
          fetchDatabaseInfo();
          toast.success("Table created and data inserted successfully!");

          // Reset all fields
          resetForm();

          closeCreateTableModal();
        } catch (insertError: any) {
          setCreateTableError(insertError.toString());
        } finally {
          setLoading(false);
          setIsProcessing(false);
        }
      };

      reader.readAsText(file);
    } catch (error: any) {
      setCreateTableError(error.toString());
      setLoading(false);
      setIsProcessing(false);
    }
  };

  // Function to handle closing the sheet with confirmation
  const handleCloseSheet = () => {
    if (
      database ||
      tableName ||
      fields.some(
        (field) =>
          field.name ||
          field.type !== "String" ||
          field.nullable ||
          field.isPrimaryKey ||
          field.isOrderBy ||
          field.isPartitionBy
      ) ||
      comment ||
      file
    ) {
      setIsConfirmDialogOpen(true);
    } else {
      closeCreateTableModal();
    }
  };

  // Function to confirm closing and reset all fields
  const confirmClose = () => {
    resetForm();
    setIsConfirmDialogOpen(false);
    closeCreateTableModal();
  };

  // Function to reset the form
  const resetForm = () => {
    setDatabase("");
    setTableName("");
    setEngine("MergeTree");
    setFields([]);
    setPrimaryKeyFields([]);
    setOrderByFields([]);
    setPartitionByField(null);
    setComment("");
    setSql("");
    setFile(null);
    setUploadedFileName("");
    setCsvDelimiter(",");
    setCsvQuoteChar('"');
    setCsvEscapeChar("\\");
    setCsvHeaderRowsToSkip(0);
    setFlattenJSON(true);
    setJsonNestedPaths([]);
    setPreviewData([]);
    setErrors({});
    setCreateTableError("");
    setStatementCopiedToClipBoard(false);
  };

  // Function to handle manual table creation
  const handleCreateManual = async () => {
    const sqlStatement = validateAndGenerateSQL();
    if (!sqlStatement) return;

    setLoading(true);
    setCreateTableError("");
    try {
      await runQuery(sqlStatement);
      fetchDatabaseInfo();
      toast.success("Table created successfully!");

      addTab({
        type: "information",
        title: `${database}.${tableName}`,
        content: { database, table: tableName, query: "" },
      });

      // Reset all fields
      resetForm();

      closeCreateTableModal();
    } catch (error: any) {
      setCreateTableError(error.toString());
    } finally {
      setLoading(false);
    }
  };

  return (
    <>
      {/* Confirmation Dialog */}
      <ConfirmationDialog
        isOpen={isConfirmDialogOpen}
        onClose={() => setIsConfirmDialogOpen(false)}
        onConfirm={confirmClose} title={""} description={""}      />

      {/* Create Table Sheet */}
      <Sheet open={isCreateTableModalOpen} onOpenChange={handleCloseSheet}>
        <SheetContent className="xl:w-[1000px] sm:w-full sm:max-w-full overflow-auto">
          <SheetHeader>
            <SheetTitle>Create Table</SheetTitle>
          </SheetHeader>

          <Tabs defaultValue="manual" className="w-full mt-4">
            <TabsList className="grid w-full grid-cols-2">
              <TabsTrigger value="manual">Manual Creation</TabsTrigger>
              <TabsTrigger value="upload">File Upload</TabsTrigger>
            </TabsList>

            {/* Manual Table Creation Tab */}
            <TabsContent value="manual">
              <ManualCreationForm
                database={database}
                tableName={tableName}
                engine={engine}
                fields={fields}
                primaryKeyFields={primaryKeyFields}
                orderByFields={orderByFields}
                partitionByField={partitionByField}
                comment={comment}
                errors={errors}
                onChange={(field, value) => {
                  if (field === "database") setDatabase(value);
                  else if (field === "tableName") setTableName(value);
                  else if (field === "engine") setEngine(value);
                  else if (field === "comment") setComment(value);
                }}
                onAddField={addField}
                onRemoveField={removeField}
                onUpdateField={updateField}
                onValidateAndGenerateSQL={validateAndGenerateSQL}
                onCreateManual={handleCreateManual}
                sql={sql}
                onCopySQL={() => {
                  navigator.clipboard.writeText(sql);
                  toast.success("SQL statement copied to clipboard!");
                  setStatementCopiedToClipBoard(true);
                  setTimeout(() => {
                    setStatementCopiedToClipBoard(false);
                  }, 4000);
                }}
                createTableError={createTableError}
                statementCopiedToClipBoard={statementCopiedToClipBoard}
                fieldTypes={[]}
                databaseData={databaseData}
              />
            </TabsContent>

            {/* File Upload Tab */}
            <TabsContent value="upload">
              <FileUploadForm
                database={database}
                tableName={tableName}
                fileType={fileType}
                file={file}
                uploadedFileName={uploadedFileName}
                csvDelimiter={csvDelimiter}
                csvQuoteChar={csvQuoteChar}
                csvEscapeChar={csvEscapeChar}
                csvHeaderRowsToSkip={csvHeaderRowsToSkip}
                flattenJSON={flattenJSON}
                jsonNestedPaths={jsonNestedPaths}
                errors={errors}
                previewData={previewData}
                fields={fields} // Pass fields here
                onChange={(field, value) => {
                  if (field === "database") setDatabase(value);
                  else if (field === "tableName") setTableName(value);
                }}
                onFileChange={handleFileChange}
                onFileTypeChange={handleFileTypeChange}
                onCsvDelimiterChange={setCsvDelimiter}
                onCsvQuoteCharChange={setCsvQuoteChar}
                onCsvEscapeCharChange={setCsvEscapeChar}
                onCsvHeaderRowsToSkipChange={setCsvHeaderRowsToSkip}
                onFlattenJSONChange={setFlattenJSON}
                onJsonNestedPathsChange={setJsonNestedPaths}
                onRemoveFile={() => {
                  setFile(null);
                  setUploadedFileName("");
                  setErrors((prev) => ({ ...prev, file: "" }));
                }}
                onCreateFromFile={handleCreateFromFile}
                createTableError={createTableError}
                isProcessing={isProcessing}
                databaseData={databaseData}
              />
            </TabsContent>
          </Tabs>
        </SheetContent>
      </Sheet>
    </>
  );
};

export default CreateTable;
