// SPDX-License-Identifier: BUSL-1.1
// Pro search for the command palette: grouped results, scope prefixes,
// telemetry and help items, entity search and the Ask Brain suggestion.
import {
  Activity, Bookmark, ChartBar, Cpu, FileText, GitBranch, Info, KeyRound,
  LayoutDashboard, MessageSquare, Network, Workflow, Zap,
} from 'lucide-svelte'
import type { Search } from 'lucide-svelte'
import { goTo, pushDashboardDetail } from '../../stores/router.svelte'
import { setSection } from '../../stores/nav.svelte'
import { listWorkspaceDashboards, listWorkspaceSavedQueries } from '../../api/workspace'
import { listDashboardFolders, folderPath } from '../../api/dashboards'
import type { DashboardFolder } from '../../types/api'
import { listModels } from '../../api/models'
import { listPipelines } from '../../api/pipelines'
import { listBrainChats } from '../../api/brain'

export type Group =
  | 'recent' | 'page' | 'table' | 'saved' | 'dashboard'
  | 'model' | 'pipeline' | 'brainchat' | 'telemetry' | 'action' | 'help'

export interface CommandItem {
  id: string
  group: Group
  label: string
  sub?: string
  icon: typeof Search
  shortcut?: string
  keywords?: string
  weight?: number
  run: () => void
}

export type Ranked = { item: CommandItem; score: number }
export type Grouped = Array<{ group: Group; items: Ranked[] }>

export const GROUP_LABEL: Record<Group, string> = {
  recent: 'Recent',
  page: 'Pages',
  action: 'Actions',
  saved: 'Saved queries',
  dashboard: 'Dashboards',
  model: 'Models',
  pipeline: 'Pipelines',
  brainchat: 'Brain chats',
  telemetry: 'Telemetry',
  table: 'Tables',
  help: 'Help',
}
const GROUP_ORDER: Group[] = [
  'recent', 'help', 'page', 'telemetry',
  'saved', 'dashboard', 'model', 'pipeline', 'brainchat',
  'table', 'action',
]

const PREFIXES: Record<string, Group> = {
  '>': 'action',
  't:': 'table',
  'q:': 'saved',
  'd:': 'dashboard',
  'm:': 'model',
  'p:': 'pipeline',
  'b:': 'brainchat',
  'tel:': 'telemetry',
  '?': 'help',
}

/** A scope prefix typed at the start of the input, e.g. "t: orders". */
export function matchPrefix(raw: string): { group: Group; rest: string } | null {
  for (const [prefix, group] of Object.entries(PREFIXES)) {
    if (raw === prefix || raw.startsWith(prefix + ' ')) {
      return { group, rest: raw.slice(prefix.length).trimStart() }
    }
  }
  return null
}

export function telemetryItems(): CommandItem[] {
  // Slugs are the Telemetry sections in routes.ts PAGE_SECTIONS.telemetry.
  const telTabs: Array<[string, string, typeof Search, string]> = [
    ['logs', 'Telemetry · Logs', FileText, 'log records ingest'],
    ['traces', 'Telemetry · Traces', GitBranch, 'spans waterfall trace'],
    ['metrics', 'Telemetry · Metrics', ChartBar, 'metrics gauges counters histogram'],
    ['service-map', 'Telemetry · Service map', Network, 'services dependencies rps p95 errors'],
    ['monitors', 'Telemetry · Monitors', Activity, 'monitors alerts thresholds'],
    ['sources', 'Telemetry · Sources', KeyRound, 'otlp ingest tokens endpoints sources'],
  ]
  const items: CommandItem[] = []
  for (const [slug, label, icon, kw] of telTabs) {
    items.push({
      id: `tel-${slug}`,
      group: 'telemetry',
      label,
      sub: 'Open telemetry tab',
      icon,
      keywords: kw,
      run: () => {
        goTo('telemetry', 'Telemetry')
        setSection(slug)
      },
    })
  }
  return items
}

function mkHelp(id: string, label: string, sub: string): CommandItem {
  return { id, group: 'help', label, sub, icon: Info, run: () => {} }
}

export function helpItems(cmd: string): CommandItem[] {
  return [
    mkHelp('help-prefixes', 'Prefixes — scope to one kind',
      '> actions · t: tables · q: saved queries · d: dashboards · m: models · p: pipelines · b: brain chats · tel: telemetry · ? help'),
    mkHelp('help-shortcuts', 'Keyboard shortcuts',
      `${cmd}K open palette · ${cmd}⇧N new query · ↑↓ select · Enter run · Esc close`),
    mkHelp('help-tip-brain', 'Type a question — Brain answers it',
      'End your query with "?" and hit Enter to seed a Brain chat with the prompt.'),
  ]
}

export interface ProEntities {
  savedQueries: Array<{ id: string; name: string; description?: string | null }>
  dashboards: Array<{ id: string; name: string; description?: string | null; folder_id?: string | null }>
  dashboardFolders: DashboardFolder[]
  models: Array<{ id: string; name: string; description?: string | null; target_database?: string }>
  pipelines: Array<{ id: string; name: string; description?: string | null; status?: string }>
  brainChats: Array<{ id: string; title: string }>
}

