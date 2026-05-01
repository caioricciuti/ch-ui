<script lang="ts">
  import type { Dashboard, Panel, PanelConfig } from '../lib/types/api'
  import type { ColumnMeta } from '../lib/utils/chart-transform'
  import { getStatValue } from '../lib/utils/chart-transform'
  import { toDashboardTimeRangePayload } from '../lib/utils/dashboard-time'
  import { withBase } from '../lib/basePath'
  import { safeParse } from '../lib/utils/safe-json'
  import Spinner from '../lib/components/common/Spinner.svelte'
  import ChartPanel from '../lib/components/dashboard/ChartPanel.svelte'
  import TimeRangeSelector from '../lib/components/dashboard/TimeRangeSelector.svelte'
  import {
    COLS, ROW_H, GAP,
    calcColW, gridToPixel, compact, containerHeight,
    type LayoutItem,
  } from '../lib/utils/grid-layout'
  import { LayoutDashboard, RefreshCw, Lock } from 'lucide-svelte'
  import logo from '../assets/logo.png'

  interface Props {
    token: string
  }

  let { token }: Props = $props()

  const inviteKey = new URLSearchParams(window.location.search).get('key') ?? ''

  let dashboard = $state<Dashboard | null>(null)
  let panels = $state<Panel[]>([])
  let panelResults = $state<Map<string, { data: Record<string, unknown>[]; meta: ColumnMeta[]; error?: string; loading: boolean }>>(new Map())
  let loading = $state(true)
  let error = $state<string | null>(null)
  let isPrivateError = $state(false)
  let dashboardTimeRange = $state('1h')

  let gridEl = $state<HTMLDivElement>(undefined!)
  let containerWidth = $state(0)
  const colW = $derived(calcColW(containerWidth))

  const displayLayouts = $derived.by(() => {
    const items: LayoutItem[] = panels.map(p => ({
      id: p.id, x: p.layout_x, y: p.layout_y, w: p.layout_w, h: p.layout_h,
    }))
    return compact(items)
  })

  const totalHeight = $derived(containerHeight(displayLayouts))

  $effect(() => {
    if (!gridEl) return
    const ro = new ResizeObserver(entries => {
      containerWidth = entries[0].contentRect.width
    })
    ro.observe(gridEl)
    return () => ro.disconnect()
  })

  $effect(() => {
    void token
    loadPublicDashboard()
  })

  function appendKey(url: string): string {
    if (!inviteKey) return url
    const sep = url.includes('?') ? '&' : '?'
    return `${url}${sep}key=${encodeURIComponent(inviteKey)}`
  }

  async function apiFetch<T>(url: string, options: RequestInit = {}): Promise<T> {
    const res = await fetch(withBase(appendKey(url)), {
      headers: { 'Content-Type': 'application/json', ...options.headers as Record<string, string> },
      ...options,
    })
    const body = await res.text()
    const parsed = safeParse(body)
    if (!res.ok) {
      if (res.status === 403 && parsed?.visibility === 'private') {
        throw Object.assign(new Error(parsed?.error ?? 'Access denied'), { isPrivate: true })
      }
      throw new Error(parsed?.error ?? `Request failed (${res.status})`)
    }
    return parsed as T
  }

  async function loadPublicDashboard() {
    loading = true
    error = null
    isPrivateError = false
    try {
      const res = await apiFetch<{ dashboard: Dashboard; panels: Panel[] }>(
        `/api/public/dashboards/${token}`,
      )
      dashboard = res.dashboard
      panels = res.panels ?? []
      runAllPanelQueries()
    } catch (e: unknown) {
      if (e && typeof e === 'object' && 'isPrivate' in e) {
        isPrivateError = true
      }
      error = e instanceof Error ? e.message : 'Failed to load dashboard'
    } finally {
      loading = false
    }
  }

  function runAllPanelQueries() {
    for (const p of panels) runPanelQuery(p)
  }

  async function runPanelQuery(p: Panel) {
    const updated = new Map(panelResults)
    updated.set(p.id, { data: [], meta: [], loading: true })
    panelResults = updated

    try {
      const res = await apiFetch<{ data: Record<string, unknown>[]; meta: ColumnMeta[]; error?: string; success?: boolean }>(
        `/api/public/dashboards/${token}/query`,
        {
          method: 'POST',
          body: JSON.stringify({
            query: p.query,
            time_range: toDashboardTimeRangePayload(dashboardTimeRange || '1h'),
          }),
        },
      )
      if (res.success === false) {
        const next = new Map(panelResults)
        next.set(p.id, { data: [], meta: [], error: res.error ?? 'Query failed', loading: false })
        panelResults = next
      } else {
        const next = new Map(panelResults)
        next.set(p.id, { data: res.data ?? [], meta: res.meta ?? [], loading: false })
        panelResults = next
      }
    } catch (e: unknown) {
      const next = new Map(panelResults)
      next.set(p.id, { data: [], meta: [], error: e instanceof Error ? e.message : 'Query failed', loading: false })
      panelResults = next
    }
  }

  function handleTimeRangeChange(nextRange: string) {
    dashboardTimeRange = nextRange
    runAllPanelQueries()
  }

  function parsePanelConfig(configStr: string): PanelConfig {
    try {
      return JSON.parse(configStr || '{}')
    } catch {
      return { chartType: 'table' }
    }
  }
