// Permission hierarchy definitions for ClickHouse GRANT system
import { escapeIdentifier, formatScopeSQL } from "@/features/admin/utils/sqlEscape";

export type ScopeType = "global" | "database" | "table";

export interface PermissionNode {
  id: string;
  name: string;
  sqlPrivilege: string;
  children?: PermissionNode[];
  allowedScopes: ScopeType[];
  description?: string;
}

export interface PermissionScope {
  type: ScopeType;
  database?: string;
  table?: string;
}

export interface GrantedPermission {
  permissionId: string;
  scope: PermissionScope;
  isPartialRevoke?: boolean;
  grantOption?: boolean;
  columns?: string[];
}

// Helper to create permission nodes
const perm = (
  id: string,
  name: string,
  sqlPrivilege: string,
  allowedScopes: ScopeType[],
  children?: PermissionNode[],
  description?: string
): PermissionNode => ({
  id,
  name,
  sqlPrivilege,
  allowedScopes,
  children,
  description,
});

// ALTER COLUMN sub-permissions
const ALTER_COLUMN_CHILDREN: PermissionNode[] = [
  perm("ALTER_ADD_COLUMN", "ADD COLUMN", "ALTER ADD COLUMN", ["database", "table"], undefined, "Add new columns to tables"),
  perm("ALTER_DROP_COLUMN", "DROP COLUMN", "ALTER DROP COLUMN", ["database", "table"], undefined, "Remove columns from tables"),
  perm("ALTER_MODIFY_COLUMN", "MODIFY COLUMN", "ALTER MODIFY COLUMN", ["database", "table"], undefined, "Modify column types or defaults"),
  perm("ALTER_COMMENT_COLUMN", "COMMENT COLUMN", "ALTER COMMENT COLUMN", ["database", "table"], undefined, "Add or modify column comments"),
  perm("ALTER_CLEAR_COLUMN", "CLEAR COLUMN", "ALTER CLEAR COLUMN", ["database", "table"], undefined, "Clear column data in partitions"),
  perm("ALTER_RENAME_COLUMN", "RENAME COLUMN", "ALTER RENAME COLUMN", ["database", "table"], undefined, "Rename columns"),
];

// ALTER INDEX sub-permissions
const ALTER_INDEX_CHILDREN: PermissionNode[] = [
  perm("ALTER_ORDER_BY", "ORDER BY", "ALTER ORDER BY", ["database", "table"], undefined, "Modify table sorting key"),
  perm("ALTER_SAMPLE_BY", "SAMPLE BY", "ALTER SAMPLE BY", ["database", "table"], undefined, "Modify table sampling key"),
  perm("ALTER_ADD_INDEX", "ADD INDEX", "ALTER ADD INDEX", ["database", "table"], undefined, "Add secondary indexes"),
  perm("ALTER_DROP_INDEX", "DROP INDEX", "ALTER DROP INDEX", ["database", "table"], undefined, "Remove secondary indexes"),
  perm("ALTER_MATERIALIZE_INDEX", "MATERIALIZE INDEX", "ALTER MATERIALIZE INDEX", ["database", "table"], undefined, "Materialize index data"),
  perm("ALTER_CLEAR_INDEX", "CLEAR INDEX", "ALTER CLEAR INDEX", ["database", "table"], undefined, "Clear index data in partitions"),
];

// ALTER CONSTRAINT sub-permissions
const ALTER_CONSTRAINT_CHILDREN: PermissionNode[] = [
  perm("ALTER_ADD_CONSTRAINT", "ADD CONSTRAINT", "ALTER ADD CONSTRAINT", ["database", "table"], undefined, "Add table constraints"),
  perm("ALTER_DROP_CONSTRAINT", "DROP CONSTRAINT", "ALTER DROP CONSTRAINT", ["database", "table"], undefined, "Remove table constraints"),
];

// ALTER TTL sub-permissions
const ALTER_TTL_CHILDREN: PermissionNode[] = [
  perm("ALTER_MATERIALIZE_TTL", "MATERIALIZE TTL", "ALTER MATERIALIZE TTL", ["database", "table"], undefined, "Apply TTL rules to existing data"),
];

