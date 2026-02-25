import type { ColumnMeta, QueryStats } from '../types/query'
import { createUUID } from '../utils/uuid'
import { pushTabRouteForTab } from './router.svelte'

// ── Tab types ────────────────────────────────────────────────────

export type TabType = 'home' | 'query' | 'table' | 'database' | 'dashboard' | 'saved-queries' | 'settings' | 'dashboards' | 'schedules' | 'brain' | 'admin' | 'governance'

interface TabBase {
  id: string
  type: TabType
  name: string
}

export interface QueryTab extends TabBase {
  type: 'query'
  sql: string
  dirty: boolean
  savedQueryId?: string
  baseSql?: string
}

export interface TableTab extends TabBase {
  type: 'table'
  database: string
  table: string
}

export interface DatabaseTab extends TabBase {
  type: 'database'
  database: string
}

export interface DashboardTab extends TabBase {
  type: 'dashboard'
  dashboardId: string
}

export interface HomeTab extends TabBase {
  type: 'home'
}

export interface SingletonTab extends TabBase {
  type: 'saved-queries' | 'settings' | 'dashboards' | 'schedules' | 'brain' | 'admin' | 'governance' 
}

export type Tab = HomeTab | QueryTab | TableTab | DatabaseTab | DashboardTab | SingletonTab

// ── Tab Groups (split view) ─────────────────────────────────────

export interface TabGroup {
  id: string        // 'left' | 'right'
  tabIds: string[]  // ordered tab IDs in this group
  activeTabId: string
}

// ── Per-tab query results ────────────────────────────────────────

export interface TabResult {
  meta: ColumnMeta[]
  data: unknown[][]
  stats: QueryStats | null
  elapsedMs: number
  error: string | null
  running: boolean
}

// ── Persistence ─────────────────────────────────────────────────

const STORAGE_KEY = 'ch-ui-tabs'
const HOME_TAB_ID = 'home'
const HOME_TAB_NAME = 'Home'

interface StorageFormat {
  tabs: Tab[]
  groups: TabGroup[]
  focusedGroupId: string
}

function saveTabs(): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify({
      tabs,
      groups,
      focusedGroupId,
    }))
  } catch {
    // localStorage full or unavailable
  }
}

function loadTabs(): StorageFormat {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw) {
      const parsed = JSON.parse(raw)
      if (Array.isArray(parsed.tabs) && parsed.tabs.length > 0) {
        // Derive nextNum from existing query tab names
        for (const t of parsed.tabs) {
          if (t.type === 'query') {
            const match = t.name.match(/^Query (\d+)$/)
            if (match) {
              const n = parseInt(match[1], 10)
              if (n >= nextNum) nextNum = n + 1
            }
          }
        }
        // New format: has groups array
        if (Array.isArray(parsed.groups) && parsed.groups.length > 0) {
          return normalizeTabsState({
            tabs: parsed.tabs,
            groups: parsed.groups,
            focusedGroupId: parsed.focusedGroupId || 'left',
          })
        }
        // Legacy migration: old format had { tabs, activeTabId }
        const activeId = parsed.activeTabId || parsed.tabs[0].id
        return normalizeTabsState({
          tabs: parsed.tabs,
          groups: [{ id: 'left', tabIds: parsed.tabs.map((t: Tab) => t.id), activeTabId: activeId }],
          focusedGroupId: 'left',
        })
      }
    }
  } catch {
    // corrupt data
  }
  const homeTab = createHomeTab()
  return normalizeTabsState({
    tabs: [homeTab],
    groups: [{ id: 'left', tabIds: [homeTab.id], activeTabId: homeTab.id }],
    focusedGroupId: 'left',
  })
}

// ── State ────────────────────────────────────────────────────────

let nextNum = 1

interface CreateQueryTabOptions {
  name?: string
  savedQueryId?: string
  baseSql?: string
}

function createQueryTab(sql = '', options: CreateQueryTabOptions = {}): QueryTab {
  const id = createUUID()
  const name = options.name?.trim() ? options.name.trim() : `Query ${nextNum++}`
  return {
    id,
    type: 'query',
    name,
    sql,
    dirty: false,
    savedQueryId: options.savedQueryId,
    baseSql: options.baseSql ?? sql,
  }
}

