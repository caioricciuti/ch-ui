import { useState, useEffect, useCallback } from "react";
import {
  MetricItem,
  ChartDataConfig,
  CustomChartConfig
} from "@/features/metrics/config/metricsConfig";
import {
  SimpleBarChart,
  LineChart,
  AreaChart,
  PieChart,
  RadarChart,
  DonutChart
} from "@carbon/charts-react";
import {
  BarChartOptions,
  LineChartOptions,
  AreaChartOptions,
  PieChartOptions,
  RadarChartOptions,
  DonutChartOptions,
  ScaleTypes
} from "@carbon/charts";
import "@carbon/charts-react/styles.css";
import "./carbonCharts.css"; // Import custom Carbon Charts styles
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
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
  RefreshCw
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
import MultiSeriesChart from "./MultiSeriesChart";
import { useTheme } from "./CarbonChartsTheme";
import AgTable from "@/components/common/AgTable";
import MetricsWrapper from "./MetricsWrapper";

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
  chartConfig: CustomChartConfig;
};

// Helper to check if a chart config has multiple series
const isMultiSeriesConfig = (config: CustomChartConfig): boolean => {
  // Get all keys that aren't 'indexBy' and are objects (likely to be series)
  const seriesKeys = Object.keys(config)
    .filter(key => key !== "indexBy")
    .filter(key => typeof config[key] === "object");
  
  // If we have more than one series, it's a multi-series chart
  return seriesKeys.length > 1;
};

// Add this helper function at the top of the file, before any components
const isDateTimeField = (data: Record<string, any>[], field: string): boolean => {
  if (!data || !data.length) return false;
  const value = data[0][field];
  // Check if it's a date string format (YYYY-MM-DD)
  return typeof value === 'string' && /^\d{4}-\d{2}-\d{2}/.test(value);
};

// Add this function to format datetime values for axis labels
const formatDateTime = (dateTimeStr: string): string => {
  try {
    const date = new Date(dateTimeStr);
    return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  } catch {
    return dateTimeStr;
  }
};

// Add this function to sample data for time series
const sampleTimeSeriesData = (data: Record<string, any>[], indexField: string, maxPoints = 15): Record<string, any>[] => {
  if (!data || data.length <= maxPoints) return data;
  
  // Sort by the index field if it's a date
  const sortedData = [...data].sort((a, b) => {
    const aVal = a[indexField];
    const bVal = b[indexField];
    return aVal < bVal ? -1 : aVal > bVal ? 1 : 0;
  });
  
  const step = Math.ceil(sortedData.length / maxPoints);
  const sampledData: Record<string, any>[] = [];
  
  for (let i = 0; i < sortedData.length; i += step) {
    sampledData.push(sortedData[i]);
  }
  
  // Always include the last point for completeness
  if (sampledData.length > 0 && sampledData[sampledData.length - 1] !== sortedData[sortedData.length - 1]) {
    sampledData.push(sortedData[sortedData.length - 1]);
  }
  
  return sampledData;
};

