import React, { useRef, useEffect, useLayoutEffect, useState } from 'react';
import uPlot from 'uplot';
import 'uplot/dist/uPlot.min.css';
import type { UPlotChartProps } from './types';

/**
 * Core React wrapper for uPlot
 * Handles lifecycle, resizing, and theme changes
 */
function UPlotWrapperComponent({
  data,
  options,
  onCreate,
  onDelete,
  className = '',
  height = '100%',
  width = '100%',
}: UPlotChartProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const chartContainerRef = useRef<HTMLDivElement>(null);
  const legendContainerRef = useRef<HTMLDivElement>(null);
  const chartRef = useRef<uPlot | null>(null);
  const [isReady, setIsReady] = useState(false);

  // Initialize chart - recreate when options change
  useLayoutEffect(() => {
    if (!chartContainerRef.current || !data || data[0].length === 0) return;

    // Clean up existing chart
    if (chartRef.current) {
      chartRef.current.destroy();
      chartRef.current = null;
    }

    // Get container dimensions (use outer container for more reliable sizing)
    const rect = chartContainerRef.current.getBoundingClientRect();

    // Add legend mount callback if legend is enabled
    const legendMount = options.legend?.show ?
      (_self: uPlot, el: HTMLElement) => {
        if (legendContainerRef.current) {
          legendContainerRef.current.appendChild(el);
        }
      } : undefined;

    const chartOptions: uPlot.Options = {
      ...options,
      width: Math.max(Math.floor(rect.width), 300),
      height: typeof options.height === 'number' ? options.height : Math.max(Math.floor(rect.height), 200),
      series: options.series || [{}], // Ensure series is always defined
      legend: options.legend ? {
        ...options.legend,
        mount: legendMount,
      } : undefined,
    } as uPlot.Options;

    try {
      // Create new chart instance
      const chart = new uPlot(chartOptions, data, chartContainerRef.current);
      chartRef.current = chart;
      setIsReady(true);

      // Call onCreate callback
      if (onCreate) {
        onCreate(chart);
      }

      // Force a size sync on next frame in case initial rects were 0 or changed after mount
      requestAnimationFrame(() => {
        if (!chartContainerRef.current || !chartRef.current) return;
        const r2 = chartContainerRef.current.getBoundingClientRect();
        if (r2.width > 0 && r2.height > 0) {
          chartRef.current.setSize({ width: Math.floor(r2.width), height: Math.floor(r2.height) });
        }
      });
    } catch (error) {
      console.error('[UPlotWrapper] Failed to create chart:', error);
    }

    // Cleanup
    return () => {
      if (chartRef.current) {
        if (onDelete) {
          onDelete(chartRef.current);
        }
        chartRef.current.destroy();
        chartRef.current = null;
        setIsReady(false);
      }
    };
  }, [options, data]); // Recreate chart when options or data changes

  // Handle resize with debouncing
  useEffect(() => {
    if (!chartContainerRef.current || !chartRef.current || !isReady) return;

    let resizeTimeout: NodeJS.Timeout;
    const resizeObserver = new ResizeObserver((entries) => {
      clearTimeout(resizeTimeout);
      resizeTimeout = setTimeout(() => {
        for (const entry of entries) {
          const { width, height } = entry.contentRect;
          if (width > 0 && height > 0 && chartRef.current) {
            chartRef.current.setSize({
              width: Math.floor(width),
              height: Math.floor(height),
            });
          }
        }
      }, 16); // Debounce to roughly 60fps
    });

    resizeObserver.observe(chartContainerRef.current);

    // Also handle window resize
    const handleWindowResize = () => {
      if (chartContainerRef.current && chartRef.current) {
        const rect = chartContainerRef.current.getBoundingClientRect();
        if (rect.width > 0 && rect.height > 0) {
          chartRef.current.setSize({
            width: Math.floor(rect.width),
            height: Math.floor(rect.height),
          });
        }
      }
    };

    window.addEventListener('resize', handleWindowResize);

    return () => {
      clearTimeout(resizeTimeout);
      resizeObserver.disconnect();
      window.removeEventListener('resize', handleWindowResize);
    };
  }, [isReady]);

  // Handle container styles - ensure legend has space
  const containerStyle: React.CSSProperties = {
    width: typeof width === 'number' ? `${width}px` : width,
    height: typeof height === 'number' ? `${height}px` : height,
    position: 'relative',
    display: 'flex',
    flexDirection: 'column',
    minHeight: typeof height === 'string' ? '250px' : undefined,
    maxHeight: typeof height === 'string' ? '100%' : undefined,
  };

  const chartStyle: React.CSSProperties = {
    flex: typeof height === 'number' ? undefined : '1 1 auto',
    minHeight: typeof height === 'number' ? height : 0,
    height: typeof height === 'number' ? height : undefined,
    position: 'relative',
  };

  const legendStyle: React.CSSProperties = {
    flex: '0 0 auto',
    padding: '8px',
    backgroundColor: 'var(--legend-bg, rgba(30, 30, 30, 0.95))',
    borderTop: '1px solid var(--legend-border, rgba(255, 255, 255, 0.2))',
    minHeight: options?.legend?.show ? '40px' : '0',
  };

  return (
    <div
      ref={containerRef}
      className={`uplot-container ${className}`}
      style={containerStyle}
    >
      <div ref={chartContainerRef} style={chartStyle} />
      {options?.legend?.show && (
        <div ref={legendContainerRef} className="uplot-legend-container" style={legendStyle} />
      )}
    </div>
  );
}

// Export without memo to ensure chart updates properly
export const UPlotWrapper = UPlotWrapperComponent;
