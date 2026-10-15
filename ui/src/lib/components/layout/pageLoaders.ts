import type { Component } from 'svelte'
import type { PageRoute } from '../../routes'

/** Pages take no props, except Dashboards, which takes the open dashboard. */
export type PlainPageComponent = Component<Record<string, never>>
export type DashboardsComponent = Component<{ dashboardId?: string }>

export type LoadedPage =
  | { kind: 'dashboards'; component: DashboardsComponent }
  | { kind: 'page'; component: PlainPageComponent }

type PlainRoute = Exclude<PageRoute, 'dashboards'>

// One dynamic import per page, so each page (and what only it uses: the
// pipeline canvas, chart libraries, markdown) is its own chunk.
const loadDashboards = (): Promise<{ default: DashboardsComponent }> => import('../../../pages/Dashboards.svelte')

const LOADERS: Record<PlainRoute, () => Promise<{ default: PlainPageComponent }>> = {
  'saved-queries': () => import('../../../pages/SavedQueries.svelte'),
  telemetry: () => import('../../../pages/Telemetry.svelte'),
  pipelines: () => import('../../../pages/Pipelines.svelte'),
  models: () => import('../../../pages/Models.svelte'),
  schedules: () => import('../../../pages/Schedules.svelte'),
  brain: () => import('../../../pages/Brain.svelte'),
  lineage: () => import('../../../pages/Lineage.svelte'),
  governance: () => import('../../../pages/Governance.svelte'),
  'cluster-health': () => import('../../../pages/ClusterHealth.svelte'),
  'query-insights': () => import('../../../pages/QueryInsights.svelte'),
  'cost-center': () => import('../../../pages/CostCenter.svelte'),
  performance: () => import('../../../pages/Performance.svelte'),
  fleet: () => import('../../../pages/Fleet.svelte'),
  'schema-compare': () => import('../../../pages/SchemaCompare.svelte'),
  'operations-reports': () => import('../../../pages/OperationsReports.svelte'),
  'incident-timeline': () => import('../../../pages/IncidentTimeline.svelte'),
  admin: () => import('../../../pages/Admin.svelte'),
  settings: () => import('../../../pages/Settings.svelte'),
}

const pending = new Map<PageRoute, Promise<LoadedPage>>()
const loaded = new Map<PageRoute, LoadedPage>()

function importPage(route: PageRoute): Promise<LoadedPage> {
  if (route === 'dashboards') {
    return loadDashboards().then((m) => ({ kind: 'dashboards', component: m.default }))
  }
  return LOADERS[route]().then((m) => ({ kind: 'page', component: m.default }))
}

/** The page if it has already loaded, for a render without a spinner. */
export function loadedPage(route: PageRoute): LoadedPage | undefined {
  return loaded.get(route)
}

/** Loads a page once; later calls share the same promise. A failed load can be retried. */
export function loadPage(route: PageRoute): Promise<LoadedPage> {
  const existing = pending.get(route)
  if (existing) return existing
  const p = importPage(route).then(
    (page) => {
      loaded.set(route, page)
      return page
    },
    (e: unknown) => {
      pending.delete(route)
      throw e
    },
  )
  pending.set(route, p)
  return p
}

/** Start loading a page ahead of a click (hover, focus). Errors are ignored here. */
export function prefetchPage(route: PageRoute): void {
  loadPage(route).catch(() => {})
}
