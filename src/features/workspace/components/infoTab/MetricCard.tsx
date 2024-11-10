import React from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { LucideIcon, HelpCircle } from "lucide-react";

interface MetricCardProps {
  icon: LucideIcon;
  title: string;
  value: string | number;
  trend?: number;
  description?: string;
  previousValue?: string | number;
  isLoading?: boolean;
  error?: string;
  className?: string;
  formatter?: (value: string | number) => string;
}

const MetricCard: React.FC<MetricCardProps> = ({
  icon: Icon,
  title,
  value,
  trend,
  description,
  previousValue,
  isLoading = false,
  error,
  className = "",
  formatter = (val) => val.toString(),
}) => {
  const formattedValue = React.useMemo(() => {
    try {
      return formatter(value);
    } catch (err) {
      console.error("Error formatting value:", err);
      return "Error";
    }
  }, [value, formatter]);

  const formattedPreviousValue = React.useMemo(() => {
    if (previousValue === undefined) return null;
    try {
      return formatter(previousValue);
    } catch (err) {
      console.error("Error formatting previous value:", err);
      return "Error";
    }
  }, [previousValue, formatter]);

  const renderTrendIndicator = () => {
    if (trend === undefined || trend === 0) return null;

    const isPositive = trend > 0;
    const trendColor = isPositive ? "text-green-500" : "text-red-500";
    const trendIcon = isPositive ? "↑" : "↓";

    return (
      <div className={`flex items-center space-x-1 ${trendColor}`}>
        <span className="text-xs">{trendIcon}</span>
        <span className="text-xs font-medium">
          {Math.abs(trend).toFixed(1)}%
        </span>
      </div>
    );
  };

  const renderValue = () => {
    if (error) {
      return <div className="text-red-500 text-sm">Error: {error}</div>;
    }

    if (isLoading) {
      return (
        <div className="animate-pulse">
          <div className="h-8 bg-muted rounded w-24"></div>
        </div>
      );
    }

    return (
      <div className="space-y-1">
        <div className="text-2xl font-bold tracking-tight">
          {formattedValue}
        </div>
        {formattedPreviousValue && (
          <div className="text-sm text-muted-foreground">
            Previous: {formattedPreviousValue}
          </div>
        )}
      </div>
    );
  };

  return (
    <TooltipProvider>
      <Tooltip delayDuration={300}>
        <TooltipTrigger asChild>
          <Card
            className={`relative overflow-hidden group hover:shadow-lg transition-all duration-300 ${className}`}
          >
            <CardHeader className="space-y-0 pb-2">
              <div className="flex items-center justify-between">
                <div className="flex items-center space-x-2">
                  <Icon className="w-4 h-4 text-muted-foreground" />
                  <CardTitle className="text-sm font-medium">
                    {title}
                    {description && (
                      <HelpCircle className="w-3 h-3 inline ml-1 text-muted-foreground" />
                    )}
                  </CardTitle>
                </div>
                {renderTrendIndicator()}
              </div>
            </CardHeader>
            <CardContent>
              {renderValue()}
              <div
                className="absolute bottom-0 left-0 w-full h-1 bg-primary/10 group-hover:bg-primary/20 
                transition-colors duration-300"
              />
            </CardContent>
          </Card>
        </TooltipTrigger>
        {description && (
          <TooltipContent side="top" className="max-w-xs">
            <p className="text-sm">{description}</p>
          </TooltipContent>
        )}
      </Tooltip>
    </TooltipProvider>
  );
};

export default MetricCard;
