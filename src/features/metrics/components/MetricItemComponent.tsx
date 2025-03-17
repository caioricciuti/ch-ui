import { useState, useEffect, ComponentType, ReactNode } from "react";
import {
  MetricItem,
} from "@/features/metrics/config/metricsConfig";
import {
  Bar,
  BarChart,
  Line,
  LineChart,
  Area,
  AreaChart,
  Pie,
  PieChart,
  Radar,
  RadarChart,
  RadialBar,
  RadialBarChart,
  XAxis,
  YAxis,
  CartesianGrid,
  PolarGrid,
  PolarAngleAxis,
  PolarRadiusAxis,
  ResponsiveContainer,
  Tooltip as RechartsTooltip,
  Legend as RechartsLegend,
  BarProps,
  LineProps,
  AreaProps,
  PieProps,
  RadarProps,
  RadialBarProps,
  LegendType,
} from "recharts";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  ChartContainer,
  ChartTooltipContent,
  ChartLegendContent,
} from "@/components/ui/chart";
import CHUITable from "@/components/common/table/CHUItable";
import { Skeleton } from "@/components/ui/skeleton";
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
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { toast } from "sonner";
import ErrorCard from "@/features/metrics/components/ErrorCard";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import useAppStore from "@/store";
import React from "react";

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

type ChartProps = {
  data: Record<string, any>[];
  chartType: string;
  chartConfig: any;
};

type ChartComponentType = typeof BarChart | typeof LineChart | typeof AreaChart | typeof PieChart | typeof RadarChart | typeof RadialBarChart;
type DataComponentType = ComponentType<BarProps | LineProps | AreaProps | PieProps | RadarProps | RadialBarProps>;

const MemoizedChart: React.FC<ChartProps> = React.memo(({ 
  data, 
  chartType, 
  chartConfig 
}) => {
  let ChartComponent: ChartComponentType;
  let DataComponent: DataComponentType;

  switch (chartType) {
    case "line":
      ChartComponent = LineChart;
      DataComponent = Line as DataComponentType;
      break;
    case "area":
      ChartComponent = AreaChart;
      DataComponent = Area as DataComponentType;
      break;
    case "pie":
      ChartComponent = PieChart;
      DataComponent = Pie as DataComponentType;
      break;
    case "radar":
      ChartComponent = RadarChart;
      DataComponent = Radar as DataComponentType;
      break;
    case "radial":
      ChartComponent = RadialBarChart;
      DataComponent = RadialBar as DataComponentType;
      break;
    case "bar":
    default:
      ChartComponent = BarChart;
      DataComponent = Bar as DataComponentType;
  }

  const dataKeys = Object.keys(chartConfig).filter(
    (key) => key !== "indexBy"
  );

  if (!dataKeys.length || !chartConfig.indexBy) {
    return <div className="text-muted-foreground font-bold">Invalid chart configuration</div>;
  }

  const dataKey = dataKeys[0];
  const maxValue = Math.max(
    ...data.map((d: any) => d[dataKey as string])
  );
  const yAxisMax = Math.ceil(maxValue * 1.1);

  const renderDataComponent = (key: string) => {
    const commonProps = {
      key,
      fill: `var(--color-${key})`,
      stroke: `var(--color-${key})`,
      strokeWidth: 2,
      dot: false,
      dataKey: key,
      legendType: "none" as LegendType,
    };

    if (chartType === "pie" || chartType === "radial") {
      return (
        <DataComponent
          {...commonProps}
          data={data}
          nameKey={chartConfig.indexBy}
          label={true}
        />
      );
    } else {
      return (
        <DataComponent
          {...commonProps}
          radius={chartType === "bar" ? 4 : undefined}
          fillOpacity={chartType === "area" ? 0.3 : 1}
          layout={chartType === "bar" ? "vertical" : undefined}
        />
      );
    }
  };

  return (
    <ChartContainer
      config={chartConfig}
      className="mt-4 h-[250px] w-full"
    >
      <ResponsiveContainer width="100%" height="100%">
        <ChartComponent
          data={data}
          margin={{ top: 5, right: 30, left: 40, bottom: 5 }}
        >
          {["bar", "line", "area"].includes(chartType) && (
            <>
              <CartesianGrid strokeDasharray="3 3" vertical={false} />
              <XAxis
                dataKey={chartConfig.indexBy}
                tickLine={false}
                axisLine={false}
                tick={{ fontSize: 12 }}
                tickMargin={10}
                tickFormatter={(value) => value.toString().slice(0, 10)}
              />
              <YAxis
                tickLine={false}
                axisLine={false}
                tick={{ fontSize: 12 }}
                width={40}
                domain={[0, yAxisMax]}
                allowDataOverflow={false}
              />
            </>
          )}
          {chartType === "radar" && (
            <>
              <PolarGrid />
              <PolarAngleAxis dataKey={chartConfig.indexBy} />
              <PolarRadiusAxis />
            </>
          )}
          <RechartsTooltip content={<ChartTooltipContent indicator="dot" />} />
          <RechartsLegend content={<ChartLegendContent />} />
          {dataKeys.map(renderDataComponent)}
        </ChartComponent>
      </ResponsiveContainer>
    </ChartContainer>
  );
});

