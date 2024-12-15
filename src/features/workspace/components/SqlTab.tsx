import React from "react";
import SQLEditor from "@/features/workspace/editor/SqlEditor";
import {
  ResizablePanel,
  ResizablePanelGroup,
  ResizableHandle,
} from "@/components/ui/resizable";
import useAppStore from "@/store";
import CHUITable from "@/components/common/table/CHUItable";
import { Loader2, FileX2 } from "lucide-react";
import { toast } from "sonner";
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";

interface SqlTabProps {
  tabId: string;
}

const SqlTab: React.FC<SqlTabProps> = ({ tabId }) => {
  const { getTabById, runQuery, fetchDatabaseInfo } = useAppStore();
  const tab = getTabById(tabId);

  const handleRunQuery = async (query: string) => {
    try {
      const shouldRefresh =
        /^\s*(CREATE|DROP|ALTER|TRUNCATE|RENAME|INSERT|UPDATE|DELETE)\s+/i.test(
          query
        );

      const result = await runQuery(query, tabId);

      if (result.error) {
        // If there's an error, don't refresh the Data Explorer
        return;
      } else {
        // Only refresh and show success messages if there's no error
        if (shouldRefresh) {
          await fetchDatabaseInfo();
          toast.success("Data Explorer refreshed due to schema change");
        }
      }
    } catch (error) {
      console.error("Error running query:", error);
      toast.error(
        "Failed to execute query. Please check the console for more details."
      );
    }
  };

  const renderResults = () => {
    if (tab?.isLoading) {
      return (
        <div className="h-full w-full flex items-center justify-center">
          <Loader2 size={24} className="animate-spin mr-2" />
          <p>Running query</p>
        </div>
      );
    }

    if (tab?.error) {
      return (
        <div className="m-4">
          <Alert variant="destructive">
            <AlertTitle>Error</AlertTitle>
            <AlertDescription>{tab.error}</AlertDescription>
          </Alert>
        </div>
      );
    }

    if (!tab?.result) {
      return (
        <div className="h-full flex items-center justify-center">
          <div className="flex flex-col items-center">
            <FileX2 size={48} className="text-muted-foreground mb-4" />
            <p className="text-sm text-muted-foreground">
              There's no data yet! Run a query to get started.
            </p>
          </div>
        </div>
      );
    }

    if (tab.result.error) {
      return (
        <div className="m-4">
          <Alert variant="destructive">
            <AlertTitle>Error</AlertTitle>
            <AlertDescription>{tab.result.error}</AlertDescription>
          </Alert>
        </div>
      );
    }

    if (tab.result.message) {
      return (
        <div className="m-4">
          <Alert variant="info">
            <AlertDescription>{tab.result.message}</AlertDescription>
          </Alert>
        </div>
      );
    }

    if (
      tab.result.data &&
      tab.result.data.length > 0 &&
      tab.result.meta.length > 0
    ) {
      return (
        <div className="h-full">
          <CHUITable
            result={{
              meta: tab.result.meta,
              data: tab.result.data,
              statistics: tab.result.statistics,
              message: tab.result.message,
              query_id: tab.result.query_id,
            }}
          />
        </div>
      );
    }

    return (
      <div className="p-4 m-4 border text-sm rounded-md text-blue-600 dark:text-blue-200 bg-blue-100/30 border-blue-300">
        Query executed successfully. No data returned.
      </div>
    );
  };

  if (!tab) return null;

  return (
    <div className="h-full">
      <ResizablePanelGroup direction="vertical">
        <ResizablePanel defaultSize={50} minSize={25}>
          <SQLEditor tabId={tabId} onRunQuery={handleRunQuery} />
        </ResizablePanel>
        <ResizableHandle withHandle />
        <ResizablePanel defaultSize={50} minSize={25}>
          {renderResults()}
        </ResizablePanel>
      </ResizablePanelGroup>
    </div>
  );
};

export default SqlTab;
