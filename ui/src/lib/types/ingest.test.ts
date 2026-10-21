import { describe, expect, it } from 'vitest'
import { KEY_PLACEHOLDER, buildSnippet, defaultReceiveTable, pickIngestPipeline, receiveSetupError, esEndpoint, hecEndpoint, shipperApisFor, snippetFile, withOtelSinkDefaults, formatAge, formatRate, rejectedEntries, shippersFor, signalEndpoint, usesOtelLayout } from './ingest'

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

describe('Elasticsearch and Splunk HEC shippers', () => {
  it('offers them for logs and events, never for traces', () => {
    expect(shipperApisFor('logs').map((a) => a.id)).toEqual(['native', 'elasticsearch', 'splunk'])
    expect(shipperApisFor('events').map((a) => a.id)).toEqual(['native', 'elasticsearch', 'splunk'])
    expect(shipperApisFor('traces').map((a) => a.id)).toEqual(['native'])
    expect(shippersFor('logs', 'elasticsearch').map((s) => s.id)).toEqual(['filebeat', 'logstash', 'fluentbit-es', 'vector-es'])
    expect(shippersFor('events', 'splunk').map((s) => s.id)).toEqual(['vector-hec', 'otel-hec', 'curl-hec'])
    expect(shippersFor('traces', 'elasticsearch')).toEqual([])
  })

  it('derives the es and hec urls from the base url', () => {
    expect(esEndpoint(BASE)).toBe('https://chui.example.com/ingest/es')
    expect(hecEndpoint(BASE)).toBe('https://chui.example.com/ingest/services/collector')
  })

  it('builds Filebeat with host, protocol, path, the key as password and setup off', () => {
    const s = buildSnippet({ baseUrl: BASE, signal: 'logs', shipper: 'filebeat', key: KEY })
    expect(s).toBe(
      [
        'output.elasticsearch:',
        '  hosts: ["chui.example.com:443"]',
        '  protocol: https',
        '  path: /ingest/es',
        '  username: chui',
        `  password: ${KEY}`,
        `  # or: api_key: "chui:${KEY}"`,
        '',
        'setup.ilm.enabled: false',
        'setup.template.enabled: false',
        '',
      ].join('\n'),
    )
    expect(snippetFile('filebeat')).toBe('filebeat.yml')
    const plain = buildSnippet({ baseUrl: 'http://10.0.0.5:8080/ingest', signal: 'events', shipper: 'filebeat' })
    expect(plain).toContain('  hosts: ["10.0.0.5:8080"]\n  protocol: http\n')
    expect(plain).toContain(`password: ${KEY_PLACEHOLDER}`)
  })

  it('builds Logstash with ILM, templates and data streams off', () => {
    const s = buildSnippet({ baseUrl: BASE, signal: 'logs', shipper: 'logstash', key: KEY })
    expect(s).toContain('hosts => ["https://chui.example.com:443/ingest/es"]')
    expect(s).toContain(`password => "${KEY}"`)
    for (const line of ['user => "chui"', 'ilm_enabled => false', 'manage_template => false', 'data_stream => false', 'index => "logstash-%{+yyyy.MM.dd}"']) {
      expect(s).toContain(line)
    }
  })

  it('builds the Fluent Bit es output with the path and suppressed type name', () => {
    const s = buildSnippet({ baseUrl: BASE, signal: 'logs', shipper: 'fluentbit-es', key: KEY })
    expect(s).toContain('    Name               es\n')
    expect(s).toMatch(/Host\s+chui\.example\.com\n/)
    expect(s).toMatch(/Port\s+443\n/)
    expect(s).toMatch(/Tls\s+On\n/)
    expect(s).toMatch(/Path\s+\/ingest\/es\n/)
    expect(s).toMatch(/HTTP_User\s+chui\n/)
    expect(s).toMatch(new RegExp(`HTTP_Passwd\\s+${KEY}\\n`))
    expect(s).toContain('    Suppress_Type_Name On\n')
    expect(s).toMatch(/Logstash_Format\s+On\n/)
  })

  it('builds the Vector elasticsearch sink with v8, bulk and basic auth', () => {
    const s = buildSnippet({ baseUrl: BASE, signal: 'events', shipper: 'vector-es', key: KEY })
    expect(s).toContain('type = "elasticsearch"')
    expect(s).toContain('endpoints = ["https://chui.example.com/ingest/es"]')
    expect(s).toContain('api_version = "v8"\nmode = "bulk"')
    expect(s).toContain('auth.strategy = "basic"')
    expect(s).toContain(`auth.password = "${KEY}"`)
    // The server's es_url wins over the derived one.
    expect(buildSnippet({ baseUrl: BASE, esUrl: 'https://other:4318/ingest/es/', signal: 'logs', shipper: 'vector-es' })).toContain('endpoints = ["https://other:4318/ingest/es"]')
  })

  it('builds the HEC snippets with the token', () => {
    const vec = buildSnippet({ baseUrl: BASE, signal: 'logs', shipper: 'vector-hec', key: KEY })
    expect(vec).toContain('type = "splunk_hec_logs"')
    expect(vec).toContain('endpoint = "https://chui.example.com/ingest"')
    expect(vec).toContain(`default_token = "${KEY}"`)
    expect(vec).toContain('acknowledgements.indexer_acknowledgements_enabled = false')

    const otel = buildSnippet({ baseUrl: BASE, signal: 'logs', shipper: 'otel-hec', key: KEY })
    expect(otel).toBe(`exporters:\n  splunk_hec:\n    endpoint: https://chui.example.com/ingest/services/collector\n    token: ${KEY}\n`)

    const curl = buildSnippet({ baseUrl: BASE, hecUrl: 'https://chui.example.com/ingest/services/collector', signal: 'events', shipper: 'curl-hec', key: KEY })
    expect(curl).toContain("curl 'https://chui.example.com/ingest/services/collector'")
    expect(curl).toContain(`-H 'Authorization: Splunk ${KEY}'`)
    // Two concatenated HEC events.
    const body = curl.slice(curl.indexOf("-d '") + 4, curl.lastIndexOf("'"))
    const [first, second] = body.split('}{').map((part, i, all) => (i === 0 ? part + '}' : i === all.length - 1 ? '{' + part : `{${part}}`))
    expect(JSON.parse(first).event).toBe('user signed in')
    expect(JSON.parse(second).event.level).toBe('error')
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

describe('pickIngestPipeline', () => {
  const pipes = [
    { id: 't1', signal: 'traces' as const },
    { id: 'l1', signal: 'logs' as const },
    { id: 'l2', signal: 'logs' as const },
  ]

  it('keeps the requested pipeline whatever its signal', () => {
    expect(pickIngestPipeline(pipes, { keepId: 't1', signal: 'logs' })).toBe('t1')
  })

  it('picks the first pipeline of the signal', () => {
    expect(pickIngestPipeline(pipes, { signal: 'logs' })).toBe('l1')
    expect(pickIngestPipeline(pipes, { keepId: 'gone', signal: 'traces' })).toBe('t1')
  })

  it('keeps an earlier pick only for the same signal', () => {
    expect(pickIngestPipeline(pipes, { previousId: 'l2', signal: 'logs' })).toBe('l2')
    expect(pickIngestPipeline(pipes, { previousId: 'l2', signal: 'traces' })).toBe('t1')
    expect(pickIngestPipeline([{ id: 'l1', signal: 'logs' as const }], { previousId: 'l1', signal: 'traces' })).toBe('')
    expect(pickIngestPipeline(pipes, { previousId: 'l2' })).toBe('l2')
  })

  it('returns no pipeline when only other signals have one', () => {
    expect(pickIngestPipeline([{ id: 't1', signal: 'traces' as const }], { signal: 'logs' })).toBe('')
  })

  it('falls back to the first pipeline without a signal, none when empty', () => {
    expect(pickIngestPipeline(pipes, {})).toBe('t1')
    expect(pickIngestPipeline([], {})).toBe('')
  })
})

describe('receive setup', () => {
  const ok = { signal: 'logs' as const, database: 'default', table: 'otel_logs', ttl_days: 30 }

  it('defaults the table per signal', () => {
    expect(defaultReceiveTable('logs')).toBe('otel_logs')
    expect(defaultReceiveTable('traces')).toBe('otel_traces')
  })

  it('accepts a complete form', () => {
    expect(receiveSetupError(ok)).toBe('')
  })

  it('names the first missing or bad field', () => {
    expect(receiveSetupError({ ...ok, database: ' ' })).toBe('Pick a database.')
    expect(receiveSetupError({ ...ok, table: '' })).toBe('Enter a table name.')
    expect(receiveSetupError({ ...ok, ttl_days: 0 })).toMatch(/Retention/)
    expect(receiveSetupError({ ...ok, ttl_days: 1.5 })).toMatch(/Retention/)
    expect(receiveSetupError({ ...ok, ttl_days: Number.NaN })).toMatch(/Retention/)
  })
})