const CarbonChart: React.FC<ChartProps> = React.memo(({ 
  data, 
  chartType, 
  chartConfig 
}) => {
  // Get theme from context
  const { theme } = useTheme();
  
  // Early return if data or configuration is invalid
  if (!data || !chartConfig?.indexBy) {
    return <div className="text-muted-foreground font-bold">Invalid chart configuration</div>;
  }
  
  // Check if indexBy field is a datetime
  const indexBy = chartConfig.indexBy;
  const isDateTime = isDateTimeField(data, indexBy);
  
  // Sample time series data to avoid overwhelming the chart
  const processedData = isDateTime ? sampleTimeSeriesData(data, indexBy) : data;
  
  // Detect if this is time series data and automatically set chart type if needed
  let effectiveChartType = chartType;
  if (isDateTime && (chartType === 'bar' || !chartType)) {
    // For time series data, line charts work better than bar charts
    effectiveChartType = 'line';
  }
  
  // Check if this is a multi-series chart
  if (isMultiSeriesConfig(chartConfig)) {
    // Extract series keys (all keys except indexBy that are objects)
    const series = Object.keys(chartConfig)
      .filter(key => key !== "indexBy")
      .filter(key => typeof chartConfig[key] === "object");
    
    // Create labels and colors mapping
    const labels: Record<string, string> = {};
    const colors: Record<string, string> = {};
    
    series.forEach(key => {
      const config = chartConfig[key] as ChartDataConfig;
      if (config.label) {
        labels[key] = config.label.toString();
      }
      if (config.color) {
        colors[key] = config.color;
      }
    });
    
    return (
      <div className="carbon-chart-container">
        <MultiSeriesChart 
          data={processedData}
          chartType={(effectiveChartType as 'line' | 'bar' | 'area') || 'line'}
          indexBy={chartConfig.indexBy as string}
          series={series}
          labels={labels}
          colors={colors}
          theme={theme}
          isDateTime={isDateTime}
        />
      </div>
    );
  }
  
  // Extract key configs for single series charts
  const dataKeys = Object.keys(chartConfig).filter(key => key !== "indexBy");
  
  // Format data for Carbon Charts
  const carbonData = processedData.map(item => {
    const result: Record<string, any> = {
      group: item[indexBy]?.toString() || ""
    };
    
    dataKeys.forEach(key => {
      if (typeof item[key] !== 'undefined') {
        result.value = item[key];
        // Store the key for reference
        result.key = key;
      }
    });
    
    return result;
  });

  // Get color from config
  const dataKey = dataKeys[0];
  const color = dataKey && typeof chartConfig[dataKey] === 'object' 
    ? (chartConfig[dataKey] as ChartDataConfig).color || "var(--chart-1)"
    : "var(--chart-1)";

  // Prepare base chart options
  const baseOptions = {
    title: "",
    height: "100%",
    width: "100%",
    toolbar: {
      enabled: false
    },
    legend: {
      enabled: true,
      alignment: "center"
    },
    color: {
      scale: {
        // Use the first dataKey for the color
        [dataKeys[0]]: color
      }
    },
    theme: theme,
    resizable: true
  };

  // Add axis configuration based on whether it's a datetime field
  const axesConfig = isDateTime ? {
    left: {
      mapsTo: "value",
      scaleType: ScaleTypes.LINEAR
    },
    bottom: {
      mapsTo: "group",
      scaleType: ScaleTypes.LABELS,
      title: "",
      ticks: {
        // Show fewer ticks for datetime values
        max: 6
      },
      formatter: (value: string) => formatDateTime(value)
    }
  } : {
    left: {
      mapsTo: "value",
      scaleType: ScaleTypes.LINEAR
    },
    bottom: {
      mapsTo: "group",
      scaleType: ScaleTypes.LABELS,
      // If too many labels, only show a subset
      ticks: processedData.length > 20 ? { max: 10 } : undefined
    }
  };

  // Create chart-specific options
  switch (effectiveChartType) {
    case "line": {
      const options: LineChartOptions = {
        ...baseOptions,
        axes: axesConfig,
        curve: "curveMonotoneX"
      };
      return (
        <div className="carbon-chart-container">
          <LineChart data={carbonData} options={options} />
        </div>
      );
    }
    case "area": {
      const options: AreaChartOptions = {
        ...baseOptions,
        axes: axesConfig,
        curve: "curveMonotoneX"
      };
      return (
        <div className="carbon-chart-container">
          <AreaChart data={carbonData} options={options} />
        </div>
      );
    }
    case "pie": {
      const options: PieChartOptions = {
        ...baseOptions,
        legend: {
          ...baseOptions.legend
        }
      };
      return (
        <div className="carbon-chart-container">
          <PieChart data={carbonData} options={options} />
        </div>
      );
    }
    case "radar": {
      const options: RadarChartOptions = {
        ...baseOptions,
        radar: {
          axes: {
            angle: "group",
            value: "value"
          }
        }
      };
      return (
        <div className="carbon-chart-container">
          <RadarChart data={carbonData} options={options} />
        </div>
      );
    }
    case "radial":
    case "donut": {
      const options: DonutChartOptions = {
        ...baseOptions,
        legend: {
          ...baseOptions.legend
        }
      };
      return (
        <div className="carbon-chart-container">
          <DonutChart data={carbonData} options={options} />
        </div>
      );
    }
    case "bar":
    default: {
      const options: BarChartOptions = {
        ...baseOptions,
        axes: axesConfig,
        bars: {
          maxWidth: 50
        }
      };
      return (
        <div className="carbon-chart-container">
          <SimpleBarChart data={carbonData} options={options} />
        </div>
      );
    }
  }
});

function MetricItemComponent({ item }: Props) {
  const { runQuery } = useAppStore();
  const [queryResult, setQueryResult] = useState<QueryResult | null>(null);
  const [loading, setLoading] = useState<boolean>(true);
  const [errorMessage, setErrorMessage] = useState<string>("");

  const fetchData = useCallback(async () => {
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
      <CarbonChart
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
      <AgTable
        data={queryResult}
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
                  onClick={() => fetchData()}
                >
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

// Export the component wrapped with MetricsWrapper
export default function WrappedMetricItemComponent(props: Props) {
  return (
    <MetricsWrapper>
      <MetricItemComponent {...props} />
    </MetricsWrapper>
  );
}
