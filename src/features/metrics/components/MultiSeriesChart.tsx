import React, { useMemo } from "react";
import { LineChart, StackedBarChart, AreaChart } from "@carbon/charts-react";
import { ScaleTypes } from "@carbon/charts";
import { formatDateTime } from "../utils/formatDateTime";
import { useTheme } from "./CarbonChartsTheme";

export interface MultiSeriesChartProps {
  data: Record<string, any>[];
  chartType: "line" | "bar" | "area";
  indexBy: string;
  series: string[];
  labels?: Record<string, string>;
  colors?: Record<string, string>;
  theme?: string;
  isDateTime?: boolean;
}

const MultiSeriesChart: React.FC<MultiSeriesChartProps> = ({
  data,
  chartType,
  indexBy,
  series,
  labels = {},
  colors = {},
  theme: propTheme,
  isDateTime = false,
}) => {
  // Get theme from context, but allow prop override
  const { theme: contextTheme } = useTheme();
  const theme = propTheme || contextTheme;
  
  const formattedData = useMemo(() => {
    if (!data || !series || !indexBy) return [];

    // Format data for multi-series charts
    return data.map((item) => {
      const result: Record<string, any> = {
        group: item[indexBy]?.toString() || "",
      };

      // Add each series value
      series.forEach((seriesKey) => {
        if (typeof item[seriesKey] !== "undefined") {
          result[labels[seriesKey] || seriesKey] = item[seriesKey];
        } else {
          // If value is undefined, set it to 0 or null based on chart type
          result[labels[seriesKey] || seriesKey] =
            chartType === "line" || chartType === "area" ? null : 0;
        }
      });

      return result;
    });
  }, [data, indexBy, series, labels, chartType]);

  // Configure the chart options
  const options = useMemo(() => {
    // Create color scale from the colors object
    const colorScale: Record<string, string> = {};
    series.forEach((key) => {
      const label = labels[key] || key;
      colorScale[label] =
        colors[key] ||
        `var(--chart-${(Object.keys(colorScale).length % 10) + 1})`;
    });

    // Determine axis type based on datetime
    const axesConfig = isDateTime
      ? {
          left: {
            mapsTo: "value",
            scaleType: ScaleTypes.LINEAR,
            includeZero: chartType === "bar",
            title: "",
          },
          bottom: {
            mapsTo: "group",
            scaleType: ScaleTypes.LABELS,
            title: "",
            ticks: {
              max: 6,
            },
            formatter: (value: string) => formatDateTime(value),
          },
        }
      : {
          left: {
            mapsTo: "value",
            scaleType: ScaleTypes.LINEAR,
            includeZero: chartType === "bar",
            title: "",
          },
          bottom: {
            mapsTo: "group",
            scaleType: ScaleTypes.LABELS,
            title: "",
          },
        };

    return {
      title: "",
      height: "100%",
      width: "100%",
      axes: axesConfig,
      legend: {
        alignment: "center",
        enabled: true,
      },
      toolbar: {
        enabled: false,
      },
      color: {
        scale: colorScale,
      },
      theme,
      resizable: true,
      grid: {
        x: {
          enabled: true,
        },
        y: {
          enabled: true,
        },
      },
      ...(chartType === "line"
        ? {
            curve: "curveMonotoneX",
            points: {
              enabled: formattedData.length < 30,
              radius: 3,
            },
          }
        : {}),
      ...(chartType === "area"
        ? {
            curve: "curveMonotoneX",
            points: {
              enabled: formattedData.length < 30,
              radius: 3,
            },
            filled: true,
          }
        : {}),
      ...(chartType === "bar"
        ? {
            bars: {
              maxWidth: 50,
            },
          }
        : {}),
    };
  }, [
    series,
    labels,
    colors,
    theme,
    chartType,
    formattedData.length,
    isDateTime,
  ]);

  // Render the appropriate chart based on chartType
  switch (chartType) {
    case "line":
      return <LineChart data={formattedData} options={options} />;
    case "area":
      return <AreaChart data={formattedData} options={options} />;
    case "bar":
    default:
      return <StackedBarChart data={formattedData} options={options} />;
  }
};

export default MultiSeriesChart;
