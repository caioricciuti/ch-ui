// Privilege definitions for DBeaver-style privilege management

export interface PrivilegeDefinition {
  id: string;
  sql: string;
  label: string;
  description: string;
  scopable: boolean; // Can be scoped to database.table
}

export interface PrivilegeGroup {
  name: string;
  privileges: PrivilegeDefinition[];
}

/**
 * Table Privileges - can be scoped to database.table
 */
export const TABLE_PRIVILEGES: PrivilegeDefinition[] = [
  {
    id: "SELECT",
    sql: "SELECT",
    label: "SELECT",
    description: "Read data from tables",
    scopable: true,
  },
  {
    id: "INSERT",
    sql: "INSERT",
    label: "INSERT",
    description: "Insert data into tables",
    scopable: true,
  },
  {
    id: "ALTER",
    sql: "ALTER",
    label: "ALTER",
    description: "Modify table structure",
    scopable: true,
  },
  {
    id: "DELETE",
    sql: "ALTER DELETE",
    label: "DELETE",
    description: "Delete rows from tables",
    scopable: true,
  },
  {
    id: "CREATE",
    sql: "CREATE",
    label: "CREATE",
    description: "Create new databases and tables",
    scopable: true,
  },
  {
    id: "CREATE_VIEW",
    sql: "CREATE VIEW",
    label: "CREATE VIEW",
    description: "Create new views",
    scopable: true,
  },
  {
    id: "DROP",
    sql: "DROP",
    label: "DROP",
    description: "Drop databases, tables, and views",
    scopable: true,
  },
  {
    id: "INDEX",
    sql: "ALTER INDEX",
    label: "INDEX",
    description: "Create or drop indexes",
    scopable: true,
  },
  {
    id: "TRUNCATE",
    sql: "TRUNCATE",
    label: "TRUNCATE",
    description: "Truncate tables",
    scopable: true,
  },
  {
    id: "SHOW",
    sql: "SHOW",
    label: "SHOW",
    description: "View table structure",
    scopable: true,
  },
  {
    id: "OPTIMIZE",
    sql: "OPTIMIZE",
    label: "OPTIMIZE",
    description: "Optimize table parts",
    scopable: true,
  },
  {
    id: "UPDATE",
    sql: "ALTER UPDATE",
    label: "UPDATE",
    description: "Update existing rows",
    scopable: true,
  },
];

/**
 * Other Privileges - typically global scope
 */
export const OTHER_PRIVILEGES: PrivilegeDefinition[] = [
  {
    id: "SYSTEM",
    sql: "SYSTEM",
    label: "SYSTEM",
    description: "System administration commands",
    scopable: false,
  },
  {
    id: "KILL_QUERY",
    sql: "KILL QUERY",
    label: "KILL QUERY",
    description: "Terminate running queries",
    scopable: false,
  },
  {
    id: "KILL_TRANSACTION",
    sql: "KILL TRANSACTION",
    label: "KILL TRANSACTION",
    description: "Terminate running transactions",
    scopable: false,
  },
  {
    id: "CREATE_USER",
    sql: "CREATE USER",
    label: "CREATE USER",
    description: "Create user accounts",
    scopable: false,
  },
  {
    id: "ALTER_USER",
    sql: "ALTER USER",
    label: "ALTER USER",
    description: "Modify user accounts",
    scopable: false,
  },
  {
    id: "DROP_USER",
    sql: "DROP USER",
    label: "DROP USER",
    description: "Delete user accounts",
    scopable: false,
  },
  {
    id: "CREATE_ROLE",
    sql: "CREATE ROLE",
    label: "CREATE ROLE",
    description: "Create roles",
    scopable: false,
  },
  {
    id: "ALTER_ROLE",
    sql: "ALTER ROLE",
    label: "ALTER ROLE",
    description: "Modify roles",
    scopable: false,
  },
  {
    id: "DROP_ROLE",
    sql: "DROP ROLE",
    label: "DROP ROLE",
    description: "Delete roles",
    scopable: false,
  },
  {
    id: "ROLE_ADMIN",
    sql: "ROLE ADMIN",
    label: "ROLE ADMIN",
    description: "Grant roles to other users",
    scopable: false,
  },
  {
    id: "SHOW_ACCESS",
    sql: "SHOW ACCESS",
    label: "SHOW ACCESS",
    description: "View access control objects",
    scopable: false,
  },
  {
    id: "BACKUP",
    sql: "BACKUP",
    label: "BACKUP",
    description: "Create backups",
    scopable: false,
  },
  {
    id: "TABLE_ENGINE",
    sql: "TABLE ENGINE",
    label: "TABLE ENGINE",
    description: "Control table engine usage",
    scopable: false,
  },
  {
    id: "INTROSPECTION",
    sql: "INTROSPECTION",
    label: "INTROSPECTION",
    description: "Use introspection functions",
    scopable: false,
  },
  {
    id: "SOURCES",
    sql: "SOURCES",
    label: "SOURCES",
    description: "Access external data sources",
    scopable: false,
  },
  {
    id: "CREATE_QUOTA",
    sql: "CREATE QUOTA",
    label: "CREATE QUOTA",
    description: "Create resource quotas",
    scopable: false,
  },
  {
    id: "ALTER_QUOTA",
    sql: "ALTER QUOTA",
    label: "ALTER QUOTA",
    description: "Modify resource quotas",
    scopable: false,
  },
  {
    id: "DROP_QUOTA",
    sql: "DROP QUOTA",
    label: "DROP QUOTA",
    description: "Delete resource quotas",
    scopable: false,
  },
  {
    id: "CREATE_ROW_POLICY",
    sql: "CREATE ROW POLICY",
    label: "CREATE ROW POLICY",
    description: "Create row-level policies",
    scopable: false,
  },
  {
    id: "ALTER_ROW_POLICY",
    sql: "ALTER ROW POLICY",
    label: "ALTER ROW POLICY",
    description: "Modify row-level policies",
    scopable: false,
  },
  {
    id: "DROP_ROW_POLICY",
    sql: "DROP ROW POLICY",
    label: "DROP ROW POLICY",
    description: "Delete row-level policies",
    scopable: false,
  },
  {
    id: "CREATE_SETTINGS_PROFILE",
    sql: "CREATE SETTINGS PROFILE",
    label: "CREATE SETTINGS PROFILE",
    description: "Create settings profiles",
    scopable: false,
  },
  {
    id: "ALTER_SETTINGS_PROFILE",
    sql: "ALTER SETTINGS_PROFILE",
    label: "ALTER SETTINGS PROFILE",
    description: "Modify settings profiles",
    scopable: false,
  },
  {
    id: "DROP_SETTINGS_PROFILE",
    sql: "DROP SETTINGS PROFILE",
    label: "DROP SETTINGS PROFILE",
    description: "Delete settings profiles",
    scopable: false,
  },
];

export const PRIVILEGE_GROUPS: PrivilegeGroup[] = [
  {
    name: "Table Privileges",
    privileges: TABLE_PRIVILEGES,
  },
  {
    name: "Other Privileges",
    privileges: OTHER_PRIVILEGES,
  },
];

/**
 * Find a privilege definition by ID
 */
export function findPrivilegeById(id: string): PrivilegeDefinition | undefined {
  return [...TABLE_PRIVILEGES, ...OTHER_PRIVILEGES].find((p) => p.id === id);
}
