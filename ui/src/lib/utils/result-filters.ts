// Filtering & ordering of query results (issue #117).
//
// Two execution modes share these definitions:
//  - client: sort/filter the in-memory rows of a complete result set
//  - server: wrap the original query in SELECT * FROM (...) WHERE/ORDER BY and
//    re-run it, so truncated or huge results are ordered over the full dataset
//    (BigQuery-style, per the maintainer's note on the issue).

import type { ColumnMeta } from '../types/query'
import { getDisplayType } from './ch-types'

export type FilterOperator =
  | 'contains'
  | 'notContains'
  | 'eq'
  | 'neq'
  | 'gt'
  | 'gte'
  | 'lt'
  | 'lte'
  | 'isNull'
  | 'isNotNull'

export interface ColumnFilter {
  column: string
  operator: FilterOperator
  value: string
}

export interface ResultSort {
  column: string
  dir: 'asc' | 'desc'
}

export const OPERATOR_LABELS: Record<FilterOperator, string> = {
  contains: 'contains',
  notContains: 'does not contain',
  eq: '=',
  neq: '≠',
  gt: '>',
  gte: '≥',
  lt: '<',
  lte: '≤',
  isNull: 'is NULL',
  isNotNull: 'is not NULL',
}

/** True when the operator needs no value input. */
export function isUnaryOperator(op: FilterOperator): boolean {
  return op === 'isNull' || op === 'isNotNull'
}

/**
 * Validate a filter value for a column type. Numeric columns require a numeric
 * value — otherwise the server-side SQL would fail (`id > 'abc'`) while client
 * mode silently fell back to string comparison.
 */
export function isValidFilterValue(chType: string, op: FilterOperator, value: string): boolean {
  if (isUnaryOperator(op)) return true
  if (getDisplayType(chType) === 'number') return NUM_RE.test(value)
  return true
}

/** Operators that make sense for a column's ClickHouse type. */
export function operatorsFor(chType: string): FilterOperator[] {
  switch (getDisplayType(chType)) {
    case 'number':
    case 'date':
      return ['eq', 'neq', 'gt', 'gte', 'lt', 'lte', 'isNull', 'isNotNull']
    case 'bool':
      return ['eq', 'neq', 'isNull', 'isNotNull']
    default:
      return ['contains', 'notContains', 'eq', 'neq', 'gt', 'gte', 'lt', 'lte', 'isNull', 'isNotNull']
  }
}

// ── Client-side comparison ───────────────────────────────────────

const INT_RE = /^-?\d+$/
const NUM_RE = /^-?\d+(\.\d+)?([eE][+-]?\d+)?$/

function isNullish(v: unknown): boolean {
  return v === null || v === undefined
}

function asComparableString(v: unknown): string {
  if (typeof v === 'string') return v
  if (typeof v === 'object') return JSON.stringify(v) ?? ''
  return String(v)
}

/**
 * Type-aware comparison of two non-null cell values from the same column.
 * Int64/UInt64 arrive as quoted strings from ClickHouse JSON output, so
 * integer-looking strings are compared as BigInt to preserve precision.
 */
export function compareValues(a: unknown, b: unknown, chType: string): number {
  switch (getDisplayType(chType)) {
    case 'number': {
      const sa = String(a)
      const sb = String(b)
      if (INT_RE.test(sa) && INT_RE.test(sb)) {
        const ba = BigInt(sa)
        const bb = BigInt(sb)
        return ba < bb ? -1 : ba > bb ? 1 : 0
      }
      const na = Number(a)
      const nb = Number(b)
      if (!Number.isNaN(na) && !Number.isNaN(nb)) return na < nb ? -1 : na > nb ? 1 : 0
      return sa < sb ? -1 : sa > sb ? 1 : 0
    }
    case 'date': {
      // ISO-formatted strings compare correctly lexicographically
      const sa = String(a)
      const sb = String(b)
      return sa < sb ? -1 : sa > sb ? 1 : 0
    }
    case 'bool': {
      const na = a === true || a === 1 || a === 'true' ? 1 : 0
      const nb = b === true || b === 1 || b === 'true' ? 1 : 0
      return na - nb
    }
    default: {
      const sa = asComparableString(a)
      const sb = asComparableString(b)
      return sa < sb ? -1 : sa > sb ? 1 : 0
    }
  }
}

/**
 * Return a sorted copy of the rows. NULLs sort last in both directions,
 * matching ClickHouse's default NULLS LAST so client and server modes agree.
 */
export function sortRows(data: unknown[][], meta: ColumnMeta[], sort: ResultSort): unknown[][] {
  const ci = meta.findIndex((c) => c.name === sort.column)
  if (ci < 0) return data
  const chType = meta[ci].type
  const sign = sort.dir === 'asc' ? 1 : -1
  return [...data].sort((ra, rb) => {
    const a = ra[ci]
    const b = rb[ci]
    const aNull = isNullish(a)
    const bNull = isNullish(b)
    if (aNull && bNull) return 0
    if (aNull) return 1
    if (bNull) return -1
    return sign * compareValues(a, b, chType)
  })
}

// ── Client-side filtering ────────────────────────────────────────

