import type { DashboardVariable, PanelLink } from '../types/api'
import { isDateType, parseTimeValue, type ColumnMeta } from './chart-transform'
import type { VariableValues } from './dashboard-variables'

/** What was clicked on a panel: the value, its column, its series and its row. */
export interface ClickContext {
  /** The clicked value as the result holds it. */
  value: unknown
  /** Column of the value. */
  field: string
  /** Series label of the clicked point; empty when the panel has none. */
  series: string
  /** The clicked row, by column name. */
  row: Record<string, unknown>
  /** The value as epoch milliseconds when its column holds times, else null. */
  timeMs: number | null
  /** Time window around the clicked point (epoch ms): the range a dashboard link opens on. */
  window: { from: number; to: number } | null
}

/** A click on a panel with links: the context and where on screen it happened. */
export interface PanelClick {
  ctx: ClickContext
  x: number
  y: number
}

/**
 * The context of a click. A time column's value becomes an instant (an ISO
 * string in ${__value}); window is the bucket around it, when the panel
 * knows one.
 */
export function buildClickContext(
  row: Record<string, unknown>,
  field: string,
  series: string,
  columns: ColumnMeta[],
  window: { from: number; to: number } | null = null,
): ClickContext {
  const value = row[field]
  const type = columns.find(c => c.name === field)?.type
  const timeMs = type && isDateType(type) ? parseTimeValue(value) : null
  return { value, field, series, row, timeMs, window }
}

/**
 * How an interpolated value is written:
 * url: each value percent-encoded (encodeURIComponent).
 * sql: a ClickHouse literal; finite numbers bare, everything else a quoted
 *   string (the server's QuoteString), null as NULL.
 * text: as is, for values that are encoded later (URL parameters, variables).
 */
export type LinkEncoder = 'url' | 'sql' | 'text'

