/**
 * Product areas that render as full pages driven by the URL, not as tabs.
 * Tabs stay for the query workspace (queries, tables, databases, models,
 * dashboard detail), where holding two things open side by side is the
 * point. Everything here is breadcrumb-shaped: one place, one URL, no
 * "close" button.
 */
import {
  Bookmark, Brain, LayoutDashboard, ScrollText, Workflow, Boxes, Clock,
  Scale, HeartPulse, Gauge, Coins, Shield, KeyRound,
} from 'lucide-svelte'

/** Any lucide-svelte icon; they all share this component type. */
export type IconComponent = typeof Bookmark

export interface PageRouteMeta {
  label: string
  path: string
  /** One line for sidebars, the palette and Home. */
  description: string
  icon: IconComponent
  /** Needs an active Pro license; PageRouter shows ProRequired otherwise. */
  pro?: boolean
}

const ROUTES = {
  'saved-queries': { label: 'Saved Queries', path: '/saved-queries', description: 'Reusable SQL', icon: Bookmark },
  dashboards: { label: 'Dashboards', path: '/dashboards', description: 'Panels over saved queries', icon: LayoutDashboard },
  telemetry: { label: 'Telemetry', path: '/telemetry', description: 'Logs, traces and metrics', icon: ScrollText },
  pipelines: { label: 'Pipelines', path: '/pipelines', description: 'Stream data into ClickHouse', icon: Workflow },
  models: { label: 'Models', path: '/models', description: 'SQL transformations in order', icon: Boxes },
  schedules: { label: 'Schedules', path: '/schedules', description: 'Saved queries on a cron', icon: Clock, pro: true },
  brain: { label: 'Brain', path: '/brain', description: 'AI assistant for your data', icon: Brain },
  governance: { label: 'Governance', path: '/governance', description: 'Access, policies, audit', icon: Scale, pro: true },
  'cluster-health': { label: 'Cluster Health', path: '/cluster-health', description: 'Nodes, replication, parts', icon: HeartPulse, pro: true },
  'query-insights': { label: 'Query Insights', path: '/query-insights', description: 'Latency and failures', icon: Gauge, pro: true },
  'cost-center': { label: 'Cost Center', path: '/cost-center', description: 'Who spends what', icon: Coins, pro: true },
  performance: { label: 'Performance', path: '/performance', description: 'Regressions and measured improvements', icon: Gauge, pro: true },
  fleet: { label: 'Fleet', path: '/fleet', description: 'Health across connections', icon: HeartPulse, pro: true },
  'schema-compare': { label: 'Schema Compare', path: '/schema-compare', description: 'Review differences between environments', icon: Boxes, pro: true },
  'operations-reports': { label: 'Reports', path: '/reports', description: 'Weekly operations summaries', icon: ScrollText, pro: true },
  'incident-timeline': { label: 'Incident Timeline', path: '/incident-timeline', description: 'Correlate failures and operational events', icon: Clock, pro: true },
  admin: { label: 'Admin', path: '/admin', description: 'Users, connections, MCP', icon: Shield },
  settings: { label: 'License', path: '/license', description: 'Edition and entitlements', icon: KeyRound },
} as const satisfies Record<string, PageRouteMeta>

export type PageRoute = keyof typeof ROUTES

export const PAGE_ROUTES: Record<PageRoute, PageRouteMeta> = ROUTES

export function isPageRouteType(type: string | undefined | null): type is PageRoute {
  return !!type && type in PAGE_ROUTES
}

/** Path -> route type, for parsing the URL. */
export const PATH_TO_PAGE: Record<string, PageRoute> = Object.fromEntries(
  (Object.keys(PAGE_ROUTES) as PageRoute[]).map((k) => [PAGE_ROUTES[k].path, k]),
) as Record<string, PageRoute>

/**
 * The rail groups. Six icons instead of fourteen: the rail says what you
 * are doing, the context panel lists where you can go. `query` is the
 * workspace, everything else is a list of page routes.
 */
export interface NavGroup {
  id: 'query' | 'explore' | 'visualize' | 'build' | 'operate' | 'settings'
  label: string
  routes: PageRoute[]
}

