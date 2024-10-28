import React, { useState, useRef, useCallback, useMemo } from "react";
import {
  flexRender,
  useReactTable,
  getCoreRowModel,
  getSortedRowModel,
  getFilteredRowModel,
  getPaginationRowModel,
  ColumnFiltersState,
  SortingState,
  PaginationState,
  ColumnDef,
  RowData,
  VisibilityState,
  Table,
  Row,
} from "@tanstack/react-table";
import { useVirtualizer } from "@tanstack/react-virtual";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@/components/ui/select";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  DropdownMenuSeparator,
  DropdownMenuLabel,
  DropdownMenuCheckboxItem,
} from "@/components/ui/dropdown-menu";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";

import FilterComponent from "@/components/misc/FilterComponent";

import {
  ArrowDown,
  ArrowUp,
  ChevronLeft,
  ChevronRight,
  ChevronsLeft,
  ChevronsRight,
  Search,
  Columns,
  Filter,
  FilterX,
  Info,
} from "lucide-react";
import DownloadDialog from "@/components/misc/DownloadDialog";

interface TableProps<T extends RowData> {
  result: {
    meta?: { name: string; type: string }[];
    data?: T[];
    statistics?: {
      elapsed: number;
      rows_read: number;
      bytes_read: number;
    };
    message?: string;
    query_id?: string;
  };
  initialPageSize?: number;
}

const DEFAULT_COLUMN_SIZE = 150;

// Remove the getNestedValue function since it's not suitable for keys with dots
// function getNestedValue(obj: any, path: string) {
//   return path.split('.').reduce((acc, part) => acc && acc[part], obj);
// }

