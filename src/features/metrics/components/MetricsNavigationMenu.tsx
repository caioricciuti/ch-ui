import * as React from "react";
import { Link, useLocation, useNavigate } from "react-router-dom";
import { metrics } from "@/features/metrics/config/metricsConfig";
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@/components/ui/breadcrumb";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  DropdownMenuSeparator,
} from "@/components/ui/dropdown-menu";
import { Button } from "@/components/ui/button";
import { ChevronDown } from "lucide-react";

interface Metric {
  title: string;
  description: string;
  icon: React.ElementType;
  scope?: string;
}

export function MetricsNavigationMenu() {
  const navigate = useNavigate();

  const handleMetricClick = React.useCallback((metric: Metric) => {
    const scope = metric.scope;
    navigate(scope ? `/metrics?scope=${scope}` : "/metrics");
  }, [navigate]);

  return (
    <nav className="flex items-center justify-between w-full container pt-2 mb-4 mx-auto" aria-label="Metrics navigation">
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="outline" className="flex items-center gap-2" aria-label="Select metric">
            Metrics
            <ChevronDown className="h-4 w-4" aria-hidden="true" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" className="w-[300px]">
          {metrics.map((metric, index) => (
            <React.Fragment key={metric.title}>
              {index > 0 && <DropdownMenuSeparator />}
              <DropdownMenuItem
                onClick={() => handleMetricClick(metric)}
                className="flex items-start gap-2 p-2 cursor-pointer"
                role="menuitem"
              >
                <div className="mt-1">
                  {React.createElement(metric.icon, {
                    className: "h-5 w-5",
                    "aria-hidden": "true"
                  })}
                </div>
                <div className="flex flex-col">
                  <span className="font-medium">{metric.title}</span>
                  <span className="text-sm text-muted-foreground line-clamp-2">
                    {metric.description}
                  </span>
                </div>
              </DropdownMenuItem>
            </React.Fragment>
          ))}
        </DropdownMenuContent>
      </DropdownMenu>
      <DynamicBreadcrumb />
    </nav>
  );
}

const DynamicBreadcrumb = () => {
  const location = useLocation();
  const scope = new URLSearchParams(location.search).get("scope");

  const breadcrumbItems = React.useMemo(() => [
    { label: "Metrics", to: "/metrics" },
    ...(scope
      ? [
          {
            label: scope.charAt(0).toUpperCase() + scope.slice(1),
            to: `/metrics?scope=${scope}`,
          },
        ]
      : []),
  ], [scope]);

  return (
    <Breadcrumb aria-label="Breadcrumb navigation">
      <BreadcrumbList>
        {breadcrumbItems.map((item, index) => (
          <React.Fragment key={item.to}>
            {index > 0 && <BreadcrumbSeparator />}
            <BreadcrumbItem>
              {index === breadcrumbItems.length - 1 ? (
                <BreadcrumbPage>{item.label}</BreadcrumbPage>
              ) : (
                <BreadcrumbLink asChild>
                  <Link to={item.to}>{item.label}</Link>
                </BreadcrumbLink>
              )}
            </BreadcrumbItem>
          </React.Fragment>
        ))}
      </BreadcrumbList>
    </Breadcrumb>
  );
};

export default MetricsNavigationMenu;