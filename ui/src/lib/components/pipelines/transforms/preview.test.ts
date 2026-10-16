import { describe, expect, it } from 'vitest'
import { alignOutputs, buildRows, diffRecords, flatten, parseSampleRecords, stepInput, valueText } from './preview'

describe('parseSampleRecords', () => {
  it('reads one object per line, skipping blank lines', () => {
    expect(parseSampleRecords('{"a":1}\n\n {"b":2} \n')).toEqual({ records: [{ a: 1 }, { b: 2 }], error: '' })
  })

  it('reads a JSON array', () => {
    expect(parseSampleRecords('[{"a":1},{"a":2}]').records).toHaveLength(2)
  })

  it('names the bad line', () => {
    expect(parseSampleRecords('{"a":1}\n{oops}').error).toMatch(/^Line 2 is not valid JSON/)
    expect(parseSampleRecords('{"a":1}\n[1]').error).toBe('Record 2 is not a JSON object')
    expect(parseSampleRecords('').error).toBe('Paste at least one JSON record')
  })

  it('caps the count', () => {
    const text = Array.from({ length: 101 }, () => '{}').join('\n')
    expect(parseSampleRecords(text).error).toBe('At most 100 records; got 101')
  })
})

describe('flatten and diff', () => {
  it('flattens nested objects to dot paths', () => {
    expect(flatten({ a: { b: 1, c: { d: 'x' } }, e: [1], f: {} })).toEqual({ 'a.b': 1, 'a.c.d': 'x', e: [1], f: {} })
  })

  it('marks added, changed, removed and same fields', () => {
    const d = diffRecords(
      { Body: 'level=info', LogAttributes: { k: 'v' }, drop: 1 },
      { Body: 'level=info', LogAttributes: { k: 'v2', level: 'info' } },
    )
    expect(d.map((f) => [f.path, f.change])).toEqual([
      ['Body', 'same'],
      ['LogAttributes.k', 'changed'],
      ['LogAttributes.level', 'added'],
      ['drop', 'removed'],
    ])
  })
})

describe('alignOutputs', () => {
  it('uses the server mapping when given', () => {
    expect(alignOutputs([{}, {}, {}], [{}], [2])).toEqual([2])
  })

  it('uses the dropped indexes over the heuristic', () => {
    // Identical records: only drops can say which one went.
    const input = [{ a: 1 }, { a: 1 }, { a: 1 }]
    expect(alignOutputs(input, [{ a: 1 }, { a: 1 }], null, [1])).toEqual([0, 2])
    expect(alignOutputs(input, [{ a: 1 }, { a: 1 }], null, [0])).toEqual([1, 2])
  })

  it('maps one to one when nothing was dropped', () => {
    expect(alignOutputs([{ a: 1 }, { a: 2 }], [{ a: 9 }, { a: 8 }])).toEqual([0, 1])
  })

  it('finds which inputs survived a filter', () => {
    const input = [{ id: 1, lvl: 'debug' }, { id: 2, lvl: 'error' }, { id: 3, lvl: 'debug' }, { id: 4, lvl: 'warn' }]
    expect(alignOutputs(input, [input[1], input[3]])).toEqual([1, 3])
  })

  it('keeps order when outputs were also changed', () => {
    const input = [{ id: 1, msg: 'a' }, { id: 2, msg: 'b' }, { id: 3, msg: 'c' }]
    const output = [{ id: 1, msg: 'A' }, { id: 3, msg: 'C' }]
    expect(alignOutputs(input, output)).toEqual([0, 2])
  })
})

describe('buildRows', () => {
  const input = [{ id: 1, Body: 'x' }, { id: 2, Body: 'bad' }, { id: 3, Body: 'y' }]

  it('marks dropped records with the error that dropped them', () => {
    const rows = buildRows(input, { node_id: 'p', output: [{ id: 1, Body: 'x', a: 1 }, { id: 3, Body: 'y', a: 2 }], dropped: 1, errors: [{ index: 1, message: 'invalid character' }] }, 'transform_parse')
    expect(rows.map((r) => [r.index, r.dropped, r.reason])).toEqual([
      [0, false, ''],
      [1, true, 'Parse failed: invalid character'],
      [2, false, ''],
    ])
    expect(rows[0].fields.find((f) => f.path === 'a')?.change).toBe('added')
  })

  it('strikes the records drops names, with its reason', () => {
    const step = { node_id: 'f', output: [input[1], input[2]], dropped: 1, drops: [{ index: 0, reason: 'filter: did not match the keep rules' }] }
    const rows = buildRows(input, step, 'transform_filter')
    expect(rows.map((r) => [r.dropped, r.reason])).toEqual([[true, 'filter: did not match the keep rules'], [false, ''], [false, '']])
  })

  it('prefers the server reason, then a per type default', () => {
    const step = { node_id: 'f', output: [input[0]], dropped: 2, kept: [0], drops: [{ index: 1, reason: 'Body eq bad' }] }
    const rows = buildRows(input, step, 'transform_filter')
    expect(rows.map((r) => r.reason)).toEqual(['', 'Body eq bad', 'Filtered out'])
    expect(buildRows(input, { node_id: 's', output: [], dropped: 3 }, 'transform_sample')[0].reason).toBe('Not sampled')
  })

  it('keeps a record with a kept parse error', () => {
    const rows = buildRows([{ Body: 'oops' }], { node_id: 'p', output: [{ Body: 'oops' }], dropped: 0, errors: [{ index: 0, message: 'no match' }] }, 'transform_parse')
    expect(rows[0]).toMatchObject({ dropped: false, error: 'no match' })
  })
})

describe('stepInput', () => {
  it('is the request input for the first step and the previous output after', () => {
    const res = { input: [{ a: 1 }, { a: 2 }], steps: [{ node_id: 'x', output: [{ a: 2 }], dropped: 1 }, { node_id: 'y', output: [], dropped: 1 }] }
    expect(stepInput(res, 0)).toEqual(res.input)
    expect(stepInput(res, 1)).toEqual([{ a: 2 }])
  })
})

describe('valueText', () => {
  it('prints values for cells', () => {
    expect(valueText('x')).toBe('x')
    expect(valueText(null)).toBe('null')
    expect(valueText(undefined)).toBe('')
    expect(valueText({ a: 1 })).toBe('{"a":1}')
  })
})
