interface AppQuery {
  query: string;
}

export const appQueries: Record<string, AppQuery> = {
  getIntellisense: {
    query: `
    SELECT 
      database,
      table,
      name AS column_name,
      type AS column_type
    FROM system.columns
    ORDER BY database, table, column_name;
    `,
  },
  getDatabasesTables: {
    query: `
    SELECT
      databases.name AS database_name,
      tables.name AS table_name,
      tables.engine AS table_type
    FROM system.databases AS databases
    LEFT JOIN system.tables AS tables
      ON databases.name = tables.database

    UNION ALL

    SELECT
      database AS database_name,
      name AS table_name,
      'Dictionary' AS table_type
    FROM system.dictionaries

    ORDER BY database_name, table_name;
    `,
  },
  getClickHouseFunctions: {
    query: `SELECT name from system.functions`,
  },
  getKeywords: {
    query: `SELECT keyword FROM system.keywords`,
  },
  getSavedQueries: {
    query: `SELECT * FROM CH_UI.saved_queries order by updated_at desc`,
  },
};
