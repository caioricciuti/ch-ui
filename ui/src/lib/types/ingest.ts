export type IngestSignal = 'logs' | 'traces' | 'events'

export const INGEST_SIGNALS: { value: IngestSignal; label: string; hint: string }[] = [
  { value: 'logs', label: 'Logs', hint: 'OTLP logs (protobuf or JSON), written in the otel_logs layout Telemetry reads.' },
  { value: 'traces', label: 'Traces', hint: 'OTLP traces, written in the otel_traces layout with its trace id lookup table.' },
  { value: 'events', label: 'Events', hint: 'NDJSON, a JSON array or one JSON object, inserted as rows into the sink table.' },
]

export function isIngestSignal(v: unknown): v is IngestSignal {
  return v === 'logs' || v === 'traces' || v === 'events'
}

export function signalHint(signal: unknown): string {
  return INGEST_SIGNALS.find((s) => s.value === signal)?.hint ?? ''
}

/** Logs and traces sinks create their table in the OpenTelemetry layout. */
export function usesOtelLayout(signal: unknown): boolean {
  return signal === 'logs' || signal === 'traces'
}

export interface IngestKey {
  id: string
  name: string
  prefix: string
  created_by: string | null
  created_at: string
  last_used_at: string | null
  revoked_at: string | null
}

export interface IngestStats {
  signal: IngestSignal
  received: { requests: number; records: number; bytes: number }
  rejected: Record<string, number>
  written: { rows: number; batches: number; last_write_at: string | null }
  spool: { bytes: number; segments: number; oldest_age_ms: number }
  rate: { records_per_sec_1m: number; bytes_per_sec_1m: number }
  last_error: string | null
}

export interface IngestInfo {
  base_url: string
  signals: IngestSignal[]
  separate_listener: boolean
}

const REJECT_LABELS: Record<string, string> = {
  auth: 'bad key',
  rate_limit: 'rate limit',
  quota: 'daily quota',
  too_large: 'too large',
  malformed: 'malformed',
  spool_full: 'spool full',
}

/** Non-zero rejection counters, largest first. */
export function rejectedEntries(rejected: Record<string, number> | null | undefined): { reason: string; label: string; count: number }[] {
  return Object.entries(rejected ?? {})
    .filter(([, n]) => typeof n === 'number' && n > 0)
    .map(([reason, count]) => ({ reason, label: REJECT_LABELS[reason] ?? reason.replace(/_/g, ' '), count }))
    .sort((a, b) => b.count - a.count)
}

/** "1.2k/s" style rate; whole numbers below 10 keep one decimal. */
export function formatRate(perSec: number): string {
  if (!Number.isFinite(perSec) || perSec <= 0) return '0/s'
  if (perSec < 10) return `${perSec.toFixed(1).replace(/\.0$/, '')}/s`
  if (perSec < 1000) return `${Math.round(perSec)}/s`
  if (perSec < 1_000_000) return `${(perSec / 1000).toFixed(1).replace(/\.0$/, '')}k/s`
  return `${(perSec / 1_000_000).toFixed(1).replace(/\.0$/, '')}M/s`
}

/** Age of the oldest spooled record: "0s", "42s", "3m 5s", "2h 10m". */
export function formatAge(ms: number): string {
  if (!Number.isFinite(ms) || ms <= 0) return '0s'
  const s = Math.floor(ms / 1000)
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m ${s % 60}s`
  const h = Math.floor(m / 60)
  return `${h}h ${m % 60}m`
}

// ── Shipper snippets ─────────────────────────────────────────

export type Shipper = 'collector' | 'sdk' | 'vector' | 'fluentbit' | 'curl'

export const KEY_PLACEHOLDER = '<YOUR_KEY>'

const SHIPPER_LABELS: Record<Shipper, string> = {
  collector: 'OpenTelemetry Collector',
  sdk: 'OTel SDK env vars',
  vector: 'Vector',
  fluentbit: 'Fluent Bit',
  curl: 'curl',
}

/** Shippers that can send a signal. Vector has no reliable OTLP encoder, so it only sends events. */
export function shippersFor(signal: IngestSignal): { id: Shipper; label: string }[] {
  const ids: Shipper[] = signal === 'events' ? ['vector', 'fluentbit', 'curl'] : ['collector', 'sdk', 'fluentbit', 'curl']
  return ids.map((id) => ({ id, label: SHIPPER_LABELS[id] }))
}

/** Base URL without a trailing slash. */
export function normalizeBaseUrl(base: string): string {
  return base.trim().replace(/\/+$/, '')
}

export function signalEndpoint(base: string, signal: IngestSignal): string {
  return `${normalizeBaseUrl(base)}/v1/${signal}`
}

function shellQuote(s: string): string {
  return `'${s.replace(/'/g, `'\\''`)}'`
}

