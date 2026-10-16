// Transform (processor) nodes: the steps between a pipeline's source and sink.
// Config shapes follow the processors contract; the server validates again on save.

export type TransformNodeType =
  | 'transform_parse'
  | 'transform_map'
  | 'transform_filter'
  | 'transform_sample'
  | 'transform_redact'
  | 'transform_route'

export const TRANSFORM_NODE_TYPES: { type: TransformNodeType; label: string; description: string; pro: boolean }[] = [
  { type: 'transform_parse', label: 'Parse', description: 'JSON, logfmt, regex, access logs, syslog', pro: false },
  { type: 'transform_map', label: 'Map fields', description: 'Rename, copy, set, drop fields', pro: false },
  { type: 'transform_filter', label: 'Filter', description: 'Keep or drop records by rules', pro: false },
  { type: 'transform_sample', label: 'Sample', description: 'Keep a share of records', pro: true },
  { type: 'transform_redact', label: 'Redact', description: 'Mask emails, IPs, tokens, cards', pro: true },
  { type: 'transform_route', label: 'Route', description: 'Send records to several sinks', pro: true },
]

/** Most transform nodes one pipeline may have, the Route node not counted. */
export const MAX_TRANSFORMS = 20

export function isTransformType(t: unknown): t is TransformNodeType {
  return TRANSFORM_NODE_TYPES.some((m) => m.type === t)
}

export function isProTransform(t: unknown): boolean {
  return TRANSFORM_NODE_TYPES.some((m) => m.type === t && m.pro)
}

/** The server's message when a Pro transform runs without Pro. */
export function proTransformMessage(t: TransformNodeType): string {
  switch (t) {
    case 'transform_sample': return 'Sampling needs CH-UI Pro'
    case 'transform_redact': return 'Redaction needs CH-UI Pro'
    case 'transform_route': return 'Routing needs CH-UI Pro'
    default: return ''
  }
}

export function transformLabel(t: TransformNodeType): string {
  return TRANSFORM_NODE_TYPES.find((m) => m.type === t)?.label ?? t
}

// ---- Config shapes ----

export type ParseFormat = 'json' | 'logfmt' | 'regex' | 'nginx' | 'apache' | 'syslog'
export type OnError = 'keep' | 'drop'

export interface ParseConfig {
  field: string
  format: ParseFormat
  pattern: string
  target: string
  prefix: string
  on_error: OnError
  remove_source: boolean
}

export interface FieldPair {
  from: string
  to: string
}

export interface SetField {
  field: string
  value: string
}

export type TimestampFormat = 'rfc3339' | 'unix' | 'unix_ms' | 'unix_ns' | string

export interface MapConfig {
  rename: FieldPair[]
  copy: FieldPair[]
  set: SetField[]
  drop: string[]
  severity_from: string
  timestamp_from: string
  timestamp_format: TimestampFormat
}

export type RuleOp =
  | 'eq' | 'ne' | 'contains' | 'not_contains' | 'matches'
  | 'gt' | 'gte' | 'lt' | 'lte' | 'exists' | 'missing' | 'in'

export interface Rule {
  field: string
  op: RuleOp
  /** The typed text. For `in` it is a JSON array, e.g. ["a","b"]. */
  value: string
}

export type MatchMode = 'all' | 'any'

export interface FilterConfig {
  mode: 'keep' | 'drop'
  match: MatchMode
  rules: Rule[]
}

export interface SampleConfig {
  keep_percent: number
  always_keep: { match: MatchMode; rules: Rule[] }
  key: string
}

export type RedactKind = 'email' | 'ipv4' | 'ipv6' | 'credit_card' | 'jwt' | 'bearer_token' | 'aws_key' | 'custom'

export interface CustomPattern {
  name: string
  pattern: string
}

export interface RedactConfig {
  kinds: RedactKind[]
  custom: CustomPattern[]
  fields: string[]
  replacement: string
}

export type RouteMode = 'first' | 'all'

