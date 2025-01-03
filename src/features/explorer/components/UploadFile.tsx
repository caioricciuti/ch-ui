// components/UploadFromFile/UploadFromFile.tsx
import { useState, useEffect, useCallback } from "react";
import { z } from "zod";
import Papa from "papaparse";
import {
    Sheet,
    SheetContent,
    SheetHeader,
    SheetTitle,
} from "@/components/ui/sheet";
import { Button } from "@/components/ui/button";
import {
    Card,
    CardContent,
    CardDescription,
    CardHeader,
    CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from "@/components/ui/select";
import { toast } from "sonner";
import useAppStore from "@/store";
import ConfirmationDialog from "@/components/common/ConfirmationDialog";
import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
} from "@/components/ui/table";
import { Progress } from "@/components/ui/progress";


// Schema for file validation
const fileSchema = z.object({
    database: z.string().min(1, "Database is required"),
    tableName: z.string().min(1, "Table name is required"),
    file: z.instanceof(File).refine((file) => {
        const validTypes = ["text/csv", "application/json"];
        return validTypes.includes(file.type);
    }, "Only CSV and JSON files are supported"),
});

const UploadFromFile = () => {
    const {
        isUploadFileModalOpen,
        selectedDatabaseForUpload,
        closeUploadFileModal,
        fetchDatabaseInfo,
        dataBaseExplorer,
        runQuery,
        addTab,
    } = useAppStore();

    // State management
    const [database, setDatabase] = useState("");
    const [tableName, setTableName] = useState("");
    const [selectedFile, setSelectedFile] = useState<File | null>(null);
    const [previewData, setPreviewData] = useState<any[] | null>(null);
    const [fullData, setFullData] = useState<any[] | null>(null);
    const [columns, setColumns] = useState<string[]>([]);
    const [isLoading, setIsLoading] = useState(false);
    const [errors, setErrors] = useState<Record<string, string>>({});
    const [isConfirmDialogOpen, setIsConfirmDialogOpen] = useState(false);
    const [uploadProgress, setUploadProgress] = useState(0);
    const [totalBatches, setTotalBatches] = useState(0);
    const [completedBatches, setCompletedBatches] = useState(0);


    // Effect to set default database
    useEffect(() => {
        if (selectedDatabaseForUpload) {
            setDatabase(selectedDatabaseForUpload);
        }
    }, [selectedDatabaseForUpload]);

    // File handling
    const handleFileChange = async (e: React.ChangeEvent<HTMLInputElement>) => {
        const file = e.target.files?.[0];
        if (!file) return;

        setSelectedFile(file);
        setErrors({});
        setIsLoading(true);
        setPreviewData(null);
        setFullData(null);
        setColumns([]);
        setUploadProgress(0);
        setTotalBatches(0);
        setCompletedBatches(0);

        try {
            if (file.type === "text/csv") {
                await parseCSV(file);
            } else if (file.type === "application/json") {
                await parseJSON(file);
            }
        } catch (error) {
            toast.error("Error parsing file");
            console.error("File parsing error:", error);
        } finally {
            setIsLoading(false);
        }
    };

    const parseCSV = (file: File): Promise<void> => {
        return new Promise((resolve, reject) => {
            Papa.parse(file, {
                header: true,
                skipEmptyLines: true,
                complete: (results) => {
                    if (results.errors.length > 0) {
                        console.error("CSV parsing errors:", results.errors);
                        reject(new Error("Error parsing CSV file"));
                        return;
                    }

                    const allData = results.data as any[];
                    setFullData(allData);
                    setPreviewData(allData.slice(0, 5));
                    setColumns(results.meta.fields || []);
                    resolve();
                },
                error: (error) => {
                    toast.error(`Error parsing CSV: ${error.message}`);
                    reject(error);
                },
            });
        });
    };

    const parseJSON = async (file: File) => {
        try {
            const text = await file.text();
            const data = JSON.parse(text);
            const allData = Array.isArray(data) ? data : [data];
            const cols = new Set<string>();

            allData.forEach(item => {
                Object.keys(item).forEach(key => cols.add(key));
            });

            setFullData(allData);
            setPreviewData(allData.slice(0, 5));
            setColumns(Array.from(cols));
        } catch (error) {
            toast.error("Error parsing JSON file");
            throw error;
        }
    };

    // Escape column names for ClickHouse
    const escapeColumnName = (columnName: string): string => {
        return `\`${columnName.replace(/`/g, '``')}\``;
    };

    // Escape string values for SQL
    const escapeValue = (value: any): string => {
        if (value === null || value === undefined || value === '') {
            return 'NULL';
        }
        if (typeof value === 'number') {
            return value.toString();
        }
        if (typeof value === 'boolean') {
            return value ? '1' : '0';
        }
        return `'${value.toString().replace(/'/g, "''")}'`;
    };

    // Table creation and data upload
    const generateCreateTableSQL = (): string => {
        if (!columns.length) throw new Error("No columns detected");

        const columnDefinitions = columns.map(col =>
            `  ${escapeColumnName(col)} String NULL`
        ).join(",\n");

        return `CREATE TABLE IF NOT EXISTS ${database}.${tableName}
(
${columnDefinitions}
) ENGINE = MergeTree
ORDER BY tuple()`;
    };

    const generateInsertSQL = (data: any[]): string => {
        const escapedColumns = columns.map(col => escapeColumnName(col));

        const values = data.map(row => {
            const rowValues = columns.map(col => escapeValue(row[col]));
            return `(${rowValues.join(', ')})`;
        }).join(',\n');

        return `INSERT INTO ${database}.${tableName}
    (${escapedColumns.join(', ')})
    VALUES
    ${values}`;
    };


    // Split data into chunks for batch insertion
    const chunkArray = useCallback(<T,>(array: T[], size: number): T[][] => {
        const chunks: T[][] = [];
        for (let i = 0; i < array.length; i += size) {
            chunks.push(array.slice(i, i + size));
        }
        return chunks;
    }, []);


    const handleUpload = async () => {
        if (!selectedFile || !database || !tableName || !fullData) {
            setErrors({ form: "Please fill in all required fields and ensure file is loaded" });
            return;
        }

        setIsLoading(true);
        setUploadProgress(0);
        setCompletedBatches(0);

        try {
            // Create table
            const createTableSQL = generateCreateTableSQL();
            console.log('Create Table SQL:', createTableSQL);
            const createResult = await runQuery(createTableSQL);
            if (createResult.error) throw new Error(createResult.error);

            // Insert data in batches
            const BATCH_SIZE = 100; // Adjust based on your needs
            const dataChunks = chunkArray(fullData, BATCH_SIZE);
            setTotalBatches(dataChunks.length);
            
            for (let i = 0; i < dataChunks.length; i++) {
                const insertSQL = generateInsertSQL(dataChunks[i]);
                console.log(`Inserting batch ${i + 1} of ${dataChunks.length}`);
                const insertResult = await runQuery(insertSQL);
                if (insertResult.error) throw new Error(insertResult.error);
                 setCompletedBatches(prev => prev + 1);
                 setUploadProgress((((i + 1) / dataChunks.length) * 100));
            }
           
            // Wait a bit before fetching updated info
            await new Promise(resolve => setTimeout(resolve, 1000));
             
            await fetchDatabaseInfo();
            toast.success("Data uploaded successfully!");

             addTab({
                id: `${database}.${tableName}`,
                type: "information",
                title: `${database}.${tableName}`,
                content: { database, table: tableName },
            });

            resetForm();
            closeUploadFileModal();
        } catch (error: any) {
            console.error('Upload error:', error);
            toast.error(error.message);
        } finally {
            setIsLoading(false);
            setUploadProgress(0);
            setTotalBatches(0);
            setCompletedBatches(0);
        }
    };


    // Form reset and closing
    const resetForm = () => {
        setDatabase("");
        setTableName("");
        setSelectedFile(null);
        setPreviewData(null);
        setFullData(null);
        setColumns([]);
        setErrors({});
        setUploadProgress(0);
        setTotalBatches(0);
        setCompletedBatches(0);
    };

    const handleCloseSheet = () => {
        if (selectedFile || database || tableName) {
            setIsConfirmDialogOpen(true);
        } else {
            closeUploadFileModal();
        }
    };

    const handleConfirmClose = () => {
        resetForm();
        setIsConfirmDialogOpen(false);
        closeUploadFileModal();
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

            <Sheet open={isUploadFileModalOpen} onOpenChange={handleCloseSheet}>
                <SheetContent className="xl:w-[800px] sm:w-full sm:max-w-full overflow-y-auto">
                    <SheetHeader>
                        <SheetTitle>
                            Upload Data from File
                            {database && tableName && (
                                <span className="text-muted-foreground ml-2">
                                    {`${database}.${tableName}`}
                                </span>
                            )}
                        </SheetTitle>
                    </SheetHeader>

                    <div className="space-y-6 mt-6">
                        <div className="grid grid-cols-2 gap-4">
                            <div className="space-y-2">
                                <Label htmlFor="database">Database</Label>
                                <Select
                                    value={database}
                                    onValueChange={setDatabase}
                                >
                                    <SelectTrigger>
                                        <SelectValue placeholder="Select database" />
                                    </SelectTrigger>
                                    <SelectContent>
                                        {dataBaseExplorer.map((db) => (
                                            <SelectItem key={db.name} value={db.name}>
                                                {db.name}
                                            </SelectItem>
                                        ))}
                                    </SelectContent>
                                </Select>
                                {errors.database && (
                                    <p className="text-sm text-red-500">{errors.database}</p>
                                )}
                            </div>

                            <div className="space-y-2">
                                <Label htmlFor="tableName">Table Name</Label>
                                <Input
                                    id="tableName"
                                    value={tableName}
                                    onChange={(e) => setTableName(e.target.value)}
                                    placeholder="Enter table name"
                                />
                                {errors.tableName && (
                                    <p className="text-sm text-red-500">{errors.tableName}</p>
                                )}
                            </div>
                        </div>

                        <Card>
                            <CardHeader>
                                <CardTitle>File Upload</CardTitle>
                                <CardDescription>
                                    Upload a CSV or JSON file to import data
                                    {fullData && (
                                        <span className="block mt-1 text-sm">
                                            Total rows: {fullData.length}
                                        </span>
                                    )}
                                </CardDescription>
                            </CardHeader>
                            <CardContent>
                                <Input
                                    type="file"
                                    accept=".csv,.json"
                                    onChange={handleFileChange}
                                    className="mb-4"
                                    disabled={isLoading}
                                />
                                {uploadProgress > 0 && isLoading && (
                                    <div className="mb-4">
                                        <Progress value={uploadProgress} />
                                        <p className="text-sm text-muted-foreground mt-1">
                                            Uploading {completedBatches} of {totalBatches} batches
                                        </p>
                                    </div>
                                )}
                                {previewData && (
                                    <div className="mt-4">
                                        <h4 className="font-medium mb-2">Preview (first 5 rows):</h4>
                                        <div className="overflow-x-auto">
                                            <Table>
                                                <TableHeader>
                                                    <TableRow>
                                                        {columns.map((col) => (
                                                            <TableHead key={col}>{col}</TableHead>
                                                        ))}
                                                    </TableRow>
                                                </TableHeader>
                                                <TableBody>
                                                    {previewData.map((row, i) => (
                                                        <TableRow key={i}>
                                                            {columns.map((col) => (
                                                                <TableCell key={col}>
                                                                    {String(row[col] ?? '')}
                                                                </TableCell>
                                                            ))}
                                                        </TableRow>
                                                    ))}
                                                </TableBody>
                                            </Table>
                                        </div>
                                    </div>
                                )}
                            </CardContent>
                        </Card>

                        <div className="flex justify-end space-x-4">
                            <Button
                                variant="outline"
                                onClick={() => setIsConfirmDialogOpen(true)}
                                disabled={isLoading}
                            >
                                Cancel
                            </Button>
                            <Button
                                onClick={handleUpload}
                                disabled={isLoading || !selectedFile || !fullData || uploadProgress > 0}
                            >
                                {isLoading ? "Uploading..." : "Upload"}
                            </Button>
                        </div>
                    </div>
                </SheetContent>
            </Sheet>
        </>
    );
};

export default UploadFromFile;