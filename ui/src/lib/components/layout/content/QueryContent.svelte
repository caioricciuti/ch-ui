<script lang="ts">
  import type { QueryTab } from '../../../stores/tabs.svelte'
  import { updateTabSQL, getTabResult, setTabResult, markQueryTabSaved } from '../../../stores/tabs.svelte'
  import { formatSQL, explainQuery, fetchQueryPlan, runSampleQuery, fetchQueryProfile } from '../../../api/query'
  import type { QueryPlanNode } from '../../../types/query'
  import type { SavedQuery } from '../../../types/api'
  import { executeStreamQuery } from '../../../api/stream'
  import { apiPost, apiPut } from '../../../api/client'
  import { appendDefaultLimit } from '../../../utils/sql'
  import { error as toastError, success as toastSuccess } from '../../../stores/toast.svelte'
  import SqlEditor from '../../editor/SqlEditor.svelte'
  import Toolbar from '../../editor/Toolbar.svelte'
  import ResultPanel from '../../editor/ResultPanel.svelte'

  interface Props {
    tab: QueryTab
  }

  let { tab }: Props = $props()

  let editorComponent: SqlEditor
  const savedSplit = parseFloat(localStorage.getItem('ch-ui-split-percent') ?? '40')
  let splitPercent = $state(isNaN(savedSplit) ? 40 : savedSplit)
  let dragging = $state(false)
  let containerEl: HTMLDivElement
  let abortController: AbortController | null = null

  // Stream telemetry for the viewer
  let streamRows = $state(0)
  let streamChunks = $state(0)
  let streamStartedAt = $state<number | null>(null)
  let streamLastChunkAt = $state<number | null>(null)

  // Query plan state
  let planNodes = $state<QueryPlanNode[]>([])
  let planLines = $state<string[]>([])
  let planSource = $state('')
  let planLoading = $state(false)
  let planError = $state<string | null>(null)

  // Inline profile state
  let profile = $state<Record<string, unknown> | null>(null)
  let profileAvailable = $state(false)
  let profileReason = $state<string | null>(null)
  let profileLoading = $state(false)
  let profileError = $state<string | null>(null)

  // Sampling mode from last sample action
  let samplingMode = $state<string | null>(null)

  // Save modal state
  let showSaveModal = $state(false)
  let saveName = $state('')
  let saveDescription = $state('')
  let saving = $state(false)

  const result = $derived(getTabResult(tab.id))

  function handleSQLChange(sql: string) {
    updateTabSQL(tab.id, sql)
  }

  function toPositionalRows(meta: Array<{ name: string }>, rows: any[]): unknown[][] {
    if (!Array.isArray(rows)) return []
    return rows.map((row: any) => {
      if (Array.isArray(row)) return row
      return meta.map((col) => row[col.name])
    })
  }

  async function handleRun(sql?: string) {
    const query = sql ?? editorComponent?.getValue() ?? ''
    if (!query.trim()) return

    // Cancel any in-flight query
    if (abortController) abortController.abort()
    abortController = new AbortController()

    const limitedQuery = appendDefaultLimit(query)
    const startTime = performance.now()

    // Reset telemetry and inline profile for this run
    streamRows = 0
    streamChunks = 0
    streamStartedAt = Date.now()
    streamLastChunkAt = null
    profile = null
    profileAvailable = false
    profileReason = null
    profileError = null
    samplingMode = null

    // Row buffer for progressive accumulation
    let rowBuffer: unknown[][] = []
    let rafId: number | null = null

    setTabResult(tab.id, { running: true, error: null, meta: [], data: [], stats: null, elapsedMs: 0 })

    try {
      await executeStreamQuery(
        limitedQuery,
        (meta) => {
          setTabResult(tab.id, { meta, running: true })
        },
        (rows, seq) => {
          rowBuffer.push(...rows)
          streamRows = rowBuffer.length
          streamChunks = seq + 1
          streamLastChunkAt = Date.now()
          // Batch UI updates to animation frame
          if (!rafId) {
            rafId = requestAnimationFrame(() => {
              setTabResult(tab.id, { data: rowBuffer, elapsedMs: Math.round(performance.now() - startTime) })
              rafId = null
            })
          }
        },
        (stats, totalRows) => {
          // Final flush
          if (rafId) { cancelAnimationFrame(rafId); rafId = null }
          streamRows = totalRows || rowBuffer.length
          streamLastChunkAt = Date.now()
          setTabResult(tab.id, {
            data: rowBuffer,
            stats: stats ?? null,
            elapsedMs: Math.round(performance.now() - startTime),
            running: false,
          })
          void loadInlineProfile(limitedQuery)
        },
        (error) => {
          if (rafId) { cancelAnimationFrame(rafId); rafId = null }
          streamLastChunkAt = Date.now()
          setTabResult(tab.id, { error, running: false, elapsedMs: Math.round(performance.now() - startTime) })
        },
        abortController.signal,
      )
    } catch (e: any) {
      // AbortError is expected on cancel
      if (e.name !== 'AbortError') {
        streamLastChunkAt = Date.now()
        setTabResult(tab.id, { error: e.message, running: false })
      }
    }
  }

  function handleCancel() {
    if (abortController) {
      abortController.abort()
      abortController = null
      setTabResult(tab.id, { running: false })
    }
  }

  async function handleFormat() {
    const sql = editorComponent?.getValue() ?? ''
    if (!sql.trim()) return
    try {
      const formatted = await formatSQL(sql)
      editorComponent?.setValue(formatted)
    } catch (e: any) {
      toastError(e.message)
    }
  }

  async function handleExplain() {
    const sql = editorComponent?.getValue() ?? ''
    if (!sql.trim()) return

    profile = null
    profileAvailable = false
    profileReason = null
    profileError = null
    samplingMode = null

    setTabResult(tab.id, { running: true, error: null, meta: [], data: [], stats: null })

    try {
      const res = await explainQuery(sql)

      setTabResult(tab.id, {
        meta: res.meta ?? [],
        data: toPositionalRows(res.meta ?? [], res.data ?? []),
        stats: res.statistics ?? null,
        elapsedMs: res.elapsed_ms ?? 0,
        error: null,
        running: false,
      })
    } catch (e: any) {
      setTabResult(tab.id, { error: e.message, running: false })
    }
  }

  async function handleLoadPlan() {
    const sql = editorComponent?.getValue() ?? ''
    if (!sql.trim()) return

    planLoading = true
    planError = null
    try {
      const res = await fetchQueryPlan(sql)
      planNodes = res.nodes ?? []
      planLines = res.lines ?? []
      planSource = res.source ?? ''
      if (!planNodes.length && !planLines.length) {
        planError = 'No plan rows returned by ClickHouse'
      }
    } catch (e: any) {
      planError = e.message
    } finally {
      planLoading = false
    }
  }

  async function handleSample(perShard: number) {
    const sql = editorComponent?.getValue() ?? ''
    if (!sql.trim()) return

    setTabResult(tab.id, { running: true, error: null, meta: [], data: [], stats: null })
    samplingMode = null

    try {
      const res = await runSampleQuery({ query: sql, per_shard: perShard, shard_by: '_shard_num' })
      const rows = toPositionalRows(res.meta ?? [], (res.data as any[]) ?? [])
      setTabResult(tab.id, {
        meta: res.meta ?? [],
        data: rows,
        stats: res.statistics ?? null,
        elapsedMs: res.elapsed_ms ?? 0,
        error: null,
        running: false,
      })
      samplingMode = res.sampling_mode ?? 'global'
      streamRows = rows.length
      streamChunks = rows.length > 0 ? 1 : 0
      streamStartedAt = Date.now()
      streamLastChunkAt = Date.now()

      if (res.warning) toastSuccess(res.warning)
    } catch (e: any) {
      setTabResult(tab.id, { error: e.message, running: false })
    }
  }

  async function loadInlineProfile(executedQuery: string) {
    profileLoading = true
    profileError = null
    profileReason = null
    try {
      const res = await fetchQueryProfile(executedQuery)
      profileAvailable = !!res.available
      profile = res.profile ?? null
      profileReason = res.reason ?? null
    } catch (e: any) {
      profileAvailable = false
      profile = null
      profileError = e.message
    } finally {
      profileLoading = false
    }
  }

  function handleSaveClick() {
    if (tab.savedQueryId) {
      void saveLinkedSavedQuery()
      return
    }
    saveName = tab.name
    saveDescription = ''
    showSaveModal = true
  }

  async function saveLinkedSavedQuery() {
    const sql = editorComponent?.getValue() ?? tab.sql
    if (!sql.trim() || !tab.savedQueryId) return

    saving = true
    try {
      await apiPut(`/api/saved-queries/${tab.savedQueryId}`, {
        query: sql,
      })
      markQueryTabSaved(tab.id, { baseSql: sql })
      toastSuccess('Saved query updated')
    } catch (e: any) {
      toastError(e.message)
    } finally {
      saving = false
    }
  }

  async function handleSaveConfirm() {
    const sql = editorComponent?.getValue() ?? tab.sql
    if (!sql.trim() || !saveName.trim()) return

    saving = true
    try {
      const created = await apiPost<SavedQuery>('/api/saved-queries', {
        name: saveName.trim(),
        description: saveDescription.trim(),
        query: sql,
      })
      if (created?.id) {
        markQueryTabSaved(tab.id, {
          savedQueryId: created.id,
          name: created.name,
          baseSql: sql,
        })
      } else {
        markQueryTabSaved(tab.id, { baseSql: sql })
      }
      toastSuccess('Query saved')
      showSaveModal = false
    } catch (e: any) {
      toastError(e.message)
    } finally {
      saving = false
    }
  }

  // ── Drag handle logic ──
  function onDragStart(e: MouseEvent) {
    e.preventDefault()
    dragging = true
    document.addEventListener('mousemove', onDragMove)
    document.addEventListener('mouseup', onDragEnd)
  }

  function onDragMove(e: MouseEvent) {
    if (!containerEl) return
    const rect = containerEl.getBoundingClientRect()
    const y = e.clientY - rect.top
    const pct = (y / rect.height) * 100
    splitPercent = Math.max(15, Math.min(85, pct))
  }

  function onDragEnd() {
    dragging = false
    document.removeEventListener('mousemove', onDragMove)
    document.removeEventListener('mouseup', onDragEnd)
    localStorage.setItem('ch-ui-split-percent', String(splitPercent))
  }
