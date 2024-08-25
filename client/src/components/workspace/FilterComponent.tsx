import { useState, useEffect } from "react";
import { Input } from "@/components/ui/input";
import { Column } from "@tanstack/react-table";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

type Operator =
  | "equals"
  | "contains"
  | "starts_with"
  | "ends_with"
  | "greater_than"
  | "less_than";

const operators: { value: Operator; label: string }[] = [
  { value: "equals", label: "Equals" },
  { value: "contains", label: "Contains" },
  { value: "starts_with", label: "Starts with" },
  { value: "ends_with", label: "Ends with" },
  { value: "greater_than", label: "Greater than" },
  { value: "less_than", label: "Less than" },
];

const FilterComponent = ({ column }: { column: Column<any, unknown> }) => {
  const [operator, setOperator] = useState<Operator>("equals");
  const [value, setValue] = useState<string>("");

  useEffect(() => {
    const filterValue = column.getFilterValue() as
      | { operator: Operator; value: string }
      | undefined;
    if (filterValue) {
      setOperator(filterValue.operator);
      setValue(filterValue.value);
    }
  }, [column]);

  const updateFilter = (newOperator: Operator, newValue: string) => {
    const filterValue = newValue
      ? { operator: newOperator, value: newValue }
      : null;
    column.setFilterValue(filterValue);
  };

  return (
    <div className="space-y-2">
      <Select
        value={operator}
        onValueChange={(newOperator: Operator) => {
          setOperator(newOperator);
          updateFilter(newOperator, value);
        }}
      >
        <SelectTrigger className="w-full">
          <SelectValue placeholder="Select operator" />
        </SelectTrigger>
        <SelectContent>
          {operators.map((op) => (
            <SelectItem key={op.value} value={op.value}>
              {op.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <Input
        placeholder={`Filter ${column.columnDef.header as string}...`}
        value={value}
        onChange={(e) => {
          setValue(e.target.value);
          updateFilter(operator, e.target.value);
        }}
        className="w-full"
      />
    </div>
  );
};

export default FilterComponent;
