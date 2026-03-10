const WRITE_PATTERN = /^\s*(INSERT|CREATE|DROP|ALTER|TRUNCATE|RENAME|ATTACH|DETACH|OPTIMIZE|GRANT|REVOKE|KILL|SYSTEM|SET|USE)\b/i

/** Check if a query is a write (DDL/DML) operation */
export function isWriteQuery(query: string): boolean {
  // Strip leading SQL comments
  const stripped = query.replace(/^\s*--.*$/gm, '').trim()
  return WRITE_PATTERN.test(stripped)
}
