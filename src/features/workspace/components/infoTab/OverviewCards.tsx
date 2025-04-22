// OverviewCards.tsx
import React from "react";
import { HardDrive, Table, Database, Calendar } from "lucide-react";
import MetricCard from "./MetricCard";
import {
  formatBytes,
  formatDate,
  formatNumber,
} from "@/lib/utils";

interface OverviewCardsProps {
  data: any; // Flexible to handle both database and table data
  tableName?: string;
}

const OverviewCards: React.FC<OverviewCardsProps> = ({ data, tableName }) => {
  const metrics = tableName
    ? [
        {
          icon: Database,
          title: "Total Rows",
          value: formatNumber(data.total_rows),
          description: "Number of rows in the table",
        },
        {
          icon: HardDrive,
          title: "Total Size",
          value: formatBytes(data.total_bytes ?? 0),
          description: "Table size on disk",
        },
        {
          icon: Table,
          title: "Partitions",
          value: data.partition_count,
          description: "Number of partitions in the table",
        },
        {
          icon: Calendar,
          title: "Last Modified",
          value: formatDate(data.last_modified_partition),
          description: "Last modification of the table",
        },
      ]
    : [
        {
          icon: Table,
          title: "Total Tables",
          value: data.table_count,
          description: "Number of tables in the database",
        },
        {
          icon: HardDrive,
          title: "Total Size",
          value: formatBytes(data.total_bytes),
          description: "Database size on disk",
        },
        {
          icon: Database,
          title: "Total Rows",
          value: formatNumber(data.total_rows),
          description: "Total rows across all tables",
        },
        {
          icon: Calendar,
          title: "Last Modified",
          value: formatDate(data.last_modified),
          description: "Last modification of the database",
        },
      ];

  return (
    <div className="space-y-4">
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        {metrics.map((metric, index) => (
          <MetricCard key={index} {...metric} />
        ))}
      </div>
    </div>
  );
};

export default OverviewCards;
