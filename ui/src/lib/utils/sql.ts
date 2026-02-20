const WRITE_PATTERN = /^\s*(INSERT|CREATE|DROP|ALTER|TRUNCATE|RENAME|ATTACH|DETACH|OPTIMIZE|GRANT|REVOKE|KILL|SYSTEM|SET|USE)\b/i
const LIMIT_PATTERN = /\bLIMIT\s+\d+/i

/** Check if a query is a write (DDL/DML) operation */
export function isWriteQuery(query: string): boolean {
  // Strip leading SQL comments
  const stripped = query.replace(/^\s*--.*$/gm, '').trim()
  return WRITE_PATTERN.test(stripped)
}

/** Check if a query already has a LIMIT clause */
export function hasLimit(query: string): boolean {
  return LIMIT_PATTERN.test(query)
}

/** Append a default LIMIT if the query doesn't have one (for SELECT only) */
export function appendDefaultLimit(query: string, limit = 1000): string {
  if (isWriteQuery(query) || hasLimit(query)) return query
  return query.replace(/;\s*$/, '') + ` LIMIT ${limit}`
}
