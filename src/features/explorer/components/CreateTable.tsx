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
import ManualCreationForm from "@/features/explorer/components/ManualCreationForm";

const TIME_FIELDS = ["Date", "DateTime"];

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
    dataBaseExplorer,
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

  const [isConfirmDialogOpen, setIsConfirmDialogOpen] = useState(false);

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
    const selectedDb = dataBaseExplorer.find(
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
      comment
      // Removed: || file
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
      const response = await runQuery(sqlStatement);

      if (response.error) {
        setCreateTableError(response.error);
        setLoading(false);
        return;
      }

      fetchDatabaseInfo();
      toast.success("Table created successfully!");

      addTab({
        id: `${database}.${tableName}`,
        type: "information",
        title: `${database}.${tableName}`,
        content: { database, table: tableName },
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
        onConfirm={confirmClose}
        title={"Confirm Close"}
        description={
          "Are you sure you want to close? All your work will be lost."
        }
      />

      {/* Create Table Sheet */}
      <Sheet open={isCreateTableModalOpen} onOpenChange={handleCloseSheet}>
        <SheetContent
          onOpenAutoFocus={(e) => {
            e.preventDefault();
            // auto focus on the first input
            const firstInput = (e.target as HTMLElement)?.querySelector(
              "input"
            );
            if (firstInput) {
              firstInput.focus();
            }
          }}
          className="xl:w-[1200px] sm:w-full sm:max-w-full overflow-auto"
        >
          <SheetHeader>
            <SheetTitle>
              Create Table{" "}
              {database && tableName && (
                <span className="text-primary/50">{`"${database}.${tableName}"`}</span>
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
            databaseData={dataBaseExplorer}
          />
        </SheetContent>
      </Sheet>
    </>
  );
};

export default CreateTable;
