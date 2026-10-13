import { describe, expect, it } from 'vitest'
import { generateCSV, generateSQLInsert, generateTSV, toSQLLiteral } from './export'

describe('generateCSV formula guard', () => {
  const meta = [
    { name: 'n', type: 'Int64' },
    { name: 's', type: 'String' },
  ]

  it('keeps negative numbers intact', () => {
    const csv = generateCSV(meta, [[-5, '-5'], ['-12345678901234567890', 'x']])
    const lines = csv.split('\n')
    expect(lines[1]).toBe("-5,'-5")
    expect(lines[2]).toBe('-12345678901234567890,x')
  })

  it('still guards formula-like strings', () => {
    const csv = generateCSV(meta, [[1, '=SUM(A1)'], [2, '@cmd'], [3, '+1']])
    expect(csv.split('\n').slice(1)).toEqual(["1,'=SUM(A1)", "2,'@cmd", "3,'+1"])
  })

  it('guards non-numeric text in a numeric column', () => {
    expect(generateCSV(meta, [['-x', '']]).split('\n')[1]).toBe("'-x,")
  })

  it('applies the same rule to TSV', () => {
    expect(generateTSV(meta, [[-1.5, '-1.5']]).split('\n')[1]).toBe("-1.5\t'-1.5")
  })
})

describe('generateSQLInsert', () => {
  it('quotes digit-only strings in String columns and leaves big ints unquoted', () => {
    const sql = generateSQLInsert(
      [{ name: 'zip', type: 'String' }, { name: 'id', type: 'UInt64' }, { name: 'd', type: 'Nullable(Date)' }],
      [['01234', '18446744073709551615', '2024-01-01'], ['5', 7, null]],
    )
    expect(sql).toContain("('01234', 18446744073709551615, '2024-01-01')")
    expect(sql).toContain("('5', 7, NULL)")
  })

  it('renders arrays, maps and tuples as ClickHouse literals', () => {
    expect(toSQLLiteral([1, 2], 'Array(Int32)')).toBe('[1, 2]')
    expect(toSQLLiteral(['a', "it's"], 'Array(String)')).toBe("['a', 'it\\'s']")
    expect(toSQLLiteral([[1], []], 'Array(Array(UInt8))')).toBe('[[1], []]')
    expect(toSQLLiteral({ a: 1, b: 2 }, 'Map(String, UInt64)')).toBe("map('a', 1, 'b', 2)")
    expect(toSQLLiteral({ '1': 'x' }, 'Map(UInt8, String)')).toBe("map(1, 'x')")
    expect(toSQLLiteral([1, 'x'], 'Tuple(UInt8, String)')).toBe("(1, 'x')")
    expect(toSQLLiteral({ a: 1, b: 'x' }, 'Tuple(a UInt8, b String)')).toBe("(1, 'x')")
    expect(toSQLLiteral({ k: ['v'] }, 'Map(String, Array(String))')).toBe("map('k', ['v'])")
  })

  it('handles LowCardinality and Nullable wrappers', () => {
    expect(toSQLLiteral('42', 'LowCardinality(Nullable(String))')).toBe("'42'")
    expect(toSQLLiteral('42', 'Nullable(Int32)')).toBe('42')
  })

  it('falls back to value inference when the type is unknown', () => {
    expect(toSQLLiteral(3)).toBe('3')
    expect(toSQLLiteral('3')).toBe("'3'")
    expect(toSQLLiteral(true)).toBe('true')
    expect(toSQLLiteral([1, 'a'])).toBe("[1, 'a']")
  })
})