function createHomeTab(): HomeTab {
  return {
    id: HOME_TAB_ID,
    type: 'home',
    name: HOME_TAB_NAME,
  }
}

function normalizeTabsState(state: StorageFormat): StorageFormat {
  const existingHome = state.tabs.find((tab) => tab.type === 'home') as HomeTab | undefined
  const homeTab = existingHome ?? createHomeTab()

  const nonHomeTabs = state.tabs.filter((tab) => tab.type !== 'home' && tab.id !== HOME_TAB_ID)
  const normalizedTabs: Tab[] = [homeTab, ...nonHomeTabs]
  const tabIdSet = new Set(normalizedTabs.map((tab) => tab.id))

  const incomingGroups = state.groups.length > 0
    ? state.groups.map((group) => ({ ...group }))
    : [{ id: 'left', tabIds: [], activeTabId: homeTab.id }]

  if (!incomingGroups.some((group) => group.id === 'left')) {
    incomingGroups[0] = { ...incomingGroups[0], id: 'left' }
  }
  if (incomingGroups.length > 2) {
    incomingGroups.splice(2)
  }
  if (incomingGroups.length === 2) {
    incomingGroups[1] = { ...incomingGroups[1], id: 'right' }
  }

  const normalizedGroups = incomingGroups.map((group) => {
    const seen = new Set<string>()
    const ids = group.tabIds.filter((tabId) => {
      if (!tabIdSet.has(tabId) || tabId === homeTab.id || seen.has(tabId)) return false
      seen.add(tabId)
      return true
    })
    return { ...group, tabIds: ids }
  })

  const leftGroup = normalizedGroups.find((group) => group.id === 'left') ?? normalizedGroups[0]
  if (!leftGroup) {
    normalizedGroups.push({ id: 'left', tabIds: [homeTab.id], activeTabId: homeTab.id })
  } else {
    leftGroup.tabIds = [homeTab.id, ...leftGroup.tabIds]
  }

  const assignedTabIds = new Set(normalizedGroups.flatMap((group) => group.tabIds))
  for (const tab of nonHomeTabs) {
    if (!assignedTabIds.has(tab.id)) {
      const target = normalizedGroups.find((group) => group.id === 'left') ?? normalizedGroups[0]
      target.tabIds.push(tab.id)
      assignedTabIds.add(tab.id)
    }
  }

  const hydratedGroups = normalizedGroups
    .filter((group) => group.id === 'left' || group.tabIds.length > 0)
    .map((group) => {
      const activeTabId = group.tabIds.includes(group.activeTabId)
        ? group.activeTabId
        : (group.tabIds[0] ?? homeTab.id)
      return { ...group, activeTabId }
    })

  const groups = hydratedGroups.length > 0
    ? hydratedGroups
    : [{ id: 'left', tabIds: [homeTab.id], activeTabId: homeTab.id }]
  const focusedGroupId = groups.some((group) => group.id === state.focusedGroupId)
    ? state.focusedGroupId
    : 'left'

  return {
    tabs: normalizedTabs,
    groups,
    focusedGroupId,
  }
}

const initial = loadTabs()

let tabs = $state<Tab[]>(initial.tabs.map((tab) => {
  if (tab.type !== 'query') return tab
  const queryTab = tab as QueryTab
  return {
    ...queryTab,
    baseSql: typeof queryTab.baseSql === 'string'
      ? queryTab.baseSql
      : (queryTab.dirty ? '' : queryTab.sql),
  }
}))
let groups = $state<TabGroup[]>(initial.groups)
let focusedGroupId = $state<string>(initial.focusedGroupId)
let results = $state<Map<string, TabResult>>(new Map())

// Auto-save on any change (debounced via microtask)
let saveQueued = false
function queueSave(): void {
  if (saveQueued) return
  saveQueued = true
  queueMicrotask(() => {
    saveTabs()
    saveQueued = false
  })
}

// ── Internal helpers ────────────────────────────────────────────

function findGroupForTab(tabId: string): string | undefined {
  return groups.find(g => g.tabIds.includes(tabId))?.id
}

