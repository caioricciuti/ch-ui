import React, { useState, useMemo, useCallback } from "react";
import { toast } from "sonner";
import { Loader2, FileX2 } from "lucide-react";
import { AgGridReact } from "ag-grid-react";
import { ColDef, AllCommunityModule } from "ag-grid-community";
import { themeBalham, colorSchemeDark } from "ag-grid-community";

// Component imports
import SQLEditor from "@/features/workspace/editor/SqlEditor";
import {
  ResizablePanel,
  ResizablePanelGroup,
  ResizableHandle,
} from "@/components/ui/resizable";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";
import { useTheme } from "@/components/common/theme-provider";
import DownloadDialog from "@/components/common/DownloadDialog";
import EmptyQueryResult from "./EmptyQueryResult";
import StatisticsDisplay from "./StatisticsDisplay";

// Store
import useAppStore from "@/store";

// Types
interface SqlTabProps {
  tabId: string;
}

interface IRow {
  [key: string]: any;
}

/**
 * SqlTab component that provides a SQL editor and result viewer
 *
 * Displays a resizable split panel with SQL editor on top and
 * query results, metadata, and statistics tabs on the bottom.
 */
const SqlTab: React.FC<SqlTabProps> = ({ tabId }) => {
  const { getTabById, runQuery, fetchDatabaseInfo } = useAppStore();
  const tab = getTabById(tabId);
  const { theme } = useTheme();
  const [activeTab, setActiveTab] = useState<string>("results");

  // Configure AG Grid theme based on app theme
  const gridTheme =
    theme === "light" ? themeBalham : themeBalham.withPart(colorSchemeDark);

  // AG Grid configuration
  const defaultColDef: ColDef = {
    flex: 1,
    minWidth: 130,
    sortable: true,
    filter: true,
    resizable: true,
    filterParams: { buttons: ["reset", "apply"] },
  };

  const [columnDefs, setColumnDefs] = useState<ColDef<IRow>[]>([]);
  const [rowData, setRowData] = useState<IRow[]>([]);

  // Detect schema-changing queries to refresh database explorer
  const isSchemaModifyingQuery = (query: string): boolean => {
    return /^\s*(CREATE|DROP|ALTER|TRUNCATE|RENAME|INSERT|UPDATE|DELETE)\s+/i.test(
      query
    );
  };

  // Handle query execution
  const handleRunQuery = useCallback(
    async (query: string) => {
      try {
        const shouldRefresh = isSchemaModifyingQuery(query);
        const result = await runQuery(query, tabId);

        if (!result.error && shouldRefresh) {
          await fetchDatabaseInfo();
          toast.success("Data Explorer refreshed due to schema change");
        }
      } catch (error) {
        console.error("Error running query:", error);
        toast.error(
          "Failed to execute query. Please check the console for more details."
        );
      }
    },
    [runQuery, tabId, fetchDatabaseInfo]
  );

  // Process result data into grid-compatible format
  useMemo(() => {
    if (tab?.result?.data?.length && tab?.result?.meta?.length) {
      const colDefs: ColDef<IRow>[] = tab.result.meta.map((col: any) => ({
        headerName: col.name,
        field: col.name,
      }));

      setRowData(tab.result.data);
      setColumnDefs(colDefs);
    } else {
      setColumnDefs([]);
      setRowData([]);
    }
  }, [tab?.result?.data, tab?.result?.meta]);

  // UI rendering functions
  const renderLoading = () => (
    <div className="h-full w-full flex items-center justify-center">
      <Loader2 size={24} className="animate-spin mr-2" />
      <p>Running query...</p>
    </div>
  );

  const renderError = (errorMessage: string) => (
    <div className="m-4">
      <Alert variant="destructive">
        <AlertTitle>Error</AlertTitle>
        <AlertDescription>{errorMessage}</AlertDescription>
      </Alert>
    </div>
  );

  const renderEmpty = () => (
    <div className="h-full flex items-center justify-center">
      <div className="flex flex-col items-center">
        <FileX2 size={48} className="text-muted-foreground mb-4" />
        <p className="text-sm text-muted-foreground">
          There's no data yet! Run a query to get started.
        </p>
      </div>
    </div>
  );

  const renderResultsTab = () => {
    if (!columnDefs.length || !rowData.length) {
      return tab?.result?.statistics ? (
        <EmptyQueryResult statistics={tab.result.statistics} />
      ) : null;
    }

    return (
      <div className="h-full flex flex-col">
        <div className="flex-1">
          <AgGridReact
            rowData={rowData}
            columnDefs={columnDefs}
            defaultColDef={defaultColDef}
            modules={[AllCommunityModule]}
            theme={gridTheme}
            pagination={true}
            paginationPageSize={100}
            enableCellTextSelection={true}
            animateRows={true}
            suppressMovableColumns={false}
          />
        </div>
      </div>
    );
  };

  const renderMetadataTab = () => {
    if (!tab?.result?.meta?.length) return null;

    return (
      <div className="h-full flex flex-col">
        <div className="flex-1">
          <AgGridReact
            rowData={tab.result.meta}
            columnDefs={[
              { headerName: "Column Name", field: "name", flex: 1 },
              { headerName: "Data Type", field: "type", flex: 1 },
            ]}
            defaultColDef={defaultColDef}
            modules={[AllCommunityModule]}
            theme={gridTheme}
            pagination={true}
            enableCellTextSelection={true}
          />
        </div>
      </div>
    );
  };

  const renderStatisticsResults = () => {
    if (!tab?.result?.statistics) return null;
    return <StatisticsDisplay statistics={tab.result.statistics} />;
  };

  const renderResultTabs = () => {
    const hasData = tab?.result?.data?.length > 0;
    const hasMeta = tab?.result?.meta?.length > 0;

    return (
      <Tabs
        value={activeTab}
        onValueChange={setActiveTab}
        className="h-full flex flex-col"
      >
        <TabsList className="rounded-none border-b px-4">
          <TabsTrigger value="results">
            Results
            {hasData && (
              <div className="ml-2 text-muted-foreground items-center flex">
                ({tab?.result.data.length} rows)
                <DownloadDialog data={tab?.result.data} />
              </div>
            )}
          </TabsTrigger>
          <TabsTrigger value="metadata">
            Metadata
            {hasMeta && (
              <div className="ml-2 text-muted-foreground items-center flex">
                ({tab?.result.meta.length} columns)
                <DownloadDialog data={tab?.result.meta} />
              </div>
            )}
          </TabsTrigger>
          <TabsTrigger value="statistics">Statistics</TabsTrigger>
        </TabsList>
        <div className="flex-1">
          <TabsContent value="results" className="h-full m-0">
            {renderResultsTab()}
          </TabsContent>
          <TabsContent value="metadata" className="h-full m-0">
            {renderMetadataTab()}
          </TabsContent>
          <TabsContent value="statistics" className="h-full m-0">
            {renderStatisticsResults()}
          </TabsContent>
        </div>
      </Tabs>
    );
  };

  // Render main results section based on current state
  const renderResults = () => {
    if (tab?.isLoading) return renderLoading();
    if (tab?.error) return renderError(tab.error);
    if (!tab?.result) return renderEmpty();
    if (tab.result.error) return renderError(tab.result.error);

    return renderResultTabs();
  };

  // Return null if tab doesn't exist
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
