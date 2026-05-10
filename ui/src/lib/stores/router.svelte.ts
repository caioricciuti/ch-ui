import { withBase, stripBase } from '../basePath'
import type { SingletonTab } from './tabs.svelte'
import { getActiveTab, getTabs, openDashboardTab, openHomeTab, openSingletonTab, setActiveTab } from './tabs.svelte'

// ── URL ↔ Tab mapping ────────────────────────────────────────────

const TAB_PATHS: Record<string, string> = {
  'home': '/',
  'saved-queries': '/saved-queries',
  'dashboards': '/dashboards',
  'schedules': '/schedules',
  'brain': '/brain',
  'admin': '/admin',
  'governance': '/governance',
  'pipelines': '/pipelines',
  'models': '/models',
  'model': '/models',
  'telemetry': '/telemetry',
  'settings': '/license',
}

const PATH_TABS: Record<string, { type: SingletonTab['type']; label: string }> = {
  '/saved-queries': { type: 'saved-queries', label: 'Saved Queries' },
  '/dashboards': { type: 'dashboards', label: 'Dashboards' },
  '/schedules': { type: 'schedules', label: 'Schedules' },
  '/brain': { type: 'brain', label: 'Brain' },
  '/admin': { type: 'admin', label: 'Admin' },
  '/governance': { type: 'governance', label: 'Governance' },
  '/pipelines': { type: 'pipelines', label: 'Pipelines' },
  '/models': { type: 'models', label: 'Models' },
  '/telemetry': { type: 'telemetry', label: 'Telemetry' },
  '/settings': { type: 'settings', label: 'License' },
  '/license': { type: 'settings', label: 'License' },
}

// Prevents pushState during popstate-triggered tab activation
let suppressPush = false

// ── Pipeline sub-route state ─────────────────────────────────────

let pipelineId = $state<string | undefined>(undefined)

export function getCurrentPipelineId(): string | undefined {
  return pipelineId
}

// ── URL helpers ──────────────────────────────────────────────────

function buildUrl(path: string, tabId?: string): string {
  const fullPath = withBase(path)
  if (tabId) return `${fullPath}?tab=${tabId}`
  return fullPath
}

function currentTabParam(): string | null {
  return new URLSearchParams(window.location.search).get('tab')
}

function pushUrl(path: string, tabId?: string): void {
  const url = buildUrl(path, tabId)
  const currentPath = window.location.pathname

  if (currentPath !== withBase(path)) {
    history.pushState(null, '', url)
  } else if (currentTabParam() !== tabId) {
    history.replaceState(null, '', url)
  }
}

// ── Push helpers ─────────────────────────────────────────────────

export function pushTabRoute(tabType: string): void {
  if (suppressPush) return
  const path = TAB_PATHS[tabType] ?? '/'
  const activeTab = getActiveTab()
  pushUrl(path, activeTab?.id)
}

export function pushTabRouteForTab(tab: { id: string; type: string; dashboardId?: string }): void {
  if (suppressPush) return
  if (tab.type === 'dashboard' && tab.dashboardId) {
    pushUrl(`/dashboards/${tab.dashboardId}`, tab.id)
    return
  }
  const path = TAB_PATHS[tab.type] ?? '/'
  pushUrl(path, tab.id)
}

export function pushDashboardDetail(id: string): void {
  if (suppressPush) return
  const dashTab = getTabs().find(t => t.type === 'dashboard' && 'dashboardId' in t && t.dashboardId === id)
  pushUrl('/dashboards/' + id, dashTab?.id)
}

export function pushDashboardList(): void {
  if (suppressPush) return
  const tab = getTabs().find(t => t.type === 'dashboards')
  pushUrl('/dashboards', tab?.id)
}

export function pushPipelineDetail(id: string): void {
  if (suppressPush) return
  const tab = getTabs().find(t => t.type === 'pipelines')
  pushUrl('/pipelines/' + id, tab?.id)
  pipelineId = id
}

export function pushPipelineList(): void {
  if (suppressPush) return
  const tab = getTabs().find(t => t.type === 'pipelines')
  pushUrl('/pipelines', tab?.id)
  pipelineId = undefined
}

// ── Parse current URL ───────────────────────────────────────────

export function parseRoute(): { type: string; dashboardId?: string; pipelineId?: string } {
  const path = stripBase(window.location.pathname)

  // /dashboards/:id
  const dashMatch = path.match(/^\/dashboards\/(.+)$/)
  if (dashMatch) {
    return { type: 'dashboard', dashboardId: dashMatch[1] }
  }

  // /pipelines/:id
  const pipeMatch = path.match(/^\/pipelines\/(.+)$/)
  if (pipeMatch) {
    return { type: 'pipelines', pipelineId: pipeMatch[1] }
  }

  // Known singleton paths
  const entry = PATH_TABS[path]
  if (entry) {
    return { type: entry.type }
  }

  // Default: home (query editor)
  return { type: 'home' }
}

// ── Restore from ?tab= query param ─────────────────────────────

function tryRestoreFromTabParam(): boolean {
  const tabId = currentTabParam()
  if (!tabId) return false
  const tab = getTabs().find(t => t.id === tabId)
  if (!tab) return false
  suppressPush = true
  setActiveTab(tabId)
  suppressPush = false
  return true
}

function updateSubRouteState(): void {
  const match = stripBase(window.location.pathname).match(/^\/pipelines\/(.+)$/)
  pipelineId = match?.[1]
}

// ── Sync URL → tab state ────────────────────────────────────────

function syncRouteToTabs(): void {
  const route = parseRoute()

  // Update pipeline sub-route state
  pipelineId = route.pipelineId

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
  if (route.type === 'pipelines') {
    suppressPush = true
    openSingletonTab('pipelines', 'Pipelines')
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

  // On initial load, try ?tab= param first (survives reload reliably)
  if (!tryRestoreFromTabParam()) {
    // Fallback: sync from URL pathname
    syncRouteToTabs()
  }
  updateSubRouteState()

  // Seed ?tab= if missing so a subsequent reload works
  const activeTab = getActiveTab()
  if (activeTab && !currentTabParam()) {
    const url = buildUrl(stripBase(window.location.pathname), activeTab.id)
    history.replaceState(null, '', url)
  }

  // Handle browser back/forward
  window.addEventListener('popstate', () => {
    if (!tryRestoreFromTabParam()) {
      syncRouteToTabs()
    }
    updateSubRouteState()
  })
}
