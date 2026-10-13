<script lang="ts">
  import { onDestroy } from 'svelte'
  import type { Dashboard, DashboardVariable, Panel, PanelConfig } from '../lib/types/api'
  import type { ColumnMeta } from '../lib/utils/chart-transform'
  import { computeStat } from '../lib/utils/chart-transform'
  import { toDashboardTimeRangePayload, dashboardRangeFromUrlParams, dashboardRangeToUrlParams } from '../lib/utils/dashboard-time'
  import { setDashboardThemeOverride } from '../lib/components/dashboard/dashboard-theme.svelte'
  import PanelTable from '../lib/components/dashboard/PanelTable.svelte'
  import { withBase } from '../lib/basePath'
  import { safeParse } from '../lib/utils/safe-json'
  import Spinner from '../lib/components/common/Spinner.svelte'
  import ChartPanel from '../lib/components/dashboard/ChartPanel.svelte'
  import StatPanel from '../lib/components/dashboard/StatPanel.svelte'
  import MarkdownPanel from '../lib/components/dashboard/MarkdownPanel.svelte'
  import GaugePanel from '../lib/components/dashboard/GaugePanel.svelte'
  import PiePanel from '../lib/components/dashboard/PiePanel.svelte'
  import TimeRangeSelector from '../lib/components/dashboard/TimeRangeSelector.svelte'
  import VariableBar from '../lib/components/dashboard/VariableBar.svelte'
  import { fetchPublicVariableOptions } from '../lib/api/dashboards'
  import {
    initialVariableValues, readVariablesFromSearch, reconcileValues, writeVariablesToUrl,
    type VariableValues,
  } from '../lib/utils/dashboard-variables'
  import {
    STACK_BELOW, calcColW, gridToPixel, compact, containerHeight, stackLayout,
    type LayoutItem,
  } from '../lib/utils/grid-layout'
  import { LayoutDashboard, RefreshCw, Lock, Timer } from 'lucide-svelte'
  import logo from '../assets/logo.png'

  interface Props {
    token: string
  }

  let { token }: Props = $props()

  const urlParams = new URLSearchParams(window.location.search)
  const inviteKey = urlParams.get('key') ?? ''

  // Link options for wall screens: ?from=&to= (time range), ?theme=dark|light
  // and ?refresh=30s (auto refresh: s, m or h, at least 5 s).
  const themeParam = urlParams.get('theme')
  if (themeParam === 'dark' || themeParam === 'light') setDashboardThemeOverride(themeParam)
  onDestroy(() => {
    if (themeParam === 'dark' || themeParam === 'light') setDashboardThemeOverride(null)
  })

  function parseRefresh(v: string | null): number {
    const m = /^(\d+)\s*(s|m|h)?$/i.exec((v ?? '').trim())
    if (!m) return 0
    const n = Number(m[1]) * ({ s: 1, m: 60, h: 3600 }[(m[2] ?? 's').toLowerCase() as 's' | 'm' | 'h'])
    return n > 0 ? Math.max(5, n) : 0
  }
  const refreshSeconds = parseRefresh(urlParams.get('refresh'))

  let dashboard = $state<Dashboard | null>(null)
  let panels = $state<Panel[]>([])
  let panelResults = $state<Map<string, { data: Record<string, unknown>[]; meta: ColumnMeta[]; error?: string; loading: boolean }>>(new Map())
  let loading = $state(true)
  let error = $state<string | null>(null)
  let isPrivateError = $state(false)
  let dashboardTimeRange = $state(dashboardRangeFromUrlParams(urlParams.get('from'), urlParams.get('to')) ?? '1h')
  let variables = $state<DashboardVariable[]>([])
  let variableValues = $state<VariableValues>({})

  let gridEl = $state<HTMLDivElement>(undefined!)
  let containerWidth = $state(0)
  const colW = $derived(calcColW(containerWidth))
  const stacked = $derived(containerWidth > 0 && containerWidth < STACK_BELOW)

  const displayLayouts = $derived.by(() => {
    const items: LayoutItem[] = panels.map(p => ({
      id: p.id, x: p.layout_x, y: p.layout_y, w: p.layout_w, h: p.layout_h,
    }))
    return stacked ? stackLayout(items) : compact(items)
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
      const res = await apiFetch<{ dashboard: Dashboard; panels: Panel[]; variables?: DashboardVariable[] }>(
        `/api/public/dashboards/${token}`,
      )
      dashboard = res.dashboard
      panels = res.panels ?? []
      variables = res.variables ?? []
      // A text variable on a share link is always its default.
      const fromUrl = readVariablesFromSearch(urlParams)
      for (const v of variables) if (v.type === 'text') delete fromUrl[v.name]
      variableValues = initialVariableValues(variables, fromUrl)
      writeVariablesToUrl(variables, variableValues)
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

  // Auto refresh from ?refresh=. A tick is skipped while the previous
  // refresh is still running, so a slow ClickHouse never piles up queries.
  let panelsInFlight = 0
  $effect(() => {
    if (!refreshSeconds || !dashboard) return
    const timer = setInterval(() => {
      if (panelsInFlight === 0) runAllPanelQueries()
    }, refreshSeconds * 1000)
    return () => clearInterval(timer)
  })

  // Per panel request sequence: only the latest request may write a result.
  const panelSeq = new Map<string, number>()

  async function runPanelQuery(p: Panel) {
    if (p.panel_type === 'text') return

    const seq = (panelSeq.get(p.id) ?? 0) + 1
    panelSeq.set(p.id, seq)
    const isCurrent = () => panelSeq.get(p.id) === seq

    // Keep the last result on screen while it refreshes.
    const prev = panelResults.get(p.id)
    const updated = new Map(panelResults)
    updated.set(p.id, { data: prev?.data ?? [], meta: prev?.meta ?? [], loading: true })
    panelResults = updated

    panelsInFlight++
    try {
      const res = await apiFetch<{ data: Record<string, unknown>[]; meta: ColumnMeta[]; error?: string; success?: boolean }>(
        `/api/public/dashboards/${token}/query`,
        {
          method: 'POST',
          body: JSON.stringify({
            panel_id: p.id,
            time_range: toDashboardTimeRangePayload(dashboardTimeRange || '1h'),
            variables: variableValues,
          }),
        },
      )
      if (!isCurrent()) return
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
      if (!isCurrent()) return
      const next = new Map(panelResults)
      next.set(p.id, { data: [], meta: [], error: e instanceof Error ? e.message : 'Query failed', loading: false })
      panelResults = next
    } finally {
      panelsInFlight--
    }
  }

  function handleTimeRangeChange(nextRange: string) {
    dashboardTimeRange = nextRange
    // Keep the link shareable: the range goes in the address bar.
    const url = new URL(window.location.href)
    const { from, to } = dashboardRangeToUrlParams(nextRange)
    url.searchParams.set('from', from)
    url.searchParams.set('to', to)
    history.replaceState(history.state, '', url)
    runAllPanelQueries()
  }

  function handleVariableValuesChange(values: VariableValues) {
    variableValues = reconcileValues(variables, values)
    writeVariablesToUrl(variables, variableValues)
    runAllPanelQueries()
  }

  function loadVariableOptions(name: string, values: VariableValues) {
    return fetchPublicVariableOptions(token, name, {
      variables: values,
      time_range: toDashboardTimeRangePayload(dashboardTimeRange || '1h'),
    }, inviteKey)
  }

  function parsePanelConfig(configStr: string): PanelConfig {
    try {
      return JSON.parse(configStr || '{}')
    } catch {
      return { chartType: 'table' }
    }
  }
</script>

<div class="flex flex-col h-screen bg-canvas text-fg">
  <!-- Header -->
  <div class="flex flex-wrap items-center justify-between gap-2 px-4 py-3 border-b border-edge-subtle shrink-0">
    <div class="flex items-center gap-3">
      <img src={logo} alt="CH-UI" class="w-7 h-7 rounded-lg" />
      {#if dashboard}
        <div class="flex items-center gap-2">
          <LayoutDashboard size={16} class="text-accent" />
          <h1 class="text-sm font-semibold">{dashboard.name}</h1>
        </div>
        {#if dashboard.description}
          <span class="text-xs text-fg-3 truncate max-w-[40ch]">{dashboard.description}</span>
        {/if}
      {/if}
    </div>
    {#if dashboard}
      <div class="flex items-center gap-2">
        {#if refreshSeconds}
          <span class="inline-flex items-center gap-1 text-xs text-fg-3" title="Auto refresh">
            <Timer size={12} class="text-accent" /> {refreshSeconds}s
          </span>
        {/if}
        <TimeRangeSelector value={dashboardTimeRange} onchange={handleTimeRangeChange} />
        <button
          class="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded-md border border-edge text-fg-2 hover:bg-hover"
          onclick={() => runAllPanelQueries()}
        >
          <RefreshCw size={12} /> Refresh
        </button>
      </div>
    {/if}
  </div>

  {#if dashboard && variables.length > 0}
    <VariableBar
      {variables}
      values={variableValues}
      loadOptions={loadVariableOptions}
      onchange={handleVariableValuesChange}
      lockText
    />
  {/if}

  <!-- Content -->
  <div class="flex-1 overflow-auto p-4">
    {#if loading}
      <div class="flex items-center justify-center py-20"><Spinner /></div>
    {:else if error}
      <div class="flex flex-col items-center justify-center py-20 gap-3">
        {#if isPrivateError}
          <Lock size={36} class="text-fg-4" />
          <p class="text-sm font-medium text-fg-2">This dashboard is private</p>
          <p class="text-xs text-fg-3 max-w-sm text-center">Access requires an invite. Check your email for a magic link from the dashboard owner.</p>
        {:else}
          <LayoutDashboard size={36} class="text-fg-4" />
          <p class="text-[13px] text-danger">{error}</p>
        {/if}
      </div>
    {:else if panels.length === 0}
      <div class="flex flex-col items-center justify-center py-20 gap-2 text-fg-3">
        <LayoutDashboard size={36} class="text-fg-4" />
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
              class="absolute flex flex-col bg-surface border border-edge-subtle rounded-lg overflow-hidden"
              style="left:{pos.left}px; top:{pos.top}px; width:{pos.width}px; height:{pos.height}px;
                transition: left 0.15s ease, top 0.15s ease, width 0.15s ease, height 0.15s ease;"
            >
              <div class="flex items-center px-3 py-2 border-b border-edge-subtle bg-surface-2 shrink-0">
                <span class="text-xs font-medium text-fg-2 truncate">{panel.name}</span>
              </div>
              <div class="flex-1 min-h-0 min-w-0 overflow-hidden {panel.panel_type === 'text' ? '' : 'p-2'}">
                {#if panel.panel_type === 'text'}
                  <MarkdownPanel content={cfg.content ?? ''} />
                {:else if !result || (result.loading && result.data.length === 0 && result.meta.length === 0)}
                  <div class="flex items-center justify-center h-full"><Spinner size="sm" /></div>
                {:else if result.error}
                  <p class="text-xs text-danger p-2">{result.error}</p>
                {:else if panel.panel_type === 'stat'}
                  <StatPanel stat={computeStat(result.data, result.meta, cfg)} />
                {:else if panel.panel_type === 'gauge'}
                  <GaugePanel stat={computeStat(result.data, result.meta, cfg)} min={cfg.gaugeMin} max={cfg.gaugeMax} />
                {:else if panel.panel_type === 'pie'}
                  <PiePanel data={result.data} meta={result.meta} config={cfg} />
                {:else if panel.panel_type === 'timeseries' || panel.panel_type === 'bar'}
                  <ChartPanel data={result.data} meta={result.meta} config={cfg} />
                {:else}
                  <PanelTable data={result.data} meta={result.meta} />
                {/if}
              </div>
            </div>
          {/if}
        {/each}
      </div>
    {/if}
  </div>
</div>
