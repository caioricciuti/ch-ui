import { describe, expect, it } from 'vitest'
import type { DashboardVariable, PanelLink } from '../types/api'
import {
  bucketStep, bucketWindow, buildClickContext, interpolate, linkLabel, quoteSqlString, resolveLink, safeUrl, shareLinkIssue,
} from './drilldown'

const meta = [
  { name: 'host', type: 'String' },
  { name: 'hits', type: 'UInt64' },
  { name: 'ts', type: 'DateTime' },
]

const row = { host: "o'brien\\db", hits: 42, ts: '2026-10-14T10:00:00Z', empty: null }

describe('buildClickContext', () => {
  it('reads the value and turns a time column into an instant', () => {
    const ctx = buildClickContext(row, 'host', 'hits', meta)
    expect(ctx).toMatchObject({ value: "o'brien\\db", field: 'host', series: 'hits', timeMs: null, window: null })
    const t = buildClickContext(row, 'ts', '', meta, { from: 1, to: 2 })
    expect(t.timeMs).toBe(Date.parse('2026-10-14T10:00:00Z'))
    expect(t.window).toEqual({ from: 1, to: 2 })
  })
})

describe('interpolate', () => {
  const ctx = buildClickContext(row, 'host', 'series a', meta)

  it('fills every token kind', () => {
    const vars = { env: ['prod'], dbs: ['a', 'b'] }
    expect(interpolate('${__value}|${__field}|${__series}|${__row.hits}|${env}|${dbs}', ctx, 'text', vars))
      .toBe("o'brien\\db|host|series a|42|prod|a,b")
  })

  it('leaves unknown tokens and missing row columns alone', () => {
    expect(interpolate('${nope} ${__row.gone} $__value', ctx, 'text')).toBe('${nope} ${__row.gone} $__value')
  })

  it('url: encodes each value', () => {
    expect(interpolate('https://x.example/?h=${__value}&s=${__series}', ctx, 'url'))
      .toBe("https://x.example/?h=o'brien%5Cdb&s=series%20a")
    expect(interpolate('${v}', ctx, 'url', { v: ['a&b', 'c d'] })).toBe('a%26b,c%20d')
  })

  it('sql: quotes strings like the server, numbers stay bare', () => {
    expect(interpolate('${__value}', ctx, 'sql')).toBe("'o''brien\\\\db'")
    expect(quoteSqlString("a'b\\c")).toBe("'a''b\\\\c'")
    expect(quoteSqlString("\\'")).toBe("'\\\\'''")
    expect(interpolate('${__row.hits}', ctx, 'sql')).toBe('42')
    expect(interpolate('${dbs}', ctx, 'sql', { dbs: ['a', "b'c"] })).toBe("'a','b''c'")
  })

  it('sql: non-finite numbers and other types are quoted', () => {
    const odd = buildClickContext({ a: NaN, b: Infinity, c: true, d: { k: 1 }, e: '12' }, 'a', '', [])
    expect(interpolate('${__row.a} ${__row.b} ${__row.c} ${__row.d} ${__row.e}', odd, 'sql'))
      .toBe(`'NaN' 'Infinity' 'true' '{"k":1}' '12'`)
  })

  it('nulls are empty in url and text, NULL in sql', () => {
    const n = buildClickContext(row, 'empty', '', meta)
    expect(interpolate('[${__value}]', n, 'url')).toBe('[]')
    expect(interpolate('[${__value}]', n, 'text')).toBe('[]')
    expect(interpolate('x = ${__value}', n, 'sql')).toBe('x = NULL')
    expect(interpolate('x = ${__value:raw}', n, 'sql')).toBe('x = ')
  })

  it('raw is unquoted in sql only', () => {
    expect(interpolate('IN (${__value:raw})', ctx, 'sql')).toBe("IN (o'brien\\db)")
    expect(interpolate('${__value:raw}', ctx, 'url')).toBe("o'brien%5Cdb")
    expect(interpolate('${__value:raw}', ctx, 'text')).toBe("o'brien\\db")
  })

  it('time values pass as the result holds them, epoch seconds as ISO instants', () => {
    const t = buildClickContext({ ts: '2026-10-14T10:00:00Z' }, 'ts', '', meta)
    const iso = '2026-10-14T10:00:00Z'
    expect(t.timeMs).toBe(Date.UTC(2026, 9, 14, 10))
    expect(interpolate('${__value}', t, 'text')).toBe(iso)
    expect(interpolate('${__value}', t, 'sql')).toBe(`'${iso}'`)
    const epoch = buildClickContext({ ts: 1_760_436_000 }, 'ts', '', meta)
    expect(interpolate('${__value}', epoch, 'text')).toBe(new Date(1_760_436_000_000).toISOString())
  })
})

describe('safeUrl', () => {
  it('allows absolute http and https only', () => {
    expect(safeUrl('https://x.example/a?b=1')).toBe('https://x.example/a?b=1')
    expect(safeUrl('HTTP://X.example')).toBe('http://x.example/')
    for (const bad of [
      'javascript:alert(1)',
      'JaVaScRiPt:alert(1)',
      ' javascript:alert(1)',
      'java\nscript:alert(1)',
      '\tjavascript:alert(1)',
      'data:text/html,<script>alert(1)</script>',
      'vbscript:msgbox(1)',
      '//evil.example/x',
      '/relative/path',
      'evil.example',
      'file:///etc/passwd',
      'ftp://x.example',
      '',
    ]) {
      expect(safeUrl(bad), bad).toBeNull()
    }
  })
})

