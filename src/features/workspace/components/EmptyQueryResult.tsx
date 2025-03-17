import React from "react";
import { FileX2, Clock, Database, HardDrive } from "lucide-react";
import { Card, CardContent } from "@/components/ui/card";

interface QueryStatistics {
  elapsed: number;
  rows_read: number;
  bytes_read: number;
}

interface EmptyQueryResultProps {
  statistics: QueryStatistics;
}

const EmptyQueryResult: React.FC<EmptyQueryResultProps> = ({ statistics }) => {
  const formatBytes = (bytes: number) => {
    if (bytes === 0) return "0 Bytes";
    const k = 1024;
    const sizes = ["Bytes", "KB", "MB", "GB"];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return `${parseFloat((bytes / Math.pow(k, i)).toFixed(2))} ${sizes[i]}`;
  };

  const formatTime = (seconds: number) => {
    return `${(seconds * 1000).toFixed(2)}ms`;
  };

  return (
    <div className="h-full flex items-center justify-center p-8">
      <div className="max-w-md w-full">
        <div className="flex flex-col items-center mb-8">
          <h3 className="text-lg font-semibold mb-2">No Results</h3>
          <p className="text-muted-foreground text-center">
            This query executed successfully but didn't return any data.
          </p>
        </div>

        <div className="grid grid-cols-3 gap-4">
          <Card>
            <CardContent className="pt-6">
              <div className="flex flex-col items-center text-center">
                <Clock className="h-6 w-6 mb-2 text-blue-500" />
                <div className="font-medium">Execution Time</div>
                <div className="text-sm text-muted-foreground">
                  {formatTime(statistics.elapsed)}
                </div>
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardContent className="pt-6">
              <div className="flex flex-col items-center text-center">
                <Database className="h-6 w-6 mb-2 text-emerald-500" />
                <div className="font-medium">Rows Read</div>
                <div className="text-sm text-muted-foreground">
                  {statistics.rows_read.toLocaleString()}
                </div>
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardContent className="pt-6">
              <div className="flex flex-col items-center text-center">
                <HardDrive className="h-6 w-6 mb-2 text-purple-500" />
                <div className="font-medium">Data Read</div>
                <div className="text-sm text-muted-foreground">
                  {formatBytes(statistics.bytes_read)}
                </div>
              </div>
            </CardContent>
          </Card>
        </div>

        <div className="mt-8 text-center">
          <p className="text-sm text-muted-foreground">
            Try modifying your query or check the table's contents to ensure
            there's data to retrieve.
          </p>
        </div>
      </div>
    </div>
  );
};

export default EmptyQueryResult;