function hostParts(base: string): { host: string; port: number; tls: boolean; path: string } {
  try {
    const u = new URL(normalizeBaseUrl(base))
    const tls = u.protocol === 'https:'
    return { host: u.hostname, port: u.port ? Number(u.port) : tls ? 443 : 80, tls, path: u.pathname.replace(/\/+$/, '') }
  } catch {
    return { host: 'localhost', port: 80, tls: false, path: '/ingest' }
  }
}

function collector(base: string, signal: IngestSignal, key: string): string {
  return [
    'receivers:',
    '  otlp:',
    '    protocols:',
    '      grpc:',
    '        endpoint: 0.0.0.0:4317',
    '      http:',
    '        endpoint: 0.0.0.0:4318',
    '',
    'processors:',
    '  batch: {}',
    '',
    'exporters:',
    '  otlphttp/ch-ui:',
    `    ${signal}_endpoint: ${signalEndpoint(base, signal)}`,
    '    headers:',
    `      Authorization: "Bearer ${key}"`,
    '',
    'service:',
    '  pipelines:',
    `    ${signal}:`,
    '      receivers: [otlp]',
    '      processors: [batch]',
    '      exporters: [otlphttp/ch-ui]',
    '',
  ].join('\n')
}

function sdk(base: string, signal: IngestSignal, key: string): string {
  const upper = signal.toUpperCase()
  // OTEL_EXPORTER_OTLP_*_HEADERS values are URL-encoded, so the space is %20.
  return [
    `export OTEL_${upper}_EXPORTER=otlp`,
    `export OTEL_EXPORTER_OTLP_${upper}_PROTOCOL=http/protobuf`,
    `export OTEL_EXPORTER_OTLP_${upper}_ENDPOINT=${signalEndpoint(base, signal)}`,
    `export OTEL_EXPORTER_OTLP_${upper}_HEADERS="Authorization=Bearer%20${key}"`,
    '',
  ].join('\n')
}

function vector(base: string, key: string): string {
  return [
    'sources:',
    '  app_logs:',
    '    type: file',
    '    include: ["/var/log/app/*.log"]',
    '',
    'sinks:',
    '  ch_ui:',
    '    type: http',
    '    inputs: [app_logs]',
    `    uri: ${signalEndpoint(base, 'events')}`,
    '    method: post',
    '    encoding:',
    '      codec: json',
    '    framing:',
    '      method: newline_delimited',
    '    auth:',
    '      strategy: bearer',
    `      token: "${key}"`,
    '',
  ].join('\n')
}

function fluentbit(base: string, signal: IngestSignal, key: string): string {
  const { host, port, tls, path } = hostParts(base)
  const kv = (k: string, v: string | number) => `    ${k.padEnd(12)}${v}`
  const input =
    signal === 'traces'
      ? ['[INPUT]', kv('Name', 'opentelemetry'), kv('Listen', '0.0.0.0'), kv('Port', 4318)]
      : ['[INPUT]', kv('Name', 'tail'), kv('Path', '/var/log/app/*.log'), kv('Tag', 'app')]
  const output = ['[OUTPUT]', kv('Name', signal === 'events' ? 'http' : 'opentelemetry'), kv('Match', '*'), kv('Host', host), kv('Port', port)]
  if (signal === 'events') output.push(kv('URI', `${path}/v1/events`), kv('Format', 'json_lines'))
  else output.push(kv(signal === 'logs' ? 'Logs_uri' : 'Traces_uri', `${path}/v1/${signal}`))
  output.push(kv('Header', `Authorization Bearer ${key}`), kv('Tls', tls ? 'On' : 'Off'))
  return [...input, '', ...output, ''].join('\n')
}

