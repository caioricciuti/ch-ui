import { describe, it, expect } from 'vitest'
import type { ColumnMeta } from '../types/query'
import {
  compareValues,
  sortRows,
  filterRows,
  cycleSort,
  operatorsFor,
  isValidFilterValue,
  isWrappableQuery,
  buildWrappedQuery,
  escapeIdentifier,
  escapeStringLiteral,
} from './result-filters'

const meta: ColumnMeta[] = [
  { name: 'id', type: 'UInt64' },
  { name: 'name', type: 'String' },
  { name: 'score', type: 'Float64' },
  { name: 'created', type: 'DateTime' },
  { name: 'active', type: 'Bool' },
]

describe('cycleSort', () => {
  it('cycles none → asc → desc → none', () => {
    const asc = cycleSort(null, 'id')
    expect(asc).toEqual({ column: 'id', dir: 'asc' })
    const desc = cycleSort(asc, 'id')
    expect(desc).toEqual({ column: 'id', dir: 'desc' })
    expect(cycleSort(desc, 'id')).toBeNull()
  })

  it('switching column restarts at asc', () => {
    expect(cycleSort({ column: 'id', dir: 'desc' }, 'name')).toEqual({ column: 'name', dir: 'asc' })
  })
})

describe('compareValues', () => {
  it('compares numbers numerically', () => {
    expect(compareValues(2, 10, 'UInt32')).toBeLessThan(0)
    expect(compareValues(10.5, 2.1, 'Float64')).toBeGreaterThan(0)
  })

  it('compares Int64/UInt64 string values as BigInt without precision loss', () => {
    // Both exceed Number.MAX_SAFE_INTEGER and differ in the last digit
    expect(compareValues('9007199254740993', '9007199254740992', 'UInt64')).toBeGreaterThan(0)
    expect(compareValues('-9007199254740993', '9007199254740993', 'Int64')).toBeLessThan(0)
  })

  it('compares dates lexicographically (ISO order)', () => {
    expect(compareValues('2024-01-02 00:00:00', '2024-01-10 00:00:00', 'DateTime')).toBeLessThan(0)
  })

  it('compares booleans false < true', () => {
    expect(compareValues(false, true, 'Bool')).toBeLessThan(0)
    expect(compareValues(1, 0, 'Bool')).toBeGreaterThan(0)
  })

  it('compares strings', () => {
    expect(compareValues('apple', 'banana', 'String')).toBeLessThan(0)
  })
})

describe('sortRows', () => {
  const rows: unknown[][] = [
    ['3', 'carol', 1.5, '2024-03-01 00:00:00', true],
    ['1', 'alice', null, '2024-01-01 00:00:00', false],
    ['2', null, 9.9, '2024-02-01 00:00:00', true],
  ]

  it('sorts ascending by numeric column', () => {
    const out = sortRows(rows, meta, { column: 'id', dir: 'asc' })
    expect(out.map((r) => r[0])).toEqual(['1', '2', '3'])
  })

  it('sorts descending', () => {
    const out = sortRows(rows, meta, { column: 'id', dir: 'desc' })
    expect(out.map((r) => r[0])).toEqual(['3', '2', '1'])
  })

  it('keeps NULLs last in both directions (ClickHouse NULLS LAST default)', () => {
    const asc = sortRows(rows, meta, { column: 'name', dir: 'asc' })
    expect(asc.map((r) => r[1])).toEqual(['alice', 'carol', null])
    const desc = sortRows(rows, meta, { column: 'name', dir: 'desc' })
    expect(desc.map((r) => r[1])).toEqual(['carol', 'alice', null])
  })

  it('returns input unchanged for unknown column', () => {
    expect(sortRows(rows, meta, { column: 'nope', dir: 'asc' })).toBe(rows)
  })

  it('does not mutate the input', () => {
    const copy = rows.map((r) => [...r])
    sortRows(rows, meta, { column: 'id', dir: 'asc' })
    expect(rows).toEqual(copy)
  })
})

