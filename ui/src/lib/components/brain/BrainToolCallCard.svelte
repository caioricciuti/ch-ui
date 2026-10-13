<script lang="ts">
  import { Check, X, Ban, HelpCircle } from 'lucide-svelte'
  import type { ToolCall } from '../../types/brain'
  import BrainApprovalActions from './BrainApprovalActions.svelte'
  import Spinner from '../common/Spinner.svelte'

  interface Props {
    toolCall: ToolCall
  }

  let { toolCall }: Props = $props()
  let expanded = $state(false)
  let localDecision = $state<'approved' | 'declined' | null>(null)

  const summary = $derived(formatArgs(toolCall.args))
  const resultPreview = $derived(formatResult(toolCall.result))

  function formatArgs(args: unknown): string {
    if (args == null) return ''
    if (typeof args === 'string') {
      try {
        const parsed = JSON.parse(args)
        return formatArgs(parsed)
      } catch {
        return args.length > 80 ? args.slice(0, 80) + '…' : args
      }
    }
    if (typeof args === 'object') {
      const obj = args as Record<string, unknown>
      const parts: string[] = []
      for (const k of Object.keys(obj)) {
        const v = obj[k]
        if (v == null || v === '') continue
        const vs = typeof v === 'string' ? v : JSON.stringify(v)
        parts.push(`${k}=${vs.length > 40 ? vs.slice(0, 40) + '…' : vs}`)
      }
      return parts.join(' · ')
    }
    return String(args)
  }

  /** Every argument with its full value, for the approval review. */
  function argEntries(args: unknown): [string, string][] {
    let value: unknown = args
    if (typeof value === 'string') {
      try {
        value = JSON.parse(value)
      } catch {
        return [['arguments', value as string]]
      }
    }
    if (value == null || typeof value !== 'object') return []
    const out: [string, string][] = []
    for (const [k, v] of Object.entries(value as Record<string, unknown>)) {
      if (v == null || v === '') continue
      out.push([k, typeof v === 'string' ? v : JSON.stringify(v, null, 2)])
    }
    return out
  }

  function formatResult(result: unknown): string {
    if (result == null) return ''
    try {
      return JSON.stringify(result, null, 2)
    } catch {
      return String(result)
    }
  }

  const toolLabel: Record<string, string> = {
    list_tables: 'List tables',
    describe_table: 'Describe table',
    run_query: 'Run query',
    get_insights: 'Profile table',
    list_dashboards: 'List dashboards',
    get_dashboard: 'Get dashboard',
    list_saved_queries: 'List saved queries',
    list_models: 'List models',
    list_pipelines: 'List pipelines',
    get_pipeline_graph: 'Get pipeline graph',
    create_saved_query: 'Save query',
    create_model: 'Create model',
    create_dashboard: 'Create dashboard',
    create_pipeline: 'Create pipeline',
    configure_pipeline: 'Configure pipeline',
    start_pipeline: 'Start pipeline',
    add_dashboard_panel: 'Add panel',
    update_saved_query: 'Update query',
    update_model: 'Update model',
    delete_dashboard: 'Delete dashboard',
    delete_dashboard_panel: 'Delete panel',
    delete_model: 'Delete model',
    delete_saved_query: 'Delete query',
    delete_pipeline: 'Delete pipeline',
    schedule_model: 'Schedule model',
    run_model: 'Run model',
    build_model: 'Build model',
    list_services: 'List services',
    query_logs: 'Query logs',
    query_traces: 'Query traces',
    find_trace: 'Find trace',
    list_metrics: 'List metrics',
    query_metrics: 'Query metrics',
    analyze_query_log: 'Analyze query log',
    query_pattern: 'Query pattern',
    explain_query: 'Explain query',
    table_storage: 'Table storage',
    merges_and_mutations: 'Merges and mutations',
    compare_query_estimates: 'Compare estimates',
  }
  const label = $derived(toolLabel[toolCall.tool] ?? toolCall.tool)
  const isApproval = $derived(toolCall.status === 'pending_approval' && !localDecision)
  const approvalArgs = $derived(isApproval ? argEntries(toolCall.args) : [])
  const isDestructive = $derived(toolCall.tool?.startsWith('delete_') ?? false)
  const isWrite = $derived(
    toolCall.tool?.startsWith('create_') ||
    toolCall.tool?.startsWith('update_') ||
    toolCall.tool?.startsWith('delete_') ||
    toolCall.tool === 'add_dashboard_panel' ||
    toolCall.tool === 'configure_pipeline' ||
    toolCall.tool === 'start_pipeline' ||
    toolCall.tool === 'schedule_model' ||
    toolCall.tool === 'run_model' ||
    toolCall.tool === 'build_model'
  )

  const openUrl = $derived.by(() => {
    const r = toolCall.result as Record<string, unknown> | undefined
    if (!r) return null
    const v = r.open_url
    return typeof v === 'string' ? v : null
  })

  const queryRowSummary = $derived.by(() => {
    if (toolCall.tool !== 'run_query') return null
    const r = toolCall.result as Record<string, unknown> | undefined
    if (!r) return null
    const data = r.data as unknown[][] | undefined
    const meta = r.meta as unknown[] | undefined
    if (!Array.isArray(data) || !Array.isArray(meta)) return null
    return `${data.length} row${data.length === 1 ? '' : 's'} · ${meta.length} col${meta.length === 1 ? '' : 's'}`
  })

  // explain_query: the plan text, shown as is in a monospace block.
  const explainText = $derived.by(() => {
    if (toolCall.tool !== 'explain_query') return null
    const r = toolCall.result as Record<string, unknown> | undefined
    return r && typeof r.text === 'string' && r.text ? r.text : null
  })

  // compare_query_estimates: before and after numbers in a small table.
  interface EstimateNumbers {
    rows: number
    marks: number
    parts: number
    granules_selected: number
    granules_known: boolean
  }
  function isEstimate(v: unknown): v is EstimateNumbers {
    if (v == null || typeof v !== 'object') return false
    const o = v as Record<string, unknown>
    return typeof o.rows === 'number' && typeof o.marks === 'number' && typeof o.parts === 'number'
  }
  const compareRows = $derived.by(() => {
    if (toolCall.tool !== 'compare_query_estimates') return null
    const r = toolCall.result as Record<string, unknown> | undefined
    if (!r || !isEstimate(r.before) || !isEstimate(r.after)) return null
    const b = r.before
    const a = r.after
    const rows: { label: string; before: number; after: number }[] = [
      { label: 'Rows', before: b.rows, after: a.rows },
      { label: 'Marks', before: b.marks, after: a.marks },
      { label: 'Parts', before: b.parts, after: a.parts },
    ]
    if (b.granules_known && a.granules_known) rows.push({ label: 'Granules', before: b.granules_selected, after: a.granules_selected })
    return rows
  })
  function deltaLabel(before: number, after: number): string {
    if (before === after) return 'same'
    if (before === 0) return after > 0 ? `+${after.toLocaleString('en-US')}` : after.toLocaleString('en-US')
    const pct = ((after - before) / before) * 100
    return `${pct > 0 ? '+' : ''}${pct.toFixed(1)}%`
  }

  const effectiveStatus = $derived(localDecision === 'approved' ? 'pending' : localDecision === 'declined' ? 'declined' : toolCall.status)
  const compact = $derived(
    !expanded &&
    effectiveStatus !== 'pending' &&
    effectiveStatus !== 'pending_approval' &&
    effectiveStatus !== 'running'
  )

  /** Any lucide-svelte icon; they all share this component type. */
  type IconComponent = typeof Check
  function statusIcon(s?: string): IconComponent {
    switch (s) {
      case 'pending_approval': return HelpCircle
      case 'error': return X
      case 'declined': return Ban
      case 'success': default: return Check
    }
  }
  function statusClass(s?: string): string {
    switch (s) {
      case 'pending_approval': return 'text-warning'
      case 'error': return 'text-danger'
      case 'declined': return 'text-muted-foreground'
      case 'success': default: return 'text-success'
    }
  }
