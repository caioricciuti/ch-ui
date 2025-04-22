import React, { useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Loader2, RefreshCcw } from "lucide-react";
import useAppStore from "@/store";
import { Button } from "@/components/ui/button";
import AgTable from "@/components/common/AgTable";

interface SchemaSectionProps {
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

const SchemaSection: React.FC<SchemaSectionProps> = ({
  database,
  tableName,
}) => {
  const { runQuery } = useAppStore();
  const [schemaData, setSchemaData] = useState<QueryResult | null>(null);
  const [schemaError, setSchemaError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [isRefreshing, setIsRefreshing] = useState(false);

  const fetchSchemaData = async (isRefresh = false) => {
    if (isRefresh) {
      setIsRefreshing(true);
    } else {
      setLoading(true);
    }
    setSchemaError(null);

    try {
      const query = `DESCRIBE \`${database}\`.\`${tableName}\``;

      const response = await runQuery(query);

      if (response && response.data && response.data.length > 0) {
        setSchemaData(response);
      } else {
        setSchemaError("No schema information available for this table.");
      }
    } catch (error: any) {
      setSchemaError(error.message || "Failed to fetch schema data.");
    } finally {
      setLoading(false);
      setIsRefreshing(false);
    }
  };

  React.useEffect(() => {
    fetchSchemaData();
  }, [database, tableName]);

  if (loading) {
    return (
      <Card>
        <CardContent className="min-h-[400px] flex items-center justify-center">
          <div className="flex flex-col items-center gap-2">
            <Loader2 className="h-8 w-8 animate-spin text-primary" />
            <p className="text-sm text-muted-foreground">
              Loading schema data...
            </p>
          </div>
        </CardContent>
      </Card>
    );
  }

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between space-y-0 bg">
        <CardTitle>Schema</CardTitle>
        <Button
          onClick={() => fetchSchemaData(true)}
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
      <CardContent className="max-h-[500px] overflow-auto">
        {schemaError ? (
          <Alert variant="destructive">
            <AlertTitle>Error</AlertTitle>
            <AlertDescription>{schemaError}</AlertDescription>
          </Alert>
        ) : schemaData ? (
          <div
            className="rounded-md border overflow-hidden"
            style={{ height: "400px" }}
          >
            <AgTable data={schemaData} />
          </div>
        ) : (
          <Alert>
            <AlertTitle>No Data</AlertTitle>
            <AlertDescription>
              No schema information available for this table.
            </AlertDescription>
          </Alert>
        )}
      </CardContent>
    </Card>
  );
};

export default SchemaSection;
