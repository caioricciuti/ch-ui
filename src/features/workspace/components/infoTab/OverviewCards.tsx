import React from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Progress } from "@/components/ui/progress";
import {
  Database,
  HardDrive,
  Layers,
  Calendar,
  Table,
  ArrowUpRight,
  ArrowDownRight,
} from "lucide-react";
import MetricCard from "./MetricCard";
import { formatBytes, formatDate } from "@/lib/utils";

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

interface OverviewCardsProps {
  data: DatabaseData | TableData;
  tableName?: string;
  className?: string;
}

const OverviewCards: React.FC<OverviewCardsProps> = ({
  data,
  tableName,
  className = "",
}) => {
  const getStorageEfficiency = (total: number, lifetime: number): number => {
    if (lifetime === 0) return 0;
    return (total / lifetime) * 100;
  };

  const getMetrics = () => {
    if (tableName) {
      const tableData = data as TableData;
      return [
        {
          icon: Database,
          title: "Total Rows",
          value: tableData.total_rows,
          formatter: (v: number) => v.toLocaleString(),
          description: "Current number of rows in the table",
          trend:
            tableData.lifetime_rows > 0
              ? ((tableData.total_rows - tableData.lifetime_rows) /
                  tableData.lifetime_rows) *
                100
              : 0,
        },
        {
          icon: HardDrive,
          title: "Total Size",
          value: tableData.total_bytes,
          formatter: formatBytes,
          description: "Current size of the table on disk",
          previousValue: tableData.lifetime_bytes,
        },
        {
          icon: Layers,
          title: "Partitions",
          value: tableData.partition_count,
          formatter: (v: number) => v.toLocaleString(),
          description: "Number of partitions in the table",
        },
        {
          icon: Calendar,
          title: "Last Modified",
          value: tableData.last_modified_partition,
          formatter: formatDate,
          description: "Last time the table data was modified",
        },
      ];
    }

    const dbData = data as DatabaseData;
    return [
      {
        icon: Table,
        title: "Total Tables",
        value: dbData.table_count,
        formatter: (v: number) => v.toLocaleString(),
        description: "Number of tables in the database",
      },
      {
        icon: HardDrive,
        title: "Total Size",
        value: dbData.total_bytes,
        formatter: formatBytes,
        description: "Total size of all tables",
        previousValue: dbData.lifetime_bytes,
      },
      {
        icon: Database,
        title: "Total Rows",
        value: dbData.total_rows,
        formatter: (v: number) => v.toLocaleString(),
        description: "Total number of rows across all tables",
        trend:
          dbData.lifetime_rows > 0
            ? ((dbData.total_rows - dbData.lifetime_rows) /
                dbData.lifetime_rows) *
              100
            : 0,
      },
      {
        icon: Calendar,
        title: "Last Modified",
        value: dbData.last_modified,
        formatter: formatDate,
        description: "Last modification time across all tables",
      },
    ];
  };

  const efficiency = getStorageEfficiency(
    data.total_bytes,
    data.lifetime_bytes
  );

  const getEfficiencyColor = (value: number): string => {
    if (value >= 90) return "text-red-500";
    if (value >= 70) return "text-yellow-500";
    return "text-green-500";
  };

  return (
    <div className={`space-y-6 ${className}`}>
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        {getMetrics().map((metric, index) => (
          <MetricCard key={index} {...metric} />
        ))}
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center space-x-2 text-base">
            <HardDrive className="w-4 h-4" />
            <span>Storage Efficiency</span>
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="flex items-center justify-between">
            <div className="space-y-1">
              <p
                className={`text-xl font-bold ${getEfficiencyColor(
                  efficiency
                )}`}
              >
                {efficiency.toFixed(1)}%
              </p>
              <p className="text-sm text-muted-foreground">
                of lifetime data currently stored
              </p>
            </div>
            {efficiency > 70 && (
              <div className="text-sm text-muted-foreground bg-muted p-2 rounded-md">
                Consider optimizing storage
              </div>
            )}
          </div>
          <Progress
            value={efficiency}
            className="h-2"
            indicatorClassName={getEfficiencyColor(efficiency)}
          />
          <div className="grid grid-cols-2 gap-4 mt-4 text-sm">
            <div>
              <p className="text-muted-foreground">Current Size</p>
              <p className="font-medium">{formatBytes(data.total_bytes)}</p>
            </div>
            <div>
              <p className="text-muted-foreground">Lifetime Size</p>
              <p className="font-medium">{formatBytes(data.lifetime_bytes)}</p>
            </div>
          </div>
        </CardContent>
      </Card>
    </div>
  );
};

export default OverviewCards;
