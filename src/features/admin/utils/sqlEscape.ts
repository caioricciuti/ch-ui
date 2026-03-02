/**
 * Escapes a ClickHouse identifier by wrapping in backticks and escaping internal backticks.
 * Use for usernames, role names, database names, table names, cluster names.
 */
export function escapeIdentifier(name: string): string {
  return '`' + name.replace(/`/g, '``') + '`';
}

/**
 * Escapes a string literal for use inside single quotes in SQL.
 * Doubles single quotes per SQL standard.
 */
export function escapeStringLiteral(value: string): string {
  return value.replace(/\\/g, '\\\\').replace(/'/g, "\\'");
}

/**
 * Formats a permission scope into escaped SQL syntax.
 * Returns: *.*, `db`.*, `db`.`table`
 */
export function formatScopeSQL(scope: { type: string; database?: string; table?: string }): string {
  switch (scope.type) {
    case "global":
      return "*.*";
    case "database":
      return scope.database ? `${escapeIdentifier(scope.database)}.*` : "*.*";
    case "table":
      if (scope.database && scope.table) {
        return `${escapeIdentifier(scope.database)}.${escapeIdentifier(scope.table)}`;
      }
      return scope.database ? `${escapeIdentifier(scope.database)}.*` : "*.*";
    default:
      return "*.*";
  }
}
