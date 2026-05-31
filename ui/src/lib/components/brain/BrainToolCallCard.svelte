<script lang="ts">
  import type { ToolCall } from '../../types/brain'
  import { approveBrainToolCall, declineBrainToolCall } from '../../api/brain'
  import Spinner from '../common/Spinner.svelte'

  interface Props {
    toolCall: ToolCall
  }

  let { toolCall }: Props = $props()
  let expanded = $state(false)
  let deciding = $state(false)
  let localDecision = $state<'approved' | 'declined' | null>(null)
  const compact = $derived(
    !expanded &&
    effectiveStatus !== 'pending' &&
    effectiveStatus !== 'pending_approval' &&
    effectiveStatus !== 'running'
  )

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
  }
  const label = $derived(toolLabel[toolCall.tool] ?? toolCall.tool)
  const isApproval = $derived(toolCall.status === 'pending_approval' && !localDecision)
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

  async function onApprove() {
    if (!toolCall.approvalId || deciding) return
    deciding = true
    try {
      await approveBrainToolCall(toolCall.approvalId)
      localDecision = 'approved'
    } catch (e) {
      console.error('approve failed', e)
    } finally {
      deciding = false
    }
  }

  async function onDecline() {
    if (!toolCall.approvalId || deciding) return
    deciding = true
    try {
      await declineBrainToolCall(toolCall.approvalId)
      localDecision = 'declined'
    } catch (e) {
      console.error('decline failed', e)
    } finally {
      deciding = false
    }
  }

  const effectiveStatus = $derived(localDecision === 'approved' ? 'pending' : localDecision === 'declined' ? 'declined' : toolCall.status)

  function statusIcon(s?: string): string {
    switch (s) {
      case 'pending_approval': return '?'
      case 'pending': case 'running': return ''
      case 'error': return '✗'
      case 'declined': return '⊘'
      case 'success': default: return '✓'
    }
  }
  function statusClass(s?: string): string {
    switch (s) {
      case 'pending_approval': return 'text-amber-500'
      case 'error': return 'text-red-500'
      case 'declined': return 'text-muted-foreground'
      case 'success': default: return 'text-green-500'
    }
  }
</script>

<div
  class="w-full {compact ? 'mb-1' : 'mb-2'} rounded-lg border bg-card/60 text-xs overflow-hidden {isApproval ? 'border-amber-500/40 bg-amber-500/5' : 'border-border'}"
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
        <span class={statusClass(effectiveStatus)}>{statusIcon(effectiveStatus)}</span>
      {/if}
    </span>
    <span class="font-mono font-medium text-foreground">{label}</span>
    {#if queryRowSummary}
      <span class="text-muted-foreground truncate">{queryRowSummary}</span>
    {:else if summary}
      <span class="text-muted-foreground truncate">{summary}</span>
    {/if}
    {#if isApproval}
      <span class="ml-auto text-[10px] uppercase tracking-wide text-amber-600 font-semibold">Needs approval</span>
    {:else if localDecision === 'approved' && effectiveStatus === 'pending'}
      <span class="ml-auto text-[10px] uppercase tracking-wide text-ch-blue font-semibold">Running…</span>
    {:else if localDecision === 'declined' && effectiveStatus === 'declined'}
      <span class="ml-auto text-[10px] uppercase tracking-wide text-muted-foreground font-semibold">Declined</span>
    {:else}
      <span class="ml-auto text-muted-foreground/60 text-[10px]">
        {expanded ? 'collapse' : 'details'}
      </span>
    {/if}
  </button>

  {#if isApproval}
    <div class="border-t border-amber-500/30 px-3 py-2 flex flex-wrap items-center gap-2">
      <p class="text-[11px] text-muted-foreground flex-1 min-w-0">
        Brain wants to <strong class="text-foreground">{label.toLowerCase()}</strong>. Review the details and approve to run.
      </p>
      <button
        type="button"
        onclick={onDecline}
        disabled={deciding}
        class="px-3 py-1 rounded-md text-[11px] font-medium border border-border bg-background hover:bg-secondary disabled:opacity-50"
      >Decline</button>
      <button
        type="button"
        onclick={onApprove}
        disabled={deciding}
        class="px-3 py-1 rounded-md text-[11px] font-medium bg-amber-500 hover:bg-amber-600 text-white disabled:opacity-50"
      >Approve</button>
    </div>
  {/if}

  {#if openUrl && effectiveStatus === 'success' && isWrite}
    <div class="border-t border-border px-3 py-2 bg-background/50">
      <a href={openUrl} class="text-[11px] text-ch-blue hover:underline">Open →</a>
    </div>
  {/if}

  {#if expanded}
    <div class="border-t border-border bg-background/50 px-3 py-2 space-y-2">
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
