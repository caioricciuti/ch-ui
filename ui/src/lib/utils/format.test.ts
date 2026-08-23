import { describe, it, expect } from 'vitest'
import { formatCompactNumber } from './format'

describe('formatCompactNumber', () => {
  it('leaves small counts alone', () => {
    expect(formatCompactNumber(0)).toBe('0')
    expect(formatCompactNumber(1)).toBe('1')
    expect(formatCompactNumber(999)).toBe('999')
  })

  it('scales large counts with a short suffix', () => {
    expect(formatCompactNumber(1_500)).toBe('1.50K')
    expect(formatCompactNumber(15_990_000)).toBe('15.99M')
    expect(formatCompactNumber(3_020_000_000)).toBe('3.02B')
    expect(formatCompactNumber(1_250_000_000_000)).toBe('1.25T')
  })

  it('handles fractional throughput values', () => {
    expect(formatCompactNumber(2.5)).toBe('2.50')
    expect(formatCompactNumber(3_020_500.5)).toBe('3.02M')
  })
})
