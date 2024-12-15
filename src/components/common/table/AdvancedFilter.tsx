import React, { useState, useRef } from "react";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Separator } from "@/components/ui/separator";
import { Badge } from "@/components/ui/badge";
import {
  X,
  Filter,
  ChevronDown,
  Equal,
  ArrowRightFromLine,
  ArrowLeftFromLine,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { FilterOperator, ParsedFilter } from "./Filter";

interface FilterCondition {
  id: string;
  column: string;
  operator: FilterOperator;
  value: string;
}

interface AdvancedFilterProps {
  columns: { id: string; header: string }[];
  onFilterChange: (filters: FilterCondition[]) => void;
  className?: string;
}

const OPERATORS = [
  { label: "equals", value: "=", icon: Equal },
  { label: "contains", value: "~", icon: Equal },
  { label: "greater than", value: ">", icon: Equal },
  { label: "less than", value: "<", icon: Equal },
  { label: "greater than or equal", value: ">=", icon: ArrowRightFromLine },
  { label: "less than or equal", value: "<=", icon: ArrowLeftFromLine },
] as const;

export function AdvancedFilter({
  columns,
  onFilterChange,
  className,
}: AdvancedFilterProps) {
  const [open, setOpen] = useState(false);
  const [filters, setFilters] = useState<FilterCondition[]>([]);
  const [currentFilter, setCurrentFilter] = useState<Partial<FilterCondition>>({
    id: Math.random().toString(36).substring(7),
  });
  const [step, setStep] = useState<"column" | "operator" | "value">("column");
  const inputRef = useRef<HTMLInputElement>(null);

  const handleAddFilter = () => {
    if (currentFilter.column && currentFilter.operator && currentFilter.value) {
      setFilters([...filters, currentFilter as FilterCondition]);
      onFilterChange([...filters, currentFilter as FilterCondition]);
      setCurrentFilter({
        id: Math.random().toString(36).substring(7),
      });
      setStep("column");
    }
  };

  const handleRemoveFilter = (id: string) => {
    const newFilters = filters.filter((f) => f.id !== id);
    setFilters(newFilters);
    onFilterChange(newFilters);
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Enter" && currentFilter.value) {
      handleAddFilter();
    }
  };

  return (
    <div className={cn("flex flex-col gap-2", className)}>
      <div className="flex items-center gap-2 flex-wrap">
        <Popover open={open} onOpenChange={setOpen}>
          <PopoverTrigger asChild>
            <Button
              variant="outline"
              className={cn(
                "h-9 w-full justify-between md:w-[240px]",
                open && "border-primary"
              )}
              aria-label={
                filters.length
                  ? `${filters.length} active filters`
                  : "Add filter"
              }
              aria-expanded={open}
              role="combobox"
            >
              <div className="flex items-center gap-2">
                <Filter className="h-4 w-4" />
                <span className="text-sm font-normal">
                  {filters.length ? `${filters.length} filters` : "Add filter"}
                </span>
              </div>
              <ChevronDown className="h-4 w-4 opacity-50" />
            </Button>
          </PopoverTrigger>
          <PopoverContent
            className="w-[320px] p-0"
            align="start"
            aria-description="Filter configuration panel"
          >
            <div className="p-2 flex flex-col gap-2">
              {/* Active filters */}
              {filters.length > 0 && (
                <>
                  <div className="flex flex-wrap gap-1">
                    {filters.map((filter) => (
                      <Badge
                        key={filter.id}
                        variant="secondary"
                        className="h-7 gap-1 pl-2 pr-1 flex items-center"
                      >
                        <span className="text-xs">
                          {filter.column} {filter.operator} {filter.value}
                        </span>
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-4 w-4 p-0 hover:bg-transparent"
                          onClick={() => handleRemoveFilter(filter.id)}
                          aria-label={`Remove filter for ${filter.column}`}
                        >
                          <X className="h-3 w-3" />
                        </Button>
                      </Badge>
                    ))}
                  </div>
                  <Separator />
                </>
              )}

              {/* Filter builder */}
              <div className="flex flex-col gap-2">
                {step === "column" && (
                  <Command>
                    <CommandInput
                      placeholder="Search columns..."
                      aria-label="Search columns"
                    />
                    <CommandList>
                      <CommandEmpty>No columns found.</CommandEmpty>
                      <CommandGroup>
                        <ScrollArea className="h-[200px]">
                          {columns.map((column) => (
                            <CommandItem
                              key={column.id}
                              value={column.id}
                              onSelect={(value) => {
                                setCurrentFilter((prev) => ({
                                  ...prev,
                                  column: value,
                                }));
                                setStep("operator");
                              }}
                            >
                              {column.header}
                            </CommandItem>
                          ))}
                        </ScrollArea>
                      </CommandGroup>
                    </CommandList>
                  </Command>
                )}

                {step === "operator" && (
                  <div className="grid grid-cols-2 gap-1">
                    {OPERATORS.map((op) => (
                      <Button
                        key={op.value}
                        variant="outline"
                        className="justify-start gap-2"
                        onClick={() => {
                          setCurrentFilter((prev) => ({
                            ...prev,
                            operator: op.value,
                          }));
                          setStep("value");
                        }}
                      >
                        <op.icon className="h-4 w-4" />
                        {op.label}
                      </Button>
                    ))}
                  </div>
                )}

                {step === "value" && (
                  <div className="flex gap-2">
                    <Input
                      placeholder="Enter value..."
                      value={currentFilter.value || ""}
                      onChange={(e) =>
                        setCurrentFilter((prev) => ({
                          ...prev,
                          value: e.target.value,
                        }))
                      }
                      onKeyDown={handleKeyDown}
                      autoFocus
                      aria-label="Filter value"
                    />
                    <Button onClick={handleAddFilter} aria-label="Add filter">
                      Add
                    </Button>
                  </div>
                )}

                {/* Steps indicator */}
                <div className="flex items-center justify-between text-xs text-muted-foreground mt-2">
                  <div className="flex items-center gap-2">
                    <div
                      className={cn(
                        "h-1 w-8 rounded",
                        step === "column" ? "bg-primary" : "bg-muted"
                      )}
                    />
                    <div
                      className={cn(
                        "h-1 w-8 rounded",
                        step === "operator" ? "bg-primary" : "bg-muted"
                      )}
                    />
                    <div
                      className={cn(
                        "h-1 w-8 rounded",
                        step === "value" ? "bg-primary" : "bg-muted"
                      )}
                    />
                  </div>
                  <span>
                    {step === "column" && "Select column"}
                    {step === "operator" && "Choose operator"}
                    {step === "value" && "Enter value"}
                  </span>
                </div>
              </div>
            </div>
          </PopoverContent>
        </Popover>
      </div>
    </div>
  );
}