export interface RouteDef {
  /** Stable id, the edge's source handle. */
  id: string
  name: string
  match: MatchMode
  rules: Rule[]
}

export interface RouteConfig {
  mode: RouteMode
  routes: RouteDef[]
  /** Records matching no route: dropped, or sent on through the unmatched output. */
  unmatched: 'drop' | 'output'
}

/** Source handle of the route node's unmatched output. */
export const UNMATCHED_HANDLE = 'unmatched'
export const ROUTE_MAX = 16
export const ROUTE_NAME_MAX = 100
export const ROUTE_ID_PATTERN = /^[a-z0-9_-]{1,32}$/

/** The first free id of the form r1, r2, ... */
export function nextRouteId(routes: { id: string }[]): string {
  const used = new Set(routes.map((r) => r.id))
  let n = 1
  while (used.has(`r${n}`) || `r${n}` === UNMATCHED_HANDLE) n++
  return `r${n}`
}

/** The outputs of a route node, in handle order: one per route, then unmatched when it is an output. */
export function routeOutputs(c: RouteConfig): { handle: string; label: string }[] {
  const out = c.routes.map((r) => ({ handle: r.id, label: r.name.trim() || r.id }))
  if (c.unmatched === 'output') out.push({ handle: UNMATCHED_HANDLE, label: 'unmatched' })
  return out
}

// ---- Option lists for the panels ----

export const PARSE_FORMATS: { value: ParseFormat; label: string; hint: string }[] = [
  { value: 'json', label: 'JSON', hint: 'The field holds a JSON object; its keys become fields.' },
  { value: 'logfmt', label: 'logfmt', hint: 'key=value pairs, e.g. level=info msg="started" took=12ms.' },
  { value: 'regex', label: 'Regex (named groups)', hint: 'Each named group becomes a field. Go RE2 syntax.' },
  { value: 'nginx', label: 'nginx combined', hint: 'nginx combined access log: remote_addr, request, status, body_bytes_sent, referer, user_agent.' },
  { value: 'apache', label: 'Apache combined', hint: 'Apache combined access log, same fields as nginx combined.' },
  { value: 'syslog', label: 'Syslog', hint: 'RFC 5424 or RFC 3164 lines: priority, timestamp, hostname, app name, message.' },
]

/** Starting regex per preset, so the regex format opens with a working example. */
export const REGEX_EXAMPLE = '(?P<ip>\\S+) \\S+ \\S+ \\[(?P<time>[^\\]]+)\\] "(?P<method>\\S+) (?P<path>\\S+)[^"]*" (?P<status>\\d{3})'

export const RULE_OPS: { value: RuleOp; label: string; needsValue: boolean }[] = [
  { value: 'eq', label: 'equals', needsValue: true },
  { value: 'ne', label: 'not equals', needsValue: true },
  { value: 'contains', label: 'contains', needsValue: true },
  { value: 'not_contains', label: 'does not contain', needsValue: true },
  { value: 'matches', label: 'matches regex', needsValue: true },
  { value: 'gt', label: '>', needsValue: true },
  { value: 'gte', label: '>=', needsValue: true },
  { value: 'lt', label: '<', needsValue: true },
  { value: 'lte', label: '<=', needsValue: true },
  { value: 'in', label: 'in list', needsValue: true },
  { value: 'exists', label: 'exists', needsValue: false },
  { value: 'missing', label: 'is missing', needsValue: false },
]

export function opNeedsValue(op: RuleOp): boolean {
  return RULE_OPS.find((o) => o.value === op)?.needsValue ?? true
}

export const TIMESTAMP_FORMATS: { value: string; label: string }[] = [
  { value: 'rfc3339', label: 'RFC 3339 (2026-10-16T09:30:00Z)' },
  { value: 'unix', label: 'Unix seconds' },
  { value: 'unix_ms', label: 'Unix milliseconds' },
  { value: 'unix_ns', label: 'Unix nanoseconds' },
  { value: 'layout', label: 'Go layout' },
]

