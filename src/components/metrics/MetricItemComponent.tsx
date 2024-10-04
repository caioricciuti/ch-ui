import { useState, useEffect, ComponentType } from "react";
import { MetricItem } from "@/helpers/metricsConfig";
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
} from "recharts";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
  ChartLegend,
  ChartLegendContent,
} from "@/components/ui/chart";
import CHUITable from "@/components/table/CHUItable";
import { Skeleton } from "../ui/skeleton";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import {
  AlertCircleIcon,
  CopyIcon,
  InfoIcon,
  EllipsisVertical,
  DownloadCloud,
  RefreshCcw,
  Braces,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { toast } from "sonner";

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import useAppStore from "@/store/appStore";

interface Props {
  item: MetricItem;
}

function MetricItemComponent({ item }: Props) {
  const { runQuery } = useAppStore();
  const [data, setData] = useState<any>(null);
  const [loading, setLoading] = useState<boolean>(true);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);

  const fetchData = async () => {
    try {
      setLoading(true);
      const result = await runQuery(item.query);
      setData(result);
      setErrorMessage(null);
    } catch (err: any) {
      console.error(err);
      setErrorMessage(err.message);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchData();
  }, [item.query, runQuery]);

  if (loading) {
    return (
      <>
        <Skeleton className="h-4 w-full mt-2" />
        <Skeleton className="h-2 w-full mt-2" />
        <Skeleton className="h-6 w-full mt-2" />
        <Skeleton className="h-8 w-full mt-2" />
      </>
    );
  }

  if (errorMessage) {
    return (
      <Card className="p-4 bg-red-300 dark:bg-red-600/30 text-primary/50">
        <div className="flex items-center justify-between">
          <div className="flex items-center">
            <p>Error loading {item.title}</p>
            <Button
              className="ml-4 bg-transparent text-red-600 hover:text-red-500 hover:bg-inherit"
              onClick={() => {
                navigator.clipboard.writeText(errorMessage);
                toast.info("Error message copied to clipboard");
              }}
            >
              Copy Error
            </Button>
          </div>
          <div className="flex items-center space-x-2">
            <Button
              size="icon"
              onClick={fetchData}
              className=" ml-4 bg-transparent text-red-600 hover:text-red-500 hover:bg-inherit"
            >
              <RefreshCcw className="w-4 h-4" />
            </Button>
            <TooltipProvider>
              <Tooltip>
                <TooltipTrigger>
                  <AlertCircleIcon className="w-6 h-6 text-muted-foreground cursor-pointer" />
                </TooltipTrigger>
                <TooltipContent>
                  <div className="p-2 max-w-[400px]">
                    <p className="text-muted-foreground text-xs">
                      {errorMessage}
                    </p>
                    <Button
                      className="mt-2"
                      variant="ghost"
                      size="icon"
                      onClick={() => {
                        const query = item.query;
                        navigator.clipboard.writeText(query);
                        toast.success("Query copied to clipboard");
                      }}
                    >
                      <CopyIcon className="w-4 h-4" />
                    </Button>
                  </div>
                </TooltipContent>
              </Tooltip>
            </TooltipProvider>
          </div>
        </div>
      </Card>
    );
  }

  const renderCardContent = () => {
    if (!data) {
      return <div className="text-muted-foreground font-bold">No data</div>;
    }
    return data.data.map((item: any, index: number) => (
      <div key={index} className="mb-2">
        {Object.entries(item).map(([key, value]) => (
          <div key={key}>
            <span className="font-extrabold text-3xl">{value?.toString()}</span>
          </div>
        ))}
      </div>
    ));
  };

  const handleDownloadData = (data: any) => {
    const blob = new Blob([JSON.stringify(data, null, 2)], {
      type: "application/json",
    });
    const fileName = `${item.title.replace(" ", "_").toLocaleLowerCase()}.json`;
    downloadBlob(blob, fileName);
  };

  const downloadBlob = (blob: Blob, fileName: string) => {
    const link = document.createElement("a");
    if (link.download !== undefined) {
      const url = URL.createObjectURL(blob);
      link.setAttribute("href", url);
      link.setAttribute("download", fileName);
      link.style.visibility = "hidden";
      document.body.appendChild(link);
      link.click();
      document.body.removeChild(link);
    }
  };

  const renderChart = () => {
    if (!data || !data.data || data.data.length === 0 || !item.chartConfig) {
      return <div className="text-muted-foreground font-bold">No data</div>;
    }

    let ChartComponent: ComponentType<any>;
    let DataComponent: ComponentType<any>;

    switch (item.chartType) {
      case "line":
        ChartComponent = LineChart;
        DataComponent = Line;
        break;
      case "area":
        ChartComponent = AreaChart;
        DataComponent = Area as unknown as ComponentType<any>;
        break;
      case "pie":
        ChartComponent = PieChart;
        DataComponent = Pie as unknown as ComponentType<any>;
        break;
      case "radar":
        ChartComponent = RadarChart;
        DataComponent = Radar as unknown as ComponentType<any>;
        break;
      case "radial":
        ChartComponent = RadialBarChart;
        DataComponent = RadialBar as unknown as ComponentType<any>;
        break;
      case "bar":
      default:
        ChartComponent = BarChart;
        DataComponent = Bar as unknown as ComponentType<any>;
    }

    const dataKeys = Object.keys(item.chartConfig).filter(
      (key) => key !== "indexBy"
    );
    const dataKey = dataKeys[0];
    const maxValue = Math.max(
      ...data.data.map((d: any) => d[dataKey as string])
    );
    const yAxisMax = Math.ceil(maxValue * 1.1); // Add 10% padding to the top

    return (
      <ChartContainer
        config={item.chartConfig}
        className="mt-4 h-[250px] w-full"
      >
        <ChartComponent
          data={data.data}
          margin={{ top: 5, right: 30, left: 40, bottom: 5 }}
        >
          {["bar", "line", "area"].includes(item.chartType as string) && (
            <>
              <CartesianGrid strokeDasharray="3 3" vertical={false} />
              <XAxis
                dataKey={item.chartConfig.indexBy as string}
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
          {item.chartType === "radar" && (
            <>
              <PolarGrid />
              <PolarAngleAxis dataKey={item.chartConfig.indexBy as string} />
              <PolarRadiusAxis />
            </>
          )}
          <ChartTooltip content={<ChartTooltipContent indicator="dot" />} />
          <ChartLegend content={<ChartLegendContent />} />
          {dataKeys.map((key) => (
            <DataComponent
              key={key}
              dataKey={key}
              fill={`var(--color-${key})`}
              stroke={`var(--color-${key})`}
              strokeWidth={2}
              dot={false}
              radius={item.chartType === "bar" ? [4, 4, 0, 0] : undefined}
              fillOpacity={item.chartType === "area" ? 0.3 : 1}
              {...(item.chartType === "pie" || item.chartType === "radial"
                ? {
                    data: data.data,
                    nameKey: item.chartConfig?.indexBy as string,
                    label: true,
                  }
                : {})}
            >
              {item.chartType === "pie" &&
                data.data.map((_: any, index: number) => (
                  <PieChart>
                    <Pie
                      key={index}
                      data={data.data}
                      dataKey={key}
                      nameKey={item.chartConfig?.indexBy as string}
                      cx="50%"
                      cy="50%"
                      outerRadius={item.chartType === "radial" ? 80 : 50}
                      fill={`var(--color-${key})`}
                      label
                    />
                  </PieChart>
                ))}
            </DataComponent>
          ))}
        </ChartComponent>
      </ChartContainer>
    );
  };

  const renderTable = () => {
    if (!data || !data.data || data.data.length === 0) {
      return <div className="text-muted-foreground font-bold">No data</div>;
    }
    return (
      <CHUITable
        result={{
          meta: data.meta,
          data: data.data,
          statistics: {
            elapsed: data.statistics.time_ellapsed,
            rows_read: data.statistics.rows_read,
            bytes_read: JSON.stringify(data).length,
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
                    const query = item.query;
                    navigator.clipboard.writeText(query);
                    toast.success("Query copied to clipboard");
                  }}
                >
                  <CopyIcon className="w-4 h-4 mr-2" />
                  Copy Query
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => handleDownloadData(data)}>
                  <DownloadCloud className="w-4 h-4 mr-2" />
                  Download Data
                </DropdownMenuItem>
                {/* copyData to Clipboard */}

                <DropdownMenuItem
                  onClick={() => {
                    const jsonString = JSON.stringify(data.data, null, 2);
                    navigator.clipboard
                      .writeText(jsonString)
                      .then(() => {
                        toast.success("Data copied to clipboard");
                      })
                      .catch((err) => {
                        console.error("Failed to copy: ", err);
                        toast.error("Failed to copy data to clipboard");
                      });
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