function curl(base: string, signal: IngestSignal, key: string, now: Date): string {
  let body: string
  if (signal === 'events') {
    body = JSON.stringify([{ message: 'hello from curl' }])
  } else if (signal === 'logs') {
    body = JSON.stringify({
      resourceLogs: [{
        resource: { attributes: [{ key: 'service.name', value: { stringValue: 'curl-test' } }] },
        scopeLogs: [{ logRecords: [{ severityText: 'INFO', severityNumber: 9, body: { stringValue: 'hello from curl' } }] }],
      }],
    })
  } else {
    const startNs = BigInt(now.getTime()) * 1_000_000n
    body = JSON.stringify({
      resourceSpans: [{
        resource: { attributes: [{ key: 'service.name', value: { stringValue: 'curl-test' } }] },
        scopeSpans: [{
          spans: [{
            traceId: '5b8efff798038103d269b633813fc60c',
            spanId: 'eee19b7ec3c1b174',
            name: 'hello from curl',
            kind: 2,
            startTimeUnixNano: String(startNs),
            endTimeUnixNano: String(startNs + 25_000_000n),
          }],
        }],
      }],
    })
  }
  const lines = [
    `curl -sS -X POST ${shellQuote(signalEndpoint(base, signal))} \\`,
    `  -H ${shellQuote(`Authorization: Bearer ${key}`)} \\`,
    `  -H 'Content-Type: application/json' \\`,
    `  -d ${shellQuote(body)}`,
    '',
  ]
  if (signal === 'events') lines.unshift('# Fields must match the sink table columns.')
  return lines.join('\n')
}

/** A ready-to-paste config for one shipper. The key falls back to the placeholder. */
export function buildSnippet(opts: { baseUrl: string; signal: IngestSignal; shipper: Shipper; key?: string; now?: Date }): string {
  const key = opts.key?.trim() || KEY_PLACEHOLDER
  switch (opts.shipper) {
    case 'collector':
      return collector(opts.baseUrl, opts.signal, key)
    case 'sdk':
      return sdk(opts.baseUrl, opts.signal, key)
    case 'vector':
      return vector(opts.baseUrl, key)
    case 'fluentbit':
      return fluentbit(opts.baseUrl, opts.signal, key)
    case 'curl':
      return curl(opts.baseUrl, opts.signal, key, opts.now ?? new Date())
  }
}

/** File name hint shown above a snippet. */
export function snippetFile(shipper: Shipper): string {
  switch (shipper) {
    case 'collector':
      return 'otel-collector.yaml'
    case 'sdk':
      return 'shell environment'
    case 'vector':
      return 'vector.yaml'
    case 'fluentbit':
      return 'fluent-bit.conf'
    case 'curl':
      return 'shell'
  }
}


const OTEL_TABLES: Partial<Record<IngestSignal, string>> = { logs: 'otel_logs', traces: 'otel_traces' }

/**
 * Sink config with the table defaulted for an ingest signal, or null when
 * nothing changes. Only an empty table or one of our own defaults is
 * replaced; a name the user typed is kept.
 */
export function withOtelSinkDefaults(sink: Record<string, unknown>, signal: unknown): Record<string, unknown> | null {
  if (!isIngestSignal(signal)) return null
  const target = OTEL_TABLES[signal]
  if (!target) return null
  const table = typeof sink.table === 'string' ? sink.table.trim() : ''
  const isDefault = table === '' || Object.values(OTEL_TABLES).includes(table)
  if (!isDefault || (table === target && sink.create_table === true)) return null
  return { ...sink, table: target, create_table: true }
}
