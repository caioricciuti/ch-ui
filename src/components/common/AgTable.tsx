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

export default function AgTable({ data }: AgTableProps) {
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
      field: key,
    }));
  }, [data?.data]);

  const metaColumnDefs = useMemo(() => {
    if (!data?.meta?.length) return [];
    return Object.keys(data.meta[0]).map((key) => ({
      headerName: key,
      field: key,
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

  return (
    <Tabs defaultValue="results">
      <TabsList className="w-full">
        <TabsTrigger className="w-full" value="results">
          Results {data.rows && `(${data.rows} rows)`}
          <DownloadDialog data={data.data || []} />
        </TabsTrigger>
        <TabsTrigger className="w-full" value="metadata">
          Metadata {data.meta && `(${data.meta.length} fields)`}
          <DownloadDialog data={data?.meta || []} />
        </TabsTrigger>
        <TabsTrigger className="w-full" value="statistics">
          Statistics
        </TabsTrigger>
      </TabsList>

      <TabsContent value="results" className="ag-theme-balham" style={{ height: "350px" }}>
        <div className="h-full w-full">
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
      <TabsContent value="metadata" className="ag-theme-balham" style={{ height: "350px" }}>
        <div className="h-full w-full">
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
      <TabsContent value="statistics">
        {data.statistics && <StatisticsDisplay statistics={data.statistics} />}
      </TabsContent>
    </Tabs>
  );
}
