import React, { useMemo } from "react";
import uPlot, { AlignedData } from "uplot";
import { useTheme } from "@/components/common/theme-provider";
import { UPlotWrapper } from "./uplot/UPlotWrapper";
import {
  PALETTE,
  formatValue as fmtValue,
  formatBytes as fmtBytes,
  formatShortNumber,
} from "./uplot/utils";
import { createTooltipPlugin } from "./uplot/tooltip";

interface UPlotChartProps {
  data: Record<string, any>[];
  chartType: "line" | "area" | "bar";
  indexBy: string;
  series: string[];
  labels?: Record<string, string>;
  colors?: Record<string, string>;
  seriesUnits?: Record<string, string>; // keyed by label
  seriesDecimals?: Record<string, number>; // keyed by label
  isDateTime?: boolean;
  height?: number | string;
  showLegend?: boolean;
  showTooltip?: boolean;
}

const UPlotChart: React.FC<UPlotChartProps> = ({
  data,
  chartType = "line",
  indexBy,
  series,
  labels = {},
  colors = {},
  isDateTime = false,
  height = 250,
  showLegend = false,
  showTooltip = true,
  seriesUnits = {},
  seriesDecimals = {},
}) => {
  const { theme } = useTheme();
  const isDark = theme === "dark";
  const chartTheme = {
    axis: {
      stroke: isDark ? "rgba(255,255,255,0.35)" : "rgba(0,0,0,0.45)",
      font: "11px Inter, system-ui, -apple-system, sans-serif",
      labelColor: isDark ? "rgba(255,255,255,0.8)" : "rgba(0,0,0,0.7)",
    },
    grid: {
      stroke: isDark ? "rgba(255,255,255,0.12)" : "rgba(0,0,0,0.12)",
      width: 1,
    },
    series: {
      points: { show: false },
    },
    tooltip: {
      background: isDark ? "rgba(30,30,30,0.95)" : "rgba(255,255,255,0.98)",
      textColor: isDark ? "rgba(255,255,255,0.9)" : "rgba(0,0,0,0.9)",
      borderColor: isDark ? "rgba(255,255,255,0.2)" : "rgba(0,0,0,0.3)",
    },
  };

  function colorWithAlpha(color: string, alpha: number): string {
    const c = color;
    if (c.startsWith("#")) {
      const hex = c.replace("#", "");
      const bigint = parseInt(
        hex.length === 3
          ? hex
              .split("")
              .map((ch) => ch + ch)
              .join("")
          : hex,
        16
      );
      const r = (bigint >> 16) & 255;
      const g = (bigint >> 8) & 255;
      const b = bigint & 255;
      return `rgba(${r}, ${g}, ${b}, ${alpha})`;
    }
    if (c.startsWith("rgb(")) {
      const inside = c.slice(4, -1);
      return `rgba(${inside}, ${alpha})`;
    }
    if (c.startsWith("hsl(")) {
      // add alpha channel using CSS Color 4 syntax
      return c.replace(/\)$/, ` / ${alpha})`);
    }
    return c;
  }

  // Detect categorical x-axis (non-date, non-number)
  const { isCategorical, categories } = useMemo(() => {
    if (!data || data.length === 0)
      return { isCategorical: false, categories: [] as string[] };
    const sample = data[0]?.[indexBy];
    const isNum = typeof sample === "number";
    const isDate = isDateTime;
    const isCat = !isDate && !isNum;
    const cats = isCat ? data.map((row) => String(row[indexBy])) : [];
    return { isCategorical: isCat, categories: cats };
  }, [data, indexBy, isDateTime]);

  // Transform data to uPlot format
  const plotData: AlignedData = useMemo(() => {
    if (!data || data.length === 0) {
      return [[]];
    }

    const toEpochSeconds = (v: any): number => {
      if (v == null) return NaN;
      if (typeof v === "number") {
        // if it's ms
        if (v > 1e12) return Math.floor(v / 1000);
        // if it's seconds
        if (v > 1e6) return Math.floor(v);
        return v; // already small seconds or ordinal
      }
      if (v instanceof Date) return Math.floor(v.getTime() / 1000);
      const s = String(v);
      // normalize common ClickHouse format "YYYY-MM-DD HH:MM:SS"
      const iso = s.includes("T") ? s : s.replace(" ", "T");
      const t = Date.parse(iso);
      if (!Number.isNaN(t)) return Math.floor(t / 1000);
      return NaN;
    };

    // Sort data if it's time series
    const sortedData = isDateTime
      ? [...data].sort(
          (a, b) =>
            new Date(a[indexBy]).getTime() - new Date(b[indexBy]).getTime()
        )
      : data;

    // X-axis values
    const xValues = sortedData.map((row, i) => {
      if (isDateTime) {
        return toEpochSeconds(row[indexBy]);
      }
      if (isCategorical) {
        return i; // ordinal index
      }
      const n = Number(row[indexBy]);
      return Number.isFinite(n) ? n : i;
    });

    // Y-axis values for each series
    const yValues = series.map((seriesName) =>
      sortedData.map((row) =>
        row[seriesName] !== undefined && row[seriesName] !== null
          ? Number(row[seriesName])
          : null
      )
    );

    // Filter out invalid x values and ensure ascending order
    const points = xValues.map(
      (x, i) => ({ x, i } as { x: number; i: number })
    );
    const valid = points.filter((p) => Number.isFinite(p.x));
    valid.sort((a, b) => a.x - b.x);

    const alignedX: number[] = [];
    const alignedY: (number | null)[][] = series.map(() => [] as (number | null)[]);

    let lastX: number | null = null;
    for (const p of valid) {
      const xv = p.x;
      if (lastX !== null && xv === lastX) continue; // dedupe duplicate x buckets
      alignedX.push(xv);
      series.forEach((sName, sIdx) => {
        const v = sortedData[p.i][sName];
        alignedY[sIdx].push(v !== undefined && v !== null ? Number(v) : null);
      });
      lastX = xv;
    }

    return [alignedX, ...alignedY];
  }, [data, indexBy, series, isDateTime, isCategorical]);

  // Bar paths helper for grouped bars
  function makeBarPaths(
    seriesIdx: number,
    seriesCount: number
  ): uPlot.Series["paths"] {
    return (u, sidx, idx0, idx1) => {
      const x = u.data[0] as number[];
      const y = u.data[sidx] as (number | null)[];

      const px = (val: number) => u.valToPos(val, "x", true);
      const py = (val: number) => u.valToPos(val, "y", true);

      // estimate step in pixels
      let step = 10;
      if (x.length > 1) {
        const x0 = px(x[0]);
        const x1 = px(x[1]);
        step = Math.abs(x1 - x0);
      }
      const gap = 0.2; // 20% gap of step
      const fullBar = Math.max(1, Math.floor(step * (1 - gap)));
      const barW = Math.max(1, Math.floor(fullBar / seriesCount));

      const p = new Path2D();

      const baseOffset = -((seriesCount - 1) * barW) / 2;
      const offset = baseOffset + seriesIdx * barW;

      for (let i = idx0; i <= idx1; i++) {
        const yi = y[i];
        if (yi == null) continue;
        const xv = px(x[i]) + offset;
        const y0 = py(0);
        const yv = py(yi);
        const top = Math.min(y0, yv);
        const h = Math.abs(y0 - yv);
        p.rect(
          Math.floor(xv - barW / 2),
          Math.floor(top),
          Math.max(1, barW),
          Math.max(1, h)
        );
      }

      return { stroke: null, fill: p, clip: null } as uPlot.Series.Paths;
    };
  }

  // Chart options
  const options: uPlot.Options = useMemo(() => {
    // When height is a string (like "100%"), let UPlotWrapper handle sizing
    const numericHeight = typeof height === 'number' ? height : 300;
    const plugins: uPlot.Plugin[] = [];

    // Add tooltip plugin if enabled
    if (showTooltip) {
      plugins.push(
        createTooltipPlugin({
          decimals: 2,
          background: chartTheme.tooltip.background,
          textColor: chartTheme.tooltip.textColor,
          borderColor: chartTheme.tooltip.borderColor,
          formatX: (u, idx) => {
            if (isDateTime) {
              const ts = Number(u.data[0][idx]) * 1000;
              const d = new Date(ts);
              return `${d.toLocaleDateString()} ${d.toLocaleTimeString()}`;
            }
            if (isCategorical && categories[idx] != null) {
              return categories[idx];
            }
            return String(u.data[0][idx]);
          },
          formatSeriesValue: (label, value) => {
            const unit = seriesUnits[label];
            const dec = seriesDecimals[label] ?? 2;
            if (unit && unit.toLowerCase() === "bytes") {
              return fmtBytes(value as number, 1);
            }
            return fmtValue(value as number, dec, unit);
          },
        })
      );
    }

    // Create series configuration
    const seriesConfig: uPlot.Series[] = [
      {}, // X-axis placeholder
      ...series.map((seriesName, idx) => {
        const seriesColor = colors[seriesName] || PALETTE[idx % PALETTE.length];

        const baseSeries: uPlot.Series = {
          label: labels[seriesName] || seriesName,
          stroke: seriesColor,
          width: 2,
          scale: "y",
          show: true,
          spanGaps: false,
          points: {
            show: chartTheme.series.points.show,
          },
        };

        // Add area fill for area charts
        if (chartType === "area") {
          baseSeries.fill = colorWithAlpha(seriesColor, 0.18);
        }

        // Add bar paths for bar charts
        if (chartType === "bar") {
          baseSeries.paths = makeBarPaths(idx, series.length);
          baseSeries.stroke = seriesColor;
          baseSeries.fill = colorWithAlpha(seriesColor, 0.7);
          baseSeries.width = 0;
          baseSeries.points = { show: false };
        }

        return baseSeries;
      }),
    ];

    // detect y-axis unit if any series declares bytes
    const hasBytes = Object.values(seriesUnits || {}).some(
      (u) => (u || "").toLowerCase() === "bytes"
    );

    return {
      // uPlot requires both width and height in the options type.
      // Width is recalculated in the wrapper using the container size.
      width: 1,
      height: numericHeight,
      series: seriesConfig,
      plugins,
      scales: {
        x: {
          time: isDateTime,
          ...(isCategorical || chartType === "bar" ? { distr: 2 as any } : {}),
        },
        y: {
          auto: true,
        },
      },
      axes: [
        {
          stroke: chartTheme.axis.stroke,
          font: chartTheme.axis.font,
          size: 55,
          grid: {
            stroke: chartTheme.grid.stroke,
            width: chartTheme.grid.width,
          },
          ...(isDateTime
            ? {
                values: (u: uPlot, vals: number[]) => {
                  const desired = 6;
                  const skip = Math.max(1, Math.ceil(vals.length / desired));
                  const span =
                    vals.length > 1 ? vals[vals.length - 1] - vals[0] : 0;
                  const showDate = span > 24 * 60 * 60; // > 1 day
                  return vals.map((v, i) => {
                    if (i % skip !== 0) return "";
                    const d = new Date(v * 1000);
                    return showDate
                      ? `${d.toLocaleDateString()} ${d.toLocaleTimeString([], {
                          hour: "2-digit",
                          minute: "2-digit",
                        })}`
                      : d.toLocaleTimeString([], {
                          hour: "2-digit",
                          minute: "2-digit",
                        });
                  });
                },
              }
            : isCategorical
            ? {
                values: (_u: uPlot, vals: number[]) => {
                  const maxLabels = 6;
                  const step = Math.max(
                    1,
                    Math.ceil(categories.length / maxLabels)
                  );
                  const truncate = (s: string) =>
                    s.length > 14 ? s.slice(0, 12) + "…" : s;
                  return vals.map((v, idx) => {
                    const i = Math.round(v);
                    if (!Number.isFinite(i) || i < 0 || i >= categories.length)
                      return "";
                    return i % step === 0 ? truncate(categories[i]) : "";
                  });
                },
              }
            : {}),
        },
        {
          stroke: chartTheme.axis.stroke,
          font: chartTheme.axis.font,
          size: 55,
          grid: {
            stroke: chartTheme.grid.stroke,
            width: chartTheme.grid.width,
          },
          values: (_u: uPlot, vals: number[]) =>
            hasBytes
              ? vals.map((v) => fmtBytes(v as number, 1))
              : vals.map((v) => formatShortNumber(v as number)),
        },
      ],
      legend: showLegend
        ? {
            show: true,
            live: false,
          }
        : undefined,
    };
  }, [
    series,
    labels,
    colors,
    chartType,
    isDateTime,
    height,
    chartTheme,
    showLegend,
    showTooltip,
    isCategorical,
    categories,
    seriesUnits,
  ]);

  if (!data || data.length === 0 || plotData[0].length === 0) {
    return (
      <div
        style={{
          height,
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
        }}
      >
        <span style={{ color: chartTheme.axis.labelColor }}>
          No data available
        </span>
      </div>
    );
  }

  return (
    <UPlotWrapper
      data={plotData}
      options={options}
      height={height}
      width="100%"
    />
  );
};

export default UPlotChart;
