import {
  HomeIcon,
  TableIcon,
  TerminalSquareIcon,
  Settings2,
  HardDriveIcon,
  NetworkIcon,
  CpuIcon,
  AlertTriangleIcon,
} from "lucide-react";
import { ReactNode, ComponentType } from "react";

interface Metrics {
  title: string;
  description: string;
  scope: string;
  icon: React.ElementType;
  items?: MetricItem[];
}

interface MetricItem {
  title: string;
  query: string;
  type: "card" | "table" | "chart";
  chartType?: "bar" | "line" | "area" | "pie" | "radar" | "donut";
  description: string;
  chartConfig?: CustomChartConfig;
  tiles?: number;
}

type ChartDataConfig = {
  label?: ReactNode;
  icon?: ComponentType<{}>;
  color?: string;
  unit?: string;
  decimals?: number;
};

type CustomChartConfig = {
  indexBy: string;
  isDateTime?: boolean;
  [key: string]: ChartDataConfig | string | boolean | undefined;
};

// Removed theme indirection for simplicity

// Helper function to create a consistent chart config object
const createChartConfig = (
  indexBy: string,
  valueKey: string,
  label: string,
  color: string = "#447EBC",
): CustomChartConfig => {
  return {
    indexBy,
    [valueKey]: {
      label,
      color,
    },
  };
};

