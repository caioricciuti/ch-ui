import React, { useState, useCallback } from "react";
import { X, Plus, ChevronDown } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
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
import { cn } from "@/lib/utils";

interface FilterCondition {
  id: string;
  column: string;
  operator: string;
  value: string;
}

interface SmartFilterProps {
  columns: { id: string; header: string }[];
  onFilterChange: (filters: FilterCondition[]) => void;
  className?: string;
}

const OPERATORS = [
  { label: "equals", value: "=" },
  { label: "contains", value: "~" },
  { label: "greater than", value: ">" },
  { label: "less than", value: "<" },
  { label: "greater than or equal", value: ">=" },
  { label: "less than or equal", value: "<=" },
];

export function SmartFilter({
  columns,
  onFilterChange,
  className,
}: SmartFilterProps) {
  const [open, setOpen] = useState(false);
  const [filters, setFilters] = useState<FilterCondition[]>([]);
  const [searchValue, setSearchValue] = useState("");
  const [currentFilter, setCurrentFilter] = useState<Partial<FilterCondition>>({
    id: Math.random().toString(36).substring(7),
  });


  const handleAddFilter = useCallback(() => {
    if (currentFilter.column && currentFilter.operator && currentFilter.value) {
      const newFilter = {
        ...currentFilter,
        id: Math.random().toString(36).substring(7),
      } as FilterCondition;
      
      const newFilters = [...filters, newFilter];
      setFilters(newFilters);
      onFilterChange(newFilters);
      setCurrentFilter({ id: Math.random().toString(36).substring(7) });
      setSearchValue("");
    }
  }, [currentFilter, filters, onFilterChange]);

  const handleRemoveFilter = useCallback((id: string) => {
    const newFilters = filters.filter((f) => f.id !== id);
    setFilters(newFilters);
    onFilterChange(newFilters);
  }, [filters, onFilterChange]);

  const handleKeyDown = useCallback((e: React.KeyboardEvent) => {
    if (e.key === "Enter" && currentFilter.value) {
      handleAddFilter();
    }
  }, [currentFilter.value, handleAddFilter]);

  return (
    <div className={cn("space-y-2", className)}>
      <div className="flex items-center gap-2">
        <div className="flex-none max-w-[400px] overflow-hidden">
          <div className="flex items-center gap-2 py-1">
            {filters.map((filter) => (
              <Badge
                key={filter.id}
                variant="secondary"
                className="flex-none h-4 px-2 flex items-center gap-1 text-xs"
              >
                <span>
                  {columns.find((c) => c.id === filter.column)?.header || filter.column}
                </span>
                <span className="text-muted-foreground">{filter.operator}</span>
                <span>{filter.value}</span>
                <Button
                  variant="ghost"
                  size="icon"
                  className="h-4 w-4 p-0 hover:bg-transparent"
                  onClick={() => handleRemoveFilter(filter.id)}
                >
                  <X className="h-3 w-3" />
                </Button>
              </Badge>
            ))}
          </div>
        </div>

        <Popover open={open} onOpenChange={setOpen}>
          <PopoverTrigger asChild>
            <Button
              variant="outline"
              size="sm"
              className="h-7 px-2 gap-1"
              onClick={() => setOpen(true)}
              aria-label="Add filter"
            >
              <Plus className="h-4 w-4" />
              Add filter
              <ChevronDown className="h-3 w-3 opacity-50" />
            </Button>
          </PopoverTrigger>
          <PopoverContent className="w-[300px] p-2" align="start" aria-description="Filter configuration panel">
            <div className="space-y-4">
              {/* Column selection */}
              {!currentFilter.column && (
                <div className="space-y-2">
                  <label className="text-xs font-medium" htmlFor="column-select">Column</label>
                  <Command className="text-xs">
                    <CommandInput 
                      placeholder="Search columns..."
                      id="column-select"
                      className="text-xs"
                    />
                    <CommandList>
                      <CommandEmpty className="text-xs">No columns found.</CommandEmpty>
                      <CommandGroup heading="Columns" className="text-xs">
                        {columns.map((column) => (
                          <CommandItem
                            key={column.id}
                            value={column.id}
                            onSelect={(value) => {
                              setCurrentFilter((prev) => ({ ...prev, column: value }));
                            }}
                            className="text-xs"
                          >
                            {column.header}
                          </CommandItem>
                        ))}
                      </CommandGroup>
                    </CommandList>
                  </Command>
                </div>
              )}

              {/* Operator selection */}
              {currentFilter.column && !currentFilter.operator && (
                <div className="space-y-2">
                  <div className="flex items-center justify-between">
                    <label className="text-xs font-medium" htmlFor="operator-select">Operator</label>
                    <Button 
                      variant="ghost" 
                      size="sm" 
                      className="text-xs h-6 px-2"
                      onClick={() => setCurrentFilter(prev => ({ ...prev, column: undefined }))}
                    >
                      Back
                    </Button>
                  </div>
                  <Command className="text-xs">
                    <CommandInput 
                      placeholder="Search operators..."
                      id="operator-select"
                      className="text-xs"
                    />
                    <CommandList>
                      <CommandEmpty className="text-xs">No operators found.</CommandEmpty>
                      <CommandGroup heading="Operators" className="text-xs">
                        {OPERATORS.map((operator) => (
                          <CommandItem
                            key={operator.value}
                            value={operator.value}
                            onSelect={(value) => {
                              setCurrentFilter((prev) => ({ ...prev, operator: value }));
                            }}
                            className="text-xs"
                          >
                            {operator.label}
                          </CommandItem>
                        ))}
                      </CommandGroup>
                    </CommandList>
                  </Command>
                </div>
              )}

              {/* Value input */}
              {currentFilter.column && currentFilter.operator && (
                <div className="space-y-2">
                  <div className="flex items-center justify-between">
                    <label className="text-xs font-medium" htmlFor="filter-value">Value</label>
                    <Button 
                      variant="ghost" 
                      size="sm" 
                      className="text-xs h-6 px-2"
                      onClick={() => setCurrentFilter(prev => ({ ...prev, operator: undefined }))}
                    >
                      Back
                    </Button>
                  </div>
                  <div className="flex gap-2">
                    <Input
                      id="filter-value"
                      placeholder="Enter value..."
                      value={currentFilter.value || ""}
                      onChange={(e) =>
                        setCurrentFilter((prev) => ({
                          ...prev,
                          value: e.target.value,
                        }))
                      }
                      onKeyDown={handleKeyDown}
                      aria-label="Filter value"
                      className="text-xs"
                    />
                    <Button
                      onClick={handleAddFilter}
                      disabled={!currentFilter.value}
                      aria-label="Add filter"
                      className="text-xs"
                    >
                      Add
                    </Button>
                  </div>
                </div>
              )}
            </div>
          </PopoverContent>
        </Popover>
      </div>
    </div>
  );
}
