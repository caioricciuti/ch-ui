<script lang="ts">
  import { onDestroy, untrack } from 'svelte'
  import { goTo, goWorkspace, navigate, pushDashboardDetail } from '../lib/stores/router.svelte'
  import type { Dashboard, DashboardFolder, DashboardVariable, Panel, PanelLink } from '../lib/types/api'
  import { apiGet, apiPost, apiPut, apiDel, ApiError } from '../lib/api/client'
  import { openQueryTab } from '../lib/stores/tabs.svelte'
  import { linkLabel, resolveLink, type ClickContext, type PanelClick } from '../lib/utils/drilldown'
  import ContextMenu, { type ContextMenuItem } from '../lib/components/common/ContextMenu.svelte'
  import {
    listDashboardFolders, setDashboardStar, deleteDashboard as apiDeleteDashboard, fetchVariableOptions,
    type DashboardDetail,
  } from '../lib/api/dashboards'
  import {
    initialVariableValues, readVariablesFromUrl, reconcileValues, writeVariablesToUrl,
    type VariableValues,
  } from '../lib/utils/dashboard-variables'
  import DashboardBrowser from '../lib/components/dashboard/DashboardBrowser.svelte'
  import { success as toastSuccess, error as toastError } from '../lib/stores/toast.svelte'
  import { toDashboardTimeRangePayload, dashboardRangeFromUrlParams, dashboardRangeToUrlParams } from '../lib/utils/dashboard-time'
  import { nextFreeRow } from '../lib/utils/grid-layout'
  import { getSession } from '../lib/stores/session.svelte'
  import Button from '../lib/components/common/Button.svelte'
  import Spinner from '../lib/components/common/Spinner.svelte'
  import ConfirmDialog from '../lib/components/common/ConfirmDialog.svelte'
  import InputDialog from '../lib/components/common/InputDialog.svelte'
  import Badge from '../lib/components/common/Badge.svelte'
  import Tooltip from '../lib/components/common/Tooltip.svelte'
  import PanelEditor from '../lib/components/dashboard/PanelEditor.svelte'
  import DashboardGrid, { type PanelResult } from '../lib/components/dashboard/DashboardGrid.svelte'
  import type { ColumnMeta } from '../lib/types/query'
  import PanelRenderer from '../lib/components/dashboard/PanelRenderer.svelte'
  import TimeRangeSelector from '../lib/components/dashboard/TimeRangeSelector.svelte'
  import ShareDialog from '../lib/components/dashboard/ShareDialog.svelte'
  import DashboardSettings from '../lib/components/dashboard/DashboardSettings.svelte'
  import VariableBar from '../lib/components/dashboard/VariableBar.svelte'
  import ReportEditor from '../lib/components/reports/ReportEditor.svelte'
  import { fetchReports, type ReportChannel, type ReportInput, type ReportWorkerMode } from '../lib/api/reports'
  import { isProActive } from '../lib/stores/license.svelte'
  import { Plus, RefreshCw, Share2, ChevronDown, ChevronRight, Star, Timer, Settings, Info, X, Pencil, Mail } from 'lucide-svelte'

  interface Props {
    dashboardId?: string
  }

  let { dashboardId }: Props = $props()

  // Viewers are read only: the server refuses their writes, so the page does
  // not offer them (same rule as the dashboard browser).
  const session = $derived(getSession())
  const canWrite = $derived(session?.role !== 'viewer')

  // Folders, for the breadcrumb of the detail view
  let folders = $state<DashboardFolder[]>([])
  const folderCrumbs = $derived.by(() => {
    if (!currentDashboard?.folder_id) return [] as DashboardFolder[]
    const byId = new Map(folders.map((f) => [f.id, f]))
    const out: DashboardFolder[] = []
    let cur = byId.get(currentDashboard.folder_id)
    for (let i = 0; cur && i < 64; i++) {
      out.unshift(cur)
      cur = cur.parent_id ? byId.get(cur.parent_id) : undefined
    }
    return out
  })

  // Detail view
  let currentDashboard = $state<Dashboard | null>(null)
  let panels = $state<Panel[]>([])
  let panelResults = $state<Map<string, PanelResult>>(new Map())
  let detailLoading = $state(false)
  let detailError = $state<string | null>(null)
  let loadedDashboardId = $state<string | null>(null)
  let dashboardTimeRange = $state(localStorage.getItem('ch-ui-dashboard-time-range') ?? '1h')
  let dashboardVariables = $state<DashboardVariable[]>([])
  let variableValues = $state<VariableValues>({})

  /** The range in the address bar (?from=&to=), so a copied link opens on the same range. */
  function writeRangeToUrl(range: string) {
    const url = new URL(window.location.href)
    const { from, to } = dashboardRangeToUrlParams(range)
    url.searchParams.set('from', from)
    url.searchParams.set('to', to)
    history.replaceState(history.state, '', url)
  }

  function readRangeFromUrl(): string | null {
    const params = new URLSearchParams(window.location.search)
    return dashboardRangeFromUrlParams(params.get('from'), params.get('to'))
  }

  // Panels built on another connection run on the session's connection;
  // they say so instead of failing with a confusing error.
  let connectionNames = $state<Map<string, string>>(new Map())
  let connectionNamesRequested = false

  function connectionNotice(p: Panel): string | null {
    const current = session?.connectionId
    if (!p.connection_id || !current || p.connection_id === current) return null
    const built = connectionNames.get(p.connection_id) ?? 'another connection'
    const running = session?.connectionName || 'this connection'
    return `Built on ${built}, running on ${running}`
  }

  $effect(() => {
    const current = session?.connectionId
    if (connectionNamesRequested || !current) return
    if (!panels.some((p) => p.connection_id && p.connection_id !== current)) return
    connectionNamesRequested = true
    apiGet<{ id: string; name: string }[]>('/api/connections')
      .then((list) => { connectionNames = new Map(list.map((c) => [c.id, c.name])) })
      .catch(() => {})
  })

  // Panel editor page
  let panelEditorOpen = $state(false)
  let editingPanel = $state<Panel | null>(null)
  let confirmOpen = $state(false)
  let confirmLoading = $state(false)
  let confirmTitle = $state('')
  let confirmDescription = $state('')
  let confirmTargetDashboardId = $state<string | null>(null)
  let confirmTargetPanelId = $state<string | null>(null)

  // Share dialog
  let shareDialogOpen = $state(false)

  // Dashboard settings
  let settingsOpen = $state(false)

  // Subscribe: a scheduled report of this dashboard (Pro), prefilled with
  // the current range and variable values.
  let subscribeOpen = $state(false)
  let subscribePrefill = $state<Partial<ReportInput> | null>(null)
  let subscribeChannels = $state<ReportChannel[]>([])
  let subscribeModes = $state<Record<string, ReportWorkerMode>>({})
  let subscribeDomains = $state<string[]>([])
  let subscribeLoading = $state(false)
  const canSubscribe = $derived(canWrite && isProActive())

  async function openSubscribe() {
    if (!currentDashboard || subscribeLoading) return
    subscribeLoading = true
    try {
      const res = await fetchReports()
      subscribeChannels = res.channels
      subscribeModes = res.worker_modes
      subscribeDomains = res.recipient_domains
      const range = /^[1-9][0-9]{0,3}[mhdwM]$/.test(dashboardTimeRange) ? dashboardTimeRange : '24h'
      subscribePrefill = {
        name: currentDashboard.name,
        source_type: 'dashboard',
        dashboard_id: currentDashboard.id,
        connection_id: session?.connectionId ?? '',
        time_range: range,
        variables: { ...variableValues },
      }
      subscribeOpen = true
    } catch (e: unknown) {
      toastError(e instanceof Error ? e.message : String(e))
    } finally {
      subscribeLoading = false
    }
  }

  // Fullscreen panel view
  let fullscreenPanel = $state<Panel | null>(null)

  // Auto-refresh
  const REFRESH_OPTIONS: { label: string; seconds: number }[] = [
    { label: 'Off', seconds: 0 },
    { label: '5s', seconds: 5 },
    { label: '10s', seconds: 10 },
    { label: '30s', seconds: 30 },
    { label: '1m', seconds: 60 },
    { label: '5m', seconds: 300 },
    { label: '15m', seconds: 900 },
    { label: '30m', seconds: 1800 },
    { label: '1h', seconds: 3600 },
  ]
  let refreshInterval = $state(0)
  let refreshTimer = $state<ReturnType<typeof setInterval> | null>(null)
  let refreshDropdownOpen = $state(false)
  let refreshDropdownEl = $state<HTMLDivElement>(undefined!)
  let refreshBtnEl = $state<HTMLButtonElement>(undefined!)
  let refreshCountdown = $state(0)
  let countdownTimer = $state<ReturnType<typeof setInterval> | null>(null)

  function setRefreshInterval(seconds: number) {
    refreshInterval = seconds
    refreshDropdownOpen = false

    clearRefreshTimers()
    refreshCountdown = seconds

    if (seconds > 0) {
      runAllPanelQueries()
      refreshCountdown = seconds
      countdownTimer = setInterval(() => {
        refreshCountdown = Math.max(0, refreshCountdown - 1)
      }, 1000)
      refreshTimer = setInterval(() => {
        refreshCountdown = seconds
        // A slow dashboard must not pile up queries: skip this tick while
        // the previous refresh is still running.
        if (panelsInFlight > 0) return
        runAllPanelQueries()
      }, seconds * 1000)
    }
  }

  $effect(() => {
    if (refreshDropdownOpen) {
      const handler = (e: MouseEvent) => {
        if (!refreshDropdownEl?.contains(e.target as Node) && !refreshBtnEl?.contains(e.target as Node)) {
          refreshDropdownOpen = false
        }
      }
      document.addEventListener('mousedown', handler)
      return () => document.removeEventListener('mousedown', handler)
    }
  })

  function clearRefreshTimers() {
    if (refreshTimer) { clearInterval(refreshTimer); refreshTimer = null }
    if (countdownTimer) { clearInterval(countdownTimer); countdownTimer = null }
  }

  // Clean up timers when leaving dashboard detail
  $effect(() => {
    const id = dashboardId
    if (!id) {
      clearRefreshTimers()
      refreshInterval = 0
      refreshCountdown = 0
    }
  })

  // Leaving the page entirely must stop auto-refresh too, or every panel
  // query keeps running in the background.
  onDestroy(clearRefreshTimers)

  // Rename
  let renameOpen = $state(false)
  let renaming = $state(false)
  let titleInput = $state('')

  function openRename() {
    if (!currentDashboard || !canWrite) return
    titleInput = currentDashboard.name
    renameOpen = true
  }

  $effect(() => {
    const id = dashboardId
    if (!id) {
      if (loadedDashboardId !== null) {
        currentDashboard = null
        panels = []
        dashboardVariables = []
        variableValues = {}
        panelResults = new Map()
        loadedDashboardId = null
      }
      return
    }
    if (loadedDashboardId === id) return
    loadedDashboardId = id
    untrack(() => {
      const fromUrl = readRangeFromUrl()
      if (fromUrl) dashboardTimeRange = fromUrl
      writeRangeToUrl(dashboardTimeRange)
    })
    void loadDashboardDetail(id)
    if (folders.length === 0) listDashboardFolders().then((f) => (folders = f)).catch(() => {})
  })

  async function deleteDashboard(id: string) {
    try {
      await apiDeleteDashboard(id)
      toastSuccess('Dashboard deleted')
      goTo('dashboards', 'Dashboards')
    } catch (e: unknown) {
      toastError(errorMessage(e))
    }
  }

  function errorMessage(e: unknown): string {
    return e instanceof Error ? e.message : String(e)
  }

  // Switching dashboards quickly must not let an earlier response win.
  let detailSeq = 0

  async function loadDashboardDetail(id: string) {
    const seq = ++detailSeq
    detailLoading = true
    detailError = null
    panelResults = new Map()
    try {
      const res = await apiGet<Partial<DashboardDetail>>(`/api/dashboards/${id}`)
      if (seq !== detailSeq) return
      currentDashboard = res.dashboard ?? null
      panels = res.panels ?? []
      setVariables(res.variables ?? [], readVariablesFromUrl())
      runAllPanelQueries(res.panels ?? [])
    } catch (e: unknown) {
      if (seq !== detailSeq) return
      detailError = errorMessage(e)
      toastError(errorMessage(e))
    } finally {
      if (seq === detailSeq) detailLoading = false
    }
  }

  /** New definitions: keep the values that still fit them, mirrored in the URL. */
  function setVariables(defs: DashboardVariable[], values: VariableValues) {
    dashboardVariables = defs
    variableValues = initialVariableValues(defs, values)
    writeVariablesToUrl(defs, variableValues)
  }

  function handleVariableValuesChange(values: VariableValues) {
    variableValues = reconcileValues(dashboardVariables, values)
    writeVariablesToUrl(dashboardVariables, variableValues)
    runAllPanelQueries()
  }

  function handleVariablesSaved(defs: DashboardVariable[]) {
    setVariables(defs, variableValues)
    runAllPanelQueries()
  }

  function loadVariableOptions(name: string, values: VariableValues) {
    return fetchVariableOptions(currentDashboard?.id ?? dashboardId ?? '', name, {
      variables: values,
      time_range: toDashboardTimeRangePayload(dashboardTimeRange || '1h'),
    })
  }

  function openDashboardListTab() {
    goTo('dashboards', 'Dashboards')
  }

  function openFolderInList(folderId: string) {
    navigate(`/dashboards?folder=${encodeURIComponent(folderId)}`)
  }

  async function toggleCurrentStar() {
    if (!currentDashboard) return
    const next = !currentDashboard.starred
    currentDashboard = { ...currentDashboard, starred: next }
    try {
      await setDashboardStar(currentDashboard.id, next)
    } catch (e: unknown) {
      currentDashboard = { ...currentDashboard, starred: !next }
      toastError(errorMessage(e))
    }
  }

  function runAllPanelQueries(panelsToRun = panels) {
    for (const p of panelsToRun) runPanelQuery(p)
  }

  // Per panel request sequence: only the latest request for a panel may
  // write its result, so a slow older response never overwrites a newer one.
  const panelSeq = new Map<string, number>()
  let panelsInFlight = 0

  async function runPanelQuery(p: Panel) {
    if (p.panel_type === 'text') return

    const seq = (panelSeq.get(p.id) ?? 0) + 1
    panelSeq.set(p.id, seq)
    const dashSeq = detailSeq
    const isCurrent = () => panelSeq.get(p.id) === seq && dashSeq === detailSeq

    const existing = panelResults.get(p.id)
    const hasData = existing && (existing.data.length > 0 || existing.meta.length > 0)

    if (!hasData) {
      const updated = new Map(panelResults)
      updated.set(p.id, { data: [], meta: [], loading: true })
      panelResults = updated
    }

    const rangeValue = dashboardTimeRange || '1h'

    panelsInFlight++
    try {
      const res = await apiPost<{ data: Record<string, unknown>[]; meta: ColumnMeta[]; error?: string }>('/api/dashboards/query', {
        query: p.query,
        time_range: toDashboardTimeRangePayload(rangeValue),
        dashboard_id: p.dashboard_id || currentDashboard?.id,
        variables: variableValues,
      })
      if (!isCurrent()) return
      const next = new Map(panelResults)
      next.set(p.id, { data: res.data ?? [], meta: res.meta ?? [], loading: false })
      panelResults = next
    } catch (e: unknown) {
      if (!isCurrent()) return
      const next = new Map(panelResults)
      const prev = panelResults.get(p.id)
      next.set(p.id, { data: prev?.data ?? [], meta: prev?.meta ?? [], error: errorMessage(e), loading: false })
      panelResults = next
    } finally {
      panelsInFlight--
    }
  }

  function handleTimeRangeChange(nextRange: string) {
    dashboardTimeRange = nextRange
    localStorage.setItem('ch-ui-dashboard-time-range', nextRange)
    writeRangeToUrl(nextRange)
    runAllPanelQueries()
  }

  async function saveDashboardTitle(name: string) {
    if (!currentDashboard || !name || renaming) return
    if (name === currentDashboard.name) {
      renameOpen = false
      return
    }
    renaming = true
    try {
      await apiPut(`/api/dashboards/${currentDashboard.id}`, { name })
      currentDashboard = { ...currentDashboard, name }
      renameOpen = false
      toastSuccess('Dashboard renamed')
    } catch (e: unknown) {
      toastError(errorMessage(e))
    } finally {
      renaming = false
    }
  }

  function openAddPanel() {
    editingPanel = null
    panelEditorOpen = true
  }

  function openEditPanel(p: Panel) {
    editingPanel = p
    panelEditorOpen = true
  }

  function handlePanelSaved(savedPanel: Panel) {
    if (editingPanel?.id) {
      panels = panels.map(p => p.id === editingPanel!.id ? savedPanel : p)
    } else {
      panels = [...panels, savedPanel]
    }
    runPanelQuery(savedPanel)
    panelEditorOpen = false
    toastSuccess(editingPanel?.id ? 'Panel updated' : 'Panel created')
  }

  async function duplicatePanel(p: Panel) {
    if (!currentDashboard) return
    try {
      const maxY = nextFreeRow(panels.map((pan) => ({ y: pan.layout_y, h: pan.layout_h })))
      const body: Record<string, unknown> = {
        name: p.name + ' (copy)',
        panel_type: p.panel_type,
        query: p.query,
        config: p.config,
        layout_x: p.layout_x,
        layout_y: maxY,
        layout_w: p.layout_w,
        layout_h: p.layout_h,
      }
      if (p.connection_id) body.connection_id = p.connection_id
      const res = await apiPost<{ panel: Panel; id?: string }>(`/api/dashboards/${currentDashboard.id}/panels`, body)
      if (res.panel) {
        panels = [...panels, res.panel]
        runPanelQuery(res.panel)
      }
      toastSuccess('Panel duplicated')
    } catch (e: unknown) {
      toastError('Failed to duplicate: ' + errorMessage(e))
    }
  }

  async function deletePanel(panelId: string) {
    if (!currentDashboard) return
    try {
      await apiDel(`/api/dashboards/${currentDashboard.id}/panels/${panelId}`)
      panels = panels.filter(p => p.id !== panelId)
      toastSuccess('Panel deleted')
    } catch (e: unknown) {
      toastError(errorMessage(e))
    }
  }

  function requestDeleteDashboard(id: string) {
    confirmTargetDashboardId = id
    confirmTargetPanelId = null
    confirmTitle = 'Delete dashboard?'
    confirmDescription = 'This will permanently remove the dashboard and all its panels.'
    confirmOpen = true
  }

  function requestDeletePanel(id: string) {
    confirmTargetDashboardId = null
    confirmTargetPanelId = id
    confirmTitle = 'Delete panel?'
    confirmDescription = 'This panel will be permanently removed from the dashboard.'
    confirmOpen = true
  }

  async function confirmDelete() {
    if (confirmLoading) return
    confirmLoading = true
    try {
      if (confirmTargetDashboardId) {
        await deleteDashboard(confirmTargetDashboardId)
      } else if (confirmTargetPanelId) {
        await deletePanel(confirmTargetPanelId)
      }
      confirmOpen = false
      confirmTargetDashboardId = null
      confirmTargetPanelId = null
    } finally {
      confirmLoading = false
    }
  }

  $effect(() => {
    if (!fullscreenPanel) return
    const handler = (e: KeyboardEvent) => { if (e.key === 'Escape') fullscreenPanel = null }
    document.addEventListener('keydown', handler)
    return () => document.removeEventListener('keydown', handler)
  })

  // Drilldown: a click on a panel with links runs its one link, or offers
  // the links in a menu at the click.
  let linkMenu = $state<{ x: number; y: number; items: ContextMenuItem[] } | null>(null)

  function handlePanelClick(p: Panel, click: PanelClick) {
    const links = parsePanelConfig(p.config).links ?? []
    if (links.length === 0) return
    if (links.length === 1) {
      void runLink(links[0], click.ctx)
      return
    }
    linkMenu = {
      x: click.x,
      y: click.y,
      items: links.map((link, i) => ({ id: String(i), label: linkLabel(link), onSelect: () => void runLink(link, click.ctx) })),
    }
  }

  async function runLink(link: PanelLink, ctx: ClickContext) {
    const action = resolveLink(link, ctx, {
      variables: variableValues,
      range: dashboardRangeToUrlParams(dashboardTimeRange || '1h'),
    })
    switch (action.kind) {
      case 'setVariable':
        if (!dashboardVariables.some((v) => v.name === action.variable)) {
          toastError(`This dashboard has no variable ${action.variable}`)
          return
        }
        handleVariableValuesChange({ ...variableValues, [action.variable]: [action.value] })
        return
      case 'dashboard':
        try {
          await apiGet(`/api/dashboards/${encodeURIComponent(action.dashboardId)}`)
        } catch (e: unknown) {
          toastError(e instanceof ApiError && e.status === 404 ? 'That dashboard no longer exists' : errorMessage(e))
          return
        }
        pushDashboardDetail(action.dashboardId, action.search)
        // The same dashboard is not loaded again: take the new values here.
        if (action.dashboardId === loadedDashboardId) {
          const fromUrl = readRangeFromUrl()
          if (fromUrl) dashboardTimeRange = fromUrl
          setVariables(dashboardVariables, readVariablesFromUrl())
          runAllPanelQueries()
        }
        return
      case 'sql':
        openQueryTab(action.sql)
        goWorkspace()
        return
      case 'url':
        window.open(action.url, '_blank', 'noopener,noreferrer')
        return
      case 'blocked':
        toastError(action.reason)
    }
  }

  function parsePanelConfig(configStr: string): import('../lib/types/api').PanelConfig {
    try {
      return JSON.parse(configStr || '{}')
    } catch {
      return { chartType: 'table' }
    }
  }

