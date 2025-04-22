import React from 'react';
import { CarbonChartsThemeProvider } from './CarbonChartsTheme';
import './carbonCharts.css';

interface MetricsWrapperProps {
  children: React.ReactNode;
}

/**
 * Wrapper component that provides theming for Carbon Charts
 * and includes required CSS styling
 */
const MetricsWrapper: React.FC<MetricsWrapperProps> = ({ children }) => {
  return (
    <CarbonChartsThemeProvider>
      {children}
    </CarbonChartsThemeProvider>
  );
};

export default MetricsWrapper; 