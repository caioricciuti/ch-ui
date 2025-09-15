import { TimeRange } from '../context/TimeRangeContext';

interface QueryVariable {
  name: string;
  value: string | number | Date;
}

/**
 * Format date for ClickHouse queries
 */
const formatClickHouseDate = (date: Date): string => {
  return date.toISOString().slice(0, 19).replace('T', ' ');
};

/**
 * Format date range for ClickHouse BETWEEN clause
 */
const formatTimeFromTo = (timeRange: TimeRange): string => {
  const fromStr = formatClickHouseDate(timeRange.from);
  const toStr = formatClickHouseDate(timeRange.to);
  return `'${fromStr}' AND '${toStr}'`;
};

/**
 * Get all built-in variables for the current time range
 */
const getBuiltInVariables = (timeRange: TimeRange): QueryVariable[] => {
  return [
    {
      name: '$__timeFromTo',
      value: formatTimeFromTo(timeRange),
    },
    {
      name: '$__timeFrom',
      value: `'${formatClickHouseDate(timeRange.from)}'`,
    },
    {
      name: '$__timeTo',
      value: `'${formatClickHouseDate(timeRange.to)}'`,
    },
    {
      name: '$__timeFilter',
      value: `event_time BETWEEN ${formatTimeFromTo(timeRange)}`,
    },
    {
      name: '$__interval',
      value: calculateAutoInterval(timeRange),
    },
    {
      name: '$__timeGroup',
      value: getTimeGroupingFunction(timeRange),
    },
    {
      name: '$__seconds',
      value: Math.max(1, Math.floor((timeRange.to.getTime() - timeRange.from.getTime()) / 1000)),
    },
    {
      name: '$__bucketSec',
      value: getBucketStepSeconds(timeRange),
    },
    {
      name: '$__timeBucket',
      value: getAdaptiveTimeGroupExpr(timeRange),
    },
    {
      name: '$__timeBucket',
      value: getAdaptiveTimeGroupExpr(timeRange),
    },
    {
      name: '$__unixEpochFrom',
      value: Math.floor(timeRange.from.getTime() / 1000),
    },
    {
      name: '$__unixEpochTo',
      value: Math.floor(timeRange.to.getTime() / 1000),
    },
  ];
};

/**
 * Calculate appropriate interval for the time range (for grouping)
 */
const calculateAutoInterval = (timeRange: TimeRange): string => {
  const diffMs = timeRange.to.getTime() - timeRange.from.getTime();
  const diffMinutes = diffMs / (1000 * 60);

  // Return appropriate interval for ClickHouse
  if (diffMinutes <= 30) return 'INTERVAL 1 MINUTE';
  if (diffMinutes <= 180) return 'INTERVAL 5 MINUTE';
  if (diffMinutes <= 720) return 'INTERVAL 15 MINUTE';
  if (diffMinutes <= 1440) return 'INTERVAL 1 HOUR';
  if (diffMinutes <= 10080) return 'INTERVAL 6 HOUR';
  if (diffMinutes <= 43200) return 'INTERVAL 1 DAY';
  return 'INTERVAL 1 WEEK';
};

/**
 * Get the appropriate time grouping function based on the interval
 */
const getTimeGroupingFunction = (timeRange: TimeRange): string => {
  const diffMs = timeRange.to.getTime() - timeRange.from.getTime();
  const diffMinutes = diffMs / (1000 * 60);

  if (diffMinutes <= 30) return 'toStartOfMinute';
  if (diffMinutes <= 180) return 'toStartOfFiveMinutes';
  if (diffMinutes <= 720) return 'toStartOfFifteenMinutes';
  if (diffMinutes <= 1440) return 'toStartOfHour';
  if (diffMinutes <= 10080) return 'toStartOfSixHours';
  if (diffMinutes <= 43200) return 'toStartOfDay';
  return 'toStartOfWeek';
};

/**
 * Determine bucket step in seconds targeting ~120 buckets
 */
const getBucketStepSeconds = (timeRange: TimeRange, targetBuckets = 120): number => {
  const diffSec = Math.max(1, Math.floor((timeRange.to.getTime() - timeRange.from.getTime()) / 1000));
  const raw = Math.max(1, Math.floor(diffSec / Math.max(1, targetBuckets)));
  // Snap to nice steps
  const steps = [1, 5, 10, 15, 30, 60, 120, 300, 600, 900, 1800, 3600, 7200, 10800, 21600, 43200, 86400, 604800];
  let step = steps[0];
  for (const s of steps) {
    step = s;
    if (s >= raw) break;
  }
  return step;
};

/**
 * Adaptive grouping expression using toStartOfInterval(event_time, INTERVAL $__bucketSec SECOND)
 */
const getAdaptiveTimeGroupExpr = (timeRange: TimeRange): string => {
  const step = getBucketStepSeconds(timeRange);
  return `toStartOfInterval(event_time, INTERVAL ${step} SECOND)`;
};

/**
 * Interpolate query string with variables
 */
export const interpolateQuery = (
  query: string,
  timeRange: TimeRange,
  customVariables: QueryVariable[] = []
): string => {
  let interpolatedQuery = query;

  // Get all variables (built-in + custom)
  const builtInVariables = getBuiltInVariables(timeRange);
  const allVariables = [...builtInVariables, ...customVariables];

  // Replace each variable in the query
  allVariables.forEach(variable => {
    const placeholder = variable.name;
    const regex = new RegExp(escapeRegExp(placeholder), 'g');
    interpolatedQuery = interpolatedQuery.replace(regex, String(variable.value));
  });

  return interpolatedQuery;
};

/**
 * Escape special regex characters
 */
function escapeRegExp(string: string): string {
  return string.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

/**
 * Validate that a query contains time filtering
 */
// Removed validateTimeFilterQuery and getSampleTimeRange (dead code)

/**
 * Preview interpolated query for debugging
 */
export const previewQuery = (
  originalQuery: string,
  timeRange: TimeRange,
  customVariables: QueryVariable[] = []
): {
  original: string;
  interpolated: string;
  variables: QueryVariable[];
} => {
  const allVariables = [...getBuiltInVariables(timeRange), ...customVariables];
  const interpolated = interpolateQuery(originalQuery, timeRange, customVariables);

  return {
    original: originalQuery,
    interpolated,
    variables: allVariables,
  };
};