function CHUITable<T extends RowData>({
  result,
  initialPageSize = 20,
}: TableProps<T>) {
  const [sorting, setSorting] = useState<SortingState>([]);
  const [columnFilters, setColumnFilters] = useState<ColumnFiltersState>([]);
  const [globalFilter, setGlobalFilter] = useState("");
  const [columnVisibility, setColumnVisibility] = useState<VisibilityState>({});
  const [pagination, setPagination] = useState<PaginationState>({
    pageIndex: 0,
    pageSize: initialPageSize,
  });
  const [columnSizing, setColumnSizing] = useState<{ [key: string]: number }>(
    {}
  );
  const [autoSizedColumns, setAutoSizedColumns] = useState<{
    [key: string]: boolean;
  }>({});

  const sizeCache = useRef<{ [key: string]: number }>({});

  const { meta, data, statistics, message, query_id } = result;

  if (message) {
    return (
      <div className="w-full mt-4 p-4">
        <p className="text-sm">{message}</p>
        <p className="text-xs text-gray-600">
          Query ID: {query_id || "Unknown"}
        </p>
      </div>
    );
  }

  if (!data || !meta) {
    return (
      <div className="w-full mt-4 p-4 bg-gray-100 rounded-md">
        <p className="text-sm text-gray-800">No data/meta</p>
      </div>
    );
  }

  const transformedColumns: ColumnDef<T>[] = useMemo(
    () => [
      {
        id: "row-number",
        header: "#",
        cell: ({ row }) => row.index + 1,
        enableResizing: true,
        size: 60,
        minSize: 40,
        maxSize: 200,
        enableSorting: false,
        enableColumnFilter: false,
      },
      ...meta.map((col, index) => ({
        id: col.name || `col-${index}`,
        header: col.name || `Column ${index + 1}`,
        accessorKey: col.name,
        minSize: 50,
        maxSize: 1500,
        size: DEFAULT_COLUMN_SIZE,
        enableResizing: true,
        cell: ({ row }: { row: Row<T> }) => {
          const value = row.original[col.name as keyof T];

          if (typeof value === "boolean") {
            return value.toString(); // Convert boolean to string
          }

          if (typeof value === "object" && value !== null) {
            return JSON.stringify(value);
          }

          return value;
        },
      })),
    ],
    [meta, pagination.pageIndex, pagination.pageSize]
  );

  const table = useReactTable({
    data,
    columns: transformedColumns,
    state: {
      sorting,
      columnFilters,
      globalFilter,
      columnVisibility,
      pagination,
      columnSizing,
    },
    onSortingChange: setSorting,
    onColumnFiltersChange: setColumnFilters,
    onGlobalFilterChange: setGlobalFilter,
    onColumnVisibilityChange: setColumnVisibility,
    onPaginationChange: setPagination,
    onColumnSizingChange: setColumnSizing,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    enableColumnResizing: true,
    columnResizeMode: "onChange",
  });

  const tableContainerRef = useRef<HTMLDivElement>(null);

  const { rows } = table.getRowModel();

  const rowVirtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: () => tableContainerRef.current,
    estimateSize: () => 35,
    overscan: 10,
  });

  const virtualRows = rowVirtualizer.getVirtualItems();
  const totalSize = rowVirtualizer.getTotalSize();

  const paddingTop = virtualRows.length > 0 ? virtualRows[0].start || 0 : 0;
  const paddingBottom =
    virtualRows.length > 0
      ? totalSize - (virtualRows[virtualRows.length - 1].end || 0)
      : 0;

  const isFiltered = table.getState().columnFilters.length > 0;

  const clearAllFilters = useCallback(() => {
    table.resetColumnFilters();
  }, [table]);

  const renderCell = useCallback((cell: any) => {
    const value = cell.getValue();
    if (value === null) {
      return <span className="text-gray-400 italic">null</span>;
    }
    return flexRender(cell.column.columnDef.cell, cell.getContext());
  }, []);

  const calculateAutoSize = useCallback((columnId: string, table: Table<T>) => {
    if (sizeCache.current[columnId]) {
      return sizeCache.current[columnId];
    }

    const canvas = document.createElement("canvas");
    const context = canvas.getContext("2d");
    if (!context) return DEFAULT_COLUMN_SIZE;

    const computedStyle = getComputedStyle(document.body);
    context.font = computedStyle.font || "12px sans-serif";

    const headerText = columnId;
    let maxWidth = context.measureText(headerText).width + 16;

    const rows = table.getRowModel().rows.slice(0, 200);
    for (const row of rows) {
      const cellValue = row.getValue(columnId);
      const cellText =
        cellValue === null
          ? "null"
          : typeof cellValue === "object"
          ? JSON.stringify(cellValue)
          : cellValue?.toString();
      const width = context.measureText(cellText || "").width + 16;
      if (width > maxWidth) {
        maxWidth = width;
      }
    }

    const maxSize =
      table.getAllColumns().find((col) => col.id === columnId)?.columnDef
        .maxSize || 4000;
    const calculatedSize = Math.min(maxWidth, maxSize);

    sizeCache.current[columnId] = calculatedSize;
    return calculatedSize;
  }, []);

  const handleDoubleClick = useCallback(
    (header: any) => {
      const columnId = header.column.id;

      setAutoSizedColumns((prev) => {
        const isCurrentlyAutoSized = prev[columnId];

        if (isCurrentlyAutoSized) {
          setColumnSizing((old) => ({
            ...old,
            [columnId]: DEFAULT_COLUMN_SIZE,
          }));
          return { ...prev, [columnId]: false };
        } else {
          const autoSize = calculateAutoSize(columnId, table);
          setColumnSizing((old) => ({
            ...old,
            [columnId]: autoSize,
          }));
          return { ...prev, [columnId]: true };
        }
      });
    },
    [calculateAutoSize, table]
  );

  const TableRow = React.memo(({ row }: { row: Row<T> }) => (
    <tr className="hover:bg-gray-100 dark:hover:bg-gray-600 transition-colors">
      {row.getVisibleCells().map((cell) => (
        <td
          key={cell.id}
          style={{ width: cell.column.getSize() }}
          className="p-2 border dark:border-gray-700 truncate"
        >
          {renderCell(cell)}
        </td>
      ))}
    </tr>
  ));

  return (
    <div className="flex flex-col h-full w-full">
      {/* Commands at the top of the table */}
      <div className="flex items-center justify-between text-xs p-2">
        <div className="flex items-center space-x-2">
          <Select
            value={table.getState().pagination.pageSize.toString()}
            onValueChange={(value) => {
              table.setPageSize(Number(value));
            }}
          >
            <SelectTrigger className="h-8 w-[70px]">
              <SelectValue placeholder="Rows" />
            </SelectTrigger>
            <SelectContent>
              {[20, 50, 100, 200].map((pageSize) => (
                <SelectItem
                  key={pageSize}
                  value={pageSize.toString()}
                  className="text-xs"
                >
                  {pageSize}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>

          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button className="h-8 w-8 p-0">
                <Columns className="h-4 w-4" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-40">
              <div className="max-h-52 overflow-auto">
                {table.getAllColumns().map((column) => (
                  <DropdownMenuCheckboxItem
                    key={column.id}
                    checked={column.getIsVisible()}
                    onCheckedChange={(value) =>
                      column.toggleVisibility(!!value)
                    }
                    className="text-xs"
                  >
                    {column.id}
                  </DropdownMenuCheckboxItem>
                ))}
              </div>
            </DropdownMenuContent>
          </DropdownMenu>

          {isFiltered && (
            <TooltipProvider>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    onClick={clearAllFilters}
                    className="h-6 w-6 p-1 bg-transparent text-primary hover:bg-secondary"
                  >
                    <FilterX className="h-4 w-4" />
                  </Button>
                </TooltipTrigger>
                <TooltipContent>Clear all filters</TooltipContent>
              </Tooltip>
            </TooltipProvider>
          )}

          <DownloadDialog data={data} />

          {
            statistics &&
            <TooltipProvider>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Info size={14} />
                </TooltipTrigger>
                <TooltipContent>
                  <div className="text-xs p-2 text-primary/70">
                    <p>Rows Read: {statistics.rows_read}</p>
                    <p>Bytes Read: {statistics.bytes_read}</p>
                    <p>Elapsed Time: {statistics.elapsed} seconds</p>
                  </div>
                </TooltipContent>
              </Tooltip>
            </TooltipProvider>
          }
        </div>

        <div className="flex-1 px-2 max-w-sm">
          <div className="relative">
            <Input
              placeholder="Search all columns..."
              value={globalFilter ?? ""}
              onChange={(e) => setGlobalFilter(e.target.value)}
              className="h-8 w-full pl-8 pr-4 text-xs"
            />
            <Search
              className="absolute left-2 top-1/2 transform -translate-y-1/2 text-gray-400"
              size={14}
            />
          </div>
        </div>

        <div className="flex items-center space-x-2">
          <TooltipProvider>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  onClick={() => table.setPageIndex(0)}
                  disabled={!table.getCanPreviousPage()}
                  variant="outline"
                  size="icon"
                  className="h-8 w-8 p-0"
                >
                  <ChevronsLeft size={14} />
                </Button>
              </TooltipTrigger>
              <TooltipContent>First Page</TooltipContent>
            </Tooltip>
          </TooltipProvider>

          <TooltipProvider>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  onClick={() => table.previousPage()}
                  disabled={!table.getCanPreviousPage()}
                  variant="outline"
                  size="icon"
                  className="h-8 w-8 p-0"
                >
                  <ChevronLeft size={14} />
                </Button>
              </TooltipTrigger>
              <TooltipContent>Previous Page</TooltipContent>
            </Tooltip>
          </TooltipProvider>

          <span className="text-xs text-gray-600 dark:text-gray-300 min-w-[100px] text-center">
            Page {table.getState().pagination.pageIndex + 1} of{" "}
            {table.getPageCount()}
          </span>

          <TooltipProvider>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  onClick={() => table.nextPage()}
                  disabled={!table.getCanNextPage()}
                  variant="outline"
                  size="icon"
                  className="h-8 w-8 p-0"
                >
                  <ChevronRight size={14} />
                </Button>
              </TooltipTrigger>
              <TooltipContent>Next Page</TooltipContent>
            </Tooltip>
          </TooltipProvider>

          <TooltipProvider>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  onClick={() => table.setPageIndex(table.getPageCount() - 1)}
                  disabled={!table.getCanNextPage()}
                  variant="outline"
                  size="icon"
                  className="h-8 w-8 p-0"
                >
                  <ChevronsRight size={14} />
                </Button>
              </TooltipTrigger>
              <TooltipContent>Last Page</TooltipContent>
            </Tooltip>
          </TooltipProvider>

          <span>{table.getPrePaginationRowModel().rows.length} Rows</span>
        </div>
      </div>

      {/* The table content */}
      <div
        ref={tableContainerRef}
        className="flex-grow overflow-auto relative"
        style={{ willChange: "transform" }}
      >
        <table className="w-full text-xs border-collapse table-fixed">
          <thead className="sticky top-0 bg-gray-500">
            {table.getHeaderGroups().map((headerGroup) => (
              <tr key={headerGroup.id}>
                {headerGroup.headers.map((header) => (
                  <th
                    key={header.id}
                    style={{
                      width: header.getSize(),
                      position: "relative",
                    }}
                    className="text-left p-2 font-bold bg-gray-500 text-white"
                  >
                    {header.isPlaceholder ? null : (
                      <>
                        {header.column.id === "row-number" ? (
                          "#"
                        ) : (
                          <div className="flex items-center justify-between">
                            <div
                              className={`flex items-center space-x-2 ${
                                header.column.getCanSort()
                                  ? "cursor-pointer select-none"
                                  : ""
                              }`}
                              onClick={
                                header.column.getCanSort()
                                  ? header.column.getToggleSortingHandler()
                                  : undefined
                              }
                            >
                              {flexRender(
                                header.column.columnDef.header,
                                header.getContext()
                              )}
                              {{
                                asc: <ArrowUp className="h-3 w-3 ml-1" />,
                                desc: <ArrowDown className="h-3 w-3 ml-1" />,
                              }[header.column.getIsSorted() as string] ?? (
                                <span className="h-3 w-3 ml-1" />
                              )}
                            </div>
                            <DropdownMenu>
                              <DropdownMenuTrigger asChild>
                                {header.column.getFilterValue() ? (
                                  <FilterX className="h-3 w-3" />
                                ) : (
                                  <Filter className="h-3 w-3" />
                                )}
                              </DropdownMenuTrigger>
                              <DropdownMenuContent align="end" className="w-56">
                                <DropdownMenuLabel className="text-xs">
                                  Filter{" "}
                                  {header.column.columnDef.header as string}
                                </DropdownMenuLabel>
                                <DropdownMenuSeparator />
                                <div className="p-2">
                                  <FilterComponent column={header.column} />
                                </div>
                                <DropdownMenuSeparator />
                                <DropdownMenuItem
                                  onClick={() =>
                                    header.column.setFilterValue(null)
                                  }
                                  className="text-xs"
                                >
                                  Clear filter
                                </DropdownMenuItem>
                              </DropdownMenuContent>
                            </DropdownMenu>
                          </div>
                        )}
                        {header.column.getCanResize() && (
                          <div
                            onMouseDown={header.getResizeHandler()}
                            onTouchStart={header.getResizeHandler()}
                            onDoubleClick={() => handleDoubleClick(header)}
                            className={`absolute right-0 top-0 h-full w-1 bg-gray-300 cursor-col-resize select-none touch-none hover:bg-gray-400 ${
                              header.column.getIsResizing() ? "bg-gray-500" : ""
                            }`}
                          />
                        )}
                      </>
                    )}
                  </th>
                ))}
              </tr>
            ))}
          </thead>
          <tbody>
            {paddingTop > 0 && (
              <tr>
                <td style={{ height: `${paddingTop}px` }} />
              </tr>
            )}
            {virtualRows.map((virtualRow) => (
              <TableRow
                key={rows[virtualRow.index].id}
                row={rows[virtualRow.index]}
              />
            ))}
            {paddingBottom > 0 && (
              <tr>
                <td style={{ height: `${paddingBottom}px` }} />
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}

export default React.memo(CHUITable);
