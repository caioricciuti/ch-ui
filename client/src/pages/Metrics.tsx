import React, { useEffect, useState } from "react";
import {
  LineChart,
  Line,
  CartesianGrid,
  XAxis,
  YAxis,
  Tooltip,
  Legend,
  ResponsiveContainer,
} from "recharts";
import { Skeleton } from "@/components/ui/skeleton";
import { toast } from "sonner";
import useTabStore from "@/stores/tabs.store";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { ScrollArea } from "@/components/ui/scroll-area";
import CHUITable from "@/components/CHUITable"; // Import your CHUITable component
// Assuming you want to apply to all databases
const database = null; // set to null to apply to all databases

const timeInterval = "1 day"; // 1-day interval
const type = "'QueryFinish'"; // Example value
const user = "'default'"; // Example value
const queryKind = "'Select'"; // Example value

// Queries using the provided SQL commands
const queries = [
  {
    query: `SELECT version()`,
    title: "ClickHouse Version",
    plot: "Card",
    description: "Shows the ClickHouse server version.",
  },
  {
    query: `SELECT uptime() as uptime`,
    title: "Server Uptime",
    plot: "Card",
    description: "Displays the server uptime.",
  },
  {
    query: `SELECT count() as "Number of databases" FROM system.databases`,
    title: "Number of Databases",
    plot: "Card",
    description: "Total number of databases in the cluster.",
  },
  {
    query: `SELECT count() as "Number of tables" FROM system.tables ${
      database ? `WHERE database IN ('${database}')` : ""
    }`,
    title: "Number of Tables",
    plot: "Card",
    description: "Total number of tables across all databases.",
  },
  {
    query: `SELECT sum(total_rows) as "Number of rows" FROM system.tables ${
      database ? `WHERE database IN ('${database}')` : ""
    }`,
    title: "Number of Rows",
    plot: "Card",
    description: "Total number of rows across all databases.",
  },
  {
    query: `SELECT count() as "Number of columns" FROM system.columns ${
      database ? `WHERE database IN ('${database}')` : ""
    }`,
    title: "Number of Columns",
    plot: "Card",
    description: "Total number of columns across all databases.",
  },
  {
    query: `SELECT cluster, shard_num, replica_num, host_name, host_address, port, is_local, errors_count, slowdowns_count FROM system.clusters`,
    title: "Cluster Information",
    plot: "Table",
    description: "Details about the cluster nodes, including error counts.",
  },
  {
    query: `SELECT concatAssumeInjective(database, '.', table) as db_table, round(100 * progress, 1) as "progress" FROM system.merges ${
      database ? `WHERE database IN ('${database}')` : ""
    } ORDER BY progress DESC LIMIT 5`,
    title: "Top 5 Merges by Progress",
    plot: "Table",
    description: "Merges in progress, sorted by progress.",
  },
  {
    query: `SELECT concatAssumeInjective(database, '.', table) as db_table, round(elapsed, 1) as "elapsed", round(100 * progress, 1) as "progress", is_mutation, partition_id, result_part_path, source_part_paths, num_parts, formatReadableSize(total_size_bytes_compressed) as "total_size_compressed", formatReadableSize(bytes_read_uncompressed) as "read_uncompressed", formatReadableSize(bytes_written_uncompressed) as "written_uncompressed", columns_written, formatReadableSize(memory_usage) as "memory_usage", thread_id FROM system.merges ${
      database ? `WHERE database IN ('${database}')` : ""
    }`,
    title: "Detailed Merges Information",
    plot: "Table",
    description: "Detailed information about ongoing merges.",
  },
  {
    query: `SELECT concatAssumeInjective(database, '.', table, ' - ', mutation_id) as db_table, length(parts_to_do_names) as parts_remaining FROM system.mutations WHERE parts_remaining > 0 ${
      database ? `AND database IN ('${database}')` : ""
    } ORDER BY parts_remaining DESC`,
    title: "Top Mutations by Parts Remaining",
    plot: "Table",
    description: "Mutations with remaining parts, sorted by parts remaining.",
  },
  {
    query: `SELECT concatAssumeInjective(database, '.', table) as db_table, mutation_id, command, create_time, parts_to_do_names, is_done, latest_failed_part, if(latest_fail_time = '1970-01-01 01:00:00', 'success', 'failure') as success, if(latest_fail_time = '1970-01-01 01:00:00', '-', CAST(latest_fail_time, 'String')) as fail_time, latest_fail_reason FROM system.mutations ${
      database ? `WHERE database IN ('${database}')` : ""
    } ORDER BY is_done ASC, create_time DESC LIMIT 10`,
    title: "Recent Mutations",
    plot: "Table",
    description: "Recent mutations, including success/failure status.",
  },
  {
    query: `SELECT concatAssumeInjective(database, '.', table) as db_table, queue_size FROM system.replicas ${
      database ? `WHERE database IN ('${database}')` : ""
    } ORDER BY absolute_delay DESC LIMIT 10`,
    title: "Top 10 Replicas by Queue Size",
    plot: "Table",
    description: "Replicas sorted by queue size.",
  },
  {
    query: `SELECT concatAssumeInjective(database, '.', table) as db_table, is_leader, is_readonly, absolute_delay, queue_size, inserts_in_queue, merges_in_queue FROM system.replicas ${
      database ? `WHERE database IN ('${database}')` : ""
    } ORDER BY absolute_delay DESC LIMIT 10`,
    title: "Replica Status",
    plot: "Table",
    description: "Status of replicas, including delays and queue sizes.",
  },
  {
    query: `SELECT count() as "Total queries" FROM system.query_log WHERE type IN (${type}) AND initial_user IN (${user}) AND query_kind IN (${queryKind}) AND event_time >= now() - INTERVAL ${timeInterval}`,
    title: "Total Queries",
    plot: "Card",
    description: "Total number of queries executed in the last day.",
  },
  {
    query: `SELECT avg(memory_usage) as "Avg query memory", toStartOfInterval(query_start_time, INTERVAL 1 hour) as time FROM system.query_log WHERE type IN (${type}) AND initial_user IN (${user}) AND query_kind IN (${queryKind}) AND event_time >= now() - INTERVAL ${timeInterval} GROUP BY time ORDER BY time`,
    title: "Average Query Memory Usage",
    plot: "Line",
    description: "Average memory usage of queries over the last day.",
  },
  {
    query: `SELECT query_duration_ms as "Query time" FROM system.query_log WHERE type IN (${type}) AND initial_user IN (${user}) AND query_kind IN (${queryKind}) AND event_time >= now() - INTERVAL ${timeInterval} LIMIT 1000`,
    title: "Query Times",
    plot: "Table",
    description: "List of query execution times in the last day.",
  },
  {
    query: `SELECT initial_user, count() as c FROM system.query_log WHERE type IN (${type}) AND initial_user IN (${user}) AND query_kind IN (${queryKind}) GROUP BY initial_user LIMIT 100`,
    title: "Query Count by User",
    plot: "Table",
    description: "Number of queries per user in the last day.",
  },
  {
    query: `SELECT avg(query_duration_ms) as "Avg query time", toStartOfInterval(query_start_time, INTERVAL 1 hour) as time FROM system.query_log WHERE type IN (${type}) AND initial_user IN (${user}) AND query_kind IN (${queryKind}) AND event_time >= now() - INTERVAL ${timeInterval} GROUP BY time ORDER BY time`,
    title: "Average Query Duration",
    plot: "Line",
    description: "Average query duration over time.",
  },
  {
    query: `SELECT toStartOfInterval(query_start_time, INTERVAL 1 hour) as time, any(normalizeQuery(query)) AS normalized_query, count() as c FROM system.query_log WHERE type != 'QueryStart' AND event_time >= now() - INTERVAL ${timeInterval} AND initial_user IN (${user}) AND query_kind IN (${queryKind}) AND normalized_query_hash IN (SELECT normalized_query_hash FROM system.query_log WHERE type IN (${type}) AND event_time >= now() - INTERVAL ${timeInterval} AND query_kind IN (${queryKind}) GROUP BY normalized_query_hash ORDER BY count() DESC LIMIT 5) GROUP BY normalized_query_hash, time ORDER BY time`,
    title: "Top 5 Frequent Queries",
    plot: "Table",
    description: "The most frequent queries over the last day.",
  },
  {
    query: `SELECT toStartOfInterval(query_start_time, INTERVAL 1 hour) as time, any(normalizeQuery(query)) AS normalized_query, avg(query_duration_ms) as avg_query_duration FROM system.query_log WHERE type != 'QueryStart' AND event_time >= now() - INTERVAL ${timeInterval} AND type IN (${type}) AND initial_user IN (${user}) AND query_kind IN (${queryKind}) AND normalized_query_hash IN (SELECT normalized_query_hash FROM system.query_log WHERE type IN (${type}) AND initial_user IN (${user}) AND query_kind IN (${queryKind}) AND event_time >= now() - INTERVAL ${timeInterval} GROUP BY normalized_query_hash ORDER BY avg(query_duration_ms) DESC LIMIT 10) GROUP BY normalized_query_hash, time ORDER BY time`,
    title: "Top 10 Slowest Queries",
    plot: "Table",
    description: "The slowest queries executed over the last day.",
  },
  {
    query: `SELECT toStartOfInterval(query_start_time, INTERVAL 1 hour) as time, initial_user as user, count() as "number of queries by" FROM system.query_log WHERE query_kind IN (${queryKind}) AND type IN (${type}) AND event_time >= now() - INTERVAL ${timeInterval} AND initial_user IN (SELECT initial_user FROM system.query_log WHERE query_kind IN (${queryKind}) AND type IN (${type}) AND event_time >= now() - INTERVAL ${timeInterval} GROUP BY initial_user ORDER BY count() DESC LIMIT 10) GROUP BY initial_user, time ORDER BY time`,
    title: "Query Count by User Over Time",
    plot: "Line",
    description: "Number of queries per user over time.",
  },
  {
    query: `SELECT toStartOfInterval(query_start_time, INTERVAL 1 hour) as time, max(memory_usage) as "Max Memory Usage" FROM system.query_log WHERE event_time >= now() - INTERVAL ${timeInterval} GROUP BY time ORDER BY time DESC`,
    title: "Max Memory Usage",
    plot: "Line",
    description: "Maximum memory usage by queries over time.",
  },
  {
    query: `SELECT toStartOfInterval(toDateTime(event_time), INTERVAL 60 second), sum(read_rows) read_rows, sum(written_rows) written_rows FROM system.query_log WHERE event_time >= now() - INTERVAL ${timeInterval} GROUP BY event_time ORDER BY event_time ASC`,
    title: "Read/Write Operations",
    plot: "Line",
    description: "Read and write operations over the last day.",
  },
  {
    query: `SELECT query_start_time, type, query_duration_ms, initial_user, substring(query_id,1, 8) as query_id, query_kind, normalizeQuery(query) AS normalized_query, concat( toString(read_rows), ' rows / ', formatReadableSize(read_bytes) ) AS read, concat( toString(written_rows), ' rows / ', formatReadableSize(written_bytes) ) AS written, concat( toString(result_rows), ' rows / ', formatReadableSize(result_bytes) ) AS result, formatReadableSize(memory_usage) AS "memory usage" FROM system.query_log WHERE type IN (${type}) AND initial_user IN (${user}) AND query_kind IN (${queryKind}) AND event_time >= now() - INTERVAL ${timeInterval} ORDER BY query_duration_ms DESC LIMIT 1000`,
    title: "Query Details",
    plot: "Table",
    description: "Detailed information about the top 1000 queries executed.",
  },
];

