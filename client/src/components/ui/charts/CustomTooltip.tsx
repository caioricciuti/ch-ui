// src/components/ui/charts/CustomTooltip.tsx
import React from "react";
import { ChartTooltipContent } from "@/components/ui/chart";

interface CustomTooltipProps {
  active?: boolean;
  payload?: any[];
  label?: string;
}

const CustomTooltip: React.FC<CustomTooltipProps> = ({
  active,
  payload,
  label,
}) => {
  if (active && payload && payload.length) {
    return <ChartTooltipContent label={label} payload={payload} />;
  }

  return null;
};

export default CustomTooltip;
