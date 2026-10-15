import type { DashboardVariable, DashboardVariableOption } from '../types/api'

/** Sentinel value for "All"; same as VARIABLE_ALL in api/dashboards. */
const ALL = '$__all'

/** Current values of the dashboard variables, by name. */
export type VariableValues = Record<string, string[]>

/** Loads the options of one variable given the current values of the others. */
export type VariableOptionsLoader = (
  name: string,
  values: VariableValues,
) => Promise<{ options: DashboardVariableOption[]; truncated: boolean }>

const PARAM_PREFIX = 'var-'

/** Values in ?var-name=v1&var-name=v2 (Grafana style; repeat for several values). */
export function readVariablesFromSearch(params: URLSearchParams): VariableValues {
  const out: VariableValues = {}
  for (const [key, value] of params) {
    if (!key.startsWith(PARAM_PREFIX)) continue
    const name = key.slice(PARAM_PREFIX.length)
    if (!name) continue
    ;(out[name] ??= []).push(value)
  }
  return out
}

/**
 * Replace every var-* parameter with the given values. Constants are left
 * out: the server always uses their default. Other parameters are kept.
 */
export function writeVariablesToSearch(params: URLSearchParams, variables: DashboardVariable[], values: VariableValues): void {
  for (const key of [...params.keys()]) {
    if (key.startsWith(PARAM_PREFIX)) params.delete(key)
  }
  for (const v of variables) {
    if (v.type === 'constant') continue
    for (const value of values[v.name] ?? []) params.append(PARAM_PREFIX + v.name, value)
  }
}

export function readVariablesFromUrl(): VariableValues {
  return readVariablesFromSearch(new URLSearchParams(window.location.search))
}

/** The values in the address bar, so a copied link opens on the same selection. */
export function writeVariablesToUrl(variables: DashboardVariable[], values: VariableValues): void {
  const url = new URL(window.location.href)
  writeVariablesToSearch(url.searchParams, variables, values)
  history.replaceState(history.state, '', url)
}

/**
 * Keep only values the variables can hold: known, non-constant names, one
 * value unless multi, All only with include_all.
 */
export function reconcileValues(variables: DashboardVariable[], values: VariableValues): VariableValues {
  const out: VariableValues = {}
  for (const v of variables) {
    if (v.type === 'constant') continue
    let list = values[v.name]
    if (!list || list.length === 0) continue
    if (list.includes(ALL)) {
      if (!v.include_all || (v.type !== 'query' && v.type !== 'custom')) continue
      list = [ALL]
    }
    out[v.name] = v.multi ? [...new Set(list)] : list.slice(0, 1)
  }
  return out
}

/**
 * Values on first load: the address bar wins, then each default. Variables
 * without either stay unset; the server and the bar both fall back to the
 * first option (or All for a multi variable with include_all).
 */
export function initialVariableValues(variables: DashboardVariable[], fromUrl: VariableValues): VariableValues {
  const merged: VariableValues = {}
  for (const v of variables) {
    const url = fromUrl[v.name]
    if (url && url.length > 0) merged[v.name] = url
    else if (v.default && v.default.length > 0) merged[v.name] = [...v.default]
  }
  return reconcileValues(variables, merged)
}

/** "a, b, c" into ["a", "b", "c"], trimmed, without blanks or repeats. */
export function parseOptionList(text: string | undefined): string[] {
  const out: string[] = []
  for (const part of (text ?? '').split(',')) {
    const p = part.trim()
    if (p && !out.includes(p)) out.push(p)
  }
  return out
}

/** Options known without asking the server: custom and interval lists. */
export function staticOptions(v: DashboardVariable): DashboardVariableOption[] | null {
  if (v.type !== 'custom' && v.type !== 'interval') return null
  return parseOptionList(v.options).map((value) => ({ value, label: value }))
}

