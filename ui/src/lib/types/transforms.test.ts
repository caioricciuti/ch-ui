import { describe, expect, it } from 'vitest'
import {
  TRANSFORM_NODE_TYPES,
  defaultTransformConfig,
  fieldPathProblem,
  isProTransform,
  isTransformType,
  mapPathHint,
  nextRouteId,
  proTransformMessage,
  readFilter,
  readMap,
  readRedact,
  readRoute,
  readSample,
  routeOutputs,
  regexGroupNames,
  regexProblem,
  transformProblems,
  withCustomKind,
} from './transforms'
import { CONNECTOR_FIELDS, newNodeConfig } from './pipelines'

describe('transform node types', () => {
  it('lists the six transforms, sample, redact and route as Pro', () => {
    expect(TRANSFORM_NODE_TYPES.map((t) => t.type)).toEqual(['transform_parse', 'transform_map', 'transform_filter', 'transform_sample', 'transform_redact', 'transform_route'])
    expect(TRANSFORM_NODE_TYPES.filter((t) => t.pro).map((t) => t.type)).toEqual(['transform_sample', 'transform_redact', 'transform_route'])
    expect(isProTransform('transform_parse')).toBe(false)
    expect(isTransformType('source_kafka')).toBe(false)
    expect(isTransformType('transform_map')).toBe(true)
  })

  it('uses the server messages for Pro nodes', () => {
    expect(proTransformMessage('transform_sample')).toBe('Sampling needs CH-UI Pro')
    expect(proTransformMessage('transform_redact')).toBe('Redaction needs CH-UI Pro')
    expect(proTransformMessage('transform_route')).toBe('Routing needs CH-UI Pro')
  })

  it('has no generic connector fields', () => {
    for (const t of TRANSFORM_NODE_TYPES) expect(CONNECTOR_FIELDS[t.type]).toEqual([])
  })
})

describe('defaults', () => {
  it('parses Body into LogAttributes for OTel logs', () => {
    const c = defaultTransformConfig('transform_parse', 'logs')
    expect(c).toMatchObject({ field: 'Body', target: 'LogAttributes', format: 'json', on_error: 'keep', remove_source: false })
  })

  it('parses into the record root otherwise', () => {
    expect(defaultTransformConfig('transform_parse', 'events')).toMatchObject({ field: 'Body', format: 'json', on_error: 'keep', target: '' })
    expect(newNodeConfig('transform_parse', 'p1')).toMatchObject({ target: '' })
  })

  it('newNodeConfig hands transforms their defaults with the signal', () => {
    expect(newNodeConfig('transform_parse', 'p1', 'logs')).toMatchObject({ target: 'LogAttributes' })
    expect(newNodeConfig('transform_redact', 'p1')).toMatchObject({ kinds: ['email', 'credit_card', 'jwt', 'bearer_token', 'aws_key'], replacement: '[REDACTED:{kind}]' })
    expect(newNodeConfig('transform_filter', 'p1')).toMatchObject({ rules: [] })
  })

  it('every default is valid, so a new node saves', () => {
    for (const t of TRANSFORM_NODE_TYPES) {
      for (const sig of ['logs', 'traces', 'events', null]) {
        expect(transformProblems(t.type, defaultTransformConfig(t.type, sig)), `${t.type} ${sig}`).toEqual([])
      }
    }
  })

  it('samples by TraceId for OTel signals', () => {
    expect(defaultTransformConfig('transform_sample', 'traces')).toMatchObject({ key: 'TraceId', keep_percent: 10 })
    expect(defaultTransformConfig('transform_sample', 'events')).toMatchObject({ key: '' })
  })
})

describe('field paths', () => {
  it('points OTel users at dotted map keys', () => {
    expect(mapPathHint('logs')).toContain('LogAttributes.http.method')
    expect(mapPathHint('events')).toBe('')
  })

  it('accepts the contract forms', () => {
    for (const p of ['Body', 'a.b.c', "LogAttributes['http.method']", 'LogAttributes.k', 'LogAttributes.http.method']) expect(fieldPathProblem(p), p).toBe('')
  })

  it('rejects broken paths', () => {
    expect(fieldPathProblem('')).toBe('Enter a field')
    expect(fieldPathProblem(' a')).toMatch(/spaces/)
    expect(fieldPathProblem('a..b')).toMatch(/empty parts/)
    expect(fieldPathProblem('.a')).toMatch(/empty parts/)
    expect(fieldPathProblem("LogAttributes['']")).toMatch(/empty/)
    expect(fieldPathProblem("['k']")).toMatch(/Name the map/)
    expect(fieldPathProblem("a['x']['y']")).toMatch(/one \['key'\]/)
  })
})

describe('regex', () => {
  it('finds RE2 and JS style named groups', () => {
    expect(regexGroupNames('(?P<ip>\\S+) (?<path>/\\S*) (\\d+)')).toEqual(['ip', 'path'])
  })

  it('flags what RE2 lacks and what does not compile', () => {
    expect(regexProblem('')).toBe('Enter a pattern')
    expect(regexProblem('(?=x)a')).toMatch(/Lookarounds/)
    expect(regexProblem('(?<!x)a')).toMatch(/Lookarounds/)
    expect(regexProblem('(a)\\1')).toMatch(/Backreferences/)
    expect(regexProblem('(?P<a>x')).not.toBe('')
    expect(regexProblem('(?P<a>x)')).toBe('')
  })
})

