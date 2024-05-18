export const analyticsQueries = [
  {
    query: `SELECT * FROM system.clusters`,
    format: "JSONEachRow",
    title: "Cluster",
    plot: "Table", // Cluster information is best displayed as a table
    description:
      "Displays information about the ClickHouse cluster, including its name, version, and other relevant details.",
  },
  {
    query: `SELECT version()`,
    format: "JSONEachRow",
    title: "Version",
    plot: "Card", // Version information is best displayed as a card
    description: "Shows the ClickHouse server version currently running.",
  },
  {
    query: `SELECT uptime() as uptime`,
    format: "JSONEachRow",
    title: "Uptime",
    data_format: "time",
    plot: "Card", // Uptime is best displayed as a card
    description:
      "Displays the ClickHouse server's uptime in human-readable format.",
  },
  {
    query: `SELECT sum(rows) AS total_rows FROM system.parts WHERE active`,
    format: "JSONEachRow",
    title: "Total rows",
    data_format: "number",
    plot: "Card", // Total rows displayed as a card
    description:
      "Shows the total number of rows stored across all active tables in the ClickHouse cluster.",
  },
  {
    query: `SELECT count(*) AS total_columns FROM system.columns`,
    format: "JSONEachRow",
    title: "Total columns",
    data_format: "number",
    plot: "Card", // Total columns displayed as a card
    description:
      "Displays the total number of columns defined in all ClickHouse tables.",
  },
  {
    query: `SELECT
     name as Name,
     path as Path,
     formatReadableSize(free_space) as Free,
     formatReadableSize(total_space) as Total,
     ROUND(1 - free_space/total_space, 2) as Used,
     ROUND((1 - free_space/total_space) * 100, 2) as "Used %"
       FROM system.disks
 `,
    format: "JSONEachRow",
    title: "Disk Usage",
    data_format: "bytes",
    plot: "Table", // Disk usage displayed as a heatmap
    description:
      "Provides a detailed overview of disk space usage for each disk in the ClickHouse cluster.",
  },
  {
    query: `SELECT concatAssumeInjective(table.database, '.', name) as name, table_stats.total_rows as total_rows FROM system.tables table LEFT JOIN (SELECT table, database, sum(rows) as total_rows FROM system.parts WHERE active GROUP BY table, database) AS table_stats ON table.name = table_stats.table AND table.database = table_stats.database ORDER BY total_rows DESC LIMIT 10`,
    format: "JSONEachRow",
    title: "Top 10 Tables by Rows",
    plot: "Bar", // Top 10 tables by rows displayed as a bar chart
    description: "Lists the top 10 tables with the highest number of rows.",
  },
  {
    query: `SELECT concatAssumeInjective(table.database, '.', name) as name, col_stats.col_count as total_columns FROM system.tables table LEFT JOIN (SELECT database, table, count() as col_count FROM system.columns GROUP BY table, database) as col_stats ON table.name = col_stats.table AND col_stats.database = table.database ORDER BY total_columns DESC LIMIT 10`,
    format: "JSONEachRow",
    title: "Top 10 Tables by Columns",
    plot: "Bar", // Top 10 tables by columns displayed as a bar chart
    description: "Lists the top 10 tables with the highest number of columns.",
  },
  {
    query: `SELECT concatAssumeInjective(table.database, '.', table.name) AS name, COALESCE(table_stats.total_rows, 0) AS total_rows, formatReadableSize(COALESCE(table_stats.bytes_on_disk, 0)) AS disk_size, formatReadableSize(COALESCE(table_stats.data_compressed_bytes, 0)) AS compressed_size FROM system.tables AS table LEFT JOIN (SELECT database, table AS name, sum(rows) AS total_rows, sum(bytes_on_disk) AS bytes_on_disk, sum(data_compressed_bytes) AS data_compressed_bytes FROM system.parts WHERE active GROUP BY database, table) AS table_stats ON table.name = table_stats.name AND table.database = table_stats.database ORDER BY total_rows DESC LIMIT 10`,
    format: "JSONEachRow",
    title: "Top 10 Tables by Disk Size",
    plot: "Bar", // Top 10 tables by disk size displayed as a bar chart
    description: "Lists the top 10 tables that consume the most disk space.",
  },
  {
    query: `SELECT source, type, status, count() as "count" FROM system.dictionaries GROUP BY source, type, status ORDER BY status DESC, source`,
    format: "JSONEachRow",
    title: "Dictionaries by Status",
    plot: "Table", // Dictionaries by status displayed as a table
    description:
      "Displays information about dictionaries (data lookups) used in ClickHouse queries, including their status (active, inactive, etc.).",
  },
  {
    query: `SELECT name, table.database, engine, partitions, parts, formatReadableSize(bytes_on_disk) as "disk_size", col_count, table_stats.total_rows, formatReadableSize(data_uncompressed_bytes) as "uncompressed_size" FROM system.tables table LEFT JOIN (SELECT table, database, uniq(table, partition) as "partitions", count() as parts, sum(bytes_on_disk) as bytes_on_disk, sum(data_uncompressed_bytes) as data_uncompressed_bytes, sum(rows) as total_rows, max(col_count) as col_count FROM system.parts parts LEFT JOIN (SELECT database, table, count() as col_count FROM system.columns GROUP BY table, database) as col_stats ON parts.table = col_stats.table AND col_stats.database = parts.database WHERE active GROUP BY table, database) AS table_stats ON table.name = table_stats.table AND table.database = table_stats.database WHERE lower(name) != 'information_schema' ORDER BY bytes_on_disk DESC LIMIT 1000`,
    format: "JSONEachRow",
    title: "Table Details",
    plot: "Table", // Detailed table information displayed as a table
    description:
      "Provides a detailed breakdown of table information, including name, database, engine, partitions, parts, disk size, column count, row count, and uncompressed data size.",
  },
  {
    query: `SELECT database, table, partition_id, name, disk, level FROM system.detached_parts`,
    format: "JSONEachRow",
    title: "Detached Parts",
    plot: "Table", // Detached parts information displayed as a table
    description:
      "Lists information about detached parts (data segments) that are not currently attached to a table.",
  },
  {
    query: `SELECT modification_time as timestamp, concatAssumeInjective(database, '.', table) as table, rows FROM system.parts parts WHERE modification_time > now() - INTERVAL 3 MINUTE ORDER BY modification_time ASC`,
    format: "JSONEachRow",
    title: "Recent Modifications",
    plot: "Line", // Recent modifications displayed as a line chart
    description:
      "Shows a timeline of recent modifications (data changes) made to ClickHouse tables.",
  },
  {
    query: `SELECT concatAssumeInjective(database, '.', table) as dbTable, count() as "partitions", sum(part_count) as "parts", max(part_count) as "max_parts_per_partition" FROM (SELECT database, table, count() as "part_count" FROM system.parts WHERE active GROUP BY database, table, partition) partitions GROUP BY database, table ORDER BY max_parts_per_partition DESC LIMIT 10`,
    format: "JSONEachRow",
    title: "Partitions and Parts",
    plot: "Bar", // Partitions and parts displayed as a bar chart
    description:
      "Provides information about the distribution of partitions and parts across ClickHouse tables.",
  },
  {
    query: `SELECT sum(bytes_on_disk) AS total_bytes_on_disk FROM system.parts WHERE active`,
    format: "JSONEachRow",
    title: "Total Bytes on Disk",
    data_format: "bytes",
    plot: "Card", // Total bytes on disk displayed as a card
    description:
      "Displays the total size of data stored on disk by ClickHouse, including all active parts.",
  },
  {
    query: `SELECT avg(bytes_on_disk / rows) AS avg_row_size FROM system.parts WHERE active AND rows > 0`,
    format: "JSONEachRow",
    title: "Average Row Size",
    data_format: "bytes",
    plot: "Card", // Average row size displayed as a card
    description:
      "Calculates the average size of a row across all ClickHouse tables.",
  },
  {
    query: `SELECT concatAssumeInjective(database, '.', table) AS table, sum(bytes_on_disk) AS total_disk_usage FROM system.parts WHERE active GROUP BY database, table ORDER BY total_disk_usage DESC LIMIT 10`,
    format: "JSONEachRow",
    title: "Top 10 Largest Tables by Disk Usage",
    plot: "Bar", // Top 10 largest tables by disk usage displayed as a bar chart
    description:
      "Lists the top 10 tables that consume the most disk space, sorted by total disk usage.",
  },
  {
    query: `SELECT concatAssumeInjective(database, '.', table) AS table, count() AS part_count FROM system.parts WHERE active GROUP BY database, table ORDER BY part_count DESC LIMIT 10`,
    format: "JSONEachRow",
    title: "Top 10 Tables by Number of Parts",
    plot: "Bar", // Top 10 tables by number of parts displayed as a bar chart
    description:
      "Lists the top 10 tables with the highest number of parts, sorted by part count.",
  },
  {
    query: `SELECT count() AS total_tables FROM system.tables`,
    format: "JSONEachRow",
    title: "Total Number of Tables",
    data_format: "number",
    plot: "Card", // Total number of tables displayed as a card
    description:
      "Shows the total number of tables defined in the ClickHouse cluster.",
  },
  {
    query: `SELECT count() AS total_databases FROM system.databases`,
    format: "JSONEachRow",
    title: "Total Number of Databases",
    data_format: "number",
    plot: "Card", // Total number of databases displayed as a card
    description:
      "Shows the total number of databases defined in the ClickHouse cluster.",
  },
  {
    query: `SELECT database, sum(bytes_on_disk) AS total_disk_usage FROM system.parts WHERE active GROUP BY database ORDER BY total_disk_usage DESC`,
    format: "JSONEachRow",
    title: "Storage Usage by Database",
    plot: "Bar", // Storage usage by database displayed as a bar chart
    description:
      "Displays the disk space usage breakdown for each database in the ClickHouse cluster.",
  },
  {
    query: `SELECT engine, sum(bytes_on_disk) AS total_disk_usage FROM system.parts WHERE active GROUP BY engine ORDER BY total_disk_usage DESC`,
    format: "JSONEachRow",
    title: "Storage Usage by Table Engine",
    plot: "Bar", // Storage usage by table engine displayed as a bar chart
    description:
      "Shows the disk space usage breakdown for different ClickHouse storage engines.",
  },
  {
    query: `SELECT engine, sum(rows) AS total_rows FROM system.parts WHERE active GROUP BY engine ORDER BY total_rows DESC`,
    format: "JSONEachRow",
    title: "Total Rows by Table Engine",
    plot: "Bar", // Total rows by table engine displayed as a bar chart
    description:
      "Displays the total number of rows stored across tables using different ClickHouse storage engines.",
  },
];
