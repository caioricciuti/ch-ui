// src/pages/metrics/more/index.tsx
import { useEffect, useState } from "react";
import { useLocation } from "react-router-dom";
import { toast } from "sonner";
import useMetricsStore, { Metric } from "@/stores/metrics.store";
import { ScrollArea } from "@/components/ui/scroll-area";
import useAuthStore from "@/stores/user.store";
import { Loader2, CopyIcon } from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { CHUITable } from "@/components/CHUITable";
import {
  LineChart,
  BarChart,
  AreaChart,
  Line,
  Bar,
  Area,
  CartesianGrid,
  XAxis,
  YAxis,
  Tooltip,
  Legend,
  ResponsiveContainer,
} from "recharts";
import { ChartTooltipContent, ChartLegendContent } from "@/components/ui/chart";
import MetricsNavigationMenu from "@/components/MetricsNavigationMenu";
import { useNavigate } from "react-router-dom";

function MorePage() {
  const location = useLocation();
  const navigate = useNavigate();
  const { metricsData, lastFetched, fetchMetrics } = useMetricsStore();
  const { user } = useAuthStore();
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const CACHE_DURATION = 5 * 60 * 1000; // 5 minutes

  useEffect(() => {
    if (!user?.activeOrganization && !loading) {
      toast.error("Please select an organization to view metrics.");
      navigate("/organizations"); // Redirect to organizations page
      return;
    }

    const now = Date.now();
    if (!metricsData || !lastFetched || now - lastFetched >= CACHE_DURATION) {
      const currentPath = location.pathname;
      fetchMetrics(setLoading, setError, currentPath);
    }
  }, [
    user,
    metricsData,
    lastFetched,
    fetchMetrics,
    location.pathname,
    navigate,
  ]);

  if (!user?.activeOrganization) {
    return null; // Or a placeholder component
  }

  /**
   * Handler to copy a SQL query to the clipboard.
   * @param query SQL query string.
   */
  const handleCopyQuery = (query: string) => {
    navigator.clipboard.writeText(query);
    toast.success("Query copied to clipboard!");
  };

  /**
   * Renders a metric card based on its type.
   * @param metric Metric object containing data and configuration.
   */
  const renderMetric = (metric: Metric) => {
    return (
      <Card key={metric.title} className="shadow-md">
        <CardHeader className="flex justify-between items-center">
          <CardTitle className="text-sm">{metric.title}</CardTitle>
          <Button
            variant="ghost"
            size="icon"
            onClick={() => handleCopyQuery(metric.query)}
            aria-label={`Copy query for ${metric.title}`}
          >
            <CopyIcon className="w-4 h-4" />
          </Button>
        </CardHeader>
        <CardContent>
          {metric.type === "card" && (
            <>
              <p className="text-2xl font-bold">
                {Object.values(metric.data[0] || {}).join(" / ") || "N/A"}
              </p>
              <p className="text-xs text-muted-foreground">
                {metric.description}
              </p>
            </>
          )}
          {metric.type === "table" && (
            <ScrollArea className="h-[400px]">
              <CHUITable
                result={{
                  meta: Object.keys(metric.data[0] || {}).map((key) => ({
                    name: key,
                    type: "String",
                  })),
                  data: metric.data,
                  statistics: {
                    elapsed: 0,
                    rows_read: metric.data.length,
                    bytes_read: 0,
                  },
                }}
              />
            </ScrollArea>
          )}
          {(metric.type === "line" ||
            metric.type === "bar-chart" ||
            metric.type === "area-chart") && (
            <>
              <div className="h-64">
                <ResponsiveContainer width="100%" height="100%">
                  <>
                    {metric.type === "line" && (
                      <LineChart data={metric.data}>
                        <CartesianGrid strokeDasharray="3 3" />
                        <XAxis dataKey="event_time" />
                        <YAxis />
                        <Tooltip content={<ChartTooltipContent />} />
                        <Legend content={<ChartLegendContent />} />
                        <Line
                          type="monotone"
                          dataKey="value"
                          stroke="hsl(var(--chart-1))"
                          dot={false}
                        />
                      </LineChart>
                    )}
                    {metric.type === "bar-chart" && (
                      <BarChart data={metric.data}>
                        <CartesianGrid strokeDasharray="3 3" />
                        <XAxis dataKey="event_time" />
                        <YAxis />
                        <Tooltip content={<ChartTooltipContent />} />
                        <Legend content={<ChartLegendContent />} />
                        <Bar
                          dataKey="count"
                          fill="hsl(var(--chart-2))"
                          radius={[4, 4, 0, 0]}
                        />
                      </BarChart>
                    )}
                    {metric.type === "area-chart" && (
                      <AreaChart data={metric.data}>
                        <defs>
                          <linearGradient
                            id={`color${metric.title}`}
                            x1="0"
                            y1="0"
                            x2="0"
                            y2="1"
                          >
                            <stop
                              offset="5%"
                              stopColor="hsl(var(--chart-3))"
                              stopOpacity={0.8}
                            />
                            <stop
                              offset="95%"
                              stopColor="hsl(var(--chart-3))"
                              stopOpacity={0}
                            />
                          </linearGradient>
                        </defs>
                        <CartesianGrid strokeDasharray="3 3" />
                        <XAxis dataKey="event_time" />
                        <YAxis />
                        <Tooltip content={<ChartTooltipContent />} />
                        <Legend content={<ChartLegendContent />} />
                        <Area
                          type="monotone"
                          dataKey="value"
                          stroke="hsl(var(--chart-3))"
                          fillOpacity={1}
                          fill={`url(#color${metric.title})`}
                        />
                      </AreaChart>
                    )}
                  </>
                </ResponsiveContainer>
              </div>
              {metric.description && (
                <p className="text-xs text-muted-foreground mt-4">
                  {metric.description}
                </p>
              )}
            </>
          )}
        </CardContent>
      </Card>
    );
  };

  /**
   * Groups metrics into categories for organized display.
   */
  const groupedMetrics: Record<string, Metric[]> = {
    More:
      metricsData?.filter((m) =>
        [
          "Settings",
          "MergeTree Settings",
          "Disks",
          "Backups",
          "Metrics",
          "Asynchronous Metrics",
          "Custom Dashboard",
          "Clusters",
          "Zookeeper",
          "Connections",
        ].includes(m.title)
      ) || [],
  };

  return (
    <div className="p-4 space-y-6">
      <div className="flex flex-col md:flex-row items-start md:items-center justify-between">
        <MetricsNavigationMenu />
      </div>
      {loading && (
        <div className="flex justify-center items-center h-64">
          <Loader2 className="h-8 w-8 animate-spin" />
        </div>
      )}
      {error && (
        <Alert variant="destructive">
          <AlertTitle>Error</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}
      <ScrollArea className="h-[calc(100vh-200px)]">
        {metricsData && (
          <div className="space-y-6">
            {Object.entries(groupedMetrics).map(([group, metrics]) => (
              <div key={group} className="space-y-6">
                <h2 className="text-2xl font-semibold">{group}</h2>
                <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
                  {metrics.map((metric) => renderMetric(metric))}
                </div>
              </div>
            ))}
          </div>
        )}
      </ScrollArea>
    </div>
  );
}

export default MorePage;
