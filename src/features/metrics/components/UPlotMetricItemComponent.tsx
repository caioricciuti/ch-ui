import { useState, useEffect, useCallback, useRef } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import {
  CopyIcon,
  InfoIcon,
  EllipsisVertical,
  DownloadCloud,
  Braces,
  RefreshCw,
  Zap,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { toast } from "sonner";
import ErrorCard from "./ErrorCard";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import useAppStore from "@/store";
import AgTable from "@/components/common/AgTable";
import { useTimeRange } from "../context/TimeRangeContext";
import { interpolateQuery, previewQuery } from "../utils/queryInterpolation";
import UPlotMetricChart from "./UPlotMetricChart";
import { MetricItem } from "../config/metricsConfig";

// removed inline statistics badges for cleaner UI

interface Props {
  item: MetricItem;
}

interface TableMeta {
  name: string;
  type: string;
}

interface QueryResult {
  data: Record<string, any>[];
  error?: string | null;
  meta?: TableMeta[];
  statistics?: {
    elapsed: number;
    rows_read: number;
    bytes_read: number;
  };
}

function UPlotMetricItemComponent({ item }: Props) {
  const { runQuery } = useAppStore();
  const { timeRange } = useTimeRange();
  const [queryResult, setQueryResult] = useState<QueryResult | null>(null);
  const [loading, setLoading] = useState<boolean>(true);
  const [errorMessage, setErrorMessage] = useState<string>("");
  const [queryPreview, setQueryPreview] = useState<string>("");
  const chartContainerRef = useRef<HTMLDivElement>(null);
  const [chartHeight, setChartHeight] = useState<number>(250);

  const fetchData = useCallback(async () => {
    try {
      setLoading(true);

      // Interpolate query with current time range
      const interpolatedQuery = interpolateQuery(item.query, timeRange);
      setQueryPreview(interpolatedQuery);

      const result = await runQuery(interpolatedQuery);

      if (result.error) {
        setQueryResult({ data: [], error: result.error });
        setErrorMessage(result.error);
      } else if (result.data && result.data.length > 0) {
        const transformedResult: QueryResult = {
          data: result.data,
          meta: result.meta as TableMeta[],
          statistics: {
            elapsed: result.statistics?.elapsed || 0,
            rows_read: result.statistics?.rows_read || 0,
            bytes_read: result.statistics?.bytes_read || 0,
          },
        };
        setQueryResult(transformedResult);
        setErrorMessage("");
      } else {
        setQueryResult({ data: [] });
        setErrorMessage("No data returned from the query.");
      }
    } catch (err: any) {
      console.error(err);
      setQueryResult({ data: [] });
      setErrorMessage(err.message || "An unexpected error occurred.");
    } finally {
      setLoading(false);
    }
  }, [item.query, runQuery, timeRange]);

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  // Observe container size for charts
  useEffect(() => {
    if (!chartContainerRef.current || item.type !== 'chart') return;

    const updateChartHeight = () => {
      if (chartContainerRef.current) {
        const rect = chartContainerRef.current.getBoundingClientRect();
        // Account for card header (~40px) and minimal padding
        const availableHeight = Math.max(200, rect.height - 50);
        setChartHeight(availableHeight);
      }
    };

    const resizeObserver = new ResizeObserver(updateChartHeight);
    resizeObserver.observe(chartContainerRef.current);
    updateChartHeight(); // Initial calculation

    return () => {
      resizeObserver.disconnect();
    };
  }, [item.type]);

  const handleDownloadData = useCallback((data: QueryResult) => {
    if (!data) return;

    const blob = new Blob([JSON.stringify(data, null, 2)], {
      type: "application/json",
    });
    const fileName = `${item.title.replace(/\s+/g, "_").toLowerCase()}.json`;
    const link = document.createElement("a");
    if (link.download !== undefined) {
      const url = URL.createObjectURL(blob);
      link.setAttribute("href", url);
      link.setAttribute("download", fileName);
      link.style.visibility = "hidden";
      document.body.appendChild(link);
      link.click();
      document.body.removeChild(link);
      URL.revokeObjectURL(url);
    }
  }, [item.title]);

  const handleCopyQuery = useCallback(() => {
    const preview = previewQuery(item.query, timeRange);
    navigator.clipboard.writeText(preview.interpolated);
    toast.success("Interpolated query copied to clipboard");
  }, [item.query, timeRange]);

  if (loading) {
    return (
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <Skeleton className="h-6 w-3/4" />
            <div className="flex items-center gap-1">
              <Skeleton className="h-6 w-6" />
              <Skeleton className="h-6 w-6" />
            </div>
          </div>
        </CardHeader>
        <CardContent>
          <Skeleton className="h-[250px] w-full" />
        </CardContent>
      </Card>
    );
  }

  if (errorMessage) {
    return (
      <ErrorCard
        item={item}
        errorMessage={errorMessage}
        fetchData={fetchData}
        queryPreview={queryPreview}
      />
    );
  }

  const renderCardContent = () => {
    if (!queryResult || !queryResult.data || queryResult.data.length === 0) {
      return <div className="text-muted-foreground font-bold">No data</div>;
    }

    return queryResult.data.map((item: any, index: number) => (
      <div key={index} className="mb-2">
        {Object.entries(item).map(([key, value]) => (
          <div key={key} className="space-y-1">
            <span className="font-semibold text-xl">{value?.toString()}</span>
            <div className="text-[10px] text-muted-foreground capitalize">{key.replace(/_/g, ' ')}</div>
          </div>
        ))}
      </div>
    ));
  };

  const renderChart = () => {
    if (!queryResult || !queryResult.data || queryResult.data.length === 0) {
      return <div className="text-muted-foreground font-bold">No data</div>;
    }

    if (!item.chartConfig || !item.chartConfig.indexBy) {
      return <div className="text-muted-foreground font-bold">Invalid chart configuration</div>;
    }

    return (
      <UPlotMetricChart
        data={queryResult.data}
        chartType={(item.chartType as 'line' | 'area' | 'bar') || 'line'}
        chartConfig={item.chartConfig}
        height={250}
      />
    );
  };

  const renderTable = () => {
    if (!queryResult || !queryResult.data || queryResult.data.length === 0) {
      return <div className="text-muted-foreground font-bold">No data</div>;
    }
    return (
      <AgTable
        data={queryResult}
        height="100%"
        showMetadata={item.showTableMetadata}
        showStatistics={item.showTableStatistics}
        showHeader={!item.hideTableHeader}
      />
    );
  };

  const renderContent = () => {
    switch (item.type) {
      case "card":
        return renderCardContent();
      case "chart":
        return renderChart();
      case "table":
        return renderTable();
      default:
        return null;
    }
  };

  // Statistics badges removed (elapsed/rows/bytes) per design feedback

  return (
    <Card className={cn("h-full flex flex-col", item.type === 'chart' && "overflow-hidden")} ref={item.type === 'chart' ? chartContainerRef : undefined}>
      <CardHeader className={cn("shrink-0", item.type === 'chart' ? "pb-1 pt-3" : "pb-2")}>
        <div className="flex items-center justify-between">
          <CardTitle className="text-lg truncate flex items-center gap-2">
            {item.title}
            {item.type === 'chart' && <Zap className="h-4 w-4 text-primary" />}
          </CardTitle>
          <div className="flex items-center space-x-1">
            <TooltipProvider>
              <Tooltip>
                <TooltipTrigger>
                  <InfoIcon className="w-4 h-4 text-muted-foreground cursor-pointer" />
                </TooltipTrigger>
                <TooltipContent>
                  <div className="p-2 max-w-[350px]">
                    <p className="text-sm text-muted-foreground mb-0">
                      {item.description}
                    </p>
                  </div>
                </TooltipContent>
              </Tooltip>
            </TooltipProvider>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="ghost" size="sm">
                  <EllipsisVertical className="w-4 h-4" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem onClick={handleCopyQuery}>
                  <CopyIcon className="w-4 h-4 mr-2" />
                  Copy Query
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => fetchData()}>
                  <RefreshCw className="w-4 h-4 mr-2" />
                  Refresh Data
                </DropdownMenuItem>
                <DropdownMenuItem
                  onClick={() => handleDownloadData(queryResult!)}
                >
                  <DownloadCloud className="w-4 h-4 mr-2" />
                  Download Data
                </DropdownMenuItem>
                <DropdownMenuItem
                  onClick={() => {
                    const jsonString = JSON.stringify(queryResult!.data, null, 2);
                    navigator.clipboard
                      .writeText(jsonString)
                      .then(() => toast.success("Data copied to clipboard"))
                      .catch(() => toast.error("Failed to copy data to clipboard"));
                  }}
                >
                  <Braces className="w-4 h-4 mr-2" />
                  Copy Data
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </div>
        {/* Removed noisy inline statistics badges */}
      </CardHeader>
      <CardContent className={cn(
        "flex-1 min-h-0",
        item.type === 'chart' ? "p-2 pt-0 pb-3" : "pt-0 overflow-auto"
      )}>
        <div className={cn("h-full", item.type === 'chart' && "flex flex-col")}>
          {item.type === 'chart' ? (
            <UPlotMetricChart
              data={queryResult?.data || []}
              chartType={(item.chartType as 'line' | 'area' | 'bar') || 'line'}
              chartConfig={item.chartConfig!}
              height={chartHeight}
            />
          ) : (
            renderContent()
          )}
        </div>
      </CardContent>
    </Card>
  );
}

export default UPlotMetricItemComponent;