</script>

<div class="flex flex-col h-full overflow-hidden" bind:this={containerEl}>
  <!-- Editor pane -->
  <div class="flex flex-col min-h-[80px] overflow-hidden border-b border-gray-200 dark:border-gray-800" style="height: {splitPercent}%">
    <Toolbar
      running={result?.running ?? false}
      onrun={() => handleRun()}
      oncancel={handleCancel}
      onformat={handleFormat}
      onexplain={handleExplain}
      onsave={handleSaveClick}
    />
    <div class="flex-1 min-h-0">
      <SqlEditor
        bind:this={editorComponent}
        value={tab.sql}
        onrun={handleRun}
        onchange={handleSQLChange}
      />
    </div>
  </div>

  <!-- Drag handle -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="h-1 shrink-0 cursor-row-resize group flex items-center justify-center hover:bg-ch-blue/20 transition-colors {dragging ? 'bg-ch-blue/30' : 'bg-gray-200 dark:bg-gray-800'}"
    onmousedown={onDragStart}
  >
    <div class="w-8 h-0.5 rounded-full {dragging ? 'bg-ch-blue' : 'bg-gray-600 group-hover:bg-ch-blue/60'} transition-colors"></div>
  </div>

  <!-- Results pane -->
  <div class="flex-1 min-h-[80px] min-h-0 overflow-hidden flex flex-col">
    <ResultPanel
      meta={result?.meta ?? []}
      data={result?.data ?? []}
      loading={result?.running ?? false}
      error={result?.error ?? null}
      stats={result?.stats ?? null}
      elapsedMs={result?.elapsedMs ?? 0}
      running={result?.running ?? false}
      {streamRows}
      {streamChunks}
      {streamStartedAt}
      {streamLastChunkAt}
      planNodes={planNodes}
      planLines={planLines}
      planSource={planSource}
      planLoading={planLoading}
      planError={planError}
      onLoadPlan={handleLoadPlan}
      onSample={handleSample}
      {profile}
      {profileAvailable}
      {profileReason}
      {profileLoading}
      {profileError}
      samplingMode={samplingMode}
    />
  </div>
