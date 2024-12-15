import React from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import CHUItable from "@/components/common/table/CHUItable";
import { AlertCircle, Loader2, RefreshCcw } from "lucide-react";
import useAppStore from "@/store";

interface SchemaSectionProps {
  database: string;
  tableName: string;
}

interface SchemaResult {
  name: string;
  type: string;
  default_type: string;
  default_expression: string;
  comment: string;
  codec_expression: string;
  ttl_expression: string;
}

const SchemaSection: React.FC<SchemaSectionProps> = ({ database, tableName }) => {
  const { runQuery } = useAppStore();
  const [schemaData, setSchemaData] = React.useState<SchemaResult[]>([]);
  const [schemaError, setSchemaError] = React.useState<string | null>(null);
  const [loading, setLoading] = React.useState(false);
  const [isRefreshing, setIsRefreshing] = React.useState(false);

  const sanitize = (input: string): string =>
    input.replace(/[^a-zA-Z0-9_]/g, "");

  const fetchSchemaData = async (isRefresh = false) => {
    if (isRefresh) {
      setIsRefreshing(true);
    } else {
      setLoading(true);
    }
    setSchemaError(null);

    try {
      const sanitizedDatabase = sanitize(database);
      const sanitizedTable = sanitize(tableName);

      const query = `DESCRIBE ${sanitizedDatabase}.${sanitizedTable}`;

      const response = (await runQuery(query)) as { data: SchemaResult[] };

      if (response.data && response.data.length > 0) {
        setSchemaData(response.data);
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

  const getTableMeta = (data: SchemaResult[]) => {
    if (!data.length) return [];
    return Object.keys(data[0]).map((key) => ({
      name: key,
      type: "string",
    }));
  };

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
      <CardHeader className="flex flex-row items-center justify-between space-y-0">
        <CardTitle>Schema</CardTitle>
        <button
          onClick={() => fetchSchemaData(true)}
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
        {schemaError ? (
          <Alert variant="destructive">
            <AlertCircle className="h-4 w-4" />
            <AlertTitle>Error</AlertTitle>
            <AlertDescription>{schemaError}</AlertDescription>
          </Alert>
        ) : schemaData.length > 0 ? (
          <div className="rounded-md border">
            <CHUItable
              result={{
                meta: getTableMeta(schemaData),
                data: schemaData,
                statistics: {
                  elapsed: 0,
                  rows_read: schemaData.length,
                  bytes_read: 0,
                },
                message: "",
                query_id: "",
              }}
            />
          </div>
        ) : (
          <Alert>
            <AlertCircle className="h-4 w-4" />
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
