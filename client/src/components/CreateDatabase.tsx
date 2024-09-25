import { useState, useEffect, useRef } from "react";
import { z } from "zod";
import { CopyIcon, CopyCheck, AlertCircle } from "lucide-react";
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
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import { toast } from "sonner";
import useTabStore from "@/stores/tabs.store";

const ENGINE_OPTIONS = [
  "Atomic",
  "Lazy",
  "MySQL",
  "PostgreSQL",
  "MaterializedMySQL",
  "MaterializedPostgreSQL",
  "Replicated",
  "SQLite",
];

const CreateDatabase = () => {
  const {
    isCreateDatabaseModalOpen,
    closeCreateDatabaseModal,
    fetchDatabaseData,
    databaseData,
    runQuery,
    addTab,
  } = useTabStore();

  // State variables for database creation
  const [databaseName, setDatabaseName] = useState("");
  const [ifNotExists, setIfNotExists] = useState(false);
  const [onCluster, setOnCluster] = useState(false);
  const [clusterName, setClusterName] = useState("");
  const [engine, setEngine] = useState("Atomic");
  const [comment, setComment] = useState("");
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
      setSql("");
      setErrors({});
      setCreateDatabaseError("");
      setStatementCopiedToClipboard(false);
    }
  }, [isCreateDatabaseModalOpen]);

  // Validate if the database name is unique unless IF NOT EXISTS is checked
  const validateDatabaseName = (name: string): boolean => {
    if (ifNotExists) return true;
    const existingDb = databaseData.find(
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
        .refine((value) => !/\s/.test(value), {
          message: "Database name cannot contain spaces",
        }),
      ifNotExists: z.boolean(),
      onCluster: z.boolean(),
      clusterName: z.string().min(1, "Cluster name is required").optional(),
      engine: z.string(),
      comment: z.string().optional(),
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
    });

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
      });

      let sqlStatement = `CREATE DATABASE `;
      if (ifNotExists) {
        sqlStatement += `IF NOT EXISTS `;
      }
      sqlStatement += `${databaseName} `;

      if (onCluster && clusterName) {
        sqlStatement += `ON CLUSTER ${clusterName} `;
      }

      if (engine) {
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
      await runQuery("", sqlStatement);
      fetchDatabaseData();
      toast.success("Database created successfully!");

      // Optionally, add a new tab or perform other actions
      addTab({
        title: databaseName,
        content: { query: "", database: databaseName, table: "" },
        type: "information",
        databaseData: [],
      });

      // Reset all fields
      setDatabaseName("");
      setIfNotExists(false);
      setOnCluster(false);
      setClusterName("");
      setEngine("Atomic");
      setComment("");
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

  // Function to handle closing the sheet with confirmation
  const handleCloseSheet = () => {
    if (
      databaseName ||
      ifNotExists ||
      onCluster ||
      clusterName ||
      engine !== "Atomic" ||
      comment
    ) {
      setIsConfirmDialogOpen(true);
    } else {
      closeCreateDatabaseModal();
    }
  };

  // Function to confirm closing and reset all fields
  const confirmClose = () => {
    setDatabaseName("");
    setIfNotExists(false);
    setOnCluster(false);
    setClusterName("");
    setEngine("Atomic");
    setComment("");
    setSql("");
    setErrors({});
    setCreateDatabaseError("");
    setStatementCopiedToClipboard(false);
    setIsConfirmDialogOpen(false);
    closeCreateDatabaseModal();
  };

  return (
    <>
      {/* Confirmation Dialog */}
      <Dialog open={isConfirmDialogOpen} onOpenChange={setIsConfirmDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Confirm Close</DialogTitle>
            <DialogDescription>
              Are you sure you want to close? All your work will be lost.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setIsConfirmDialogOpen(false)}
            >
              Cancel
            </Button>
            <Button variant="destructive" onClick={confirmClose}>
              Yes, Close
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Create Database Sheet */}
      <Sheet open={isCreateDatabaseModalOpen} onOpenChange={handleCloseSheet}>
        <SheetContent className="xl:w-[600px] sm:w-full sm:max-w-full overflow-auto">
          <SheetHeader>
            <SheetTitle>Create Database</SheetTitle>
          </SheetHeader>

          <div className="space-y-4">
            {/* Database Name */}
            <div className="space-y-2">
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

            {/* ENGINE Selector */}
            <div className="space-y-2">
              <Label htmlFor="engine-select">ENGINE</Label>
              <Select value={engine} onValueChange={setEngine}>
                <SelectTrigger id="engine-select">
                  <SelectValue placeholder="Select engine" />
                </SelectTrigger>
                <SelectContent>
                  {ENGINE_OPTIONS.map((eng) => (
                    <SelectItem key={eng} value={eng}>
                      {eng}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

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
                  loading || !databaseName || (onCluster && !clusterName)
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
                <pre className="bg-primary/20 p-2 rounded mt-2 text-sm overflow-x-auto">
                  {sql}
                </pre>
              </div>
            )}

            {/* Error Message */}
            {createDatabaseError && (
              <Alert variant="destructive" className="mt-4">
                <AlertCircle className="h-4 w-4" />
                <AlertDescription>{createDatabaseError}</AlertDescription>
              </Alert>
            )}
          </div>
        </SheetContent>
      </Sheet>
    </>
  );
};

export default CreateDatabase;