describe('filterRows', () => {
  const rows: unknown[][] = [
    ['1', 'Alice Johnson', 5.5, '2024-01-01 00:00:00', true],
    ['2', 'bob smith', 2.0, '2024-02-01 00:00:00', false],
    ['3', null, 9.9, '2024-03-01 00:00:00', true],
  ]

  it('contains is case-insensitive and excludes NULLs', () => {
    const out = filterRows(rows, meta, [{ column: 'name', operator: 'contains', value: 'ALICE' }])
    expect(out.map((r) => r[0])).toEqual(['1'])
  })

  it('notContains excludes matches and NULLs', () => {
    const out = filterRows(rows, meta, [{ column: 'name', operator: 'notContains', value: 'smith' }])
    expect(out.map((r) => r[0])).toEqual(['1'])
  })

  it('eq on numeric column coerces the value', () => {
    const out = filterRows(rows, meta, [{ column: 'score', operator: 'eq', value: '2' }])
    expect(out.map((r) => r[0])).toEqual(['2'])
  })

  it('gt/lte compare numerically', () => {
    expect(filterRows(rows, meta, [{ column: 'score', operator: 'gt', value: '5' }]).map((r) => r[0])).toEqual(['1', '3'])
    expect(filterRows(rows, meta, [{ column: 'score', operator: 'lte', value: '5.5' }]).map((r) => r[0])).toEqual(['1', '2'])
  })

  it('isNull / isNotNull', () => {
    expect(filterRows(rows, meta, [{ column: 'name', operator: 'isNull', value: '' }]).map((r) => r[0])).toEqual(['3'])
    expect(filterRows(rows, meta, [{ column: 'name', operator: 'isNotNull', value: '' }]).map((r) => r[0])).toEqual(['1', '2'])
  })

  it('eq on bool column accepts true/1', () => {
    const out = filterRows(rows, meta, [{ column: 'active', operator: 'eq', value: 'true' }])
    expect(out.map((r) => r[0])).toEqual(['1', '3'])
  })

  it('AND-combines multiple filters', () => {
    const out = filterRows(rows, meta, [
      { column: 'score', operator: 'gt', value: '1' },
      { column: 'active', operator: 'eq', value: 'true' },
    ])
    expect(out.map((r) => r[0])).toEqual(['1', '3'])
  })

  it('ignores filters on unknown columns', () => {
    expect(filterRows(rows, meta, [{ column: 'nope', operator: 'eq', value: 'x' }])).toEqual(rows)
  })
})

describe('operatorsFor', () => {
  it('gives comparison operators to numbers and dates', () => {
    expect(operatorsFor('UInt64')).toContain('gte')
    expect(operatorsFor('Nullable(DateTime)')).toContain('lt')
    expect(operatorsFor('UInt64')).not.toContain('contains')
  })

  it('gives contains to strings', () => {
    expect(operatorsFor('String')).toContain('contains')
    expect(operatorsFor('LowCardinality(String)')).toContain('contains')
  })
})

describe('isValidFilterValue', () => {
  it('requires numeric values on numeric columns', () => {
    expect(isValidFilterValue('UInt64', 'gt', 'abc')).toBe(false)
    expect(isValidFilterValue('UInt64', 'gt', '42')).toBe(true)
    expect(isValidFilterValue('Nullable(Float64)', 'eq', '-1.5')).toBe(true)
    expect(isValidFilterValue('Float64', 'eq', '1e3')).toBe(true)
  })

  it('accepts anything for strings, dates, and unary operators', () => {
    expect(isValidFilterValue('String', 'contains', 'abc')).toBe(true)
    expect(isValidFilterValue('DateTime', 'gte', '2024-01-01')).toBe(true)
    expect(isValidFilterValue('UInt64', 'isNull', '')).toBe(true)
  })
})

