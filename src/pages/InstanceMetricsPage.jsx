import { useEffect } from "react";
import { useClickHouseState } from "@/providers/ClickHouseContext";
import ReactECharts from "echarts-for-react";
import { AgGridReact } from "ag-grid-react";
import "ag-grid-community/styles/ag-grid.css";
import "ag-grid-community/styles/ag-theme-alpine.css";
import { useTheme } from "@/providers/theme";
import { Loader2, InfoIcon, Copy as CopyIcon } from "lucide-react";
import { lightTheme, darkTheme } from "@/helpers/echartsThemes";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { Button } from "@/components/ui/button";
import { toast } from "sonner";

export default function InstanceMetricsPage() {
  const { theme } = useTheme();
  const {
    isServerAvailable,
    isLoading,
    setIsLoading,
    getDataAnalytics,
    analyticsData,
  } = useClickHouseState();

  useEffect(() => {
    setIsLoading(true);
    if (isServerAvailable && !analyticsData.length) {
      getDataAnalytics();
    }
    setIsLoading(false);
  }, [isServerAvailable, analyticsData.length]);

  const formatNumber = (number) => new Intl.NumberFormat().format(number);
  const formatTime = (seconds) =>
    `${Math.floor(seconds / (60 * 60 * 24))} days`;
  const formatBytes = (bytes) => `${(bytes / (1024 * 1024)).toFixed(6)} MB`;

  const renderContent = (item) => {
    if (item.plot === "Card") {
      return (
        <div className="flex flex-col">
          {Object.keys(item.data[0]).map((key, index) => {
            let formattedValue;

            if (item.data_format === "number") {
              formattedValue = formatNumber(item.data[0][key]);
            } else if (item.data_format === "time") {
              formattedValue = formatTime(item.data[0][key]);
            } else if (item.data_format === "bytes") {
              formattedValue = formatBytes(item.data[0][key]);
            } else {
              formattedValue = item.data[0][key];
            }
            return (
              <div key={index} className="flex justify-between items-center">
                <div className="flex flex-col">
                  <div className="text-lg font-bold">{formattedValue}</div>
                </div>
              </div>
            );
          })}
        </div>
      );
    }

    if (item.plot === "Table") {
      return (
        <div
          className={`${
            theme === "dark" ? "ag-theme-alpine-dark" : "ag-theme-alpine"
          }  w-full flex-grow`}
          style={{ height: 300, width: "100%" }}
        >
          <AgGridReact
            paginationPageSize={20}
            enableCellTextSelection={true}
            rowData={item.data}
            columnDefs={Object.keys(item.data[0] || {}).map((key) => ({
              headerName: key,
              field: key,
            }))}
            pagination={true}
          />
        </div>
      );
    }

    let options = {};
    switch (item.plot) {
      case "Bar":
        options = {
          xAxis: {
            type: "category",
            data: item.data.map(
              (d) =>
                d.timestamp ||
                d.database ||
                d.table ||
                d.engine ||
                d["Database name"] ||
                d["Table name"] ||
                d.name
            ),
            axisLabel: {
              rotate: 35,
              interval: 0,
            },
          },
          tooltip: {
            trigger: "axis",
            axisPointer: {
              type: "shadow",
            },
          },
          grid: {
            left: "20%",
            right: "10%",
            bottom: "30%",
          },
          yAxis: {
            type: "value",
          },
          series: [
            {
              data: item.data.map(
                (d) =>
                  d.total_rows ||
                  d.total_columns ||
                  d["Number of databases"] ||
                  d["Number of tables"] ||
                  d.total_disk_usage ||
                  d.part_count ||
                  d.table_count ||
                  d.database_count ||
                  d.rows
              ),
              type: "bar",
              barCategoryGap: "20%",
            },
          ],
        };
        break;
      case "Line":
        options = {
          grid: {
            left: "5%",
            right: "5%",
            bottom: "5%",
            containLabel: true,
          },
          xAxis: {
            type: "category",
            data: item.data.map((d) => d.timestamp),
          },
          yAxis: {
            type: "value",
          },
          series: [
            {
              data: item.data.map((d) => d.rows),
              type: "line",
              smooth: true,
            },
          ],
          tooltip: {
            trigger: "axis",
            axisPointer: {
              type: "shadow",
            },
          },
        };
        break;
      default:
        return "Invalid plot type";
    }

    const currentTheme = theme === "dark" ? darkTheme : lightTheme;

    return (
      <ReactECharts
        option={options}
        theme={currentTheme}
        style={{ height: 400, width: "100%" }}
      />
    );
  };

  const getGridClassNames = (plotType) => {
    if (plotType === "Card") {
      return "grid-cols-1 md:grid-cols-3 lg:grid-cols-3";
    } else {
      return "grid-cols-1 md:grid-cols-2 lg:grid-cols-2";
    }
  };

  if (isLoading) {
    return (
      <div className="flex justify-center items-center h-full">
        <div className="flex flex-col items-center">
          <Loader2 size={100} className="animate-spin" />
          <span className="ml-2">Loading metrics...</span>
        </div>
      </div>
    );
  }

  // Sort analyticsData based on the plot type to group similar plots together: Cards first, then Tables, then Charts
  const sortedAnalyticsData = analyticsData.sort((a, b) => {
    const plotOrder = {
      Card: 1,
      Table: 2,
      Pie: 3,
      Bar: 4,
      Line: 5,
    };
    return plotOrder[a.plot] - plotOrder[b.plot];
  });

  return (
    <div className="p-4 h-screen flex flex-col">
      <div>
        <h1 className="text-3xl font-bold">Metrics</h1>
        <p>Check the status of the ClickHouse server and some key metrics.</p>
      </div>
      <div className="mt-8 grid gap-4 grid-cols-4">
        {sortedAnalyticsData.map((item, index) => (
          <div
            key={index}
            className={`border rounded-md p-2 ${
              item.plot === "Card"
                ? "col-span-4 md:col-span-2 lg:col-span-1"
                : "col-span-4 md:col-span-4 lg:col-span-2"
            }`}
          >
            <div className="flex justify-between items-center mb-4">
              <h2 className="text-lg font-semibold">{item.title}</h2>
              <TooltipProvider>
                <Tooltip>
                  <TooltipTrigger>
                    <InfoIcon className="text-gray-400" size={22} />
                  </TooltipTrigger>
                  <TooltipContent className="items-center p-2">
                    {item.description}
                    <Button
                      className="ml-4 p-0"
                      size="xs"
                      variant="ghost"
                      onClick={() => {
                        navigator.clipboard.writeText(item.query);
                        toast.success(
                          `${item.title} - Query copied to clipboard!`
                        );
                      }}
                    >
                      <CopyIcon size={16} />
                    </Button>
                  </TooltipContent>
                </Tooltip>
              </TooltipProvider>
            </div>
            {renderContent(item)}
          </div>
        ))}
      </div>
    </div>
  );
}
