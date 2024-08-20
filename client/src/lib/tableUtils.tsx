// tableUtils.tsx
export const getColumnType = (value: any): string => {
  if (value instanceof Date) return "date";
  if (typeof value === "number") return "number";
  if (typeof value === "boolean") return "boolean";
  if (typeof value === "string") {
    // Check for ISO date string
    if (/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}.\d{3}Z$/.test(value))
      return "date";
    // Check for common date formats
    if (
      /^\d{4}-\d{2}-\d{2}$/.test(value) ||
      /^\d{2}\/\d{2}\/\d{4}$/.test(value) ||
      /^\d{2}\/\d{2}\/\d{2}$/.test(value) ||
      /^\d{2}-\d{2}-\d{4}$/.test(value) ||
      /^\d{2}-\d{2}-\d{2}$/.test(value) ||
      /^\d{4}\/\d{2}\/\d{2}$/.test(value) ||
      /^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/.test(value) ||
      /^\d{2}\/\d{2}\/\d{4} \d{2}:\d{2}:\d{2}$/.test(value) ||
      /^\d{2}\/\d{2}\/\d{2} \d{2}:\d{2}:\d{2}$/.test(value) ||
      /^\d{2}-\d{2}-\d{4} \d{2}:\d{2}:\d{2}$/.test(value) ||
      /^\d{2}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/.test(value)
    )
      return "date";
  }
  return "string";
};