describe('escaping', () => {
  it('escapes identifiers with backticks', () => {
    expect(escapeIdentifier('simple')).toBe('`simple`')
    expect(escapeIdentifier('we`ird')).toBe('`we\\`ird`')
    expect(escapeIdentifier('back\\slash')).toBe('`back\\\\slash`')
  })

  it('escapes string literals', () => {
    expect(escapeStringLiteral("o'brien")).toBe("'o\\'brien'")
    expect(escapeStringLiteral('a\\b')).toBe("'a\\\\b'")
  })
})

describe('isWrappableQuery', () => {
  it('accepts SELECT and WITH, including leading comments and trailing semicolons', () => {
    expect(isWrappableQuery('SELECT 1')).toBe(true)
    expect(isWrappableQuery('  select * from t;  ')).toBe(true)
    expect(isWrappableQuery('WITH x AS (SELECT 1) SELECT * FROM x')).toBe(true)
    expect(isWrappableQuery('-- comment\nSELECT 1')).toBe(true)
    expect(isWrappableQuery('/* block */ SELECT 1')).toBe(true)
  })

  it('rejects non-SELECT, multi-statement, FORMAT and INTO OUTFILE', () => {
    expect(isWrappableQuery('INSERT INTO t VALUES (1)')).toBe(false)
    expect(isWrappableQuery('SELECT 1; SELECT 2')).toBe(false)
    expect(isWrappableQuery('SELECT * FROM t FORMAT JSONEachRow')).toBe(false)
    expect(isWrappableQuery("SELECT 1 INTO OUTFILE 'f.csv'")).toBe(false)
    expect(isWrappableQuery('')).toBe(false)
  })
})

describe('buildWrappedQuery', () => {
  it('wraps with marker, WHERE, ORDER BY and LIMIT', () => {
    const sql = buildWrappedQuery(
      'SELECT * FROM events;',
      [{ column: 'name', operator: 'eq', value: "o'brien" }],
      { column: 'id', dir: 'desc' },
      meta,
      1000,
    )
    expect(sql).toContain('/* ch-ui:result-filter */')
    expect(sql).toContain('SELECT * FROM (\nSELECT * FROM events\n) AS __chui_src')
    expect(sql).toContain("WHERE `name` = 'o\\'brien'")
    expect(sql).toContain('ORDER BY `id` DESC')
    expect(sql).toContain('LIMIT 1000')
  })

  it('omits WHERE/ORDER BY when absent', () => {
    const sql = buildWrappedQuery('SELECT 1', [], null, meta, 50)
    expect(sql).not.toContain('WHERE')
    expect(sql).not.toContain('ORDER BY')
    expect(sql).toContain('LIMIT 50')
  })

  it('uses bare numeric literals on numeric columns', () => {
    const sql = buildWrappedQuery('SELECT 1', [{ column: 'id', operator: 'gte', value: '42' }], null, meta, 10)
    expect(sql).toContain('WHERE `id` >= 42')
  })

  it('uses positionCaseInsensitive for contains', () => {
    const sql = buildWrappedQuery('SELECT 1', [{ column: 'name', operator: 'contains', value: 'foo' }], null, meta, 10)
    expect(sql).toContain("positionCaseInsensitive(toString(`name`), 'foo') > 0")
  })

  it('handles NULL operators', () => {
    const sql = buildWrappedQuery('SELECT 1', [{ column: 'name', operator: 'isNull', value: '' }], null, meta, 10)
    expect(sql).toContain('WHERE `name` IS NULL')
  })

  it('AND-combines filters and quotes date comparisons', () => {
    const sql = buildWrappedQuery(
      'SELECT 1',
      [
        { column: 'created', operator: 'gte', value: '2024-01-01' },
        { column: 'active', operator: 'eq', value: 'true' },
      ],
      null,
      meta,
      10,
    )
    expect(sql).toContain("WHERE `created` >= '2024-01-01' AND `active` = true")
  })
})
