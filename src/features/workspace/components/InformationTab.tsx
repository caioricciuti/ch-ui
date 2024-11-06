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
  last_modified: number;
}

interface TableData {
  database: string;
  name: string;
  engine: string;
  total_rows: number;
  total_bytes: number;
  lifetime_rows: number;
  lifetime_bytes: number;
  metadata_modification_time: number;
  create_table_query: string;
  partition_count: number;
  last_modified_partition: number;
}

const InfoTab: React.FC<InfoTabProps> = ({ database, tableName }) => {
  const { runQuery } = useAppStore();

  const [loading, setLoading] = useState(false);
  const [data, setData] = useState<DatabaseData | TableData | null>(null);
  const [error, setError] = useState<string | null>(null);

  const [sampleLoading, setSampleLoading] = useState(false);
  const [sampleData, setSampleData] = useState<any[]>([]);
  const [sampleError, setSampleError] = useState<string | null>(null);

  const sanitize = (input: string) => {
    return input.replace(/[^a-zA-Z0-9_]/g, "");
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
      fetchSampleData();
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
      console.log(sanitizedDatabase, sanitizedTable);
      const query = `
        SELECT *
        FROM ${sanitizedDatabase}.${sanitizedTable}
        LIMIT 20
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

  const formatBytes = (bytes: number): string => {
    if (bytes === 0) return "0 Bytes";
    const k = 1024;
    const sizes = ["Bytes", "KB", "MB", "GB", "TB"];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + " " + sizes[i];
  };

  const formatDate = (timestamp: number): string => {
    return new Date(timestamp * 1000).toLocaleString();
  };

  const renderOverviewCards = () => {
    if (!data) return null;

    const cards = [
      {
        title: tableName ? "Total Rows" : "Total Tables",
        value: tableName
          ? (data as TableData).total_rows?.toLocaleString()
          : (data as DatabaseData).table_count?.toLocaleString(),
        icon: tableName ? (
          <Database className="h-4 w-4 text-muted-foreground" />
        ) : (
          <Table className="h-4 w-4 text-muted-foreground" />
        ),
      },
      {
        title: "Total Size",
        value: formatBytes(data.total_bytes),
        icon: <HardDrive className="h-4 w-4 text-muted-foreground" />,
      },
      {
        title: tableName ? "Partitions" : "Total Rows",
        value: tableName
          ? (data as TableData).partition_count?.toLocaleString()
          : (data as DatabaseData).total_rows?.toLocaleString(),
        icon: <Layers className="h-4 w-4 text-muted-foreground" />,
      },
      {
        title: "Last Modified",
        value: formatDate(
          tableName
            ? (data as TableData).last_modified_partition
            : (data as DatabaseData).last_modified
        ),
        icon: <Calendar className="h-4 w-4 text-muted-foreground" />,
      },
    ];

    return (
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        {cards.map((card, index) => (
          <Card key={index}>
            <CardHeader className="flex items-center justify-between pb-2">
              <div className="flex space-x-2 items-center">
                {card.icon}
                <CardTitle className="text-sm font-medium">
                  {card.title}
                </CardTitle>
              </div>
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{card.value}</div>
            </CardContent>
          </Card>
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
            value: formatDate((data as TableData).last_modified_partition),
          },
          {
            label: "Last Metadata Modification",
            value: formatDate((data as TableData).metadata_modification_time),
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
            value: formatDate((data as DatabaseData).last_modified),
          },
        ];

    return (
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
        {details.map((detail, index) => (
          <div key={index}>
            <p className="text-sm font-medium text-muted-foreground">
              {detail.label}
            </p>
            <p className="text-lg font-semibold">{detail.value}</p>
          </div>
        ))}
      </div>
    );
  };

  return (
    <div className="container mt-8">
      {loading ? (
        <div className="flex items-center justify-center h-64">
          <Loader2 className="w-8 h-8 animate-spin" />
        </div>
      ) : error ? (
        <Alert variant="destructive">
          <AlertTitle>Error</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : data ? (
        <Tabs defaultValue="overview" className="w-full">
          <TabsList className="mb-4">
            <TabsTrigger value="overview">Overview</TabsTrigger>
            <TabsTrigger value="details">Details</TabsTrigger>
            {tableName && (
              <>
                <TabsTrigger value="query">Create Query</TabsTrigger>
                <TabsTrigger value="data_sample">Data Sample</TabsTrigger>
              </>
            )}
          </TabsList>

          <TabsContent value="overview" className="space-y-6">
            {renderOverviewCards()}
            <Card>
              <CardHeader>
                <CardTitle>Storage Efficiency</CardTitle>
              </CardHeader>
              <CardContent>
                <Progress
                  value={(data.total_bytes / data.lifetime_bytes) * 100}
                  className="h-2"
                />
                <p className="text-xs text-muted-foreground mt-1">
                  {((data.total_bytes / data.lifetime_bytes) * 100).toFixed(2)}%
                  of lifetime data
                </p>
              </CardContent>
            </Card>
          </TabsContent>

          <TabsContent value="details" className="space-y-6">
            <Card>
              <CardHeader>
                <CardTitle>
                  {tableName ? "Table Details" : "Database Details"}
                </CardTitle>
              </CardHeader>
              <CardContent>{renderDetailsContent()}</CardContent>
            </Card>
          </TabsContent>

          {tableName && (
            <>
              <TabsContent value="query">
                <Card>
                  <CardHeader>
                    <CardTitle>Create Table Query</CardTitle>
                  </CardHeader>
                  <CardContent>
                    <pre className="bg-muted p-4 rounded-md max-w-128 overflow-x-auto">
                      <code>{(data as TableData).create_table_query}</code>
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
                      <div className="flex items-center justify-center h-64">
                        <Loader2 className="w-8 h-8 animate-spin" />
                      </div>
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
                            bytes_read: 0
                          },
                          message: "",
                          query_id: "",
                        }}
                        initialPageSize={20}
                      />
                    ) : (
                      <Alert variant="warning">
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
        <Alert variant="destructive">
          <AlertTitle>Data Missing</AlertTitle>
          <AlertDescription>No data available to display.</AlertDescription>
        </Alert>
      )}
    </div>
  );
};

export default InfoTab;