export const REDACT_KINDS: { value: Exclude<RedactKind, 'custom'>; label: string }[] = [
  { value: 'email', label: 'Email addresses' },
  { value: 'ipv4', label: 'IPv4 addresses' },
  { value: 'ipv6', label: 'IPv6 addresses' },
  { value: 'credit_card', label: 'Credit card numbers (Luhn checked)' },
  { value: 'jwt', label: 'JWTs' },
  { value: 'bearer_token', label: 'Bearer tokens' },
  { value: 'aws_key', label: 'AWS access keys' },
]

export const DEFAULT_REPLACEMENT = '[REDACTED:{kind}]'

// ---- Defaults ----

/** OTel logs rows keep the raw line in Body and extra fields in LogAttributes. */
function isOtelLogs(signal: string | null | undefined): boolean {
  return signal === 'logs'
}

export function defaultTransformConfig(type: TransformNodeType, signal?: string | null): Record<string, unknown> {
  switch (type) {
    case 'transform_parse': {
      const c: ParseConfig = {
        field: 'Body',
        format: 'json',
        pattern: '',
        target: isOtelLogs(signal) ? 'LogAttributes' : '',
        prefix: '',
        on_error: 'keep',
        remove_source: false,
      }
      return { ...c }
    }
    case 'transform_map': {
      const c: MapConfig = { rename: [], copy: [], set: [], drop: [], severity_from: '', timestamp_from: '', timestamp_format: 'rfc3339' }
      return { ...c }
    }
    case 'transform_filter': {
      const c: FilterConfig = { mode: 'keep', match: 'all', rules: isOtelLogs(signal) ? [{ field: 'SeverityText', op: 'ne', value: 'DEBUG' }] : [] }
      return { ...c }
    }
    case 'transform_sample': {
      const c: SampleConfig = {
        keep_percent: 10,
        always_keep: { match: 'any', rules: isOtelLogs(signal) ? [{ field: 'SeverityText', op: 'eq', value: 'ERROR' }] : [] },
        key: signal === 'traces' || isOtelLogs(signal) ? 'TraceId' : '',
      }
      return { ...c }
    }
    case 'transform_redact': {
      const c: RedactConfig = { kinds: ['email', 'credit_card', 'jwt', 'bearer_token', 'aws_key'], custom: [], fields: [], replacement: DEFAULT_REPLACEMENT }
      return { ...c }
    }
    case 'transform_route': {
      const c: RouteConfig = {
        mode: 'first',
        routes: [{ id: 'r1', name: 'errors', match: 'any', rules: isOtelLogs(signal) ? [{ field: 'SeverityText', op: 'eq', value: 'ERROR' }] : [] }],
        unmatched: 'output',
      }
      return { ...c }
    }
  }
}

// ---- Reading stored configs (tolerant of missing or odd values) ----

function str(v: unknown, fallback = ''): string {
  return typeof v === 'string' ? v : fallback
}

function oneOf<T extends string>(v: unknown, allowed: readonly T[], fallback: T): T {
  return typeof v === 'string' && (allowed as readonly string[]).includes(v) ? (v as T) : fallback
}

function records(v: unknown): Record<string, unknown>[] {
  return Array.isArray(v) ? v.filter((x): x is Record<string, unknown> => !!x && typeof x === 'object' && !Array.isArray(x)) : []
}

function strings(v: unknown): string[] {
  return Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : []
}

const PARSE_FORMAT_VALUES = PARSE_FORMATS.map((f) => f.value)
const RULE_OP_VALUES = RULE_OPS.map((o) => o.value)
const REDACT_KIND_VALUES: RedactKind[] = [...REDACT_KINDS.map((k) => k.value), 'custom']

export function readParse(c: Record<string, unknown>): ParseConfig {
  return {
    field: str(c.field),
    format: oneOf(c.format, PARSE_FORMAT_VALUES, 'json'),
    pattern: str(c.pattern),
    target: str(c.target),
    prefix: str(c.prefix),
    on_error: oneOf(c.on_error, ['keep', 'drop'] as const, 'keep'),
    remove_source: c.remove_source === true,
  }
}