function isHomeTabId(tabId: string): boolean {
  const tab = tabs.find((entry) => entry.id === tabId)
  return !!tab && tab.type === 'home'
}

function resolveTargetGroupId(targetGroupId?: string): string {
  const candidate = targetGroupId ?? focusedGroupId
  if (groups.some((group) => group.id === candidate)) return candidate
  return groups[0]?.id ?? 'left'
}

// ── Getters ─────────────────────────────────────────────────────

export function getTabs(): Tab[] {
  return tabs
}

/** Backward-compat: returns focused group's active tab ID */
export function getActiveTabId(): string {
  const group = groups.find(g => g.id === focusedGroupId) ?? groups[0]
  return group?.activeTabId ?? ''
}

/** Backward-compat: returns focused group's active tab */
export function getActiveTab(): Tab | undefined {
  return tabs.find(t => t.id === getActiveTabId())
}

// ── Group getters ───────────────────────────────────────────────

export function getGroups(): TabGroup[] {
  return groups
}

export function getFocusedGroupId(): string {
  return focusedGroupId
}

export function isSplit(): boolean {
  return groups.length === 2
}

export function getGroupTabs(groupId: string): Tab[] {
  const group = groups.find(g => g.id === groupId)
  if (!group) return []
  return group.tabIds.map(id => tabs.find(t => t.id === id)).filter(Boolean) as Tab[]
}

export function getGroupActiveTab(groupId: string): Tab | undefined {
  const group = groups.find(g => g.id === groupId)
  if (!group) return undefined
  return tabs.find(t => t.id === group.activeTabId)
}

export function getGroupActiveTabId(groupId: string): string {
  const group = groups.find(g => g.id === groupId)
  return group?.activeTabId ?? ''
}

// ── Tab result accessors ─────────────────────────────────────────

export function getTabResult(tabId: string): TabResult | undefined {
  return results.get(tabId)
}

export function setTabResult(tabId: string, partial: Partial<TabResult>): void {
  const current = results.get(tabId) ?? {
    meta: [],
    data: [],
    stats: null,
    elapsedMs: 0,
    error: null,
    running: false,
  }
  const updated = new Map(results)
  updated.set(tabId, { ...current, ...partial })
  results = updated
}

export function clearTabResult(tabId: string): void {
  const updated = new Map(results)
  updated.delete(tabId)
  results = updated
}

// ── Actions ─────────────────────────────────────────────────────

export function setActiveTab(id: string, groupId?: string): void {
  const gid = groupId ?? findGroupForTab(id) ?? focusedGroupId
  groups = groups.map(g =>
    g.id === gid ? { ...g, activeTabId: id } : g
  )
  focusedGroupId = gid
  // Sync URL to match the activated tab
  const tab = tabs.find(t => t.id === id)
  if (tab) pushTabRouteForTab(tab)
  queueSave()
}

export function setFocusedGroup(groupId: string): void {
  focusedGroupId = groupId
}

// ── Open tabs (with deduplication) ───────────────────────────────

export function openHomeTab(): void {
  const homeTab = tabs.find((tab) => tab.type === 'home') as HomeTab | undefined
  if (homeTab) {
    setActiveTab(homeTab.id, 'left')
    return
  }

  const tab = createHomeTab()
  tabs = [tab, ...tabs]
  const leftGroup = groups.find((group) => group.id === 'left')
  if (leftGroup) {
    groups = groups.map((group) =>
      group.id === 'left'
        ? { ...group, tabIds: [tab.id, ...group.tabIds], activeTabId: tab.id }
        : group,
    )
  } else {
    groups = [{ id: 'left', tabIds: [tab.id], activeTabId: tab.id }, ...groups]
  }
  focusedGroupId = 'left'
  pushTabRouteForTab(tab)
  queueSave()
}

export function openQueryTab(sql = '', targetGroupId?: string): void {
  const tab = createQueryTab(sql)
  tabs = [...tabs, tab]
  const gid = resolveTargetGroupId(targetGroupId)
  groups = groups.map(g =>
    g.id === gid ? { ...g, tabIds: [...g.tabIds, tab.id], activeTabId: tab.id } : g
  )
  focusedGroupId = gid
  queueSave()
}

