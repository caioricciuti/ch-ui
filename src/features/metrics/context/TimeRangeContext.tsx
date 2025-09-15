import React, { createContext, useContext, useState, useCallback, ReactNode } from 'react';

export interface TimeRange {
  from: Date;
  to: Date;
  label?: string;
}

interface TimeRangePreset {
  label: string;
  value: string;
  from: () => Date;
  to: () => Date;
}

export const TIME_RANGE_PRESETS: TimeRangePreset[] = [
  {
    label: 'Last 5 minutes',
    value: '5m',
    from: () => new Date(Date.now() - 5 * 60 * 1000),
    to: () => new Date(),
  },
  {
    label: 'Last 15 minutes',
    value: '15m',
    from: () => new Date(Date.now() - 15 * 60 * 1000),
    to: () => new Date(),
  },
  {
    label: 'Last 30 minutes',
    value: '30m',
    from: () => new Date(Date.now() - 30 * 60 * 1000),
    to: () => new Date(),
  },
  {
    label: 'Last 1 hour',
    value: '1h',
    from: () => new Date(Date.now() - 60 * 60 * 1000),
    to: () => new Date(),
  },
  {
    label: 'Last 3 hours',
    value: '3h',
    from: () => new Date(Date.now() - 3 * 60 * 60 * 1000),
    to: () => new Date(),
  },
  {
    label: 'Last 6 hours',
    value: '6h',
    from: () => new Date(Date.now() - 6 * 60 * 60 * 1000),
    to: () => new Date(),
  },
  {
    label: 'Last 12 hours',
    value: '12h',
    from: () => new Date(Date.now() - 12 * 60 * 60 * 1000),
    to: () => new Date(),
  },
  {
    label: 'Last 24 hours',
    value: '24h',
    from: () => new Date(Date.now() - 24 * 60 * 60 * 1000),
    to: () => new Date(),
  },
  {
    label: 'Last 2 days',
    value: '2d',
    from: () => new Date(Date.now() - 2 * 24 * 60 * 60 * 1000),
    to: () => new Date(),
  },
  {
    label: 'Last 7 days',
    value: '7d',
    from: () => new Date(Date.now() - 7 * 24 * 60 * 60 * 1000),
    to: () => new Date(),
  },
  {
    label: 'Last 30 days',
    value: '30d',
    from: () => new Date(Date.now() - 30 * 24 * 60 * 60 * 1000),
    to: () => new Date(),
  },
];

interface AutoRefreshOptions {
  enabled: boolean;
  interval: number; // in milliseconds
}

export const AUTO_REFRESH_INTERVALS = [
  { label: 'Off', value: 0 },
  { label: '5s', value: 5000 },
  { label: '10s', value: 10000 },
  { label: '30s', value: 30000 },
  { label: '1m', value: 60000 },
  { label: '5m', value: 300000 },
  { label: '15m', value: 900000 },
  { label: '30m', value: 1800000 },
];

interface TimeRangeContextValue {
  timeRange: TimeRange;
  selectedPreset: string | null;
  autoRefresh: AutoRefreshOptions;
  setTimeRange: (range: TimeRange) => void;
  setPreset: (preset: string) => void;
  setAutoRefresh: (options: AutoRefreshOptions) => void;
  refreshData: () => void;
  isRefreshing: boolean;
}

const TimeRangeContext = createContext<TimeRangeContextValue | null>(null);

export const useTimeRange = (): TimeRangeContextValue => {
  const context = useContext(TimeRangeContext);
  if (!context) {
    throw new Error('useTimeRange must be used within a TimeRangeProvider');
  }
  return context;
};

interface TimeRangeProviderProps {
  children: ReactNode;
  defaultPreset?: string;
}

export const TimeRangeProvider: React.FC<TimeRangeProviderProps> = ({
  children,
  defaultPreset = '1h'
}) => {
  // Initialize with default preset
  const initialPreset = TIME_RANGE_PRESETS.find(p => p.value === defaultPreset) || TIME_RANGE_PRESETS[3];
  const [timeRange, setTimeRangeState] = useState<TimeRange>({
    from: initialPreset.from(),
    to: initialPreset.to(),
    label: initialPreset.label,
  });

  const [selectedPreset, setSelectedPreset] = useState<string | null>(defaultPreset);
  const [autoRefresh, setAutoRefreshState] = useState<AutoRefreshOptions>({
    enabled: false,
    interval: 0,
  });
  const [isRefreshing, setIsRefreshing] = useState(false);

  const setTimeRange = useCallback((range: TimeRange) => {
    setTimeRangeState(range);
    // Clear preset when custom range is set
    if (!range.label) {
      setSelectedPreset(null);
    }
  }, []);

  const setPreset = useCallback((preset: string) => {
    const presetConfig = TIME_RANGE_PRESETS.find(p => p.value === preset);
    if (presetConfig) {
      setTimeRangeState({
        from: presetConfig.from(),
        to: presetConfig.to(),
        label: presetConfig.label,
      });
      setSelectedPreset(preset);
    }
  }, []);

  const setAutoRefresh = useCallback((options: AutoRefreshOptions) => {
    setAutoRefreshState(options);
  }, []);

  const refreshData = useCallback(() => {
    setIsRefreshing(true);

    // If we have a preset selected, update the time range to current time
    if (selectedPreset) {
      const presetConfig = TIME_RANGE_PRESETS.find(p => p.value === selectedPreset);
      if (presetConfig) {
        setTimeRangeState({
          from: presetConfig.from(),
          to: presetConfig.to(),
          label: presetConfig.label,
        });
      }
    }

    // Simulate refresh delay
    setTimeout(() => {
      setIsRefreshing(false);
    }, 500);
  }, [selectedPreset]);

  // Auto-refresh effect
  React.useEffect(() => {
    if (!autoRefresh.enabled || autoRefresh.interval === 0) {
      return;
    }

    const intervalId = setInterval(() => {
      refreshData();
    }, autoRefresh.interval);

    return () => clearInterval(intervalId);
  }, [autoRefresh, refreshData]);

  const value: TimeRangeContextValue = {
    timeRange,
    selectedPreset,
    autoRefresh,
    setTimeRange,
    setPreset,
    setAutoRefresh,
    refreshData,
    isRefreshing,
  };

  return (
    <TimeRangeContext.Provider value={value}>
      {children}
    </TimeRangeContext.Provider>
  );
};
