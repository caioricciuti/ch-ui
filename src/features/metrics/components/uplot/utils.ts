// Minimal chart utilities and palette

export const PALETTE = [
  '#7EB26D', // green
  '#EAB839', // yellow
  '#6ED0E0', // light blue
  '#EF843C', // orange
  '#E24D42', // red
  '#447EBC', // blue
  '#BA43A9', // purple
  '#705DA0', // violet
  '#508642', // dark green
  '#CCA300', // dark yellow
  '#1F78C1', // alt blue
  '#C15C17', // dark orange
  '#890F02', // dark red
  '#0A437C', // darker blue
  '#6D1F62', // dark purple
  '#584477', // dark violet
];

export function formatValue(
  value: number | null,
  decimals: number = 2,
  unit?: string
): string {
  if (value == null) return '-';
  const formatted = value.toLocaleString('en-US', {
    minimumFractionDigits: 0,
    maximumFractionDigits: decimals,
  });
  return unit ? `${formatted} ${unit}` : formatted;
}

export function formatBytes(value: number | null, decimals: number = 1): string {
  if (value == null) return '-';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let n = Math.abs(value);
  let i = 0;
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024;
    i++;
  }
  const sign = value < 0 ? '-' : '';
  return `${sign}${n.toFixed(decimals)} ${units[i]}`;
}

export function formatShortNumber(value: number | null): string {
  if (value == null) return '-';
  const n = Math.abs(value);
  const sign = value < 0 ? '-' : '';
  if (n >= 1_000_000_000) return `${sign}${(n / 1_000_000_000).toFixed(1)}B`;
  if (n >= 1_000_000) return `${sign}${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${sign}${(n / 1_000).toFixed(1)}k`;
  return `${value}`;
}