// ALTER TABLE sub-permissions
const ALTER_TABLE_CHILDREN: PermissionNode[] = [
  perm("ALTER_UPDATE", "UPDATE", "ALTER UPDATE", ["database", "table"], undefined, "Perform ALTER TABLE UPDATE"),
  perm("ALTER_DELETE", "DELETE", "ALTER DELETE", ["database", "table"], undefined, "Perform ALTER TABLE DELETE"),
  perm("ALTER_COLUMN", "COLUMN", "ALTER COLUMN", ["database", "table"], ALTER_COLUMN_CHILDREN, "Column operations"),
  perm("ALTER_INDEX", "INDEX", "ALTER INDEX", ["database", "table"], ALTER_INDEX_CHILDREN, "Index operations"),
  perm("ALTER_CONSTRAINT", "CONSTRAINT", "ALTER CONSTRAINT", ["database", "table"], ALTER_CONSTRAINT_CHILDREN, "Constraint operations"),
  perm("ALTER_TTL", "TTL", "ALTER TTL", ["database", "table"], ALTER_TTL_CHILDREN, "TTL operations"),
  perm("ALTER_SETTINGS", "SETTINGS", "ALTER SETTINGS", ["database", "table"], undefined, "Modify table settings"),
  perm("ALTER_MOVE_PARTITION", "MOVE PARTITION", "ALTER MOVE PARTITION", ["database", "table"], undefined, "Move partitions between disks"),
  perm("ALTER_FETCH_PARTITION", "FETCH PARTITION", "ALTER FETCH PARTITION", ["database", "table"], undefined, "Fetch partitions from replica"),
  perm("ALTER_FREEZE_PARTITION", "FREEZE PARTITION", "ALTER FREEZE PARTITION", ["database", "table"], undefined, "Create partition backups"),
];

// ALTER LIVE VIEW sub-permissions
const ALTER_LIVE_VIEW_CHILDREN: PermissionNode[] = [
  perm("ALTER_LIVE_VIEW_REFRESH", "REFRESH", "ALTER LIVE VIEW REFRESH", ["database", "table"], undefined, "Refresh live view"),
  perm("ALTER_LIVE_VIEW_MODIFY_QUERY", "MODIFY QUERY", "ALTER LIVE VIEW MODIFY QUERY", ["database", "table"], undefined, "Modify live view query"),
];

// CREATE sub-permissions
const CREATE_CHILDREN: PermissionNode[] = [
  perm("CREATE_DATABASE", "DATABASE", "CREATE DATABASE", ["global", "database"], undefined, "Create databases"),
  perm("CREATE_TABLE", "TABLE", "CREATE TABLE", ["global", "database"], undefined, "Create tables"),
  perm("CREATE_VIEW", "VIEW", "CREATE VIEW", ["global", "database"], undefined, "Create views"),
  perm("CREATE_DICTIONARY", "DICTIONARY", "CREATE DICTIONARY", ["global", "database"], undefined, "Create dictionaries"),
  perm("CREATE_FUNCTION", "FUNCTION", "CREATE FUNCTION", ["global"], undefined, "Create functions"),
];

// DROP sub-permissions
const DROP_CHILDREN: PermissionNode[] = [
  perm("DROP_DATABASE", "DATABASE", "DROP DATABASE", ["global", "database"], undefined, "Drop databases"),
  perm("DROP_TABLE", "TABLE", "DROP TABLE", ["global", "database", "table"], undefined, "Drop tables"),
  perm("DROP_VIEW", "VIEW", "DROP VIEW", ["global", "database", "table"], undefined, "Drop views"),
  perm("DROP_DICTIONARY", "DICTIONARY", "DROP DICTIONARY", ["global", "database"], undefined, "Drop dictionaries"),
  perm("DROP_FUNCTION", "FUNCTION", "DROP FUNCTION", ["global"], undefined, "Drop functions"),
];

