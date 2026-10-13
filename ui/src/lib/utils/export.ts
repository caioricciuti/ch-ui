import type { ColumnMeta } from '../types/query'
import { getDisplayType } from './ch-types'

const NUMERIC_TEXT = /^[+-]?(\d+(\.\d*)?|\.\d+)([eE][+-]?\d+)?$/

function normalizeScalar(val: unknown): string {
  if (val === null || val === undefined) return ''
  if (typeof val === 'object') return JSON.stringify(val)
  return String(val)
}

/** True when the value is a plain number (JS number/bigint, or numeric text in a numeric column). */
function isNumericValue(val: unknown, chType: string | undefined): boolean {
  if (typeof val === 'number' || typeof val === 'bigint') return true
  if (typeof val !== 'string' || !chType) return false
  return getDisplayType(chType) === 'number' && NUMERIC_TEXT.test(val)
}

function escapeDelimited(val: unknown, delimiter: ',' | '\t', chType?: string): string {
  let s = normalizeScalar(val)
  // Prevent CSV formula injection: prefix dangerous leading characters with a single quote
  // so spreadsheet applications don't interpret them as formulas. Numbers such as -5 are
  // left alone: a spreadsheet reads them as numbers, not formulas.
  if (s.length > 0 && /^[=+\-@\t\r]/.test(s) && !isNumericValue(val, chType)) {
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
  const rows = data.map(row => row.map((v, i) => escapeDelimited(v, ',', meta[i]?.type)).join(','))
  return header + '\n' + rows.join('\n')
}

/** Generate TSV (TabSeparated) */
export function generateTSV(meta: ColumnMeta[], data: unknown[][]): string {
  const header = meta.map(c => escapeDelimited(c.name, '\t')).join('\t')
  const rows = data.map(row => row.map((v, i) => escapeDelimited(v, '\t', meta[i]?.type)).join('\t'))
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

function quoteSQL(value: string): string {
  return `'${escapeSQLString(value)}'`
}

/** Strip Nullable(...) / LowCardinality(...) wrappers. */
function unwrapType(t: string): string {
  let cur = t.trim()
  for (;;) {
    const m = /^(Nullable|LowCardinality)\((.*)\)$/s.exec(cur)
    if (!m) return cur
    cur = m[2].trim()
  }
}

/** Split a type argument list on top-level commas: "String, Array(Int8)" -> two parts. */
function splitTypeArgs(args: string): string[] {
  const out: string[] = []
  let depth = 0
  let quote = ''
  let start = 0
  for (let i = 0; i < args.length; i++) {
    const ch = args[i]
    if (quote) {
      if (ch === '\\') i++
      else if (ch === quote) quote = ''
      continue
    }
    if (ch === "'" || ch === '`' || ch === '"') quote = ch
    else if (ch === '(') depth++
    else if (ch === ')') depth--
    else if (ch === ',' && depth === 0) {
      out.push(args.slice(start, i).trim())
      start = i + 1
    }
  }
  const last = args.slice(start).trim()
  if (last) out.push(last)
  return out
}

/** Parse "Name(args)" into its name and argument types. */
function parseType(t: string): { name: string; args: string[] } {
  const u = unwrapType(t)
  const open = u.indexOf('(')
  if (open < 0 || !u.endsWith(')')) return { name: u, args: [] }
  return { name: u.slice(0, open), args: splitTypeArgs(u.slice(open + 1, -1)) }
}

/** Element type of a Tuple argument, dropping the name in named tuples ("a String" -> "String"). */
function tupleElementType(arg: string): string {
  const m = /^[A-Za-z_][A-Za-z0-9_]*\s+([A-Z].*)$/s.exec(arg)
  return m ? m[1] : arg
}

/** Render a value as a ClickHouse literal, guided by the column type when known. */
export function toSQLLiteral(v: unknown, chType = ''): string {
  if (v === null || v === undefined) return 'NULL'
  const { name, args } = parseType(chType)

  if (name === 'Array' && Array.isArray(v)) {
    return `[${v.map((x) => toSQLLiteral(x, args[0] ?? '')).join(', ')}]`
  }
  if (name === 'Tuple' && v !== null && typeof v === 'object') {
    const items = Array.isArray(v) ? v : Object.values(v as Record<string, unknown>)
    const types = args.map(tupleElementType)
    return `(${items.map((x, i) => toSQLLiteral(x, types[i] ?? '')).join(', ')})`
  }
  if (name === 'Map' && v !== null && typeof v === 'object' && !Array.isArray(v)) {
    const entries = Object.entries(v as Record<string, unknown>)
    const parts = entries.map(([k, x]) => `${toSQLLiteral(k, args[0] ?? 'String')}, ${toSQLLiteral(x, args[1] ?? '')}`)
    return `map(${parts.join(', ')})`
  }

  const kind = name ? getDisplayType(name) : 'unknown'
  if (typeof v === 'boolean') return v ? 'true' : 'false'
  if (kind === 'number') {
    if (typeof v === 'number' && Number.isFinite(v)) return String(v)
    if (typeof v === 'bigint') return v.toString()
    if (typeof v === 'string' && NUMERIC_TEXT.test(v)) return v
    return quoteSQL(normalizeScalar(v))
  }
  if (kind === 'string' || kind === 'date' || kind === 'bool') {
    return quoteSQL(normalizeScalar(v))
  }
  if (kind === 'json') {
    // JSON / Object columns, and nested types whose value shape did not match.
    return quoteSQL(typeof v === 'object' ? JSON.stringify(v) : String(v))
  }

  // Unknown column type: infer from the JS value.
  if (typeof v === 'number' && Number.isFinite(v)) return String(v)
  if (typeof v === 'bigint') return v.toString()
  if (Array.isArray(v)) return `[${v.map((x) => toSQLLiteral(x)).join(', ')}]`
  if (typeof v === 'object') return quoteSQL(JSON.stringify(v))
  return quoteSQL(String(v))
}

/** Generate INSERT INTO ... VALUES SQL */
export function generateSQLInsert(meta: ColumnMeta[], data: unknown[][], table = 'result_set'): string {
  if (!meta.length) return ''
  const columns = meta.map((c) => `\`${c.name.replace(/`/g, '``')}\``).join(', ')
  const values = data.map((row) => {
    const fields = row.map((v, i) => toSQLLiteral(v, meta[i]?.type ?? ''))
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
  downloadBlob(new Blob([content], { type: mimeType }), filename)
}

export function downloadBlob(blob: Blob, filename: string): void {
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  // A large file may still be read when click() returns.
  setTimeout(() => URL.revokeObjectURL(url), 10_000)
}
