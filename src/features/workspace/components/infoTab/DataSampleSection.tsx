import React from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import CHUItable from "@/components/common/table/CHUItable";
import { Loader2, RefreshCcw } from "lucide-react";
import useAppStore from "@/store";

interface DataSampleSectionProps {
  database: string;
  tableName: string;
}

interface QueryResult {
  data: Record<string, any>[];
  statistics: {
    elapsed: number;
    rows_read: number;
    bytes_read: number;
  };
  message: string;
  query_id: string;
}

const DataSampleSection: React.FC<DataSampleSectionProps> = ({
  database,
  tableName,
}) => {
  const { runQuery } = useAppStore();
  const [sampleData, setSampleData] = React.useState<Record<string, any>[]>([]);
  const [sampleError, setSampleError] = React.useState<string | null>(null);
  const [loading, setLoading] = React.useState(false);
  const [isRefreshing, setIsRefreshing] = React.useState(false);

  const sanitize = (input: string): string =>
    input.replace(/[^a-zA-Z0-9_]/g, "");

  const fetchSampleData = async (isRefresh = false) => {
    if (isRefresh) {
      setIsRefreshing(true);
    } else {
      setLoading(true);
    }
    setSampleError(null);

    try {
      const sanitizedDatabase = sanitize(database);
      const sanitizedTable = sanitize(tableName);

      const query = `
        SELECT *
        FROM ${sanitizedDatabase}.${sanitizedTable}
        LIMIT 10
      `;

      const response = (await runQuery(query)) as QueryResult;

      if (response.data && response.data.length > 0) {
        setSampleData(response.data);
      } else {
        setSampleError("No sample data available for this table.");
      }
    } catch (error: any) {
      setSampleError(error.message || "Failed to fetch sample data.");
    } finally {
      setLoading(false);
      setIsRefreshing(false);
    }
  };

  React.useEffect(() => {
    fetchSampleData();
  }, [database, tableName]);

  const getTableMeta = (data: Record<string, any>[]) => {
    if (!data.length) return [];
    return Object.entries(data[0]).map(([key, value]) => ({
      name: key,
      type: Array.isArray(value)
        ? "array"
        : value === null
        ? "null"
        : typeof value,
    }));
  };

  if (loading) {
    return (
      <Card>
        <CardContent className="min-h-[400px] flex items-center justify-center">
          <div className="flex flex-col items-center gap-2">
            <Loader2 className="h-8 w-8 animate-spin text-primary" />
            <p className="text-sm text-muted-foreground">
              Loading sample data...
            </p>
          </div>
        </CardContent>
      </Card>
    );
  }

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between space-y-0">
        <CardTitle>Data Sample</CardTitle>
        <button
          onClick={() => fetchSampleData(true)}
          className="flex items-center space-x-2 text-sm text-muted-foreground hover:text-primary transition-colors"
          disabled={isRefreshing}
        >
          <RefreshCcw
            className={`h-4 w-4 ${isRefreshing ? "animate-spin" : ""}`}
          />
          <span>Refresh</span>
        </button>
      </CardHeader>
      <CardContent>
        {sampleError ? (
          <Alert variant="destructive">
            <AlertTitle>Error</AlertTitle>
            <AlertDescription>{sampleError}</AlertDescription>
          </Alert>
        ) : sampleData.length > 0 ? (
          <div className="rounded-md border">
            <CHUItable
              result={{
                meta: getTableMeta(sampleData),
                data: sampleData,
                statistics: {
                  elapsed: 0,
                  rows_read: sampleData.length,
                  bytes_read: 0,
                },
                message: "",
                query_id: "",
              }}
            />
          </div>
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
  );
};

export default DataSampleSection;