export function entityItems(e: ProEntities): CommandItem[] {
  const items: CommandItem[] = []

  for (const q of e.savedQueries) {
    items.push({
      id: `saved-${q.id}`,
      group: 'saved',
      label: q.name,
      sub: q.description || 'Saved query',
      icon: Bookmark,
      run: () => goTo('saved-queries', 'Saved Queries'),
    })
  }

  for (const d of e.dashboards) {
    items.push({
      id: `dash-${d.id}`,
      group: 'dashboard',
      label: d.name,
      sub: folderPath(e.dashboardFolders, d.folder_id).join(' / ') || d.description || 'Dashboard',
      icon: LayoutDashboard,
      run: () => pushDashboardDetail(d.id),
    })
  }

  for (const m of e.models) {
    items.push({
      id: `model-${m.id}`,
      group: 'model',
      label: m.name,
      sub: m.target_database ? `Model · ${m.target_database}` : (m.description || 'Model'),
      icon: Cpu,
      run: () => goTo('models', 'Models'),
    })
  }

  for (const p of e.pipelines) {
    items.push({
      id: `pipe-${p.id}`,
      group: 'pipeline',
      label: p.name,
      sub: p.status ? `Pipeline · ${p.status}` : (p.description || 'Pipeline'),
      icon: Workflow,
      run: () => goTo('pipelines', 'Pipelines'),
    })
  }

  for (const c of e.brainChats) {
    items.push({
      id: `chat-${c.id}`,
      group: 'brainchat',
      label: c.title || 'Untitled chat',
      sub: 'Brain chat',
      icon: MessageSquare,
      run: () => goTo('brain', 'Brain'),
    })
  }

  return items
}

/** Loads the searchable entities. A field is set only when its call settled. */
export async function loadProEntities(): Promise<Partial<ProEntities>> {
  const results = await Promise.allSettled([
    listWorkspaceSavedQueries().catch(() => []),
    listWorkspaceDashboards().catch(() => []),
    listModels().then(r => r.models ?? []).catch(() => []),
    listPipelines().then(r => r.pipelines ?? []).catch(() => []),
    listBrainChats(false).catch(() => []),
    listDashboardFolders().catch((): DashboardFolder[] => []),
  ])
  const out: Partial<ProEntities> = {}
  if (results[0].status === 'fulfilled') out.savedQueries = results[0].value
  if (results[1].status === 'fulfilled') out.dashboards = results[1].value
  if (results[2].status === 'fulfilled') out.models = results[2].value
  if (results[3].status === 'fulfilled') out.pipelines = results[3].value
  if (results[4].status === 'fulfilled') out.brainChats = results[4].value
  if (results[5].status === 'fulfilled') out.dashboardFolders = results[5].value
  return out
}

/** An "Ask Brain" item when the unscoped input reads like a question. */
export function brainSuggestion(parsed: { scope: Group | null; term: string }): CommandItem | null {
  const t = parsed.term.trim()
  if (parsed.scope) return null
  if (!t) return null
  const wordCount = t.split(/\s+/).length
  const looksLikeQuestion =
    t.endsWith('?') ||
    /^(how|why|what|where|when|show|find|give|tell|explain|list|count|top|do|does|can|should|is|are)\b/i.test(t)
  if (wordCount < 4 && !looksLikeQuestion) return null
  return {
    id: 'brain-ask',
    group: 'action',
    label: `Ask Brain: ${t}`,
    sub: 'Open Brain chat with this prompt',
    icon: Zap,
    shortcut: '↵',
    weight: 100,
    run: () => {
      try {
        sessionStorage.setItem('ch-ui-brain-prompt-seed', t)
      } catch {}
      goTo('brain', 'Brain')
    },
  }
}

/** Groups ranked matches: recent plus a curated set when empty, else per group. */
export function groupResults(ranked: Ranked[], scope: Group | null, inputEmpty: boolean): Grouped {
  if (inputEmpty) {
    const recentOnly = ranked.filter(x => x.item.group === 'recent').slice(0, 5)
    const curatedIds = new Set([
      'page-home', 'act-new-query', 'page-telemetry', 'page-brain',
      'page-saved-queries', 'page-dashboards',
    ])
    const curated = ranked.filter(x => curatedIds.has(x.item.id))
    const combined: Grouped = []
    if (recentOnly.length > 0) combined.push({ group: 'recent', items: recentOnly })
    if (curated.length > 0) combined.push({ group: 'page', items: curated })
    return combined
  }

  const buckets: Partial<Record<Group, Ranked[]>> = {}
  for (const r of ranked) {
    const g = r.item.group
    if (!buckets[g]) buckets[g] = []
    buckets[g]!.push(r)
  }
  const perGroupCap = scope ? 50 : 6
  const out: Grouped = []
  for (const g of GROUP_ORDER) {
    const arr = buckets[g]
    if (!arr || arr.length === 0) continue
    arr.sort((a, b) => b.score - a.score)
    out.push({ group: g, items: arr.slice(0, perGroupCap) })
  }
  return out
}

/** Splits a label into runs for fuzzy-match highlighting. */
export function highlight(label: string, term: string): Array<{ ch: string; on: boolean }> {
  if (!term) return [{ ch: label, on: false }]
  const lt = label.toLowerCase()
  const lq = term.toLowerCase()
  const out: Array<{ ch: string; on: boolean }> = []
  let ti = 0
  for (let i = 0; i < label.length; i++) {
    if (ti < lq.length && lt[i] === lq[ti]) {
      out.push({ ch: label[i], on: true })
      ti++
    } else {
      out.push({ ch: label[i], on: false })
    }
  }
  return out
}
