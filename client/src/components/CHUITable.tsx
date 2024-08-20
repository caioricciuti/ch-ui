import { useState, useRef, useEffect, useCallback } from "react";
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
import { cn } from "@/lib/utils";
import { getColumnType } from "@/lib/tableUtils";
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
  Download,
} from "lucide-react";

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

  // If there's a message, display it instead of the table
  if (message) {
    console.log(message);
    return (
      <div className="w-full mt-4 p-4">
        <p className="text-sm">{message}</p>
        <p className="text-xs text-gray-600">
          Query ID: {query_id || "Unknown"}
        </p>
      </div>
    );
  }

  // If there's no data or meta, display a loading message
  if (!data || !meta) {
    return (
      <div className="w-full mt-4 p-4 bg-gray-100 rounded-md">
        <p className="text-sm text-gray-800">Loading...</p>
      </div>
    );
  }

  const transformedColumns: ColumnDef<T>[] = meta.map((col, index) => ({
    id: col.name || `col-${index}`,
    header: col.name || `Column ${index + 1}`,
    accessorKey: col.name,
    size: 150, // Default column width
  }));

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

  const tableRef = useRef<HTMLTableElement>(null);

  useEffect(() => {
    const handleResize = () => {
      table.resetColumnSizing();
    };

    window.addEventListener("resize", handleResize);
    return () => {
      window.removeEventListener("resize", handleResize);
    };
  }, [table]);

  const renderCell = useCallback((cell: any) => {
    const value = cell.getValue();
    const columnType = getColumnType(value);

    switch (columnType) {
      case "number":
        return value.toLocaleString();
      default:
        return flexRender(cell.column.columnDef.cell, cell.getContext());
    }
  }, []);

  return (
    <div className="w-full space-y-4 mt-4">
      <div className="text-xs text-gray-600 justify-between flex items-center">
        <div>
          {statistics.rows_read} rows in {statistics.elapsed} ms
          {statistics.bytes_read > 0 && ` (${statistics.bytes_read} bytes)`}
        </div>
        <Button className="h-6 w-6 p-0 bg-transparent text-primary hover:bg-secondary">
          <Download className="h-4 w-4" />
        </Button>
      </div>
      <div className="overflow-auto h-64">
        <table ref={tableRef} className="text-xs">
          <thead className="sticky top-0 z-20 bg-gray-500">
            {table.getHeaderGroups().map((headerGroup) => (
              <tr key={headerGroup.id}>
                {headerGroup.headers.map((header) => (
                  <th
                    key={header.id}
                    className="text-left px-3 py-1 font-bold"
                    style={{ width: header.getSize() }}
                  >
                    <div className="flex items-center justify-between">
                      <div
                        className={cn(
                          "flex items-center space-x-2",
                          header.column.getCanSort() &&
                            "cursor-pointer select-none"
                        )}
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
                          <Button
                            variant="ghost"
                            size="sm"
                            className="h-6 w-6 p-0"
                          >
                            <Filter className="h-3 w-3" />
                          </Button>
                        </DropdownMenuTrigger>
                        <DropdownMenuContent align="end" className="w-56">
                          <DropdownMenuLabel className="text-xs">
                            Filter {header.column.columnDef.header as string}
                          </DropdownMenuLabel>
                          <DropdownMenuSeparator />
                          <div className="p-2">
                            <FilterComponent
                              column={header.column}
                              table={table}
                            />
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
                    {header.column.getCanResize() && (
                      <div
                        onMouseDown={header.getResizeHandler()}
                        onTouchStart={header.getResizeHandler()}
                        className={`resizer ${
                          header.column.getIsResizing() ? "isResizing" : ""
                        }`}
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
                className="hover:bg-gray-100 dark:hover:bg-gray-500 transition-colors"
              >
                {row.getVisibleCells().map((cell) => (
                  <td
                    key={cell.id}
                    className="px-3 py-2 border truncate"
                    style={{ width: cell.column.getSize() }}
                  >
                    {renderCell(cell)}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <div className="flex items-center justify-between  p-1 text-xs">
        <div className="flex justify-between items-center">
          <div className="flex items-center">
            <Select
              value={table.getState().pagination.pageSize.toString()}
              onValueChange={(value) => {
                table.setPageSize(Number(value));
              }}
            >
              <SelectTrigger className="text-sm">
                <SelectValue placeholder="Rows" />
              </SelectTrigger>
              <SelectContent>
                {[20, 50, 100, 200].map((pageSize) => (
                  <SelectItem
                    key={pageSize}
                    value={pageSize.toString()}
                    className="text-sm p-1"
                  >
                    {pageSize}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button className="p-2">
                  <Columns className="h-4 w-4" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <div className="max-h-52 overflow-auto">
                  {table.getAllColumns().map((column) => {
                    return (
                      <DropdownMenuCheckboxItem
                        key={column.id}
                        checked={column.getIsVisible()}
                        onCheckedChange={(value) =>
                          column.toggleVisibility(!!value)
                        }
                      >
                        {column.id}
                      </DropdownMenuCheckboxItem>
                    );
                  })}
                </div>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
          <div className="relative">
            <Input
              placeholder="Search all columns..."
              value={globalFilter ?? ""}
              onChange={(e) => setGlobalFilter(e.target.value)}
              className="pl-10 pr-4 py-2 w-full text-sm"
            />
            <Search
              className="absolute left-3 top-1/2 transform -translate-y-1/2 text-gray-400"
              size={18}
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
                  className="h-8 w-8"
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
                  className="h-8 w-8"
                >
                  <ChevronLeft size={14} />
                </Button>
              </TooltipTrigger>
              <TooltipContent>Previous Page</TooltipContent>
            </Tooltip>
          </TooltipProvider>
          <TooltipProvider>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  onClick={() => table.nextPage()}
                  disabled={!table.getCanNextPage()}
                  variant="outline"
                  size="icon"
                  className="h-8 w-8"
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
                  className="h-8 w-8"
                >
                  <ChevronsRight size={14} />
                </Button>
              </TooltipTrigger>
              <TooltipContent>Last Page</TooltipContent>
            </Tooltip>
          </TooltipProvider>
        </div>
        <span className="text-xs text-gray-600 dark:text-gray-300">
          Page {table.getState().pagination.pageIndex + 1} of{" "}
          {table.getPageCount()}
        </span>
        <span className="text-xs text-gray-600 dark:text-gray-300">
          {table.getPrePaginationRowModel().rows.length} Rows
        </span>
      </div>
    </div>
  );
}

export default CHUITable;
