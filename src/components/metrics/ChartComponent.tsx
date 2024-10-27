import { Area, AreaChart, Bar, BarChart, CartesianGrid, Line, LineChart, Pie, PieChart, PolarAngleAxis, PolarGrid, PolarRadiusAxis, Radar, RadarChart, RadialBar, RadialBarChart, ResponsiveContainer, XAxis, YAxis } from "recharts";
import { ChartTooltip, ChartTooltipContent, ChartLegend, ChartLegendContent, ChartConfig, ChartContainer } from "../ui/chart";
import { CategoricalChartProps } from "recharts/types/chart/generateCategoricalChart";

export type ChartComponentProps = {
  chartType: 'pie' | 'bar' | 'line' | 'area' | 'radar' | 'radial' | undefined;
  data: any;
  config: { indexBy: string, data: ChartConfig};
} & CategoricalChartProps;

const ChartComponent = ({ chartType, data, margin, config }: ChartComponentProps) => {
  const dataKeys = Object.keys(config).filter((key) => key !== "indexBy");

  const dataKey = dataKeys[0];
  const maxValue = Math.max(...data.map((d: any) => d[dataKey as string]));
  const yAxisMax = Math.ceil(maxValue * 1.1); // Add 10% padding to the top

  const renderChart = () => {
    switch (chartType) {
      case "line":
        return (
          <LineChart data={data} margin={margin}>
            <CartesianGrid strokeDasharray="3 3" vertical={false} />
            <XAxis
              dataKey={config.indexBy}
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
            <ChartTooltip content={<ChartTooltipContent indicator="dot" />} />
            <ChartLegend content={<ChartLegendContent />} />
            {dataKeys.map((key) => (
              <Line key={key} dataKey={key} fill={`var(--color-${key})`} stroke={`var(--color-${key})`} strokeWidth={2} dot={false} />
            ))}
          </LineChart>
        );
      case "area":
        return (
          <AreaChart data={data} margin={margin}>
            <CartesianGrid strokeDasharray="3 3" vertical={false} />
            <XAxis
              dataKey={config.indexBy}
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
            <ChartTooltip content={<ChartTooltipContent indicator="dot" />} />
            <ChartLegend content={<ChartLegendContent />} />
            {dataKeys.map((key) => (
              <Area key={key} dataKey={key} fill={`var(--color-${key})`} stroke={`var(--color-${key})`} strokeWidth={2} dot={false} />
            ))}
          </AreaChart>
        );
      case "pie":
        return (
          <PieChart data={data} margin={margin}>
            <ChartTooltip content={<ChartTooltipContent indicator="dot" />} />
            <ChartLegend content={<ChartLegendContent />} />
            {dataKeys.map((key) => (
              <Pie key={key} dataKey={key} fill={`var(--color-${key})`} stroke={`var(--color-${key})`} strokeWidth={2} data={data} nameKey={config.indexBy as string} label={true} />
            ))}
          </PieChart>
        );
      case "radar":
        return (
          <RadarChart data={data} margin={margin}>
            <PolarGrid />
            <PolarAngleAxis dataKey={config.indexBy as string} />
            <PolarRadiusAxis />
            <ChartTooltip content={<ChartTooltipContent indicator="dot" />} />
            <ChartLegend content={<ChartLegendContent />} />
            {dataKeys.map((key) => (
              <Radar key={key} dataKey={key} fill={`var(--color-${key})`} stroke={`var(--color-${key})`} strokeWidth={2} dot={false} />
            ))}
          </RadarChart>
        );
      case "radial":
        return (
          <RadialBarChart data={data} margin={margin}>
            <ChartTooltip content={<ChartTooltipContent indicator="dot" />} />
            <ChartLegend content={<ChartLegendContent />} />
            {dataKeys.map((key) => (
              <RadialBar key={key} dataKey={key} fill={`var(--color-${key})`} stroke={`var(--color-${key})`} strokeWidth={2} data={data} label={true} />
            ))}
          </RadialBarChart>
        );
      case "bar":
      default:
        return (
          <BarChart data={data} margin={margin}>
            <CartesianGrid strokeDasharray="3 3" vertical={false} />
            <XAxis
              dataKey={config.indexBy}
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
            <ChartTooltip content={<ChartTooltipContent indicator="dot" />} />
            <ChartLegend content={<ChartLegendContent />} />
            {dataKeys.map((key) => (
              <Bar key={key} dataKey={key} fill={`var(--color-${key})`} stroke={`var(--color-${key})`} strokeWidth={2} fillOpacity={1} />
            ))}
          </BarChart>
        );
    }
  };

  return (
    <ChartContainer config={config.data} className="mt-4 h-[250px] w-full">
      <ResponsiveContainer width="100%" height="100%">
        {renderChart()}
      </ResponsiveContainer>
    </ChartContainer>
  );
};

export default ChartComponent;
