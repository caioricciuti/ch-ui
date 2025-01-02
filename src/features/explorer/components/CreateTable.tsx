// components/CreateTable/CreateTable.tsx
import { useState, useEffect } from "react";
import { z } from "zod";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { toast } from "sonner";
import useAppStore from "@/store";

import ConfirmationDialog from "@/components/common/ConfirmationDialog";
import ManualCreationForm from "./ManualCreationForm";
import { Field } from "./FieldManagement";

const TIME_FIELDS = ["Date", "DateTime"] as const;

// Schema for field validation
const fieldSchema = z.object({
  name: z.string().min(1, "Name is required").refine(
    (val) => !/\s/.test(val),
    "Name cannot contain spaces"
  ),
  type: z.string().min(1, "Type is required"),
  nullable: z.boolean(),
  description: z.string(),
  isPrimaryKey: z.boolean(),
  isOrderBy: z.boolean(),
  isPartitionBy: z.boolean(),
  customType: z.string().optional(),
}).refine((data) => {
  if (data.type === "Other") {
    return !!data.customType?.trim();
  }
  return true;
}, {
  message: "Custom type is required when 'Other' is selected",
  path: ["customType"],
});

const CreateTable = () => {
  const {
    isCreateTableModalOpen,
    selectedDatabaseForCreateTable,
    closeCreateTableModal,
    fetchDatabaseInfo,
    dataBaseExplorer,
    runQuery,
    addTab,
  } = useAppStore();

  // State management
  const [database, setDatabase] = useState("");
  const [tableName, setTableName] = useState("");
  const [engine, setEngine] = useState("MergeTree");
  const [fields, setFields] = useState<Field[]>([]);
  const [primaryKeyFields, setPrimaryKeyFields] = useState<string[]>([]);
  const [orderByFields, setOrderByFields] = useState<string[]>([]);
  const [partitionByField, setPartitionByField] = useState<string | null>(null);
  const [comment, setComment] = useState("");
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [createTableError, setCreateTableError] = useState<string>("");
  const [isLoading, setIsLoading] = useState(false);
  const [isConfirmDialogOpen, setIsConfirmDialogOpen] = useState(false);

  // Effect to set default database
  useEffect(() => {
    if (selectedDatabaseForCreateTable) {
      setDatabase(selectedDatabaseForCreateTable);
    }
  }, [selectedDatabaseForCreateTable]);

  // Effect to update derived states when fields change
  useEffect(() => {
    setPrimaryKeyFields(
      fields.filter((field) => field.isPrimaryKey && field.name)
        .map((field) => field.name)
    );

    setOrderByFields(
      fields.filter((field) => field.isOrderBy && field.name)
        .map((field) => field.name)
    );

    const pbField = fields.find((field) => field.isPartitionBy && field.name)?.name || null;
    setPartitionByField(pbField);
  }, [fields]);

  // Field management handlers
  const addField = () => {
    setFields([
      ...fields,
      {
        name: "",
        type: "String",
        nullable: false,
        description: "",
        isPrimaryKey: false,
        isOrderBy: false,
        isPartitionBy: false,
      },
    ]);
  };

  const removeField = (index: number) => {
    setFields(fields.filter((_, i) => i !== index));
    // Clean up any errors related to this field
    setErrors((prev) => {
      const newErrors = { ...prev };
      Object.keys(newErrors)
        .filter(key => key.startsWith(`fields.${index}`))
        .forEach(key => delete newErrors[key]);
      return newErrors;
    });
  };

  const updateField = (index: number, key: keyof Field, value: any) => {
    const updatedFields = [...fields];
    updatedFields[index] = { ...updatedFields[index], [key]: value };

    // Handle partition by field exclusivity
    if (key === "isPartitionBy" && value === true) {
      updatedFields.forEach((field, i) => {
        if (i !== index && field.isPartitionBy) {
          updatedFields[i] = { ...field, isPartitionBy: false };
        }
      });
    }

    setFields(updatedFields);
    
    // Clear field-specific errors when the field is updated
    if (errors[`fields.${index}.${key}`]) {
      setErrors(prev => {
        const newErrors = { ...prev };
        delete newErrors[`fields.${index}.${key}`];
        return newErrors;
      });
    }
  };

  // Validation helpers
  const validateTableName = (name: string): boolean => {
    const selectedDb = dataBaseExplorer.find(db => db.name === database);
    return !selectedDb?.children?.some(
      table => table.name.toLowerCase() === name.toLowerCase()
    );
  };

  // Schema for table validation
  const tableSchema = z.object({
    database: z.string().min(1, "Database is required"),
    tableName: z.string().min(1, "Table name is required")
      .refine(validateTableName, "Table name already exists in this database"),
    fields: z.array(fieldSchema)
      .min(1, "At least one field is required"),
  });

  // SQL Generation
  const validateAndGenerateSQL = (): string | null => {
    try {
      // Validate the form data
      tableSchema.parse({ database, tableName, fields });
      
      // Generate SQL
      const fieldDefinitions = fields.map(field => {
        const typeStr = field.type === "Other" ? field.customType : field.type;
        const nullableStr = field.nullable ? "NULL" : "NOT NULL";
        const commentStr = field.description ? ` COMMENT '${field.description}'` : '';
        return `${field.name} ${typeStr} ${nullableStr}${commentStr}`;
      }).join(",\n    ");

      let sql = `CREATE TABLE ${database}.${tableName}\n(\n    ${fieldDefinitions}\n) ENGINE = ${engine}`;

      if (primaryKeyFields.length > 0) {
        sql += `\nPRIMARY KEY (${primaryKeyFields.join(", ")})`;
      }

      if (orderByFields.length > 0) {
        sql += `\nORDER BY (${orderByFields.join(", ")})`;
      }

      if (partitionByField) {
        const partitionField = fields.find(f => f.name === partitionByField);
        if (partitionField && TIME_FIELDS.includes(partitionField.type as any)) {
          sql += `\nPARTITION BY toYYYYMM(${partitionByField})`;
        } else {
          sql += `\nPARTITION BY ${partitionByField}`;
        }
      }

      if (comment) {
        sql += `\nCOMMENT '${comment}'`;
      }

      setErrors({});
      return sql;
    } catch (error) {
      if (error instanceof z.ZodError) {
        const newErrors: Record<string, string> = {};
        error.errors.forEach(err => {
          newErrors[err.path.join(".")] = err.message;
        });
        setErrors(newErrors);
      } else {
        toast.error("Validation failed");
      }
      return null;
    }
  };

  // Table Creation
  const handleCreateManual = async () => {
    const sql = validateAndGenerateSQL();
    if (!sql) return;

    setIsLoading(true);
    setCreateTableError("");

    try {
      const response = await runQuery(sql);
      if (response.error) {
        setCreateTableError(response.error);
        return;
      }

      await fetchDatabaseInfo();
      toast.success("Table created successfully!");

      // Add new tab for the created table
      addTab({
        id: `${database}.${tableName}`,
        type: "information",
        title: `${database}.${tableName}`,
        content: { database, table: tableName },
      });

      resetForm();
      closeCreateTableModal();
    } catch (error: any) {
      setCreateTableError(error.toString());
    } finally {
      setIsLoading(false);
    }
  };

  // Form reset and closing
  const resetForm = () => {
    setDatabase("");
    setTableName("");
    setEngine("MergeTree");
    setFields([]);
    setPrimaryKeyFields([]);
    setOrderByFields([]);
    setPartitionByField(null);
    setComment("");
    setErrors({});
    setCreateTableError("");
  };

  const handleCloseSheet = () => {
    const hasChanges = database || 
      tableName || 
      fields.length > 0 || 
      comment || 
      engine !== "MergeTree";

    if (hasChanges) {
      setIsConfirmDialogOpen(true);
    } else {
      closeCreateTableModal();
    }
  };

  const handleConfirmClose = () => {
    resetForm();
    setIsConfirmDialogOpen(false);
    closeCreateTableModal();
  };

  return (
    <>
      <ConfirmationDialog
        isOpen={isConfirmDialogOpen}
        onClose={() => setIsConfirmDialogOpen(false)}
        onConfirm={handleConfirmClose}
        title="Confirm Close"
        description="Are you sure you want to close? All your changes will be lost."
      />

      <Sheet open={isCreateTableModalOpen} onOpenChange={handleCloseSheet}>
        <SheetContent
          className="xl:w-[1200px] sm:w-full sm:max-w-full overflow-y-auto"
          onOpenAutoFocus={(e) => {
            e.preventDefault();
            const firstInput = document.querySelector('input');
            firstInput?.focus();
          }}
        >
          <SheetHeader>
            <SheetTitle>
              Create Table
              {database && tableName && (
                <span className="text-muted-foreground ml-2">
                  {`${database}.${tableName}`}
                </span>
              )}
            </SheetTitle>
          </SheetHeader>

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
              switch (field) {
                case "database":
                  setDatabase(value);
                  break;
                case "tableName":
                  setTableName(value);
                  break;
                case "engine":
                  setEngine(value);
                  break;
                case "comment":
                  setComment(value);
                  break;
              }
            }}
            onAddField={addField}
            onRemoveField={removeField}
            onUpdateField={updateField}
            onValidateAndGenerateSQL={validateAndGenerateSQL}
            onCreateManual={handleCreateManual}
            createTableError={createTableError}
            isLoading={isLoading}
            databaseData={dataBaseExplorer}
          />
        </SheetContent>
      </Sheet>
    </>
  );
};

export default CreateTable;