function pairs(v: unknown): FieldPair[] {
  return records(v).map((r) => ({ from: str(r.from), to: str(r.to) }))
}

export function readMap(c: Record<string, unknown>): MapConfig {
  return {
    rename: pairs(c.rename),
    copy: pairs(c.copy),
    set: records(c.set).map((r) => ({ field: str(r.field), value: typeof r.value === 'string' ? r.value : r.value === undefined || r.value === null ? '' : JSON.stringify(r.value) })),
    drop: strings(c.drop),
    severity_from: str(c.severity_from),
    timestamp_from: str(c.timestamp_from),
    timestamp_format: str(c.timestamp_format, 'rfc3339') || 'rfc3339',
  }
}

export function readRules(v: unknown): Rule[] {
  return records(v).map((r) => ({
    field: str(r.field),
    op: oneOf(r.op, RULE_OP_VALUES, 'eq'),
    value: typeof r.value === 'string' ? r.value : r.value === undefined || r.value === null ? '' : JSON.stringify(r.value),
  }))
}

export function readFilter(c: Record<string, unknown>): FilterConfig {
  return {
    mode: oneOf(c.mode, ['keep', 'drop'] as const, 'keep'),
    match: oneOf(c.match, ['all', 'any'] as const, 'all'),
    rules: readRules(c.rules),
  }
}

export function readSample(c: Record<string, unknown>): SampleConfig {
  const ak = c.always_keep && typeof c.always_keep === 'object' ? (c.always_keep as Record<string, unknown>) : {}
  const pct = typeof c.keep_percent === 'number' && Number.isFinite(c.keep_percent) ? c.keep_percent : 10
  return {
    keep_percent: pct,
    always_keep: { match: oneOf(ak.match, ['all', 'any'] as const, 'any'), rules: readRules(ak.rules) },
    key: str(c.key),
  }
}

export function readRedact(c: Record<string, unknown>): RedactConfig {
  return {
    kinds: strings(c.kinds).filter((k): k is RedactKind => (REDACT_KIND_VALUES as string[]).includes(k)),
    custom: records(c.custom).map((r) => ({ name: str(r.name), pattern: str(r.pattern) })),
    fields: strings(c.fields),
    replacement: typeof c.replacement === 'string' ? c.replacement : DEFAULT_REPLACEMENT,
  }
}

export function readRoute(c: Record<string, unknown>): RouteConfig {
  return {
    mode: oneOf(c.mode, ['first', 'all'] as const, 'first'),
    routes: records(c.routes).map((r) => ({
      id: str(r.id),
      name: str(r.name),
      match: oneOf(r.match, ['all', 'any'] as const, 'all'),
      rules: readRules(r.rules),
    })),
    unmatched: oneOf(c.unmatched, ['drop', 'output'] as const, 'drop'),
  }
}

// ---- Validation (hints in the panel; the server has the final say) ----

/**
 * Checks a field path: `name`, `a.b.c`, `LogAttributes['k']` or `LogAttributes.k`.
 * Returns a message, or '' when the path is fine.
 */
