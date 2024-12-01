import React, { useEffect, useState, useCallback } from "react";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import {
  RefreshCcw,
  Database,
  FileText,
  Code,
  ChevronRight,
  Table,
  AlertCircle,
} from "lucide-react";
import useAppStore from "@/store";
import LoadingOverlay from "./LoadingOverlay";
import OverviewCards from "./OverviewCards";
import DetailsContent from "./DetailsContent";
import CreateQuerySection from "./CreateQuerySection";
import DataSampleSection from "./DataSampleSection";
import SchemaSection from "./SchemaSection";

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
  last_modified_partition: Date;
}

const InfoTab: React.FC<InfoTabProps> = ({ database, tableName }) => {
  const { runQuery } = useAppStore();
  const [loading, setLoading] = useState(false);
  const [data, setData] = useState<DatabaseData | TableData | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [activeTab, setActiveTab] = useState("overview");
  const [isRefreshing, setIsRefreshing] = useState(false);

  const sanitize = (input: string): string =>
    input.replace(/[^a-zA-Z0-9_]/g, "");

  const fetchData = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const sanitizedDatabase = sanitize(database);
      let query: string;

      if (tableName) {
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
            (
              SELECT count()
              FROM system.parts
              WHERE database = '${sanitizedDatabase}'
              AND table = '${sanitizedTable}'
              AND active
            ) AS partition_count,
            (
              SELECT max(modification_time)
              FROM system.parts
              WHERE database = '${sanitizedDatabase}'
              AND table = '${sanitizedTable}'
              AND active
            ) AS last_modified_partition
          FROM system.tables
          WHERE database = '${sanitizedDatabase}'
          AND name = '${sanitizedTable}'
        `;
      } else {
        query = `
          SELECT 
            name,
            engine,
            (
              SELECT count()
              FROM system.tables
              WHERE database = '${sanitizedDatabase}'
            ) AS table_count,
            (
              SELECT sum(total_rows)
              FROM system.tables
              WHERE database = '${sanitizedDatabase}'
            ) AS total_rows,
            (
              SELECT sum(total_bytes)
              FROM system.tables
              WHERE database = '${sanitizedDatabase}'
            ) AS total_bytes,
            (
              SELECT sum(lifetime_rows)
              FROM system.tables
              WHERE database = '${sanitizedDatabase}'
            ) AS lifetime_rows,
            (
              SELECT sum(lifetime_bytes)
              FROM system.tables
              WHERE database = '${sanitizedDatabase}'
            ) AS lifetime_bytes,
            (
              SELECT max(metadata_modification_time)
              FROM system.tables
              WHERE database = '${sanitizedDatabase}'
            ) AS last_modified
          FROM system.databases
          WHERE name = '${sanitizedDatabase}'
        `;
      }

      const response = await runQuery(query);

      if (response.data?.[0]) {
        setData(response.data[0]);
      } else {
        throw new Error(
          `No data found for ${tableName ? "table" : "database"}.`
        );
      }
    } catch (error: any) {
      setError(error.message || "An unexpected error occurred.");
      setData(null);
    } finally {
      setLoading(false);
    }
  }, [database, tableName, runQuery]);

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  const refreshData = async () => {
    if (isRefreshing) return;

    setIsRefreshing(true);
    try {
      await fetchData();
    } finally {
      setIsRefreshing(false);
    }
  };

  const renderHeader = () => (
    <header className="flex items-center justify-between p-4">
      <div className="flex items-center space-x-3">
        <Database className="w-6 h-6 text-primary" />
        <div>
          <h2 className="text-2xl font-bold">
            {tableName ? tableName : database}
          </h2>
          <p className="text-sm text-muted-foreground">
            {tableName ? `Database: ${database}` : "Database Information"}
          </p>
        </div>
      </div>
      <button
        onClick={refreshData}
        className="flex items-center space-x-2 px-4 py-2 rounded-md bg-primary/10 hover:bg-primary/20 transition-all disabled:opacity-50 disabled:cursor-not-allowed"
        disabled={isRefreshing || loading}
      >
        <RefreshCcw
          className={`w-4 h-4 ${isRefreshing ? "animate-spin" : ""}`}
        />
        <span>{isRefreshing ? "Refreshing..." : "Refresh"}</span>
      </button>
    </header>
  );

  const renderTabs = () => (
    <Tabs
      value={activeTab}
      onValueChange={setActiveTab}
      className="w-full space-y-6"
    >
      <TabsList className="grid w-full grid-cols-5 gap-1">
        <TabsTrigger value="overview" className="flex items-center space-x-2">
          <FileText className="w-4 h-4" />
          <span>Overview</span>
        </TabsTrigger>
        <TabsTrigger value="details" className="flex items-center space-x-2">
          <Code className="w-4 h-4" />
          <span>Details</span>
        </TabsTrigger>
        {tableName && (
          <>
            <TabsTrigger value="query" className="flex items-center space-x-2">
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
            <TabsTrigger value="schema" className="flex items-center space-x-2">
              <Table className="w-4 h-4" />
              <span>Schema</span>
            </TabsTrigger>
          </>
        )}
      </TabsList>

      <TabsContent value="overview" className="space-y-6">
        {data && (
          <OverviewCards
            data={data as DatabaseData | TableData}
            tableName={tableName}
          />
        )}
      </TabsContent>

      <TabsContent value="details" className="space-y-6">
        {data && <DetailsContent data={data} tableName={tableName} />}
      </TabsContent>

      {tableName && (
        <TabsContent value="schema">
          <SchemaSection database={database} tableName={tableName} />
        </TabsContent>
      )}

      {tableName && data && (
        <>
          <TabsContent value="query">
            <CreateQuerySection data={data as TableData} />
          </TabsContent>
          <TabsContent value="data_sample">
            <DataSampleSection database={database} tableName={tableName} />
          </TabsContent>
        </>
      )}
    </Tabs>
  );

  return (
    <div className="space-y-6 relative min-h-[400px] p-4">
      {renderHeader()}

      {loading && <LoadingOverlay />}

      {error && !loading && (
        <Alert variant="destructive">
          <AlertCircle className="h-4 w-4" />
          <AlertTitle>Error</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}

      {data && !loading && renderTabs()}
    </div>
  );
};

export default InfoTab;
