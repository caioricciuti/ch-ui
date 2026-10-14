import { describe, expect, it } from 'vitest'
import { firstKeyword, maskNonCode, splitStatements, statementAt } from './statements'

const texts = (sql: string) => splitStatements(sql).map((s) => s.text)

describe('splitStatements', () => {
  it('returns one statement when there is no separator', () => {
    expect(texts('SELECT 1')).toEqual(['SELECT 1'])
    expect(texts('  SELECT 1 ;  ')).toEqual(['SELECT 1'])
  })

  it('splits on semicolons and keeps document offsets', () => {
    const sql = 'SELECT 1;\n  SELECT 2;'
    const st = splitStatements(sql)
    expect(st.map((s) => s.text)).toEqual(['SELECT 1', 'SELECT 2'])
    expect(sql.slice(st[1].from, st[1].to)).toBe('SELECT 2')
  })

  it('ignores semicolons inside strings, identifiers, heredocs and comments', () => {
    const sql = [
      "SELECT 'a;b', 'it\\'s;', 'x'';y'",
      'SELECT `c;d`, "e;f"',
      'SELECT $$g;h$$, $tag$i;j$tag$',
      'SELECT 1 -- k;l\n+ 1',
      'SELECT 2 /* m; /* nested; */ n; */',
      'SELECT 3 # o;p\n',
    ].join(';\n')
    expect(texts(sql)).toEqual([
      "SELECT 'a;b', 'it\\'s;', 'x'';y'",
      'SELECT `c;d`, "e;f"',
      'SELECT $$g;h$$, $tag$i;j$tag$',
      'SELECT 1 -- k;l\n+ 1',
      'SELECT 2 /* m; /* nested; */ n; */',
      'SELECT 3 # o;p',
    ])
  })

  it('drops a trailing comment after the only statement (what Run sends)', () => {
    const sql = '  SELECT 1; -- note\n'
    const st = splitStatements(sql)
    expect(st.map((s) => s.text)).toEqual(['SELECT 1'])
    expect(st[0].from).toBe(2)
    expect(texts("SELECT '--x'; # c")).toEqual(["SELECT '--x'"])
  })

  it('drops empty and comment-only statements', () => {
    expect(texts(';;SELECT 1;\n-- trailing note\n;  ')).toEqual(['SELECT 1'])
    expect(texts('/* only */')).toEqual([])
  })

  it('does not treat a lone # as a comment', () => {
    expect(texts('SELECT a#b; SELECT 2')).toEqual(['SELECT a#b', 'SELECT 2'])
  })

  it('handles unterminated strings and comments without throwing', () => {
    expect(texts("SELECT 'abc; SELECT 2")).toEqual(["SELECT 'abc; SELECT 2"])
    expect(texts('SELECT 1 /* open; SELECT 2')).toEqual(['SELECT 1 /* open; SELECT 2'])
  })
})

describe('statementAt', () => {
  const sql = 'SELECT 1;\n\nSELECT 22;\nSELECT 333'
  const st = splitStatements(sql)

  it('finds the statement containing the cursor', () => {
    expect(statementAt(st, 3)?.text).toBe('SELECT 1')
    expect(statementAt(st, sql.indexOf('22'))?.text).toBe('SELECT 22')
    expect(statementAt(st, sql.length)?.text).toBe('SELECT 333')
  })

  it('falls back to the statement before the cursor', () => {
    expect(statementAt(st, sql.indexOf(';') + 1)?.text).toBe('SELECT 1')
    expect(statementAt(st, 10)?.text).toBe('SELECT 1')
  })

  it('returns null for an empty list', () => {
    expect(statementAt([], 0)).toBeNull()
  })
})

describe('maskNonCode', () => {
  it('blanks strings and comments but keeps offsets', () => {
    const sql = "SELECT '{a:String}' -- {b:Int8}\n, {c:UInt8}"
    const masked = maskNonCode(sql)
    expect(masked.length).toBe(sql.length)
    expect(masked).not.toContain('{a:')
    expect(masked).not.toContain('{b:')
    expect(masked).toContain('{c:UInt8}')
  })
})

describe('firstKeyword', () => {
  it('skips comments and upper-cases', () => {
    expect(firstKeyword('/* x */ -- y\n select 1')).toBe('SELECT')
    expect(firstKeyword("'SELECT'")).toBe('')
    expect(firstKeyword('')).toBe('')
  })
})
