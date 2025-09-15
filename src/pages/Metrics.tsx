import React, { useEffect } from "react";
import { metrics } from "@/features/metrics/config/metricsConfig";
import UPlotMetricItemComponent from "@/features/metrics/components/UPlotMetricItemComponent";
import DashboardGrid from "@/features/metrics/components/DashboardGrid";
import MetricsNavTabs from "@/features/metrics/components/MetricsNavTabs";
import TimeRangeSelector from "@/features/metrics/components/TimeRangeSelector";
import { TimeRangeProvider } from "@/features/metrics/context/TimeRangeContext";
import { Link, useLocation, useNavigate } from "react-router-dom";
import { toast } from "sonner";
import useAppStore from "@/store";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";

function MetricsOverview() {
  const location = useLocation();
  const navigate = useNavigate();
  const scope = new URLSearchParams(location.search).get("scope") || "overview";
  const { credential, isServerAvailable } = useAppStore();
  const [isLocalHostInstance, setIsLocalHostInstance] = React.useState(false);

  useEffect(() => {
    document.title = "CH-UI | Metrics";
    if (!isServerAvailable) {
      toast.error(
        "No active connection. Please configure your connection in Settings."
      );
      navigate("/settings");
      return;
    }

    if (
      credential?.url.includes("localhost") ||
      credential?.url.includes("127.0.0.1")
    ) {
      setIsLocalHostInstance(true);
    }

    const metric = metrics.find((m) => m.scope === scope);
    if (!metric) {
      navigate("/metrics?scope=overview");
      toast.error("Invalid metric scope");
    }
  }, [scope, navigate, credential, isServerAvailable]);

  const currentMetric =
    metrics.find((m) => m.scope === scope) ||
    metrics.find((m) => m.scope === "overview");

  if (!currentMetric) {
    return (
      <div className="container mx-auto mt-4">
        <div>
          <h1 className="text-2xl font-bold text-foreground">Metrics</h1>
          <p className="text-muted-foreground">
            You're trying to access an invalid metric scope...
          </p>
          <div className="grid grid-cols-4 mt-12 gap-4 border p-4 rounded-md">
            {metrics.map((metric) => (
              <div key={metric.title} className="mt-4 max-w-[250px]">
                <Link
                  to={`/metrics?scope=${metric.scope}`}
                  className="text-primary hover:underline"
                >
                  <div className="text-lg font-bold text-foreground flex items-center">
                    {metric.title}{" "}
                    <span className="ml-4">
                      {React.createElement(metric.icon)}
                    </span>
                  </div>
                </Link>
                <p className="text-muted-foreground">{metric.description}</p>
              </div>
            ))}
          </div>
        </div>
      </div>
    );
  }

  const getColSpanClass = (tiles: number) => {
    // Use a 12-column grid for more granular layout
    switch (tiles) {
      case 1:
        return "col-span-12 md:col-span-3"; // quarter width on md+
      case 2:
        return "col-span-12 md:col-span-6"; // half width
      case 3:
        return "col-span-12 md:col-span-9"; // three-quarters
      case 4:
      default:
        return "col-span-12"; // full width
    }
  };

  return (
    <TimeRangeProvider defaultPreset="7d">
      {/* Fit within App's flex h-screen; avoid nested h-screen */}
      <div className="flex-1 w-full overflow-auto">
        <main className="container mx-auto pb-4">
          <div className="py-2 flex items-center justify-between gap-4">
            <h1 className="text-xl font-semibold tracking-tight">Metrics</h1>
            <MetricsNavTabs />
          </div>
          {isLocalHostInstance && (
            <Alert className="my-2" variant="warning">
              <AlertTitle className="text-sm">Local Instance Detected</AlertTitle>
              <AlertDescription className="text-xs">
                Some metrics may not be available on local ClickHouse instances.
              </AlertDescription>
            </Alert>
          )}

          {/* Metric header with time selector */}
          <div className="mb-3 flex items-center justify-between">
            <div className="flex items-center space-x-3">
              {React.createElement(currentMetric.icon, {
                className: "w-5 h-5 text-primary",
              })}
              <div>
                <h2 className="text-lg font-semibold text-foreground">
                  {currentMetric.title}
                </h2>
                <p className="text-xs text-muted-foreground">{currentMetric.description}</p>
              </div>
            </div>
            <TimeRangeSelector />
          </div>

          <DashboardGrid
            scope={currentMetric.scope}
            items={currentMetric.items || []}
            renderItem={(it) => (
              <UPlotMetricItemComponent item={it as any} />
            )}
          />
        </main>
      </div>
    </TimeRangeProvider>
  );
}

export default MetricsOverview;
