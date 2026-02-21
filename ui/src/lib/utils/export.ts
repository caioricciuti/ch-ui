import type { ColumnMeta } from '../types/query'

function normalizeScalar(val: unknown): string {
  if (val === null || val === undefined) return ''
  if (typeof val === 'object') return JSON.stringify(val)
  return String(val)
}

function escapeDelimited(val: unknown, delimiter: ',' | '\t'): string {
  let s = normalizeScalar(val)
  // Prevent CSV formula injection: prefix dangerous leading characters with a single quote
  // so spreadsheet applications don't interpret them as formulas
  if (s.length > 0 && /^[=+\-@\t\r]/.test(s)) {
    s = "'" + s
  }
  if (s.includes(delimiter) || s.includes('"') || s.includes('\n') || s.includes('\r')) {
    return '"' + s.replace(/"/g, '""') + '"'
  }
  return s
}

function rowsToObjects(meta: ColumnMeta[], data: unknown[][]): Record<string, unknown>[] {
  const names = meta.map(c => c.name)
  return data.map((row) => {
    const obj: Record<string, unknown> = {}
    for (let j = 0; j < names.length; j++) obj[names[j]] = row[j] ?? null
    return obj
  })
}

/** Generate RFC 4180 compliant CSV from query results */
export function generateCSV(meta: ColumnMeta[], data: unknown[][]): string {
  const header = meta.map(c => escapeDelimited(c.name, ',')).join(',')
  const rows = data.map(row => row.map(v => escapeDelimited(v, ',')).join(','))
  return header + '\n' + rows.join('\n')
}

/** Generate TSV (TabSeparated) */
export function generateTSV(meta: ColumnMeta[], data: unknown[][]): string {
  const header = meta.map(c => escapeDelimited(c.name, '\t')).join('\t')
  const rows = data.map(row => row.map(v => escapeDelimited(v, '\t')).join('\t'))
  return header + '\n' + rows.join('\n')
}

/** Generate JSONEachRow / JSONLines from query results */
export function generateJSONLines(meta: ColumnMeta[], data: unknown[][]): string {
  return rowsToObjects(meta, data).map((obj) => JSON.stringify(obj)).join('\n')
}

/** Generate JSON from query results (array of objects) */
export function generateJSON(meta: ColumnMeta[], data: unknown[][]): string {
  return JSON.stringify(rowsToObjects(meta, data), null, 2)
}

/** Generate JSONCompact-like payload */
export function generateJSONCompact(meta: ColumnMeta[], data: unknown[][]): string {
  return JSON.stringify({
    meta,
    data,
    rows: data.length,
  }, null, 2)
}

/** Generate markdown table */
export function generateMarkdown(meta: ColumnMeta[], data: unknown[][]): string {
  if (!meta.length) return ''

  const header = '| ' + meta.map((c) => c.name).join(' | ') + ' |'
  const separator = '| ' + meta.map(() => '---').join(' | ') + ' |'
  const rows = data.map((row) => {
    const cells = row.map((v) => normalizeScalar(v).replace(/\|/g, '\\|').replace(/\n/g, '<br/>'))
    return '| ' + cells.join(' | ') + ' |'
  })
  return [header, separator, ...rows].join('\n')
}

function escapeSQLString(value: string): string {
  return value.replace(/\\/g, '\\\\').replace(/'/g, "\\'")
}

/** Generate INSERT INTO ... VALUES SQL */
export function generateSQLInsert(meta: ColumnMeta[], data: unknown[][], table = 'result_set'): string {
  if (!meta.length) return ''
  const columns = meta.map((c) => `\`${c.name.replace(/`/g, '``')}\``).join(', ')
  const values = data.map((row) => {
    const fields = row.map((v) => {
      if (v === null || v === undefined) return 'NULL'
      if (typeof v === 'number' && Number.isFinite(v)) return String(v)
      if (typeof v === 'boolean') return v ? '1' : '0'
      if (typeof v === 'object') return `'${escapeSQLString(JSON.stringify(v))}'`
      return `'${escapeSQLString(String(v))}'`
    })
    return `(${fields.join(', ')})`
  })
  return `INSERT INTO \`${table.replace(/`/g, '``')}\` (${columns}) VALUES\n${values.join(',\n')};`
}

/** Generate lightweight XML export */
export function generateXML(meta: ColumnMeta[], data: unknown[][]): string {
  const rows = rowsToObjects(meta, data)
  const xmlTagName = (name: string): string => {
    const cleaned = name.replace(/[^a-zA-Z0-9_.-]/g, '_')
    return /^[a-zA-Z_]/.test(cleaned) ? cleaned : `c_${cleaned}`
  }
  const escapeXml = (s: string): string => s
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&apos;')

  const xmlRows = rows.map((row) => {
    const cols = meta.map((col) => {
      const value = row[col.name]
      const content = value === null || value === undefined ? '' : escapeXml(typeof value === 'object' ? JSON.stringify(value) : String(value))
      const tag = xmlTagName(col.name)
      return `    <${tag}>${content}</${tag}>`
    }).join('\n')
    return `  <row>\n${cols}\n  </row>`
  }).join('\n')

  return `<?xml version="1.0" encoding="UTF-8"?>\n<result>\n${xmlRows}\n</result>`
}

/** Copy text to clipboard */
export async function copyToClipboard(text: string): Promise<void> {
  await navigator.clipboard.writeText(text)
}

/** Download content as a file */
export function downloadFile(content: string, filename: string, mimeType: string): void {
  const blob = new Blob([content], { type: mimeType })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)
}