</script>

<div class="flex flex-col h-screen bg-white dark:bg-gray-950 text-gray-900 dark:text-gray-100">
  <!-- Header -->
  <div class="flex items-center justify-between px-4 py-3 border-b border-gray-200 dark:border-gray-800 shrink-0">
    <div class="flex items-center gap-3">
      <img src={logo} alt="CH-UI" class="w-7 h-7 rounded-lg" />
      {#if dashboard}
        <div class="flex items-center gap-2">
          <LayoutDashboard size={16} class="text-ch-blue" />
          <h1 class="text-sm font-semibold">{dashboard.name}</h1>
        </div>
        {#if dashboard.description}
          <span class="text-xs text-gray-500 truncate max-w-[40ch]">{dashboard.description}</span>
        {/if}
      {/if}
    </div>
    {#if dashboard}
      <div class="flex items-center gap-2">
        <TimeRangeSelector value={dashboardTimeRange} onchange={handleTimeRangeChange} />
        <button
          class="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded-md border border-gray-300 dark:border-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-800"
          onclick={() => runAllPanelQueries()}
        >
          <RefreshCw size={12} /> Refresh
        </button>
      </div>
    {/if}
  </div>

  <!-- Content -->
  <div class="flex-1 overflow-auto p-4">
    {#if loading}
      <div class="flex items-center justify-center py-20"><Spinner /></div>
    {:else if error}
      <div class="flex flex-col items-center justify-center py-20 gap-3">
        {#if isPrivateError}
          <Lock size={36} class="text-gray-400 dark:text-gray-600" />
          <p class="text-sm font-medium text-gray-700 dark:text-gray-300">This dashboard is private</p>
          <p class="text-xs text-gray-500 max-w-sm text-center">Access requires an invite. Check your email for a magic link from the dashboard owner.</p>
        {:else}
          <LayoutDashboard size={36} class="text-gray-300 dark:text-gray-700" />
          <p class="text-sm text-red-500">{error}</p>
        {/if}
      </div>
    {:else if panels.length === 0}
      <div class="flex flex-col items-center justify-center py-20 gap-2 text-gray-500">
        <LayoutDashboard size={36} class="text-gray-300 dark:text-gray-700" />
        <p class="text-sm">This dashboard has no panels</p>
      </div>
    {:else}
      <div bind:this={gridEl} class="relative w-full" style="min-height: {totalHeight}px;">
        {#each panels as panel (panel.id)}
          {@const layout = displayLayouts.find(l => l.id === panel.id)}
          {@const pos = layout && colW > 0 ? gridToPixel(layout, colW) : null}
          {@const result = panelResults.get(panel.id)}
          {@const cfg = parsePanelConfig(panel.config)}
          {#if pos}
            <div
              class="absolute flex flex-col bg-gray-50 dark:bg-gray-900 border border-gray-200 dark:border-gray-800 rounded-lg overflow-hidden"
              style="left:{pos.left}px; top:{pos.top}px; width:{pos.width}px; height:{pos.height}px;
                transition: left 0.15s ease, top 0.15s ease, width 0.15s ease, height 0.15s ease;"
            >
              <div class="flex items-center px-3 py-2 border-b border-gray-200 dark:border-gray-800 bg-gray-100/50 dark:bg-gray-800/50 shrink-0">
                <span class="text-xs font-medium text-gray-700 dark:text-gray-300 truncate">{panel.name}</span>
              </div>
              <div class="flex-1 overflow-hidden p-2">
                {#if !result || result.loading}
                  <div class="flex items-center justify-center h-full"><Spinner size="sm" /></div>
                {:else if result.error}
                  <p class="text-xs text-red-500 p-2">{result.error}</p>
                {:else if panel.panel_type === 'stat'}
                  <div class="flex items-center justify-center h-full">
                    <span class="text-3xl font-bold">{getStatValue(result.data, result.meta)}</span>
                  </div>
                {:else if panel.panel_type === 'timeseries' || panel.panel_type === 'bar'}
                  <ChartPanel data={result.data} meta={result.meta} config={cfg} />
                {:else}
                  {#if result.meta.length > 0}
                    <div class="overflow-auto h-full">
                      <table class="w-full text-xs">
                        <thead>
                          <tr class="border-b border-gray-200 dark:border-gray-800">
                            {#each result.meta as col}
                              <th class="text-left py-1 px-2 text-gray-500 font-medium whitespace-nowrap">{col.name}</th>
                            {/each}
                          </tr>
                        </thead>
                        <tbody>
                          {#each result.data.slice(0, 100) as row}
                            <tr class="border-b border-gray-100 dark:border-gray-900">
                              {#each result.meta as col}
                                <td class="py-1 px-2 text-gray-700 dark:text-gray-300 whitespace-nowrap">{row[col.name] ?? '--'}</td>
                              {/each}
                            </tr>
                          {/each}
                        </tbody>
                      </table>
                    </div>
                  {:else}
                    <p class="text-xs text-gray-500 p-2">No data</p>
                  {/if}
                {/if}
              </div>
            </div>
          {/if}
        {/each}
      </div>
    {/if}
  </div>
</div>
