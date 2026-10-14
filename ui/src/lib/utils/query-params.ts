// Detect ClickHouse bind parameters of the form {name:Type} in a SQL string.
// ClickHouse binds these via HTTP param_<name>=value URL params, which CH-UI
// forwards on execution. Text inside strings and comments is ignored.

import { maskNonCode } from '../editor/statements'

export interface QueryParam {
  name: string
  type: string
}

// {name:Type} — name is a SQL identifier, type is everything up to the closing brace
// (supports parametric types like Array(UInt32), Decimal(10, 2), etc.).
const PARAM_RE = /\{\s*([a-zA-Z_]\w*)\s*:\s*([^{}]+?)\s*\}/g

/**
 * Extract the distinct bind parameters referenced in a query, in first-seen order.
 * A name that appears multiple times is returned once (first type wins).
 */
export function detectQueryParams(sql: string): QueryParam[] {
  if (!sql) return []
  const code = maskNonCode(sql)
  const seen = new Map<string, string>()
  let m: RegExpExecArray | null
  PARAM_RE.lastIndex = 0
  while ((m = PARAM_RE.exec(code)) !== null) {
    const name = m[1]
    const type = m[2].trim()
    if (!seen.has(name)) seen.set(name, type)
  }
  return Array.from(seen, ([name, type]) => ({ name, type }))
}

// String, Nullable(String), LowCardinality(String), LowCardinality(Nullable(String)).
const STRING_TYPE_RE = /^(?:(?:Nullable|LowCardinality)\s*\(\s*)*String(?:\s*\))*$/

/**
 * True when a run cannot bind the parameter. An empty value is missing,
 * except for String types, where '' is a value like any other.
 */
export function isParamValueMissing(param: QueryParam, value: string | undefined): boolean {
  if (STRING_TYPE_RE.test(param.type)) return false
  return !(value ?? '').trim()
}

/** Parse a saved query's stored parameters (a JSON object string) into string values. */
export function parseSavedParams(raw: string | null | undefined): Record<string, string> {
  if (!raw || !raw.trim()) return {}
  try {
    const parsed: unknown = JSON.parse(raw)
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return {}
    const out: Record<string, string> = {}
    for (const [k, v] of Object.entries(parsed as Record<string, unknown>)) {
      if (v !== null && v !== undefined) out[k] = String(v)
    }
    return out
  } catch {
    return {}
  }
}

/**
 * Parameter defaults to store with a saved query: for every parameter the SQL
 * uses, the value typed in the tab, else the previously stored default. Empty
 * values are never sent, so saving cannot erase a stored default.
 */
export function paramDefaultsToSave(
  names: string[],
  values: Record<string, string> | undefined,
  stored: Record<string, string> | undefined,
): Record<string, string> {
  const out: Record<string, string> = {}
  for (const name of names) {
    const typed = values?.[name] ?? ''
    const value = typed.trim() ? typed : (stored?.[name] ?? '')
    if (value.trim()) out[name] = value
  }
  return out
}
