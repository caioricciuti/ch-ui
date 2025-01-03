import { ChartConfig } from "@/components/ui/chart";
import {
  HomeIcon,
  TableIcon,
  CombineIcon,
  TerminalSquareIcon,
  Settings2,
  HardDriveIcon,
  NetworkIcon,
  CpuIcon,
  AlertTriangleIcon,
} from "lucide-react";
import { ReactNode, ComponentType } from "react";

export interface Metrics {
  title: string;
  description: string;
  scope: string;
  icon: React.ElementType;
  items?: MetricItem[];
}

export interface MetricItem {
  title: string;
  query: string;
  type: "card" | "table" | "chart";
  chartType?: "bar" | "line" | "area" | "pie" | "radar" | "radial";
  description: string;
  chartConfig?: CustomChartConfig;
  tiles?: number;
}


export type ChartTheme = {
  light: string;
  dark: string;
}


export type ChartDataConfig = {
  label?: ReactNode;
  icon?: ComponentType<{}>;
} & ({ color?: string; theme?: never } | { color?: never; theme: ChartTheme });



export type CustomChartConfig = {
    indexBy: string;
    [key: string]: ChartDataConfig | string | undefined;
}



export const metrics: Metrics[] = [
  {
    title: "Overview",
    scope: "overview",
    description: "Overview of ClickHouse metrics.",
    icon: HomeIcon,
    items: [
      {
        title: "Server Uptime (days)",
        query: `
        -- set max decimal places to 2

          SELECT 
            ROUND(uptime() / 86400, 2) AS uptime_days -- 86400 seconds in a day 
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
        title: "Daily Query Count",
        description: "Number of queries per day for the last 30 days.",
        type: "chart",
        chartType: "bar",
        query: `
          SELECT count() AS query_count, toStartOfDay(event_time) AS day 
          FROM system.query_log 
          WHERE event_time > now() - INTERVAL 30 DAY 
          GROUP BY day 
          ORDER BY day
        `,
       chartConfig: {
          indexBy: "day",
          query_count: {
            label: "Query Count",
            color: "hsl(var(--chart-1))",
          },
        },
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
          total_mb: {
            label: "Size (MB)",
            color: "hsl(var(--chart-2))",
          },
        },
        tiles: 2,
      },
      {
        title: "Number of Partitions per Table",
        query: `SELECT table, COUNT(*) AS partition_count FROM system.parts WHERE active = 1 GROUP BY table ORDER BY partition_count DESC LIMIT 20`,
        type: "chart",
        chartType: "bar",
        description: "Number of partitions per table.",
        chartConfig: {
          indexBy: "table",
          partition_count: {
            label: "Partition Count",
            color: "hsl(var(--chart-3))",
          },
        },
        tiles: 2,
      },
      {
        title: "Table Engine Distribution",
        query: `SELECT engine, COUNT(*) AS table_count FROM system.tables WHERE database NOT IN ('system', 'information_schema') GROUP BY engine ORDER BY table_count DESC`,
        type: "chart",
        chartType: "bar",
        description: "Distribution of table engines.",
        chartConfig: {
          indexBy: "engine",
          table_count: {
            label: "Table Count",
            color: "hsl(var(--chart-1))",
          },
        },
        tiles: 2,
      },
      {
        title: "Most Used Tables",
        query: `SELECT tables, COUNT(*) AS query_count FROM system.query_log WHERE event_time > now() - INTERVAL 1 DAY GROUP BY tables ORDER BY query_count DESC LIMIT 10`,
        type: "chart",
        chartType: "bar",
        description: "Top 10 most queried tables in the last 24 hours.",
        chartConfig: {
          indexBy: "tables",
          query_count: {
            label: "Query Count",
            color: "hsl(var(--chart-2))",
          },
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
                event_time > now() - INTERVAL 1 DAY
            ) AS query_counts`,
        type: "card",
        description: "Percentage of failed queries over the last 24 hours.",
        tiles: 1,
      },
      {
        title: "Average Query Duration",
        query: `
          SELECT 
            round(avg(query_duration_ms), 2) AS avg_duration_ms
          FROM 
            system.query_log
          WHERE 
            type = 'QueryFinish' 
            AND event_time > now() - INTERVAL 1 DAY`,
        type: "card",
        description:
          "Average duration of queries executed in the last 24 hours.",
        tiles: 1,
      },
      {
        title: "Total Queries (Last 24h)",
        query: `
          SELECT COUNT(*) AS total_queries 
          FROM system.query_log 
          WHERE event_time > now() - INTERVAL 1 DAY`,
        type: "card",
        description: "Total number of queries executed in the last 24 hours.",
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
            AND event_time > now() - INTERVAL 1 DAY
          GROUP BY duration_bucket
          ORDER BY duration_bucket`,
        type: "chart",
        chartType: "bar",
        description:
          "Granular distribution of query durations over the last 24 hours.",
           chartConfig: {
          indexBy: "duration_bucket",
          query_count: {
            label: "Query Count",
           color: "hsl(var(--chart-1))",
          },
        },
        tiles: 2,
      },
      {
        title: "Queries Per Second (QPS)",
        query: `
          SELECT 
            toStartOfMinute(event_time) AS minute,
            COUNT(*) AS qps
          FROM system.query_log
          WHERE type = 'QueryFinish' 
            AND event_time > now() - INTERVAL 1 HOUR
          GROUP BY minute
          ORDER BY minute`,
        type: "chart",
        chartType: "area",
        description: "Rate of queries per second over the last hour.",
        chartConfig: {
          indexBy: "minute",
          qps: {
            label: "QPS",
           color: "hsl(var(--chart-3))",
          },
        },
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
            event_time > now() - INTERVAL 1 DAY 
            AND type = 'QueryFinish' 
          GROUP BY user 
          ORDER BY query_count DESC 
          LIMIT 10`,
        type: "table",
        description:
          "Top 10 users by the number of queries executed in the last 24 hours.",
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
            AND event_time > now() - INTERVAL 1 DAY 
          ORDER BY query_duration_ms DESC 
          LIMIT 10`,
        type: "table",
        description: "Top 10 slowest queries executed in the last 24 hours.",
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
            toStartOfMinute(event_time) AS minute,
            avg(ProfileEvent_OSCPUVirtualTimeMicroseconds) AS cpu_usage
          FROM system.metric_log
          WHERE event_time > now() - INTERVAL 1 HOUR
          GROUP BY minute
          ORDER BY minute
        `,
        type: "chart",
        chartType: "line",
        description: "CPU usage over the last hour.",
         chartConfig: {
          indexBy: "minute",
          cpu_usage: {
            label: "CPU Usage",
            color: "hsl(var(--chart-5))",
          },
        },
        tiles: 2,
      },
      {
        title: "Memory Usage",
        query: `
          SELECT 
            toStartOfMinute(event_time) AS minute,
            avg(ProfileEvent_MemoryWorkerRun) AS memory_usage
          FROM system.metric_log
          WHERE event_time > now() - INTERVAL 1 HOUR
          GROUP BY minute
          ORDER BY minute
        `,
        type: "chart",
        chartType: "area",
        description: "Memory usage over the last hour.",
          chartConfig: {
          indexBy: "minute",
          memory_usage: {
            label: "Memory Usage",
            color: "hsl(var(--chart-1))",
          },
        },
        tiles: 2,
      },

      {
        title: "Threads Usage",
        query: `
        SELECT 
            toStartOfMinute(event_time) AS minute,
            avg(ProfileEvent_Query) AS threads_running
        FROM system.metric_log
        WHERE event_time > now() - INTERVAL 1 HOUR
        GROUP BY minute
        ORDER BY minute
        `,
        type: "chart",
        chartType: "area",
        description: "Threads usage over the last hour.",
         chartConfig: {
          indexBy: "minute",
          threads_running: {
            label: "Threads Running",
            color: "hsl(var(--chart-1))",
          },
        },
        tiles: 2,
      },
      {
        title: "Network Traffic",
        query: `
   SELECT
  toStartOfMinute (event_time) AS minute,
  sum(value) AS bytes_received
FROM
  system.asynchronous_metric_log
WHERE
  event_time > now () - INTERVAL 1 HOUR
  AND metric LIKE '%NetworkReceiveBytes%'
GROUP BY
  minute
ORDER BY
  minute
        `,
        type: "chart",
        chartType: "area",
        description: "Network traffic over the last hour.",
           chartConfig: {
          indexBy: "minute",
          bytes_received: {
            label: "Bytes Received",
            color: "hsl(var(--chart-4))",
          },
        },
        tiles: 2,
      },
      {
        title: "Disk Usage",
        query: `
          SELECT 
            toStartOfMinute(event_time) AS minute,
            avg(ProfileEvent_ReadCompressedBytes) AS disk_usage
          FROM system.metric_log
          WHERE event_time > now() - INTERVAL 1 HOUR
          GROUP BY minute
          ORDER BY minute
        `,
        type: "chart",
        chartType: "line",
        description: "Average disk usage over the last hour.",
          chartConfig: {
          indexBy: "minute",
          disk_usage: {
            label: "Disk Usage",
             color: "hsl(var(--chart-6))",
          },
        },
        tiles: 2,
      },
      {
        title: "Keep Alive Connections",
        query: `
          SELECT 
            toStartOfMinute(event_time) AS minute,
            avg(CurrentMetric_KeeperAliveConnections) AS active_connections
          FROM system.metric_log
          WHERE event_time > now() - INTERVAL 1 HOUR
          GROUP BY minute
          ORDER BY minute
        `,
        type: "chart",
        chartType: "line",
        description: "Active Keep alive connections over the last hour.",
         chartConfig: {
          indexBy: "minute",
          active_connections: {
            label: "Active Connections",
            color: "hsl(var(--chart-1))",
          },
        },
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
                ORDER BY size_gb DESC`,
        type: "chart",
        chartType: "bar",
        description: "Size distribution of databases.",
        chartConfig: {
          indexBy: "database",
          size_gb: {
            label: "Size (GB)",
            color: "hsl(var(--chart-2))",
          },
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
        query: `SELECT 
                  toStartOfMinute(event_time) AS minute,
                  sum(ProfileEvent_NetworkSendBytes) AS send_bytes,
                  sum(ProfileEvent_NetworkReceiveBytes) AS receive_bytes
                FROM system.metric_log
                WHERE event_time > now() - INTERVAL 1 HOUR
                GROUP BY minute
                ORDER BY minute`,
        type: "chart",
        chartType: "area",
        description: "Network traffic over the last hour.",
          chartConfig: {
          indexBy: "minute",
          send_bytes: {
            label: "Send (bytes)",
            color: "hsl(var(--chart-3))",
          },
          receive_bytes: {
            label: "Receive (bytes)",
            color: "hsl(var(--chart-4))",
          },
        },
        tiles: 2,
      },
      {
        title: "Network Connections HTTP",
        query: `SELECT 
                  toStartOfMinute(event_time) AS minute,
                  avg(CurrentMetric_HTTPConnection) AS connections
                FROM system.metric_log
                WHERE event_time > now() - INTERVAL 1 HOUR
                GROUP BY minute
                ORDER BY minute`,
        type: "chart",
        chartType: "line",
        description: "HTTP connections over the last hour.",
         chartConfig: {
          indexBy: "minute",
          connections: {
            label: "Connections",
            color: "hsl(var(--chart-1))",
          },
        },
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
        title: "Exceptions (Last 24h)",
        query: `
          SELECT COUNT(*) AS total_exceptions 
          FROM system.query_log 
          WHERE type IN ('ExceptionBeforeStart', 'ExceptionWhileProcessing') 
            AND event_time > now() - INTERVAL 1 DAY
        `,
        type: "card",
        description:
          "Total number of exceptions recorded in the last 24 hours.",
        tiles: 1,
      },
      {
        title: "Exception Rate (Last 24h)",
        query: `
          SELECT 
            round(100 * COUNTIf(type IN ('ExceptionBeforeStart', 'ExceptionWhileProcessing')) / COUNT(*), 2) AS exception_rate 
          FROM 
            system.query_log 
          WHERE 
            event_time > now() - INTERVAL 1 DAY
        `,
        type: "card",
        description:
          "Percentage of queries that resulted in exceptions over the last 24 hours.",
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
            AND event_time > now() - INTERVAL 1 HOUR 
          ORDER BY 
            event_time DESC 
          LIMIT 10
        `,
        type: "table",
        description: "Last 10 exceptions recorded in the last hour.",
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
            AND event_time > now() - INTERVAL 1 DAY 
          GROUP BY 
            user 
          ORDER BY 
            exception_count DESC 
          LIMIT 10
        `,
        type: "table",
        description:
          "Top 10 users with the most exceptions in the last 24 hours.",
        tiles: 4,
      },
      {
        title: "Exceptions Over Time",
        query: `
          SELECT 
            toStartOfHour(event_time) AS hourERROR, 
            COUNT(*) AS exception_count 
          FROM 
            system.query_log 
          WHERE 
            type IN ('ExceptionBeforeStart', 'ExceptionWhileProcessing') 
            AND event_time > now() - INTERVAL 1 DAY 
          GROUP BY 
            hourERROR 
          ORDER BY 
            hourERROR
        `,
        type: "chart",
        chartType: "line",
        description: "Count of exceptions recorded over the last 24 hours.",
           chartConfig: {
          indexBy: "hourERROR",
          exception_count: {
            label: "Exception Count",
            color: "hsl(var(--chart-2))",
          },
        },
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
            AND event_time > now() - INTERVAL 1 DAY 
          GROUP BY 
            exception 
          ORDER BY 
            count DESC 
          LIMIT 10
        `,
        type: "table",
        description: "Top 10 most common exceptions in the last 24 hours.",
        tiles: 2,
      },
    ],
  },
];