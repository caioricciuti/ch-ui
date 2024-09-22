import { useState, useRef, useCallback } from "react";
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
} from "@tanstack/react-table";
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

import FilterComponent from "@/components/workspace/FilterComponent";
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
  InfoIcon,
} from "lucide-react";
import DownloadDialog from "@/components/DonwloadDialog";

interface TableProps<T extends RowData> {
  result: {
    meta?: { name: string; type: string }[];
    data?: T[];
    statistics: {
      elapsed: number;
      rows_read: number;
      bytes_read: number;
    };
    message?: string;
    query_id?: string;
  };
  initialPageSize?: number;
}

export function CHUITable<T extends RowData>({
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
  const [columnSizing, setColumnSizing] = useState({});

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
        <p className="text-sm text-gray-800">Loading...</p>
      </div>
    );
  }

  const transformedColumns: ColumnDef<T>[] = [
    {
      id: "row-number",
      header: "#",
      cell: ({ row }) => row.index + 1,
      enableResizing: true,
      size: 40,
    },
    ...meta.map((col, index) => ({
      id: col.name || `col-${index}`,
      header: col.name || `Column ${index + 1}`,
      accessorKey: col.name,
      minSize: 50,
      maxSize: 2500,
    })),
  ];

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

  const isFiltered = table.getState().columnFilters.length > 0;

  const clearAllFilters = () => {
    table.resetColumnFilters();
  };

  const renderCell = useCallback((cell: any) => {
    const value = cell.getValue();
    if (value === null) {
      return <span className="text-gray-400 italic">null</span>;
    }
    return flexRender(cell.column.columnDef.cell, cell.getContext());
  }, []);

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

          <TooltipProvider>
            <Tooltip>
              <TooltipTrigger asChild>
                <InfoIcon size={14} />
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
      <div ref={tableContainerRef} className="flex-grow overflow-auto">
        <table className="w-full text-xs border-collapse table-fixed">
          <thead className="sticky top-0 z-20 bg-gray-500">
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
                          onClick={header.column.getToggleSortingHandler()}
                        >
                          {flexRender(
                            header.column.columnDef.header,
                            header.getContext()
                          )}
                          <div className="flex">
                            {header.column.getIsSorted() ? (
                              header.column.getIsSorted() === "desc" ? (
                                <ArrowDown className="h-3 w-3 ml-1" />
                              ) : (
                                <ArrowUp className="h-3 w-3 ml-1" />
                              )
                            ) : (
                              <span className="h-3 w-3" />
                            )}
                          </div>
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
                              Filter {header.column.columnDef.header as string}
                            </DropdownMenuLabel>
                            <DropdownMenuSeparator />
                            <div className="p-2">
                              <FilterComponent column={header.column} />
                            </div>
                            <DropdownMenuSeparator />
                            <DropdownMenuItem
                              onClick={() => header.column.setFilterValue(null)}
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
                        className="absolute right-0 top-0 h-full w-1 bg-gray-300 cursor-col-resize select-none touch-none hover:bg-gray-400"
                      />
                    )}
                  </th>
                ))}
              </tr>
            ))}
          </thead>
          <tbody>
            {table.getRowModel().rows.map((row) => (
              <tr
                key={row.id}
                className="hover:bg-gray-100 dark:hover:bg-gray-600 transition-colors"
              >
                {row.getVisibleCells().map((cell) => (
                  <td
                    key={cell.id}
                    style={{ width: cell.column.getSize() }}
                    className="p-2 border dark:border-gray-700 truncate"
                  >
                    {cell.column.id === "row-number"
                      ? row.index + 1
                      : renderCell(cell)}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

export default CHUITable;
