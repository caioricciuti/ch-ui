import { withBase, stripBase } from '../basePath'
import { getActiveTab, getTabs, openHomeTab, setActiveTab } from './tabs.svelte'
import { isPageRouteType, PAGE_ROUTES, PATH_TO_PAGE, type PageRoute } from '../routes'
import { syncSectionFromUrl } from './nav.svelte'

// ── Current route type (reactive) ────────────────────────────────
// Pages (see lib/routes.ts) render from this; the tab workspace shows when
// the route is not a page.

let routeType = $state<string>('home')

export function getRouteType(): string {
  return routeType
}

function syncRouteType(): void {
  routeType = parseRoute().type
  syncSectionFromUrl()
}

/** Navigate to a page URL. The tab workspace keeps its state underneath. */
export function navigate(path: string): void {
  if (window.location.pathname !== withBase(path)) {
    history.pushState(null, '', withBase(path))
  }
  syncRouteType()
  updateSubRouteState()
}

/** Open a product area page. */
export function goTo(type: PageRoute, _label?: string): void {
  navigate(PAGE_ROUTES[type].path)
}

/** Return to the query workspace, on whatever tab was active there. */
export function goWorkspace(): void {
  const active = getActiveTab()
  if (active) pushTabRouteForTab(active)
  else navigate('/')
}

// ── URL ↔ Tab mapping ────────────────────────────────────────────

// Workspace tab types all map to '/'. Product areas live in PAGE_ROUTES;
// dashboard and pipeline detail are sub-routes of their pages.
const TAB_PATHS: Record<string, string> = {
  home: '/',
  query: '/',
  table: '/',
  database: '/',
  model: '/',
}

// Prevents pushState during popstate-triggered tab activation
let suppressPush = false

// ── Pipeline sub-route state ─────────────────────────────────────

let pipelineId = $state<string | undefined>(undefined)
let dashboardId = $state<string | undefined>(undefined)

export function getCurrentPipelineId(): string | undefined {
  return pipelineId
}

export function getCurrentDashboardId(): string | undefined {
  return dashboardId
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
  syncRouteType()
}

// ── Push helpers ─────────────────────────────────────────────────

export function pushTabRoute(tabType: string): void {
  if (suppressPush) return
  const path = TAB_PATHS[tabType] ?? '/'
  const activeTab = getActiveTab()
  pushUrl(path, activeTab?.id)
}

export function pushTabRouteForTab(tab: { id: string; type: string }): void {
  if (suppressPush) return
  const path = TAB_PATHS[tab.type] ?? '/'
  pushUrl(path, tab.id)
}

export function pushDashboardDetail(id: string): void {
  if (suppressPush) return
  navigate('/dashboards/' + id)
  dashboardId = id
}

export function pushDashboardList(): void {
  if (suppressPush) return
  navigate('/dashboards')
  dashboardId = undefined
}

export function pushPipelineDetail(id: string): void {
  if (suppressPush) return
  navigate('/pipelines/' + id)
  pipelineId = id
}

export function pushPipelineList(): void {
  if (suppressPush) return
  navigate('/pipelines')
  pipelineId = undefined
}

// ── Parse current URL ───────────────────────────────────────────

export function parseRoute(): { type: string; dashboardId?: string; pipelineId?: string } {
  const path = stripBase(window.location.pathname)

  // /dashboards/:id
  const dashMatch = path.match(/^\/dashboards\/(.+)$/)
  if (dashMatch) {
    return { type: 'dashboards', dashboardId: dashMatch[1] }
  }

  // /pipelines/:id
  const pipeMatch = path.match(/^\/pipelines\/(.+)$/)
  if (pipeMatch) {
    return { type: 'pipelines', pipelineId: pipeMatch[1] }
  }

  // Product area pages
  const page = PATH_TO_PAGE[path]
  if (page) {
    return { type: page }
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
  const path = stripBase(window.location.pathname)
  pipelineId = path.match(/^\/pipelines\/(.+)$/)?.[1]
  dashboardId = path.match(/^\/dashboards\/(.+)$/)?.[1]
}

// ── Sync URL → tab state ────────────────────────────────────────

function syncRouteToTabs(): void {
  const route = parseRoute()

  pipelineId = route.pipelineId
  dashboardId = route.dashboardId

  // Pages are not tabs: nothing to open, the PageRouter renders from routeType.
  if (isPageRouteType(route.type)) return
  // Workspace root: make sure something is open
  if (!getActiveTab()) openHomeTab()
}

// ── Initialize ──────────────────────────────────────────────────

let initialized = false

export function initRouter(): void {
  if (initialized) return
  initialized = true

  syncRouteType()
  const onPage = isPageRouteType(routeType)

  // On initial load, try ?tab= param first (survives reload reliably)
  if (onPage || !tryRestoreFromTabParam()) {
    // Fallback: sync from URL pathname
    syncRouteToTabs()
  }
  updateSubRouteState()

  // Seed ?tab= if missing so a subsequent reload works (workspace only)
  const activeTab = getActiveTab()
  if (!onPage && activeTab && !currentTabParam()) {
    const url = buildUrl(stripBase(window.location.pathname), activeTab.id)
    history.replaceState(null, '', url)
  }

  // Handle browser back/forward
  window.addEventListener('popstate', () => {
    syncRouteType()
    if (isPageRouteType(routeType) || !tryRestoreFromTabParam()) {
      syncRouteToTabs()
    }
    updateSubRouteState()
  })
}
