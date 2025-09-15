import React, { useState } from 'react';
import {
  useTimeRange,
  TIME_RANGE_PRESETS,
  AUTO_REFRESH_INTERVALS,
} from '../context/TimeRangeContext';
import { Button } from '@/components/ui/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover';
import { Calendar } from '@/components/ui/calendar';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import {
  Clock,
  Calendar as CalendarIcon,
  RefreshCw,
  ChevronDown,
} from 'lucide-react';
import { format } from 'date-fns';

const TimeRangeSelector: React.FC = () => {
  const {
    timeRange,
    selectedPreset,
    autoRefresh,
    setTimeRange,
    setPreset,
    setAutoRefresh,
    refreshData,
    isRefreshing,
  } = useTimeRange();

  const [customRangeOpen, setCustomRangeOpen] = useState(false);
  const [customFrom, setCustomFrom] = useState<Date>(timeRange.from);
  const [customTo, setCustomTo] = useState<Date>(timeRange.to);
  const [customFromTime, setCustomFromTime] = useState(
    format(timeRange.from, 'HH:mm')
  );
  const [customToTime, setCustomToTime] = useState(
    format(timeRange.to, 'HH:mm')
  );

  const handlePresetSelect = (preset: string) => {
    setPreset(preset);
  };

  const handleAutoRefreshChange = (interval: number) => {
    setAutoRefresh({
      enabled: interval > 0,
      interval,
    });
  };

  const applyCustomRange = () => {
    const [fromHours, fromMinutes] = customFromTime.split(':').map(Number);
    const [toHours, toMinutes] = customToTime.split(':').map(Number);

    const fromDate = new Date(customFrom);
    fromDate.setHours(fromHours, fromMinutes, 0, 0);

    const toDate = new Date(customTo);
    toDate.setHours(toHours, toMinutes, 0, 0);

    setTimeRange({
      from: fromDate,
      to: toDate,
    });

    setCustomRangeOpen(false);
  };

  const formatTimeRange = () => {
    if (selectedPreset) {
      const preset = TIME_RANGE_PRESETS.find(p => p.value === selectedPreset);
      return preset?.label || 'Custom';
    }

    const formatDate = (date: Date) => {
      const now = new Date();
      const isToday = date.toDateString() === now.toDateString();
      const isYesterday =
        date.toDateString() === new Date(now.getTime() - 24 * 60 * 60 * 1000).toDateString();

      if (isToday) {
        return format(date, 'HH:mm');
      } else if (isYesterday) {
        return `Yesterday ${format(date, 'HH:mm')}`;
      } else {
        return format(date, 'MMM dd HH:mm');
      }
    };

    return `${formatDate(timeRange.from)} to ${formatDate(timeRange.to)}`;
  };

  return (
    <div className="flex items-center gap-2 p-2 bg-background border rounded-lg">
      {/* Time Range Selector */}
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="outline" className="w-64 justify-between">
            <div className="flex items-center gap-2">
              <Clock className="h-4 w-4" />
              <span className="truncate">{formatTimeRange()}</span>
            </div>
            <ChevronDown className="h-4 w-4 opacity-50" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent className="w-64" align="start">
          <DropdownMenuLabel>Quick ranges</DropdownMenuLabel>
          {TIME_RANGE_PRESETS.map((preset) => (
            <DropdownMenuItem
              key={preset.value}
              onClick={() => handlePresetSelect(preset.value)}
              className={selectedPreset === preset.value ? 'bg-accent' : ''}
            >
              {preset.label}
            </DropdownMenuItem>
          ))}
          <DropdownMenuSeparator />
          <DropdownMenuItem onClick={() => setCustomRangeOpen(true)}>
            <CalendarIcon className="h-4 w-4 mr-2" />
            Custom range
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

      {/* Auto Refresh */}
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="outline" size="sm">
            <RefreshCw className={`h-4 w-4 mr-1 ${isRefreshing ? 'animate-spin' : ''}`} />
            {AUTO_REFRESH_INTERVALS.find(i => i.value === autoRefresh.interval)?.label || 'Off'}
            <ChevronDown className="h-4 w-4 ml-1 opacity-50" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuLabel>Auto-refresh</DropdownMenuLabel>
          {AUTO_REFRESH_INTERVALS.map((interval) => (
            <DropdownMenuItem
              key={interval.value}
              onClick={() => handleAutoRefreshChange(interval.value)}
              className={autoRefresh.interval === interval.value ? 'bg-accent' : ''}
            >
              {interval.label}
            </DropdownMenuItem>
          ))}
        </DropdownMenuContent>
      </DropdownMenu>

      {/* Manual Refresh */}
      <Button
        variant="outline"
        size="sm"
        onClick={refreshData}
        disabled={isRefreshing}
      >
        <RefreshCw className={`h-4 w-4 ${isRefreshing ? 'animate-spin' : ''}`} />
      </Button>

      {/* Custom Range Modal */}
      <Popover open={customRangeOpen} onOpenChange={setCustomRangeOpen}>
        <PopoverTrigger asChild>
          <div />
        </PopoverTrigger>
        <PopoverContent className="w-auto p-0" align="start">
          <div className="p-4 space-y-4">
            <div className="text-sm font-medium">Custom time range</div>

            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label className="text-xs">From</Label>
                <Calendar
                  mode="single"
                  selected={customFrom}
                  onSelect={(date : any) => date && setCustomFrom(date)}
                  className="rounded-md border"
                  classNames={{}}
                />
                <Input
                  type="time"
                  value={customFromTime}
                  onChange={(e) => setCustomFromTime(e.target.value)}
                  className="text-xs"
                />
              </div>

              <div className="space-y-2">
                <Calendar
                  mode="single"
                  selected={customTo}
                  onSelect={(date : any) => date && setCustomTo(date)}
                  className="rounded-md border"
                  classNames={{}}
                />
                <Input
                  type="time"
                  value={customToTime}
                  onChange={(e) => setCustomToTime(e.target.value)}
                  className="text-xs"
                />
              </div>
            </div>

            <div className="flex gap-2 pt-2">
              <Button
                size="sm"
                onClick={applyCustomRange}
                className="flex-1"
              >
                Apply
              </Button>
              <Button
                variant="outline"
                size="sm"
                onClick={() => setCustomRangeOpen(false)}
                className="flex-1"
              >
                Cancel
              </Button>
            </div>
          </div>
        </PopoverContent>
      </Popover>
    </div>
  );
};

export default TimeRangeSelector;