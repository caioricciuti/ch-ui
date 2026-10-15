import type { ResultEntry, TabResults } from '../editor/result-tabs'
import {
  EMPTY_TAB_RESULTS,
  addResult,
  capRows,
  dropUnpinned,
  removeResult,
  replaceActiveResult,
  restorePinned,
  serializePinned,
  setActiveResult,
  togglePin,
  updateResult,
  type PinOutcome,
} from '../editor/result-tabs'
import { getMaxResultRows } from './query-limit.svelte'
import { isPageRouteType } from '../routes'
import type { ColumnFilter, ResultSort } from '../utils/result-filters'
import type { ModelEditState } from '../types/models'
import { createUUID } from '../utils/uuid'
import { parseSavedParams } from '../utils/query-params'
import { pushTabRouteForTab } from './router.svelte'
import { onScopeChange, scoped } from './workspace-scope'
import { stopAllRuns, stopRunForResult, stopRunsForTab } from '../editor/query-runs'

// ── Tab types ────────────────────────────────────────────────────

export type TabType = 'home' | 'query' | 'table' | 'database' | 'model'

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
  /**
   * The linked saved query's version this tab was loaded from or last saved
   * as. Saving sends it so a newer save or restore is not overwritten.
   */
  savedVersion?: number
  baseSql?: string
  /** Values typed for {name:Type} query parameters; kept per tab across switches. */
  paramValues?: Record<string, string>
  /** Parameter defaults stored with the linked saved query. */
  savedParams?: Record<string, string>
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

export interface ModelTab extends TabBase {
  type: 'model'
  modelId: string
  dirty: boolean
  edit: ModelEditState
  base: ModelEditState
  status: string
  lastError: string | null
}

export interface HomeTab extends TabBase {
  type: 'home'
}

export type Tab = HomeTab | QueryTab | TableTab | DatabaseTab | ModelTab

// ── Tab Groups (split view) ─────────────────────────────────────

export interface TabGroup {
  id: string        // 'left' | 'right'
  tabIds: string[]  // ordered tab IDs in this group
  activeTabId: string
}

// ── Per-tab query results ────────────────────────────────────────
// Each query tab keeps a list of results (one per run statement) and the
// index of the one on screen; see editor/result-tabs.ts for the rules.

export type { ResultEntry as TabResult, TabResults } from '../editor/result-tabs'

// Sort/filter view over a result (issue #117), kept per result id so it
// survives tab and result switches; reset on every fresh run.
export interface ResultViewState {
  sort: ResultSort | null
  filters: ColumnFilter[]
  /** Current result.data came from a server-side wrapped re-query. */
  serverApplied: boolean
  /** SQL of the run that produced the base result (wrapping target). */
  baseSql: string
  /** The exact SQL most recently sent to the server for this tab (may be a
   * wrapped/marked grid re-query, unlike baseSql). Used to map error
   * positions back into the editor only when it matches baseSql. */
  executedSql: string
  /** Document offset of baseSql at run time (-1 when unknown); resolves the
   * right occurrence when the same fragment appears multiple times. */
  executedFrom: number
  /** Bind parameters used by that run, re-sent on wrapped re-queries. */
  baseParams: Record<string, string> | undefined
  /** The base run hit the row limit, so in-memory data is partial. */
  truncated: boolean
  /** baseSql can be wrapped as SELECT * FROM (...) for server-side mode. */
  wrappable: boolean
}

const DEFAULT_RESULT_VIEW: ResultViewState = {
  sort: null,
  filters: [],
  serverApplied: false,
  baseSql: '',
  executedSql: '',
  executedFrom: -1,
  baseParams: undefined,
  truncated: false,
  wrappable: false,
}

// ── Persistence ─────────────────────────────────────────────────

// Both are kept per user@connection scope (see workspace-scope.ts).
const STORAGE_KEY = 'ch-ui-tabs'
// Pinned results, per query tab id, so they survive a reload (bounded size).
const PINNED_STORAGE_KEY = 'ch-ui-pinned-results'
const HOME_TAB_ID = 'home'
const HOME_TAB_NAME = 'Home'

interface StorageFormat {
  tabs: Tab[]
  groups: TabGroup[]
  focusedGroupId: string
}

// False when the last write to localStorage failed (quota, private mode).
// Tabs normally persist on every edit, so a reload loses nothing; only then
// does closing the page need to warn about unsaved changes.
let persisted = true

