import { describe, expect, it } from 'vitest'
import { safeNextPath } from './client'

const origin = 'https://ch-ui.example.com'

describe('safeNextPath', () => {
  it('keeps a same-origin path and query', () => {
    expect(safeNextPath('/dashboards/abc?range=1h', origin)).toBe('/dashboards/abc?range=1h')
    expect(safeNextPath('/?tab=123', origin)).toBe('/?tab=123')
  })

  it('drops the fragment', () => {
    expect(safeNextPath('/admin?section=users#top', origin)).toBe('/admin?section=users')
  })

  it.each([
    null,
    '',
    'https://evil.example/x',
    '//evil.example/x',
    '/\\evil.example',
    'javascript:alert(1)',
    'dashboards',
    '/login',
    '/login?next=/x',
    '/\u0000x',
    '/' + 'a'.repeat(3000),
  ])('rejects %s', (raw) => {
    expect(safeNextPath(raw, origin)).toBeNull()
  })
})
