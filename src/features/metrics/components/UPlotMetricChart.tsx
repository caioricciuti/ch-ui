import React, { useMemo } from 'react';
import UPlotChart from './UPlotChart';
import type { MetricChartProps } from './uplot/types';

const UPlotMetricChart: React.FC<MetricChartProps> = ({
  data,
  chartType,
  chartConfig,
  height = 300,
}) => {
  // Process chart configuration
  const { series, labels, colors, isDateTime, units, decimals } = useMemo(() => {
    const indexBy = chartConfig.indexBy;

    // Extract series keys (all keys except indexBy that have config objects)
    const series = Object.keys(chartConfig)
      .filter(key => key !== 'indexBy')
      .filter(key => typeof chartConfig[key] === 'object');

    // If no explicit series config, use all non-indexBy numeric columns
    const finalSeries = series.length > 0
      ? series
      : data.length > 0
        ? Object.keys(data[0]).filter(key =>
            key !== indexBy &&
            typeof data[0][key] === 'number'
          )
        : [];

    // Extract labels and colors from config
    const labels: Record<string, string> = {};
    const colors: Record<string, string> = {};
    const units: Record<string, string> = {};
    const decimals: Record<string, number> = {};

    finalSeries.forEach(key => {
      const config = chartConfig[key];
      if (config && typeof config === 'object') {
        if (config.label) labels[key] = String(config.label);
        if (config.color) colors[key] = config.color;
        if ((config as any).unit) units[key] = String((config as any).unit);
        if ((config as any).decimals !== undefined) decimals[key] = Number((config as any).decimals);
      }
    });

    // Check if indexBy field contains datetime values
    const isDateTime = chartConfig.isDateTime !== undefined
      ? Boolean(chartConfig.isDateTime)
      : (data.length > 0 && data[0][indexBy]
          ? /^\d{4}-\d{2}-\d{2}/.test(String(data[0][indexBy]))
          : false);

    return { series: finalSeries, labels, colors, isDateTime, units, decimals };
  }, [data, chartConfig]);

  return (
    <UPlotChart
      data={data}
      chartType={chartType}
      indexBy={chartConfig.indexBy}
      series={series}
      labels={labels}
      colors={colors}
      seriesUnits={Object.fromEntries(Object.entries(units).map(([k,v]) => [labels[k] || k, v]))}
      seriesDecimals={Object.fromEntries(Object.entries(decimals).map(([k,v]) => [labels[k] || k, v]))}
      isDateTime={isDateTime}
      height={height as any}
      showLegend={chartType !== 'bar'}
      showTooltip={true}
    />
  );
};

export default UPlotMetricChart;
