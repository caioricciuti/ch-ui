// DetailsContent.tsx
import React from "react";
import { Card, CardContent, CardTitle } from "@/components/ui/card";
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";
import { AlertCircle } from "lucide-react";
import { formatBytes, formatDate, formatNumber } from "@/lib/utils";

interface DetailsContentProps {
  data: any; // Flexible for table or database details
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

  const details = tableName
    ? [
        { label: "Database", value: data.database },
        { label: "Table Name", value: data.name },
        { label: "Engine", value: data.engine },
        { label: "Total Rows", value: formatNumber(data.total_rows) },
        { label: "Total Size", value: formatBytes(data.total_bytes) },
        { label: "Lifetime Rows", value: formatNumber(data.lifetime_rows) },
        { label: "Lifetime Size", value: formatBytes(data.lifetime_bytes) },
        { label: "Partitions", value: data.partition_count },
        {
          label: "Last Modified",
          value: formatDate(data.last_modified_partition),
        },
      ]
    : [
        { label: "Database Name", value: data.name },
        { label: "Engine", value: data.engine },
        { label: "Total Tables", value: formatNumber(data.table_count) },
        { label: "Total Rows", value: formatNumber(data.total_rows) },
        { label: "Total Size", value: formatBytes(data.total_bytes) },
        { label: "Lifetime Rows", value: formatNumber(data.lifetime_rows) },
        { label: "Lifetime Size", value: formatBytes(data.lifetime_bytes) },
        { label: "Last Modified", value: formatDate(data.last_modified) },
      ];

  return (
    <div className="grid gap-4 md:grid-cols-3">
      {details.map((detail, index) => (
        <Card key={index}>
          <CardTitle className="text-base p-2">{detail.label}</CardTitle>
          <CardContent>
            <p className="font-semibold p-1 text-foreground/80">
              {detail.value}
            </p>
          </CardContent>
        </Card>
      ))}
    </div>
  );
};

export default DetailsContent;
