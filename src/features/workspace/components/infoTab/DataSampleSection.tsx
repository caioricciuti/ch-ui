import React from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Loader2, RefreshCcw } from "lucide-react";
import useAppStore from "@/store";
import { Button } from "@/components/ui/button";
import AgTable from "@/components/common/AgTable";

interface DataSampleSectionProps {
  database: string;
  tableName: string;
}

interface QueryResult {
  meta?: any[];
  data?: any[];
  rows?: number;
  statistics?: {
    elapsed: number;
    rows_read: number;
    bytes_read: number;
  };
}

const DataSampleSection: React.FC<DataSampleSectionProps> = ({
  database,
  tableName,
}) => {
  const { runQuery } = useAppStore();
  const [sampleData, setSampleData] = React.useState<QueryResult | null>(null);
  const [sampleError, setSampleError] = React.useState<string | null>(null);
  const [loading, setLoading] = React.useState(false);
  const [isRefreshing, setIsRefreshing] = React.useState(false);

  const fetchSampleData = async (isRefresh = false) => {
    if (isRefresh) {
      setIsRefreshing(true);
    } else {
      setLoading(true);
    }
    setSampleError(null);

    try {
      const query = `
        SELECT *
        FROM \`${database}\`.\`${tableName}\`
        LIMIT 10
      `;

      const response = await runQuery(query);

      if (response && response.data && response.data.length > 0) {
        setSampleData(response);
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

  if (loading) {
    return (
      <Card>
        <CardContent className="min-h-[500px] flex items-center justify-center">
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
        <Button
          onClick={() => fetchSampleData(true)}
          variant="ghost"
          className="flex items-center space-x-2 text-sm"
          disabled={isRefreshing}
        >
          <RefreshCcw
            className={`h-4 w-4 ${isRefreshing ? "animate-spin" : ""}`}
          />
          <span>Refresh</span>
        </Button>
      </CardHeader>
      <CardContent>
        {sampleError ? (
          <Alert variant="destructive">
            <AlertTitle>Error</AlertTitle>
            <AlertDescription>{sampleError}</AlertDescription>
          </Alert>
        ) : sampleData ? (
          <div className="rounded-md border overflow-hidden" style={{ height: "400px" }}>
            <AgTable data={sampleData} />
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