export const metrics: Metrics[] = [
  {
    title: "Overview",
    scope: "overview",
    description: "Overview of ClickHouse metrics.",
    icon: HomeIcon,
    items: [
      {
        title: "Server Uptime",
        query: `
        -- set max decimal places to 2

        SELECT 
            CONCAT(
                CAST(ROUND(uptime() / 86400) AS String), ' d, ',
                CAST(ROUND((uptime() % 86400) / 3600) AS String), ' h, ',
                CAST(ROUND((uptime() % 3600) / 60) AS String), ' m'
            ) AS uptime_formatted 
        `,
        type: "card",
        description:
          "Total time the server has been running in seconds, minutes, hours, and days.",
        tiles: 1,
      },
      {
        title: "Total Databases",
        query: `
          SELECT COUNT(*) AS total_databases 
          FROM system.databases 
          WHERE name NOT IN ('system', 'information_schema')
        `,
        type: "card",
        description: "Total number of databases excluding system databases.",
        tiles: 1,
      },
      {
        title: "Total Tables",
        query: `
          SELECT COUNT(*) AS total_tables 
          FROM system.tables 
          WHERE database NOT IN ('system', 'information_schema') 
            AND is_temporary = 0 
            AND engine LIKE '%MergeTree%'
        `,
        type: "card",
        description: "Total number of user tables excluding temporary tables.",
        tiles: 1,
      },
      {
        title: "Version",
        query: `SELECT version() AS version`,
        type: "card",
        description:
          "Version of the ClickHouse server running on the current instance.",
        tiles: 1,
      },
      {
        title: "Running Queries",
        query: `SELECT * FROM system.processes WHERE is_cancelled = 0`,
        type: "table",
        description: "Currently running queries excluding system queries.",
        tiles: 4,
      },
      {
        title: "Query Count Over Time",
        description: "Number of queries over the selected time range.",
        type: "chart",
        chartType: "line",
        query: `
          WITH toUInt64((($__unixEpochTo - $__unixEpochFrom) / $__bucketSec) + 1) AS steps
          SELECT g.bucket, COALESCE(d.query_count, 0) AS query_count
          FROM (
            SELECT toStartOfInterval(toDateTime($__unixEpochFrom + number * $__bucketSec), INTERVAL $__bucketSec SECOND) AS bucket
            FROM numbers(steps)
          ) AS g
          LEFT JOIN (
            SELECT $__timeBucket AS bucket, COUNT(*) AS query_count
            FROM system.query_log
            WHERE event_time BETWEEN $__timeFromTo
            GROUP BY $__timeBucket
          ) AS d USING bucket
          ORDER BY g.bucket
        `,
        chartConfig: createChartConfig("bucket", "query_count", "Query Count", "#447EBC"),
        tiles: 4,
      },
    ],
  },
  {
    title: "Tables",
    description: "Metrics related to tables.",
    scope: "tables",
    icon: TableIcon,
    items: [
      {
        title: "Total Tables",
        query: `SELECT COUNT(*) AS total_tables FROM system.tables WHERE lower(database) NOT IN ('system', 'information_schema') AND is_temporary = 0 AND engine LIKE '%MergeTree%'`,
        type: "card",
        description: "Total number of user-defined tables.",
        tiles: 1,
      },
      {
        title: "Total System Tables",
        query: `SELECT COUNT(*) AS total_tables FROM system.tables WHERE lower(database) IN ('system', 'information_schema')`,
        type: "card",
        description: "Total number of system tables.",
        tiles: 1,
      },
      {
        title: "Total Temporary Tables",
        query: `SELECT COUNT(*) AS total_tables FROM system.tables WHERE is_temporary = 1`,
        type: "card",
        description: "Total number of temporary tables.",
        tiles: 1,
      },
      // card
      {
        title: "Biggest Table",
        query: `SELECT name AS table FROM system.tables WHERE database NOT IN ('system', 'information_schema') ORDER BY total_bytes DESC LIMIT 1`,
        type: "card",
        description: "Largest table in the system.",
        tiles: 1,
      },
      {
        title: "Table Cardinality",
        query: `SELECT database, name AS table, total_rows FROM system.tables WHERE database NOT IN ('system', 'information_schema') ORDER BY total_rows DESC LIMIT 10`,
        type: "table",
        description: "Number of rows in the top 10 tables.",
        tiles: 2,
      },
      {
        title: "Table Row Counts",
        query: `SELECT database, name AS table, total_rows FROM system.tables WHERE database NOT IN ('system', 'information_schema') ORDER BY total_rows DESC LIMIT 10`,
        type: "table",
        description: "Number of rows in the top 10 tables.",
        tiles: 2,
      },
      {
        title: "Table Sizes (MB)",
        query: `SELECT name AS name, total_bytes / 1024 / 1024 AS total_mb FROM system.tables WHERE database NOT IN ('system', 'information_schema') ORDER BY total_mb DESC LIMIT 20`,
        type: "chart",
        chartType: "bar",
        description: "Size distribution of the top 30 largest tables.",
        chartConfig: {
          indexBy: "name",
          total_mb: { label: "Size (MB)", color: "#EAB839", unit: "MB", decimals: 1 },
        },
        tiles: 2,
      },
      {
        title: "Number of Partitions per Table",
        query: `SELECT table, COUNT(*) AS partition_count FROM system.parts WHERE active = 1 GROUP BY table ORDER BY partition_count DESC LIMIT 20`,
        type: "chart",
        chartType: "bar",
        description: "Number of partitions per table.",
        chartConfig: createChartConfig("table", "partition_count", "Partition Count", "#6ED0E0"),
        tiles: 2,
      },
      {
        title: "Table Engine Distribution",
        query: `SELECT engine, COUNT(*) AS table_count FROM system.tables WHERE database NOT IN ('system', 'information_schema') GROUP BY engine ORDER BY table_count DESC LIMIT 15`,
        type: "chart",
        chartType: "bar",
        description: "Distribution of table engines.",
        chartConfig: createChartConfig("engine", "table_count", "Table Count", "#7EB26D"),
        tiles: 2,
      },
      {
        title: "Most Used Tables",
        query: `
          SELECT t AS table, COUNT(*) AS query_count
          FROM (
            SELECT arrayJoin(tables) AS t
            FROM system.query_log
            WHERE event_time BETWEEN $__timeFromTo
              AND type = 'QueryFinish'
          )
          GROUP BY t
          ORDER BY query_count DESC
          LIMIT 10
        `,
        type: "chart",
        chartType: "bar",
        description: "Top 10 most queried tables in the selected time range.",
        chartConfig: {
          indexBy: "table",
          query_count: { label: "Query Count", color: "#EAB839" },
        },
        tiles: 2,
      },
    ],
  },
  {
    title: "Queries",
    scope: "queries",
    description: "Comprehensive metrics related to queries in the system.",
    icon: TerminalSquareIcon,
    items: [
      {
        title: "Running Queries Count",
        query: `
          SELECT COUNT(*) AS running_queries 
          FROM system.processes 
          WHERE is_cancelled = 0 AND query NOT LIKE '%system%'`,
        type: "card",
        description: "Current number of active queries in the system.",
        tiles: 1,
      },
      {
        title: "Query Error Rate",
        query: `
          SELECT
            round(100 * failed_queries / total_queries, 2) AS error_rate
          FROM
            (
              SELECT
                COUNT(*) AS total_queries,
                COUNTIf(type IN ('ExceptionBeforeStart', 'ExceptionWhileProcessing')) AS failed_queries
              FROM
                system.query_log
              WHERE
                event_time BETWEEN $__timeFromTo
            ) AS query_counts`,
        type: "card",
        description: "Percentage of failed queries (selected range).",
        tiles: 1,
      },
      {
        title: "Average Query Duration (Selected Range)",
        query: `
          SELECT 
            round(avg(query_duration_ms), 2) AS avg_duration_ms
          FROM 
            system.query_log
          WHERE 
            type = 'QueryFinish' 
            AND event_time BETWEEN $__timeFromTo`,
        type: "card",
        description: "Average query duration in the selected time range.",
        tiles: 1,
      },
      {
        title: "Total Queries (Selected Range)",
        query: `
          SELECT COUNT(*) AS total_queries 
          FROM system.query_log 
          WHERE event_time BETWEEN $__timeFromTo`,
        type: "card",
        description: "Total number of queries (selected range).",
        tiles: 1,
      },
      {
        title: "Query Duration Distribution",
        query: `
          SELECT 
            CASE 
              WHEN query_duration_ms < 10 THEN '<10ms'
              WHEN query_duration_ms < 20 THEN '10ms-20ms'
              WHEN query_duration_ms < 50 THEN '20ms-50ms'
              WHEN query_duration_ms < 100 THEN '50ms-100ms'
              WHEN query_duration_ms < 200 THEN '100ms-200ms'
              WHEN query_duration_ms < 500 THEN '200ms-500ms'
              WHEN query_duration_ms < 1000 THEN '500ms-1s'
              WHEN query_duration_ms < 5000 THEN '1s-5s'
              WHEN query_duration_ms < 30000 THEN '5s-30s'
              ELSE '>30s'
            END AS duration_bucket,
            COUNT(*) AS query_count
          FROM system.query_log
          WHERE 
            type = 'QueryFinish'
            AND event_time BETWEEN $__timeFromTo
          GROUP BY duration_bucket
          ORDER BY duration_bucket`,
        type: "chart",
        chartType: "bar",
        description: "Distribution of query durations (selected range).",
        chartConfig: createChartConfig("duration_bucket", "query_count", "Query Count", "#447EBC"),
        tiles: 2,
      },
      {
        title: "Queries Per Minute",
        query: `
          WITH toUInt64((($__unixEpochTo - $__unixEpochFrom) / $__bucketSec) + 1) AS steps
          SELECT g.bucket, COALESCE(d.qps, 0) AS qps
          FROM (
            SELECT toStartOfInterval(toDateTime($__unixEpochFrom + number * $__bucketSec), INTERVAL $__bucketSec SECOND) AS bucket
            FROM numbers(steps)
          ) AS g
          LEFT JOIN (
            SELECT $__timeBucket AS bucket, COUNT(*) AS qps
            FROM system.query_log
            WHERE type = 'QueryFinish' AND event_time BETWEEN $__timeFromTo
            GROUP BY $__timeBucket
          ) AS d USING bucket
          ORDER BY g.bucket`,
        type: "chart",
        chartType: "area",
        description: "Queries over the selected time range (auto-grouped).",
        chartConfig: createChartConfig("bucket", "qps", "Queries", "#6ED0E0"),
        tiles: 2,
      },
      {
        title: "Queries Per User",
        query: `
          SELECT 
            user, 
            COUNT(*) AS query_count 
          FROM 
            system.query_log 
          WHERE 
            event_time BETWEEN $__timeFromTo 
            AND type = 'QueryFinish' 
          GROUP BY user 
          ORDER BY query_count DESC 
          LIMIT 10`,
        type: "table",
        description: "Top 10 users by queries (selected range).",
        tiles: 4,
      },

      {
        title: "Top Slow Queries",
        query: `
          SELECT 
            query, 
            query_duration_ms 
          FROM system.query_log 
          WHERE type = 'QueryFinish' 
            AND event_time BETWEEN $__timeFromTo 
          ORDER BY query_duration_ms DESC 
          LIMIT 10`,
        type: "table",
        description: "Top 10 slowest queries (selected range).",
        tiles: 4,
      },
    ],
  },
  {
    title: "Performance",
    scope: "performance",
    description: "Performance-related metrics.",
    icon: CpuIcon,
    items: [
      {
        title: "CPU Usage",
        query: `
          SELECT
            $__timeBucket AS bucket,
            avg(ProfileEvent_OSCPUVirtualTimeMicroseconds) AS cpu_usage
          FROM system.metric_log
          WHERE event_time BETWEEN $__timeFromTo
          GROUP BY $__timeBucket
          ORDER BY bucket WITH FILL STEP $__bucketSec
        `,
        type: "chart",
        chartType: "line",
        description: "CPU usage over the selected time range.",
        chartConfig: createChartConfig("bucket", "cpu_usage", "CPU Usage", "#BA43A9"),
        tiles: 2,
      },
      {
        title: "Memory Usage",
        query: `
          WITH toUInt64((($__unixEpochTo - $__unixEpochFrom) / $__bucketSec) + 1) AS steps
          SELECT g.bucket, COALESCE(d.memory_usage, 0) AS memory_usage
          FROM (
            SELECT toStartOfInterval(toDateTime($__unixEpochFrom + number * $__bucketSec), INTERVAL $__bucketSec SECOND) AS bucket
            FROM numbers(steps)
          ) AS g
          LEFT JOIN (
            SELECT $__timeBucket AS bucket, avg(ProfileEvent_MemoryWorkerRun) AS memory_usage
            FROM system.metric_log
            WHERE event_time BETWEEN $__timeFromTo
            GROUP BY $__timeBucket
          ) AS d USING bucket
          ORDER BY g.bucket
        `,
        type: "chart",
        chartType: "area",
        description: "Memory usage over the selected time range.",
        chartConfig: createChartConfig("bucket", "memory_usage", "Memory Usage", "#7EB26D"),
        tiles: 2,
      },

      {
        title: "Threads Usage",
        query: `
        WITH toUInt64((($__unixEpochTo - $__unixEpochFrom) / $__bucketSec) + 1) AS steps
        SELECT g.bucket, COALESCE(d.threads_running, 0) AS threads_running
        FROM (
          SELECT toStartOfInterval(toDateTime($__unixEpochFrom + number * $__bucketSec), INTERVAL $__bucketSec SECOND) AS bucket
          FROM numbers(steps)
        ) AS g
        LEFT JOIN (
          SELECT $__timeBucket AS bucket, avg(ProfileEvent_Query) AS threads_running
          FROM system.metric_log
          WHERE event_time BETWEEN $__timeFromTo
          GROUP BY $__timeBucket
        ) AS d USING bucket
        ORDER BY g.bucket
        `,
        type: "chart",
        chartType: "area",
        description: "Threads usage over the selected time range.",
        chartConfig: createChartConfig("bucket", "threads_running", "Threads Running", "#7EB26D"),
        tiles: 2,
      },
      {
        title: "Network Traffic",
        query: `
   WITH toUInt64((($__unixEpochTo - $__unixEpochFrom) / $__bucketSec) + 1) AS steps
   SELECT g.bucket, COALESCE(d.bytes_received, 0) AS bytes_received
   FROM (
     SELECT toStartOfInterval(toDateTime($__unixEpochFrom + number * $__bucketSec), INTERVAL $__bucketSec SECOND) AS bucket
     FROM numbers(steps)
   ) AS g
   LEFT JOIN (
     SELECT $__timeBucket AS bucket, sum(value) AS bytes_received
     FROM system.asynchronous_metric_log
     WHERE event_time BETWEEN $__timeFromTo AND metric LIKE '%NetworkReceiveBytes%'
     GROUP BY $__timeBucket
   ) AS d USING bucket
   ORDER BY g.bucket
        `,
        type: "chart",
        chartType: "area",
        description: "Network traffic over the selected time range.",
        chartConfig: {
          indexBy: "bucket",
          isDateTime: true,
          bytes_received: { label: "Bytes Received", color: "#EF843C", unit: "bytes", decimals: 0 },
        },
        tiles: 2,
      },
      {
        title: "Disk Usage",
        query: `
          WITH toUInt64((($__unixEpochTo - $__unixEpochFrom) / $__bucketSec) + 1) AS steps
          SELECT g.bucket, COALESCE(d.disk_usage, 0) AS disk_usage
          FROM (
            SELECT toStartOfInterval(toDateTime($__unixEpochFrom + number * $__bucketSec), INTERVAL $__bucketSec SECOND) AS bucket
            FROM numbers(steps)
          ) AS g
          LEFT JOIN (
            SELECT $__timeBucket AS bucket, avg(ProfileEvent_ReadCompressedBytes) AS disk_usage
            FROM system.metric_log
            WHERE event_time BETWEEN $__timeFromTo
            GROUP BY $__timeBucket
          ) AS d USING bucket
          ORDER BY g.bucket
        `,
        type: "chart",
        chartType: "line",
        description: "Average disk usage over the selected time range.",
        chartConfig: createChartConfig("bucket", "disk_usage", "Disk Usage", "#E24D42"),
        tiles: 2,
      },
      {
        title: "Keep Alive Connections",
        query: `
          WITH toUInt64((($__unixEpochTo - $__unixEpochFrom) / $__bucketSec) + 1) AS steps
          SELECT g.bucket, COALESCE(d.active_connections, 0) AS active_connections
          FROM (
            SELECT toStartOfInterval(toDateTime($__unixEpochFrom + number * $__bucketSec), INTERVAL $__bucketSec SECOND) AS bucket
            FROM numbers(steps)
          ) AS g
          LEFT JOIN (
            SELECT $__timeBucket AS bucket, avg(CurrentMetric_KeeperAliveConnections) AS active_connections
            FROM system.metric_log
            WHERE event_time BETWEEN $__timeFromTo
            GROUP BY $__timeBucket
          ) AS d USING bucket
          ORDER BY g.bucket
        `,
        type: "chart",
        chartType: "line",
        description: "Active Keep alive connections over the selected time range.",
        chartConfig: createChartConfig("bucket", "active_connections", "Active Connections", "#7EB26D"),
        tiles: 2,
      },
    ],
  },
  {
    title: "Storage",
    scope: "storage",
    description: "Storage-related metrics.",
    icon: HardDriveIcon,
    items: [
      {
        title: "Disk Usage",
        query: `SELECT 
                  name,
                  round(total_space / 1024 / 1024 / 1024, 2) AS total_gb,
                  round(free_space / 1024 / 1024 / 1024, 2) AS free_gb,
                  round((1 - free_space / total_space) * 100, 2) AS used_percent
                FROM system.disks`,
        type: "table",
        description: "Detailed disk usage information.",
      },
      {
        title: "Database Sizes",
        query: `SELECT 
                  database,
                  round(sum(total_bytes) / 1024 / 1024 / 1024, 2) AS size_gb
                FROM system.tables
                GROUP BY database
                ORDER BY size_gb DESC
                LIMIT 20`,
        type: "chart",
        chartType: "bar",
        description: "Size distribution of databases.",
        chartConfig: {
          indexBy: "database",
          size_gb: { label: "Size (GB)", color: "#EAB839", unit: "GB", decimals: 2 },
        },
        tiles: 4,
      },
    ],
  },
  {
    title: "Network",
    scope: "network",
    description: "Network-related metrics.",
    icon: NetworkIcon,
    items: [
      {
        title: "Network Traffic",
        query: `WITH toUInt64((($__unixEpochTo - $__unixEpochFrom) / $__bucketSec) + 1) AS steps
                SELECT g.bucket, COALESCE(d.send_bytes, 0) AS send_bytes, COALESCE(d.receive_bytes, 0) AS receive_bytes
                FROM (
                  SELECT toStartOfInterval(toDateTime($__unixEpochFrom + number * $__bucketSec), INTERVAL $__bucketSec SECOND) AS bucket
                  FROM numbers(steps)
                ) AS g
                LEFT JOIN (
                  SELECT $__timeBucket AS bucket,
                         sum(ProfileEvent_NetworkSendBytes) AS send_bytes,
                         sum(ProfileEvent_NetworkReceiveBytes) AS receive_bytes
                  FROM system.metric_log
                  WHERE event_time BETWEEN $__timeFromTo
                  GROUP BY $__timeBucket
                ) AS d USING bucket
                ORDER BY g.bucket`,
        type: "chart",
        chartType: "area",
        description: "Network traffic over the selected time range.",
        chartConfig: {
          indexBy: "bucket",
          isDateTime: true,
          send_bytes: { label: "Send", color: "#6ED0E0", unit: "bytes", decimals: 0 },
          receive_bytes: { label: "Receive", color: "#EF843C", unit: "bytes", decimals: 0 },
        },
        tiles: 2,
      },
      {
        title: "Network Connections HTTP",
        query: `WITH toUInt64((($__unixEpochTo - $__unixEpochFrom) / $__bucketSec) + 1) AS steps
                SELECT g.bucket, COALESCE(d.connections, 0) AS connections
                FROM (
                  SELECT toStartOfInterval(toDateTime($__unixEpochFrom + number * $__bucketSec), INTERVAL $__bucketSec SECOND) AS bucket
                  FROM numbers(steps)
                ) AS g
                LEFT JOIN (
                  SELECT $__timeBucket AS bucket,
                         avg(CurrentMetric_HTTPConnection) AS connections
                  FROM system.metric_log
                  WHERE event_time BETWEEN $__timeFromTo
                  GROUP BY $__timeBucket
                ) AS d USING bucket
                ORDER BY g.bucket`,
        type: "chart",
        chartType: "line",
        description: "HTTP connections over the selected time range.",
        chartConfig: createChartConfig("bucket", "connections", "Connections", "#7EB26D"),
        tiles: 2,
      },
    ],
  },
  {
    title: "Settings & Config",
    scope: "settings",
    description: "Settings and configuration.",
    icon: Settings2,
    items: [
      {
        title: "Current Settings",
        query: `SELECT name, value, description FROM system.settings`,
        type: "table",
        description: "Current ClickHouse settings.",
        tiles: 4,
      },
      {
        title: "Users",
        query: `SELECT * FROM system.users`,
        type: "table",
        description: "List of users and their authentication types.",
        tiles: 4,
      },
    ],
  },
  {
    title: "Query Exceptions",
    scope: "exceptions",
    description: "Overview of query exceptions in the system.",
    icon: AlertTriangleIcon,
    items: [
      {
        title: "Exceptions (Selected Range)",
        query: `
          SELECT COUNT(*) AS total_exceptions 
          FROM system.query_log 
          WHERE type IN ('ExceptionBeforeStart', 'ExceptionWhileProcessing') 
            AND event_time BETWEEN $__timeFromTo
        `,
        type: "card",
        description: "Total number of exceptions in the selected time range.",
        tiles: 1,
      },
      {
        title: "Exception Rate (Selected Range)",
        query: `
          SELECT 
            round(100 * COUNTIf(type IN ('ExceptionBeforeStart', 'ExceptionWhileProcessing')) / COUNT(*), 2) AS exception_rate 
          FROM 
            system.query_log 
          WHERE 
            event_time BETWEEN $__timeFromTo
        `,
        type: "card",
        description: "Percentage of queries that resulted in exceptions (selected range).",
        tiles: 1,
      },
      {
        title: "Recent Exceptions",
        query: `
          SELECT 
            event_time, 
            user, 
            query, 
            exception 
          FROM 
            system.query_log 
          WHERE 
            type IN ('ExceptionBeforeStart', 'ExceptionWhileProcessing') 
            AND event_time BETWEEN $__timeFromTo 
          ORDER BY 
            event_time DESC 
          LIMIT 10
        `,
        type: "table",
        description: "Last 10 exceptions in the selected time range.",
        tiles: 4,
      },

      {
        title: "Exceptions by User",
        query: `
          SELECT 
            user, 
            COUNT(*) AS exception_count 
          FROM 
            system.query_log 
          WHERE 
            type IN ('ExceptionBeforeStart', 'ExceptionWhileProcessing') 
            AND event_time BETWEEN $__timeFromTo 
          GROUP BY 
            user 
          ORDER BY 
            exception_count DESC 
          LIMIT 10
        `,
        type: "table",
        description: "Top 10 users by exception count (selected range).",
        tiles: 4,
      },
      {
        title: "Exceptions Over Time",
        query: `
          SELECT 
            $__timeGroupExpr AS bucket, 
            COUNT(*) AS exception_count 
          FROM 
            system.query_log 
          WHERE 
            type IN ('ExceptionBeforeStart', 'ExceptionWhileProcessing') 
            AND event_time BETWEEN $__timeFromTo 
          GROUP BY 
            bucket 
          ORDER BY 
            bucket
        `,
        type: "chart",
        chartType: "line",
        description: "Count of exceptions over the selected time range.",
        chartConfig: createChartConfig("bucket", "exception_count", "Exception Count", "#EAB839"),
        tiles: 2,
      },
      {
        title: "Most Common Exceptions",
        query: `
          SELECT 
            exception, 
            COUNT(*) AS count 
          FROM 
            system.query_log 
          WHERE 
            type IN ('ExceptionBeforeStart', 'ExceptionWhileProcessing') 
            AND event_time BETWEEN $__timeFromTo 
          GROUP BY 
            exception 
          ORDER BY 
            count DESC 
          LIMIT 10
        `,
        type: "table",
        description: "Top 10 most common exceptions (selected range).",
        tiles: 2,
      },
    ],
  },
];
