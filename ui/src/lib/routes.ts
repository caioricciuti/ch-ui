/**
 * Product areas that render as full pages driven by the URL, not as tabs.
 * Tabs stay for the query workspace (queries, tables, databases, models,
 * dashboard detail), where holding two things open side by side is the
 * point. Everything here is breadcrumb-shaped: one place, one URL, no
 * "close" button.
 */
import {
  Bookmark, Brain, LayoutDashboard, ScrollText, Workflow, Boxes, Clock,
  Scale, HeartPulse, Gauge, Coins, Shield, KeyRound, GitFork,
} from 'lucide-svelte'

/** Any lucide-svelte icon; they all share this component type. */
export type IconComponent = typeof Bookmark

export interface PageRouteMeta {
  label: string
  path: string
  /** One line for sidebars, the palette and Home. */
  description: string
  icon: IconComponent
  /** Needs Pro (active, or read-only during grace); PageRouter shows ProRequired otherwise. */
  pro?: boolean
  /** Path of the ch-ui.com docs page for this screen, e.g. `/docs/models/`. */
  docs: string
}

const ROUTES = {
  'saved-queries': { label: 'Saved Queries', path: '/saved-queries', description: 'Reusable SQL', icon: Bookmark, docs: '/docs/querying/#saved-queries' },
  dashboards: { label: 'Dashboards', path: '/dashboards', description: 'Panels over saved queries', icon: LayoutDashboard, docs: '/docs/dashboards/' },
  telemetry: { label: 'Telemetry', path: '/telemetry', description: 'Logs, traces and metrics', icon: ScrollText, docs: '/docs/telemetry/' },
  pipelines: { label: 'Pipelines', path: '/pipelines', description: 'Stream data into ClickHouse', icon: Workflow, docs: '/docs/pipelines/' },
  models: { label: 'Models', path: '/models', description: 'SQL transformations in order', icon: Boxes, docs: '/docs/models/' },
  schedules: { label: 'Schedules', path: '/schedules', description: 'Query jobs and report delivery on a cron', icon: Clock, pro: true, docs: '/docs/schedules/' },
  brain: { label: 'Brain', path: '/brain', description: 'AI assistant for your data', icon: Brain, docs: '/docs/brain/' },
  lineage: { label: 'Lineage', path: '/lineage', description: 'How data flows between tables', icon: GitFork, docs: '/docs/lineage/' },
  governance: { label: 'Governance', path: '/governance', description: 'Access, policies, audit', icon: Scale, pro: true, docs: '/docs/governance/' },
  'cluster-health': { label: 'Cluster Health', path: '/cluster-health', description: 'Nodes, replication, parts', icon: HeartPulse, pro: true, docs: '/docs/cluster-health/' },
  'query-insights': { label: 'Query Insights', path: '/query-insights', description: 'Latency and failures', icon: Gauge, pro: true, docs: '/docs/query-insights/' },
  'cost-center': { label: 'Cost Center', path: '/cost-center', description: 'Who spends what', icon: Coins, pro: true, docs: '/docs/cost-center/' },
  performance: { label: 'Performance', path: '/performance', description: 'Regressions and measured improvements', icon: Gauge, pro: true, docs: '/docs/performance/' },
  fleet: { label: 'Fleet', path: '/fleet', description: 'Health across connections', icon: HeartPulse, pro: true, docs: '/docs/performance/#fleet-overview' },
  'schema-compare': { label: 'Schema Compare', path: '/schema-compare', description: 'Review differences between environments', icon: Boxes, pro: true, docs: '/docs/schema-compare/' },
  'operations-reports': { label: 'Reports', path: '/reports', description: 'Weekly operations summaries', icon: ScrollText, pro: true, docs: '/docs/operations-reports/' },
  'incident-timeline': { label: 'Incident Timeline', path: '/incident-timeline', description: 'Correlate failures and operational events', icon: Clock, pro: true, docs: '/docs/incident-timeline/' },
  admin: { label: 'Admin', path: '/admin', description: 'Users, connections, MCP', icon: Shield, docs: '/docs/admin/' },
  settings: { label: 'License', path: '/license', description: 'Edition and entitlements', icon: KeyRound, docs: '/docs/license/' },
} as const satisfies Record<string, PageRouteMeta>

export type PageRoute = keyof typeof ROUTES

export const PAGE_ROUTES: Record<PageRoute, PageRouteMeta> = ROUTES

export function isPageRouteType(type: string | undefined | null): type is PageRoute {
  return !!type && type in PAGE_ROUTES
}

/**
 * Old paths that still arrive from docs, emails and bookmarks. The router
 * rewrites them to the current path, keeping the query string.
 */