export function fieldPathProblem(path: string): string {
  const p = path.trim()
  if (!p) return 'Enter a field'
  if (p !== path) return 'Remove the spaces around the field'
  const bracket = p.match(/^(.*)\['(.*)'\]$/)
  const head = bracket ? bracket[1] : p
  if (bracket && !bracket[2]) return 'The map key in [\'...\'] is empty'
  if (!head) return 'Name the map before [\'...\']'
  if (/[[\]']/.test(head)) return "Use one ['key'] at the end, e.g. LogAttributes['http.method']"
  if (head.split('.').some((s) => s === '')) return 'A field path cannot have empty parts (check the dots)'
  return ''
}

/** Path hint for OTel rows, where attributes are maps with dotted keys. */
export function mapPathHint(signal: string | null | undefined): string {
  if (signal === 'logs') return "Map keys: LogAttributes.http.method (or LogAttributes['http.method']) reads the key http.method."
  if (signal === 'traces') return "Map keys: SpanAttributes.http.method (or SpanAttributes['http.method']) reads the key http.method."
  return ''
}

/** Named groups of an RE2 pattern: (?P<name>...) or (?<name>...). */
export function regexGroupNames(pattern: string): string[] {
  const names: string[] = []
  for (const m of pattern.matchAll(/\(\?P?<([A-Za-z_][A-Za-z0-9_]*)>/g)) names.push(m[1])
  return names
}

/**
 * Checks a pattern as far as the browser can: RE2 has no lookarounds or
 * backreferences, and the rest is compiled with the JS engine.
 */
export function regexProblem(pattern: string): string {
  if (!pattern) return 'Enter a pattern'
  if (/\(\?<?[=!]/.test(pattern)) return 'Lookarounds are not supported (Go RE2)'
  if (/\\[1-9]|\\k</.test(pattern)) return 'Backreferences are not supported (Go RE2)'
  try {
    new RegExp(pattern.replace(/\(\?P</g, '(?<'))
  } catch (e) {
    return e instanceof Error ? e.message : 'Invalid pattern'
  }
  return ''
}

function ruleProblems(rules: Rule[], where: string): string[] {
  const out: string[] = []
  rules.forEach((r, i) => {
    const n = `${where} rule ${i + 1}`
    const fp = fieldPathProblem(r.field)
    if (fp) out.push(`${n}: ${fp}`)
    if (r.op === 'in') {
      let ok = false
      try { ok = Array.isArray(JSON.parse(r.value)) } catch { ok = false }
      if (!ok) out.push(`${n}: "in list" needs a JSON array, e.g. ["a","b"]`)
    } else if (r.op === 'matches') {
      const rp = regexProblem(r.value)
      if (rp) out.push(`${n}: ${rp}`)
    } else if (opNeedsValue(r.op) && r.value === '' && r.op !== 'eq' && r.op !== 'ne') {
      out.push(`${n}: enter a value`)
    }
  })
  return out
}

/** Messages for the panel; empty when the config looks valid. */
export function transformProblems(type: TransformNodeType, config: Record<string, unknown>): string[] {
  const out: string[] = []
  switch (type) {
    case 'transform_parse': {
      const c = readParse(config)
      const fp = fieldPathProblem(c.field)
      if (fp) out.push(`Field: ${fp}`)
      if (c.target) {
        const tp = fieldPathProblem(c.target)
        if (tp) out.push(`Target: ${tp}`)
      }
      if (c.format === 'regex') {
        const rp = regexProblem(c.pattern)
        if (rp) out.push(`Pattern: ${rp}`)
        else if (regexGroupNames(c.pattern).length === 0) out.push('Pattern: add at least one named group, e.g. (?P<status>\\d+)')
      }
      break
    }
    case 'transform_map': {
      const c = readMap(config)
      const check = (label: string, p: string) => {
        const m = fieldPathProblem(p)
        if (m) out.push(`${label}: ${m}`)
      }
      c.copy.forEach((r, i) => { check(`Copy ${i + 1} from`, r.from); check(`Copy ${i + 1} to`, r.to) })
      c.rename.forEach((r, i) => { check(`Rename ${i + 1} from`, r.from); check(`Rename ${i + 1} to`, r.to) })
      c.set.forEach((r, i) => check(`Set ${i + 1}`, r.field))
      c.drop.forEach((d, i) => check(`Drop ${i + 1}`, d))
      if (c.severity_from) check('Severity from', c.severity_from)
      if (c.timestamp_from) {
        check('Timestamp from', c.timestamp_from)
        if (c.timestamp_format.startsWith('layout:') && !c.timestamp_format.slice(7).trim()) out.push('Timestamp: enter a Go layout, e.g. 2006-01-02 15:04:05')
      }
      break
    }
    case 'transform_filter': {
      const c = readFilter(config)
      out.push(...ruleProblems(c.rules, 'Filter'))
      break
    }
    case 'transform_sample': {
      const c = readSample(config)
      if (!(c.keep_percent >= 0 && c.keep_percent <= 100)) out.push('Keep percent must be between 0 and 100')
      if (c.key) {
        const kp = fieldPathProblem(c.key)
        if (kp) out.push(`Key: ${kp}`)
      }
      out.push(...ruleProblems(c.always_keep.rules, 'Always keep'))
      break
    }
    case 'transform_redact': {
      const c = readRedact(config)
      const builtIn = c.kinds.filter((k) => k !== 'custom')
      if (builtIn.length === 0 && c.custom.length === 0) out.push('Pick at least one kind or add a custom pattern')
      c.custom.forEach((p, i) => {
        if (!p.name.trim()) out.push(`Custom ${i + 1}: enter a name`)
        const rp = regexProblem(p.pattern)
        if (rp) out.push(`Custom ${i + 1}: ${rp}`)
      })
      c.fields.forEach((f, i) => {
        const m = fieldPathProblem(f)
        if (m) out.push(`Field ${i + 1}: ${m}`)
      })
      break
    }
    case 'transform_route': {
      const c = readRoute(config)
      if (c.routes.length > ROUTE_MAX) out.push(`At most ${ROUTE_MAX} routes`)
      const ids = new Set<string>()
      const names = new Set<string>()
      c.routes.forEach((r, i) => {
        const n = r.name.trim() ? `Route ${r.name.trim()}` : `Route ${i + 1}`
        if (r.name.length > ROUTE_NAME_MAX) out.push(`${n}: name is longer than ${ROUTE_NAME_MAX} characters`)
        else if (names.has(r.name.trim())) out.push(`${n}: another route has this name`)
        if (r.name.trim()) names.add(r.name.trim())
        if (!ROUTE_ID_PATTERN.test(r.id) || r.id === UNMATCHED_HANDLE) out.push(`${n}: invalid id "${r.id}"`)
        else if (ids.has(r.id)) out.push(`${n}: duplicate id "${r.id}"`)
        ids.add(r.id)
        out.push(...ruleProblems(r.rules, n))
      })
      break
    }
  }
  return out
}

/** Redact config as sent: `custom` is in kinds exactly when custom patterns exist. */
export function withCustomKind(c: RedactConfig): RedactConfig {
  const kinds = c.kinds.filter((k) => k !== 'custom')
  return { ...c, kinds: c.custom.length ? [...kinds, 'custom'] : kinds }
}

// ---- Running counters ----

export interface TransformCounters {
  node_id: string
  in: number
  out: number
  dropped: number
  errors: number
  /** Route nodes: records sent through each output, unmatched included when it is an output. */
  outputs?: { handle: string; out: number }[] | null
  /** Route nodes: records that matched no route and were dropped. */
  unmatched_dropped?: number
}

// ---- Preview API ----

export interface PreviewNode {
  id: string
  node_type: TransformNodeType
  config: Record<string, unknown>
}

export interface PreviewRequest {
  nodes: PreviewNode[]
  records?: Record<string, unknown>[]
  /** Previewing a node after the route: the route output (handle) whose records go on. */
  branch?: string
}

export interface PreviewError {
  index: number
  message: string
}

export interface PreviewStep {
  node_id: string
  output: Record<string, unknown>[]
  dropped: number
  errors?: PreviewError[] | null
  /** Optional: for each output record, its index in this step's input. */
  kept?: number[] | null
  /** Dropped records by index into this step's input, with the reason. */
  drops?: { index: number; reason: string }[] | null
  /** Route step: input indexes sent through each output. */
  routes?: { handle: string; kept: number[] }[] | null
  /** Route step: input indexes that matched no route. */
  unmatched?: number[] | null
}

export interface PreviewResponse {
  input: Record<string, unknown>[]
  steps: PreviewStep[]
}

export const PREVIEW_MAX_RECORDS = 100
