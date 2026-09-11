<script lang="ts">
  import { goTo, navigate } from '../lib/stores/router.svelte'
  import type { Dashboard, DashboardFolder, Panel } from '../lib/types/api'
  import { apiGet, apiPost, apiPut, apiDel } from '../lib/api/client'
  import { listDashboardFolders, setDashboardStar, deleteDashboard as apiDeleteDashboard } from '../lib/api/dashboards'
  import DashboardBrowser from '../lib/components/dashboard/DashboardBrowser.svelte'
  import { success as toastSuccess, error as toastError } from '../lib/stores/toast.svelte'
  import { toDashboardTimeRangePayload } from '../lib/utils/dashboard-time'
  import Button from '../lib/components/common/Button.svelte'
  import Spinner from '../lib/components/common/Spinner.svelte'
  import ConfirmDialog from '../lib/components/common/ConfirmDialog.svelte'
  import Badge from '../lib/components/common/Badge.svelte'
  import Tooltip from '../lib/components/common/Tooltip.svelte'
  import PanelEditor from '../lib/components/dashboard/PanelEditor.svelte'
  import DashboardGrid from '../lib/components/dashboard/DashboardGrid.svelte'
  import StatPanel from '../lib/components/dashboard/StatPanel.svelte'
  import ChartPanel from '../lib/components/dashboard/ChartPanel.svelte'
  import MarkdownPanel from '../lib/components/dashboard/MarkdownPanel.svelte'
  import GaugePanel from '../lib/components/dashboard/GaugePanel.svelte'
  import PiePanel from '../lib/components/dashboard/PiePanel.svelte'
  import { computeStat } from '../lib/utils/chart-transform'
  import TimeRangeSelector from '../lib/components/dashboard/TimeRangeSelector.svelte'
  import ShareDialog from '../lib/components/dashboard/ShareDialog.svelte'
  import DashboardSettings from '../lib/components/dashboard/DashboardSettings.svelte'
  import { Plus, RefreshCw, Share2, ChevronDown, ChevronRight, Star, Timer, Settings, Info, X } from 'lucide-svelte'

  interface Props {
    dashboardId?: string
  }

  let { dashboardId }: Props = $props()

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
  let panelResults = $state<Map<string, { data: any[]; meta: any[]; error?: string; loading: boolean }>>(new Map())
  let detailLoading = $state(false)
  let detailError = $state<string | null>(null)
  let loadedDashboardId = $state<string | null>(null)
  let dashboardTimeRange = $state(localStorage.getItem('ch-ui-dashboard-time-range') ?? '1h')

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

    if (refreshTimer) { clearInterval(refreshTimer); refreshTimer = null }
    if (countdownTimer) { clearInterval(countdownTimer); countdownTimer = null }
    refreshCountdown = seconds

    if (seconds > 0) {
      runAllPanelQueries()
      refreshCountdown = seconds
      countdownTimer = setInterval(() => {
        refreshCountdown = Math.max(0, refreshCountdown - 1)
      }, 1000)
      refreshTimer = setInterval(() => {
        refreshCountdown = seconds
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

  // Clean up timers when leaving dashboard detail
  $effect(() => {
    const id = dashboardId
    if (!id) {
      if (refreshTimer) { clearInterval(refreshTimer); refreshTimer = null }
      if (countdownTimer) { clearInterval(countdownTimer); countdownTimer = null }
      refreshInterval = 0
      refreshCountdown = 0
    }
  })

  // Inline edit
  let editingTitle = $state(false)
  let titleInput = $state('')

  $effect(() => {
    const id = dashboardId
    if (!id) {
      if (loadedDashboardId !== null) {
        currentDashboard = null
        panels = []
        panelResults = new Map()
        loadedDashboardId = null
      }
      return
    }
    if (loadedDashboardId === id) return
    loadedDashboardId = id
    void loadDashboardDetail(id)
    if (folders.length === 0) listDashboardFolders().then((f) => (folders = f)).catch(() => {})
  })

  async function deleteDashboard(id: string) {
    try {
      await apiDeleteDashboard(id)
      toastSuccess('Dashboard deleted')
      goTo('dashboards', 'Dashboards')
    } catch (e: any) {
      toastError(e.message)
    }
  }

  async function loadDashboardDetail(id: string) {
    detailLoading = true
    detailError = null
    panelResults = new Map()
    try {
      const res = await apiGet<{ dashboard: Dashboard; panels: Panel[] }>(`/api/dashboards/${id}`)
      currentDashboard = res.dashboard
      panels = res.panels ?? []
      runAllPanelQueries(res.panels ?? [])
    } catch (e: any) {
      detailError = e.message
      toastError(e.message)
    } finally {
      detailLoading = false
    }
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
    } catch (e: any) {
      currentDashboard = { ...currentDashboard, starred: !next }
      toastError(e.message)
    }
  }

  function runAllPanelQueries(panelsToRun = panels) {
    for (const p of panelsToRun) runPanelQuery(p)
  }

  async function runPanelQuery(p: Panel) {
    if (p.panel_type === 'text') return

    const existing = panelResults.get(p.id)
    const hasData = existing && (existing.data.length > 0 || existing.meta.length > 0)

    if (!hasData) {
      const updated = new Map(panelResults)
      updated.set(p.id, { data: [], meta: [], loading: true })
      panelResults = updated
    }

    const rangeValue = dashboardTimeRange || '1h'

    try {
      const res = await apiPost<{ data: any[]; meta: any[]; error?: string }>('/api/dashboards/query', {
        query: p.query,
        time_range: toDashboardTimeRangePayload(rangeValue),
      })
      const next = new Map(panelResults)
      next.set(p.id, { data: res.data ?? [], meta: res.meta ?? [], loading: false })
      panelResults = next
    } catch (e: any) {
      const next = new Map(panelResults)
      const prev = panelResults.get(p.id)
      next.set(p.id, { data: prev?.data ?? [], meta: prev?.meta ?? [], error: e.message, loading: false })
      panelResults = next
    }
  }

  function handleTimeRangeChange(nextRange: string) {
    dashboardTimeRange = nextRange
    localStorage.setItem('ch-ui-dashboard-time-range', nextRange)
    runAllPanelQueries()
  }

  async function saveDashboardTitle() {
    if (!currentDashboard || !titleInput.trim()) return
    try {
      await apiPut(`/api/dashboards/${currentDashboard.id}`, { name: titleInput.trim() })
      currentDashboard = { ...currentDashboard, name: titleInput.trim() }
      editingTitle = false
      toastSuccess('Dashboard renamed')
    } catch (e: any) {
      toastError(e.message)
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
      const maxY = panels.reduce((max, pan) => Math.max(max, pan.layout_y + pan.layout_h), 0)
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
    } catch (e: any) {
      toastError('Failed to duplicate: ' + e.message)
    }
  }

  async function deletePanel(panelId: string) {
    if (!currentDashboard) return
    try {
      await apiDel(`/api/dashboards/${currentDashboard.id}/panels/${panelId}`)
      panels = panels.filter(p => p.id !== panelId)
      toastSuccess('Panel deleted')
    } catch (e: any) {
      toastError(e.message)
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
        {#if editingTitle}
          <input
            type="text"
            class="h-7 border-b border-accent bg-transparent text-[15px] font-semibold tracking-[-0.01em] text-fg outline-none"
            bind:value={titleInput}
            onkeydown={(e) => { if (e.key === 'Enter') saveDashboardTitle(); if (e.key === 'Escape') editingTitle = false }}
            onblur={saveDashboardTitle}
          />
        {:else}
          <h1
            class="cursor-text truncate text-[15px] font-semibold tracking-[-0.01em] text-fg"
            ondblclick={() => { editingTitle = true; titleInput = currentDashboard?.name ?? '' }}
            title="Double-click to rename"
          >
            {currentDashboard?.name ?? 'Dashboard'}
          </h1>
        {/if}
      </nav>

      {#if currentDashboard}
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
          <Button size="sm" variant="outline" onclick={() => settingsOpen = true}>
            <Settings size={14} /> Settings
          </Button>
          <Button size="sm" variant="outline" onclick={() => shareDialogOpen = true}>
            <Share2 size={14} /> Share
          </Button>
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
                {#each REFRESH_OPTIONS as opt}
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
          <Button size="sm" onclick={openAddPanel}>
            <Plus size={14} /> Add Panel
          </Button>
        {/if}
      </div>
    </div>

    <div class="flex-1 min-h-0 {panelEditorOpen ? 'overflow-hidden' : 'overflow-auto p-4'}">
      {#if panelEditorOpen}
        <PanelEditor
          dashboardId={currentDashboard?.id ?? ''}
          dashboardTimeRange={dashboardTimeRange}
          panel={editingPanel}
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
        />
      {/if}
    </div>
  {/if}
</div>

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
    onclose={() => settingsOpen = false}
    onimported={(d, p) => { currentDashboard = d; panels = p; runAllPanelQueries(p); settingsOpen = false }}
    ondelete={() => { settingsOpen = false; requestDeleteDashboard(currentDashboard!.id) }}
  />
{/if}

{#if fullscreenPanel}
  {@const fsResult = panelResults.get(fullscreenPanel.id)}
  {@const fsCfg = parsePanelConfig(fullscreenPanel.config)}
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
        <MarkdownPanel content={fsCfg.content ?? ''} />
      {:else if !fsResult || fsResult.loading}
        <div class="flex items-center justify-center h-full"><Spinner /></div>
      {:else if fsResult.error}
        <p class="p-4 text-[13px] text-danger">{fsResult.error}</p>
      {:else if fullscreenPanel.panel_type === 'stat'}
        <StatPanel stat={computeStat(fsResult.data, fsResult.meta, fsCfg)} />
      {:else if fullscreenPanel.panel_type === 'gauge'}
        <GaugePanel stat={computeStat(fsResult.data, fsResult.meta, fsCfg)} min={fsCfg.gaugeMin} max={fsCfg.gaugeMax} />
      {:else if fullscreenPanel.panel_type === 'pie'}
        <PiePanel data={fsResult.data} meta={fsResult.meta} config={fsCfg} />
      {:else if fullscreenPanel.panel_type === 'timeseries' || fullscreenPanel.panel_type === 'bar'}
        <ChartPanel data={fsResult.data} meta={fsResult.meta} config={fsCfg} />
      {:else}
        {#if fsResult.meta.length > 0}
          <div class="overflow-auto h-full">
            <table class="ds-table">
              <thead>
                <tr class="ds-table-head-row">
                  {#each fsResult.meta as col}
                    <th class="ds-table-th">{col.name}</th>
                  {/each}
                </tr>
              </thead>
              <tbody>
                {#each fsResult.data as row}
                  <tr class="ds-table-row-static">
                    {#each fsResult.meta as col}
                      <td class="ds-td whitespace-nowrap">{row[col.name] ?? '--'}</td>
                    {/each}
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        {:else}
          <p class="p-4 text-[13px] text-fg-3">No data</p>
        {/if}
      {/if}
    </div>
  </div>
{/if}
