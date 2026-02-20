<script lang="ts">
  import type { Dashboard, Panel } from '../lib/types/api'
  import { apiGet, apiPost, apiPut, apiDel } from '../lib/api/client'
  import { success as toastSuccess, error as toastError } from '../lib/stores/toast.svelte'
  import { openDashboardTab, openSingletonTab } from '../lib/stores/tabs.svelte'
  import { toDashboardTimeRangePayload } from '../lib/utils/dashboard-time'
  import Button from '../lib/components/common/Button.svelte'
  import Spinner from '../lib/components/common/Spinner.svelte'
  import Sheet from '../lib/components/common/Sheet.svelte'
  import ConfirmDialog from '../lib/components/common/ConfirmDialog.svelte'
  import PanelEditor from '../lib/components/dashboard/PanelEditor.svelte'
  import DashboardGrid from '../lib/components/dashboard/DashboardGrid.svelte'
  import TimeRangeSelector from '../lib/components/dashboard/TimeRangeSelector.svelte'
  import { LayoutDashboard, Plus, Trash2, ArrowLeft, RefreshCw } from 'lucide-svelte'

  interface Props {
    dashboardId?: string
  }

  let { dashboardId }: Props = $props()

  // List view
  let dashboards = $state<Dashboard[]>([])
  let listLoading = $state(true)

  // Detail view
  let currentDashboard = $state<Dashboard | null>(null)
  let panels = $state<Panel[]>([])
  let panelResults = $state<Map<string, { data: any[]; meta: any[]; error?: string; loading: boolean }>>(new Map())
  let detailLoading = $state(false)
  let detailError = $state<string | null>(null)
  let loadedDashboardId = $state<string | null>(null)
  let dashboardTimeRange = $state(localStorage.getItem('ch-ui-dashboard-time-range') ?? '1h')

  // Create dashboard sheet
  let showCreateModal = $state(false)
  let createName = $state('')
  let createDesc = $state('')
  let creating = $state(false)

  // Panel editor page
  let panelEditorOpen = $state(false)
  let editingPanel = $state<Panel | null>(null)
  let confirmOpen = $state(false)
  let confirmLoading = $state(false)
  let confirmTitle = $state('')
  let confirmDescription = $state('')
  let confirmTargetDashboardId = $state<string | null>(null)
  let confirmTargetPanelId = $state<string | null>(null)

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
      void loadDashboards()
      return
    }
    if (loadedDashboardId === id) return
    loadedDashboardId = id
    void loadDashboardDetail(id)
  })

  async function loadDashboards() {
    listLoading = true
    try {
      const res = await apiGet<{ dashboards: Dashboard[] }>('/api/dashboards')
      dashboards = res.dashboards ?? []
    } catch (e: any) {
      toastError(e.message)
    } finally {
      listLoading = false
    }
  }

  async function createDashboard() {
    if (!createName.trim()) {
      toastError('Name is required')
      return
    }
    creating = true
    try {
      const res = await apiPost<{ dashboard: Dashboard }>('/api/dashboards', {
        name: createName.trim(),
        description: createDesc.trim(),
      })
      showCreateModal = false
      createName = ''
      createDesc = ''
      await loadDashboards()
      if (res.dashboard) {
        openDashboardTab(res.dashboard.id, res.dashboard.name)
      }
    } catch (e: any) {
      toastError(e.message)
    } finally {
      creating = false
    }
  }

  async function deleteDashboard(id: string) {
    try {
      await apiDel(`/api/dashboards/${id}`)
      dashboards = dashboards.filter(d => d.id !== id)
      toastSuccess('Dashboard deleted')
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

  function openDashboardFromList(d: Dashboard) {
    openDashboardTab(d.id, d.name)
  }

  function openDashboardListTab() {
    openSingletonTab('dashboards', 'Dashboards')
  }

  function runAllPanelQueries(panelsToRun = panels) {
    for (const p of panelsToRun) runPanelQuery(p)
  }

  async function runPanelQuery(p: Panel) {
    const updated = new Map(panelResults)
    updated.set(p.id, { data: [], meta: [], loading: true })
    panelResults = updated

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
      next.set(p.id, { data: [], meta: [], error: e.message, loading: false })
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

  function formatTime(ts: string): string {
    try {
      return new Date(ts).toLocaleString()
    } catch {
      return ts
    }
  }
</script>

<div class="flex flex-col h-full">
  {#if !dashboardId}
    <div class="flex items-center justify-between px-4 py-3 border-b border-gray-200 dark:border-gray-800">
      <div class="flex items-center gap-3">
        <LayoutDashboard size={18} class="text-ch-blue" />
        <h1 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Dashboards</h1>
      </div>
      <Button size="sm" onclick={() => { showCreateModal = true }}>
        <Plus size={14} /> Create Dashboard
      </Button>
    </div>

    <div class="flex-1 overflow-auto p-4">
      {#if listLoading}
        <div class="flex items-center justify-center py-12"><Spinner /></div>
      {:else if dashboards.length === 0}
        <div class="text-center py-12 text-gray-500">
          <LayoutDashboard size={36} class="mx-auto mb-2 text-gray-300 dark:text-gray-700" />
          <p class="mb-1">No dashboards yet</p>
          <p class="text-xs text-gray-400 dark:text-gray-600">Create a dashboard to visualize your ClickHouse data</p>
        </div>
      {:else}
        <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {#each dashboards as dashboard (dashboard.id)}
            <div
              class="bg-gray-50 dark:bg-gray-900 border border-gray-200 dark:border-gray-800 rounded-lg p-4 hover:border-gray-300 dark:hover:border-gray-700 transition-colors cursor-pointer group"
              onclick={() => openDashboardFromList(dashboard)}
              role="button"
              tabindex="0"
              onkeydown={(e) => { if (e.key === 'Enter') openDashboardFromList(dashboard) }}
            >
              <div class="flex items-start justify-between">
                <div class="flex-1 min-w-0">
                  <h3 class="text-sm font-medium text-gray-800 dark:text-gray-200 truncate">{dashboard.name}</h3>
                  {#if dashboard.description}
                    <p class="text-xs text-gray-500 mt-1 truncate">{dashboard.description}</p>
                  {/if}
                </div>
                <button
                  class="p-1.5 rounded text-gray-400 hover:text-red-400 hover:bg-gray-200 dark:hover:bg-gray-800 opacity-0 group-hover:opacity-100 transition-opacity"
                  onclick={(e) => { e.stopPropagation(); requestDeleteDashboard(dashboard.id) }}
                  title="Delete"
                >
                  <Trash2 size={14} />
                </button>
              </div>
              <div class="flex items-center gap-3 mt-3 text-xs text-gray-400">
                <span>by {dashboard.created_by}</span>
                <span>{formatTime(dashboard.updated_at)}</span>
              </div>
            </div>
          {/each}
        </div>
      {/if}
    </div>
  {:else}
    <div class="flex items-center gap-3 px-4 py-3 border-b border-gray-200 dark:border-gray-800">
      <button
        class="p-1.5 rounded text-gray-500 hover:text-gray-700 dark:hover:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-800"
        onclick={openDashboardListTab}
        title="Back to dashboard list"
      >
        <ArrowLeft size={16} />
      </button>

      {#if editingTitle}
        <input
          type="text"
          class="text-lg font-semibold bg-transparent border-b border-ch-blue text-gray-900 dark:text-gray-100 outline-none"
          bind:value={titleInput}
          onkeydown={(e) => { if (e.key === 'Enter') saveDashboardTitle(); if (e.key === 'Escape') editingTitle = false }}
          onblur={saveDashboardTitle}
        />
      {:else}
        <h1
          class="text-lg font-semibold text-gray-900 dark:text-gray-100 cursor-pointer hover:text-ch-blue"
          ondblclick={() => { editingTitle = true; titleInput = currentDashboard?.name ?? '' }}
          title="Double-click to rename"
        >
          {currentDashboard?.name ?? 'Dashboard'}
        </h1>
      {/if}

      {#if currentDashboard?.description}
        <span class="text-xs text-gray-500 truncate max-w-[32ch]">{currentDashboard.description}</span>
      {/if}

      <div class="ml-auto flex items-center gap-2">
        <TimeRangeSelector value={dashboardTimeRange} onchange={handleTimeRangeChange} />
        {#if panelEditorOpen}
          <span class="text-xs text-gray-500 dark:text-gray-400">
            Panel builder mode
          </span>
        {:else}
          <Button size="sm" variant="secondary" onclick={() => runAllPanelQueries()}>
            <RefreshCw size={14} /> Refresh
          </Button>
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
        <div class="text-sm text-red-500 bg-red-100/20 dark:bg-red-900/20 border border-red-300/50 dark:border-red-800/50 rounded-lg p-3">{detailError}</div>
      {:else if currentDashboard}
        <DashboardGrid
          dashboardId={currentDashboard.id}
          {panels}
          {panelResults}
          onpanelschange={(updated) => { panels = updated }}
          oneditpanel={openEditPanel}
          ondeletepanel={requestDeletePanel}
        />
      {/if}
    </div>
  {/if}
</div>

<Sheet open={showCreateModal} title="Create Dashboard" size="sm" onclose={() => showCreateModal = false}>
  <div class="flex flex-col gap-3">
    <div>
      <label for="dashboard-create-name" class="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">Name</label>
      <input
        id="dashboard-create-name"
        type="text"
        class="w-full text-sm bg-transparent border border-gray-300 dark:border-gray-700 rounded px-3 py-2 text-gray-800 dark:text-gray-200"
        placeholder="My Dashboard"
        bind:value={createName}
      />
    </div>
    <div>
      <label for="dashboard-create-description" class="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">Description</label>
      <input
        id="dashboard-create-description"
        type="text"
        class="w-full text-sm bg-transparent border border-gray-300 dark:border-gray-700 rounded px-3 py-2 text-gray-800 dark:text-gray-200"
        placeholder="Optional description"
        bind:value={createDesc}
      />
    </div>
    <div class="flex justify-end gap-2 pt-2">
      <Button variant="secondary" size="sm" onclick={() => showCreateModal = false}>Cancel</Button>
      <Button size="sm" loading={creating} onclick={createDashboard}>Create</Button>
    </div>
  </div>
</Sheet>

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
