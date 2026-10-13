import { describe, expect, it, vi } from 'vitest'

vi.mock('svelte-sonner', () => ({ toast: { warning: () => undefined } }))

// routes.ts pulls in lucide-svelte components; the section list is what
// matters here, so stub the icons.
vi.mock('lucide-svelte', () => {
  const names = ['Bookmark', 'Brain', 'LayoutDashboard', 'ScrollText', 'Workflow', 'Boxes', 'Clock', 'Scale', 'HeartPulse', 'Gauge', 'Coins', 'Shield', 'KeyRound']
  return Object.fromEntries(names.map((n) => [n, {}]))
})

import { PAGE_SECTIONS } from '../routes'
import {
  scopeForSection, validateSavedQueryDetails, parseTags, folderOptions, visibilityOptions,
} from './savedQueries'
import type { SavedQueryFolder } from '../types/api'

describe('saved queries sections', () => {
  it('lists All, Mine, Shared and Verified in the sidebar', () => {
    expect((PAGE_SECTIONS['saved-queries'] ?? []).map((s) => s.label)).toEqual(['All', 'Mine', 'Shared', 'Verified'])
  })

  it('maps every section id to the API scope of the same name', () => {
    for (const s of PAGE_SECTIONS['saved-queries'] ?? []) {
      expect(scopeForSection(s.id)).toBe(s.id)
    }
  })

  it('falls back to All for a missing or unknown section', () => {
    expect(scopeForSection(null)).toBe('all')
    expect(scopeForSection(undefined)).toBe('all')
    expect(scopeForSection('starred')).toBe('all')
  })
})

describe('details sheet validation', () => {
  it('requires a name', () => {
    expect(validateSavedQueryDetails({ name: '  ', tags: '' }, false, 'shared')).toBe('Name is required')
    expect(validateSavedQueryDetails({ name: 'Daily', tags: '' }, false, 'shared')).toBeNull()
  })

  it('applies the server tag limits', () => {
    const many = Array.from({ length: 21 }, (_, i) => `t${i}`).join(',')
    expect(validateSavedQueryDetails({ name: 'x', tags: many }, false, 'shared')).toBe('At most 20 tags')
    expect(validateSavedQueryDetails({ name: 'x', tags: 'a'.repeat(41) }, false, 'shared')).toBe('Tags are limited to 40 characters')
  })

  it('lets only admins choose admins visibility, but keeps an existing one', () => {
    expect(validateSavedQueryDetails({ name: 'x', tags: '' }, false, 'admins')).toMatch(/Only admins/)
    expect(validateSavedQueryDetails({ name: 'x', tags: '' }, true, 'admins')).toBeNull()
    expect(validateSavedQueryDetails({ name: 'x', tags: '' }, false, 'admins', 'admins')).toBeNull()
    expect(visibilityOptions(false).map((o) => o.value)).toEqual(['shared', 'private'])
    expect(visibilityOptions(true).map((o) => o.value)).toContain('admins')
  })

  it('splits and de-duplicates tags', () => {
    expect(parseTags(' ops, Ops ,, kpi ')).toEqual(['ops', 'kpi'])
  })
})

describe('folder picker', () => {
  it('lists nested folders as paths under the root', () => {
    const f = (id: string, name: string, parent: string | null): SavedQueryFolder => ({
      id, name, parent_id: parent, created_by: null, created_at: '', updated_at: '',
    })
    const opts = folderOptions([f('b', 'Monthly', 'a'), f('a', 'Finance', null), f('c', 'Adhoc', null)])
    expect(opts.map((o) => o.label)).toEqual(['No folder (root)', 'Adhoc', 'Finance', 'Finance / Monthly'])
    expect(opts[0].value).toBe('')
  })
})