interface SavedQueryTabInput {
  id: string
  name: string
  query: string
}

export function openSavedQueryTab(savedQuery: SavedQueryTabInput, targetGroupId?: string): void {
  const existing = tabs.find(
    (tab) => tab.type === 'query' && (tab as QueryTab).savedQueryId === savedQuery.id,
  ) as QueryTab | undefined
  if (existing) {
    tabs = tabs.map((tab) => {
      if (tab.id !== existing.id || tab.type !== 'query') return tab
      if (tab.dirty) return tab
      return {
        ...tab,
        name: savedQuery.name,
        sql: savedQuery.query,
        baseSql: savedQuery.query,
        dirty: false,
      }
    })
    setActiveTab(existing.id)
    return
  }

  const tab = createQueryTab(savedQuery.query, {
    name: savedQuery.name,
    savedQueryId: savedQuery.id,
    baseSql: savedQuery.query,
  })
  tabs = [...tabs, tab]
  const gid = resolveTargetGroupId(targetGroupId)
  groups = groups.map(g =>
    g.id === gid ? { ...g, tabIds: [...g.tabIds, tab.id], activeTabId: tab.id } : g,
  )
  focusedGroupId = gid
  queueSave()
}

export function openTableTab(database: string, table: string, targetGroupId?: string): void {
  const existing = tabs.find(
    t => t.type === 'table' && t.database === database && t.table === table
  ) as TableTab | undefined
  if (existing) {
    setActiveTab(existing.id)
    return
  }
  const tab: TableTab = {
    id: createUUID(),
    type: 'table',
    name: `${database}.${table}`,
    database,
    table,
  }
  tabs = [...tabs, tab]
  const gid = resolveTargetGroupId(targetGroupId)
  groups = groups.map(g =>
    g.id === gid ? { ...g, tabIds: [...g.tabIds, tab.id], activeTabId: tab.id } : g
  )
  focusedGroupId = gid
  queueSave()
}

export function openDatabaseTab(database: string, targetGroupId?: string): void {
  const existing = tabs.find(
    t => t.type === 'database' && t.database === database,
  ) as DatabaseTab | undefined
  if (existing) {
    setActiveTab(existing.id)
    return
  }

  const tab: DatabaseTab = {
    id: createUUID(),
    type: 'database',
    name: database,
    database,
  }

  tabs = [...tabs, tab]
  const gid = resolveTargetGroupId(targetGroupId)
  groups = groups.map(g =>
    g.id === gid ? { ...g, tabIds: [...g.tabIds, tab.id], activeTabId: tab.id } : g,
  )
  focusedGroupId = gid
  queueSave()
}

export function openDashboardTab(dashboardId: string, name = 'Dashboard', targetGroupId?: string): void {
  const existing = tabs.find(
    t => t.type === 'dashboard' && t.dashboardId === dashboardId,
  ) as DashboardTab | undefined
  if (existing) {
    setActiveTab(existing.id)
    return
  }

  const tab: DashboardTab = {
    id: createUUID(),
    type: 'dashboard',
    name,
    dashboardId,
  }

  tabs = [...tabs, tab]
  const gid = resolveTargetGroupId(targetGroupId)
  groups = groups.map(g =>
    g.id === gid ? { ...g, tabIds: [...g.tabIds, tab.id], activeTabId: tab.id } : g,
  )
  focusedGroupId = gid
  pushTabRouteForTab(tab)
  queueSave()
}

export function openSingletonTab(type: SingletonTab['type'], name: string, targetGroupId?: string): void {
  const existing = tabs.find(t => t.type === type)
  if (existing) {
    if (existing.name !== name) {
      tabs = tabs.map((tab) => (tab.id === existing.id ? { ...tab, name } : tab))
      queueSave()
    }
    setActiveTab(existing.id)
    return
  }
  const tab: SingletonTab = {
    id: createUUID(),
    type,
    name,
  }
  tabs = [...tabs, tab]
  const gid = resolveTargetGroupId(targetGroupId)
  groups = groups.map(g =>
    g.id === gid ? { ...g, tabIds: [...g.tabIds, tab.id], activeTabId: tab.id } : g
  )
  focusedGroupId = gid
  pushTabRouteForTab(tab)
  queueSave()
}

