// src/components/ui/charts/CustomLegend.tsx
import React from "react";
import { ChartLegendContent } from "@/components/ui/chart";

interface CustomLegendProps {
  payload?: any[];
}

const CustomLegend: React.FC<CustomLegendProps> = ({ payload }) => {
  return <ChartLegendContent payload={payload} />;
};

export default CustomLegend;
