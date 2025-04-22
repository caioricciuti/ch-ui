/**
 * Format a datetime string into a more readable format
 * @param dateTimeStr The datetime string to format
 * @returns A formatted string representation of the datetime
 */
export const formatDateTime = (dateTimeStr: string): string => {
  try {
    const date = new Date(dateTimeStr);
    if (isNaN(date.getTime())) {
      return dateTimeStr;
    }
    return date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
  } catch {
    return dateTimeStr;
  }
}; 