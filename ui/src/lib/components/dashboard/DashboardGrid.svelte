<script lang="ts">
  import type { Panel, PanelConfig } from '../../types/api'
  import { apiPut } from '../../api/client'
  import { error as toastError } from '../../stores/toast.svelte'
  import Spinner from '../common/Spinner.svelte'
  import ChartPanel from './ChartPanel.svelte'
  import { getStatValue } from '../../utils/chart-transform'
  import {
    COLS, ROW_H, GAP, MIN_W, MIN_H,
    calcColW, gridToPixel, compact, containerHeight,
    type LayoutItem,
  } from '../../utils/grid-layout'
  import { Pencil, Trash2, GripVertical } from 'lucide-svelte'

  interface Props {
    dashboardId: string
    panels: Panel[]
    panelResults: Map<string, { data: any[]; meta: any[]; error?: string; loading: boolean }>
    onpanelschange: (panels: Panel[]) => void
    oneditpanel: (panel: Panel) => void
    ondeletepanel: (panelId: string) => void
  }

  let { dashboardId, panels, panelResults, onpanelschange, oneditpanel, ondeletepanel }: Props = $props()

  let gridEl = $state<HTMLDivElement>(undefined!)
  let containerWidth = $state(0)
  const colW = $derived(calcColW(containerWidth))

  // Drag/resize state
  type Mode = 'idle' | 'dragging' | 'resizing'
  let mode = $state<Mode>('idle')
  let activeId = $state<string | null>(null)
  let startPointer = $state({ x: 0, y: 0 })
  let startLayout = $state({ x: 0, y: 0, w: 0, h: 0 })
  let ghostLayout = $state<{ x: number; y: number; w: number; h: number } | null>(null)
  // Store the scroll position at drag start so we account for scrolling during drag
  let startScrollTop = $state(0)

  function panelToLayout(p: Panel): LayoutItem {
    return { id: p.id, x: p.layout_x, y: p.layout_y, w: p.layout_w, h: p.layout_h }
  }

  // Compacted display layout
  const displayLayouts = $derived.by(() => {
    const items = panels.map(p => {
      if (p.id === activeId && ghostLayout) {
        return { id: p.id, ...ghostLayout }
      }
      return panelToLayout(p)
    })
    return compact(items, activeId ?? undefined)
  })

  const totalHeight = $derived(containerHeight(displayLayouts))

  // Measure container width
  $effect(() => {
    if (!gridEl) return
    const ro = new ResizeObserver(entries => {
      containerWidth = entries[0].contentRect.width
    })
    ro.observe(gridEl)
    return () => ro.disconnect()
  })

  function parsePanelConfig(configStr: string): PanelConfig {
    try {
      return JSON.parse(configStr || '{}')
    } catch {
      return { chartType: 'table' }
    }
  }

  // --- Drag handlers ---

  function handleDragStart(panelId: string, e: PointerEvent) {
    if (e.button !== 0) return
    const panel = panels.find(p => p.id === panelId)
    if (!panel) return
    e.preventDefault()
    mode = 'dragging'
    activeId = panelId
    startPointer = { x: e.clientX, y: e.clientY }
    startLayout = { x: panel.layout_x, y: panel.layout_y, w: panel.layout_w, h: panel.layout_h }
    ghostLayout = { ...startLayout }
    startScrollTop = gridEl.parentElement?.scrollTop ?? 0
    gridEl.setPointerCapture(e.pointerId)
  }

  function handleResizeStart(panelId: string, e: PointerEvent) {
    if (e.button !== 0) return
    const panel = panels.find(p => p.id === panelId)
    if (!panel) return
    e.preventDefault()
    e.stopPropagation()
    mode = 'resizing'
    activeId = panelId
    startPointer = { x: e.clientX, y: e.clientY }
    startLayout = { x: panel.layout_x, y: panel.layout_y, w: panel.layout_w, h: panel.layout_h }
    ghostLayout = { ...startLayout }
    startScrollTop = gridEl.parentElement?.scrollTop ?? 0
    gridEl.setPointerCapture(e.pointerId)
  }

  function handlePointerMove(e: PointerEvent) {
    if (mode === 'idle' || !ghostLayout) return

    const dx = e.clientX - startPointer.x
    const dy = e.clientY - startPointer.y + ((gridEl.parentElement?.scrollTop ?? 0) - startScrollTop)
    const cellW = colW + GAP
    const cellH = ROW_H + GAP

    if (mode === 'dragging') {
      const gridDx = Math.round(dx / cellW)
      const gridDy = Math.round(dy / cellH)
      ghostLayout = {
        x: Math.max(0, Math.min(COLS - startLayout.w, startLayout.x + gridDx)),
        y: Math.max(0, startLayout.y + gridDy),
        w: startLayout.w,
        h: startLayout.h,
      }
    } else if (mode === 'resizing') {
      const gridDw = Math.round(dx / cellW)
      const gridDh = Math.round(dy / cellH)
      ghostLayout = {
        x: startLayout.x,
        y: startLayout.y,
        w: Math.max(MIN_W, Math.min(COLS - startLayout.x, startLayout.w + gridDw)),
        h: Math.max(MIN_H, startLayout.h + gridDh),
      }
    }

    // Auto-scroll when near edges
    const scrollParent = gridEl.parentElement
    if (scrollParent) {
      const rect = scrollParent.getBoundingClientRect()
      const edge = 60
      if (e.clientY > rect.bottom - edge) {
        scrollParent.scrollTop += 8
      } else if (e.clientY < rect.top + edge) {
        scrollParent.scrollTop -= 8
      }
    }
  }

  function handlePointerUp(e: PointerEvent) {
    if (mode === 'idle' || !activeId) return
    gridEl.releasePointerCapture(e.pointerId)

    // Apply final compacted layout
    const finalLayouts = displayLayouts
    const originalPanels = panels
    const updatedPanels = panels.map(p => {
      const layout = finalLayouts.find(l => l.id === p.id)
      if (!layout) return p
      if (p.layout_x !== layout.x || p.layout_y !== layout.y ||
          p.layout_w !== layout.w || p.layout_h !== layout.h) {
        return { ...p, layout_x: layout.x, layout_y: layout.y, layout_w: layout.w, layout_h: layout.h }
      }
      return p
    })

    onpanelschange(updatedPanels)
    persistLayouts(updatedPanels, originalPanels)

    mode = 'idle'
    activeId = null
    ghostLayout = null
  }

  async function persistLayouts(updated: Panel[], original: Panel[]) {
    const changed = updated.filter(p => {
      const orig = original.find(o => o.id === p.id)
      if (!orig) return false
      return orig.layout_x !== p.layout_x || orig.layout_y !== p.layout_y ||
             orig.layout_w !== p.layout_w || orig.layout_h !== p.layout_h
    })
    if (changed.length === 0) return

    try {
      await Promise.all(changed.map(p =>
        apiPut(`/api/dashboards/${dashboardId}/panels/${p.id}`, {
          layout_x: p.layout_x,
          layout_y: p.layout_y,
          layout_w: p.layout_w,
          layout_h: p.layout_h,
        })
      ))
    } catch (e: any) {
      toastError('Failed to save layout: ' + e.message)
    }
  }