</script>

<div class="flex h-full min-h-0 flex-col">
  {#if !dashboardId}
    <DashboardBrowser />
  {:else}
    <div class="flex h-12 shrink-0 items-center gap-2 border-b border-edge-subtle px-5">
      <nav class="flex min-w-0 items-center gap-1 text-[13px]" aria-label="Breadcrumb">
        <button class="shrink-0 text-fg-3 transition-colors hover:text-fg" onclick={openDashboardListTab}>Dashboards</button>
        {#each folderCrumbs as crumb (crumb.id)}
          <ChevronRight size={13} class="shrink-0 text-fg-4" />
          <button class="shrink-0 truncate text-fg-3 transition-colors hover:text-fg" onclick={() => openFolderInList(crumb.id)}>{crumb.name}</button>
        {/each}
        <ChevronRight size={13} class="shrink-0 text-fg-4" />
        <h1
          class="truncate text-[15px] font-semibold tracking-[-0.01em] text-fg {canWrite ? 'cursor-text' : ''}"
          ondblclick={openRename}
          title={canWrite ? 'Double-click to rename' : undefined}
        >
          {currentDashboard?.name ?? 'Dashboard'}
        </h1>
      </nav>

      {#if currentDashboard}
        {#if canWrite}
          <button
            class="shrink-0 rounded p-0.5 text-fg-4 transition-colors hover:text-fg"
            onclick={openRename}
            aria-label="Rename dashboard"
            title="Rename dashboard"
          >
            <Pencil size={13} />
          </button>
        {/if}
        <button
          class="shrink-0 rounded p-0.5 transition-colors {currentDashboard.starred ? 'text-warning' : 'text-fg-4 hover:text-fg'}"
          onclick={toggleCurrentStar}
          aria-label={currentDashboard.starred ? 'Unstar' : 'Star'}
          aria-pressed={currentDashboard.starred}
          title={currentDashboard.starred ? 'Unstar' : 'Star'}
        >
          <Star size={14} fill={currentDashboard.starred ? 'currentColor' : 'none'} />
        </button>
        {#each currentDashboard.tags.slice(0, 4) as t (t)}<Badge>{t}</Badge>{/each}
      {/if}

      {#if currentDashboard?.description}
        <Tooltip text={currentDashboard.description} side="bottom">
          <span class="inline-flex cursor-help text-fg-4 hover:text-fg"><Info size={14} /></span>
        </Tooltip>
      {/if}

      <div class="ml-auto flex items-center gap-2">
        <TimeRangeSelector value={dashboardTimeRange} onchange={handleTimeRangeChange} />
        {#if panelEditorOpen}
          <span class="text-xs text-fg-3">
            Panel builder mode
          </span>
        {:else}
          {#if canWrite}
            <Button size="sm" variant="outline" onclick={() => settingsOpen = true}>
              <Settings size={14} /> Settings
            </Button>
            <Button size="sm" variant="outline" onclick={() => shareDialogOpen = true}>
              <Share2 size={14} /> Share
            </Button>
          {/if}
          {#if canSubscribe}
            <Button size="sm" variant="outline" loading={subscribeLoading} onclick={() => void openSubscribe()} title="Send a summary of this dashboard on a schedule">
              <Mail size={14} /> Subscribe
            </Button>
          {/if}
          <!-- Refresh with auto-refresh picker -->
          <div class="relative flex items-center">
            <button
              class="inline-flex h-7 items-center gap-1.5 rounded-l-md border border-edge bg-transparent px-2.5 text-xs font-medium text-fg-2 transition-colors hover:border-edge-strong hover:bg-hover hover:text-fg"
              onclick={() => { runAllPanelQueries(); if (refreshInterval > 0) { refreshCountdown = refreshInterval } }}
              title="Refresh now"
            >
              <RefreshCw size={13} class={refreshInterval > 0 ? 'animate-spin-slow text-accent' : ''} />
              {#if refreshInterval > 0}
                <span class="tabular-nums text-accent">{refreshCountdown}s</span>
              {:else}
                Refresh
              {/if}
            </button>
            <button
              bind:this={refreshBtnEl}
              class="inline-flex h-7 items-center rounded-r-md border border-l-0 border-edge bg-transparent px-1.5 text-fg-3 transition-colors hover:bg-hover hover:text-fg"
              onclick={() => refreshDropdownOpen = !refreshDropdownOpen}
              title="Auto-refresh interval"
              aria-label="Auto-refresh interval"
            >
              <ChevronDown size={12} />
            </button>

            {#if refreshDropdownOpen}
              <div
                bind:this={refreshDropdownEl}
                class="surface-card absolute right-0 top-full z-50 mt-1 w-36 overflow-hidden rounded-md py-1"
              >
                <div class="px-3 py-1.5">
                  <span class="text-[10px] font-medium uppercase tracking-wider text-fg-4">Auto refresh</span>
                </div>
                {#each REFRESH_OPTIONS as opt (opt.seconds)}
                  <button
                    class="flex w-full items-center justify-between px-3 py-1.5 text-xs transition-colors {refreshInterval === opt.seconds ? 'bg-accent-soft font-medium text-accent' : 'text-fg-2 hover:bg-hover'}"
                    onclick={() => setRefreshInterval(opt.seconds)}
                  >
                    {opt.label}
                    {#if refreshInterval === opt.seconds && opt.seconds > 0}
                      <Timer size={11} class="text-accent" />
                    {/if}
                  </button>
                {/each}
              </div>
            {/if}
          </div>
          {#if canWrite}
            <Button size="sm" onclick={openAddPanel}>
              <Plus size={14} /> Add Panel
            </Button>
          {/if}
        {/if}
      </div>
    </div>

    {#if currentDashboard && dashboardVariables.length > 0}
      <VariableBar
        variables={dashboardVariables}
        values={variableValues}
        loadOptions={loadVariableOptions}
        onchange={handleVariableValuesChange}
      />
    {/if}

    <div class="flex-1 min-h-0 {panelEditorOpen ? 'overflow-hidden' : 'overflow-auto p-4'}">
      {#if panelEditorOpen}
        <PanelEditor
          dashboardId={currentDashboard?.id ?? ''}
          dashboardTimeRange={dashboardTimeRange}
          panel={editingPanel}
          nextRow={nextFreeRow(panels.map((p) => ({ y: p.layout_y, h: p.layout_h })))}
          connectionId={session?.connectionId ?? ''}
          variables={variableValues}
          variableDefs={dashboardVariables}
          onclose={() => panelEditorOpen = false}
          onsave={handlePanelSaved}
        />
      {:else if detailLoading}
        <div class="flex items-center justify-center py-12"><Spinner /></div>
      {:else if detailError}
        <div class="rounded-md border border-danger/30 bg-danger-soft p-3 text-[13px] text-danger">{detailError}</div>
      {:else if currentDashboard}
        <DashboardGrid
          dashboardId={currentDashboard.id}
          {panels}
          {panelResults}
          onpanelschange={(updated) => { panels = updated }}
          oneditpanel={openEditPanel}
          onduplicatepanel={duplicatePanel}
          ondeletepanel={requestDeletePanel}
          onmaximizepanel={(p) => fullscreenPanel = p}
          onaddpanel={openAddPanel}
          {canWrite}
          {connectionNotice}
          onpanelclick={handlePanelClick}
        />
      {/if}
    </div>
  {/if}
</div>

<ContextMenu
  open={linkMenu !== null}
  x={linkMenu?.x ?? 0}
  y={linkMenu?.y ?? 0}
  items={linkMenu?.items ?? []}
  onclose={() => (linkMenu = null)}
/>

<InputDialog
  open={renameOpen}
  title="Rename dashboard"
  placeholder="Dashboard name"
  bind:value={titleInput}
  confirmLabel="Rename"
  loading={renaming}
  onconfirm={saveDashboardTitle}
  oncancel={() => (renameOpen = false)}
/>

<ConfirmDialog
  open={confirmOpen}
  title={confirmTitle}
  description={confirmDescription}
  confirmLabel="Delete"
  destructive={true}
  loading={confirmLoading}
  onconfirm={confirmDelete}
  oncancel={() => confirmOpen = false}
/>

<ReportEditor
  open={subscribeOpen}
  report={null}
  prefill={subscribePrefill}
  channels={subscribeChannels}
  workerModes={subscribeModes}
  recipientDomains={subscribeDomains}
  onclose={() => (subscribeOpen = false)}
  onsaved={() => (subscribeOpen = false)}
/>

{#if currentDashboard}
  <ShareDialog
    open={shareDialogOpen}
    dashboardId={currentDashboard.id}
    dashboardName={currentDashboard.name}
    onclose={() => shareDialogOpen = false}
  />
  <DashboardSettings
    open={settingsOpen}
    dashboard={currentDashboard}
    {panels}
    variables={dashboardVariables}
    onclose={() => settingsOpen = false}
    onvariableschange={handleVariablesSaved}
    onimported={(d, p, v) => {
      currentDashboard = d
      panels = p
      if (v) setVariables(v, variableValues)
      runAllPanelQueries(p)
      settingsOpen = false
    }}
    onduplicated={(detail) => { settingsOpen = false; pushDashboardDetail(detail.dashboard.id) }}
    ondelete={() => { settingsOpen = false; requestDeleteDashboard(currentDashboard!.id) }}
  />
{/if}

{#if fullscreenPanel}
  {@const fsResult = panelResults.get(fullscreenPanel.id)}
  {@const fsCfg = parsePanelConfig(fullscreenPanel.config)}
  {@const fsIsTable = !['text', 'stat', 'gauge', 'pie', 'timeseries', 'bar', 'histogram', 'scatter', 'heatmap'].includes(fullscreenPanel.panel_type)}
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="fixed inset-0 z-[9999] bg-canvas flex flex-col"
    onkeydown={(e) => { if (e.key === 'Escape') fullscreenPanel = null }}
    tabindex="-1"
  >
    <div class="flex h-12 shrink-0 items-center justify-between border-b border-edge-subtle px-5">
      <span class="text-[15px] font-semibold tracking-[-0.01em] text-fg">{fullscreenPanel.name}</span>
      <Button icon variant="ghost" size="sm" aria-label="Close" onclick={() => fullscreenPanel = null}>
        <X size={16} />
      </Button>
    </div>
    <div class="flex-1 min-h-0 overflow-hidden {fullscreenPanel.panel_type === 'text' ? '' : 'p-4'}">
      {#if fullscreenPanel.panel_type === 'text'}
        <PanelRenderer type="text" data={[]} meta={[]} config={fsCfg} />
      {:else if !fsResult || fsResult.loading}
        <div class="flex items-center justify-center h-full"><Spinner /></div>
      {:else if fsResult.error}
        <p class="p-4 text-[13px] text-danger">{fsResult.error}</p>
      {:else if fsIsTable && fsResult.meta.length === 0}
        <p class="p-4 text-[13px] text-fg-3">No data</p>
      {:else if fsIsTable}
        <!-- Virtualized: a fullscreen table can hold every row of a large result. -->
        <div class="flex h-full min-h-0 flex-col overflow-hidden rounded-lg border border-edge-subtle">
          <PanelRenderer type={fullscreenPanel.panel_type} data={fsResult.data} meta={fsResult.meta} config={fsCfg} table="virtual" />
        </div>
      {:else}
        <PanelRenderer type={fullscreenPanel.panel_type} data={fsResult.data} meta={fsResult.meta} config={fsCfg} />
      {/if}
    </div>
  </div>
{/if}