</div>

<!-- Drag overlay to prevent iframe/editor stealing mouse events -->
{#if dragging}
  <div class="fixed inset-0 z-50 cursor-row-resize"></div>
{/if}

<!-- Save query modal -->
{#if showSaveModal}
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="fixed inset-0 z-50 flex items-center justify-center bg-black/60"
    onclick={() => showSaveModal = false}
    onkeydown={(e) => e.key === 'Escape' && (showSaveModal = false)}
  >
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div
      class="bg-gray-50 dark:bg-gray-900 border border-gray-300 dark:border-gray-700 rounded-lg p-5 w-96 shadow-xl"
      onclick={(e) => e.stopPropagation()}
      onkeydown={(e) => e.stopPropagation()}
      tabindex="-1"
    >
      <h3 class="text-sm font-semibold text-gray-800 dark:text-gray-200 mb-3">Save Query</h3>

      <label class="block mb-2">
        <span class="text-xs text-gray-500 dark:text-gray-400">Name</span>
        <input
          type="text"
          class="mt-1 w-full px-2.5 py-1.5 bg-gray-200 dark:bg-gray-800 border border-gray-300 dark:border-gray-700 rounded text-sm text-gray-800 dark:text-gray-200 focus:outline-none focus:border-ch-blue"
          bind:value={saveName}
          onkeydown={(e) => e.key === 'Enter' && handleSaveConfirm()}
        />
      </label>

      <label class="block mb-4">
        <span class="text-xs text-gray-500 dark:text-gray-400">Description (optional)</span>
        <input
          type="text"
          class="mt-1 w-full px-2.5 py-1.5 bg-gray-200 dark:bg-gray-800 border border-gray-300 dark:border-gray-700 rounded text-sm text-gray-800 dark:text-gray-200 focus:outline-none focus:border-ch-blue"
          bind:value={saveDescription}
        />
      </label>

      <div class="flex justify-end gap-2">
        <button
          class="px-3 py-1.5 text-xs text-gray-500 dark:text-gray-400 hover:text-gray-800 dark:hover:text-gray-200 rounded hover:bg-gray-200 dark:hover:bg-gray-800"
          onclick={() => showSaveModal = false}
        >Cancel</button>
        <button
          class="px-3 py-1.5 text-xs bg-ch-blue text-white rounded hover:bg-ch-blue/80 disabled:opacity-50"
          onclick={handleSaveConfirm}
          disabled={saving || !saveName.trim()}
        >{saving ? 'Saving...' : 'Save'}</button>
      </div>
    </div>
  </div>
{/if}
