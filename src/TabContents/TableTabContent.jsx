import { useDatabaseTablesState } from "@/providers/DatabasesTablesContext";
import { useTabState } from "@/providers/TabsStateContext";
import { useTheme } from "@/providers/theme";
import { AgGridReact } from "ag-grid-react"; // React Data Grid Component
import "ag-grid-community/styles/ag-grid.css"; // Mandatory CSS required by the grid
import "ag-grid-community/styles/ag-theme-alpine.css"; // Optional theme CSS
import downloadCsv from "/src/helpers/donwloadCsv.js";
import transformRows from "/src/helpers/transformRows.js";

import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";

import {
  Table as TableShad,
  TableBody,
  TableCell,
  TableRow,
} from "@/components/ui/table";

import {
  Table as TableIcon,
  InfoIcon,
  EyeIcon,
  Loader2,
  DownloadIcon,
  FileSpreadsheetIcon,
  FileJson,
  FileX2,
  EllipsisVertical,
  FileTerminal,
} from "lucide-react";

import { Button } from "@/components/ui/button";

import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";

export default function TableTabContent({ tab }) {
  const { addQueryTab } = useTabState();
  const { theme } = useTheme();
  const { tab_title, tab_content } = tab;
  const { tablePreview, fetchTablePreview } =
    useDatabaseTablesState();
  const { updateInternalTabSelected } = useTabState();

  // Function to handle special cases for cell values
  const formatValue = (key, value) => {
    // Check for specific keys where the value should be interpreted as boolean
    if (["is_temporary", "metadata_version", "has_own_data"].includes(key)) {
      return value === 0 ? "false" : "true";
    }

    // Check for null, undefined, or empty values (string, array, object)
    if (
      value === "" || // Check for empty string
      value == null || // Check for null or undefined
      (Array.isArray(value) && value.length === 0) || // Check for empty array
      (typeof value === "object" ||
        value !== null &&
        Object.keys(value).length === 0) // Check for empty object
    ) {
      return null; // Return null or you can change it to 'N/A' or another placeholder if preferred
    }

    // Return the value if none of the above conditions are met
    return value;
  };

  // Ensure tab_content is an object and create table rows
  const detailTableRows =
    typeof tab_content === "object"
      ? Object.entries(tab_content).reduce((acc, [key, value]) => {
        const displayValue = formatValue(key, value);
        if (displayValue !== null) {
          // Only create rows for non-null values
          const row = (
            <TableRow key={key}>
              <TableCell className="font-bold whitespace-nowrap p-0">
                {key.replace(/_/g, " ").charAt(0).toUpperCase() +
                  key.replace(/_/g, " ").slice(1)}
              </TableCell>
              <TableCell className="whitespace-wrap px-2 py-1">
                {displayValue}
              </TableCell>
            </TableRow>
          );
          acc.push(row); // Add the row to the accumulator
        }
        return acc;
      }, [])
      : null;


  const currentPreview = tablePreview.find(
    (table) => table.table === tab_title
  )?.previewData;

  const renderSchemaRows = (schema) => {
    return schema.map((column) => (
      <TableRow key={column.name}>
        <TableCell className="p-1 border px-4">{column.name}</TableCell>
        <TableCell className="p-1 border px-4 text-center font-mono">
          <span className="bg-secondary px-2 rounded-full">{column.type}</span>
        </TableCell>
        <TableCell className="p-1 border px-4 text-center font-mono">
          <span className="bg-secondary px-2 rounded-full">
            {column.default_expression ? column.default_expression : "N/A"}
          </span>
        </TableCell>
        <TableCell className="p-1 border px-4">{column.comment}</TableCell>
      </TableRow>
    ));
  };

  return (
    <div className="h-[90vh] overflow-y-auto border rounded-md relative">
      <Tabs
        onValueChange={(e) => {
          updateInternalTabSelected(tab.tab_id, e);
        }}
        defaultValue={tab.tab_internal_tab_selected}
      >
        <TabsList className="sticky top-0 z-10 bg-white dark:bg-black w-full justify-between border-b">
          <TabsTrigger
            value="details"
            className="flex items-center w-full p-[10px]"
          >
            <InfoIcon className="mr-2" size={20} />
            DETAILS
          </TabsTrigger>
          <TabsTrigger
            value="schema"
            className="flex items-center w-full p-[10px]"
          >
            <TableIcon className="mr-2" size={20} />
            SCHEMA
          </TabsTrigger>
          <TabsTrigger
            value="preview"
            className="flex items-center w-full p-[10px]"
            onClick={() => {
              fetchTablePreview({ database: tab_content.database, table: tab_title });
            }}
          >
            <EyeIcon className="mr-2" size={20} />
            PREVIEW
          </TabsTrigger>

          <Popover>
            <PopoverTrigger className="p-2 hover:bg-muted">
              <EllipsisVertical className="cursor-pointer" size={20} />
            </PopoverTrigger>
            <PopoverContent className="w-max p-2 flex flex-col">
              <Button
                variant="ghost"
                onClick={() => {
                  addQueryTab({
                    tab_title: `Query - ${tab_title}`,
                    tab_content: `SELECT * FROM ${tab_content?.database}.${tab_title} LIMIT 1000;`,
                  });
                }}
              >
                <FileTerminal className="mr-2" size={20} />
                Query Table
              </Button>
            </PopoverContent>
          </Popover>
        </TabsList>

        <TabsContent value="details" className="pt-2 px-4 relative">
          <div>
            <h2 className="text-xl font-semibold mb-6">Table Info</h2>
            <TableShad>
              <TableBody>{detailTableRows}</TableBody>
            </TableShad>
          </div>
        </TabsContent>
        <TabsContent value="schema" className="pt-2 px-4 pb-6">
          <div>
            <h2 className="text-xl font-semibold mb-6">
              Schema Info - {tab_title}
            </h2>

            <TableShad>
              <TableRow>
                <TableCell className="font-bold p-1 border px-4">
                  Name
                </TableCell>
                <TableCell className="font-bold p-1 border px-4 text-center">
                  Type
                </TableCell>
                <TableCell className="font-bold p-1 border px-4 text-center">
                  Default Expression
                </TableCell>
                <TableCell className="font-bold p-1 border px-4">
                  Comment
                </TableCell>
              </TableRow>
              <TableBody>
                {tab_content.schema ? (
                  renderSchemaRows(tab_content.schema)
                ) : (
                  <div className="p-4 flex gap-4">
                    <Loader2 size={20} className="animate-spin" /> Loading
                    schema...
                  </div>
                )}
              </TableBody>
            </TableShad>
          </div>
        </TabsContent>

        <TabsContent value="preview" className="pt-2 px-4 pb-6">
          <div>
            <div className="flex items-center mb-6">
              <h2 className="text-xl font-semibold mr-6">
                Table Preview - {tab_title}
              </h2>
              <Popover>
                <PopoverTrigger>
                  <DownloadIcon className="cursor-pointer" size={20} />
                </PopoverTrigger>
                <PopoverContent className="w-max p-2 flex flex-col">
                  <Button
                    variant="link"
                    onClick={() => {
                      downloadCsv(currentPreview, tab_title, "csv");
                    }}
                  >
                    <FileSpreadsheetIcon className="mr-2" size={20} />
                    Download CSV
                  </Button>
                  <Button
                    variant="link"
                    onClick={() => {
                      downloadCsv(currentPreview, tab_title, "json");
                    }}
                  >
                    <FileJson className="mr-2" size={20} />
                    Download JSON
                  </Button>
                </PopoverContent>
              </Popover>
            </div>
            <div
              className={`${theme === "dark" ? "ag-theme-alpine-dark" : "ag-theme-alpine"
                } h-[75vh] w-full`}
            >
              {tablePreview && currentPreview?.length ? (
                <AgGridReact
                  enableCellTextSelection={true}
                  alwaysShowVerticalScroll={true}
                  rowData={transformRows(currentPreview)}
                  columnDefs={Object.keys(currentPreview[0])?.map((key) => ({
                    headerName: key,
                    field: key,
                    filter: true,
                  }))}
                  pagination={true}
                  paginationPageSize={20}
                  defaultColDef={{
                    flex: 1,
                    minWidth: 100,
                    resizable: true,
                  }}
                  gridOptions={{
                    rowStyle: { borderBottom: "1px solid #ddd" },
                    headerHeight: 50,
                    rowHeight: 40,
                    defaultColDef: {
                      sortable: true,
                      filter: true,
                      resizable: true,
                    },

                    columnTypes: {
                      numberColumn: {
                        width: 80,
                      },
                      nonEditableColumn: { editable: true },
                      dateColumn: {
                        filter: "agDateColumnFilter",
                        suppressMenu: true,
                      },
                    },
                  }}
                />
              ) : currentPreview?.length === 0 ? (
                <div className="flex p-6 rounded">
                  <div className="m-auto items-center">
                    <h1 className="text-lg font-semibold">Nothing to Show</h1>
                    <FileX2 className="m-auto mt-2" size={26} />
                  </div>
                </div>
              ) : (
                <div className="p-4 flex gap-4">
                  <Loader2 size={20} className="animate-spin" /> Loading
                  Preview...
                </div>
              )}
            </div>
          </div>
        </TabsContent>
      </Tabs>
    </div >
  );
}
