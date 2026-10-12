/**
 * Text shown in the cell inspector: objects, arrays and maps pretty-printed
 * as JSON, strings that hold JSON pretty-printed too, everything else as is.
 */
export function inspectCellText(value: unknown): { text: string; json: boolean } {
  if (value === null || value === undefined) return { text: 'NULL', json: false }
  if (typeof value === 'object') return { text: JSON.stringify(value, null, 2), json: true }
  if (typeof value === 'string') {
    const trimmed = value.trim()
    if ((trimmed.startsWith('{') && trimmed.endsWith('}')) || (trimmed.startsWith('[') && trimmed.endsWith(']'))) {
      try {
        return { text: JSON.stringify(JSON.parse(trimmed), null, 2), json: true }
      } catch {
        // Not JSON after all: show the raw text.
      }
    }
    return { text: value, json: false }
  }
  return { text: String(value), json: false }
}

/** Plain text copied for a cell: objects as compact JSON, NULL as an empty string. */
export function cellCopyText(value: unknown): string {
  if (value === null || value === undefined) return ''
  if (typeof value === 'object') return JSON.stringify(value)
  return String(value)
}