export const PATH_ALIASES: Record<string, string> = {
  '/settings': PAGE_ROUTES.settings.path,
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
  { id: 'explore', label: 'Explore', routes: ['saved-queries', 'lineage', 'brain'] },
  { id: 'visualize', label: 'Visualize', routes: ['dashboards', 'telemetry'] },
  { id: 'build', label: 'Build', routes: ['models', 'pipelines', 'schedules', 'schema-compare'] },
  { id: 'operate', label: 'Operate', routes: ['fleet', 'cluster-health', 'performance', 'query-insights', 'incident-timeline', 'cost-center', 'operations-reports', 'governance'] },
  { id: 'settings', label: 'Settings', routes: ['admin', 'settings'] },
]

/** Routes only an admin can use; the panel hides them for everyone else. */
export const ADMIN_ONLY_ROUTES: ReadonlySet<PageRoute> = new Set<PageRoute>(['admin', 'fleet', 'operations-reports', 'incident-timeline'])

export const DOCS_ORIGIN = 'https://ch-ui.com'

/**
 * Absolute docs URL for a page, or for one of its sections when that
 * section has its own page on ch-ui.com.
 */
export function docsURL(route: PageRoute, section?: string | null): string {
  const own = section ? PAGE_SECTIONS[route]?.find((s) => s.id === section)?.docs : undefined
  return DOCS_ORIGIN + (own ?? PAGE_ROUTES[route].docs)
}

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
  /** Docs page for this section when it differs from the page's own. */
  docs?: string
}

export const PAGE_SECTIONS: Partial<Record<PageRoute, PageSection[]>> = {
  'saved-queries': [
    { id: 'all', label: 'All' },
    { id: 'mine', label: 'Mine' },
    { id: 'shared', label: 'Shared' },
    { id: 'verified', label: 'Verified' },
  ],
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
    { id: 'tunnels', label: 'Connections', docs: '/docs/connections/' },
    { id: 'users', label: 'Users', docs: '/docs/admin/#users' },
    { id: 'brain', label: 'Brain', docs: '/docs/brain/' },
    { id: 'brain-budgets', label: 'Brain budgets', docs: '/docs/brain/#usage-and-budgets' },
    { id: 'brain-usage', label: 'Brain usage', docs: '/docs/brain/#usage-and-budgets' },
    { id: 'github', label: 'GitHub', docs: '/docs/github/' },
    { id: 'mcp', label: 'MCP Server', docs: '/docs/mcp/' },
    { id: 'settings', label: 'Settings', docs: '/docs/configuration/' },
  ],
  schedules: [
    { id: 'jobs', label: 'Query jobs' },
    { id: 'reports', label: 'Report delivery', docs: '/docs/reports/' },
  ],
  performance: [
    { id: 'regressions', label: 'Regressions' },
    { id: 'investigations', label: 'Investigations' },
  ],
  governance: [
    { id: 'dashboard', label: 'Dashboard' },
    { id: 'tables', label: 'Tables' },
    { id: 'queries', label: 'Query Audit' },
    { id: 'access', label: 'Access' },
    { id: 'rbac', label: 'Access control', docs: '/docs/rbac/' },
    { id: 'effective-access', label: 'Effective access', docs: '/docs/rbac/' },
    { id: 'access-history', label: 'Access history', docs: '/docs/rbac/' },
    { id: 'incidents', label: 'Incidents' },
    { id: 'policies', label: 'Policies' },
    { id: 'alerts', label: 'Alerts', docs: '/docs/alerts/' },
    { id: 'alert-rules', label: 'Alert rules', docs: '/docs/alerts/' },
    { id: 'alert-channels', label: 'Alert channels', docs: '/docs/alerts/' },
    { id: 'silences', label: 'Silences', docs: '/docs/alerts/' },
    { id: 'health-alerts', label: 'Health checks', docs: '/docs/alerts/' },
    { id: 'sql-alerts', label: 'SQL alerts', docs: '/docs/alerts/' },
    { id: 'auditlog', label: 'Audit Log', docs: '/docs/audit-log/' },
    { id: 'settings', label: 'Settings' },
  ],
  telemetry: [
    { id: 'logs', label: 'Logs' },
    { id: 'traces', label: 'Traces', pro: true },
    { id: 'metrics', label: 'Metrics', pro: true },
    { id: 'service-map', label: 'Service map', pro: true },
    { id: 'monitors', label: 'Monitors', pro: true },
    { id: 'sources', label: 'Sources', docs: '/docs/ingest/' },
  ],
  settings: [
    { id: 'license', label: 'License' },
    { id: 'instance', label: 'Instance', docs: '/docs/configuration/' },
    { id: 'legal', label: 'Legal' },
  ],
}
