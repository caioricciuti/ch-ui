import type { PanelConfig, StatThreshold } from '../types/api'
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

  // uPlot requires x-axis values to be monotonically ascending
  const sortedData = [...data].sort((a, b) => {
    if (isTime) {
      return new Date(a[xCol] as string).getTime() - new Date(b[xCol] as string).getTime()
    }
    return (Number(a[xCol]) || 0) - (Number(b[xCol]) || 0)
  })

  const xArr = new Float64Array(sortedData.length)
  for (let i = 0; i < sortedData.length; i++) {
    const raw = sortedData[i][xCol]
    if (isTime) {
      xArr[i] = new Date(raw as string).getTime() / 1000
    } else {
      xArr[i] = Number(raw) || i
    }
  }

  const series: uPlot.AlignedData = [xArr]
  for (const yCol of config.yColumns) {
    const yArr = new Float64Array(sortedData.length)
    for (let i = 0; i < sortedData.length; i++) {
      yArr[i] = Number(sortedData[i][yCol]) || 0
    }
    series.push(yArr)
  }

  return series
}

/** Extract single stat value from first row, first column (legacy, kept for backward compat) */
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

// ---------------------------------------------------------------------------
// Stat panel: computation, formatting, thresholds
// ---------------------------------------------------------------------------

type StatCalc = NonNullable<PanelConfig['statCalculation']>

function extractNumbers(data: Record<string, unknown>[], field: string): number[] {
  const nums: number[] = []
  for (const row of data) {
    const n = Number(row[field])
    if (!isNaN(n)) nums.push(n)
  }
  return nums
}

function pickField(meta: ColumnMeta[], statField?: string): string {
  if (statField && meta.some(m => m.name === statField)) return statField
  const numeric = meta.find(m => isNumericType(m.type))
  return numeric?.name ?? meta[0]?.name ?? ''
}

function reduce(nums: number[], calc: StatCalc): number | null {
  if (nums.length === 0) return null
  switch (calc) {
    case 'last':  return nums[nums.length - 1]
    case 'first': return nums[0]
    case 'mean':  return nums.reduce((a, b) => a + b, 0) / nums.length
    case 'sum':   return nums.reduce((a, b) => a + b, 0)
    case 'min':   return Math.min(...nums)
    case 'max':   return Math.max(...nums)
    case 'count': return nums.length
    case 'range': return Math.max(...nums) - Math.min(...nums)
    default:      return nums[nums.length - 1]
  }
}

function formatWithDecimals(n: number, decimals?: number): string {
  if (decimals !== undefined && decimals >= 0) return n.toFixed(decimals)
  if (Number.isInteger(n)) return n.toLocaleString()
  const abs = Math.abs(n)
  if (abs >= 1000) return n.toLocaleString(undefined, { maximumFractionDigits: 1 })
  if (abs >= 1) return n.toLocaleString(undefined, { maximumFractionDigits: 2 })
  return n.toLocaleString(undefined, { maximumFractionDigits: 4 })
}

const SI_SUFFIXES = ['', 'K', 'M', 'B', 'T']
const BYTE_SUFFIXES = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']

function formatShort(n: number, decimals?: number): string {
  const abs = Math.abs(n)
  if (abs < 1000) return formatWithDecimals(n, decimals)
  let tier = Math.floor(Math.log10(abs) / 3)
  if (tier >= SI_SUFFIXES.length) tier = SI_SUFFIXES.length - 1
  const scaled = n / Math.pow(1000, tier)
  return formatWithDecimals(scaled, decimals ?? 1) + SI_SUFFIXES[tier]
}

function formatBytes(n: number, decimals?: number): string {
  if (n === 0) return '0 B'
  const abs = Math.abs(n)
  let tier = Math.floor(Math.log2(abs) / 10)
  if (tier >= BYTE_SUFFIXES.length) tier = BYTE_SUFFIXES.length - 1
  const scaled = n / Math.pow(1024, tier)
  return formatWithDecimals(scaled, decimals ?? 1) + ' ' + BYTE_SUFFIXES[tier]
}

function formatBps(n: number, decimals?: number): string {
  return formatBytes(n, decimals) + '/s'
}

function formatDuration(seconds: number, decimals?: number): string {
  const abs = Math.abs(seconds)
  if (abs < 0.001) return formatWithDecimals(seconds * 1_000_000, decimals ?? 0) + ' µs'
  if (abs < 1) return formatWithDecimals(seconds * 1000, decimals ?? 0) + ' ms'
  if (abs < 60) return formatWithDecimals(seconds, decimals ?? 1) + ' s'
  if (abs < 3600) return formatWithDecimals(seconds / 60, decimals ?? 1) + ' min'
  if (abs < 86400) return formatWithDecimals(seconds / 3600, decimals ?? 1) + ' h'
  return formatWithDecimals(seconds / 86400, decimals ?? 1) + ' d'
}