// Main permission hierarchy
export const PERMISSION_HIERARCHY: PermissionNode[] = [
  // Data access permissions
  perm("SELECT", "SELECT", "SELECT", ["global", "database", "table"], undefined, "Read data from tables"),
  perm("INSERT", "INSERT", "INSERT", ["global", "database", "table"], undefined, "Insert data into tables"),

  // ALTER permissions (table/view scope)
  perm("ALTER", "ALTER", "ALTER", ["database", "table"], [
    perm("ALTER_TABLE", "TABLE", "ALTER TABLE", ["database", "table"], ALTER_TABLE_CHILDREN, "Table modifications"),
    perm("ALTER_LIVE_VIEW", "LIVE VIEW", "ALTER LIVE VIEW", ["database", "table"], ALTER_LIVE_VIEW_CHILDREN, "Live view modifications"),
  ], "Modify tables and views"),

  // Administrative ALTER permissions (no table scope)
  perm("ALTER_DATABASE", "ALTER DATABASE", "ALTER DATABASE", ["global", "database"], undefined, "Modify database settings"),
  perm("ALTER_USER", "ALTER USER", "ALTER USER", ["global"], undefined, "Modify user accounts"),
  perm("ALTER_ROLE", "ALTER ROLE", "ALTER ROLE", ["global"], undefined, "Modify roles"),
  perm("ALTER_QUOTA", "ALTER QUOTA", "ALTER QUOTA", ["global"], undefined, "Modify quotas"),
  perm("ALTER_ROW_POLICY", "ALTER ROW POLICY", "ALTER ROW POLICY", ["global"], undefined, "Modify row policies"),
  perm("ALTER_SETTINGS_PROFILE", "ALTER SETTINGS PROFILE", "ALTER SETTINGS PROFILE", ["global"], undefined, "Modify settings profiles"),

  // Schema permissions
  perm("CREATE", "CREATE", "CREATE", ["global", "database"], CREATE_CHILDREN, "Create database objects"),
  perm("DROP", "DROP", "DROP", ["global", "database", "table"], DROP_CHILDREN, "Drop database objects"),
  perm("TRUNCATE", "TRUNCATE", "TRUNCATE", ["global", "database", "table"], undefined, "Truncate tables"),

  // Other common permissions
  perm("OPTIMIZE", "OPTIMIZE", "OPTIMIZE", ["global", "database", "table"], undefined, "Optimize table parts"),
  perm("SHOW", "SHOW", "SHOW", ["global", "database", "table"], undefined, "View table structure"),
  perm("KILL_QUERY", "KILL QUERY", "KILL QUERY", ["global"], undefined, "Terminate running queries"),
  perm("SYSTEM", "SYSTEM", "SYSTEM", ["global"], undefined, "System administration"),

  // Access management permissions
  perm("CREATE_USER", "CREATE USER", "CREATE USER", ["global"], undefined, "Create user accounts"),
  perm("CREATE_ROLE", "CREATE ROLE", "CREATE ROLE", ["global"], undefined, "Create roles"),
  perm("ROLE_ADMIN", "ROLE ADMIN", "ROLE ADMIN", ["global"], undefined, "Grant roles to others"),
  perm("SHOW_ACCESS", "SHOW ACCESS", "SHOW ACCESS", ["global"], undefined, "View access control objects"),

  // Backup and recovery
  perm("BACKUP", "BACKUP", "BACKUP", ["global"], undefined, "Create backups"),
  perm("UNDROP_TABLE", "UNDROP TABLE", "UNDROP TABLE", ["global", "database"], undefined, "Restore dropped tables"),

  // Additional query control
  perm("KILL_TRANSACTION", "KILL TRANSACTION", "KILL TRANSACTION", ["global"], undefined, "Terminate running transactions"),

  // Table engine control
  perm("TABLE_ENGINE", "TABLE ENGINE", "TABLE ENGINE", ["global"], undefined, "Control which table engines can be used"),

  // Introspection
  perm("INTROSPECTION", "INTROSPECTION", "INTROSPECTION", ["global"], undefined, "Use introspection functions (addressToLine, demangle)"),
  perm("dictGet", "dictGet", "dictGet", ["global", "database"], undefined, "Access dictionary functions"),

  // Data sources
  perm("SOURCES", "SOURCES", "SOURCES", ["global"], [
    perm("S3", "S3", "S3", ["global"], undefined, "Access S3 data sources"),
    perm("HDFS", "HDFS", "HDFS", ["global"], undefined, "Access HDFS data sources"),
    perm("MYSQL", "MYSQL", "MYSQL", ["global"], undefined, "Access MySQL data sources"),
    perm("POSTGRES", "POSTGRES", "POSTGRES", ["global"], undefined, "Access PostgreSQL data sources"),
    perm("SQLITE", "SQLITE", "SQLITE", ["global"], undefined, "Access SQLite data sources"),
    perm("ODBC", "ODBC", "ODBC", ["global"], undefined, "Access ODBC data sources"),
    perm("JDBC", "JDBC", "JDBC", ["global"], undefined, "Access JDBC data sources"),
    perm("MONGO", "MONGO", "MONGO", ["global"], undefined, "Access MongoDB data sources"),
    perm("REDIS", "REDIS", "REDIS", ["global"], undefined, "Access Redis data sources"),
    perm("KAFKA", "KAFKA", "KAFKA", ["global"], undefined, "Access Kafka data sources"),
    perm("NATS", "NATS", "NATS", ["global"], undefined, "Access NATS data sources"),
    perm("RABBITMQ", "RABBITMQ", "RABBITMQ", ["global"], undefined, "Access RabbitMQ data sources"),
    perm("URL", "URL", "URL", ["global"], undefined, "Access URL data sources"),
    perm("FILE", "FILE", "FILE", ["global"], undefined, "Access file data sources"),
    perm("REMOTE", "REMOTE", "REMOTE", ["global"], undefined, "Access remote data sources"),
    perm("CLUSTER", "CLUSTER", "CLUSTER", ["global"], undefined, "Access cluster data sources"),
    perm("AZURE", "AZURE", "AZURE", ["global"], undefined, "Access Azure data sources"),
    perm("HIVE", "HIVE", "HIVE", ["global"], undefined, "Access Hive data sources"),
  ], "Data source access permissions"),
];

