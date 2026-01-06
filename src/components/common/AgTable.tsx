import { useMemo } from "react";
import { AgGridReact } from "ag-grid-react";
import { ColDef, AllCommunityModule, ICellRendererParams } from "ag-grid-community";
import { themeBalham, colorSchemeDark } from "ag-grid-community";
import { useTheme } from "@/components/common/theme-provider";
import EmptyQueryResult from "@/features/workspace/components/EmptyQueryResult";
import StatisticsDisplay from "@/features/workspace/components/StatisticsDisplay";
import DownloadDialog from "@/components/common/DownloadDialog";


import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";

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

interface AgTableProps {
  data: QueryResult;
  height?: number | string; // container height
  showMetadata?: boolean;
  showStatistics?: boolean;
  showHeader?: boolean;
}

// Format complex values for display in the grid
const formatCellValue = (value: any): string => {
  if (value === null || value === undefined) {
    return "<em>null</em>";
  }
  if (typeof value === 'object') {
    return JSON.stringify(value, null, 2);
  }
  return String(value);
};

// Cell renderer that can handle HTML content
const CustomCellRenderer = (props: ICellRendererParams) => {
  const formattedValue = formatCellValue(props.value);
  return <div dangerouslySetInnerHTML={{ __html: formattedValue }} />;
};

export default function AgTable({
  data,
  height = "350px",
  showMetadata = false,
  showStatistics = true,
  showHeader = true
}: AgTableProps) {
  const { theme } = useTheme();

  const gridTheme =
    theme === "light" ? themeBalham : themeBalham.withPart(colorSchemeDark);

  const defaultColDef: ColDef = {
    flex: 1,
    minWidth: 130,
    sortable: true,
    filter: true,
    resizable: true,
    filterParams: { buttons: ["reset", "apply"] },
    cellRenderer: CustomCellRenderer,
    autoHeight: true,
  };

  const dataColumnDefs = useMemo(() => {
    if (!data?.data?.length) return [];
    return Object.keys(data.data[0]).map((key) => ({
      headerName: key,
      valueGetter: (params: any) => params.data[key],
    }));
  }, [data?.data]);

  const metaColumnDefs = useMemo(() => {
    if (!data?.meta?.length) return [];
    return Object.keys(data.meta[0]).map((key) => ({
      headerName: key,
      valueGetter: (params: any) => params.data[key],
    }));
  }, [data?.meta]);

  // If no data is available yet or still loading
  if (!data || (!data.data && !data.meta && !data.statistics)) {
    return null;
  }

  // If there's data but it's empty
  if (data.rows === 0 || !data.data?.length) {
    return data.statistics ? (
      <EmptyQueryResult statistics={data.statistics} />
    ) : null;
  }

  const containerHeight =
    typeof height === "number" ? `${height}px` : height || "350px";
  const isFull = containerHeight === "100%";

  return (
    <Tabs defaultValue="results" className="h-full flex flex-col">
      {showHeader && (
        <TabsList className="w-full shrink-0">
          <TabsTrigger className="w-full" value="results">
            Results {data.rows && `(${data.rows} rows)`}
          </TabsTrigger>
          {showMetadata && (
            <TabsTrigger className="w-full" value="metadata">
              Metadata {data.meta && `(${data.meta.length} fields)`}
            </TabsTrigger>
          )}
          {showStatistics && (
            <TabsTrigger className="w-full" value="statistics">
              Statistics
            </TabsTrigger>
          )}
        </TabsList>
      )}

      <TabsContent value="results" className="flex-1 min-h-0" style={{ height: 'auto' }}>
        <div className="flex items-center justify-end pb-2">
          <DownloadDialog data={data.data || []} />
        </div>
        <div
          className={`ag-theme-balham w-full overflow-auto ${isFull ? 'h-full flex-1 min-h-0' : ''}`}
          style={isFull ? undefined : { height: containerHeight }}
        >
          <AgGridReact
            rowData={data.data || []}
            columnDefs={dataColumnDefs}
            defaultColDef={defaultColDef}
            modules={[AllCommunityModule]}
            theme={gridTheme}
            pagination={true}
            paginationPageSize={100}
            enableCellTextSelection={true}
            animateRows={true}
            domLayout="normal"
          />
        </div>
      </TabsContent>
      {showMetadata && (
        <TabsContent
          value="metadata"
          className="flex-1 min-h-0"
          style={{ height: 'auto' }}
        >
          <div className="flex items-center justify-end pb-2">
            <DownloadDialog data={data?.meta || []} />
          </div>
          <div
            className={`ag-theme-balham w-full overflow-auto ${isFull ? 'h-full flex-1 min-h-0' : ''}`}
            style={isFull ? undefined : { height: containerHeight }}
          >
            <AgGridReact
              rowData={data.meta || []}
              columnDefs={metaColumnDefs}
              defaultColDef={defaultColDef}
              modules={[AllCommunityModule]}
              theme={gridTheme}
              pagination={true}
              enableCellTextSelection={true}
            />
          </div>
        </TabsContent>
      )}
      {showStatistics && (
        <TabsContent value="statistics" className="flex-1 min-h-0 overflow-auto">
          {data.statistics && <StatisticsDisplay statistics={data.statistics} />}
        </TabsContent>
      )}
    </Tabs>
  );
}
