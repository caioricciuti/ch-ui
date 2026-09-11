import { describe, it, expect, vi } from 'vitest'

// The module registers a DOMPurify hook at import time, which needs a DOM;
// these tests only exercise the pure highlighter, so stub the sanitizer.
vi.mock('dompurify', () => ({ default: { addHook: () => {}, sanitize: (html: string) => html } }))

import { highlightSQL } from './brain-markdown'

describe('highlightSQL', () => {
  it('escapes markup between tokens, not only inside them', () => {
    const out = highlightSQL('SELECT 1 <img src=x onerror=alert(1)>')
    expect(out).not.toContain('<img')
    expect(out).toContain('&lt;img src=x onerror=alert(')
    expect(out).toContain(')&gt;')
    // the only raw tags left are the highlighter's own spans
    expect(out.replace(/<\/?span[^>]*>/g, '')).not.toMatch(/[<>]/)
  })

  it('escapes operators and comparison characters', () => {
    const out = highlightSQL("SELECT * FROM t WHERE a < b AND c > 'x'")
    expect(out).toContain('&lt;')
    expect(out).toContain('&gt;')
    expect(out).not.toMatch(/[^&]lt;|<(?!\/?span)/)
  })

  it('still highlights keywords, strings, numbers and comments', () => {
    const out = highlightSQL("SELECT count() FROM t WHERE x = 'y' -- note\nLIMIT 10")
    expect(out).toContain('font-semibold">SELECT</span>')
    expect(out).toContain(`text-success">'y'</span>`)
    expect(out).toContain('italic">-- note</span>')
    expect(out).toContain('text-warning">10</span>')
  })

  it('round-trips text with no tokens at all', () => {
    expect(highlightSQL('<>&"')).toBe('&lt;&gt;&amp;&quot;')
  })
})
