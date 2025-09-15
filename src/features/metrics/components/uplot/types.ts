import type uPlot from 'uplot';

export interface UPlotChartProps {
  data: uPlot.AlignedData;
  options: uPlot.Options;
  onCreate?: (chart: uPlot) => void;
  onDelete?: (chart: uPlot) => void;
  className?: string;
  height?: number | string;
  width?: number | string;
}

export interface MetricChartProps {
  data: Record<string, any>[];
  chartType: 'line' | 'area' | 'bar';
  chartConfig: {
    indexBy: string;
    [key: string]: any;
  };
  height?: number | string;
}

export interface TooltipOptions {
  formatX?: (u: uPlot, idx: number) => string;
  formatTime?: (value: number) => string;
  formatValue?: (
    value: number | null,
    decimals?: number,
    unit?: string
  ) => string;
  formatSeriesValue?: (seriesLabel: string, value: number | null) => string;
  decimals?: number;
  unit?: string;
  background?: string;
  textColor?: string;
  borderColor?: string;
}