// Utility functions

/**
 * Get all permission IDs in the tree (flattened)
 */
export function getAllPermissionIds(nodes: PermissionNode[] = PERMISSION_HIERARCHY): string[] {
  const ids: string[] = [];
  for (const node of nodes) {
    ids.push(node.id);
    if (node.children) {
      ids.push(...getAllPermissionIds(node.children));
    }
  }
  return ids;
}

/**
 * Find a permission node by ID
 */
export function findPermissionById(
  id: string,
  nodes: PermissionNode[] = PERMISSION_HIERARCHY
): PermissionNode | undefined {
  for (const node of nodes) {
    if (node.id === id) return node;
    if (node.children) {
      const found = findPermissionById(id, node.children);
      if (found) return found;
    }
  }
  return undefined;
}

/**
 * Get all child IDs (recursive) for a permission
 */
export function getChildIds(node: PermissionNode): string[] {
  const ids: string[] = [];
  if (node.children) {
    for (const child of node.children) {
      ids.push(child.id);
      ids.push(...getChildIds(child));
    }
  }
  return ids;
}

/**
 * Find parent ID for a given permission ID
 */
export function findParentId(
  targetId: string,
  nodes: PermissionNode[] = PERMISSION_HIERARCHY,
  parentId?: string
): string | undefined {
  for (const node of nodes) {
    if (node.id === targetId) return parentId;
    if (node.children) {
      const found = findParentId(targetId, node.children, node.id);
      if (found !== undefined) return found;
    }
  }
  return undefined;
}

/**
 * Get all ancestor IDs for a permission (parent, grandparent, etc.)
 */
export function getAncestorIds(id: string): string[] {
  const ancestors: string[] = [];
  let currentId: string | undefined = id;

  while (currentId) {
    const parentId = findParentId(currentId);
    if (parentId) {
      ancestors.push(parentId);
      currentId = parentId;
    } else {
      break;
    }
  }

  return ancestors;
}

/**
 * Format scope for display
 */
export function formatScope(scope: PermissionScope): string {
  switch (scope.type) {
    case "global":
      return "*.*";
    case "database":
      return scope.database ? `${scope.database}.*` : "*.*";
    case "table":
      return scope.database && scope.table
        ? `${scope.database}.${scope.table}`
        : scope.database
          ? `${scope.database}.*`
          : "*.*";
    default:
      return "*.*";
  }
}

/**
 * Generate SQL GRANT statement for a permission
 */
export function generateGrantSql(
  permission: PermissionNode,
  scope: PermissionScope,
  username: string
): string {
  const scopeStr = formatScopeSQL(scope);
  return `GRANT ${permission.sqlPrivilege} ON ${scopeStr} TO ${escapeIdentifier(username)}`;
}
