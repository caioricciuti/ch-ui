<script lang="ts" module>
  // One in-flight stream per tab, shared across remounts: the component is
  // keyed by tab id, so switching tabs (or split-moving) destroys the instance
  // while the stream keeps running. Keeping the controller here lets the
  // remounted instance cancel or supersede it.
  const tabAborts = new Map<string, AbortController>()

  // Backend stream handler hard-caps maxRows (internal/server/handlers/query.go).
  const BACKEND_MAX_ROWS = 1_000_000
</script>

<script lang="ts">
  import type { QueryTab } from '../../../stores/tabs.svelte'
  import { updateTabSQL, getTabResult, setTabResult, getTabResultView, setTabResultView, resetTabResultView, markQueryTabSaved } from '../../../stores/tabs.svelte'
  import type { ColumnFilter, ResultSort } from '../../../utils/result-filters'
  import { filterRows, sortRows, cycleSort, isWrappableQuery, buildWrappedQuery } from '../../../utils/result-filters'
  import { getResultFiltersEnabled } from '../../../stores/result-filters.svelte'
  import { formatSQL, explainQuery, fetchQueryPlan, runSampleQuery, fetchQueryProfile, estimateQuery } from '../../../api/query'
  import type { QueryPlanNode, QueryEstimateResult } from '../../../types/query'
  import type { SavedQuery } from '../../../types/api'
  import { executeStreamQuery } from '../../../api/stream'
  import { apiPost, apiPut } from '../../../api/client'
  import { getMaxResultRows } from '../../../stores/query-limit.svelte'
  import { error as toastError, success as toastSuccess } from '../../../stores/toast.svelte'
  import { detectQueryParams } from '../../../utils/query-params'
  import { parseCHError, byteToCharOffset } from '../../../utils/ch-error'
  import { isProActive, loadLicense } from '../../../stores/license.svelte'
  import { openSingletonTab, openQueryTab } from '../../../stores/tabs.svelte'
  import { onMount } from 'svelte'
  import { Lock, Braces, X } from 'lucide-svelte'
  import SqlEditor from '../../editor/SqlEditor.svelte'
  import Toolbar from '../../editor/Toolbar.svelte'
  import ResultPanel from '../../editor/ResultPanel.svelte'
  import Sheet from '../../common/Sheet.svelte'
  import QueryHistoryPanel from '../../editor/QueryHistoryPanel.svelte'

  interface Props {
    tab: QueryTab
  }

  let { tab }: Props = $props()

  let editorComponent: SqlEditor
  const savedSplit = parseFloat(localStorage.getItem('ch-ui-split-percent') ?? '40')
  let splitPercent = $state(isNaN(savedSplit) ? 40 : savedSplit)
  let dragging = $state(false)
  let containerEl: HTMLDivElement

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

  // Query cost estimate state
  let estimate = $state<QueryEstimateResult | null>(null)
  let estimateLoading = $state(false)
  let estimateTimer: ReturnType<typeof setTimeout> | null = null
  let lastEstimatedSQL = ''

  // Query history drawer
  let showHistorySheet = $state(false)

  function handleHistoryOpen(sql: string) {
    showHistorySheet = false
    openQueryTab(sql)
  }

  function handleHistoryInsert(sql: string) {
    showHistorySheet = false
    editorComponent?.setValue(sql)
  }

  // Save modal state
  let showSaveModal = $state(false)
  let saveName = $state('')
  let saveDescription = $state('')
  let saving = $state(false)

  const result = $derived(getTabResult(tab.id))

  // ── Query parameters ({name:Type}) — Pro feature ──
  // The component is keyed per tab id, so seeding currentSql once in onMount is
  // safe; it's kept in sync afterwards by the editor's onchange handler.
  let currentSql = $state('')
  const detectedParams = $derived(detectQueryParams(currentSql))
  const proActive = $derived(isProActive())
  let paramValues = $state<Record<string, string>>({})
  let showParamsPanel = $state(false)
  let autoOpenedFor = ''

  // Auto-open the parameters panel the first time a new set of params appears,
  // so users discover it — but respect a manual close for that same set.
  $effect(() => {
    const sig = detectedParams.map((p) => p.name).join(',')
    if (sig && sig !== autoOpenedFor) {
      showParamsPanel = true
      autoOpenedFor = sig
    }
  })

  onMount(() => {
    currentSql = tab.sql ?? ''
    void loadLicense()
  })

  function handleSQLChange(sql: string) {
    currentSql = sql
    updateTabSQL(tab.id, sql)
    debouncedEstimate(sql)
  }

  function debouncedEstimate(sql: string) {
    if (estimateTimer) clearTimeout(estimateTimer)
    const trimmed = sql.trim()
    if (!trimmed || trimmed === lastEstimatedSQL) return
    // Only estimate SELECT/WITH queries
    const upper = trimmed.toUpperCase()
    if (!upper.startsWith('SELECT') && !upper.startsWith('WITH')) {
      estimate = null
      return
    }
    estimateTimer = setTimeout(() => void runEstimate(trimmed), 1500)
  }

  async function runEstimate(sql: string) {
    lastEstimatedSQL = sql
    estimateLoading = true
    try {
      estimate = await estimateQuery(sql)
    } catch {
      estimate = null
    } finally {
      estimateLoading = false
    }
  }

  function toPositionalRows(meta: Array<{ name: string }>, rows: any[]): unknown[][] {
    if (!Array.isArray(rows)) return []
    return rows.map((row: any) => {
      if (Array.isArray(row)) return row
      return meta.map((col) => row[col.name])
    })
  }

  async function runStreamingQuery(query: string, params: Record<string, string> | undefined, opts: { baseRun?: boolean } = {}) {
    // Cancel any in-flight query for this tab (even one started before a remount)
    tabAborts.get(tab.id)?.abort()
    const abortController = new AbortController()
    tabAborts.set(tab.id, abortController)

    const maxResultRows = getMaxResultRows()
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

    setTabResultView(tab.id, { executedSql: query })
    setTabResult(tab.id, { running: true, error: null, meta: [], data: [], stats: null, elapsedMs: 0 })

    try {
      await executeStreamQuery(
        query,
        maxResultRows,
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
          if (opts.baseRun) {
            // Hitting the limit means the in-memory rows are a partial view, so
            // sort/filter must go through a server-side re-query (issue #117).
            // The backend hard-caps at 1M rows regardless of the requested limit.
            const effectiveLimit = Math.min(maxResultRows, BACKEND_MAX_ROWS)
            setTabResultView(tab.id, { truncated: rowBuffer.length >= effectiveLimit })
          }
          void loadInlineProfile(query)
        },
        (error) => {
          if (rafId) { cancelAnimationFrame(rafId); rafId = null }
          streamLastChunkAt = Date.now()
          setTabResult(tab.id, { error, running: false, elapsedMs: Math.round(performance.now() - startTime) })
        },
        abortController.signal,
        params,
      )
    } catch (e: any) {
      // AbortError is expected on cancel
      if (e.name !== 'AbortError') {
        streamLastChunkAt = Date.now()
        setTabResult(tab.id, { error: e.message, running: false })
      }
    }
  }

  async function handleRun(sql?: string, sqlFrom?: number) {
    let query: string
    let queryFrom: number
    if (sql !== undefined) {
      const lead = sql.length - sql.trimStart().length
      query = sql.trim()
      queryFrom = sqlFrom !== undefined ? sqlFrom + lead : -1
    } else {
      const picked = editorComponent?.getSelectedOrAllWithRange()
      query = picked?.text ?? ''
      queryFrom = picked?.from ?? -1
    }
    if (!query) return

    // Query parameters ({name:Type}) are a Pro feature. Block non-Pro users with
    // an upsell rather than letting ClickHouse fail with an unbound-parameter error.
    const runParams = detectQueryParams(query)
    if (runParams.length > 0 && !proActive) {
      toastError('Query parameters are a Pro feature — upgrade to run parameterized queries.')
      return
    }
    const params = runParams.length > 0
      ? Object.fromEntries(runParams.map(p => [p.name, paramValues[p.name] ?? '']))
      : undefined

    resetTabResultView(tab.id, {
      baseSql: query,
      executedFrom: queryFrom,
      baseParams: params,
      wrappable: isWrappableQuery(query),
    })
    await runStreamingQuery(query, params, { baseRun: true })
  }

  // ── Result filtering & ordering (issue #117) ──
  // Client mode sorts/filters the in-memory rows; server mode re-runs the query
  // wrapped in SELECT * FROM (...) so truncated results are ordered over the
  // full dataset instead of just the loaded page.
  const CLIENT_OP_MAX_ROWS = 50_000

  const view = $derived(getTabResultView(tab.id))
  // When a server-side re-query produced the current rows, the data is
  // physically filtered — the toggle must not hide that provenance (chips,
  // badge) or the escape path (removing filters re-runs the base query).
  const filtersEnabled = $derived(getResultFiltersEnabled() || view.serverApplied)

  const displayData = $derived.by(() => {
    const rows = result?.data ?? []
    if (!filtersEnabled || view.serverApplied) return rows
    const meta = result?.meta ?? []
    let out = rows
    if (view.filters.length > 0) out = filterRows(out, meta, view.filters)
    if (view.sort) out = sortRows(out, meta, view.sort)
    return out
  })

  function applyView(sort: ResultSort | null, filters: ColumnFilter[]) {
    if (!result || result.running) return
    const needsServer = view.serverApplied || view.truncated || result.data.length > CLIENT_OP_MAX_ROWS
    if (needsServer && view.wrappable && view.baseSql) {
      const hasView = sort !== null || filters.length > 0
      setTabResultView(tab.id, { sort, filters, serverApplied: hasView })
      // The clear-filters re-run carries the marker too: it's grid mechanics,
      // not a user-initiated run, so it must not land in query history.
      const sql = hasView
        ? buildWrappedQuery(view.baseSql, filters, sort, result.meta, getMaxResultRows())
        : `/* ch-ui:result-filter */ ${view.baseSql}`
      // Re-running the base query replaces the in-memory data under the
      // current limit, so truncation must be recomputed for it.
      void runStreamingQuery(sql, view.baseParams, { baseRun: !hasView })
    } else {
      setTabResultView(tab.id, { sort, filters, serverApplied: false })
    }
  }

  function handleSortToggle(column: string) {
    applyView(cycleSort(view.sort, column), view.filters)
  }

  function handleFilterChange(column: string, filter: ColumnFilter | null) {
    const next = view.filters.filter((f) => f.column !== column)
    if (filter) next.push(filter)
    applyView(view.sort, next)
  }

  function handleClearFilters() {
    applyView(view.sort, [])
  }

  // ── Error position → editor jump ──
  // ClickHouse reports "failed at position N" as an offset into the executed
  // SQL. Only map it back when the parser says the position indexes the query
  // itself, the executed text was the plain editor SQL (not a wrapped/marked
  // grid re-query), and that text is still present in the doc — anchored at
  // the offset it was run from, so duplicated fragments resolve correctly.
  const errorJump = $derived.by(() => {
    if (!result?.error) return null
    const parsed = parseCHError(result.error)
    if (parsed.position === null || !parsed.positionInQuery) return null
    if (!view.executedSql || view.executedSql !== view.baseSql) return null
    const base = view.executedFrom >= 0 && currentSql.startsWith(view.executedSql, view.executedFrom)
      ? view.executedFrom
      : currentSql.indexOf(view.executedSql)
    if (base < 0) return null
    const from = base + byteToCharOffset(view.executedSql, parsed.position - 1)
    // Select the offending token only if the document actually has it at the
    // target (newer servers report it without the wrapping quotes).
    let to = from
    if (parsed.token) {
      for (const cand of [parsed.token, `'${parsed.token}'`]) {
        if (currentSql.startsWith(cand, from)) {
          to = from + cand.length
          break
        }
      }
    }
    // Label line/col from the mapped document position — ClickHouse's own
    // line/col are relative to the executed fragment, not the document.
    const before = currentSql.slice(0, from)
    const line = (before.match(/\n/g)?.length ?? 0) + 1
    const col = from - before.lastIndexOf('\n')
    return { from, to, line, col }
  })

  function handleGoToError() {
    const jump = errorJump
    if (jump) editorComponent?.selectRange(jump.from, jump.to)
  }

  function handleCancel() {
    const controller = tabAborts.get(tab.id)
    if (controller) {
      controller.abort()
      tabAborts.delete(tab.id)
    }
    setTabResult(tab.id, { running: false })
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

    tabAborts.get(tab.id)?.abort()
    resetTabResultView(tab.id)
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

    tabAborts.get(tab.id)?.abort()
    resetTabResultView(tab.id)
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

  // Default param values to persist with a saved query, or undefined when none.
  function paramDefaultsFor(sql: string): Record<string, string> | undefined {
    const list = detectQueryParams(sql)
    if (list.length === 0) return undefined
    return Object.fromEntries(list.map(p => [p.name, paramValues[p.name] ?? '']))
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
        parameters: paramDefaultsFor(sql) ?? {},
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
        parameters: paramDefaultsFor(sql) ?? {},
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
      onhistory={() => (showHistorySheet = true)}
      onparams={() => (showParamsPanel = !showParamsPanel)}
      paramCount={detectedParams.length}
      paramsActive={showParamsPanel}
      {estimate}
      {estimateLoading}
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

  <!-- Query parameters panel (Pro) -->
  {#if showParamsPanel}
    <div class="shrink-0 border-b border-gray-200 dark:border-gray-800 bg-gray-50/70 dark:bg-gray-900/40">
      <div class="flex items-center justify-between px-3 py-1.5 border-b border-gray-200/70 dark:border-gray-800/70">
        <div class="flex items-center gap-1.5 text-xs font-semibold text-gray-700 dark:text-gray-200">
          <Braces size={13} class="text-ch-orange" />
          Query Parameters
          {#if detectedParams.length > 0}
            <span class="text-gray-400 font-normal">· {detectedParams.length} detected</span>
          {/if}
        </div>
        <button class="ds-icon-btn" onclick={() => (showParamsPanel = false)} title="Close" aria-label="Close parameters">
          <X size={14} />
        </button>
      </div>

      <div class="px-3 py-2.5">
        {#if detectedParams.length === 0}
          <!-- Educational empty state -->
          <p class="text-xs text-gray-500 dark:text-gray-400">
            Add bind parameters to your SQL with
            <code class="px-1 py-0.5 rounded bg-gray-200/70 dark:bg-gray-800 font-mono text-[11px]">{'{name:Type}'}</code>
            syntax — they'll appear here with an input for each. For example:
          </p>
          <pre class="mt-2 text-[11px] font-mono text-gray-600 dark:text-gray-300 bg-white dark:bg-gray-950/40 border border-gray-200 dark:border-gray-800 rounded-md px-2.5 py-2 overflow-x-auto">SELECT * FROM events
WHERE user_id = {'{user_id:UInt64}'}
  AND created_at >= {'{since:DateTime}'}</pre>
          {#if !proActive}
            <p class="mt-2 text-[11px] text-gray-400 flex items-center gap-1">
              <Lock size={11} class="text-ch-orange" /> Running parameterized queries is a Pro feature.
            </p>
          {/if}
        {:else if proActive}
          <div class="flex flex-wrap gap-3">
            {#each detectedParams as p (p.name)}
              <label class="flex flex-col gap-1">
                <span class="text-[11px] font-mono text-gray-500">
                  {p.name}<span class="text-gray-400">:{p.type}</span>
                </span>
                <input
                  class="ds-input-sm w-44"
                  bind:value={paramValues[p.name]}
                  placeholder={`value (${p.type})`}
                  spellcheck="false"
                  onkeydown={(e) => { if (e.key === 'Enter') handleRun() }}
                />
              </label>
            {/each}
          </div>
          <p class="mt-2 text-[11px] text-gray-400">
            Values are bound safely by ClickHouse and saved as defaults with the query.
          </p>
        {:else}
          <!-- Pro upsell -->
          <div class="flex items-center justify-between gap-3">
            <div class="flex items-center gap-2 text-xs text-gray-600 dark:text-gray-300">
              <Lock size={14} class="text-ch-orange shrink-0" />
              <span>
                This query uses <strong>{detectedParams.length}</strong>
                parameter{detectedParams.length === 1 ? '' : 's'}
                (<span class="font-mono">{detectedParams.map((p) => p.name).join(', ')}</span>).
                Query parameters are a <strong>Pro</strong> feature.
              </span>
            </div>
            <button class="ds-btn-primary px-2.5 py-1 shrink-0" onclick={() => openSingletonTab('settings', 'License')}>
              Upgrade
            </button>
          </div>
        {/if}
      </div>
    </div>
  {/if}

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
      data={displayData}
      loading={result?.running ?? false}
      error={result?.error ?? null}
      stats={result?.stats ?? null}
      elapsedMs={result?.elapsedMs ?? 0}
      running={result?.running ?? false}
      totalRows={filtersEnabled && !view.serverApplied && view.filters.length > 0 ? (result?.data.length ?? 0) : null}
      sort={filtersEnabled ? view.sort : null}
      filters={filtersEnabled ? view.filters : []}
      serverApplied={view.serverApplied}
      partialView={filtersEnabled && view.truncated && !view.wrappable && (view.sort !== null || view.filters.length > 0)}
      onsort={filtersEnabled ? handleSortToggle : undefined}
      onfilterchange={filtersEnabled ? handleFilterChange : undefined}
      onclearfilters={handleClearFilters}
      ongotoerror={errorJump ? handleGoToError : undefined}
      errorTargetLine={errorJump?.line ?? null}
      errorTargetCol={errorJump?.col ?? null}
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
      {estimate}
    />
  </div>
</div>

<!-- Drag overlay to prevent iframe/editor stealing mouse events -->
{#if dragging}
  <div class="fixed inset-0 z-50 cursor-row-resize"></div>
{/if}

<!-- Query history drawer -->
<Sheet open={showHistorySheet} title="Query History" size="md" onclose={() => (showHistorySheet = false)}>
  <QueryHistoryPanel onopen={handleHistoryOpen} oninsert={handleHistoryInsert} />
</Sheet>

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
