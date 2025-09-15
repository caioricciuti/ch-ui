import type uPlot from "uplot";
import { formatValue } from "./utils";
import type { TooltipOptions } from "./types";

/**
 * Create a tooltip plugin for uPlot charts
 */
export function createTooltipPlugin(
  options: TooltipOptions = {}
): uPlot.Plugin {
  const {
    formatX,
    formatValue: customFormatValue = formatValue,
    formatSeriesValue,
    decimals = 2,
    unit,
    background = 'rgba(30, 30, 30, 0.95)',
    textColor = 'rgba(255, 255, 255, 0.9)',
    borderColor = 'rgba(255, 255, 255, 0.1)',
  } = options;

  let tooltipEl: HTMLDivElement | null = null;
  let seriesEls: HTMLDivElement[] = [];

  return {
    hooks: {
      init: [
        (u) => {
          // Create tooltip container
          tooltipEl = document.createElement("div");
          tooltipEl.className = "uplot-tooltip";
          tooltipEl.style.cssText = `
            position: absolute;
            display: none;
            padding: 8px 12px;
            background: ${background};
            border: 1px solid ${borderColor};
            border-radius: 4px;
            font-size: 12px;
            color: ${textColor};
            pointer-events: none;
            z-index: 100;
            box-shadow: 0 2px 8px rgba(0, 0, 0, 0.5);
            max-width: 300px;
          `;

          // Create time header
          const timeEl = document.createElement("div");
          timeEl.className = "tooltip-time";
          timeEl.style.cssText = `
            font-weight: 600;
            margin-bottom: 6px;
            padding-bottom: 6px;
            border-bottom: 1px solid rgba(255, 255, 255, 0.1);
          `;
          tooltipEl.appendChild(timeEl);

          // Create series containers
          seriesEls = [];
          for (let i = 1; i < u.series.length; i++) {
            const seriesEl = document.createElement("div");
            seriesEl.className = "tooltip-series";
            seriesEl.style.cssText = `
              display: flex;
              align-items: center;
              gap: 8px;
              margin: 4px 0;
            `;

            const markerEl = document.createElement("div");
            markerEl.className = "tooltip-marker";
            const stroke = u.series[i]?.stroke;
            let strokeColor = '#888'; // default color
            if (stroke) {
              strokeColor = typeof stroke === 'function'
                ? (stroke as any)(u, i)
                : stroke as string;
            }
            markerEl.style.cssText = `
              width: 10px;
              height: 10px;
              background: ${strokeColor};
              border-radius: 1px;
              flex-shrink: 0;
            `;

            const labelEl = document.createElement("span");
            labelEl.className = "tooltip-label";
            labelEl.style.cssText = `
              flex: 1;
              color: ${textColor};
              opacity: 0.8;
            `;

            const valueEl = document.createElement("span");
            valueEl.className = "tooltip-value";
            valueEl.style.cssText = `
              font-weight: 600;
              color: ${textColor};
            `;

            seriesEl.appendChild(markerEl);
            seriesEl.appendChild(labelEl);
            seriesEl.appendChild(valueEl);
            tooltipEl.appendChild(seriesEl);
            seriesEls.push(seriesEl);
          }

          u.over.appendChild(tooltipEl);
        },
      ],
      destroy: [
        () => {
          if (tooltipEl && tooltipEl.parentNode) {
            tooltipEl.parentNode.removeChild(tooltipEl);
          }
          tooltipEl = null;
          seriesEls = [];
        },
      ],
      setCursor: [
        (u) => {
          const { idx, left, top } = u.cursor;

          if (idx == null || !tooltipEl) {
            if (tooltipEl) {
              tooltipEl.style.display = "none";
            }
            return;
          }

          // Update time
          const timeEl = tooltipEl.querySelector(
            ".tooltip-time"
          ) as HTMLDivElement;
          if (timeEl && u.data[0][idx] != null) {
            if (formatX) {
              timeEl.textContent = formatX(u, idx);
            } else {
              // default behavior assumes seconds epoch
              const timestamp = Number(u.data[0][idx]) * 1000;
              const date = new Date(timestamp);
              const dateStr = date.toLocaleDateString("en-US", {
                month: "short",
                day: "numeric",
                year: "numeric",
              });
              const timeStr = date.toLocaleTimeString("en-US", {
                hour: "2-digit",
                minute: "2-digit",
                second: "2-digit",
              });
              timeEl.textContent = `${dateStr} ${timeStr}`;
            }
          }

          // Update series values - show ALL series at this x position
          let hasVisibleSeries = false;

          for (let i = 1; i < u.series.length; i++) {
            const series = u.series[i];
            const seriesEl = seriesEls[i - 1];

            if (!seriesEl) continue;

            const value = u.data[i][idx];

            // Only show series in tooltip if it's visible in the chart
            if (value != null && series.show !== false) {
              hasVisibleSeries = true;
              seriesEl.style.display = "flex";
              seriesEl.style.opacity = "1";

              const labelEl = seriesEl.querySelector(
                ".tooltip-label"
              ) as HTMLSpanElement;
              const valueEl = seriesEl.querySelector(
                ".tooltip-value"
              ) as HTMLSpanElement;
              const markerEl = seriesEl.querySelector(
                ".tooltip-marker"
              ) as HTMLDivElement;

              if (labelEl)
                labelEl.textContent = (series.label || `Series ${i}`) as string;
              if (valueEl) {
                if (formatSeriesValue) {
                  valueEl.textContent = formatSeriesValue(String(series.label || `Series ${i}`), value);
                } else {
                  valueEl.textContent = customFormatValue(value, decimals, unit);
                }
              }
              if (markerEl) {
                const strokeColor = typeof series.stroke === 'function'
                  ? series.stroke(u, i)
                  : series.stroke;
                markerEl.style.background = strokeColor as string;
              }
            } else {
              seriesEl.style.display = "none";
            }
          }

          if (hasVisibleSeries) {
            // Position tooltip
            tooltipEl.style.display = "block";

            const rect = u.over.getBoundingClientRect();
            const tooltipRect = tooltipEl.getBoundingClientRect();

            let xPos = (left ?? 0) + 15;
            let yPos = (top ?? 0) + 15;

            // Adjust position if tooltip would go off screen
            if (xPos + tooltipRect.width > rect.width) {
              xPos = (left ?? 0) - tooltipRect.width - 15;
            }
            if (yPos + tooltipRect.height > rect.height) {
              yPos = (top ?? 0) - tooltipRect.height - 15;
            }

            tooltipEl.style.left = `${Math.max(0, xPos)}px`;
            tooltipEl.style.top = `${Math.max(0, yPos)}px`;
          } else {
            tooltipEl.style.display = "none";
          }
        },
      ],
    },
  };
}