describe('transformProblems', () => {
  it('wants a named group for regex parsing', () => {
    expect(transformProblems('transform_parse', { field: 'Body', format: 'regex', pattern: '\\d+' })).toEqual([
      'Pattern: add at least one named group, e.g. (?P<status>\\d+)',
    ])
  })

  it('checks map rows and the Go layout', () => {
    expect(transformProblems('transform_map', {})).toEqual([])
    const p = transformProblems('transform_map', { rename: [{ from: 'a', to: '' }], timestamp_from: 'ts', timestamp_format: 'layout:' })
    expect(p).toContain('Rename 1 to: Enter a field')
    expect(p).toContain('Timestamp: enter a Go layout, e.g. 2006-01-02 15:04:05')
  })

  it('checks filter rules', () => {
    expect(transformProblems('transform_filter', { rules: [] })).toEqual([])
    const p = transformProblems('transform_filter', {
      rules: [
        { field: 'a', op: 'in', value: 'x' },
        { field: 'b', op: 'matches', value: '(?=y)' },
        { field: 'c', op: 'contains', value: '' },
        { field: 'd', op: 'exists', value: '' },
        { field: 'e', op: 'eq', value: '' },
      ],
    })
    expect(p).toEqual([
      'Filter rule 1: "in list" needs a JSON array, e.g. ["a","b"]',
      'Filter rule 2: Lookarounds are not supported (Go RE2)',
      'Filter rule 3: enter a value',
    ])
  })

  it('bounds the sample percent', () => {
    expect(transformProblems('transform_sample', { keep_percent: 120 })).toEqual(['Keep percent must be between 0 and 100'])
    expect(transformProblems('transform_sample', { keep_percent: 0 })).toEqual([])
  })

  it('wants something to redact', () => {
    expect(transformProblems('transform_redact', { kinds: [] })).toEqual(['Pick at least one kind or add a custom pattern'])
    expect(transformProblems('transform_redact', { kinds: [], custom: [{ name: '', pattern: 'ORD-\\d+' }] })).toEqual(['Custom 1: enter a name'])
  })
})

describe('reading stored configs', () => {
  it('fills gaps and drops junk', () => {
    expect(readMap({ rename: [{ from: 'a' }, 'x'], set: [{ field: 'n', value: 3 }] })).toMatchObject({
      rename: [{ from: 'a', to: '' }],
      set: [{ field: 'n', value: '3' }],
      timestamp_format: 'rfc3339',
    })
    expect(readFilter({ mode: 'nope', rules: [{ field: 'x', op: 'bogus', value: ['a'] }] })).toEqual({
      mode: 'keep',
      match: 'all',
      rules: [{ field: 'x', op: 'eq', value: '["a"]' }],
    })
    expect(readSample({}).always_keep).toEqual({ match: 'any', rules: [] })
    expect(readRedact({ kinds: ['email', 'phone'] }).kinds).toEqual(['email'])
  })

  it('keeps custom in kinds exactly when custom patterns exist', () => {
    const base = readRedact({ kinds: ['email', 'custom'] })
    expect(withCustomKind(base).kinds).toEqual(['email'])
    expect(withCustomKind({ ...base, custom: [{ name: 'order', pattern: 'ORD-\\d+' }] }).kinds).toEqual(['email', 'custom'])
  })
})

describe('route config', () => {
  it('reads a stored config tolerantly', () => {
    expect(readRoute({})).toEqual({ mode: 'first', routes: [], unmatched: 'drop' })
    expect(readRoute({ mode: 'all', unmatched: 'output', routes: [{ id: 'r1', name: 'errors', match: 'any', rules: [{ field: 'level', op: 'eq', value: 'error' }] }, 'junk'] })).toEqual({
      mode: 'all',
      unmatched: 'output',
      routes: [{ id: 'r1', name: 'errors', match: 'any', rules: [{ field: 'level', op: 'eq', value: 'error' }] }],
    })
  })

  it('lists outputs per route, then unmatched when it is an output', () => {
    const c = readRoute({ routes: [{ id: 'r1', name: 'errors' }, { id: 'r2', name: ' ' }], unmatched: 'output' })
    expect(routeOutputs(c)).toEqual([{ handle: 'r1', label: 'errors' }, { handle: 'r2', label: 'r2' }, { handle: 'unmatched', label: 'unmatched' }])
    expect(routeOutputs({ ...c, unmatched: 'drop' }).map((o) => o.handle)).toEqual(['r1', 'r2'])
  })

  it('hands out the first free id', () => {
    expect(nextRouteId([])).toBe('r1')
    expect(nextRouteId([{ id: 'r1' }, { id: 'r3' }])).toBe('r2')
  })

  it('names bad routes', () => {
    // Zero routes and unnamed routes save: every record is unmatched, the handle shows the id.
    expect(transformProblems('transform_route', { routes: [] })).toEqual([])
    const p = transformProblems('transform_route', {
      routes: [
        { id: 'r1', name: 'a', rules: [{ field: '', op: 'eq', value: '' }] },
        { id: 'r1', name: 'a' },
        { id: 'Bad Id', name: '' },
        { id: 'unmatched', name: 'x' },
      ],
    })
    expect(p).toEqual([
      'Route a rule 1: Enter a field',
      'Route a: another route has this name',
      'Route a: duplicate id "r1"',
      'Route 3: invalid id "Bad Id"',
      'Route x: invalid id "unmatched"',
    ])
    const many = Array.from({ length: 17 }, (_, i) => ({ id: `r${i + 1}`, name: `n${i}` }))
    expect(transformProblems('transform_route', { routes: many })).toEqual(['At most 16 routes'])
  })

  it('defaults to an errors route for logs and an unmatched output', () => {
    expect(defaultTransformConfig('transform_route', 'logs')).toEqual({
      mode: 'first',
      routes: [{ id: 'r1', name: 'errors', match: 'any', rules: [{ field: 'SeverityText', op: 'eq', value: 'ERROR' }] }],
      unmatched: 'output',
    })
  })
})