function MetricItemComponent({ item }: Props) {
  const { runQuery } = useAppStore();
  const [queryResult, setQueryResult] = useState<QueryResult | null>(null);
  const [loading, setLoading] = useState<boolean>(true);
  const [errorMessage, setErrorMessage] = useState<string>("");

  const fetchData = React.useCallback(async () => {
    try {
      setLoading(true);
      const result = await runQuery(item.query);

      if (result.error) {
        setQueryResult({ data: [], error: result.error });
        setErrorMessage(result.error);
      } else if (result.data && result.data.length > 0) {
        // Transform the result to match our QueryResult interface
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
  }, [item.query, runQuery]);

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  // Memoize handlers
  const handleDownloadData = React.useCallback((data: QueryResult) => {
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

  if (loading) {
    return (
      <Card>
        <CardHeader>
          <Skeleton className="h-6 w-3/4" />
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
          <div key={key}>
            <span className="font-extrabold text-3xl">{value?.toString()}</span>
          </div>
        ))}
      </div>
    ));
  };

  const renderChart = () => {
    if (!queryResult || !queryResult.data || queryResult.data.length === 0) {
      return <div className="text-muted-foreground font-bold">No data</div>;
    }

    return (
      <MemoizedChart
        data={queryResult.data}
        chartType={item.chartType || "bar"}
        chartConfig={item.chartConfig || { indexBy: "" }}
      />
    );
  };

  const renderTable = () => {
    if (!queryResult || !queryResult.data || queryResult.data.length === 0) {
      return <div className="text-muted-foreground font-bold">No data</div>;
    }
    return (
      <CHUITable
        result={{
          meta: queryResult.meta || [],
          data: queryResult.data,
          statistics: {
            elapsed: queryResult.statistics?.elapsed || 0,
            rows_read: queryResult.statistics?.rows_read || 0,
            bytes_read: queryResult.statistics?.bytes_read || 0,
          },
        }}
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

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center justify-between">
          <CardTitle className="text-lg truncate">{item.title}</CardTitle>
          <div className="flex items-center space-x-1">
            <TooltipProvider>
              <Tooltip>
                <TooltipTrigger>
                  <InfoIcon className="w-5 h-5 text-muted-foreground cursor-pointer" />
                </TooltipTrigger>
                <TooltipContent>
                  <div className="p-2 max-w-[350px]">
                    <p className="text-sm text-muted-foreground">
                      {item.description}
                    </p>
                  </div>
                </TooltipContent>
              </Tooltip>
            </TooltipProvider>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="ghost" size="icon">
                  <EllipsisVertical className="w-4 h-4" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem
                  onClick={() => {
                    navigator.clipboard.writeText(item.query);
                    toast.success("Query copied to clipboard");
                  }}
                >
                  <CopyIcon className="w-4 h-4 mr-2" />
                  Copy Query
                </DropdownMenuItem>
                <DropdownMenuItem
                  onClick={() => handleDownloadData(queryResult!)}
                >
                  <DownloadCloud className="w-4 h-4 mr-2" />
                  Download Data
                </DropdownMenuItem>
                <DropdownMenuItem
                  onClick={() => {
                    const jsonString = JSON.stringify(
                      queryResult!.data,
                      null,
                      2
                    );
                    navigator.clipboard
                      .writeText(jsonString)
                      .then(() => toast.success("Data copied to clipboard"))
                      .catch(() =>
                        toast.error("Failed to copy data to clipboard")
                      );
                  }}
                >
                  <Braces className="w-4 h-4 mr-2" />
                  Copy Data
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </div>
      </CardHeader>
      <CardContent className="truncate">{renderContent()}</CardContent>
    </Card>
  );
}

export default MetricItemComponent;