describe('resolveLink', () => {
  const ctx = buildClickContext(row, 'host', 'hits', meta)
  const env = { variables: { env: ['prod'], host: ['old'] }, range: { from: 'now-1h', to: 'now' } }

  it('setVariable takes the clicked value or its template', () => {
    expect(resolveLink({ kind: 'setVariable', variable: 'host' }, ctx, env))
      .toEqual({ kind: 'setVariable', variable: 'host', value: "o'brien\\db" })
    expect(resolveLink({ kind: 'setVariable', variable: 'host', template: '${__series}' }, ctx, env))
      .toEqual({ kind: 'setVariable', variable: 'host', value: 'hits' })
  })

  it('setVariable passes a time as the panel returned it', () => {
    const cols = [
      { name: 'ts', type: "DateTime('Europe/Madrid')" },
      { name: 'ms', type: 'DateTime64(3)' },
      { name: 'day', type: 'Date' },
    ]
    const r = { ts: '2026-01-01T09:00:00Z', ms: '2026-01-01T09:00:00.120Z', day: '2026-01-01' }
    const value = (field: string) => resolveLink({ kind: 'setVariable', variable: 'v' }, buildClickContext(r, field, '', cols), env)
    expect(value('ts')).toEqual({ kind: 'setVariable', variable: 'v', value: '2026-01-01T09:00:00Z' })
    expect(value('ms')).toEqual({ kind: 'setVariable', variable: 'v', value: '2026-01-01T09:00:00.120Z' })
    expect(value('day')).toEqual({ kind: 'setVariable', variable: 'v', value: '2026-01-01' })
  })

  it('dashboard: params, carried variables and the range', () => {
    const link: PanelLink = { kind: 'dashboard', dashboardId: 'd2', carryVariables: true, params: { host: '${__value}' } }
    const out = resolveLink(link, ctx, env)
    expect(out.kind).toBe('dashboard')
    if (out.kind !== 'dashboard') return
    const params = new URLSearchParams(out.search)
    expect(params.getAll('var-env')).toEqual(['prod'])
    expect(params.getAll('var-host')).toEqual(["o'brien\\db"])
    expect(params.get('from')).toBe('now-1h')
    expect(params.get('to')).toBe('now')
  })

  it('dashboard: a time click opens on its bucket and carries nothing unasked', () => {
    const t = buildClickContext(row, 'ts', '', meta, bucketWindow(1_760_436_000, 300))
    const out = resolveLink({ kind: 'dashboard', dashboardId: 'd2' }, t, env)
    if (out.kind !== 'dashboard') throw new Error(out.kind)
    const params = new URLSearchParams(out.search)
    expect(params.get('from')).toBe('1760436000000')
    expect(params.get('to')).toBe('1760436300000')
    expect(params.has('var-env')).toBe(false)
  })

  it('dashboard: an id that is not one is refused', () => {
    expect(resolveLink({ kind: 'dashboard', dashboardId: '../admin?x=1' }, ctx, env).kind).toBe('blocked')
    expect(resolveLink({ kind: 'dashboard' }, ctx, env).kind).toBe('blocked')
  })

  it('sql is interpolated with sql quoting', () => {
    expect(resolveLink({ kind: 'sql', template: 'SELECT * FROM t WHERE host = ${__value} AND env IN (${env})' }, ctx, env))
      .toEqual({ kind: 'sql', sql: "SELECT * FROM t WHERE host = 'o''brien\\\\db' AND env IN ('prod')" })
  })

  it('url is encoded and must stay http(s)', () => {
    expect(resolveLink({ kind: 'url', template: 'https://x.example/h/${__value}' }, ctx, env))
      .toEqual({ kind: 'url', url: "https://x.example/h/o'brien%5Cdb" })
    expect(resolveLink({ kind: 'url', template: '${__value}' }, buildClickContext({ v: 'javascript:alert(1)' }, 'v', '', []), env).kind)
      .toBe('blocked')
    // A token cannot become the scheme: the value is encoded first.
    expect(resolveLink({ kind: 'url', template: '${__value}//x' }, buildClickContext({ v: 'https:' }, 'v', '', []), env).kind)
      .toBe('blocked')
  })
})

describe('link helpers', () => {
  const vars: DashboardVariable[] = [
    { name: 'host', type: 'query', query: 'SELECT 1' },
    { name: 'note', type: 'text' },
  ]

  it('names share link limits', () => {
    expect(shareLinkIssue({ kind: 'sql', template: 'x' }, vars)).not.toBeNull()
    expect(shareLinkIssue({ kind: 'dashboard', dashboardId: 'd' }, vars)).not.toBeNull()
    expect(shareLinkIssue({ kind: 'setVariable', variable: 'note' }, vars)).not.toBeNull()
    expect(shareLinkIssue({ kind: 'setVariable', variable: 'host' }, vars)).toBeNull()
    expect(shareLinkIssue({ kind: 'url', template: 'https://x' }, vars)).toBeNull()
  })

  it('labels links without one', () => {
    expect(linkLabel({ kind: 'setVariable', variable: 'host' })).toBe('Filter by host')
    expect(linkLabel({ kind: 'dashboard', dashboardId: 'd' }, 'Hosts')).toBe('Open Hosts')
    expect(linkLabel({ kind: 'url', label: ' Docs ' })).toBe('Docs')
  })

  it('bucket step is the nearest gap', () => {
    expect(bucketStep([0, 60, 120, 300], 1)).toBe(60)
    expect(bucketStep([0, 60, 120, 300], 3)).toBe(180)
    expect(bucketStep([5], 0)).toBe(0)
    expect(bucketWindow(10, 0)).toEqual({ from: 10_000, to: 70_000 })
  })
})
