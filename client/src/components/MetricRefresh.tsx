// src/components/MetricRefresh.tsx
import React, { useState, useEffect } from "react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { RefreshCcw, Check, ChevronDown, X } from "lucide-react";
import useMetricsStore from "@/stores/metrics.store";
import { Button } from "./ui/button";
import { toast } from "sonner";

type TimeUnit = "seconds" | "minutes" | "hours" | "days";

interface CustomInterval {
  amount: number;
  unit: TimeUnit;
}

const predefinedIntervals: { label: string; interval: number }[] = [
  { label: "2 seconds", interval: 2000 },
  { label: "15 seconds", interval: 15000 },
  { label: "1 minute", interval: 60000 },
  { label: "10 minutes", interval: 600000 },
  { label: "30 minutes", interval: 1800000 },
  { label: "1 hour", interval: 3600000 },
];

const MetricRefresh: React.FC = () => {
  const { fetchMetrics } = useMetricsStore();
  const [selectedInterval, setSelectedInterval] = useState<number | null>(null);
  const [customInterval, setCustomInterval] = useState<CustomInterval>({
    amount: 1,
    unit: "minutes",
  });
  const [isCustom, setIsCustom] = useState(false);
  const [timer, setTimer] = useState<NodeJS.Timeout | null>(null);

  const clearExistingTimer = () => {
    if (timer) {
      clearInterval(timer);
      setTimer(null);
    }
  };

  const startTimer = (interval: number) => {
    clearExistingTimer();
    const newTimer = setInterval(() => {
      fetchMetrics();
      toast.success("Metrics refreshed automatically.");
    }, interval);
    setTimer(newTimer);
  };

  const handleSelectInterval = (interval: number) => {
    setSelectedInterval(interval);
    setIsCustom(false);
    startTimer(interval);
    toast.success(`Metrics will refresh every ${interval / 1000} seconds.`);
  };

  const handleCustomInterval = () => {
    const { amount, unit } = customInterval;
    let interval = 0;
    switch (unit) {
      case "seconds":
        interval = amount * 1000;
        break;
      case "minutes":
        interval = amount * 60 * 1000;
        break;
      case "hours":
        interval = amount * 60 * 60 * 1000;
        break;
      case "days":
        interval = amount * 24 * 60 * 60 * 1000;
        break;
      default:
        break;
    }
    if (interval > 0) {
      setSelectedInterval(interval);
      setIsCustom(true);
      startTimer(interval);
      toast.success(`Metrics will refresh every ${amount} ${unit}.`);
    } else {
      toast.error("Please enter a valid interval.");
    }
  };

  useEffect(() => {
    // Cleanup on unmount
    return () => {
      clearExistingTimer();
    };
  }, [timer]);

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="sm" className="flex items-center">
          <RefreshCcw className="w-4 h-4 mr-1" />
          Refresh
          <ChevronDown className="w-4 h-4 ml-1" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-56">
        <div className="px-4 py-2">
          <span className="text-sm font-semibold">Auto Refresh</span>
        </div>
        {predefinedIntervals.map((item) => (
          <DropdownMenuItem
            key={item.label}
            onSelect={() => handleSelectInterval(item.interval)}
            className={
              selectedInterval === item.interval && !isCustom
                ? "bg-gray-100 dark:bg-gray-700"
                : ""
            }
          >
            {selectedInterval === item.interval && !isCustom && (
              <Check className="w-4 h-4 mr-2" />
            )}
            {item.label}
          </DropdownMenuItem>
        ))}
        <DropdownMenuItem
          onSelect={() => setIsCustom(true)}
          className={isCustom ? "bg-gray-100 dark:bg-gray-700" : ""}
        >
          {isCustom && <Check className="w-4 h-4 mr-2" />}
          Custom
        </DropdownMenuItem>
        {isCustom && (
          <div className="px-4 py-2">
            <div className="flex items-center space-x-2">
              <input
                type="number"
                min="1"
                value={customInterval.amount}
                onChange={(e) =>
                  setCustomInterval({
                    ...customInterval,
                    amount: Number(e.target.value),
                  })
                }
                className="w-16 px-2 py-1 border rounded"
                placeholder="1"
              />
              <select
                value={customInterval.unit}
                onChange={(e) =>
                  setCustomInterval({
                    ...customInterval,
                    unit: e.target.value as TimeUnit,
                  })
                }
                className="px-2 py-1 border rounded"
              >
                <option value="seconds">Seconds</option>
                <option value="minutes">Minutes</option>
                <option value="hours">Hours</option>
                <option value="days">Days</option>
              </select>
              <Button
                variant="secondary"
                size="sm"
                onClick={handleCustomInterval}
              >
                Set
              </Button>
            </div>
          </div>
        )}
        {selectedInterval && (
          <DropdownMenuItem onSelect={clearExistingTimer}>
            <X className="w-4 h-4 mr-2 text-red-500" />
            Stop Auto Refresh
          </DropdownMenuItem>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
};

export default MetricRefresh;
