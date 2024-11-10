import React from "react";
import { Card, CardContent } from "@/components/ui/card";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { formatBytes, formatDate } from "@/lib/utils";
import { AlertCircle } from "lucide-react";

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
  partition_count: number;
  create_table_query: string;
  last_modified_partition: number;
  metadata_modification_time: number;
}

interface DetailItem {
  label: string;
  value: string | number | null;
  description?: string;
  formatter?: (value: any) => string;
}

interface DetailsContentProps {
  data: DatabaseData | TableData;
  tableName?: string;
}

const DetailsContent: React.FC<DetailsContentProps> = ({ data, tableName }) => {
  if (!data) {
    return (
      <Alert>
        <AlertCircle className="h-4 w-4" />
        <AlertTitle>No Data</AlertTitle>
        <AlertDescription>No details available to display.</AlertDescription>
      </Alert>
    );
  }

  const getTableDetails = (data: TableData): DetailItem[] => [
    {
      label: "Database",
      value: data.database,
      description: "The database this table belongs to",
    },
    {
      label: "Table Name",
      value: data.name,
      description: "Name of the table",
    },
    {
      label: "Engine",
      value: data.engine,
      description: "Storage engine used by the table",
    },
    {
      label: "Total Rows",
      value: data.total_rows,
      formatter: (v) => v?.toLocaleString() ?? "0",
      description: "Current number of rows in the table",
    },
    {
      label: "Total Size",
      value: data.total_bytes,
      formatter: formatBytes,
      description: "Current size of the table",
    },
    {
      label: "Lifetime Rows",
      value: data.lifetime_rows,
      formatter: (v) => v?.toLocaleString() ?? "0",
      description: "Total number of rows ever stored in the table",
    },
    {
      label: "Lifetime Size",
      value: data.lifetime_bytes,
      formatter: formatBytes,
      description: "Total size of all data ever stored",
    },
    {
      label: "Partitions",
      value: data.partition_count,
      formatter: (v) => v?.toLocaleString() ?? "0",
      description: "Number of partitions in the table",
    },
    {
      label: "Last Modified",
      value: data.last_modified_partition,
      formatter: formatDate,
      description: "Last time the table data was modified",
    },
    {
      label: "Last Metadata Modification",
      value: data.metadata_modification_time,
      formatter: formatDate,
      description: "Last time the table structure was modified",
    },
  ];

  const getDatabaseDetails = (data: DatabaseData): DetailItem[] => [
    {
      label: "Database Name",
      value: data.name,
      description: "Name of the database",
    },
    {
      label: "Engine",
      value: data.engine,
      description: "Default storage engine",
    },
    {
      label: "Total Tables",
      value: data.table_count,
      formatter: (v) => v?.toLocaleString() ?? "0",
      description: "Number of tables in the database",
    },
    {
      label: "Total Rows",
      value: data.total_rows,
      formatter: (v) => v?.toLocaleString() ?? "0",
      description: "Total rows across all tables",
    },
    {
      label: "Total Size",
      value: data.total_bytes,
      formatter: formatBytes,
      description: "Total size of all tables",
    },
    {
      label: "Lifetime Rows",
      value: data.lifetime_rows,
      formatter: (v) => v?.toLocaleString() ?? "0",
      description: "Total rows ever stored in the database",
    },
    {
      label: "Lifetime Size",
      value: data.lifetime_bytes,
      formatter: formatBytes,
      description: "Total size of all data ever stored",
    },
    {
      label: "Last Modified",
      value: data.last_modified,
      formatter: formatDate,
      description: "Last modification time",
    },
  ];

  const details = tableName
    ? getTableDetails(data as TableData)
    : getDatabaseDetails(data as DatabaseData);

  return (
    <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
      {details.map((detail, index) => (
        <Card
          key={index}
          className="hover:shadow-md transition-all duration-300 group"
        >
          <CardContent className="pt-6">
            <div className="space-y-2">
              <div className="flex items-center justify-between">
                <p className="text-sm font-medium text-muted-foreground">
                  {detail.label}
                </p>
                {detail.description && (
                  <div className="hidden group-hover:block text-xs text-muted-foreground">
                    {detail.description}
                  </div>
                )}
              </div>
              <p className="text-lg font-semibold">
                {detail.formatter
                  ? detail.formatter(detail.value)
                  : detail.value?.toString() || "N/A"}
              </p>
            </div>
          </CardContent>
        </Card>
      ))}
    </div>
  );
};

export default DetailsContent;
