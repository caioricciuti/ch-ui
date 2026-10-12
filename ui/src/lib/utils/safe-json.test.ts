import { describe, it, expect } from 'vitest'
import { safeParse } from './safe-json'

describe('safeParse', () => {
  it('parses ordinary JSON like JSON.parse', () => {
    expect(safeParse<{ a: number; b: string[]; c: null }>('{"a":1.5,"b":["x"],"c":null}')).toEqual({
      a: 1.5,
      b: ['x'],
      c: null,
    })
  })

  it('keeps safe integers as numbers', () => {
    expect(safeParse<{ n: number }>('{"n":9007199254740991}').n).toBe(9007199254740991)
  })

  it('returns integers beyond 2^53 as exact strings', () => {
    const parsed = safeParse<{ id: string; neg: string }>('{"id":816687988383154176,"neg":-816687988383154176}')
    expect(parsed.id).toBe('816687988383154176')
    expect(parsed.neg).toBe('-816687988383154176')
  })

  it('throws on invalid JSON', () => {
    expect(() => safeParse('{not json')).toThrow()
  })
})
