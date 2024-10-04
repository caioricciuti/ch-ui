import React, { useEffect, useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Progress } from "@/components/ui/progress";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import {
  Loader2,
  AlertCircle,
  Database,
  Table,
  Clock,
  HardDrive,
  Layers,
  Calendar,
} from "lucide-react";
import useAppStore from "@/store/appStore";

interface InfoTabProps {
  database: string;
  tableName?: string;
}

const InfoTab: React.FC<InfoTabProps> = ({ database, tableName }) => {
  const { runQuery } = useAppStore();
  const [loading, setLoading] = useState(false);
  const [data, setData] = useState<any>({});
  const [error, setError] = useState<string | null>(null);

  const fetchData = async () => {
    setLoading(true);
    setError(null);

    try {
      let query;
      if (tableName) {
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
            (SELECT count() FROM system.parts WHERE database = '${database}' AND table = '${tableName}') AS partition_count,
            (SELECT max(modification_time) FROM system.parts WHERE database = '${database}' AND table = '${tableName}') AS last_modified_partition
          FROM system.tables
          WHERE database = '${database}' AND name = '${tableName}'
        `;
      } else {
        query = `
          SELECT 
            name AS database,
            engine,
            (SELECT count() FROM system.tables WHERE database = '${database}') AS table_count,
            (SELECT sum(total_rows) FROM system.tables WHERE database = '${database}') AS total_rows,
            (SELECT sum(total_bytes) FROM system.tables WHERE database = '${database}') AS total_bytes,
            (SELECT sum(lifetime_rows) FROM system.tables WHERE database = '${database}') AS lifetime_rows,
            (SELECT sum(lifetime_bytes) FROM system.tables WHERE database = '${database}') AS lifetime_bytes,
            (SELECT max(metadata_modification_time) FROM system.tables WHERE database = '${database}') AS last_modified
          FROM system.databases
          WHERE name = '${database}'
        `;
      }
      const { data } = await runQuery(query);

      if (data && data.length > 0) {
        setData(data[0]);
      } else {
        throw new Error("No data found for the specified database or table.");
      }
    } catch (error: any) {
      setError(error.message);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchData();
  }, [database, tableName]);

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Loader2 className="w-8 h-8 animate-spin" />
      </div>
    );
  }

  if (error) {
    return (
      <Alert variant="destructive">
        <AlertCircle className="h-4 w-4" />
        <AlertTitle>Error</AlertTitle>
        <AlertDescription>{error}</AlertDescription>
      </Alert>
    );
  }

  const formatBytes = (bytes: number) => {
    if (bytes === 0) return "0 Bytes";
    const k = 1024;
    const sizes = ["Bytes", "KB", "MB", "GB", "TB"];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + " " + sizes[i];
  };

  const formatDate = (timestamp: number) => {
    return new Date(timestamp * 1000).toLocaleString();
  };

  return (
    <div className="container mt-8">
      <Tabs defaultValue="overview" className="w-full">
        <TabsList>
          <TabsTrigger value="overview">Overview</TabsTrigger>
          <TabsTrigger value="details">Details</TabsTrigger>
          {tableName && <TabsTrigger value="query">Create Query</TabsTrigger>}
        </TabsList>

        <TabsContent value="overview" className="space-y-6">
          <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
            <Card>
              <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
                <CardTitle className="text-sm font-medium">
                  Total Rows
                </CardTitle>
                <Database className="h-4 w-4 text-muted-foreground" />
              </CardHeader>
              <CardContent>
                <div className="text-2xl font-bold">
                  {data.total_rows?.toLocaleString()}
                </div>
              </CardContent>
            </Card>
            <Card>
              <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
                <CardTitle className="text-sm font-medium">
                  Total Size
                </CardTitle>
                <HardDrive className="h-4 w-4 text-muted-foreground" />
              </CardHeader>
              <CardContent>
                <div className="text-2xl font-bold">
                  {formatBytes(data.total_bytes)}
                </div>
              </CardContent>
            </Card>
            <Card>
              <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
                <CardTitle className="text-sm font-medium">
                  {tableName ? "Partitions" : "Tables"}
                </CardTitle>
                <Layers className="h-4 w-4 text-muted-foreground" />
              </CardHeader>
              <CardContent>
                <div className="text-2xl font-bold">
                  {tableName ? data.partition_count : data.table_count}
                </div>
              </CardContent>
            </Card>
            <Card>
              <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
                <CardTitle className="text-sm font-medium">
                  Last Modified
                </CardTitle>
                <Calendar className="h-4 w-4 text-muted-foreground" />
              </CardHeader>
              <CardContent>
                <div className="text-2xl font-bold">
                  {formatDate(
                    tableName
                      ? data.last_modified_partition
                      : data.last_modified
                  )}
                </div>
              </CardContent>
            </Card>
          </div>

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
            <CardContent>
              <div className="space-y-4">
                <div className="grid grid-cols-2 gap-4">
                  <div>
                    <p className="text-sm font-medium text-muted-foreground">
                      Database
                    </p>
                    <p className="text-lg font-semibold">{data.database}</p>
                  </div>
                  {tableName && (
                    <div>
                      <p className="text-sm font-medium text-muted-foreground">
                        Table Name
                      </p>
                      <p className="text-lg font-semibold">{data.name}</p>
                    </div>
                  )}
                  <div>
                    <p className="text-sm font-medium text-muted-foreground">
                      Engine
                    </p>
                    <p className="text-lg font-semibold">{data.engine}</p>
                  </div>
                  <div>
                    <p className="text-sm font-medium text-muted-foreground">
                      Lifetime Rows
                    </p>
                    <p className="text-lg font-semibold">
                      {data.lifetime_rows?.toLocaleString()}
                    </p>
                  </div>
                  <div>
                    <p className="text-sm font-medium text-muted-foreground">
                      Lifetime Bytes
                    </p>
                    <p className="text-lg font-semibold">
                      {formatBytes(data.lifetime_bytes)}
                    </p>
                  </div>
                  {tableName && (
                    <div>
                      <p className="text-sm font-medium text-muted-foreground">
                        Last Metadata Modification
                      </p>
                      <p className="text-lg font-semibold">
                        {formatDate(data.metadata_modification_time)}
                      </p>
                    </div>
                  )}
                </div>
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        {tableName && (
          <TabsContent value="query">
            <Card>
              <CardHeader>
                <CardTitle>Create Table Query</CardTitle>
              </CardHeader>
              <CardContent>
                <pre className="bg-muted p-4 rounded-md overflow-x-auto">
                  <code>{data.create_table_query}</code>
                </pre>
              </CardContent>
            </Card>
          </TabsContent>
        )}
      </Tabs>
    </div>
  );
};

export default InfoTab;