/** ClickHouse string literal, same as the server's QuoteString. */
export function quoteSqlString(s: string): string {
  return "'" + s.replace(/\\/g, '\\\\').replace(/'/g, "''") + "'"
}

function plainText(v: unknown): string {
  if (v == null) return ''
  if (typeof v === 'object') return JSON.stringify(v)
  return String(v)
}

function encodeOne(v: unknown, encoder: LinkEncoder): string {
  if (encoder === 'sql') {
    if (v == null) return 'NULL'
    if (typeof v === 'number' && Number.isFinite(v)) return String(v)
    return quoteSqlString(plainText(v))
  }
  const text = plainText(v)
  return encoder === 'url' ? encodeURIComponent(text) : text
}

/** The clicked value as written into a link: a time as an ISO instant. */
function clickedValue(ctx: ClickContext): unknown {
  return ctx.timeMs != null ? new Date(ctx.timeMs).toISOString() : ctx.value
}

const TOKEN = /\$\{([^{}]+)\}/g

/**
 * Fill the ${...} tokens of a template: ${__value}, ${__field}, ${__series},
 * ${__row.<column>}, ${<variable>} (several values joined by commas) and
 * ${__value:raw} (sql only: the value unquoted; anywhere else it is encoded
 * like ${__value}). Unknown tokens are left as they are.
 */
export function interpolate(
  template: string,
  ctx: ClickContext,
  encoder: LinkEncoder,
  variables: VariableValues = {},
): string {
  return template.replace(TOKEN, (whole, name: string) => {
    if (name === '__value') return encodeOne(clickedValue(ctx), encoder)
    if (name === '__value:raw') {
      const v = clickedValue(ctx)
      return encoder === 'sql' ? plainText(v) : encodeOne(v, encoder)
    }
    if (name === '__field') return encodeOne(ctx.field, encoder)
    if (name === '__series') return encodeOne(ctx.series, encoder)
    if (name.startsWith('__row.')) {
      const col = name.slice('__row.'.length)
      return Object.hasOwn(ctx.row, col) ? encodeOne(ctx.row[col], encoder) : whole
    }
    const values = Object.hasOwn(variables, name) ? variables[name] : undefined
    if (values) return values.map(v => encodeOne(v, encoder)).join(',')
    return whole
  })
}

/**
 * An http(s) URL, parsed and normalized, or null for anything else:
 * javascript:, data:, vbscript:, relative and protocol-relative URLs.
 */
export function safeUrl(text: string): string | null {
  let url: URL
  try {
    url = new URL(text)
  } catch {
    return null
  }
  if (url.protocol !== 'http:' && url.protocol !== 'https:') return null
  return url.href
}

/** What a link does once its templates are filled. */
export type LinkAction =
  | { kind: 'setVariable'; variable: string; value: string }
  | { kind: 'dashboard'; dashboardId: string; search: string }
  | { kind: 'sql'; sql: string }
  | { kind: 'url'; url: string }
  | { kind: 'blocked'; reason: string }

export interface LinkEnvironment {
  /** Current variable values of the dashboard. */
  variables: VariableValues
  /** Current range as URL parameters, for a dashboard link from a click without a time. */
  range?: { from: string; to: string }
}

/** Default label of a link in the click menu. */
export function linkLabel(link: PanelLink, dashboardName?: string): string {
  if (link.label?.trim()) return link.label.trim()
  switch (link.kind) {
    case 'setVariable': return `Filter by ${link.variable ?? 'value'}`
    case 'dashboard': return `Open ${dashboardName ?? 'dashboard'}`
    case 'sql': return 'Open in SQL editor'
    case 'url': return 'Open link'
  }
}

/** Fill a link's templates for one click. */
export function resolveLink(link: PanelLink, ctx: ClickContext, env: LinkEnvironment): LinkAction {
  switch (link.kind) {
    case 'setVariable': {
      if (!link.variable) return { kind: 'blocked', reason: 'This link has no variable' }
      const value = link.template ? interpolate(link.template, ctx, 'text', env.variables) : plainText(clickedValue(ctx))
      return { kind: 'setVariable', variable: link.variable, value }
    }
    case 'dashboard': {
      // The id goes into the app's URL path: only the shape of a dashboard id.
      if (!link.dashboardId || !/^[A-Za-z0-9_-]{1,100}$/.test(link.dashboardId)) {
        return { kind: 'blocked', reason: 'This link has no dashboard' }
      }
      const params = new URLSearchParams()
      const vars: VariableValues = {}
      if (link.carryVariables) Object.assign(vars, env.variables)
      for (const [name, tpl] of Object.entries(link.params ?? {})) {
        vars[name] = [interpolate(tpl, ctx, 'text', env.variables)]
      }
      for (const [name, values] of Object.entries(vars)) {
        for (const v of values) params.append('var-' + name, v)
      }
      if (ctx.window) {
        params.set('from', String(Math.floor(ctx.window.from)))
        params.set('to', String(Math.ceil(ctx.window.to)))
      } else if (env.range) {
        params.set('from', env.range.from)
        params.set('to', env.range.to)
      }
      const search = params.toString()
      return { kind: 'dashboard', dashboardId: link.dashboardId, search: search ? '?' + search : '' }
    }
    case 'sql':
      return { kind: 'sql', sql: interpolate(link.template ?? '', ctx, 'sql', env.variables) }
    case 'url': {
      const url = safeUrl(interpolate(link.template ?? '', ctx, 'url', env.variables))
      return url ? { kind: 'url', url } : { kind: 'blocked', reason: 'Only http and https links can open' }
    }
  }
}

/**
 * Why a link will not work on a share link, or null when it will. Share
 * links run SQL and other dashboards only for signed in users, and accept
 * only option values for variables (text variables stay at their default).
 */
export function shareLinkIssue(link: PanelLink, variables: DashboardVariable[]): string | null {
  if (link.kind === 'sql' || link.kind === 'dashboard') return 'Not available on share links'
  if (link.kind === 'setVariable') {
    const v = variables.find(x => x.name === link.variable)
    if (v && (v.type === 'text' || v.type === 'constant')) return 'Not available on share links'
  }
  return null
}

/** The window of a time bucket that starts at `at` (epoch seconds) and lasts `step` seconds. */
export function bucketWindow(at: number, step: number): { from: number; to: number } {
  const span = Number.isFinite(step) && step > 0 ? step : 60
  return { from: at * 1000, to: (at + span) * 1000 }
}

/** The smallest gap between neighbouring x values around index i: the bucket size. */
export function bucketStep(xs: ArrayLike<number>, i: number): number {
  let step = Infinity
  if (i > 0) step = Math.min(step, xs[i] - xs[i - 1])
  if (i + 1 < xs.length) step = Math.min(step, xs[i + 1] - xs[i])
  return Number.isFinite(step) && step > 0 ? step : 0
}