// ── Close / update ───────────────────────────────────────────────

export function closeTab(id: string): void {
  if (isHomeTabId(id)) return

  const groupId = findGroupForTab(id)
  if (!groupId) return

  // Remove from group
  groups = groups.map(g => {
    if (g.id !== groupId) return g
    const newTabIds = g.tabIds.filter(tid => tid !== id)
    let newActiveId = g.activeTabId
    if (g.activeTabId === id) {
      const idx = g.tabIds.indexOf(id)
      const newIdx = Math.min(idx, newTabIds.length - 1)
      newActiveId = newTabIds[newIdx] ?? ''
    }
    return { ...g, tabIds: newTabIds, activeTabId: newActiveId }
  })

  // Remove tab data
  tabs = tabs.filter(t => t.id !== id)
  clearTabResult(id)

  // Collapse empty group
  const emptyGroup = groups.find(g => g.tabIds.length === 0)
  if (emptyGroup) {
    groups = groups.filter(g => g.tabIds.length > 0)
    if (groups.length === 0) {
      // Keep the pinned Home tab alive.
      const homeTab = createHomeTab()
      tabs = [homeTab]
      groups = [{ id: 'left', tabIds: [homeTab.id], activeTabId: homeTab.id }]
    }
    focusedGroupId = groups[0].id
  }

  // If all tabs gone from last group, ensure Home exists.
  if (tabs.length === 0) {
    const homeTab = createHomeTab()
    tabs = [homeTab]
    groups = [{ id: 'left', tabIds: [homeTab.id], activeTabId: homeTab.id }]
    focusedGroupId = 'left'
  }

  const normalized = normalizeTabsState({ tabs, groups, focusedGroupId })
  tabs = normalized.tabs
  groups = normalized.groups
  focusedGroupId = normalized.focusedGroupId

  queueSave()
}

export function updateTabSQL(id: string, sql: string): void {
  tabs = tabs.map((tab) => {
    if (tab.id !== id || tab.type !== 'query') return tab
    const baseSql = typeof tab.baseSql === 'string' ? tab.baseSql : ''
    return { ...tab, sql, dirty: sql !== baseSql }
  })
  queueSave()
}

export function renameTab(id: string, name: string): void {
  if (isHomeTabId(id)) return
  tabs = tabs.map(t => (t.id === id ? { ...t, name } : t))
  queueSave()
}

export function markQueryTabSaved(id: string, options: { savedQueryId?: string; name?: string; baseSql?: string } = {}): void {
  tabs = tabs.map((tab) => {
    if (tab.id !== id || tab.type !== 'query') return tab
    const name = options.name?.trim() ? options.name.trim() : tab.name
    const baseSql = options.baseSql ?? tab.sql
    return {
      ...tab,
      name,
      savedQueryId: options.savedQueryId ?? tab.savedQueryId,
      baseSql,
      dirty: false,
    }
  })
  queueSave()
}

export function isTabDirty(id: string): boolean {
  const tab = tabs.find((entry) => entry.id === id)
  return !!(tab && tab.type === 'query' && tab.dirty)
}

// ── Reorder (within a group) ─────────────────────────────────────

export function reorderTab(groupId: string, fromIndex: number, toIndex: number): void {
  if (fromIndex === toIndex) return
  groups = groups.map(g => {
    if (g.id !== groupId) return g
    const updated = [...g.tabIds]
    const [moved] = updated.splice(fromIndex, 1)
    if (!moved || isHomeTabId(moved)) return g
    updated.splice(toIndex, 0, moved)
    if (g.id === 'left') {
      const homeIndex = updated.findIndex((tabId) => isHomeTabId(tabId))
      if (homeIndex > 0) {
        const [homeId] = updated.splice(homeIndex, 1)
        updated.unshift(homeId)
      }
    }
    return { ...g, tabIds: updated }
  })
  queueSave()
}

// ── Split / move / unsplit ───────────────────────────────────────

