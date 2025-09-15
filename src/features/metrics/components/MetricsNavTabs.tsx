import * as React from "react";
import { useNavigate, useLocation } from "react-router-dom";
import { metrics } from "@/features/metrics/config/metricsConfig";

export default function MetricsNavTabs() {
  const navigate = useNavigate();
  const location = useLocation();
  const scope = new URLSearchParams(location.search).get("scope") || "overview";

  const handleClick = (nextScope: string) => {
    navigate(`/metrics?scope=${nextScope}`);
  };

  return (
    <div className="w-full overflow-x-auto">
      <div className="inline-flex items-center gap-2 whitespace-nowrap p-1 rounded-md border bg-background">
        {metrics.map((m) => {
          const active = m.scope === scope;
          return (
            <button
              key={m.scope}
              onClick={() => handleClick(m.scope)}
              className={
                `flex items-center gap-2 px-3 py-1.5 rounded-md transition-colors ` +
                (active
                  ? `bg-primary text-primary-foreground`
                  : `hover:bg-accent text-foreground`)
              }
              aria-current={active ? "page" : undefined}
            >
              {React.createElement(m.icon, { className: "h-4 w-4" })}
              <span className="text-sm font-medium">{m.title}</span>
            </button>
          );
        })}
      </div>
    </div>
  );
}