</script>

<div
  class="w-full {compact ? 'mb-1' : 'mb-2'} rounded-lg border bg-card/60 text-xs overflow-hidden {isApproval ? 'border-warning/40 bg-warning-soft' : 'border-edge-subtle'}"
>
  <button
    type="button"
    onclick={() => (expanded = !expanded)}
    class="w-full text-left flex items-center gap-2 {compact ? 'px-2 py-1' : 'px-3 py-2'} hover:bg-card transition-colors"
  >
    <span class="flex-shrink-0 inline-flex items-center justify-center w-4">
      {#if effectiveStatus === 'pending' || effectiveStatus === 'running'}
        <Spinner size="sm" />
      {:else}
        {@const StatusIcon = statusIcon(effectiveStatus)}
        <span class="inline-flex {statusClass(effectiveStatus)}" title={effectiveStatus ?? 'success'}><StatusIcon size={13} /></span>
      {/if}
    </span>
    <span class="font-mono font-medium text-foreground">{label}</span>
    {#if queryRowSummary}
      <span class="text-muted-foreground truncate">{queryRowSummary}</span>
    {:else if summary}
      <span class="text-muted-foreground truncate">{summary}</span>
    {/if}
    {#if isApproval}
      <span class="ml-auto text-[10px] uppercase tracking-wide text-warning font-semibold">Needs approval</span>
    {:else if localDecision === 'approved' && effectiveStatus === 'pending'}
      <span class="ml-auto text-[10px] uppercase tracking-wide text-accent font-semibold">Running…</span>
    {:else if localDecision === 'declined' && effectiveStatus === 'declined'}
      <span class="ml-auto text-[10px] uppercase tracking-wide text-muted-foreground font-semibold">Declined</span>
    {:else}
      <span class="ml-auto text-muted-foreground/60 text-[10px]">
        {expanded ? 'collapse' : 'details'}
      </span>
    {/if}
  </button>

  {#if isApproval}
    <div class="border-t border-warning/30 px-3 py-2 space-y-2">
      <div class="text-xs text-fg-3">
        Tool <span class="font-mono text-fg">{toolCall.tool}</span> will run with:
      </div>
      {#if approvalArgs.length === 0}
        <div class="text-xs text-fg-3">No arguments.</div>
      {/if}
      {#each approvalArgs as [key, value] (key)}
        <div>
          <div class="text-xs font-mono text-fg-3">{key}</div>
          <pre class="mt-0.5 rounded bg-surface-2 px-2 py-1 text-xs font-mono text-fg whitespace-pre-wrap break-words max-h-64 overflow-auto">{value}</pre>
        </div>
      {/each}
    </div>
    <BrainApprovalActions approvalId={toolCall.approvalId} {label} destructive={isDestructive} bind:decision={localDecision} />
  {/if}

  {#if openUrl && effectiveStatus === 'success' && isWrite}
    <div class="border-t border-border px-3 py-2 bg-background/50">
      <a href={openUrl} class="text-[11px] text-accent hover:underline">Open →</a>
    </div>
  {/if}

  {#if expanded}
    <div class="border-t border-border bg-background/50 px-3 py-2 space-y-2">
      {#if explainText}
        <div>
          <div class="text-[10px] uppercase tracking-wide text-muted-foreground mb-1">EXPLAIN</div>
          <pre class="text-[11px] font-mono whitespace-pre overflow-auto max-h-80 rounded bg-surface-2 px-2 py-1 text-fg">{explainText}</pre>
        </div>
      {/if}
      {#if compareRows}
        <div>
          <div class="text-[10px] uppercase tracking-wide text-muted-foreground mb-1">Estimated data read</div>
          <div class="overflow-x-auto">
            <table class="w-full border-collapse text-[11px]">
              <thead>
                <tr class="text-left text-fg-3">
                  <th class="border-b border-edge-subtle py-1 pr-3 font-medium"></th>
                  <th class="border-b border-edge-subtle py-1 pr-3 text-right font-medium">Before</th>
                  <th class="border-b border-edge-subtle py-1 pr-3 text-right font-medium">After</th>
                  <th class="border-b border-edge-subtle py-1 text-right font-medium">Change</th>
                </tr>
              </thead>
              <tbody>
                {#each compareRows as row (row.label)}
                  <tr>
                    <td class="py-0.5 pr-3 text-fg-2">{row.label}</td>
                    <td class="py-0.5 pr-3 text-right font-mono tabular-nums text-fg">{row.before.toLocaleString('en-US')}</td>
                    <td class="py-0.5 pr-3 text-right font-mono tabular-nums text-fg">{row.after.toLocaleString('en-US')}</td>
                    <td class="py-0.5 text-right font-mono tabular-nums {row.after < row.before ? 'text-success' : row.after > row.before ? 'text-danger' : 'text-fg-3'}">{deltaLabel(row.before, row.after)}</td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
          <p class="mt-1 text-[11px] text-fg-3">Estimates of data read, not latency.</p>
        </div>
      {/if}
      {#if toolCall.args != null}
        <div>
          <div class="text-[10px] uppercase tracking-wide text-muted-foreground mb-1">Args</div>
          <pre class="text-[11px] font-mono whitespace-pre-wrap break-all">{formatResult(toolCall.args)}</pre>
        </div>
      {/if}
      {#if resultPreview}
        <div>
          <div class="text-[10px] uppercase tracking-wide text-muted-foreground mb-1">Result</div>
          <pre class="text-[11px] font-mono whitespace-pre-wrap break-all max-h-64 overflow-auto">{resultPreview}</pre>
        </div>
      {/if}
    </div>
  {/if}
</div>
