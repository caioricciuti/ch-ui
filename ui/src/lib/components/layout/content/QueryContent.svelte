<script lang="ts" module>
  import { createFrameBatcher } from '../../../editor/runGuard'
  import { queryRuns } from '../../../editor/query-runs'

  // One in-flight run per tab, shared across remounts: the component is
  // keyed by tab id, so switching tabs (or split-moving) destroys the instance
  // while the stream keeps running. The shared registry lets the remounted
  // instance cancel or supersede it, lets the tab store stop it when the tab,
  // its result or the connection goes away, and lets late callbacks of a
  // superseded run (rows, done, error) see that they no longer own the result.
  const tabRuns = queryRuns

  // Backend stream handler hard-caps maxRows (internal/server/handlers/query.go).
  const BACKEND_MAX_ROWS = 1_000_000
</script>

<script lang="ts">
  import { goTo } from '../../../stores/router.svelte'
  import type { QueryTab } from '../../../stores/tabs.svelte'
  import {
    updateTabSQL,
    getTabResult,
    getTabResults,
    setTabResult,
    isTabRunning,
    startTabResult,
    dropUnpinnedTabResults,
    setActiveTabResult,
    closeTabResult,
    toggleTabResultPin,
    getResultView,
    setResultView,
    resetResultView,
    markQueryTabSaved,
    setTabParamValues,
  } from '../../../stores/tabs.svelte'
  import { createUUID } from '../../../utils/uuid'
  import { resultLabel, MAX_PINNED_PER_TAB } from '../../../editor/result-tabs'
  import { exportFullResult, type FullExportFormat } from '../../../api/stream'
  import { downloadBlob } from '../../../utils/export'
  import { formatNumber } from '../../../utils/format'
  import ResultTabs from '../../editor/ResultTabs.svelte'
  import type { ColumnFilter, ResultSort } from '../../../utils/result-filters'
  import { filterRows, sortRows, cycleSort, isWrappableQuery, buildWrappedQuery } from '../../../utils/result-filters'
  import { getResultFiltersEnabled } from '../../../stores/result-filters.svelte'
  import { formatSQLDetailed, explainQuery, fetchQueryPlan, runSampleQuery, fetchQueryProfile, estimateQuery } from '../../../api/query'
  import type { QueryPlanNode, QueryEstimateResult, QueryProgress } from '../../../types/query'
  import type { SavedQuery, SavedQueryFolder, SavedQueryVisibility } from '../../../types/api'
  import { listSavedQueryFolders, folderOptions as savedQueryFolderOptions, visibilityOptions } from '../../../api/savedQueries'
  import { executeStreamQuery } from '../../../api/stream'
  import { apiPost, apiPut } from '../../../api/client'
  import { getMaxResultRows } from '../../../stores/query-limit.svelte'
  import { error as toastError, success as toastSuccess, info as toastInfo, warning as toastWarning } from '../../../stores/toast.svelte'
  import { budgetWarningText } from '../../brain/brain-budget'
  import { detectQueryParams, isParamValueMissing, paramDefaultsToSave } from '../../../utils/query-params'
  import { firstKeyword, splitStatements, statementAt } from '../../../editor/statements'
  import { parseCHError, byteToCharOffset } from '../../../utils/ch-error'
  import { isProActive, loadLicense } from '../../../stores/license.svelte'
  import { getSession } from '../../../stores/session.svelte'
  import { openQueryTab, openSavedQueryTab } from '../../../stores/tabs.svelte'
  import { createInlineProfileLoader, EMPTY_PROFILE, type InlineProfileState } from '../../../editor/inline-profile'
  import { linkedSaveBody, savedQueryConflict } from '../../../editor/saved-query-save'
  import { onMount, untrack } from 'svelte'
  import { Lock, X } from 'lucide-svelte'
  import Button from '../../common/Button.svelte'
  import SqlEditor from '../../editor/SqlEditor.svelte'
  import Toolbar from '../../editor/Toolbar.svelte'
  import ParamsPopover from '../../editor/ParamsPopover.svelte'
  import ResultPanel from '../../editor/ResultPanel.svelte'
  import Sheet from '../../common/Sheet.svelte'
  import Modal from '../../common/Modal.svelte'
  import FormField from '../../common/FormField.svelte'
  import Input from '../../common/Input.svelte'
  import Select from '../../common/Select.svelte'
  import QueryHistoryPanel from '../../editor/QueryHistoryPanel.svelte'
  import AskAIBar from '../../editor/AskAIBar.svelte'
  import SqlDiff from '../../common/SqlDiff.svelte'
  import Badge from '../../common/Badge.svelte'
  import Spinner from '../../common/Spinner.svelte'
  import { fixSQLStream, type FixSQLResult } from '../../../api/brainPro'
  import { fixRange, fixTarget, locateExecuted } from '../../../editor/fix-with-ai'

  interface Props {
    tab: QueryTab
  }

  let { tab }: Props = $props()

  let editorComponent: SqlEditor
  const savedSplitRaw = localStorage.getItem('ch-ui-split-percent')
  const savedSplit = parseFloat(savedSplitRaw ?? '40')
  let splitPercent = $state(isNaN(savedSplit) ? 40 : savedSplit)
  // Until the user drags the splitter, the editor pane sizes itself to the
  // SQL (a two-line query does not deserve half the screen) and the results
  // get the rest. A drag switches to the fixed percentage and remembers it.
  let userSized = $state(savedSplitRaw !== null)
  let dragging = $state(false)
  let containerEl: HTMLDivElement

  // Stream telemetry for the viewer
  let streamRows = $state(0)
  let streamChunks = $state(0)
  let streamStartedAt = $state<number | null>(null)
  let streamLastChunkAt = $state<number | null>(null)
  // Live progress of the running query (rows read, throughput, percent done).
  let streamProgress = $state<QueryProgress | null>(null)

  // Query plan state
  let planNodes = $state<QueryPlanNode[]>([])
  let planLines = $state<string[]>([])
  let planSource = $state('')
  let planLoading = $state(false)
  let planError = $state<string | null>(null)

  // Inline profile of the latest run; a late answer for an earlier run is dropped.
  let profileState = $state<InlineProfileState>(EMPTY_PROFILE)
  const profiles = createInlineProfileLoader(fetchQueryProfile, (next) => (profileState = next))

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
  let saveFolder = $state('')
  let saveVisibility = $state<SavedQueryVisibility>('shared')
  let saveFolders = $state<SavedQueryFolder[]>([])
  let saving = $state(false)
  const saveIsAdmin = $derived(getSession()?.role === 'admin')

  const result = $derived(getTabResult(tab.id))
  const tabResults = $derived(getTabResults(tab.id))
  const running = $derived(isTabRunning(tab.id))
  // Stream telemetry and the inline profile describe the latest run; another
  // result tab on screen shows only its own numbers.
  let lastRunResultId = $state<string | null>(null)
  const showsLastRun = $derived(!!result && result.id === lastRunResultId)

  // ── Query parameters ({name:Type}) ──
  // The component is keyed per tab id, so seeding currentSql once in onMount is
  // safe; it's kept in sync afterwards by the editor's onchange handler.
  let currentSql = $state('')
  const detectedParams = $derived(detectQueryParams(currentSql))
  const proActive = $derived(isProActive())
  // Values live in the tab (so they survive tab switches and reloads) and
  // start from the saved query's stored defaults.
  let paramValues = $state<Record<string, string>>(untrack(() => ({ ...(tab.paramValues ?? {}) })))
  $effect(() => {
    const values = $state.snapshot(paramValues)
    untrack(() => {
      if (JSON.stringify(values) !== JSON.stringify(tab.paramValues ?? {})) setTabParamValues(tab.id, values)
    })
  })
  // Re-opening the saved query over this tab can bring new stored defaults.
  $effect(() => {
    const incoming = tab.paramValues ?? {}
    untrack(() => {
      if (JSON.stringify(incoming) !== JSON.stringify($state.snapshot(paramValues))) paramValues = { ...incoming }
    })
  })
  // The parameters popover is anchored to the toolbar button. It never opens
  // on its own: Run opens it when a value is missing, the button toggles it.
  let showParamsPanel = $state(false)
  let paramsButtonEl = $state<HTMLButtonElement | null>(null)
  let paramsAnchor = $state({ x: 16, y: 80 })
  let focusParam = $state<string | null>(null)

  function openParams(focus: string | null = null) {
    const rect = paramsButtonEl?.getBoundingClientRect()
    if (rect) paramsAnchor = { x: rect.left, y: rect.bottom + 6 }
    focusParam = focus
    showParamsPanel = true
  }

  onMount(() => {
    currentSql = tab.sql ?? ''
    void loadLicense()
  })

  function handleSQLChange(sql: string) {
    currentSql = sql
    updateTabSQL(tab.id, sql)
    debouncedEstimate(sql)
  }

  // ── Ask AI (text-to-SQL) bar, Pro ─────────────────────────────────────────
  let showAsk = $state(false)
  let askQuestion = $state('')

  // What Ask AI edits: the same text Run would execute (the selection, else
  // the statement under the cursor, else the whole query), taken when the
  // question is sent.
  let askTarget: { text: string; from: number } | null = null

  function askContext(): string {
    if (!editorComponent) return ''
    askTarget = editorComponent.getRunnable()
    return askTarget.text
  }

  function insertGeneratedSQL(sql: string) {
    if (!editorComponent) return
    const doc = editorComponent.getValue()
    let next = sql
    const target = askTarget
    askTarget = null
    if (target?.text) {
      // Put the revised query where its source was. If that text moved
      // while the AI was writing, find it again; replace everything only
      // when it is gone.
      const from = doc.slice(target.from, target.from + target.text.length) === target.text ? target.from : doc.indexOf(target.text)
      if (from >= 0) next = doc.slice(0, from) + sql + doc.slice(from + target.text.length)
    }
    // One editor transaction, so a single undo brings the old SQL back.
    editorComponent.setValue(next)
    currentSql = next
    updateTabSQL(tab.id, next)
  }

  // EXPLAIN ESTIMATE runs after a long typing pause, for a single SELECT
  // whose parameters all have values; anything else clears the estimate.
  const ESTIMATE_DEBOUNCE_MS = 3000

  function debouncedEstimate(sql: string) {
    if (estimateTimer) clearTimeout(estimateTimer)
    estimateTimer = null
    const statements = splitStatements(sql)
    const single = statements.length === 1 ? statements[0].text : ''
    const kw = single ? firstKeyword(single) : ''
    if (!single || (kw !== 'SELECT' && kw !== 'WITH')) {
      estimate = null
      lastEstimatedSQL = ''
      return
    }
    const params = paramsFor([single], false)
    if (params === null) {
      estimate = null
      return
    }
    const key = single + '\u0000' + JSON.stringify(params ?? {})
    if (key === lastEstimatedSQL) return
    estimateTimer = setTimeout(() => void runEstimate(single, params, key), ESTIMATE_DEBOUNCE_MS)
  }

  async function runEstimate(sql: string, params: Record<string, string> | undefined, key: string) {
    lastEstimatedSQL = key
    estimateLoading = true
    try {
      const res = await estimateQuery(sql, params)
      // A newer edit may have cleared or replaced the estimate meanwhile.
      if (lastEstimatedSQL === key) estimate = res
    } catch {
      if (lastEstimatedSQL === key) estimate = null
    } finally {
      estimateLoading = false
    }
  }

  /**
   * Bind parameters for the given statements. Returns undefined when they use
   * none, null when one has no value (and, if ask is set, opens the
   * parameters popover on it).
   */
  function paramsFor(sqls: string[], ask = true): Record<string, string> | undefined | null {
    const seen = new Set<string>()
    const list = sqls.flatMap((q) => detectQueryParams(q)).filter((p) => !seen.has(p.name) && seen.add(p.name))
    if (list.length === 0) return undefined
    const missing = list.find((p) => isParamValueMissing(p, paramValues[p.name]))
    if (missing) {
      if (ask) openParams(missing.name)
      return null
    }
    return Object.fromEntries(list.map((p) => [p.name, paramValues[p.name] ?? '']))
  }

  function toPositionalRows(meta: Array<{ name: string }>, rows: unknown): unknown[][] {
    if (!Array.isArray(rows)) return []
    return rows.map((row: unknown) => {
      if (Array.isArray(row)) return row as unknown[]
      const record = (row ?? {}) as Record<string, unknown>
      return meta.map((col) => record[col.name])
    })
  }

  function errorMessage(e: unknown): string {
    return e instanceof Error ? e.message : String(e)
  }

  type RunOutcome = 'ok' | 'error' | 'stopped'

  /**
   * Claims a result tab for a run; the run's own id is the result id. A run
   * supersedes the tab's in-flight run (even one started before a remount);
   * a side run (a re-query of one existing result) supersedes only an
   * earlier run filling that result, so a Run All keeps going. The
   * superseded result stops spinning and says why. The run started last
   * owns the stream telemetry and the inline profile.
   */
  function claimRun(resultId: string, onStop: () => void = () => {}, side = false): number {
    const tabId = tab.id
    const cancel = () => {
      onStop()
      const r = getTabResults(tabId).results.find((x) => x.id === resultId)
      if (r?.running) setTabResult(tabId, resultId, { running: false, error: 'Query cancelled' })
    }
    const runId = side ? tabRuns.startSide(tabId, resultId, cancel) : tabRuns.start(tabId, cancel, resultId)
    lastRunResultId = resultId
    profiles.reset()
    samplingMode = null
    return runId
  }

  async function runStreamingQuery(query: string, params: Record<string, string> | undefined, opts: { baseRun?: boolean; resultId: string; side?: boolean }): Promise<RunOutcome> {
    let outcome: RunOutcome = 'stopped'
    const resultId = opts.resultId
    const maxResultRows = getMaxResultRows()
    const startTime = performance.now()
    const abortController = new AbortController()
    // Rows accumulate here and reach the result at most once per frame.
    const rows = createFrameBatcher<unknown[]>((data) => {
      setTabResult(tab.id, resultId, { data, elapsedMs: Math.round(performance.now() - startTime) })
    })
    // Aborts the fetch and drops the pending frame of rows when superseded.
    const runId = claimRun(resultId, () => {
      abortController.abort()
      rows.close()
    }, opts.side)
    const live = () => tabRuns.isCurrent(tab.id, runId)
    // A side run can overlap a Run All; telemetry belongs to the run started last.
    const ownsTelemetry = () => lastRunResultId === resultId

    // Reset telemetry for this run
    streamRows = 0
    streamChunks = 0
    streamStartedAt = Date.now()
    streamLastChunkAt = null
    streamProgress = null

    setResultView(resultId, { executedSql: query })
    setTabResult(tab.id, resultId, { running: true, error: null, meta: [], data: [], stats: null, elapsedMs: 0, rowsDropped: false })

    try {
      await executeStreamQuery(
        query,
        maxResultRows,
        (meta) => {
          if (!live()) return
          setTabResult(tab.id, resultId, { meta, running: true })
        },
        (chunk, seq) => {
          if (!live()) return
          rows.push(chunk)
          if (!ownsTelemetry()) return
          streamRows = rows.items.length
          streamChunks = seq + 1
          streamLastChunkAt = Date.now()
        },
        (stats, totalRows, queryId) => {
          if (!live()) return
          outcome = 'ok'
          // Final flush
          rows.close()
          if (ownsTelemetry()) {
            streamRows = totalRows || rows.items.length
            streamLastChunkAt = Date.now()
          }
          setTabResult(tab.id, resultId, {
            data: rows.items,
            stats: stats ?? null,
            elapsedMs: Math.round(performance.now() - startTime),
            running: false,
          })
          if (opts.baseRun) {
            // Hitting the limit means the in-memory rows are a partial view, so
            // sort/filter must go through a server-side re-query (issue #117).
            // The backend hard-caps at 1M rows regardless of the requested limit.
            const effectiveLimit = Math.min(maxResultRows, BACKEND_MAX_ROWS)
            setResultView(resultId, { truncated: rows.items.length >= effectiveLimit })
          }
          // The profile is looked up by query_id; without one there is nothing
          // to find. A run started since owns the profile panel.
          if (queryId && ownsTelemetry()) void profiles.load(queryId, query)
        },
        (error) => {
          if (!live()) return
          outcome = 'error'
          rows.close()
          if (ownsTelemetry()) streamLastChunkAt = Date.now()
          setTabResult(tab.id, resultId, { error, running: false, elapsedMs: Math.round(performance.now() - startTime) })
        },
        abortController.signal,
        params,
        (progress) => {
          if (!live() || !ownsTelemetry()) return
          streamProgress = progress
        },
      )
    } catch (e) {
      // AbortError is expected on cancel; a superseded run owns nothing.
      if (!live() || (e instanceof Error && e.name === 'AbortError')) return 'stopped'
      rows.close()
      if (ownsTelemetry()) streamLastChunkAt = Date.now()
      setTabResult(tab.id, resultId, { error: errorMessage(e), running: false })
      return 'error'
    }
    return live() ? outcome : 'stopped'
  }

  async function handleRun(sql?: string, sqlFrom?: number) {
    let query: string
    let queryFrom: number
    if (sql !== undefined) {
      const lead = sql.length - sql.trimStart().length
      query = sql.trim()
      queryFrom = sqlFrom !== undefined ? sqlFrom + lead : -1
    } else {
      const picked = editorComponent?.getRunnable()
      query = picked?.text ?? ''
      queryFrom = picked?.from ?? -1
    }
    if (!query) return

    // Query parameters ({name:Type}) are bound by ClickHouse. A parameter
    // without a value would fail there; ask for it here, with the cursor
    // already in the first empty field.
    const params = paramsFor([query])
    if (params === null) return
    await runOne(query, queryFrom, params, 'replace')
  }

  /** 1-based position of the statement at a document offset, or null. */
  function statementNumber(from: number): number | null {
    if (from < 0) return null
    const statements = splitStatements(currentSql)
    const st = statementAt(statements, from)
    return st ? statements.indexOf(st) + 1 : null
  }

  /** "n · TAG" for results that are not a plain run of the statement. */
  function taggedLabel(tag: string, from: number): string {
    const n = statementNumber(from)
    return n !== null ? `${n} · ${tag}` : tag
  }

  /** Starts a result tab for a run that is about to take over the result pane. */
  function newResult(sql: string, from: number, params: Record<string, string> | undefined, mode: 'replace' | 'append', label?: string): string {
    const id = createUUID()
    startTabResult(tab.id, { id, label: label ?? resultLabel(sql, statementNumber(from)), sql, params }, mode)
    return id
  }

  /**
   * Runs one statement. 'replace' (Mod-Enter) takes the place of the active
   * result unless it is pinned; 'append' (a run of several) adds a result tab.
   */
  function runOne(query: string, queryFrom: number, params: Record<string, string> | undefined, mode: 'replace' | 'append'): Promise<RunOutcome> {
    const resultId = newResult(query, queryFrom, params, mode)
    resetResultView(resultId, {
      baseSql: query,
      executedFrom: queryFrom,
      baseParams: params,
      wrappable: isWrappableQuery(query),
    })
    return runStreamingQuery(query, params, { baseRun: true, resultId })
  }

  // Shift-Mod-Enter: run every statement in order, each into its own result
  // tab, and stop at the first error. Pinned results stay; other results of
  // earlier runs are cleared first.
  let runAllActive = false

  async function handleRunAll(statements: { text: string; from: number }[]) {
    if (runAllActive || statements.length === 0) return
    const params = paramsFor(statements.map((st) => st.text))
    if (params === null) return
    runAllActive = true
    if (statements.length > 1) dropUnpinnedTabResults(tab.id)
    try {
      for (let i = 0; i < statements.length; i++) {
        const st = statements[i]
        const own = paramsFor([st.text], false) ?? undefined
        const outcome = await runOne(st.text, st.from, own, statements.length > 1 ? 'append' : 'replace')
        if (outcome !== 'ok') {
          if (outcome === 'error' && statements.length > 1) toastError(`Statement ${i + 1} of ${statements.length} failed; the rest did not run`)
          return
        }
      }
      if (statements.length > 1) toastSuccess(`Ran ${statements.length} statements`)
    } finally {
      runAllActive = false
    }
  }

  /** The text Run would execute (selection or statement under the cursor) and its bind parameters. */
  function runnableWithParams(): { sql: string; from: number; params: Record<string, string> | undefined } | null {
    const picked = editorComponent?.getRunnable()
    const sql = picked?.text ?? ''
    if (!sql.trim()) return null
    const params = paramsFor([sql])
    if (params === null) return null
    return { sql, from: picked?.from ?? -1, params }
  }

  // ── Result filtering & ordering (issue #117) ──
  // Client mode sorts/filters the in-memory rows; server mode re-runs the query
  // wrapped in SELECT * FROM (...) so truncated results are ordered over the
  // full dataset instead of just the loaded page.
  const CLIENT_OP_MAX_ROWS = 50_000

  const view = $derived(getResultView(result?.id ?? ''))
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
      setResultView(result.id, { sort, filters, serverApplied: hasView })
      // The clear-filters re-run carries the marker too: it's grid mechanics,
      // not a user-initiated run, so it must not land in query history.
      const sql = hasView
        ? buildWrappedQuery(view.baseSql, filters, sort, result.meta, getMaxResultRows())
        : `/* ch-ui:result-filter */ ${view.baseSql}`
      // Re-running the base query replaces the in-memory data under the
      // current limit, so truncation must be recomputed for it. It runs
      // beside the tab's run: sorting one result must not stop a Run All.
      void runStreamingQuery(sql, view.baseParams, { baseRun: !hasView, resultId: result.id, side: true })
    } else {
      setResultView(result.id, { sort, filters, serverApplied: false })
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
    const base = locateExecuted(currentSql, view)
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

  // ── Fix with AI ──
  // Offered for errors a model can fix, only when the failed query is the
  // plain editor text and still sits where it ran from. The fix is shown as
  // a diff and replaces exactly that statement, as one undoable change.
  const fixAvailable = $derived(fixTarget({ pro: proActive, error: result?.error, doc: currentSql, view }))
  let fixOpen = $state(false)
  let fixLoading = $state(false)
  let fixPreview = $state('')
  let fixPhase = $state('')
  let fixError = $state('')
  let fixResult = $state<FixSQLResult | null>(null)
  let fixOriginal = $state<{ from: number; sql: string } | null>(null)
  let fixController: AbortController | null = null

  const fixBadge = $derived.by((): { tone: 'success' | 'danger' | 'neutral'; label: string; title?: string } | null => {
    const v = fixResult?.validation
    if (!v) return null
    if (v.status === 'passed') return { tone: 'success', label: 'Checked with EXPLAIN' }
    if (v.status === 'failed') return { tone: 'danger', label: 'Still fails', title: v.error }
    return { tone: 'neutral', label: 'Not verified', title: v.error ?? v.reason }
  })

  async function handleFixWithAI() {
    const target = fixAvailable
    const error = result?.error
    if (!target || !error) return
    fixController?.abort()
    const controller = new AbortController()
    fixController = controller
    fixOriginal = target
    fixResult = null
    fixError = ''
    fixPreview = ''
    fixPhase = ''
    fixLoading = true
    fixOpen = true
    try {
      const res = await fixSQLStream(
        { sql: target.sql, error, params: view.baseParams },
        {
          ondelta: (text) => {
            fixPreview += text
            fixPhase = ''
          },
          onreset: () => (fixPreview = ''),
          onvalidating: () => (fixPhase = 'Checking the fix with EXPLAIN'),
          onrepair: (round) => (fixPhase = `ClickHouse rejected the fix, repairing (${round} of 2)`),
        },
        controller.signal,
      )
      if (fixController !== controller) return
      if (!res.sql.trim()) fixError = 'The AI did not return a query.'
      else fixResult = res
      if (res.budget) toastWarning(budgetWarningText(res.budget))
    } catch (e: unknown) {
      if (e instanceof DOMException && e.name === 'AbortError') return
      let message = e instanceof Error ? e.message : 'Failed to fix the query'
      if (/no active ai model/i.test(message)) message = 'No AI provider configured. Add one in Admin > Brain.'
      if (fixController === controller) fixError = message
    } finally {
      if (fixController === controller) {
        fixLoading = false
        fixController = null
      }
    }
  }

  function closeFix() {
    fixController?.abort()
    fixController = null
    fixLoading = false
    fixOpen = false
  }

  function acceptFix() {
    const res = fixResult
    const original = fixOriginal
    if (!res || !original || !editorComponent) return
    const range = fixRange(editorComponent.getValue(), original.from, original.sql)
    if (!range) {
      toastError('The query changed in the editor since it ran. Run it again to fix it.')
      return
    }
    editorComponent.replaceRange(range.from, range.to, res.sql)
    closeFix()
    toastSuccess('Query replaced. Undo restores the original.')
  }

  function handleCancel() {
    // Stopping the run marks its result cancelled: without an error the empty
    // result would read as "executed successfully".
    tabRuns.stop(tab.id)
  }

  // ── Result tabs ──
  function handleSelectResult(id: string) {
    setActiveTabResult(tab.id, id)
  }

  function handlePinResult(id: string) {
    if (toggleTabResultPin(tab.id, id) === 'limit') {
      toastInfo(`A tab keeps at most ${MAX_PINNED_PER_TAB} pinned results. Unpin one first.`)
    }
  }

  function handleCloseResult(id: string) {
    // Closing the result that is still streaming stops its query too.
    closeTabResult(tab.id, id)
  }

  // ── Full result export (server side) ──
  let exportJob = $state<{ format: FullExportFormat; bytes: number; controller: AbortController } | null>(null)
  const canExportFull = $derived(!!result && !!result.sql && !result.running && !result.error && result.meta.length > 0)

  async function handleExportFull(format: FullExportFormat) {
    const r = result
    if (!r || !r.sql || exportJob) return
    const controller = new AbortController()
    exportJob = { format, bytes: 0, controller }
    try {
      const { blob, filename } = await exportFullResult(r.sql, format, r.params, (bytes) => {
        if (exportJob?.controller === controller) exportJob = { ...exportJob, bytes }
      }, controller.signal)
      downloadBlob(blob, filename)
      toastSuccess(`Exported ${filename}`)
    } catch (e) {
      if (!controller.signal.aborted) toastError(errorMessage(e))
    } finally {
      if (exportJob?.controller === controller) exportJob = null
    }
  }

  function handleCancelExport() {
    exportJob?.controller.abort()
    exportJob = null
  }

  let formatting = false

  async function handleFormat() {
    if (formatting) return
    const sql = editorComponent?.getValue() ?? ''
    if (!sql.trim()) return
    formatting = true
    try {
      const res = await formatSQLDetailed(sql)
      // Replacing the whole document would drop anything typed while the
      // request was out; leave the newer text alone.
      if ((editorComponent?.getValue() ?? '') !== sql) {
        toastInfo('Query changed while formatting. Format again to apply.')
        return
      }
      if (res.formatted !== sql) {
        editorComponent?.setValue(res.formatted)
        if (res.comments_dropped) toastInfo('ClickHouse formatting removes comments. Undo (Cmd/Ctrl+Z) brings them back.')
      }
    } catch (e) {
      toastError(errorMessage(e))
    } finally {
      formatting = false
    }
  }

  async function handleExplain() {
    const picked = runnableWithParams()
    if (!picked) return

    // Explain takes over the active result like a run does, so a stream still
    // in flight is stopped and a newer run makes this response stale.
    // No sql: an EXPLAIN result has no statement to export or re-run as is.
    const resultId = newResult('', picked.from, picked.params, 'replace', taggedLabel('EXPLAIN', picked.from))
    const runId = claimRun(resultId)
    resetResultView(resultId)

    try {
      const res = await explainQuery(picked.sql, picked.params)
      if (!tabRuns.isCurrent(tab.id, runId)) return

      setTabResult(tab.id, resultId, {
        meta: res.meta ?? [],
        data: toPositionalRows(res.meta ?? [], res.data ?? []),
        stats: res.statistics ?? null,
        elapsedMs: res.elapsed_ms ?? 0,
        error: null,
        running: false,
      })
    } catch (e) {
      if (!tabRuns.isCurrent(tab.id, runId)) return
      setTabResult(tab.id, resultId, { error: errorMessage(e), running: false })
    }
  }

  async function handleLoadPlan() {
    const picked = runnableWithParams()
    if (!picked) return

    planLoading = true
    planError = null
    try {
      const res = await fetchQueryPlan(picked.sql, picked.params)
      planNodes = res.nodes ?? []
      planLines = res.lines ?? []
      planSource = res.source ?? ''
      if (!planNodes.length && !planLines.length) {
        planError = 'No plan rows returned by ClickHouse'
      }
    } catch (e) {
      planError = errorMessage(e)
    } finally {
      planLoading = false
    }
  }

  async function handleSample(perShard: number) {
    const picked = runnableWithParams()
    if (!picked) return

    const resultId = newResult('', picked.from, picked.params, 'replace', taggedLabel('SAMPLE', picked.from))
    const runId = claimRun(resultId)
    resetResultView(resultId)

    try {
      const res = await runSampleQuery({ query: picked.sql, per_shard: perShard, shard_by: '_shard_num', params: picked.params })
      if (!tabRuns.isCurrent(tab.id, runId)) return
      const rows = toPositionalRows(res.meta ?? [], res.data)
      setTabResult(tab.id, resultId, {
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
    } catch (e) {
      if (!tabRuns.isCurrent(tab.id, runId)) return
      setTabResult(tab.id, resultId, { error: errorMessage(e), running: false })
    }
  }

  // Default param values to persist with a saved query. An empty field keeps
  // the stored default instead of erasing it.
  function paramDefaultsFor(sql: string): Record<string, string> {
    return paramDefaultsToSave(detectQueryParams(sql).map((p) => p.name), paramValues, tab.savedParams)
  }

  function handleSaveClick() {
    if (saving || showSaveModal || saveConflict) return
    if (tab.savedQueryId) {
      void saveLinkedSavedQuery()
      return
    }
    saveName = tab.name
    saveDescription = ''
    saveFolder = ''
    saveVisibility = 'shared'
    showSaveModal = true
    // Folders are optional; a failure leaves the picker at the root.
    listSavedQueryFolders().then((f) => (saveFolders = f)).catch(() => (saveFolders = []))
  }

  // The stored saved query when a save found it changed since this tab
  // loaded it; the conflict dialog is open while set.
  let saveConflict = $state<SavedQuery | null>(null)

  /**
   * Saves the editor over its linked saved query, only if it is still the
   * version this tab loaded (or, from the conflict dialog, the version shown
   * there). A newer one opens the conflict dialog instead of overwriting it.
   */
  async function saveLinkedSavedQuery(overVersion?: number) {
    const sql = editorComponent?.getValue() ?? tab.sql
    if (!sql.trim() || !tab.savedQueryId || saving) return

    saving = true
    try {
      const parameters = paramDefaultsFor(sql)
      const updated = await apiPut<SavedQuery>(`/api/saved-queries/${tab.savedQueryId}`, linkedSaveBody(sql, parameters, overVersion ?? tab.savedVersion))
      markQueryTabSaved(tab.id, { baseSql: sql, savedParams: parameters, savedVersion: updated?.version })
      saveConflict = null
      toastSuccess('Saved query updated')
    } catch (e) {
      const current = savedQueryConflict(e)
      if (current) saveConflict = current
      else toastError(errorMessage(e))
    } finally {
      saving = false
    }
  }

  function openNewerVersion() {
    const current = saveConflict
    if (!current) return
    saveConflict = null
    openSavedQueryTab({ ...current, name: `${current.name} (latest)` }, undefined, { newTab: true })
  }

  async function handleSaveConfirm() {
    // Enter in the form and the Save button can both fire before the first
    // request returns; one saved query per confirm.
    if (saving) return
    const sql = editorComponent?.getValue() ?? tab.sql
    if (!sql.trim() || !saveName.trim()) return

    saving = true
    try {
      const parameters = paramDefaultsFor(sql)
      const created = await apiPost<SavedQuery>('/api/saved-queries', {
        name: saveName.trim(),
        description: saveDescription.trim(),
        query: sql,
        parameters,
        folder_id: saveFolder,
        visibility: saveVisibility,
      })
      if (created?.id) {
        markQueryTabSaved(tab.id, {
          savedQueryId: created.id,
          savedVersion: created.version,
          name: created.name,
          baseSql: sql,
          savedParams: parameters,
        })
      } else {
        markQueryTabSaved(tab.id, { baseSql: sql, savedParams: parameters })
      }
      toastSuccess('Query saved')
      showSaveModal = false
    } catch (e) {
      toastError(errorMessage(e))
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
    userSized = true
    document.removeEventListener('mousemove', onDragMove)
    document.removeEventListener('mouseup', onDragEnd)
    localStorage.setItem('ch-ui-split-percent', String(splitPercent))
  }

  const runShortcut = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform) ? '⌘↵' : 'Ctrl+↵'
  const EDITOR_LINE_PX = 20
  const editorPaneStyle = $derived.by(() => {
    if (userSized) return `height: ${splitPercent}%`
    const lines = Math.max(4, currentSql.split('\n').length)
    const chrome = 40 + 16 + (showAsk ? 48 : 0) // toolbar, editor padding, Ask AI bar
    return `height: ${lines * EDITOR_LINE_PX + chrome}px; max-height: 55%`
  })
</script>

<div class="flex flex-col h-full overflow-hidden" bind:this={containerEl}>
  <!-- Editor pane -->
  <div class="flex flex-col min-h-[80px] overflow-hidden border-b border-edge-subtle" style={editorPaneStyle}>
    <Toolbar
      {running}
      onrun={() => handleRun()}
      oncancel={handleCancel}
      onformat={handleFormat}
      onexplain={handleExplain}
      onask={() => (showAsk = !showAsk)}
      askActive={showAsk}
      askPro={proActive}
      onsave={handleSaveClick}
      onhistory={() => (showHistorySheet = true)}
      onparams={() => (showParamsPanel ? (showParamsPanel = false) : openParams())}
      paramCount={detectedParams.length}
      paramsActive={showParamsPanel}
      bind:paramsEl={paramsButtonEl}
      {estimate}
      {estimateLoading}
    />

    <!-- Ask AI (text-to-SQL) bar -->
    {#if showAsk}
      <div class="shrink-0 border-b border-edge-subtle bg-ch-orange/5">
        {#if proActive}
          <AskAIBar bind:question={askQuestion} getSql={askContext} oninsert={insertGeneratedSQL} onclose={() => (showAsk = false)} />
        {:else}
          <!-- Pro upsell -->
          <div class="flex items-center justify-between gap-3 px-3 py-2">
            <div class="flex items-center gap-2 text-xs text-fg-2">
              <Lock size={14} class="text-accent shrink-0" />
              <span>
                <strong>Ask AI</strong> turns a plain-English question into a ClickHouse query grounded in your schema.
                It's a <strong>Pro</strong> feature.
              </span>
            </div>
            <div class="flex items-center gap-2 shrink-0">
              <Button size="sm" onclick={() => goTo('settings', 'License')}>Upgrade</Button>
              <Button icon variant="ghost" size="sm" onclick={() => (showAsk = false)} title="Close" aria-label="Close Ask AI">
                <X size={14} />
              </Button>
            </div>
          </div>
        {/if}
      </div>
    {/if}

    <div class="flex-1 min-h-0">
      <SqlEditor
        bind:this={editorComponent}
        value={tab.sql}
        onrun={handleRun}
        onrunall={(statements) => void handleRunAll(statements)}
        onchange={handleSQLChange}
        onsave={handleSaveClick}
        onformat={handleFormat}
        oncancel={handleCancel}
        {running}
        placeholder="Write SQL here. {runShortcut} runs the statement under the cursor, Save keeps it for later."
      />
    </div>
  </div>

  <!-- Query parameters: popover anchored to the toolbar button, never reflows the page -->
  {#if showParamsPanel && detectedParams.length > 0}
    <ParamsPopover
      params={detectedParams}
      bind:values={paramValues}
      {focusParam}
      x={paramsAnchor.x}
      y={paramsAnchor.y}
      onclose={() => (showParamsPanel = false)}
      onrun={() => { showParamsPanel = false; void handleRun() }}
    />
  {/if}

  <!-- Drag handle -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="h-1 shrink-0 cursor-row-resize group flex items-center justify-center hover:bg-ch-orange/20 transition-colors {dragging ? 'bg-ch-orange/30' : 'bg-surface-2'}"
    onmousedown={onDragStart}
  >
    <div class="w-8 h-0.5 rounded-full {dragging ? 'bg-ch-orange' : 'bg-fg-4 group-hover:bg-accent/60'} transition-colors"></div>
  </div>

  <!-- Results pane -->
  <div class="flex-1 min-h-[80px] min-h-0 overflow-hidden flex flex-col">
    {#if tabResults.results.length > 0}
      <ResultTabs
        results={tabResults.results}
        activeIndex={tabResults.activeResult}
        onselect={handleSelectResult}
        onpin={handlePinResult}
        onclose={handleCloseResult}
      />
    {/if}
    {#if result?.rowsDropped && !result.running}
      <div class="flex items-center gap-1.5 px-3 py-1 border-b border-edge-subtle bg-surface text-xs text-warning shrink-0">
        Pinned result restored with {formatNumber(result.data.length)} of {formatNumber(result.savedRowCount ?? 0)} rows. Re-run to reload.
      </div>
    {/if}
    {#key result?.id}
    <ResultPanel
      meta={result?.meta ?? []}
      data={displayData}
      loading={result?.running ?? false}
      executed={result !== undefined}
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
      onfixwithai={fixAvailable && !result?.running ? () => void handleFixWithAI() : undefined}
      streamRows={showsLastRun ? streamRows : 0}
      streamChunks={showsLastRun ? streamChunks : 0}
      streamStartedAt={showsLastRun ? streamStartedAt : null}
      streamLastChunkAt={showsLastRun ? streamLastChunkAt : null}
      progress={showsLastRun ? streamProgress : null}
      planNodes={planNodes}
      planLines={planLines}
      planSource={planSource}
      planLoading={planLoading}
      planError={planError}
      onLoadPlan={handleLoadPlan}
      onSample={handleSample}
      profile={showsLastRun ? profileState.profile : null}
      profileAvailable={showsLastRun && profileState.available}
      profileReason={showsLastRun ? profileState.reason : null}
      profileLoading={showsLastRun && profileState.loading}
      profileError={showsLastRun ? profileState.error : null}
      samplingMode={showsLastRun ? samplingMode : null}
      {estimate}
      onexportfull={canExportFull ? (format) => void handleExportFull(format) : undefined}
      exportProgress={exportJob ? { format: exportJob.format, bytes: exportJob.bytes } : null}
      oncancelexport={handleCancelExport}
      sql={result?.sql ?? ''}
      connectionId={getSession()?.connectionId ?? ''}
      chartKey={tab.id}
    />
    {/key}
  </div>
</div>

<!-- Drag overlay to prevent iframe/editor stealing mouse events -->
{#if dragging}
  <div class="fixed inset-0 z-50 cursor-row-resize"></div>
{/if}

<!-- Fix with AI -->
<Sheet
  open={fixOpen}
  title="Fix with AI"
  description="A minimal fix for the failed query. Review the change before replacing it."
  size="lg"
  onclose={closeFix}
>
  <div class="space-y-4 text-[13px]">
    {#if fixLoading}
      <div class="flex items-center gap-2 text-fg-3">
        <Spinner size="sm" />
        <span aria-live="polite">{fixPhase || 'Writing a fix'}</span>
      </div>
      {#if fixPreview}
        <pre class="max-h-64 overflow-auto rounded-md bg-surface-2 px-3 py-2 font-mono text-xs text-fg-2 whitespace-pre-wrap">{fixPreview}</pre>
      {/if}
    {:else if fixError}
      <p class="text-danger">{fixError}</p>
    {:else if fixResult && fixOriginal}
      <div class="flex flex-wrap items-center gap-2">
        {#if fixBadge}
          <Badge tone={fixBadge.tone} title={fixBadge.title}>{fixBadge.label}</Badge>
        {/if}
        {#if fixResult.validation && fixResult.validation.rounds > 0}
          <span class="text-xs text-fg-3">{fixResult.validation.rounds} repair{fixResult.validation.rounds === 1 ? '' : 's'} after ClickHouse rejected it</span>
        {/if}
      </div>
      {#if fixResult.explanation}
        <p class="text-fg">{fixResult.explanation}</p>
      {/if}
      {#if fixResult.validation?.status === 'failed' && fixResult.validation.error}
        <p class="text-xs text-danger whitespace-pre-wrap break-words">{fixResult.validation.error}</p>
      {/if}
      <div class="overflow-hidden rounded-lg border border-edge-subtle bg-surface">
        <SqlDiff before={fixOriginal.sql} after={fixResult.sql} beforeLabel="Failed query" afterLabel="Fix" />
      </div>
    {/if}
  </div>
  {#snippet footer()}
    <Button size="sm" variant="ghost" onclick={closeFix}>Cancel</Button>
    <Button size="sm" disabled={!fixResult || fixLoading} onclick={acceptFix}>Replace query</Button>
  {/snippet}
</Sheet>

<!-- Query history drawer -->
<Sheet open={showHistorySheet} title="Query History" size="md" onclose={() => (showHistorySheet = false)}>
  <QueryHistoryPanel onopen={handleHistoryOpen} oninsert={handleHistoryInsert} />
</Sheet>

<!-- Save conflict: the linked saved query changed since this tab loaded it -->
<Modal
  open={saveConflict !== null}
  title="This saved query changed"
  size="md"
  onclose={() => (saveConflict = null)}
>
  {#if saveConflict}
    <p class="text-[13px] leading-relaxed text-fg-2">
      <strong class="text-fg">{saveConflict.name}</strong> was saved or restored as version {saveConflict.version} since you opened it.
      Overwrite replaces it with your edits; version {saveConflict.version} stays in its history.
    </p>
  {/if}
  {#snippet footer()}
    <Button size="sm" variant="outline" onclick={() => (saveConflict = null)} disabled={saving}>Cancel</Button>
    <Button size="sm" variant="outline" onclick={openNewerVersion} disabled={saving}>Open the newer version in a new tab</Button>
    <Button size="sm" loading={saving} onclick={() => saveConflict && void saveLinkedSavedQuery(saveConflict.version)}>Overwrite</Button>
  {/snippet}
</Modal>

<!-- Save query -->
<Sheet
  open={showSaveModal}
  title="Save query"
  size="sm"
  onclose={() => (showSaveModal = false)}
>
  <form
    class="space-y-4"
    onsubmit={(e) => { e.preventDefault(); void handleSaveConfirm() }}
  >
    <FormField label="Name" for="save-query-name" required controlWidth="full">
      <Input id="save-query-name" bind:value={saveName} autocomplete="off" />
    </FormField>
    <FormField label="Description" for="save-query-description" controlWidth="full" hint="Optional.">
      <Input id="save-query-description" bind:value={saveDescription} autocomplete="off" />
    </FormField>
    <FormField label="Folder" for="save-query-folder" controlWidth="full">
      <Select id="save-query-folder" options={savedQueryFolderOptions(saveFolders)} bind:value={saveFolder} />
    </FormField>
    <FormField label="Visible to" for="save-query-visibility" controlWidth="full" hint="Everyone can see and run shared queries. Change it later from Saved Queries.">
      <Select id="save-query-visibility" options={visibilityOptions(saveIsAdmin)} bind:value={saveVisibility} />
    </FormField>
    <button type="submit" class="hidden" aria-hidden="true"></button>
  </form>
  {#snippet footer()}
    <Button size="sm" variant="ghost" onclick={() => (showSaveModal = false)}>Cancel</Button>
    <Button size="sm" loading={saving} disabled={!saveName.trim()} onclick={() => void handleSaveConfirm()}>Save</Button>
  {/snippet}
</Sheet>