// test
import useAuthStore from "@/stores/user.store";
import { useNavigate } from "react-router-dom";

function MetricsPage() {
  const { runQuery } = useTabStore();
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const [data, setData] = useState([]);
  const { user } = useAuthStore();
  const navigate = useNavigate();

  useEffect((): void => {
    if (!user?.activeOrganization) {
      navigate("/organizations");
      toast.error("Please select an organization to view metrics.");
    }
    const fetchMetrics = async () => {
      try {
        setLoading(true);
        setError(null);

        const results = await Promise.all(
          queries.map((q) => runQuery("", q.query))
        );

        const formattedData = results.map((result, index) => {
          if (result.error) {
            throw new Error(
              `Error in query ${queries[index].title}: ${result.error}`
            );
          }
          return {
            ...queries[index],
            data: result.data || [],
          };
        });

        setData(formattedData);
      } catch (err) {
        setError("Failed to load data.");
        toast.error(`Failed to load data: ${err.message || err}`);
      } finally {
        setLoading(false);
      }
    };

    fetchMetrics();
  }, [runQuery]);

  if (loading) {
    return (
      <div className="p-4 space-y-4">
        <Skeleton className="h-8 w-3/4" />
        <Skeleton className="h-32 w-full" />
        <Skeleton className="h-64 w-full" />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }

  if (error) {
    return <div className="p-4 text-red-500">{error}</div>;
  }

  const renderChart = (metric) => {
    const chartData = metric.data.map((item) => ({
      name: item.time,
      value: parseFloat(Object.values(item)[1]),
    }));

    return (
      <ResponsiveContainer width="100%" height={300}>
        <LineChart data={chartData}>
          <CartesianGrid strokeDasharray="3 3" />
          <XAxis dataKey="name" />
          <YAxis />
          <Tooltip />
          <Legend />
          <Line type="monotone" dataKey="value" stroke="#8884d8" />
        </LineChart>
      </ResponsiveContainer>
    );
  };

  const renderTable = (metric) => {
    const headers = Object.keys(metric.data[0] || {});
    const meta = headers.map((header) => ({ name: header, type: "String" }));

    return (
      <CHUITable
        result={{
          meta,
          data: metric.data,
          statistics: {
            elapsed: 0,
            rows_read: metric.data.length,
            bytes_read: 0,
          },
        }}
      />
    );
  };

  const renderMetric = (metric) => {
    switch (metric.plot) {
      case "Card":
        return (
          <Card className="col-span-1">
            <CardHeader>
              <CardTitle>{metric.title}</CardTitle>
            </CardHeader>
            <CardContent>
              <p className="text-2xl font-bold">
                {metric.data[0]?.[Object.keys(metric.data[0])[0]] || "N/A"}
              </p>
              <p className="text-sm text-muted-foreground">
                {metric.description}
              </p>
            </CardContent>
          </Card>
        );
      case "Table":
        return (
          <Card className="col-span-3">
            <CardHeader>
              <CardTitle>{metric.title}</CardTitle>
            </CardHeader>
            <CardContent>
              <ScrollArea className="h-[400px]">
                {renderTable(metric)}
              </ScrollArea>
            </CardContent>
          </Card>
        );
      case "Line":
        return (
          <Card className="col-span-3">
            <CardHeader>
              <CardTitle>{metric.title}</CardTitle>
            </CardHeader>
            <CardContent>
              {renderChart(metric)}
              <p className="text-sm text-muted-foreground mt-4">
                {metric.description}
              </p>
            </CardContent>
          </Card>
        );
      default:
        return null;
    }
  };

  const groupedMetrics = {
    "System Info": data.filter((m) =>
      [
        "ClickHouse Version",
        "Server Uptime",
        "Number of Databases",
        "Number of Tables",
        "Number of Rows",
        "Number of Columns",
      ].includes(m.title)
    ),
    "Cluster Info": data.filter((m) =>
      [
        "Cluster Information",
        "Top 5 Merges by Progress",
        "Detailed Merges Information",
      ].includes(m.title)
    ),
    "Query Metrics": data.filter((m) =>
      [
        "Total Queries",
        "Average Query Memory Usage",
        "Query Times",
        "Query Count by User",
        "Average Query Duration",
      ].includes(m.title)
    ),
    Performance: data.filter((m) =>
      [
        "Top 5 Frequent Queries",
        "Top 10 Slowest Queries",
        "Max Memory Usage",
        "Read/Write Operations",
      ].includes(m.title)
    ),
    "Detailed Info": data.filter((m) => ["Query Details"].includes(m.title)),
  };

  return (
    <ScrollArea className="h-screen">
      <div className="p-8 space-y-8">
        <h1 className="text-3xl font-bold">Cluster Metrics</h1>
        <Tabs defaultValue="System Info">
          <TabsList>
            {Object.keys(groupedMetrics).map((group) => (
              <TabsTrigger key={group} value={group}>
                {group}
              </TabsTrigger>
            ))}
          </TabsList>
          {Object.entries(groupedMetrics).map(([group, metrics]) => (
            <TabsContent key={group} value={group}>
              <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
                {metrics.map((metric, index) => (
                  <React.Fragment key={index}>
                    {renderMetric(metric)}
                  </React.Fragment>
                ))}
              </div>
            </TabsContent>
          ))}
        </Tabs>
      </div>
    </ScrollArea>
  );
}

export default MetricsPage;