/** What the server uses for a variable that has no value: mirrors its resolver. */
export function fallbackValue(v: DashboardVariable, options: DashboardVariableOption[]): string[] {
  if (v.default && v.default.length > 0) return v.multi ? [...v.default] : v.default.slice(0, 1)
  if (v.include_all && v.multi) return [ALL]
  return options.length > 0 ? [options[0].value] : []
}

const INSTANT = /^(\d{4}-\d{2}-\d{2})T(\d{2}:\d{2}:\d{2})(?:\.(\d+))?(Z|[+-]\d{2}:?\d{2})$/i

/**
 * An ISO instant as a key to compare by: UTC, fractional digits without
 * trailing zeros (DateTime64 keeps up to nine). Null for anything else.
 */
function instantKey(s: string): string | null {
  const m = INSTANT.exec(s)
  if (!m) return null
  const frac = (m[3] ?? '').replace(/0+$/, '')
  const tail = (frac ? '.' + frac : '') + 'Z'
  if (m[4].toUpperCase() === 'Z') return `${m[1]}T${m[2]}${tail}`
  const whole = Date.parse(`${m[1]}T${m[2]}${m[4]}`)
  return Number.isNaN(whole) ? null : new Date(whole).toISOString().slice(0, 19) + tail
}

/** Whether a value is an ISO instant: a date and time clicked in a panel. */
export function isInstant(value: string): boolean {
  return instantKey(value) != null
}

/**
 * The option a value stands for: the value itself, or the option of a
 * DateTime variable whose instant it is (panels return a DateTime as an ISO
 * instant, its options are ClickHouse text). Null when it is neither.
 */
export function canonicalValue(value: string, options: DashboardVariableOption[]): string | null {
  if (options.some((o) => o.value === value)) return value
  const key = instantKey(value)
  if (key == null) return null
  return options.find((o) => o.instant != null && instantKey(o.instant) === key)?.value ?? null
}

/**
 * The selection to keep once the options of a variable are known: the
 * current values that are still options (an instant as its option), else
 * the fallback. A truncated or
 * empty list cannot prove a value wrong, so the values are kept then.
 */
export function resolveSelection(
  v: DashboardVariable,
  current: string[] | undefined,
  options: DashboardVariableOption[],
  truncated = false,
): string[] {
  if (current && current.length > 0) {
    if (current.includes(ALL) && v.include_all) return [ALL]
    if (truncated || options.length === 0) return current
    const kept = [...new Set(current.map((c) => canonicalValue(c, options)).filter((c) => c != null))]
    if (kept.length > 0) return v.multi ? kept : kept.slice(0, 1)
  }
  return fallbackValue(v, options)
}

/** Whether a variable query refers to $name or ${name} / ${name:format}. */
export function referencesVariable(query: string, name: string): boolean {
  const n = name.replace(/[^A-Za-z0-9_]/g, '\\$&')
  return new RegExp(`\\$(?:\\{${n}(?::[^}]*)?\\}|${n}(?![A-Za-z0-9_]))`).test(query)
}

/**
 * Whether variable v may change its options when one of the named variables
 * changes. A query variable without its query (share links strip it) is
 * assumed to depend on everything before it.
 */
export function dependsOn(v: DashboardVariable, changed: Iterable<string>): boolean {
  if (v.type !== 'query') return false
  if (!v.query) return true
  for (const name of changed) {
    if (referencesVariable(v.query, name)) return true
  }
  return false
}

export function sameValues(a: string[] | undefined, b: string[] | undefined): boolean {
  const x = a ?? []
  const y = b ?? []
  return x.length === y.length && x.every((v, i) => v === y[i])
}

export function variableLabel(v: DashboardVariable): string {
  return v.label?.trim() || v.name
}

/** Name rule of the server: letters, digits and _, not a leading digit or __. */
export function isValidVariableName(name: string): boolean {
  return /^[A-Za-z_][A-Za-z0-9_]{0,63}$/.test(name) && !name.startsWith('__')
}
