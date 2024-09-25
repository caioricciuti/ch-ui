// src/components/ui/charts/ChartContainer.tsx
import React from "react";
import { ResponsiveContainer } from "recharts";

interface ChartContainerProps {
  children: React.ReactNode;
  className?: string;
}

const ChartContainer: React.FC<ChartContainerProps> = ({
  children,
  className,
}) => {
  return (
    <div
      className={`p-4 bg-white dark:bg-gray-800 rounded shadow ${className}`}
    >
      <ResponsiveContainer width="100%" height="100%">
        {React.isValidElement(children) ? children : <div />}
      </ResponsiveContainer>
    </div>
  );
};

export default ChartContainer;
