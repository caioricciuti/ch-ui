import { describe, expect, it } from 'vitest'
import { fixRange, fixTarget, locateExecuted } from './fix-with-ai'

const err47 = "Code: 47. DB::Exception: Missing columns: 'nme' while processing query. (UNKNOWN_IDENTIFIER)"

describe('fix with AI anchoring', () => {
  const q = 'SELECT nme FROM users'
  const view = { executedSql: q, baseSql: q, executedFrom: 0 }

  it('offers a fix for a repairable error on a plain editor run', () => {
    expect(fixTarget({ pro: true, error: err47, doc: q, view })).toEqual({ from: 0, sql: q })
  })

  it('needs Pro, a repairable code and the plain editor text', () => {
    expect(fixTarget({ pro: false, error: err47, doc: q, view })).toBeNull()
    expect(fixTarget({ pro: true, error: 'Code: 241. DB::Exception: Memory limit exceeded. (MEMORY_LIMIT_EXCEEDED)', doc: q, view })).toBeNull()
    expect(fixTarget({ pro: true, error: 'Query cancelled', doc: q, view })).toBeNull()
    // A wrapped grid re-query is not the editor text.
    const wrapped = { ...view, executedSql: `/* ch-ui:result-filter */ SELECT * FROM (${q})` }
    expect(fixTarget({ pro: true, error: err47, doc: q, view: wrapped })).toBeNull()
  })

  it('anchors at the run offset when the text is duplicated', () => {
    const doc = `${q};\n${q};`
    const second = { ...view, executedFrom: q.length + 2 }
    expect(fixTarget({ pro: true, error: err47, doc, view: second })?.from).toBe(q.length + 2)
    // Edited since the run and ambiguous: no fix (Go to error still searches).
    const moved = { ...view, executedFrom: 5 }
    expect(fixTarget({ pro: true, error: err47, doc, view: moved })).toBeNull()
    expect(locateExecuted(doc, moved)).toBe(0)
  })

  it('hides the fix once the executed text is gone', () => {
    expect(fixTarget({ pro: true, error: err47, doc: 'SELECT name FROM users', view })).toBeNull()
  })

  it('finds a single moved copy', () => {
    const doc = `-- note\n${q}`
    expect(fixTarget({ pro: true, error: err47, doc, view })?.from).toBe(8)
  })
})

describe('fix with AI accept', () => {
  it('replaces exactly the executed statement', () => {
    const doc = 'SELECT 1;\nSELECT nme FROM users;\nSELECT 2;'
    const original = 'SELECT nme FROM users'
    const from = doc.indexOf(original)
    const range = fixRange(doc, from, original)
    expect(range).toEqual({ from, to: from + original.length })
    if (!range) return
    const next = doc.slice(0, range.from) + 'SELECT name FROM users' + doc.slice(range.to)
    expect(next).toBe('SELECT 1;\nSELECT name FROM users;\nSELECT 2;')
  })

  it('refuses when the text changed while the sheet was open', () => {
    expect(fixRange('SELECT name FROM users', 0, 'SELECT nme FROM users')).toBeNull()
    expect(fixRange('SELECT 1', -1, 'SELECT 1')).toBeNull()
  })
})
