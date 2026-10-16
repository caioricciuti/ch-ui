import { describe, expect, it } from 'vitest'
import { KEY_PLACEHOLDER, buildSnippet, withOtelSinkDefaults, formatAge, formatRate, rejectedEntries, shippersFor, signalEndpoint, usesOtelLayout } from './ingest'

const BASE = 'https://chui.example.com/ingest/'
const KEY = 'chi_abcdef0123456789'

describe('signalEndpoint', () => {
  it('joins the base url and the signal path without a double slash', () => {
    expect(signalEndpoint(BASE, 'logs')).toBe('https://chui.example.com/ingest/v1/logs')
    expect(signalEndpoint('http://h:8080/ingest', 'events')).toBe('http://h:8080/ingest/v1/events')
  })
})

describe('shippersFor', () => {
  it('offers OTLP shippers for logs and traces, Vector only for events', () => {
    expect(shippersFor('logs').map((s) => s.id)).toEqual(['collector', 'sdk', 'fluentbit', 'curl'])
    expect(shippersFor('traces').map((s) => s.id)).not.toContain('vector')
    expect(shippersFor('events').map((s) => s.id)).toEqual(['vector', 'fluentbit', 'curl'])
  })
})

describe('buildSnippet', () => {
  it('uses the placeholder when no key is given', () => {
    const s = buildSnippet({ baseUrl: BASE, signal: 'logs', shipper: 'collector' })
    expect(s).toContain(`Authorization: "Bearer ${KEY_PLACEHOLDER}"`)
  })

  it('builds an otlphttp exporter with the signal endpoint and bearer header', () => {
    const s = buildSnippet({ baseUrl: BASE, signal: 'traces', shipper: 'collector', key: KEY })
    expect(s).toContain('  otlphttp/ch-ui:\n    traces_endpoint: https://chui.example.com/ingest/v1/traces\n    headers:\n      Authorization: "Bearer chi_abcdef0123456789"')
    expect(s).toContain('    traces:\n      receivers: [otlp]')
    expect(s).toContain('exporters: [otlphttp/ch-ui]')
  })

  it('sets per-signal SDK env vars with full paths and an encoded header', () => {
    const s = buildSnippet({ baseUrl: BASE, signal: 'logs', shipper: 'sdk', key: KEY })
    expect(s).toContain('export OTEL_LOGS_EXPORTER=otlp')
    expect(s).toContain('export OTEL_EXPORTER_OTLP_LOGS_PROTOCOL=http/protobuf')
    expect(s).toContain('export OTEL_EXPORTER_OTLP_LOGS_ENDPOINT=https://chui.example.com/ingest/v1/logs')
    expect(s).toContain('export OTEL_EXPORTER_OTLP_LOGS_HEADERS="Authorization=Bearer%20chi_abcdef0123456789"')
  })

  it('points Vector at the events endpoint with bearer auth and NDJSON framing', () => {
    const s = buildSnippet({ baseUrl: BASE, signal: 'events', shipper: 'vector', key: KEY })
    expect(s).toContain('type: http')
    expect(s).toContain('uri: https://chui.example.com/ingest/v1/events')
    expect(s).toContain('method: newline_delimited')
    expect(s).toContain('strategy: bearer')
    expect(s).toContain(`token: "${KEY}"`)
  })

  it('splits the base url into Fluent Bit host, port, uri and tls', () => {
    const s = buildSnippet({ baseUrl: BASE, signal: 'logs', shipper: 'fluentbit', key: KEY })
    expect(s).toMatch(/Name\s+opentelemetry/)
    expect(s).toMatch(/Host\s+chui\.example\.com\n/)
    expect(s).toMatch(/Port\s+443\n/)
    expect(s).toMatch(/Logs_uri\s+\/ingest\/v1\/logs\n/)
    expect(s).toMatch(/Header\s+Authorization Bearer chi_abcdef0123456789\n/)
    expect(s).toMatch(/Tls\s+On\n/)

    const plain = buildSnippet({ baseUrl: 'http://10.0.0.5:8080/ingest', signal: 'events', shipper: 'fluentbit' })
    expect(plain).toMatch(/Name\s+http\n/)
    expect(plain).toMatch(/Port\s+8080\n/)
    expect(plain).toMatch(/URI\s+\/ingest\/v1\/events\n/)
    expect(plain).toMatch(/Format\s+json_lines\n/)
    expect(plain).toMatch(/Tls\s+Off\n/)
  })

  it('builds curl commands with valid OTLP JSON bodies', () => {
    const now = new Date('2026-10-16T12:00:00Z')
    const logs = buildSnippet({ baseUrl: BASE, signal: 'logs', shipper: 'curl', key: KEY, now })
    expect(logs).toContain("curl -sS -X POST 'https://chui.example.com/ingest/v1/logs'")
    expect(logs).toContain(`-H 'Authorization: Bearer ${KEY}'`)
    const logsBody = JSON.parse(logs.slice(logs.indexOf("-d '") + 4, logs.lastIndexOf("'"))) as { resourceLogs: unknown[] }
    expect(logsBody.resourceLogs).toHaveLength(1)

    const traces = buildSnippet({ baseUrl: BASE, signal: 'traces', shipper: 'curl', key: KEY, now })
    const body = JSON.parse(traces.slice(traces.indexOf("-d '") + 4, traces.lastIndexOf("'"))) as {
      resourceSpans: { scopeSpans: { spans: { traceId: string; spanId: string; startTimeUnixNano: string; endTimeUnixNano: string }[] }[] }[]
    }
    const span = body.resourceSpans[0].scopeSpans[0].spans[0]
    expect(span.traceId).toMatch(/^[0-9a-f]{32}$/)
    expect(span.spanId).toMatch(/^[0-9a-f]{16}$/)
    expect(span.startTimeUnixNano).toBe('1792152000000000000')
    expect(BigInt(span.endTimeUnixNano) > BigInt(span.startTimeUnixNano)).toBe(true)
  })

  it('shell-quotes a key that contains a single quote', () => {
    const s = buildSnippet({ baseUrl: BASE, signal: 'events', shipper: 'curl', key: "a'b" })
    expect(s).toContain(`-H 'Authorization: Bearer a'\\''b'`)
  })
})

