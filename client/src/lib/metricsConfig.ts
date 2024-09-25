// src/config/menuItemsConfig.ts
import {
  HomeIcon,
  TableIcon,
  CombineIcon,
  PlusCircle,
  TerminalSquareIcon,
} from "lucide-react";

export interface Query {
  title: string;
  query: string;
  type: "card" | "table" | "line" | "bar-chart" | "area-chart";
  description?: string;
}

export interface MenuItem {
  title: string;
  href: string;
  icon: React.ElementType;
  description?: string;
  countVariant?: string;
  items?: MenuItem[];
  queries?: Query[];
}

export const menuItemsConfig: MenuItem[] = [
  {
    title: "Overview",
    href: "/metrics",
    icon: HomeIcon,
    queries: [
      {
        title: "Running Queries",
        query: `SELECT * FROM system.processes WHERE is_cancelled = 0`,
        type: "table",
        description: "Currently running queries excluding system queries.",
      },
      {
        title: "Databases and Tables",
        query: `WITH
                  dbs AS (SELECT COUNT(*) AS total_databases FROM system.databases),
                  tbls AS (SELECT COUNT(*) AS total_tables FROM system.tables)
                SELECT dbs.total_databases, tbls.total_tables FROM dbs, tbls;`,
        type: "card",
        description: "Total number of databases and tables.",
      },
      {
        title: "Version and Uptime",
        query: `SELECT version() AS version, uptime() AS uptime;`,
        type: "card",
        description: "Current ClickHouse version and server uptime.",
      },
      {
        title: "Query Count Last 24 Hours",
        query: `
          SELECT toStartOfHour(event_time) AS event_time, user, COUNT(*) AS count
          FROM merge(system, '^query_log')
          WHERE type = 'QueryFinish' AND event_time >= (now() - INTERVAL 24 HOUR) AND user != ''
          GROUP BY event_time, user
          ORDER BY event_time ASC, count DESC`,
        type: "bar-chart",
        description: "Number of queries executed in the last 24 hours by user.",
      },
      {
        title: "Memory Usage Last 24h (Avg / 10 Minutes)",
        query: `SELECT toStartOfTenMinutes(event_time) as event_time,
                   avg(CurrentMetric_MemoryTracking) AS avg_memory,
                   formatReadableSize(avg_memory) AS readable_avg_memory
            FROM merge(system, '^metric_log')
            WHERE event_time >= (now() - INTERVAL 24 HOUR)
            GROUP BY event_time
            ORDER BY event_time ASC`,
        type: "area-chart",
        description: "Average memory usage over the last 24 hours.",
      },
      {
        title: "CPU Usage Last 24h (Avg / 10 Minutes)",
        query: `SELECT toStartOfTenMinutes(event_time) AS event_time,
                   avg(ProfileEvent_OSCPUVirtualTimeMicroseconds) / 1000000 as avg_cpu
            FROM merge(system, '^metric_log')
            WHERE event_time >= (now() - INTERVAL 24 HOUR)
            GROUP BY event_time
            ORDER BY event_time ASC`,
        type: "area-chart",
        description: "Average CPU usage over the last 24 hours.",
      },
      {
        title: "Merge and PartMutation Last 24h (Avg)",
        query: `SELECT toStartOfHour(event_time) AS event_time,
                   avg(CurrentMetric_Merge) AS avg_CurrentMetric_Merge,
                   avg(CurrentMetric_PartMutation) AS avg_CurrentMetric_PartMutation
            FROM merge(system, '^metric_log')
            WHERE event_time >= (now() - INTERVAL 24 HOUR)
            GROUP BY event_time
            ORDER BY event_time ASC`,
        type: "area-chart",
        description:
          "Average merge and part mutation metrics over the last 24 hours.",
      },
    ],
  },
  {
    title: "Tables",
    href: "/metrics/tables",
    icon: TableIcon,
    queries: [
      // Define queries specific to Tables here
      {
        title: "Total Tables",
        query: `SELECT COUNT(*) AS total_tables FROM system.tables WHERE lower(database) NOT IN ('system', 'information_schema') AND is_temporary = 0 AND engine LIKE '%MergeTree%'`,
        type: "card",
        description: "Total number of user-defined tables.",
      },
      // Add more table-related queries
    ],
  },
  {
    title: "Queries",
    href: "/metrics/queries",
    icon: TerminalSquareIcon,
    queries: [
      // Define queries specific to Queries here
      {
        title: "Running Queries Count",
        query: `SELECT COUNT(*) AS running_queries FROM system.processes WHERE is_cancelled = 0 AND query NOT LIKE '%system%'`,
        type: "card",
        description: "Number of currently running queries.",
      },
      // Add more queries-related queries
    ],
  },
  {
    title: "Merges",
    href: "/metrics/merges",
    icon: CombineIcon,
    queries: [
      // Define queries specific to Merges here
      {
        title: "Total Merges",
        query: `SELECT COUNT(*) AS total_merges FROM system.merges WHERE 1 = 1`,
        type: "card",
        description: "Total number of ongoing merges.",
      },
      // Add more merges-related queries
    ],
  },
  {
    title: "More",
    href: "/metrics/more",
    icon: PlusCircle,
    queries: [
      // Define queries specific to More here
      {
        title: "Disk Usage",
        query: `SELECT name, total_space AS size FROM system.disks`,
        type: "table",
        description: "Disk usage details.",
      },
      // Add more miscellaneous queries
    ],
  },
];