export function splitTabToSide(tabId: string, side: 'left' | 'right'): void {
  if (isHomeTabId(tabId)) return

  if (groups.length >= 2) {
    moveTabToGroup(tabId, side)
    return
  }

  const sourceGroup = groups.find(g => g.tabIds.includes(tabId))
  if (!sourceGroup || sourceGroup.tabIds.length <= 1) return

  const remainingTabIds = sourceGroup.tabIds.filter(id => id !== tabId)
  const idx = sourceGroup.tabIds.indexOf(tabId)
  const remainingActive = sourceGroup.activeTabId === tabId
    ? remainingTabIds[Math.min(idx, remainingTabIds.length - 1)] ?? remainingTabIds[0] ?? ''
    : sourceGroup.activeTabId

  if (side === 'right') {
    groups = [
      { id: 'left', tabIds: remainingTabIds, activeTabId: remainingActive },
      { id: 'right', tabIds: [tabId], activeTabId: tabId },
    ]
    focusedGroupId = 'right'
  } else {
    groups = [
      { id: 'left', tabIds: [tabId], activeTabId: tabId },
      { id: 'right', tabIds: remainingTabIds, activeTabId: remainingActive },
    ]
    focusedGroupId = 'left'
  }

  const normalized = normalizeTabsState({ tabs, groups, focusedGroupId })
  tabs = normalized.tabs
  groups = normalized.groups
  focusedGroupId = normalized.focusedGroupId

  queueSave()
}

export function splitTab(tabId: string): void {
  if (isHomeTabId(tabId)) return

  if (groups.length >= 2) {
    // Already split — move to other group
    const sourceGroupId = findGroupForTab(tabId)
    const targetGroupId = sourceGroupId === 'left' ? 'right' : 'left'
    moveTabToGroup(tabId, targetGroupId)
    return
  }
  splitTabToSide(tabId, 'right')
}

export function moveTabToGroup(tabId: string, targetGroupId: string): void {
  if (isHomeTabId(tabId)) return

  const sourceGroupId = findGroupForTab(tabId)
  if (!sourceGroupId || sourceGroupId === targetGroupId) return

  // If target group doesn't exist yet, create it (this enables cross-group drag to create split)
  if (!groups.find(g => g.id === targetGroupId)) {
    const sourceGroup = groups.find(g => g.id === sourceGroupId)
    if (!sourceGroup || sourceGroup.tabIds.length <= 1) return
    // Create split
    splitTab(tabId)
    return
  }

  groups = groups.map(g => {
    if (g.id === sourceGroupId) {
      const newTabIds = g.tabIds.filter(id => id !== tabId)
      const idx = g.tabIds.indexOf(tabId)
      const newActive = g.activeTabId === tabId
        ? (newTabIds[Math.min(idx, newTabIds.length - 1)] ?? newTabIds[0] ?? '')
        : g.activeTabId
      return { ...g, tabIds: newTabIds, activeTabId: newActive }
    }
    if (g.id === targetGroupId) {
      return { ...g, tabIds: [...g.tabIds, tabId], activeTabId: tabId }
    }
    return g
  })

  // Collapse empty groups
  const emptyGroup = groups.find(g => g.tabIds.length === 0)
  if (emptyGroup) {
    groups = groups.filter(g => g.tabIds.length > 0)
    focusedGroupId = groups[0]?.id ?? 'left'
  } else {
    focusedGroupId = targetGroupId
  }

  const normalized = normalizeTabsState({ tabs, groups, focusedGroupId })
  tabs = normalized.tabs
  groups = normalized.groups
  focusedGroupId = normalized.focusedGroupId

  queueSave()
}

export function unsplit(): void {
  const allTabIds = groups.flatMap(g => g.tabIds)
  const homeId = tabs.find((tab) => tab.type === 'home')?.id ?? HOME_TAB_ID
  const ordered = [homeId, ...allTabIds.filter((id) => id !== homeId)]
  const preferredActive = groups.find(g => g.id === focusedGroupId)?.activeTabId ?? ordered[0]
  const activeId = preferredActive === homeId ? homeId : (ordered.includes(preferredActive) ? preferredActive : homeId)
  groups = [{ id: 'left', tabIds: ordered, activeTabId: activeId }]
  focusedGroupId = 'left'
  queueSave()
}

// Legacy alias
export const addTab = openQueryTab