describe('stats formatting', () => {
  it('keeps only non-zero rejections, largest first', () => {
    expect(rejectedEntries({ auth: 0, rate_limit: 3, quota: 0, malformed: 7 })).toEqual([
      { reason: 'malformed', label: 'malformed', count: 7 },
      { reason: 'rate_limit', label: 'rate limit', count: 3 },
    ])
    expect(rejectedEntries(null)).toEqual([])
  })

  it('formats rates', () => {
    expect(formatRate(0)).toBe('0/s')
    expect(formatRate(2.5)).toBe('2.5/s')
    expect(formatRate(3)).toBe('3/s')
    expect(formatRate(420.4)).toBe('420/s')
    expect(formatRate(1500)).toBe('1.5k/s')
    expect(formatRate(2_000_000)).toBe('2M/s')
  })

  it('formats spool ages', () => {
    expect(formatAge(0)).toBe('0s')
    expect(formatAge(42_000)).toBe('42s')
    expect(formatAge(185_000)).toBe('3m 5s')
    expect(formatAge(7_800_000)).toBe('2h 10m')
  })

  it('knows which signals use the OpenTelemetry layout', () => {
    expect(usesOtelLayout('logs')).toBe(true)
    expect(usesOtelLayout('traces')).toBe(true)
    expect(usesOtelLayout('events')).toBe(false)
  })
})

describe('withOtelSinkDefaults', () => {
  it('fills an empty table and turns create_table on', () => {
    expect(withOtelSinkDefaults({ database: 'default' }, 'logs')).toEqual({ database: 'default', table: 'otel_logs', create_table: true })
  })

  it('swaps the previous default when the signal changes', () => {
    expect(withOtelSinkDefaults({ table: 'otel_logs', create_table: true }, 'traces')).toEqual({ table: 'otel_traces', create_table: true })
  })

  it('keeps a table the user typed, and leaves events alone', () => {
    expect(withOtelSinkDefaults({ table: 'my_logs' }, 'logs')).toBeNull()
    expect(withOtelSinkDefaults({ table: '' }, 'events')).toBeNull()
  })

  it('reports no change when already defaulted', () => {
    expect(withOtelSinkDefaults({ table: 'otel_traces', create_table: true }, 'traces')).toBeNull()
  })
})