function matchesFilter(value: unknown, filter: ColumnFilter, chType: string): boolean {
  switch (filter.operator) {
    case 'isNull':
      return isNullish(value)
    case 'isNotNull':
      return !isNullish(value)
  }
  if (isNullish(value)) return false

  switch (filter.operator) {
    case 'contains':
      return asComparableString(value).toLowerCase().includes(filter.value.toLowerCase())
    case 'notContains':
      return !asComparableString(value).toLowerCase().includes(filter.value.toLowerCase())
    case 'eq':
    case 'neq': {
      let equal: boolean
      if (getDisplayType(chType) === 'number' && NUM_RE.test(filter.value)) {
        equal = compareValues(value, filter.value, chType) === 0
      } else if (getDisplayType(chType) === 'bool') {
        const want = /^(true|1)$/i.test(filter.value)
        const got = value === true || value === 1 || value === 'true'
        equal = want === got
      } else {
        equal = asComparableString(value) === filter.value
      }
      return filter.operator === 'eq' ? equal : !equal
    }
    case 'gt':
      return compareValues(value, filter.value, chType) > 0
    case 'gte':
      return compareValues(value, filter.value, chType) >= 0
    case 'lt':
      return compareValues(value, filter.value, chType) < 0
    case 'lte':
      return compareValues(value, filter.value, chType) <= 0
  }
}

/** Apply all filters (AND-combined) to the rows. */
export function filterRows(data: unknown[][], meta: ColumnMeta[], filters: ColumnFilter[]): unknown[][] {
  if (filters.length === 0) return data
  const resolved = filters
    .map((f) => ({ f, ci: meta.findIndex((c) => c.name === f.column) }))
    .filter((r) => r.ci >= 0)
  if (resolved.length === 0) return data
  return data.filter((row) => resolved.every(({ f, ci }) => matchesFilter(row[ci], f, meta[ci].type)))
}

// ── Server-side SQL generation ───────────────────────────────────

/** Quote a ClickHouse identifier in backticks (escapes \ and `). */
export function escapeIdentifier(name: string): string {
  return '`' + name.replace(/\\/g, '\\\\').replace(/`/g, '\\`') + '`'
}

/** Quote a ClickHouse string literal in single quotes (escapes \ and '). */
export function escapeStringLiteral(value: string): string {
  return "'" + value.replace(/\\/g, '\\\\').replace(/'/g, "\\'") + "'"
}

function filterToSQL(filter: ColumnFilter, chType: string): string {
  const col = escapeIdentifier(filter.column)
  const display = getDisplayType(chType)

  switch (filter.operator) {
    case 'isNull':
      return `${col} IS NULL`
    case 'isNotNull':
      return `${col} IS NOT NULL`
    case 'contains':
      return `positionCaseInsensitive(toString(${col}), ${escapeStringLiteral(filter.value)}) > 0`
    case 'notContains':
      return `positionCaseInsensitive(toString(${col}), ${escapeStringLiteral(filter.value)}) = 0`
  }

  // Comparison operators: use a bare numeric literal for numeric columns when
  // the value parses as a number; otherwise compare as strings.
  const op = { eq: '=', neq: '!=', gt: '>', gte: '>=', lt: '<', lte: '<=' }[filter.operator]
  if (display === 'number' && NUM_RE.test(filter.value)) {
    return `${col} ${op} ${filter.value}`
  }
  if (display === 'bool' && (filter.operator === 'eq' || filter.operator === 'neq')) {
    const literal = /^(true|1)$/i.test(filter.value) ? 'true' : 'false'
    return `${col} ${op} ${literal}`
  }
  if (display === 'number' || display === 'date' || display === 'string') {
    return `${col} ${op} ${escapeStringLiteral(filter.value)}`
  }
  return `toString(${col}) ${op} ${escapeStringLiteral(filter.value)}`
}

/** Strip trailing whitespace and semicolons from a statement. */
function stripStatementTail(sql: string): string {
  return sql.replace(/[\s;]+$/g, '')
}

/**
 * True when the SQL can safely be wrapped as a subquery: a single SELECT/WITH
 * statement without FORMAT or INTO OUTFILE clauses. Conservative — anything
 * questionable falls back to client-side mode.
 */
export function isWrappableQuery(sql: string): boolean {
  const body = stripStatementTail(sql.trim())
  if (!body) return false
  // Multiple statements (naive: any interior semicolon — a false positive on a
  // semicolon inside a string literal just means we fall back to client mode)
  if (body.includes(';')) return false
  // Strip leading line/block comments to find the first keyword
  const stripped = body.replace(/^(\s*(--[^\n]*\n|\/\*[\s\S]*?\*\/))*\s*/, '')
  if (!/^(SELECT|WITH)\b/i.test(stripped)) return false
  if (/\bFORMAT\s+\w+\s*$/i.test(body)) return false
  if (/\bINTO\s+OUTFILE\b/i.test(body)) return false
  return true
}

/**
 * Wrap the original query so ClickHouse applies the filters/order over the
 * full result set. The comment marker identifies these re-queries in
 * system.query_log and CH-UI's audit log.
 */
export function buildWrappedQuery(
  baseSql: string,
  filters: ColumnFilter[],
  sort: ResultSort | null,
  meta: ColumnMeta[],
  limit: number,
): string {
  const typeOf = (column: string) => meta.find((c) => c.name === column)?.type ?? 'String'
  const where = filters.map((f) => filterToSQL(f, typeOf(f.column))).join(' AND ')
  const orderBy = sort ? `${escapeIdentifier(sort.column)} ${sort.dir === 'asc' ? 'ASC' : 'DESC'}` : ''

  let sql = `/* ch-ui:result-filter */ SELECT * FROM (\n${stripStatementTail(baseSql.trim())}\n) AS __chui_src`
  if (where) sql += `\nWHERE ${where}`
  if (orderBy) sql += `\nORDER BY ${orderBy}`
  sql += `\nLIMIT ${Math.max(1, Math.round(limit))}`
  return sql
}

/** Cycle a column's sort state: none → asc → desc → none. */
export function cycleSort(current: ResultSort | null, column: string): ResultSort | null {
  if (!current || current.column !== column) return { column, dir: 'asc' }
  if (current.dir === 'asc') return { column, dir: 'desc' }
  return null
}
