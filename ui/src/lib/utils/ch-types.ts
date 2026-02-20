/** Map ClickHouse types to display categories for cell rendering */
export type DisplayType = 'number' | 'string' | 'date' | 'bool' | 'json' | 'null' | 'unknown'

export function getDisplayType(chType: string): DisplayType {
  const t = chType.replace(/Nullable\((.+)\)/, '$1').replace(/LowCardinality\((.+)\)/, '$1')

  if (/^(U?Int|Float|Decimal)/.test(t)) return 'number'
  if (/^(Date|DateTime)/.test(t)) return 'date'
  if (/^(Bool)/.test(t)) return 'bool'
  if (/^(String|FixedString|Enum|UUID|IPv[46])/.test(t)) return 'string'
  if (/^(Array|Map|Tuple|Nested|JSON)/.test(t)) return 'json'

  return 'unknown'
}

/** Check if a value should be right-aligned (numbers) */
export function isRightAligned(chType: string): boolean {
  return getDisplayType(chType) === 'number'
}
