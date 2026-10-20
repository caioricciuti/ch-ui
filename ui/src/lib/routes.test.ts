import { describe, expect, it, vi } from 'vitest'

// routes.ts pulls in lucide-svelte components; stub the icons.
vi.mock('lucide-svelte', () => {
  const names = ['Bookmark', 'Brain', 'LayoutDashboard', 'ScrollText', 'Workflow', 'Boxes', 'Clock', 'Scale', 'HeartPulse', 'Gauge', 'Coins', 'Shield', 'KeyRound', 'GitFork']
  return Object.fromEntries(names.map((n) => [n, {}]))
})

import { NAV_GROUPS, PAGE_ROUTES, PATH_TO_PAGE, navBlocks, resolvePathAlias, type NavGroup } from './routes'

const operate = NAV_GROUPS.find((g) => g.id === 'operate') as NavGroup

describe('resolvePathAlias', () => {
  it('sends /license and /settings to the Instance page', () => {
    expect(PAGE_ROUTES.settings.path).toBe('/instance')
    expect(PAGE_ROUTES.settings.label).toBe('Instance')
    expect(resolvePathAlias('/license')).toBe('/instance')
    expect(resolvePathAlias('/settings')).toBe('/instance')
    expect(PATH_TO_PAGE['/instance']).toBe('settings')
  })

  it('leaves current and unknown paths alone', () => {
    expect(resolvePathAlias('/instance')).toBeNull()
    expect(resolvePathAlias('/governance')).toBeNull()
    expect(resolvePathAlias('/toString')).toBeNull()
  })
})

describe('Operate subgroups', () => {
  it('cover every Operate route exactly once, in order', () => {
    expect(operate.subgroups?.map((s) => s.label)).toEqual(['Health', 'Performance', 'Control'])
    expect(operate.subgroups?.flatMap((s) => s.routes)).toEqual(operate.routes)
    expect(new Set(operate.routes).size).toBe(8)
  })

  it('put each page under its subhead', () => {
    expect(operate.subgroups?.map((s) => s.routes)).toEqual([
      ['fleet', 'cluster-health'],
      ['performance', 'query-insights', 'cost-center'],
      ['governance', 'incident-timeline', 'operations-reports'],
    ])
  })
})

describe('navBlocks', () => {
  it('returns one unlabelled block for a group without subgroups', () => {
    const build = NAV_GROUPS.find((g) => g.id === 'build') as NavGroup
    expect(navBlocks(build, () => true)).toEqual([{ label: null, routes: build.routes }])
  })

  it('filters routes and drops subgroups left empty', () => {
    const hidden = new Set(['fleet', 'cluster-health', 'incident-timeline'])
    expect(navBlocks(operate, (r) => !hidden.has(r))).toEqual([
      { label: 'Performance', routes: ['performance', 'query-insights', 'cost-center'] },
      { label: 'Control', routes: ['governance', 'operations-reports'] },
    ])
  })
})
