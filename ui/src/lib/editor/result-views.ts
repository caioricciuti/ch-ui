/** The views of a query result, in footer order. Data is the default. */
export type ResultView = 'data' | 'stats' | 'schema' | 'insights' | 'chart'

export const RESULT_VIEWS: readonly { id: ResultView; label: string }[] = [
  { id: 'data', label: 'Data' },
  { id: 'stats', label: 'Stats' },
  { id: 'schema', label: 'Schema' },
  { id: 'insights', label: 'Insights' },
  { id: 'chart', label: 'Chart' },
]

export const DEFAULT_RESULT_VIEW: ResultView = 'data'
