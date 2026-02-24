import type { PanelConfig } from '../types/api'
import type uPlot from 'uplot'

export interface ColumnMeta {
  name: string
  type: string
}

export const DEFAULT_COLORS = [
  '#F97316', // orange
  '#FB923C', // orange light
  '#F59E0B', // amber
  '#D97706', // amber deep
  '#10B981', // emerald
  '#84CC16', // lime
  '#EF4444', // red
  '#EC4899', // pink
]

export const TIME_RANGES = [
  { label: 'Last 5m', value: '5m', seconds: 300 },
  { label: 'Last 15m', value: '15m', seconds: 900 },
  { label: 'Last 1h', value: '1h', seconds: 3600 },
  { label: 'Last 6h', value: '6h', seconds: 21600 },
  { label: 'Last 24h', value: '24h', seconds: 86400 },
  { label: 'Last 7d', value: '7d', seconds: 604800 },
  { label: 'Last 30d', value: '30d', seconds: 2592000 },
]

export interface ExtendedPreset {
  label: string
  value: string
  group: 'recent' | 'named' | 'duration'
}

export const EXTENDED_PRESETS: ExtendedPreset[] = [
  { label: 'Last 5 minutes',   value: '5m',   group: 'recent' },
  { label: 'Last 15 minutes',  value: '15m',  group: 'recent' },
  { label: 'Last 30 minutes',  value: '30m',  group: 'recent' },
  { label: 'Last 1 hour',      value: '1h',   group: 'recent' },
  { label: 'Last 3 hours',     value: '3h',   group: 'recent' },
  { label: 'Last 6 hours',     value: '6h',   group: 'recent' },
  { label: 'Last 12 hours',    value: '12h',  group: 'recent' },
  { label: 'Last 24 hours',    value: '24h',  group: 'recent' },
  { label: 'Today',            value: 'preset:today',        group: 'named' },
  { label: 'Yesterday',        value: 'preset:yesterday',    group: 'named' },
  { label: 'This Week',        value: 'preset:this-week',    group: 'named' },
  { label: 'Last Week',        value: 'preset:last-week',    group: 'named' },
  { label: 'This Month',       value: 'preset:this-month',   group: 'named' },
  { label: 'Last Month',       value: 'preset:last-month',   group: 'named' },
  { label: 'Last 7 days',      value: '7d',   group: 'duration' },
  { label: 'Last 30 days',     value: '30d',  group: 'duration' },
  { label: 'Last 3 Months',    value: 'preset:last-3-months', group: 'named' },
  { label: 'Last 6 Months',    value: 'preset:last-6-months', group: 'named' },
]

const DATE_TYPES = new Set([
  'Date', 'Date32', 'DateTime', 'DateTime64',
  'Nullable(Date)', 'Nullable(Date32)', 'Nullable(DateTime)', 'Nullable(DateTime64)',
])

const NUMERIC_TYPES = new Set([
  'UInt8', 'UInt16', 'UInt32', 'UInt64', 'UInt128', 'UInt256',
  'Int8', 'Int16', 'Int32', 'Int64', 'Int128', 'Int256',
  'Float32', 'Float64', 'Decimal',
])

export function isDateType(chType: string): boolean {
  if (DATE_TYPES.has(chType)) return true
  return /^(Nullable\()?(Date|DateTime)/.test(chType)
}

export function isNumericType(chType: string): boolean {
  const base = chType.replace(/^Nullable\(/, '').replace(/\)$/, '')
  if (NUMERIC_TYPES.has(base)) return true
  return /^(U?Int|Float|Decimal)/.test(base)
}

/** True when x column is categorical (not date, not numeric — i.e. String). */
export function isCategoricalX(meta: ColumnMeta[], xColumn: string): boolean {
  const xMeta = meta.find(m => m.name === xColumn)
  if (!xMeta) return false
  return !isDateType(xMeta.type) && !isNumericType(xMeta.type)
}

/**
 * Transform dashboard API row-objects into uPlot's AlignedData format.
 * Returns [xValues[], y1Values[], y2Values[], ...]
 */
export function toUPlotData(
  data: Record<string, unknown>[],
  meta: ColumnMeta[],
  config: PanelConfig,
): uPlot.AlignedData {
  if (!data.length || !config.xColumn || !config.yColumns?.length) {
    return [new Float64Array(0)]
  }

  const xCol = config.xColumn
  const xMeta = meta.find(m => m.name === xCol)
  const isTime = xMeta ? isDateType(xMeta.type) : false

  const xArr = new Float64Array(data.length)
  for (let i = 0; i < data.length; i++) {
    const raw = data[i][xCol]
    if (isTime) {
      const ts = new Date(raw as string).getTime()
      xArr[i] = ts / 1000 // uPlot uses unix seconds
    } else {
      xArr[i] = Number(raw) || i
    }
  }

  const series: uPlot.AlignedData = [xArr]
  for (const yCol of config.yColumns) {
    const yArr = new Float64Array(data.length)
    for (let i = 0; i < data.length; i++) {
      yArr[i] = Number(data[i][yCol]) || 0
    }
    series.push(yArr)
  }

  return series
}

/** Extract single stat value from first row, first column */
export function getStatValue(data: Record<string, unknown>[], meta: ColumnMeta[]): string {
  if (data.length > 0 && meta.length > 0) {
    const key = meta[0].name
    const val = data[0][key] ?? data[0][Object.keys(data[0])[0]]
    if (val === null || val === undefined) return '--'
    const num = Number(val)
    if (!isNaN(num)) {
      return num.toLocaleString()
    }
    return String(val)
  }
  return '--'
}