function saveTabs(): void {
  // Nobody signed in: nothing to save, and nothing at risk.
  const key = scoped(STORAGE_KEY)
  if (!key) {
    persisted = true
    return
  }
  try {
    localStorage.setItem(key, JSON.stringify({
      tabs,
      groups,
      focusedGroupId,
    }))
    persisted = true
  } catch {
    // localStorage full or unavailable
    persisted = false
  }
}

/** True when edited tabs could not be written to storage and a reload would lose them. */
export function hasUnpersistedChanges(): boolean {
  return !persisted && tabs.some((tab) => isTabDirty(tab.id))
}

// Set once the user already confirmed losing unsaved tabs (connection
// switch), so the browser does not ask a second time.
let unloadConfirmed = false

/** Skip the leave-page prompt (true) or ask again (false, the navigation failed). */
export function confirmUnload(confirmed = true): void {
  unloadConfirmed = confirmed
}

if (typeof window !== 'undefined') {
  window.addEventListener('beforeunload', (e) => {
    if (unloadConfirmed || !hasUnpersistedChanges()) return
    e.preventDefault()
    // Older browsers only show the prompt when returnValue is set.
    e.returnValue = ''
  })
}

function loadTabs(): StorageFormat {
  try {
    const key = scoped(STORAGE_KEY)
    const raw = key ? localStorage.getItem(key) : null
    if (raw) {
      const parsed = JSON.parse(raw)
      if (Array.isArray(parsed.tabs)) {
        // Areas that became pages (and the old dashboard tab type) are no
        // longer tabs; drop stale ones instead of rendering nothing.
        const TAB_TYPES: ReadonlySet<string> = new Set(['home', 'query', 'table', 'database', 'model'])
        parsed.tabs = parsed.tabs.filter((t: { type?: string }) => !!t.type && TAB_TYPES.has(t.type) && !isPageRouteType(t.type))
        // Model tabs saved before models v2 lack the option fields.
        for (const t of parsed.tabs) {
          if (t.type === 'model') {
            t.edit = withModelEditDefaults(t.edit)
            t.base = withModelEditDefaults(t.base)
          }
        }
      }
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
  savedVersion?: number
  baseSql?: string
  savedParams?: Record<string, string>
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
    savedVersion: options.savedVersion,
    baseSql: options.baseSql ?? sql,
    paramValues: options.savedParams ? { ...options.savedParams } : {},
    savedParams: options.savedParams,
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

function withBaseSql(tab: Tab): Tab {
  if (tab.type !== 'query') return tab
  const queryTab = tab as QueryTab
  return {
    ...queryTab,
    baseSql: typeof queryTab.baseSql === 'string'
      ? queryTab.baseSql
      : (queryTab.dirty ? '' : queryTab.sql),
  }
}

function queryTabIds(state: StorageFormat): Set<string> {
  return new Set(state.tabs.filter((t) => t.type === 'query').map((t) => t.id))
}

// The session is set before the Shell loads, so this usually reads the signed
// in scope. When the Shell was prefetched on the sign-in page there is no
// scope yet; rehydrateForScope runs once there is.
const initial = loadTabs()

let tabs = $state<Tab[]>(initial.tabs.map(withBaseSql))
let groups = $state<TabGroup[]>(initial.groups)
let focusedGroupId = $state<string>(initial.focusedGroupId)
let results = $state<Map<string, TabResults>>(loadPinnedResults(queryTabIds(initial)))
// Sort/filter state per result id.
let resultViews = $state<Map<string, ResultViewState>>(new Map())

/**
 * Replaces every tab, group and result with what the active scope saved (a
 * lone Home tab when nobody is signed in), so one person's workspace never
 * stays in memory for the next.
 */
export function rehydrateForScope(): void {
  // Queries still running belong to the previous person or connection.
  stopAllRuns()
  nextNum = 1
  const state = loadTabs()
  tabs = state.tabs.map(withBaseSql)
  groups = state.groups
  focusedGroupId = state.focusedGroupId
  results = loadPinnedResults(queryTabIds(state))
  resultViews = new Map()
  persisted = true
}

onScopeChange(rehydrateForScope)

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

/** Pinned results saved by an earlier page, for the query tabs that still exist. */
function loadPinnedResults(tabIds: Set<string>): Map<string, TabResults> {
  const out = new Map<string, TabResults>()
  try {
    const key = scoped(PINNED_STORAGE_KEY)
    const raw = key ? localStorage.getItem(key) : null
    if (!raw) return out
    const parsed: unknown = JSON.parse(raw)
    if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) return out
    for (const [tabId, saved] of Object.entries(parsed as Record<string, unknown>)) {
      if (!tabIds.has(tabId)) continue
      const entries = restorePinned(saved)
      if (entries.length > 0) out.set(tabId, { results: entries, activeResult: entries.length - 1 })
    }
  } catch {
    // corrupt or unavailable storage: start without pinned results
  }
  return out
}

let pinnedSaveQueued = false
/** Saves every tab's pinned results (metadata plus a bounded number of rows). */
function queuePinnedSave(): void {
  if (pinnedSaveQueued) return
  pinnedSaveQueued = true
  queueMicrotask(() => {
    pinnedSaveQueued = false
    const out: Record<string, ResultEntry[]> = {}
    for (const [tabId, state] of results) {
      const saved = serializePinned(state)
      if (saved.length > 0) out[tabId] = saved
    }
    const key = scoped(PINNED_STORAGE_KEY)
    if (!key) return
    try {
      if (Object.keys(out).length === 0) localStorage.removeItem(key)
      else localStorage.setItem(key, JSON.stringify(out))
    } catch {
      // quota exceeded: pinned results still live for this page
    }
  })
}

function setTabResults(tabId: string, next: TabResults, persist = false): void {
  const prev = results.get(tabId)
  if (prev === next) return
  const updated = new Map(results)
  if (next.results.length === 0) updated.delete(tabId)
  else updated.set(tabId, next)
  results = updated
  // Results that left the list take their sort/filter state with them.
  if (prev) {
    const kept = new Set(next.results.map((r) => r.id))
    const gone = prev.results.filter((r) => !kept.has(r.id))
    // A query still filling a result that left the list has nowhere to go.
    for (const r of gone) if (r.running) stopRunForResult(tabId, r.id)
    if (gone.length > 0) {
      const views = new Map(resultViews)
      for (const r of gone) views.delete(r.id)
      resultViews = views
    }
    if (persist || gone.some((r) => r.pinned)) queuePinnedSave()
  }
}

/** All results of a tab and the index of the active one. */
export function getTabResults(tabId: string): TabResults {
  return results.get(tabId) ?? EMPTY_TAB_RESULTS
}

/** The result on screen in a tab, if any. */
export function getTabResult(tabId: string): ResultEntry | undefined {
  const state = results.get(tabId)
  return state ? state.results[state.activeResult] : undefined
}

/** True while any result of the tab is still running. */
export function isTabRunning(tabId: string): boolean {
  return results.get(tabId)?.results.some((r) => r.running) ?? false
}

export interface NewResultInput {
  id: string
  label: string
  sql: string
  params?: Record<string, string>
}

function newEntry(input: NewResultInput): ResultEntry {
  return {
    ...input,
    meta: [],
    data: [],
    stats: null,
    elapsedMs: 0,
    error: null,
    running: true,
    pinned: false,
    createdAt: Date.now(),
  }
}

/**
 * Starts a result for a run. 'replace' takes the place of the active result
 * (a pinned one is kept and the new result is added beside it); 'append'
 * adds a new result tab. Either way the new result becomes active.
 */
export function startTabResult(tabId: string, input: NewResultInput, mode: 'replace' | 'append'): void {
  const state = results.get(tabId) ?? EMPTY_TAB_RESULTS
  const entry = newEntry(input)
  setTabResults(tabId, mode === 'replace' ? replaceActiveResult(state, entry) : addResult(state, entry))
}

/** Clears unpinned results before a run of several statements. */
export function dropUnpinnedTabResults(tabId: string): void {
  const state = results.get(tabId)
  if (state) setTabResults(tabId, dropUnpinned(state))
}

/**
 * Updates one result of a tab. Rows past the result row limit are not kept.
 * Does nothing when the result is gone (closed or evicted meanwhile).
 */
export function setTabResult(tabId: string, resultId: string, partial: Partial<ResultEntry>): void {
  const state = results.get(tabId)
  if (!state) return
  const patch = partial.data ? { ...partial, data: capRows(partial.data, getMaxResultRows()) } : partial
  const entry = state.results.find((r) => r.id === resultId)
  if (!entry) return
  // A finished run of a pinned result changes what is saved for reloads.
  const persist = entry.pinned && (partial.running === false || partial.pinned !== undefined)
  setTabResults(tabId, updateResult(state, resultId, patch), persist)
}

export function setActiveTabResult(tabId: string, resultId: string): void {
  const state = results.get(tabId)
  if (state) setTabResults(tabId, setActiveResult(state, resultId))
}

export function closeTabResult(tabId: string, resultId: string): void {
  // Closing the result that is still streaming stops its query too.
  stopRunForResult(tabId, resultId)
  const state = results.get(tabId)
  if (state) setTabResults(tabId, removeResult(state, resultId), true)
}

/** Pins or unpins a result; 'limit' when the tab already has the most pins. */
export function toggleTabResultPin(tabId: string, resultId: string): PinOutcome {
  const state = results.get(tabId)
  if (!state) return 'unpinned'
  const { state: next, outcome } = togglePin(state, resultId)
  setTabResults(tabId, next, outcome !== 'limit')
  return outcome
}

export function clearTabResult(tabId: string): void {
  const state = results.get(tabId)
  if (!state) return
  setTabResults(tabId, EMPTY_TAB_RESULTS, true)
}

export function getResultView(resultId: string): ResultViewState {
  return resultViews.get(resultId) ?? DEFAULT_RESULT_VIEW
}

export function setResultView(resultId: string, partial: Partial<ResultViewState>): void {
  const current = resultViews.get(resultId) ?? DEFAULT_RESULT_VIEW
  const updated = new Map(resultViews)
  updated.set(resultId, { ...current, ...partial })
  resultViews = updated
}

export function resetResultView(resultId: string, partial: Partial<ResultViewState> = {}): void {
  const updated = new Map(resultViews)
  updated.set(resultId, { ...DEFAULT_RESULT_VIEW, ...partial })
  resultViews = updated
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
  /** Stored parameter defaults, as the API returns them (JSON object string). */
  parameters?: string | null
  /** The saved query's latest version (see QueryTab.savedVersion). */
  version?: number
}

/**
 * Opens a saved query, reusing the tab already linked to it unless newTab is
 * set (a save conflict opens the stored version next to the edited one).
 */
export function openSavedQueryTab(savedQuery: SavedQueryTabInput, targetGroupId?: string, opts: { newTab?: boolean } = {}): void {
  const existing = opts.newTab ? undefined : tabs.find(
    (tab) => tab.type === 'query' && (tab as QueryTab).savedQueryId === savedQuery.id,
  ) as QueryTab | undefined
  const savedParams = parseSavedParams(savedQuery.parameters)
  if (existing) {
    tabs = tabs.map((tab) => {
      if (tab.id !== existing.id || tab.type !== 'query') return tab
      if (tab.dirty) return tab
      return {
        ...tab,
        name: savedQuery.name,
        sql: savedQuery.query,
        baseSql: savedQuery.query,
        savedVersion: savedQuery.version,
        dirty: false,
        // Values typed in this tab win; stored defaults fill the gaps.
        paramValues: { ...savedParams, ...nonEmpty(tab.paramValues) },
        savedParams,
      }
    })
    setActiveTab(existing.id)
    return
  }

  const tab = createQueryTab(savedQuery.query, {
    name: savedQuery.name,
    savedQueryId: savedQuery.id,
    savedVersion: savedQuery.version,
    baseSql: savedQuery.query,
    savedParams,
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

  // Remove tab data; a query still running in it is stopped.
  stopRunsForTab(id)
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

function nonEmpty(values: Record<string, string> | undefined): Record<string, string> {
  const out: Record<string, string> = {}
  for (const [k, v] of Object.entries(values ?? {})) {
    if (v.trim()) out[k] = v
  }
  return out
}

/** Remember the parameter values typed in a query tab. */
export function setTabParamValues(id: string, values: Record<string, string>): void {
  tabs = tabs.map((tab) => {
    if (tab.id !== id || tab.type !== 'query') return tab
    return { ...tab, paramValues: { ...values } }
  })
  queueSave()
}

export function renameTab(id: string, name: string): void {
  if (isHomeTabId(id)) return
  tabs = tabs.map(t => (t.id === id ? { ...t, name } : t))
  queueSave()
}

// Rename any open query tabs linked to a saved query (e.g. after renaming it in
// the Saved Queries list) so the tab title stays in sync.
export function renameSavedQueryTabs(savedQueryId: string, name: string): void {
  const trimmed = name.trim()
  if (!savedQueryId || !trimmed) return
  let changed = false
  tabs = tabs.map((tab) => {
    if (tab.type === 'query' && (tab as QueryTab).savedQueryId === savedQueryId && tab.name !== trimmed) {
      changed = true
      return { ...tab, name: trimmed }
    }
    return tab
  })
  if (changed) queueSave()
}

export function markQueryTabSaved(
  id: string,
  options: { savedQueryId?: string; savedVersion?: number; name?: string; baseSql?: string; savedParams?: Record<string, string> } = {},
): void {
  tabs = tabs.map((tab) => {
    if (tab.id !== id || tab.type !== 'query') return tab
    const name = options.name?.trim() ? options.name.trim() : tab.name
    const baseSql = options.baseSql ?? tab.sql
    return {
      ...tab,
      name,
      savedQueryId: options.savedQueryId ?? tab.savedQueryId,
      savedVersion: options.savedVersion ?? tab.savedVersion,
      baseSql,
      dirty: false,
      savedParams: options.savedParams ?? tab.savedParams,
    }
  })
  queueSave()
}

export function isTabDirty(id: string): boolean {
  const tab = tabs.find((entry) => entry.id === id)
  if (!tab) return false
  if (tab.type === 'query') return !!(tab as QueryTab).dirty
  if (tab.type === 'model') return !!(tab as ModelTab).dirty
  return false
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

// ── Model tabs ──────────────────────────────────────────────────

const MODEL_OPTION_DEFAULTS = {
  partitionBy: '',
  engineArgs: '',
  incrementalStrategy: '',
  uniqueKey: '',
  watermarkColumn: '',
}

function withModelEditDefaults(state: Partial<ModelEditState> | undefined): ModelEditState {
  return {
    modelName: '',
    description: '',
    targetDatabase: 'default',
    materialization: 'view',
    sqlBody: '',
    tableEngine: '',
    orderBy: '',
    ...MODEL_OPTION_DEFAULTS,
    ...(state ?? {}),
  }
}

function modelEditEqual(a: ModelEditState, b: ModelEditState): boolean {
  return a.modelName === b.modelName &&
    a.description === b.description &&
    a.targetDatabase === b.targetDatabase &&
    a.materialization === b.materialization &&
    a.sqlBody === b.sqlBody &&
    a.tableEngine === b.tableEngine &&
    a.orderBy === b.orderBy &&
    a.partitionBy === b.partitionBy &&
    a.engineArgs === b.engineArgs &&
    a.incrementalStrategy === b.incrementalStrategy &&
    a.uniqueKey === b.uniqueKey &&
    a.watermarkColumn === b.watermarkColumn
}

interface ModelTabInput {
  id: string
  name: string
  description: string
  target_database: string
  materialization: string
  sql_body: string
  table_engine: string
  order_by: string
  partition_by?: string
  engine_args?: string
  incremental_strategy?: string
  unique_key?: string
  watermark_column?: string
  status: string
  last_error: string | null
}

export function openModelTab(model: ModelTabInput, targetGroupId?: string): void {
  const existing = tabs.find(t => t.type === 'model' && (t as ModelTab).modelId === model.id) as ModelTab | undefined
  if (existing) {
    setActiveTab(existing.id)
    return
  }

  const editState: ModelEditState = {
    modelName: model.name,
    description: model.description,
    targetDatabase: model.target_database,
    materialization: model.materialization,
    sqlBody: model.sql_body,
    tableEngine: model.table_engine,
    orderBy: model.order_by,
    partitionBy: model.partition_by ?? '',
    engineArgs: model.engine_args ?? '',
    incrementalStrategy: model.incremental_strategy ?? '',
    uniqueKey: model.unique_key ?? '',
    watermarkColumn: model.watermark_column ?? '',
  }

  const tab: ModelTab = {
    id: createUUID(),
    type: 'model',
    name: model.name,
    modelId: model.id,
    dirty: false,
    edit: { ...editState },
    base: { ...editState },
    status: model.status,
    lastError: model.last_error ?? null,
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

export function updateModelTabEdit(tabId: string, partial: Partial<ModelEditState>): void {
  tabs = tabs.map(tab => {
    if (tab.id !== tabId || tab.type !== 'model') return tab
    const modelTab = tab as ModelTab
    const edit = { ...modelTab.edit, ...partial }
    const dirty = !modelEditEqual(edit, modelTab.base)
    const name = edit.modelName || modelTab.name
    return { ...modelTab, edit, dirty, name }
  })
  queueSave()
}

export function markModelTabSaved(tabId: string, model: { name: string; status: string; last_error: string | null }): void {
  tabs = tabs.map(tab => {
    if (tab.id !== tabId || tab.type !== 'model') return tab
    const modelTab = tab as ModelTab
    return { ...modelTab, base: { ...modelTab.edit }, dirty: false, name: model.name, status: model.status, lastError: model.last_error }
  })
  queueSave()
}

export function updateModelTabStatus(tabId: string, status: string, lastError: string | null): void {
  tabs = tabs.map(tab => {
    if (tab.id !== tabId || tab.type !== 'model') return tab
    return { ...tab, status, lastError }
  })
  queueSave()
}
