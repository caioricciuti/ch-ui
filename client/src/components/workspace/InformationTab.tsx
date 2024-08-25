import React, { useEffect, useState } from "react";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from "@/components/ui/accordion";
import { toast } from "sonner";
import api from "@/api/axios.config";
import CHUITable from "../CHUITable";

interface InformationTabProps {
  database: string;
  table?: string;
}

const InformationTab: React.FC<InformationTabProps> = ({ database, table }) => {
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [data, setData] = useState<any>({});

  useEffect(() => {
    const fetchInformation = async () => {
      try {
        setLoading(true);
        setError(null);

        const response = await api.get(
          `ch-queries/metrics?database=${database}${
            table ? `&table=${table}` : ""
          }`
        );

        if (!response.data) {
          throw new Error(`Failed to fetch data: ${response.statusText}`);
        }
        const result = await response.data;
        setData(result);
      } catch (err: any) {
        setError("Failed to load data.");
        toast.error(`Failed to load data: ${err.message || err}`);
      } finally {
        setLoading(false);
      }
    };

    fetchInformation();
  }, [database, table]);

  if (loading) {
    return (
      <div className="p-4 space-y-4">
        <Skeleton className="h-8 w-3/4" />
        <Skeleton className="h-32 w-full" />
        <Skeleton className="h-64 w-full" />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }

  if (error) {
    return <div className="p-4 text-red-500">{error}</div>;
  }

  const {
    databaseInfo = [],
    tablesInDatabase = [],
    viewsInDatabase = [],
    topColumns = [],
    tableInfo = [],
    columns = [],
    partitions = [],
    dataSample = [],
  } = data;

  const dbInfo = databaseInfo[0] || {};
  const tableData = tableInfo[0] || {};

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

  const renderDatabaseView = () => (
    <>
      <h2 className="text-2xl font-bold mb-4">Database: {database}</h2>
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4 mb-6">
        <div className="rounded-xl border bg-card text-card-foreground shadow p-4">
          <h3 className="text-sm font-medium">Engine</h3>
          <div className="text-2xl font-bold">{dbInfo.engine}</div>
        </div>
        <div className="rounded-xl border bg-card text-card-foreground shadow p-4">
          <h3 className="text-sm font-medium">Total Tables</h3>
          <div className="text-2xl font-bold">{dbInfo.tables}</div>
        </div>
        <div className="rounded-xl border bg-card text-card-foreground shadow p-4">
          <h3 className="text-sm font-medium">Total Rows</h3>
          <div className="text-2xl font-bold">
            {dbInfo.total_rows?.toLocaleString()}
          </div>
        </div>
        <div className="rounded-xl border bg-card text-card-foreground shadow p-4">
          <h3 className="text-sm font-medium">Total Size</h3>
          <div className="text-2xl font-bold">
            {formatBytes(dbInfo.total_bytes)}
          </div>
        </div>
      </div>

      <Tabs defaultValue="tables" className="w-full">
        <TabsList>
          <TabsTrigger value="tables">Tables</TabsTrigger>
          <TabsTrigger value="views">Views</TabsTrigger>
          <TabsTrigger value="columns">Top Columns</TabsTrigger>
        </TabsList>

        <TabsContent value="tables">
          <CHUITable
            result={{
              meta: [
                { name: "name", type: "String" },
                { name: "engine", type: "String" },
                { name: "total_rows", type: "UInt64" },
                { name: "total_bytes", type: "UInt64" },
              ],
              data: tablesInDatabase,
              statistics: {
                elapsed: 0,
                rows_read: tablesInDatabase.length,
                bytes_read: 0,
              },
            }}
          />
        </TabsContent>

        <TabsContent value="views">
          <CHUITable
            result={{
              meta: [
                { name: "name", type: "String" },
                { name: "engine", type: "String" },
              ],
              data: viewsInDatabase,
              statistics: {
                elapsed: 0,
                rows_read: viewsInDatabase.length,
                bytes_read: 0,
              },
            }}
          />
        </TabsContent>

        <TabsContent value="columns">
          <CHUITable
            result={{
              meta: [
                { name: "table", type: "String" },
                { name: "column_name", type: "String" },
                { name: "data_type", type: "String" },
                { name: "columns_in_table", type: "UInt32" },
              ],
              data: topColumns,
              statistics: {
                elapsed: 0,
                rows_read: topColumns.length,
                bytes_read: 0,
              },
            }}
          />
        </TabsContent>
      </Tabs>
    </>
  );

  const renderTableView = () => (
    <>
      <h2 className="text-2xl font-bold mb-4">
        Table: {tableData.name} (Database: {tableData.database})
      </h2>
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4 mb-6">
        <div className="rounded-xl border bg-card text-card-foreground shadow p-4">
          <h3 className="text-sm font-medium">Engine</h3>
          <div className="text-2xl font-bold">{tableData.engine}</div>
        </div>
        <div className="rounded-xl border bg-card text-card-foreground shadow p-4">
          <h3 className="text-sm font-medium">Total Rows</h3>
          <div className="text-2xl font-bold">
            {tableData.total_rows?.toLocaleString()}
          </div>
        </div>
        <div className="rounded-xl border bg-card text-card-foreground shadow p-4">
          <h3 className="text-sm font-medium">Total Size</h3>
          <div className="text-2xl font-bold">
            {formatBytes(tableData.total_bytes)}
          </div>
        </div>
        <div className="rounded-xl border bg-card text-card-foreground shadow p-4">
          <h3 className="text-sm font-medium">Created At</h3>
          <div className="text-2xl font-bold">
            {formatDate(tableData.metadata_modification_time)}
          </div>
        </div>
      </div>

      <Tabs defaultValue="columns" className="w-full">
        <TabsList>
          <TabsTrigger value="columns">Columns</TabsTrigger>
          <TabsTrigger value="details">Table Details</TabsTrigger>
          <TabsTrigger value="partitions">Partitions</TabsTrigger>
          <TabsTrigger value="sample">Data Sample</TabsTrigger>
        </TabsList>

        <TabsContent value="columns">
          <CHUITable
            result={{
              meta: [
                { name: "name", type: "String" },
                { name: "type", type: "String" },
                { name: "default", type: "String" },
                { name: "comment", type: "String" },
              ],
              data: columns.map((column: any) => ({
                ...column,
                default: column.default_kind
                  ? `${column.default_kind}: ${column.default_expression}`
                  : "N/A",
              })),
              statistics: {
                elapsed: 0,
                rows_read: columns.length,
                bytes_read: 0,
              },
            }}
          />
        </TabsContent>

        <TabsContent value="details">
          <div className="rounded-md border">
            <Accordion type="single" collapsible className="w-full">
              <AccordionItem value="create-query">
                <AccordionTrigger>Create Table Query</AccordionTrigger>
                <AccordionContent>
                  <pre className="bg-muted p-4 rounded-md overflow-x-auto">
                    <code>{tableData.create_table_query}</code>
                  </pre>
                </AccordionContent>
              </AccordionItem>
              <AccordionItem value="partition-key">
                <AccordionTrigger>Partition Key</AccordionTrigger>
                <AccordionContent>
                  {tableData.partition_key || "N/A"}
                </AccordionContent>
              </AccordionItem>
              <AccordionItem value="sorting-key">
                <AccordionTrigger>Sorting Key</AccordionTrigger>
                <AccordionContent>
                  {tableData.sorting_key || "N/A"}
                </AccordionContent>
              </AccordionItem>
              <AccordionItem value="primary-key">
                <AccordionTrigger>Primary Key</AccordionTrigger>
                <AccordionContent>
                  {tableData.primary_key || "N/A"}
                </AccordionContent>
              </AccordionItem>
            </Accordion>
          </div>
        </TabsContent>

        <TabsContent value="partitions">
          <CHUITable
            result={{
              meta: [
                { name: "partition", type: "String" },
                { name: "rows", type: "UInt64" },
                { name: "bytes", type: "UInt64" },
                { name: "min_time", type: "DateTime" },
                { name: "max_time", type: "DateTime" },
              ],
              data: partitions,
              statistics: {
                elapsed: 0,
                rows_read: partitions.length,
                bytes_read: 0,
              },
            }}
          />
        </TabsContent>

        <TabsContent value="sample">
          <CHUITable
            result={{
              meta: dataSample[0]
                ? Object.keys(dataSample[0]).map((key) => ({
                    name: key,
                    type: "String",
                  }))
                : [],
              data: dataSample,
              statistics: {
                elapsed: 0,
                rows_read: dataSample.length,
                bytes_read: 0,
              },
            }}
          />
        </TabsContent>
      </Tabs>
    </>
  );

  return (
    <div className="h-full w-full overflow-auto p-6">
      {table ? renderTableView() : renderDatabaseView()}
    </div>
  );
};

export default InformationTab;
