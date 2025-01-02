import React, { useEffect, useState, useCallback } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Progress } from "@/components/ui/progress";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import CHUItable from "@/components/common/table/CHUItable";
import {
  Loader2,
  Database,
  HardDrive,
  Layers,
  Calendar,
  Table,
  RefreshCcw,
  Copy,
  ChevronRight,
  Code,
  FileText,
  LucideIcon,
} from "lucide-react";
import useAppStore from "@/store";

interface InfoTabProps {
  database: string;
  tableName?: string;
}

interface DatabaseData {
  name: string;
  engine: string;
  table_count: number;
  total_rows: number;
  total_bytes: number;
  lifetime_rows: number;
  lifetime_bytes: number;
  last_modified: string;
}

interface TableData {
  database: string;
  name: string;
  engine: string;
  total_rows: number;
  total_bytes: number;
  lifetime_rows: number;
  lifetime_bytes: number;
  metadata_modification_time: string;
  create_table_query: string;
  partition_count: number;
  last_modified_partition: string;
}

interface MetricCardProps {
  icon: LucideIcon;
  title: string;
  value: string | number;
  trend?: number;
}

const InfoTab: React.FC<InfoTabProps> = ({ database, tableName }) => {
  const { runQuery } = useAppStore();
  const [loading, setLoading] = useState(false);
  const [data, setData] = useState<DatabaseData | TableData | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [sampleLoading, setSampleLoading] = useState(false);
  const [sampleData, setSampleData] = useState<Record<string, any>[]>([]);
  const [sampleError, setSampleError] = useState<string | null>(null);
  const [activeTab, setActiveTab] = useState("overview");
  const [isRefreshing, setIsRefreshing] = useState(false);
  const [copied, setCopied] = useState(false);

  const sanitize = (input: string): string => {
    return input.replace(/[^a-zA-Z0-9_]/g, "");
  };

  const formatBytes = (bytes: number): string => {
    if (bytes === 0) return "0 Bytes";
    const k = 1024;
    const sizes = ["Bytes", "KB", "MB", "GB", "TB"];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + " " + sizes[i];
  };

  const copyToClipboard = async (text: string): Promise<void> => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch (err) {
      console.error("Failed to copy text: ", err);
    }
  };

  const fetchData = useCallback(async () => {
    setLoading(true);
    setError(null);
    setData(null);

    try {
      let query: string;
      if (tableName) {
        const sanitizedDatabase = sanitize(database);
        const sanitizedTable = sanitize(tableName);
        query = `
          SELECT 
            database,
            name,
            engine,
            total_rows,
            total_bytes,
            lifetime_rows,
            lifetime_bytes,
            metadata_modification_time,
            create_table_query,
            (SELECT count() FROM system.parts WHERE database = '${sanitizedDatabase}' AND table = '${sanitizedTable}') AS partition_count,
            (SELECT max(modification_time) FROM system.parts WHERE database = '${sanitizedDatabase}' AND table = '${sanitizedTable}') AS last_modified_partition
          FROM system.tables
          WHERE database = '${sanitizedDatabase}' AND name = '${sanitizedTable}'
        `;
      } else {
        const sanitizedDatabase = sanitize(database);
        query = `
          SELECT 
            name,
            engine,
            (SELECT count() FROM system.tables WHERE database = '${sanitizedDatabase}') AS table_count,
            (SELECT sum(total_rows) FROM system.tables WHERE database = '${sanitizedDatabase}') AS total_rows,
            (SELECT sum(total_bytes) FROM system.tables WHERE database = '${sanitizedDatabase}') AS total_bytes,
            (SELECT sum(lifetime_rows) FROM system.tables WHERE database = '${sanitizedDatabase}') AS lifetime_rows,
            (SELECT sum(lifetime_bytes) FROM system.tables WHERE database = '${sanitizedDatabase}') AS lifetime_bytes,
            (SELECT max(metadata_modification_time) FROM system.tables WHERE database = '${sanitizedDatabase}') AS last_modified
          FROM system.databases
          WHERE name = '${sanitizedDatabase}'
        `;
      }

      const response = await runQuery(query);

      if (response.data && response.data.length > 0) {
        setData(response.data[0]);
      } else {
        throw new Error("No data found for the specified database or table.");
      }
    } catch (error: any) {
      setError(error.message || "An unexpected error occurred.");
    } finally {
      setLoading(false);
    }
  }, [database, tableName, runQuery]);

  const fetchSampleData = useCallback(async () => {
    if (!tableName) return;
    setSampleLoading(true);
    setSampleError(null);
    setSampleData([]);

    try {
      const sanitizedDatabase = sanitize(database);
      const sanitizedTable = sanitize(tableName);
      const query = `
        SELECT *
        FROM ${sanitizedDatabase}.${sanitizedTable}
        LIMIT 10
      `;
      const response = await runQuery(query);

      if (response.data && response.data.length > 0) {
        setSampleData(response.data);
      } else {
        setSampleError("No sample data available.");
      }
    } catch (error: any) {
      setSampleError(error.message || "Failed to fetch sample data.");
    } finally {
      setSampleLoading(false);
    }
  }, [database, tableName, runQuery]);

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  useEffect(() => {
    if (tableName) {
      fetchSampleData();
    }
  }, [tableName, fetchSampleData]);

  const refreshData = async () => {
    setIsRefreshing(true);
    try {
      await fetchData();
      if (tableName) {
        await fetchSampleData();
      }
    } finally {
      setIsRefreshing(false);
    }
  };

  const MetricCard: React.FC<MetricCardProps> = ({
    icon: Icon,
    title,
    value,
    trend,
  }) => (
    <Card className="relative overflow-hidden group hover:shadow-lg transition-all duration-300">
      <CardHeader className="space-y-0 pb-2">
        <div className="flex items-center justify-between">
          <div className="flex items-center space-x-2">
            <Icon className="w-4 h-4 text-muted-foreground" />
            <CardTitle className="text-sm font-medium">{title}</CardTitle>
          </div>
          {trend !== undefined && (
            <span
              className={`text-xs ${
                trend > 0 ? "text-green-500" : "text-red-500"
              }`}
            >
              {trend > 0 ? "↑" : "↓"} {Math.abs(trend)}%
            </span>
          )}
        </div>
      </CardHeader>
      <CardContent>
        <div className="text-2xl font-bold tracking-tight">{value}</div>
        <div className="absolute bottom-0 left-0 w-full h-1 bg-primary/10 group-hover:bg-primary/20 transition-colors duration-300" />
      </CardContent>
    </Card>
  );

  const LoadingOverlay: React.FC = () => (
    <div className="absolute inset-0 bg-background/80 backdrop-blur-sm flex items-center justify-center z-50">
      <div className="flex flex-col items-center space-y-4">
        <Loader2 className="w-8 h-8 animate-spin text-primary" />
        <p className="text-sm text-muted-foreground">Loading data...</p>
      </div>
    </div>
  );

  const renderOverviewCards = () => {
    if (!data) return null;

    const cards = [
      {
        icon: tableName ? Database : Table,
        title: tableName ? "Total Rows" : "Total Tables",
        value: tableName
          ? (data as TableData).total_rows?.toLocaleString() ?? "0"
          : (data as DatabaseData).table_count?.toLocaleString() ?? "0",
      },
      {
        icon: HardDrive,
        title: "Total Size",
        value: formatBytes(data.total_bytes ?? 0),
      },
      {
        icon: Layers,
        title: tableName ? "Partitions" : "Total Rows",
        value: tableName
          ? (data as TableData).partition_count?.toLocaleString() ?? "0"
          : (data as DatabaseData).total_rows?.toLocaleString() ?? "0",
      },
      {
        icon: Calendar,
        title: "Last Modified",
        value: tableName
          ? (data as TableData).last_modified_partition ?? 0
          : (data as DatabaseData).last_modified ?? 0,
      },
    ];

    return (
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        {cards.map((card, index) => (
          <MetricCard key={index} {...card} />
        ))}
      </div>
    );
  };

  const renderDetailsContent = () => {
    if (!data) return null;

    const details = tableName
      ? [
          { label: "Database", value: (data as TableData).database },
          { label: "Table Name", value: (data as TableData).name },
          { label: "Engine", value: (data as TableData).engine },
          {
            label: "Total Rows",
            value: (data as TableData).total_rows?.toLocaleString(),
          },
          {
            label: "Total Size",
            value: formatBytes((data as TableData).total_bytes),
          },
          {
            label: "Lifetime Rows",
            value: (data as TableData).lifetime_rows?.toLocaleString(),
          },
          {
            label: "Lifetime Size",
            value: formatBytes((data as TableData).lifetime_bytes),
          },
          {
            label: "Partitions",
            value: (data as TableData).partition_count?.toLocaleString(),
          },
          {
            label: "Last Modified",
            value: (data as TableData).last_modified_partition,
          },
          {
            label: "Last Metadata Modification",
            value: (data as TableData).metadata_modification_time,
          },
        ]
      : [
          { label: "Database Name", value: (data as DatabaseData).name },
          { label: "Engine", value: (data as DatabaseData).engine },
          {
            label: "Total Tables",
            value: (data as DatabaseData).table_count?.toLocaleString(),
          },
          {
            label: "Total Rows",
            value: (data as DatabaseData).total_rows?.toLocaleString(),
          },
          {
            label: "Total Size",
            value: formatBytes((data as DatabaseData).total_bytes),
          },
          {
            label: "Lifetime Rows",
            value: (data as DatabaseData).lifetime_rows?.toLocaleString(),
          },
          {
            label: "Lifetime Size",
            value: formatBytes((data as DatabaseData).lifetime_bytes),
          },
          {
            label: "Last Modified",
            value: (data as DatabaseData).last_modified,
          },
        ];

    return (
      <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
        {details.map((detail, index) => (
          <Card
            key={index}
            className="hover:shadow-md transition-all duration-300"
          >
            <CardContent className="pt-6">
              <p className="text-sm font-medium text-muted-foreground mb-1">
                {detail.label}
              </p>
              <p className="text-lg font-semibold">{detail.value}</p>
            </CardContent>
          </Card>
        ))}
      </div>
    );
  };

  return (
    <div className="relative space-y-6">
      <div className="flex items-center justify-between">
        <div className="flex items-center space-x-2">
          <Database className="w-5 h-5 text-primary" />
          <h2 className="text-2xl font-bold tracking-tight">
            {tableName ? `Table: ${tableName}` : `Database: ${database}`}
          </h2>
        </div>
        <button
          onClick={refreshData}
          className="flex items-center space-x-2 px-4 py-2 rounded-md bg-primary/10 hover:bg-primary/20 transition-colors"
          disabled={isRefreshing}
        >
          <RefreshCcw
            className={`w-4 h-4 ${isRefreshing ? "animate-spin" : ""}`}
          />
          <span>Refresh</span>
        </button>
      </div>

      {loading ? (
        <LoadingOverlay />
      ) : error ? (
        <Alert variant="destructive">
          <AlertTitle>Error</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : data ? (
        <Tabs
          value={activeTab}
          onValueChange={setActiveTab}
          className="w-full space-y-6"
        >
          <TabsList className="grid w-full grid-cols-2 lg:w-auto lg:inline-grid lg:grid-cols-none">
            <TabsTrigger
              value="overview"
              className="flex items-center space-x-2"
            >
              <FileText className="w-4 h-4" />
              <span>Overview</span>
            </TabsTrigger>
            <TabsTrigger
              value="details"
              className="flex items-center space-x-2"
            >
              <Code className="w-4 h-4" />
              <span>Details</span>
            </TabsTrigger>
            {tableName && (
              <>
                <TabsTrigger
                  value="query"
                  className="flex items-center space-x-2"
                >
                  <ChevronRight className="w-4 h-4" />
                  <span>Create Query</span>
                </TabsTrigger>
                <TabsTrigger
                  value="data_sample"
                  className="flex items-center space-x-2"
                >
                  <Table className="w-4 h-4" />
                  <span>Data Sample</span>
                </TabsTrigger>
              </>
            )}
          </TabsList>

          <TabsContent value="overview" className="space-y-6">
            {renderOverviewCards()}
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center space-x-2">
                  <HardDrive className="w-4 h-4" />
                  <span>Storage Efficiency</span>
                </CardTitle>
              </CardHeader>
              <CardContent className="space-y-2">
                <Progress
                  value={(data.total_bytes / data.lifetime_bytes) * 100}
                  className="h-2"
                />
                <p className="text-sm text-muted-foreground">
                  {((data.total_bytes / data.lifetime_bytes) * 100).toFixed(2)}%
                  of lifetime data
                </p>
              </CardContent>
            </Card>
          </TabsContent>

          <TabsContent value="details" className="space-y-6">
            {renderDetailsContent()}
          </TabsContent>

          {tableName && (
            <>
              <TabsContent value="query">
                <Card>
                  <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
                    <CardTitle className="text-lg font-bold">
                      Create Table Query
                    </CardTitle>
                    <button
                      onClick={() =>
                        copyToClipboard((data as TableData).create_table_query)
                      }
                      className="flex items-center space-x-2 text-sm text-muted-foreground hover:text-primary transition-colors"
                    >
                      <Copy className="w-4 h-4" />
                      <span>{copied ? "Copied!" : "Copy"}</span>
                    </button>
                  </CardHeader>
                  <CardContent>
                    <pre className="bg-muted p-4 rounded-lg overflow-x-auto">
                      <code className="text-sm">
                        {(data as TableData).create_table_query}
                      </code>
                    </pre>
                  </CardContent>
                </Card>
              </TabsContent>

              <TabsContent value="data_sample">
                <Card>
                  <CardHeader>
                    <CardTitle>Data Sample</CardTitle>
                  </CardHeader>
                  <CardContent>
                    {sampleLoading ? (
                      <LoadingOverlay />
                    ) : sampleError ? (
                      <Alert variant="destructive">
                        <AlertTitle>Error</AlertTitle>
                        <AlertDescription>{sampleError}</AlertDescription>
                      </Alert>
                    ) : sampleData.length > 0 ? (
                      <CHUItable
                        result={{
                          meta: Object.keys(sampleData[0]).map((key) => ({
                            name: key,
                            type: typeof sampleData[0][key],
                          })),
                          data: sampleData,
                          statistics: {
                            elapsed: 0,
                            rows_read: 0,
                            bytes_read: 0,
                          },
                          message: "",
                          query_id: "",
                        }}
                      />
                    ) : (
                      <Alert>
                        <AlertTitle>No Data</AlertTitle>
                        <AlertDescription>
                          No sample data available for this table.
                        </AlertDescription>
                      </Alert>
                    )}
                  </CardContent>
                </Card>
              </TabsContent>
            </>
          )}
        </Tabs>
      ) : (
        <Alert>
          <AlertTitle>No Data</AlertTitle>
          <AlertDescription>No data available to display.</AlertDescription>
        </Alert>
      )}
    </div>
  );
};

export default InfoTab;
