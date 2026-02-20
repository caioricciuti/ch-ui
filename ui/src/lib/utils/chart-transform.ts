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
