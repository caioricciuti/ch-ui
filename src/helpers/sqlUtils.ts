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
  const createTemporaryTable = /\bcreate\s+temporary\s+table\b/;
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
  const createUser = /\bcreate\s+user\b/;
  const createRole = /\bcreate\s+role\b/;
  const dropRole = /\bdrop\s+role\b/;
  const grantRole = /\bgrant\s+role\b/;
  const revokeRole = /\brevoke\s+role\b/;
  const dropUser = /\bdrop\s+user\b/;
  const createQuota = /\bcreate\s+quota\b/;
  const dropQuota = /\bdrop\s+quota\b/;
  const alterQuota = /\balter\s+quota\b/;
  const createSetting = /\bcreate\s+setting\b/;
  const dropSetting = /\bdrop\s+setting\b/;
  const alterSetting = /\balter\s+setting\b/;
  const createFunction = /\bcreate\s+function\b/;
  const dropFunction = /\bdrop\s+function\b/;
  const alterFunction = /\balter\s+function\b/;
  const createAggregateFunction = /\bcreate\s+aggregate\s+function\b/;
  const dropAggregateFunction = /\bdrop\s+aggregate\s+function\b/;
  const alterAggregateFunction = /\balter\s+aggregate\s+function\b/;
  const grant = /\bgrant\b/;
  const revoke = /\brevoke\b/;
  const setAllowPattern = /\bset\s+allow_.*$/i;

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
    createTemporaryTable.test(lowerQuery) ||
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
    renameTable.test(lowerQuery) ||
    createUser.test(lowerQuery) ||
    createRole.test(lowerQuery) ||
    dropRole.test(lowerQuery) ||
    grantRole.test(lowerQuery) ||
    revokeRole.test(lowerQuery) ||
    dropUser.test(lowerQuery) ||
    createQuota.test(lowerQuery) ||
    dropQuota.test(lowerQuery) ||
    alterQuota.test(lowerQuery) ||
    createSetting.test(lowerQuery) ||
    dropSetting.test(lowerQuery) ||
    alterSetting.test(lowerQuery) ||
    createFunction.test(lowerQuery) ||
    dropFunction.test(lowerQuery) ||
    alterFunction.test(lowerQuery) ||
    createAggregateFunction.test(lowerQuery) ||
    dropAggregateFunction.test(lowerQuery) ||
    alterAggregateFunction.test(lowerQuery) ||
    grant.test(lowerQuery) ||
    revoke.test(lowerQuery) ||
    setAllowPattern.test(lowerQuery)
  );
};
