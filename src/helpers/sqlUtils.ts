export const isCreateOrInsert = (query: string) => {
    // Remove lines that start with '--'
    const cleanedQuery = query
        .split('\n')
        .filter(line => !line.trim().startsWith('--'))
        .join('\n');
    const lowerQuery = cleanedQuery.toLowerCase();
    const createTableRegex = /\bcreate\s+table\b/;
    const insertRegex = /\binsert\b/;
    const alterRegex = /\balter\b/;
    const dropTableRegex = /\bdrop\s+table\b/;
    const dropColumnRegex = /\bdrop\s+column\b/;
    const dropIndexRegex = /\bdrop\s+index\b/;
    const createDatabase = /\bcreate\s+database\b/;
    const dropDatabase = /\bdrop\s+database\b/;
    const createTableAs = /\bcreate\s+table\s+as\b/;
    const createTableEngine = /\bcreate\s+table\s+engine\b/;
    const createTableIfNotExists = /\bcreate\s+table\s+if\s+not\s+exists\b/;
    const createTableLike = /\bcreate\s+table\s+like\b/;
    const createTableMaterialized = /\bcreate\s+table\s+materialized\b/;
    const createTableTemporary = /\bcreate\s+table\s+temporary\b/;
    const createTableTemporaryEngine = /\bcreate\s+table\s+temporary\s+engine\b/;
    const createTableTemporaryIfNotExists = /\bcreate\s+table\s+temporary\s+if\s+not\s+exists\b/;
    const createTableTemporaryLike = /\bcreate\s+table\s+temporary\s+like\b/;
    const createTableTemporaryMaterialized = /\bcreate\s+table\s+temporary\s+materialized\b/;
    const createTableTemporaryAs = /\bcreate\s+table\s+temporary\s+as\b/;
    // ClickHouse-specific patterns
    const createTableOnCluster = /\bcreate\s+table\s+on\s+cluster\b/;
    const createTableDistributed = /\bcreate\s+table\s+.*\bengine\s*=\s*Distributed\b/i;
    const createMaterializedView = /\bcreate\s+materialized\s+view\b/;
    const createView = /\bcreate\s+view\b/;
    const createDictionary = /\bcreate\s+dictionary\b/;
    const attachTable = /\battach\s+table\b/;
    const optimizeTable = /\boptimize\s+table\b/;
    const truncateTable = /\btruncate\s+table\b/;
    const renameTable = /\brename\s+table\b/;

    return (
      createTableRegex.test(lowerQuery) ||
      insertRegex.test(lowerQuery) ||
      alterRegex.test(lowerQuery) ||
      dropTableRegex.test(lowerQuery) ||
      dropColumnRegex.test(lowerQuery) ||
      dropIndexRegex.test(lowerQuery) ||
      createDatabase.test(lowerQuery) ||
      dropDatabase.test(lowerQuery) ||
      createTableAs.test(lowerQuery) ||
      createTableEngine.test(lowerQuery) ||
      createTableIfNotExists.test(lowerQuery) ||
      createTableLike.test(lowerQuery) ||
      createTableMaterialized.test(lowerQuery) ||
      createTableTemporary.test(lowerQuery) ||
      createTableTemporaryEngine.test(lowerQuery) ||
      createTableTemporaryIfNotExists.test(lowerQuery) ||
      createTableTemporaryLike.test(lowerQuery) ||
      createTableTemporaryMaterialized.test(lowerQuery) ||
      createTableTemporaryAs.test(lowerQuery) ||
      createTableOnCluster.test(lowerQuery) ||
      createTableDistributed.test(lowerQuery) ||
      createMaterializedView.test(lowerQuery) ||
      createView.test(lowerQuery) ||
      createDictionary.test(lowerQuery) ||
      attachTable.test(lowerQuery) ||
      optimizeTable.test(lowerQuery) ||
      truncateTable.test(lowerQuery) ||
      renameTable.test(lowerQuery)
    );
  };
