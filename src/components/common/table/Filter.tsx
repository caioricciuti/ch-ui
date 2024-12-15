import { Row, FilterFn, Column } from "@tanstack/react-table";

// Custom Filter Types
type FilterOperator = "=" | ">" | "<" | ">=" | "<=" | ":" | "~" | "AND" | "OR";

interface ParsedFilter {
  column: string;
  operator: FilterOperator;
  value: string;
}

interface FilterCell {
  getValue: () => any;
  column: Column<any, unknown>;
}

// Filter Parser
const parseFilterExpression = (filterValue: string): ParsedFilter | null => {
  // Support patterns like:
  // age:25       -> exact match
  // age=25       -> exact match
  // age>25       -> greater than
  // age>=25      -> greater than or equal
  // age<25       -> less than
  // age<=25      -> less than or equal
  // name~John    -> contains (case insensitive)
  const filterRegex = /^([a-zA-Z_][a-zA-Z0-9_]*)\s*(:|=|>|<|>=|<=|~)\s*(.+)$/;
  const match = filterValue.trim().match(filterRegex);

  if (!match) return null;

  const [, column, operator, value] = match;
  return {
    column: column.trim().toLowerCase(),
    operator: operator as FilterOperator,
    value: value.trim().replace(/^["'](.*)["']$/, "$1"),
  };
};

// Enhanced Custom Filter
const CHUIFilter: FilterFn<any> = (
  row: Row<any>,
  columnId: string,
  filterValue: string
) => {
  // Handle empty filter
  if (!filterValue?.trim()) return true;

  // Try to parse the filter expression
  const parsedFilter = parseFilterExpression(filterValue);
  if (!parsedFilter) {
    // If no valid filter expression, fall back to contains search across all columns
    const searchValue = filterValue.toLowerCase().trim();
    return row.getAllCells().some((cell: FilterCell) => {
      const value = cell.getValue();
      if (value == null) return false;
      return String(value).toLowerCase().includes(searchValue);
    });
  }

  // Get the cell value for the specified column
  const { column, operator, value } = parsedFilter;

  // Find the matching column (case insensitive)
  const matchingCell = row.getAllCells().find((cell: FilterCell) => 
    cell.column.id.toLowerCase() === column
  );

  // If the column doesn't exist, try global search
  if (!matchingCell) {
    const searchValue = value.toLowerCase();
    return row.getAllCells().some((cell: FilterCell) => {
      const cellValue = cell.getValue();
      if (cellValue == null) return false;
      return String(cellValue).toLowerCase().includes(searchValue);
    });
  }

  const cellValue = matchingCell.getValue();
  
  // Special handling for null/undefined values
  if (cellValue == null) {
    return value.toLowerCase() === "null" || value.toLowerCase() === "undefined";
  }

  // Convert both values to appropriate types for comparison
  const typedCellValue = String(cellValue).trim();
  const typedFilterValue = value.trim();

  // Numeric comparison if both values are numbers
  const numericCell = !isNaN(Number(typedCellValue));
  const numericFilter = !isNaN(Number(typedFilterValue));

  if (numericCell && numericFilter) {
    const numCell = Number(typedCellValue);
    const numFilter = Number(typedFilterValue);

    switch (operator) {
      case ":":
      case "=":
        return numCell === numFilter;
      case ">":
        return numCell > numFilter;
      case "<":
        return numCell < numFilter;
      case ">=":
        return numCell >= numFilter;
      case "<=":
        return numCell <= numFilter;
      case "~":
        return String(numCell).includes(String(numFilter));
      default:
        return false;
    }
  }

  // String comparison with proper case handling
  const cellValueLower = typedCellValue.toLowerCase();
  const filterValueLower = typedFilterValue.toLowerCase();

  switch (operator) {
    case ":":
    case "=":
      return cellValueLower === filterValueLower;
    case "~":
      return cellValueLower.includes(filterValueLower);
    case ">":
      return typedCellValue > typedFilterValue;
    case "<":
      return typedCellValue < typedFilterValue;
    case ">=":
      return typedCellValue >= typedFilterValue;
    case "<=":
      return typedCellValue <= typedFilterValue;
    default:
      return false;
  }
};

export { CHUIFilter, parseFilterExpression, type FilterOperator, type ParsedFilter };
