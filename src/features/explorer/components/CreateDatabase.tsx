import { useState, useEffect, useRef } from "react";
import { z } from "zod";
import { CopyIcon, CopyCheck, InfoIcon } from "lucide-react";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
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
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Textarea } from "@/components/ui/textarea";
import { Checkbox } from "@/components/ui/checkbox";
import { toast } from "sonner";
import useAppstore from "@/store";
import InfoDialog from "@/components/common/InfoDialog";
import ConfirmationDialog from "@/components/common/ConfirmationDialog";
import { Prism as SyntaxHighlighter } from "react-syntax-highlighter";
import { a11yDark } from "react-syntax-highlighter/dist/esm/styles/prism";

const ENGINE_OPTIONS = ["Atomic", "Lazy"];

const CreateDatabase = () => {
  const {
    isCreateDatabaseModalOpen,
    closeCreateDatabaseModal,
    fetchDatabaseInfo,
    dataBaseExplorer,
    runQuery,
    addTab,
  } = useAppstore();

  // State variables for database creation
  const [isInfoDialogOpen, setIsInfoDialogOpen] = useState(false);
  const [databaseName, setDatabaseName] = useState("");
  const [ifNotExists, setIfNotExists] = useState(false);
  const [onCluster, setOnCluster] = useState(false);
  const [clusterName, setClusterName] = useState("");
  const [engine, setEngine] = useState("Atomic");
  const [comment, setComment] = useState("");
  const [expirationTimeInSeconds, setExpirationTimeInSeconds] = useState("");
  const [sql, setSql] = useState("");
  const [loading, setLoading] = useState(false);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [createDatabaseError, setCreateDatabaseError] = useState<string>("");
  const [statementCopiedToClipboard, setStatementCopiedToClipboard] =
    useState(false);

  // State for confirmation dialog
  const [isConfirmDialogOpen, setIsConfirmDialogOpen] = useState(false);

  // Ref for SQL copy button tooltip (optional)
  const copyButtonRef = useRef<HTMLButtonElement | null>(null);

  // Effect to reset fields when modal is opened
  useEffect(() => {
    if (isCreateDatabaseModalOpen) {
      setDatabaseName("");
      setIfNotExists(false);
      setOnCluster(false);
      setClusterName("");
      setEngine("Atomic");
      setComment("");
      setExpirationTimeInSeconds("");
      setSql("");
      setErrors({});
      setCreateDatabaseError("");
      setStatementCopiedToClipboard(false);
    }
  }, [isCreateDatabaseModalOpen]);

  // Validate if the database name is unique unless IF NOT EXISTS is checked
  const validateDatabaseName = (name: string): boolean => {
    if (ifNotExists) return true;
    const existingDb = dataBaseExplorer.find(
      (db: any) => db.name.toLowerCase() === name.toLowerCase()
    );
    return !existingDb;
  };

  // Schema for database creation
  const databaseSchema = z
    .object({
      databaseName: z
        .string()
        .min(1, "Database name is required")
        .regex(
          /^[a-zA-Z0-9_]+$/,
          "Database name must contain only letters, numbers, and underscores"
        )
        .refine((value) => !/\s/.test(value), {
          message: "Database name cannot contain spaces",
        }),
      ifNotExists: z.boolean(),
      onCluster: z.boolean(),
      clusterName: z.string().min(1, "Cluster name is required").optional(),
      engine: z.string(),
      comment: z.string().optional(),
      expirationTimeInSeconds: z.preprocess((a) => {
        if (typeof a === "string" && a.trim() !== "") {
          const parsed = parseInt(a, 10);
          return isNaN(parsed) ? undefined : parsed;
        }
        return undefined;
      }, z.number().int().positive().optional()),
    })
    .refine(
      (data) => {
        if (data.onCluster) {
          return data.clusterName && data.clusterName.trim() !== "";
        }
        return true;
      },
      {
        message: "Cluster name is required when ON CLUSTER is selected",
        path: ["clusterName"],
      }
    )
    .refine((data) => validateDatabaseName(data.databaseName), {
      message: "Database name already exists",
      path: ["databaseName"],
    })
    .refine(
      (data) => {
        if (data.engine === "Lazy") {
          return typeof data.expirationTimeInSeconds === "number";
        }
        return true;
      },
      {
        message:
          "Expiration time is required and must be a positive integer when engine is Lazy",
        path: ["expirationTimeInSeconds"],
      }
    );

  // Function to validate and generate SQL
  const validateAndGenerateSQL = () => {
    try {
      databaseSchema.parse({
        databaseName,
        ifNotExists,
        onCluster,
        clusterName: onCluster ? clusterName : undefined,
        engine,
        comment,
        expirationTimeInSeconds:
          engine === "Lazy" ? expirationTimeInSeconds : undefined,
      });

      let sqlStatement = `CREATE DATABASE `;
      if (ifNotExists) {
        sqlStatement += `IF NOT EXISTS `;
      }
      sqlStatement += `${databaseName} `;

      if (onCluster && clusterName) {
        sqlStatement += `ON CLUSTER ${clusterName} `;
      }

      if (engine === "Lazy") {
        sqlStatement += `ENGINE = Lazy(${expirationTimeInSeconds}) `;
      } else if (engine) {
        sqlStatement += `ENGINE = ${engine} `;
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

  // Function to handle creating the database
  const handleCreateDatabase = async () => {
    const sqlStatement = validateAndGenerateSQL();
    if (!sqlStatement) return;

    setLoading(true);
    setCreateDatabaseError("");

    try {
      const result = await runQuery(sqlStatement);

      if (result.error) {
        setCreateDatabaseError(result.error);
        setLoading(false);
        return;
      } else {
        fetchDatabaseInfo();
        toast.success("Database created successfully!");

        // Optionally, add a new tab or perform other actions
        addTab({
          id: "database-" + databaseName,
          title: databaseName,
          type: "information",
          content: { database: databaseName, table: "" },
        });
      }

      // Reset all fields
      setDatabaseName("");
      setIfNotExists(false);
      setOnCluster(false);
      setClusterName("");
      setEngine("Atomic");
      setComment("");
      setExpirationTimeInSeconds("");
      setSql("");
      setErrors({});
      setStatementCopiedToClipboard(false);

      closeCreateDatabaseModal();
    } catch (error: any) {
      setCreateDatabaseError(error.toString());
    } finally {
      setLoading(false);
    }
  };

  // Function to handle copying SQL to clipboard
  const handleCopySQL = () => {
    navigator.clipboard.writeText(sql);
    toast.success("SQL statement copied to clipboard!");
    setStatementCopiedToClipboard(true);
    setTimeout(() => {
      setStatementCopiedToClipboard(false);
    }, 4000);
  };

  // Function to check if there are unsaved changes
  const hasUnsavedChanges = () => {
    return (
      databaseName ||
      ifNotExists ||
      onCluster ||
      clusterName ||
      engine !== "Atomic" ||
      comment ||
      expirationTimeInSeconds
    );
  };

  // Function to handle closing the sheet with confirmation
  const handleCloseSheet = () => {
    if (hasUnsavedChanges()) {
      setIsConfirmDialogOpen(true);
    } else {
      closeCreateDatabaseModal();
    }
  };

  const confirmClose = () => {
    setIsConfirmDialogOpen(false);
    closeCreateDatabaseModal();
    // Reset form fields here
    setDatabaseName("");
    setIfNotExists(false);
    setOnCluster(false);
    setClusterName("");
    setEngine("Atomic");
    setComment("");
    setExpirationTimeInSeconds("");
    setSql("");
    setErrors({});
    setStatementCopiedToClipboard(false);
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

      {/* Create Database Sheet */}
      <Sheet open={isCreateDatabaseModalOpen} onOpenChange={handleCloseSheet}>
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
          className="xl:w-[1000px] sm:w-full sm:max-w-full overflow-auto"
        >
          <SheetHeader>
            <SheetTitle>
              Create Database{" "}
              <Button
                variant="link"
                size="icon"
                onClick={() => setIsInfoDialogOpen(true)}
              >
                <InfoIcon className="w-4 h-4" />
              </Button>
            </SheetTitle>
          </SheetHeader>

          <div className="space-y-6">
            {/* Database Name */}
            <div className="grid grid-cols-2 gap-4 items-center mt-6">
              <div className="space-y-3">
                <Label htmlFor="database-name">Database Name</Label>
                <Input
                  id="database-name"
                  value={databaseName}
                  onChange={(e) => {
                    setDatabaseName(e.target.value);
                    setErrors((prev) => {
                      const newErrors = { ...prev };
                      delete newErrors.databaseName;
                      return newErrors;
                    });
                  }}
                  placeholder="Enter database name"
                  className={errors.databaseName ? "border-red-500" : ""}
                />
                {errors.databaseName && (
                  <p className="text-xs text-red-500">{errors.databaseName}</p>
                )}
              </div>
              {/* ENGINE Selector */}
              <div className="space-y-3">
                <Label htmlFor="engine-select">Engine</Label>

                <Select value={engine} onValueChange={setEngine}>
                  <SelectTrigger id="engine-select">
                    <SelectValue placeholder="Select engine" />
                  </SelectTrigger>
                  <SelectContent>
                    {ENGINE_OPTIONS.map((eng: any) => (
                      <SelectItem key={eng} value={eng}>
                        {eng}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            </div>

            {/* IF NOT EXISTS Checkbox */}
            <div className="flex items-center space-x-2">
              <Checkbox
                id="if-not-exists"
                checked={ifNotExists}
                onCheckedChange={(checked) =>
                  setIfNotExists(checked as boolean)
                }
              />
              <Label
                htmlFor="if-not-exists"
                className="text-xs text-primary/70"
              >
                IF NOT EXISTS
              </Label>
            </div>

            {/* ON CLUSTER Checkbox */}
            <div className="flex items-center space-x-2">
              <Checkbox
                id="on-cluster"
                checked={onCluster}
                onCheckedChange={(checked) => setOnCluster(checked as boolean)}
              />
              <Label htmlFor="on-cluster" className="text-xs text-primary/70">
                ON CLUSTER
              </Label>
            </div>

            {/* Cluster Name Input */}
            {onCluster && (
              <div className="space-y-2">
                <Label htmlFor="cluster-name">Cluster Name</Label>
                <Input
                  id="cluster-name"
                  value={clusterName}
                  onChange={(e) => {
                    setClusterName(e.target.value);
                    setErrors((prev) => ({ ...prev, clusterName: "" }));
                  }}
                  placeholder="Enter cluster name"
                  className={errors.clusterName ? "border-red-500" : ""}
                />
                {errors.clusterName && (
                  <p className="text-xs text-red-500">{errors.clusterName}</p>
                )}
              </div>
            )}

            {/* Expiration Time Input (Conditional) */}
            {engine === "Lazy" && (
              <div className="space-y-2">
                <Label htmlFor="expiration-time">
                  Expiration Time (seconds)
                </Label>
                <Input
                  id="expiration-time"
                  type="number"
                  value={expirationTimeInSeconds}
                  onChange={(e) => {
                    setExpirationTimeInSeconds(e.target.value);
                    setErrors((prev) => {
                      const newErrors = { ...prev };
                      delete newErrors.expirationTimeInSeconds;
                      return newErrors;
                    });
                  }}
                  placeholder="Enter expiration time in seconds"
                  className={
                    errors.expirationTimeInSeconds ? "border-red-500" : ""
                  }
                />
                {errors.expirationTimeInSeconds && (
                  <p className="text-xs text-red-500">
                    {errors.expirationTimeInSeconds}
                  </p>
                )}
              </div>
            )}

            {/* COMMENT Textarea */}
            <div className="space-y-2">
              <Label htmlFor="comment">Comment</Label>
              <Textarea
                id="comment"
                placeholder="Enter comment (optional)"
                value={comment}
                onChange={(e) => setComment(e.target.value)}
              />
            </div>

            {/* Action Buttons */}
            <div className="flex gap-4">
              <Button
                onClick={validateAndGenerateSQL}
                variant="outline"
                disabled={!databaseName}
              >
                Show Statement
              </Button>

              <Button
                onClick={handleCreateDatabase}
                disabled={
                  loading ||
                  !databaseName ||
                  (onCluster && !clusterName) ||
                  (engine === "Lazy" && !expirationTimeInSeconds)
                }
              >
                {loading ? "Creating..." : "Create Database"}
              </Button>
            </div>

            {/* Generated SQL */}
            {sql && (
              <div className="mt-4">
                <div className="flex items-center">
                  <h3 className="text-lg font-semibold">Generated SQL:</h3>
                  <Button
                    size="icon"
                    variant="link"
                    onClick={handleCopySQL}
                    className="ml-2"
                    ref={copyButtonRef}
                  >
                    {!statementCopiedToClipboard ? (
                      <CopyIcon height={18} />
                    ) : (
                      <CopyCheck height={18} className="text-green-400" />
                    )}
                  </Button>
                </div>

                <SyntaxHighlighter
                  language="sql"
                  style={a11yDark}
                  customStyle={{
                    padding: "1rem",
                    borderRadius: "0.5rem",

                    overflowX: "auto",
                  }}
                  showLineNumbers
                  wrapLines
                >
                  {sql}
                </SyntaxHighlighter>
              </div>
            )}

            {/* Error Message */}
            {createDatabaseError && (
              <Alert variant="destructive" className="mt-4">
                <AlertDescription>{createDatabaseError}</AlertDescription>
              </Alert>
            )}
          </div>

          {/* Add the InfoDialog component here */}
          <InfoDialog
            title="Database Engines"
            variant="info"
            isOpen={isInfoDialogOpen}
            onClose={() => setIsInfoDialogOpen(false)}
            link="https://clickhouse.com/docs/en/engines/database-engines/?utm_source=ch-ui&utm_medium=create-database-info"
          >
            <>
              <p className="text-sm ">
                ClickHouse offers different database engines optimized for
                various use cases. The choice of engine can affect performance,
                data storage, and replication behavior.
              </p>
              <p className="text-xs mt-3">
                CH-UI supports only Atomic and Lazy engines. Using Atomic is
                recommended for most use cases.
              </p>
            </>
          </InfoDialog>
        </SheetContent>
      </Sheet>
    </>
  );
};

export default CreateDatabase;
