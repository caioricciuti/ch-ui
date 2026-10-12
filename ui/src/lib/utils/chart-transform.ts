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

const DATE_ONLY = /^(\d{4})-(\d{2})-(\d{2})$/
const NAIVE_DATETIME = /^(\d{4})-(\d{2})-(\d{2})[ T](\d{2}):(\d{2}):(\d{2})(?:\.(\d+))?$/

/**
 * Parse a ClickHouse date or time value into epoch milliseconds.
 *
 * Panel queries ask ClickHouse for date_time_output_format=iso, so DateTime
 * values arrive as UTC instants ("2026-01-01T10:00:00Z") and parse exactly.
 * Date values ("2026-01-01") have no time of day: they are placed at local
 * midnight, so a daily chart labels each point with its own calendar day in
 * the viewer's time zone. A value without a zone (a ClickHouse profile that
 * refuses the iso setting) is read as local time, the same way. Numbers are
 * epoch seconds, or milliseconds when too large to be seconds.
 */
export function parseTimeValue(raw: unknown): number | null {
  if (raw == null || raw === '') return null
  if (typeof raw === 'number') {
    if (!Number.isFinite(raw)) return null
    return Math.abs(raw) < 1e11 ? raw * 1000 : raw
  }
  const s = String(raw).trim()
  const d = DATE_ONLY.exec(s)
  if (d) return new Date(Number(d[1]), Number(d[2]) - 1, Number(d[3])).getTime()
  const n = NAIVE_DATETIME.exec(s)
  if (n) {
    const ms = n[7] ? Number(n[7].slice(0, 3).padEnd(3, '0')) : 0
    return new Date(Number(n[1]), Number(n[2]) - 1, Number(n[3]), Number(n[4]), Number(n[5]), Number(n[6]), ms).getTime()
  }
  const t = Date.parse(s)
  return Number.isNaN(t) ? null : t
}

/** A plotted number, or null for NULL, empty and non-numeric values, so the chart shows a gap instead of a fake zero. */
function toPlotNumber(raw: unknown): number | null {
  if (raw == null || raw === '' || typeof raw === 'boolean') return null
  const n = typeof raw === 'number' ? raw : Number(raw)
  return Number.isFinite(n) ? n : null
}

/** The x column is neither a date nor a number: points are placed by row index and labelled with the value. */
export function isCategoricalColumn(meta: ColumnMeta[], column: string | undefined): boolean {
  const xMeta = meta.find(m => m.name === column)
  if (!xMeta) return false
  return !isDateType(xMeta.type) && !isNumericType(xMeta.type)
}

/**
 * The x and y columns a chart plots. Saved columns are used when the result
 * still has them; otherwise x falls back to the first date column (else the
 * first column) and y to the numeric columns, as the panel editor picks them.
 * A panel saved without axes (from the API or an import) still draws.
 */
export function resolveChartColumns(
  meta: ColumnMeta[],
  config: Pick<PanelConfig, 'xColumn' | 'yColumns'>,
): { xColumn: string | undefined; yColumns: string[] } {
  const names = new Set(meta.map(m => m.name))
  const xColumn = config.xColumn && names.has(config.xColumn)
    ? config.xColumn
    : (meta.find(m => isDateType(m.type)) ?? meta[0])?.name
  let yColumns = (config.yColumns ?? []).filter(c => names.has(c) && c !== xColumn)
  if (yColumns.length === 0) {
    yColumns = meta.filter(m => m.name !== xColumn && isNumericType(m.type)).slice(0, 8).map(m => m.name)
  }
  return { xColumn, yColumns }
}

/**
 * Transform dashboard API row-objects into uPlot's AlignedData format.
 * Returns [xValues[], y1Values[], y2Values[], ...]. Time x values are epoch
 * seconds; rows whose time or numeric x is missing are left out. Missing y
 * values are null, which uPlot draws as a gap.
 */
export function toUPlotData(
  data: Record<string, unknown>[],
  meta: ColumnMeta[],
  config: PanelConfig,
): uPlot.AlignedData {
  const { xColumn, yColumns } = resolveChartColumns(meta, config)
  if (!data.length || !xColumn || !yColumns.length) {
    return [new Float64Array(0)]
  }

  const xCol = xColumn
  const xMeta = meta.find(m => m.name === xCol)
  const isTime = xMeta ? isDateType(xMeta.type) : false
  const categorical = isCategoricalColumn(meta, xCol)

  // Categorical rows keep their order (the axis labels follow it); time and
  // numeric rows are sorted, since uPlot needs ascending x values.
  const points: { x: number; row: Record<string, unknown> }[] = []
  for (let i = 0; i < data.length; i++) {
    const row = data[i]
    let x: number | null
    if (categorical) x = i
    else if (isTime) {
      const ms = parseTimeValue(row[xCol])
      x = ms == null ? null : ms / 1000
    } else x = toPlotNumber(row[xCol])
    if (x != null) points.push({ x, row })
  }
  if (!categorical) points.sort((a, b) => a.x - b.x)

  const xArr = new Float64Array(points.length)
  for (let i = 0; i < points.length; i++) xArr[i] = points[i].x

  const ys: (number | null)[][] = yColumns.map(yCol => points.map(p => toPlotNumber(p.row[yCol])))
  return [xArr, ...ys]
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

function formatDuration(seconds: number, decimals?: number): string {
  const abs = Math.abs(seconds)
  if (abs < 0.001) return formatWithDecimals(seconds * 1_000_000, decimals ?? 0) + ' µs'
  if (abs < 1) return formatWithDecimals(seconds * 1000, decimals ?? 0) + ' ms'
  if (abs < 60) return formatWithDecimals(seconds, decimals ?? 1) + ' s'
  if (abs < 3600) return formatWithDecimals(seconds / 60, decimals ?? 1) + ' min'
  if (abs < 86400) return formatWithDecimals(seconds / 3600, decimals ?? 1) + ' h'
  return formatWithDecimals(seconds / 86400, decimals ?? 1) + ' d'
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
