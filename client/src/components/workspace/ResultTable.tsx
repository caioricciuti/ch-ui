import React, { useState, useRef, useEffect } from "react";
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
  getPaginationRowModel,
  getSortedRowModel,
  getFilteredRowModel,
  ColumnFiltersState,
  SortingState,
  PaginationState,
  ColumnDef,
  RowData,
} from "@tanstack/react-table";
import {
  ChevronDown,
  ChevronUp,
  ChevronLeft,
  ChevronRight,
  ChevronsLeft,
  ChevronsRight,
  Filter,
  Search,
  Columns,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
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
import { Card, CardContent } from "@/components/ui/card";

interface AdvancedTableProps<T extends RowData> {
  data: T[];
  columns: ColumnDef<T>[];
  initialPageSize?: number;
}

const getColumnType = (value: any): string => {
  if (value instanceof Date) return "date";
  if (typeof value === "number") return "number";
  if (typeof value === "boolean") return "boolean";
  return "string";
};

const FilterComponent = ({ column, table }: { column: any; table: any }) => {
  const firstValue = table
    .getPreFilteredRowModel()
    .flatRows[0]?.getValue(column.id);
  const columnType = getColumnType(firstValue);

  const [min, max] = React.useMemo(() => {
    if (columnType === "number" || columnType === "date") {
      let min = table.getPreFilteredRowModel().flatRows[0]?.getValue(column.id);
      let max = min;
      table.getPreFilteredRowModel().flatRows.forEach((row: any) => {
        const value = row.getValue(column.id);
        if (value < min) min = value;
        if (value > max) max = value;
      });
      return [min, max];
    }
    return [null, null];
  }, [table, column.id, columnType]);

  const [minValue, setMinValue] = useState<string | undefined>(min?.toString());
  const [maxValue, setMaxValue] = useState<string | undefined>(max?.toString());

  const handleRangeChange = () => {
    column.setFilterValue([minValue, maxValue]);
  };

  switch (columnType) {
    case "date":
    case "number":
      return (
        <div className="space-y-2">
          <Input
            placeholder="Min"
            value={minValue ?? ""}
            onChange={(e) => setMinValue(e.target.value)}
            onBlur={handleRangeChange}
            className="w-full"
          />
          <Input
            placeholder="Max"
            value={maxValue ?? ""}
            onChange={(e) => setMaxValue(e.target.value)}
            onBlur={handleRangeChange}
            className="w-full"
          />
        </div>
      );
    case "boolean":
      return (
        <Select
          value={column.getFilterValue()?.toString() ?? ""}
          onValueChange={(value) =>
            column.setFilterValue(value === "" ? undefined : value === "true")
          }
        >
          <SelectTrigger className="w-full">
            <SelectValue placeholder="Select..." />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="">All</SelectItem>
            <SelectItem value="true">True</SelectItem>
            <SelectItem value="false">False</SelectItem>
          </SelectContent>
        </Select>
      );
    default:
      return (
        <Input
          placeholder={`Filter ${column.columnDef.header}...`}
          value={(column.getFilterValue() as string) ?? ""}
          onChange={(e) => column.setFilterValue(e.target.value)}
          className="w-full"
        />
      );
  }
};

export function Table<T extends RowData>({
  data,
  columns,
  initialPageSize = 50,
}: AdvancedTableProps<T>) {
  const [sorting, setSorting] = useState<SortingState>([]);
  const [columnFilters, setColumnFilters] = useState<ColumnFiltersState>([]);
  const [globalFilter, setGlobalFilter] = useState("");
  const [pagination, setPagination] = useState<PaginationState>({
    pageIndex: 0,
    pageSize: initialPageSize,
  });
  const [columnVisibility, setColumnVisibility] = useState({});

  const table = useReactTable({
    data,
    columns,
    state: {
      sorting,
      columnFilters,
      globalFilter,
      pagination,
      columnVisibility,
    },
    onSortingChange: setSorting,
    onColumnFiltersChange: setColumnFilters,
    onGlobalFilterChange: setGlobalFilter,
    onPaginationChange: setPagination,
    onColumnVisibilityChange: setColumnVisibility,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    enableColumnResizing: true,
    columnResizeMode: "onChange",
  });

  const tableContainerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const resizeObserver = new ResizeObserver(() => {
      table.resetColumnSizing();
    });

    if (tableContainerRef.current) {
      resizeObserver.observe(tableContainerRef.current);
    }

    return () => {
      resizeObserver.disconnect();
    };
  }, [table]);

  return (
    <Card className="w-full">
      <CardContent className="p-4">
        <div className="flex flex-col space-y-4">
          <div className="flex justify-between items-center">
            <div className="relative w-64">
              <Input
                placeholder="Search all columns..."
                value={globalFilter ?? ""}
                onChange={(e) => setGlobalFilter(e.target.value)}
                className="pl-10 pr-4 py-2 w-full text-sm"
              />
              <Search
                className="absolute left-3 top-1/2 transform -translate-y-1/2 text-gray-400"
                size={16}
              />
            </div>
            <div className="flex items-center space-x-4">
              <Button
                onClick={() => {
                  setColumnFilters([]);
                  setGlobalFilter("");
                }}
              >
                Clear Filters
              </Button>
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button variant="outline">
                    <Columns className="mr-2 h-4 w-4" />
                    Columns
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end">
                  {table.getAllColumns().map((column) => {
                    return (
                      <DropdownMenuCheckboxItem
                        key={column.id}
                        className="capitalize"
                        checked={column.getIsVisible()}
                        onCheckedChange={(value) =>
                          column.toggleVisibility(!!value)
                        }
                      >
                        {column.id}
                      </DropdownMenuCheckboxItem>
                    );
                  })}
                </DropdownMenuContent>
              </DropdownMenu>
              <Select
                value={table.getState().pagination.pageSize.toString()}
                onValueChange={(value) => {
                  table.setPageSize(Number(value));
                }}
              >
                <SelectTrigger className="w-[140px] text-sm">
                  <SelectValue placeholder="Select page size" />
                </SelectTrigger>
                <SelectContent>
                  {[20, 50, 100, 200, 500].map((pageSize) => (
                    <SelectItem key={pageSize} value={pageSize.toString()}>
                      Show {pageSize}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>
          <div
            ref={tableContainerRef}
            className="overflow-auto rounded-md border"
            style={{ height: "calc(100vh - 240px)" }}
          >
            <table className="w-full border-collapse table-auto text-sm">
              <thead className="bg-gray-100 dark:bg-gray-800 sticky top-0 z-10">
                {table.getHeaderGroups().map((headerGroup) => (
                  <tr key={headerGroup.id}>
                    {headerGroup.headers.map((header) => (
                      <th
                        key={header.id}
                        className="text-left px-3 py-2 font-semibold text-gray-700 dark:text-gray-200 cursor-pointer relative"
                        style={{ width: header.getSize() }}
                      >
                        <div className="flex items-center justify-between">
                          <div
                            className="flex items-center space-x-2"
                            onClick={header.column.getToggleSortingHandler()}
                          >
                            {flexRender(
                              header.column.columnDef.header,
                              header.getContext()
                            )}
                            <div className="flex flex-col">
                              <ChevronUp
                                size={12}
                                className={
                                  header.column.getIsSorted() === "asc"
                                    ? "text-primary"
                                    : "text-gray-400"
                                }
                              />
                              <ChevronDown
                                size={12}
                                className={
                                  header.column.getIsSorted() === "desc"
                                    ? "text-primary"
                                    : "text-gray-400"
                                }
                              />
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
                                Filter{" "}
                                {header.column.columnDef.header as string}
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
                        {header.column.getCanResize() && (
                          <div
                            onMouseDown={header.getResizeHandler()}
                            onTouchStart={header.getResizeHandler()}
                            className={`absolute right-0 top-0 h-full w-1 cursor-col-resize select-none touch-none ${
                              header.column.getIsResizing()
                                ? "bg-primary"
                                : "bg-gray-300 dark:bg-gray-600"
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
                    className="hover:bg-gray-50 dark:hover:bg-gray-700 transition-colors"
                  >
                    {row.getVisibleCells().map((cell) => (
                      <td
                        key={cell.id}
                        className="px-3 py-2 border-t border-gray-200 dark:border-gray-600 truncate"
                        style={{ maxWidth: cell.column.getSize() }}
                      >
                        {flexRender(
                          cell.column.columnDef.cell,
                          cell.getContext()
                        )}
                      </td>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <div className="flex items-center justify-between mt-2 text-sm">
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
                  <TooltipContent>Next Page </TooltipContent>
                </Tooltip>
              </TooltipProvider>
              <TooltipProvider>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <Button
                      onClick={() =>
                        table.setPageIndex(table.getPageCount() - 1)
                      }
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
      </CardContent>
    </Card>
  );
}

export default Table;