</script>

{#if panels.length === 0}
  <div class="text-center py-12 text-gray-500">
    <p class="mb-1">No panels yet</p>
    <p class="text-xs text-gray-400 dark:text-gray-600">Add a panel with a SQL query to visualize data</p>
  </div>
{:else}
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    bind:this={gridEl}
    class="relative w-full"
    style="min-height: {totalHeight}px;"
    onpointermove={handlePointerMove}
    onpointerup={handlePointerUp}
  >
    {#each panels as panel (panel.id)}
      {@const layout = displayLayouts.find(l => l.id === panel.id)}
      {@const pos = layout && colW > 0 ? gridToPixel(layout, colW) : null}
      {@const result = panelResults.get(panel.id)}
      {@const cfg = parsePanelConfig(panel.config)}
      {@const isActive = (mode === 'dragging' || mode === 'resizing') && activeId === panel.id}
      {#if pos}
        <div
          class="absolute flex flex-col bg-gray-50 dark:bg-gray-900 border rounded-lg overflow-hidden group
            {isActive ? 'border-ch-blue ring-2 ring-ch-blue/30 z-20' : 'border-gray-200 dark:border-gray-800'}"
          style="left:{pos.left}px; top:{pos.top}px; width:{pos.width}px; height:{pos.height}px;
            {isActive ? '' : 'transition: left 0.15s ease, top 0.15s ease, width 0.15s ease, height 0.15s ease;'}"
        >
          <!-- Panel header — drag handle -->
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div
            class="flex items-center justify-between px-3 py-2 border-b border-gray-200 dark:border-gray-800 bg-gray-100/50 dark:bg-gray-800/50 cursor-grab active:cursor-grabbing select-none shrink-0"
            onpointerdown={(e) => handleDragStart(panel.id, e)}
          >
            <div class="flex items-center gap-2 min-w-0">
              <GripVertical size={12} class="text-gray-400 shrink-0 opacity-0 group-hover:opacity-100 transition-opacity" />
              <span class="text-xs font-medium text-gray-700 dark:text-gray-300 truncate">{panel.name}</span>
              <span class="text-[10px] px-1.5 py-0.5 rounded bg-gray-200 dark:bg-gray-700 text-gray-500">{panel.panel_type}</span>
            </div>
            <div class="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
              <button
                class="p-1 rounded text-gray-400 hover:text-ch-blue hover:bg-gray-200 dark:hover:bg-gray-700"
                onpointerdown={(e) => e.stopPropagation()}
                onclick={() => oneditpanel(panel)}
                title="Edit"
              >
                <Pencil size={12} />
              </button>
              <button
                class="p-1 rounded text-gray-400 hover:text-red-400 hover:bg-gray-200 dark:hover:bg-gray-700"
                onpointerdown={(e) => e.stopPropagation()}
                onclick={() => ondeletepanel(panel.id)}
                title="Delete"
              >
                <Trash2 size={12} />
              </button>
            </div>
          </div>

          <!-- Panel content -->
          <div class="flex-1 overflow-hidden p-2">
            {#if !result || result.loading}
              <div class="flex items-center justify-center h-full"><Spinner size="sm" /></div>
            {:else if result.error}
              <p class="text-xs text-red-500 p-2">{result.error}</p>
            {:else if panel.panel_type === 'stat'}
              <div class="flex items-center justify-center h-full">
                <span class="text-3xl font-bold text-gray-900 dark:text-gray-100">{getStatValue(result.data, result.meta)}</span>
              </div>
            {:else if panel.panel_type === 'timeseries' || panel.panel_type === 'bar'}
              <ChartPanel data={result.data} meta={result.meta} config={cfg} />
            {:else}
              <!-- Table -->
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

          <!-- Resize handle -->
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div
            class="absolute bottom-0 right-0 w-5 h-5 cursor-se-resize opacity-0 group-hover:opacity-100 transition-opacity flex items-end justify-end p-0.5"
            onpointerdown={(e) => handleResizeStart(panel.id, e)}
          >
            <svg viewBox="0 0 6 6" class="w-3 h-3 text-gray-400">
              <circle cx="5" cy="1" r="0.7" fill="currentColor" />
              <circle cx="5" cy="5" r="0.7" fill="currentColor" />
              <circle cx="1" cy="5" r="0.7" fill="currentColor" />
            </svg>
          </div>
        </div>
      {/if}
    {/each}

    <!-- Ghost placeholder during drag/resize -->
    {#if ghostLayout && mode !== 'idle' && colW > 0}
      {@const ghostPos = gridToPixel(ghostLayout, colW)}
      <div
        class="absolute rounded-lg border-2 border-dashed border-ch-blue/60 bg-ch-blue/12 pointer-events-none z-10"
        style="left:{ghostPos.left}px; top:{ghostPos.top}px; width:{ghostPos.width}px; height:{ghostPos.height}px;
          transition: left 0.1s ease, top 0.1s ease, width 0.1s ease, height 0.1s ease;"
      ></div>
    {/if}
  </div>
{/if}
