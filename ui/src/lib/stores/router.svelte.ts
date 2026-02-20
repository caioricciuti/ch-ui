import type { SingletonTab } from './tabs.svelte'
import { openDashboardTab, openHomeTab, openSingletonTab } from './tabs.svelte'

// ── URL ↔ Tab mapping ────────────────────────────────────────────

const TAB_PATHS: Record<string, string> = {
  'home': '/',
  'saved-queries': '/saved-queries',
  'dashboards': '/dashboards',
  'schedules': '/schedules',
  'brain': '/brain',
  'admin': '/admin',
  'governance': '/governance',
  'settings': '/license',
}

const PATH_TABS: Record<string, { type: SingletonTab['type']; label: string }> = {
  '/saved-queries': { type: 'saved-queries', label: 'Saved Queries' },
  '/dashboards': { type: 'dashboards', label: 'Dashboards' },
  '/schedules': { type: 'schedules', label: 'Schedules' },
  '/brain': { type: 'brain', label: 'Brain' },
  '/admin': { type: 'admin', label: 'Admin' },
  '/governance': { type: 'governance', label: 'Governance' },
  '/settings': { type: 'settings', label: 'License' },
  '/license': { type: 'settings', label: 'License' },
}

// Prevents pushState during popstate-triggered tab activation
let suppressPush = false

// ── Push helpers ─────────────────────────────────────────────────

export function pushTabRoute(tabType: string): void {
  if (suppressPush) return
  const path = TAB_PATHS[tabType] ?? '/'
  if (window.location.pathname !== path) {
    history.pushState(null, '', path)
  }
}

export function pushTabRouteForTab(tab: { type: string; dashboardId?: string }): void {
  if (suppressPush) return
  if (tab.type === 'dashboard' && tab.dashboardId) {
    const path = `/dashboards/${tab.dashboardId}`
    if (window.location.pathname !== path) {
      history.pushState(null, '', path)
    }
    return
  }
  pushTabRoute(tab.type)
}

export function pushDashboardDetail(id: string): void {
  if (suppressPush) return
  const path = '/dashboards/' + id
  if (window.location.pathname !== path) {
    history.pushState(null, '', path)
  }
}

export function pushDashboardList(): void {
  if (suppressPush) return
  if (window.location.pathname !== '/dashboards') {
    history.pushState(null, '', '/dashboards')
  }
}

// ── Parse current URL ───────────────────────────────────────────

export function parseRoute(): { type: string; dashboardId?: string } {
  const path = window.location.pathname

  // /dashboards/:id
  const dashMatch = path.match(/^\/dashboards\/(.+)$/)
  if (dashMatch) {
    return { type: 'dashboard', dashboardId: dashMatch[1] }
  }

  // Known singleton paths
  const entry = PATH_TABS[path]
  if (entry) {
    return { type: entry.type }
  }

  // Default: home (query editor)
  return { type: 'home' }
}

// ── Sync URL → tab state ────────────────────────────────────────

function syncRouteToTabs(): void {
  const route = parseRoute()
  if (route.type === 'home') {
    openHomeTab()
    return
  }
  if (route.type === 'dashboard' && route.dashboardId) {
    suppressPush = true
    openDashboardTab(route.dashboardId, 'Dashboard')
    suppressPush = false
    return
  }
  const entry = PATH_TABS[TAB_PATHS[route.type]]
  if (entry) {
    suppressPush = true
    openSingletonTab(entry.type, entry.label)
    suppressPush = false
  }
}

// ── Initialize ──────────────────────────────────────────────────

let initialized = false

export function initRouter(): void {
  if (initialized) return
  initialized = true

  // Sync initial URL → tabs
  syncRouteToTabs()

  // Handle browser back/forward
  window.addEventListener('popstate', () => {
    syncRouteToTabs()
  })
}