export const NAV_GROUPS: NavGroup[] = [
  { id: 'query', label: 'Query', routes: [] },
  { id: 'explore', label: 'Explore', routes: ['saved-queries', 'brain'] },
  { id: 'visualize', label: 'Visualize', routes: ['dashboards', 'telemetry'] },
  { id: 'build', label: 'Build', routes: ['models', 'pipelines', 'schedules', 'schema-compare'] },
  { id: 'operate', label: 'Operate', routes: ['fleet', 'cluster-health', 'performance', 'query-insights', 'incident-timeline', 'cost-center', 'operations-reports', 'governance'] },
  { id: 'settings', label: 'Settings', routes: ['admin', 'settings'] },
]

/** Routes only an admin can use; the panel hides them for everyone else. */
export const ADMIN_ONLY_ROUTES: ReadonlySet<PageRoute> = new Set<PageRoute>(['admin', 'fleet', 'operations-reports', 'incident-timeline'])

export function groupForRoute(type: string | undefined | null): NavGroup {
  if (!isPageRouteType(type)) return NAV_GROUPS[0]
  return NAV_GROUPS.find((g) => g.routes.includes(type)) ?? NAV_GROUPS[0]
}

/**
 * Sections inside a page, driven by `?section=`. They nest under the page
 * in the context panel; the page itself renders no tab bar.
 */
export interface PageSection {
  id: string
  label: string
  /** Pro-only section: the context panel shows a lock and the page swaps in ProRequired. */
  pro?: boolean
}

export const PAGE_SECTIONS: Partial<Record<PageRoute, PageSection[]>> = {
  'cluster-health': [
    { id: 'overview', label: 'Overview' },
    { id: 'replication', label: 'Replication' },
    { id: 'replication-queue', label: 'Queue issues' },
    { id: 'merges', label: 'Merges' },
    { id: 'mutations', label: 'Mutations' },
    { id: 'long-queries', label: 'Long queries' },
    { id: 'parts', label: 'Parts pressure' },
    { id: 'disks', label: 'Data location' },
    { id: 'keeper', label: 'Keeper' },
    { id: 'backups', label: 'Backups' },
  ],
  'query-insights': [
    { id: 'overview', label: 'Overview' },
    { id: 'slow', label: 'Slow queries' },
    { id: 'memory', label: 'Memory' },
    { id: 'frequent', label: 'Frequent' },
    { id: 'errors', label: 'Errors' },
    { id: 'users', label: 'Users' },
    { id: 'tables', label: 'Hot tables' },
  ],
  'cost-center': [
    { id: 'overview', label: 'Overview' },
    { id: 'teams', label: 'Teams' },
    { id: 'users', label: 'Users' },
    { id: 'queries', label: 'Cost drivers' },
    { id: 'storage', label: 'Storage' },
  ],
  admin: [
    { id: 'overview', label: 'Overview' },
    { id: 'tunnels', label: 'Connections' },
    { id: 'users', label: 'Users' },
    { id: 'brain', label: 'Brain' },
    { id: 'github', label: 'GitHub' },
    { id: 'mcp', label: 'MCP Server' },
    { id: 'settings', label: 'Settings' },
  ],
  governance: [
    { id: 'dashboard', label: 'Dashboard' },
    { id: 'tables', label: 'Tables' },
    { id: 'queries', label: 'Query Audit' },
    { id: 'access', label: 'Access' },
    { id: 'incidents', label: 'Incidents' },
    { id: 'policies', label: 'Policies' },
    { id: 'alerts', label: 'Alerts' },
    { id: 'auditlog', label: 'Audit Log' },
    { id: 'settings', label: 'Settings' },
  ],
  telemetry: [
    { id: 'logs', label: 'Logs' },
    { id: 'traces', label: 'Traces', pro: true },
    { id: 'metrics', label: 'Metrics', pro: true },
    { id: 'service-map', label: 'Service map', pro: true },
    { id: 'monitors', label: 'Monitors', pro: true },
    { id: 'sources', label: 'Sources' },
  ],
  settings: [
    { id: 'license', label: 'License' },
    { id: 'instance', label: 'Instance' },
    { id: 'legal', label: 'Legal' },
  ],
}
