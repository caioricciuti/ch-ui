import { useMemo, useState, useEffect, useCallback } from "react";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@/components/ui/select";
import { Column, Row } from "@tanstack/react-table";
import { getColumnType } from "@/lib/tableUtils";
import { format } from "date-fns";
import { Calendar as CalendarIcon } from "lucide-react";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Calendar } from "@/components/ui/calendar";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";

const FilterComponent = ({
  column,
  table,
}: {
  column: Column<any, unknown>;
  table: any;
}) => {
  const columnValues = useMemo(() => {
    return table
      .getPreFilteredRowModel()
      .flatRows.map((row: Row<any>) => row.getValue(column.id));
  }, [table, column.id]);

  const columnType = useMemo(() => {
    const nonNullValues = columnValues.filter((value: any) => value != null);
    return nonNullValues.length > 0
      ? getColumnType(nonNullValues[0])
      : "string";
  }, [columnValues]);

  const [min, max] = useMemo(() => {
    if (columnType === "number" || columnType === "date") {
      const values = columnValues.filter((value: any) => value != null);
      return [Math.min(...values), Math.max(...values)];
    }
    return [null, null];
  }, [columnValues, columnType]);

  const [minValue, setMinValue] = useState<Date | undefined>(
    min ? new Date(min) : undefined
  );
  const [maxValue, setMaxValue] = useState<Date | undefined>(
    max ? new Date(max) : undefined
  );

  const handleRangeChange = useCallback(() => {
    column.setFilterValue([
      minValue ? format(minValue, "yyyy-MM-dd") : undefined,
      maxValue ? format(maxValue, "yyyy-MM-dd") : undefined,
    ]);
  }, [column, minValue, maxValue]);

  useEffect(() => {
    handleRangeChange();
  }, [handleRangeChange]);

  const uniqueValues = useMemo(() => {
    return Array.from(new Set(columnValues)).sort();
  }, [columnValues]);

  const DatePickerWithPopover = ({
    value,
    onChange,
    placeholder,
  }: {
    value: Date | undefined;
    onChange: (date: Date | undefined) => void;
    placeholder: string;
  }) => {
    return (
      <Popover>
        <PopoverTrigger asChild>
          <Button
            variant={"outline"}
            className={cn(
              "w-full justify-start text-left font-normal",
              !value && "text-muted-foreground"
            )}
          >
            <CalendarIcon className="mr-2 h-4 w-4" />
            {value ? format(value, "PPP") : <span>{placeholder}</span>}
          </Button>
        </PopoverTrigger>
        <PopoverContent className="w-auto p-0">
          <Calendar
            mode="single"
            selected={value}
            onSelect={onChange}
            initialFocus
          />
        </PopoverContent>
      </Popover>
    );
  };

  switch (columnType) {
    case "date":
      return (
        <div className="space-y-2">
          <DatePickerWithPopover
            value={minValue}
            onChange={setMinValue}
            placeholder="Min date"
          />
          <DatePickerWithPopover
            value={maxValue}
            onChange={setMaxValue}
            placeholder="Max date"
          />
        </div>
      );
    case "number":
      return (
        <div className="space-y-2">
          <Input
            type="number"
            placeholder="Min"
            value={minValue?.toString() ?? ""}
            onChange={(e) =>
              setMinValue(
                e.target.value ? new Date(Number(e.target.value)) : undefined
              )
            }
            className="w-full"
          />
          <Input
            type="number"
            placeholder="Max"
            value={maxValue?.toString() ?? ""}
            onChange={(e) =>
              setMaxValue(
                e.target.value ? new Date(Number(e.target.value)) : undefined
              )
            }
            className="w-full"
          />
        </div>
      );
    case "boolean":
      return (
        <Select
          value={(column.getFilterValue() as string) ?? ""}
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
      return uniqueValues.length <= 10 ? (
        <Select
          value={(column.getFilterValue() as string) ?? ""}
          onValueChange={(value) => column.setFilterValue(value)}
        >
          <SelectTrigger className="w-full">
            <SelectValue placeholder="Select..." />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="">All</SelectItem>
            {uniqueValues.map((value: any) => (
              <SelectItem key={value} value={value}>
                {value}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      ) : (
        <Input
          placeholder={`Filter ${column.columnDef.header as string}...`}
          value={(column.getFilterValue() as string) ?? ""}
          onChange={(e) => column.setFilterValue(e.target.value)}
          className="w-full"
        />
      );
  }
};

export default FilterComponent;
