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
import { motion } from "framer-motion";

function MetricsPage() {
  const location = useLocation();
  const navigate = useNavigate();
  const scope = new URLSearchParams(location.search).get("scope") || "overview";
  const { credential, isServerAvailable } = useAppStore();
  const [isLocalHostInstance, setIsLocalHostInstance] = React.useState(false);

  useEffect(() => {
    document.title = "ClickHouse UI | Metrics";
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
      <div className="container mx-auto mt-4 p-6">
        <div>
          <h1 className="text-3xl font-bold text-white/90">Metrics</h1>
          <p className="text-gray-400">
            You're trying to access an invalid metric scope...
          </p>
          <div className="grid grid-cols-4 mt-12 gap-4 border border-white/10 p-4 rounded-xl bg-white/5 backdrop-blur-sm">
            {metrics.map((metric) => (
              <div key={metric.title} className="mt-4 max-w-[250px]">
                <Link
                  to={`/metrics?scope=${metric.scope}`}
                  className="text-purple-400 hover:text-purple-300 hover:underline"
                >
                  <div className="text-lg font-bold flex items-center">
                    {metric.title}{" "}
                    <span className="ml-4">
                      {React.createElement(metric.icon)}
                    </span>
                  </div>
                </Link>
                <p className="text-gray-400">{metric.description}</p>
              </div>
            ))}
          </div>
        </div>
      </div>
    );
  }

  return (
    <TimeRangeProvider defaultPreset="1h">
      <motion.div
        initial={{ opacity: 0, y: 20 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.5 }}
        className="flex-1 w-full overflow-auto"
      >
        <main className="container mx-auto p-6 space-y-6">
          <div className="flex items-center justify-between">
            <div>
              <h1 className="text-3xl font-bold tracking-tight text-white/90">Metrics</h1>
              <p className="text-gray-400">Real-time system performance monitoring</p>
            </div>
            <MetricsNavTabs />
          </div>

          {isLocalHostInstance && (
            <Alert className="my-2 bg-yellow-500/10 border-yellow-500/20 text-yellow-200" variant="default">
              <AlertTitle className="text-sm font-semibold">Local Instance Detected</AlertTitle>
              <AlertDescription className="text-xs opacity-90">
                Some metrics may not be available on local ClickHouse instances.
              </AlertDescription>
            </Alert>
          )}

          {/* Metric header with time selector */}
          <div className="flex items-center justify-between bg-white/5 p-4 rounded-xl border border-white/10 backdrop-blur-md">
            <div className="flex items-center space-x-3">
              <div className="p-2 rounded-lg bg-purple-500/20">
                {React.createElement(currentMetric.icon, {
                  className: "w-5 h-5 text-purple-400",
                })}
              </div>
              <div>
                <h2 className="text-lg font-semibold text-white/90">
                  {currentMetric.title}
                </h2>
                <p className="text-xs text-gray-400">{currentMetric.description}</p>
              </div>
            </div>
            <TimeRangeSelector />
          </div>

          <div className="rounded-xl">
            <DashboardGrid
              scope={currentMetric.scope}
              items={currentMetric.items || []}
              renderItem={(it) => (
                <div className="h-full w-full rounded-xl border border-white/10 bg-black/40 backdrop-blur-xl overflow-hidden shadow-lg hover:shadow-purple-500/5 transition-all duration-300">
                  <div className="p-4 h-full">
                    <UPlotMetricItemComponent item={it as any} />
                  </div>
                </div>
              )}
            />
          </div>
        </main>
      </motion.div>
    </TimeRangeProvider>
  );
}

export default MetricsPage;
