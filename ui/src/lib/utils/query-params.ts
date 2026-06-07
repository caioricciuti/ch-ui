// Detect ClickHouse bind parameters of the form {name:Type} in a SQL string.
// ClickHouse binds these via HTTP param_<name>=value URL params, which CH-UI
// forwards on execution (a Pro feature).

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
  const seen = new Map<string, string>()
  let m: RegExpExecArray | null
  PARAM_RE.lastIndex = 0
  while ((m = PARAM_RE.exec(sql)) !== null) {
    const name = m[1]
    const type = m[2].trim()
    if (!seen.has(name)) seen.set(name, type)
  }
  return Array.from(seen, ([name, type]) => ({ name, type }))
}

/** True when the query references at least one bind parameter. */
export function hasQueryParams(sql: string): boolean {
  return detectQueryParams(sql).length > 0
}
