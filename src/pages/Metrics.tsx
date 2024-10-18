import React, { useEffect } from "react";
import { metrics } from "@/helpers/metricsConfig";
import MetricItemComponent from "@/components/metrics/MetricItemComponent";
import MetricsNavigationMenu from "@/components/metrics/MetricsNavigationMenu";
import { Link, useLocation, useNavigate } from "react-router-dom";
import { toast } from "sonner";
import useAppStore from "@/store/appStore";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { useTitle } from "@/hooks/useTitle";

function MetricsOverview() {
  const location = useLocation();
  const navigate = useNavigate();
  const scope = new URLSearchParams(location.search).get("scope") || "overview";
  const { credential, isServerAvailable } = useAppStore();
  const [isLocalHostInstance, setIsLocalHostInstance] = React.useState(false);

  useTitle("Metrics - Workspace");

  useEffect(() => {
    if (!isServerAvailable) {
      toast.error(
        "No active connection. Please configure your connection in Settings."
      );
      navigate("/settings");
      return;
    }

    if (
      credential?.host.includes("localhost") ||
      credential?.host.includes("127.0.0.1")
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
    switch (tiles) {
      case 1:
        return "col-span-1";
      case 2:
        return "col-span-2";
      case 3:
        return "col-span-3";
      case 4:
        return "col-span-full";
      default:
        return "col-span-full";
    }
  };

  return (
    <div className="h-screen w-full overflow-auto">
      <MetricsNavigationMenu />
      <main className="container mx-auto pb-12">
        {isLocalHostInstance && (
          <Alert className="my-4" variant="warning">
            <AlertTitle>Local Instance Detected</AlertTitle>
            <AlertDescription>
              We have detected that you are using a local instance of Click
              House. There are some metrics that may not be available due to the
              limitations of the local instance.
            </AlertDescription>
          </Alert>
        )}
        <div className="mb-8">
          <div>
            <div className="flex items-center space-x-4">
              {React.createElement(currentMetric.icon, {
                className: "w-6 h-6 text-primary",
              })}
              <h1 className="text-2xl font-bold text-foreground">
                {currentMetric.title}
              </h1>
            </div>
          </div>
          <p className="text-muted-foreground">{currentMetric.description}</p>
        </div>

        <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
          {currentMetric.items?.map((item) => (
            <div key={item.title} className={getColSpanClass(item.tiles || 4)}>
              <MetricItemComponent item={item} />
            </div>
          ))}
        </div>
      </main>
    </div>
  );
}

export default MetricsOverview;
