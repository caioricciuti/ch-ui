// MetricCard.tsx
import React from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { LucideIcon } from "lucide-react";

interface MetricCardProps {
  icon: LucideIcon;
  title: string;
  value: string | number;
  description?: string;
  trend?: number;
  className?: string;
}

const MetricCard: React.FC<MetricCardProps> = ({
  icon: Icon,
  title,
  value,
  description,
  trend,
  className = "",
}) => (
  <Card
    className={`relative group hover:shadow-md transition-all duration-300 ${className}`}
  >
    <CardHeader>
      <div className="flex items-center justify-between">
        <div className="flex items-center space-x-2">
          <Icon className="w-4 h-4 text-muted-foreground" />
          <CardTitle className="text-sm font-medium">{title}</CardTitle>
        </div>
        {trend !== undefined && (
          <span
            className={`text-xs ${
              trend > 0 ? "text-green-500" : "text-red-500"
            }`}
          >
            {trend > 0 ? "↑" : "↓"} {Math.abs(trend)}%
          </span>
        )}
      </div>
    </CardHeader>
    <CardContent>
      <p className="text-lg font-bold">{value}</p>
      {description && (
        <p className="text-sm text-muted-foreground mt-1">{description}</p>
      )}
    </CardContent>
  </Card>
);

export default MetricCard;