function formatDurationMs(ms: number, decimals?: number): string {
  return formatDuration(ms / 1000, decimals)
}

function formatUnit(n: number, unit: PanelConfig['statUnit'], decimals?: number): string {
  switch (unit) {
    case 'percent':    return formatWithDecimals(n, decimals ?? 1) + '%'
    case 'short':      return formatShort(n, decimals)
    case 'bytes':      return formatBytes(n, decimals)
    case 'bps':        return formatBps(n, decimals)
    case 'duration':   return formatDuration(n, decimals)
    case 'durationMs': return formatDurationMs(n, decimals)
    case 'none':
    default:           return formatWithDecimals(n, decimals)
  }
}

const DEFAULT_THRESHOLDS: StatThreshold[] = [
  { value: -Infinity, color: '#22c55e' },
  { value: 80, color: '#f59e0b' },
  { value: 90, color: '#ef4444' },
]

export function resolveThresholdColor(value: number, thresholds?: StatThreshold[]): string {
  const t = thresholds && thresholds.length > 0 ? thresholds : DEFAULT_THRESHOLDS
  const sorted = [...t].sort((a, b) => a.value - b.value)
  let color = sorted[0].color
  for (const s of sorted) {
    if (value >= s.value) color = s.color
    else break
  }
  return color
}

export interface ComputedStat {
  raw: number | null
  value: string
  prefix: string
  suffix: string
  color: string
  colorMode: PanelConfig['statColorMode']
  field: string
}

function buildUnitParts(n: number, unit: PanelConfig['statUnit'], decimals?: number): { value: string; unitSuffix: string } {
  switch (unit) {
    case 'percent':    return { value: formatWithDecimals(n, decimals ?? 1), unitSuffix: '%' }
    case 'short': {
      const abs = Math.abs(n)
      if (abs < 1000) return { value: formatWithDecimals(n, decimals), unitSuffix: '' }
      let tier = Math.floor(Math.log10(abs) / 3)
      if (tier >= SI_SUFFIXES.length) tier = SI_SUFFIXES.length - 1
      const scaled = n / Math.pow(1000, tier)
      return { value: formatWithDecimals(scaled, decimals ?? 1), unitSuffix: SI_SUFFIXES[tier] }
    }
    case 'bytes': {
      if (n === 0) return { value: '0', unitSuffix: 'B' }
      const abs = Math.abs(n)
      let tier = Math.floor(Math.log2(abs) / 10)
      if (tier >= BYTE_SUFFIXES.length) tier = BYTE_SUFFIXES.length - 1
      const scaled = n / Math.pow(1024, tier)
      return { value: formatWithDecimals(scaled, decimals ?? 1), unitSuffix: BYTE_SUFFIXES[tier] }
    }
    case 'bps': {
      if (n === 0) return { value: '0', unitSuffix: 'B/s' }
      const abs = Math.abs(n)
      let tier = Math.floor(Math.log2(abs) / 10)
      if (tier >= BYTE_SUFFIXES.length) tier = BYTE_SUFFIXES.length - 1
      const scaled = n / Math.pow(1024, tier)
      return { value: formatWithDecimals(scaled, decimals ?? 1), unitSuffix: BYTE_SUFFIXES[tier] + '/s' }
    }
    case 'duration':   return { value: formatDuration(n, decimals).replace(/\s\S+$/, ''), unitSuffix: formatDuration(n, decimals).replace(/^[\d.,\-]+\s*/, '') }
    case 'durationMs': {
      const s = n / 1000
      return { value: formatDuration(s, decimals).replace(/\s\S+$/, ''), unitSuffix: formatDuration(s, decimals).replace(/^[\d.,\-]+\s*/, '') }
    }
    case 'none':
    default:           return { value: formatWithDecimals(n, decimals), unitSuffix: '' }
  }
}

export function computeStat(
  data: Record<string, unknown>[],
  meta: ColumnMeta[],
  config: Partial<PanelConfig>,
): ComputedStat {
  const field = pickField(meta, config.statField)
  const calc = config.statCalculation ?? 'last'
  const unit = config.statUnit ?? 'none'
  const colorMode = config.statColorMode ?? 'none'

  const empty: ComputedStat = { raw: null, value: '--', prefix: '', suffix: '', color: '#6b7280', colorMode, field }

  if (!field || data.length === 0) return empty

  const nums = extractNumbers(data, field)

  if (nums.length === 0) {
    const val = data[data.length - 1][field]
    return { ...empty, value: val != null ? String(val) : '--' }
  }

  const raw = reduce(nums, calc)
  if (raw === null) return empty

  const parts = buildUnitParts(raw, unit, config.statDecimals)
  const prefix = config.statPrefix ?? ''
  const suffix = (parts.unitSuffix || '') + (config.statSuffix ?? '')

  const color = colorMode !== 'none'
    ? resolveThresholdColor(raw, config.statThresholds)
    : '#6b7280'

  return { raw, value: parts.value, prefix, suffix, color, colorMode, field }
}
