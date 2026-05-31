<script lang="ts">
  import type { BrainArtifact, BrainMessage as BrainMessageType } from '../../types/brain'
  import { parseMessageSegments } from './brain-markdown'
  import BrainSqlBlock from './BrainSqlBlock.svelte'
  import BrainArtifactCard from './BrainArtifactCard.svelte'
  import BrainToolCallCard from './BrainToolCallCard.svelte'
  import Spinner from '../common/Spinner.svelte'

  interface Props {
    message: BrainMessageType
    artifacts: BrainArtifact[]
    streaming: boolean
    isLastMessage: boolean
    runningSql: string | null
    onRunSql: (sql: string, messageId?: string) => void
    onOpenInEditor: (sql: string) => void
  }

  let { message, artifacts, streaming, isLastMessage, runningSql, onRunSql, onOpenInEditor }: Props = $props()

  const segments = $derived(
    message.role === 'assistant' && message.content
      ? parseMessageSegments(message.content)
      : []
  )

  const PHASE_LABELS: Record<string, string> = {
    list_tables: 'Exploring schema…',
    describe_table: 'Reading table…',
    run_query: 'Running query…',
    get_insights: 'Profiling table…',
    list_dashboards: 'Reviewing dashboards…',
    get_dashboard: 'Reading dashboard…',
    list_saved_queries: 'Reviewing saved queries…',
    list_models: 'Reviewing models…',
    list_pipelines: 'Reviewing pipelines…',
    get_pipeline_graph: 'Inspecting pipeline…',
    create_saved_query: 'Awaiting approval…',
    create_model: 'Awaiting approval…',
    create_dashboard: 'Awaiting approval…',
    create_pipeline: 'Awaiting approval…',
    configure_pipeline: 'Wiring pipeline…',
    start_pipeline: 'Starting pipeline…',
    add_dashboard_panel: 'Building panel…',
    update_saved_query: 'Updating query…',
    update_model: 'Updating model…',
    delete_dashboard: 'Deleting dashboard…',
    delete_dashboard_panel: 'Deleting panel…',
    delete_model: 'Deleting model…',
    delete_saved_query: 'Deleting query…',
    delete_pipeline: 'Deleting pipeline…',
    schedule_model: 'Scheduling…',
    run_model: 'Materializing model…',
    build_model: 'Building model…',
    list_services: 'Discovering services…',
    query_logs: 'Searching logs…',
    query_traces: 'Searching traces…',
    find_trace: 'Loading trace…',
    list_metrics: 'Listing metrics…',
    query_metrics: 'Querying metric…',
  }
  const phaseLabel = $derived.by(() => {
    if (!streaming || !isLastMessage || !message.toolCalls?.length) return null
    for (let i = message.toolCalls.length - 1; i >= 0; i--) {
      const tc = message.toolCalls[i]
      if (tc.status === 'pending' || tc.status === 'pending_approval' || tc.status === 'running') {
        return PHASE_LABELS[tc.tool] ?? 'Working…'
      }
    }
    return null
  })

  function findArtifactForSql(sql: string): BrainArtifact | undefined {
    return artifacts.find(art => {
      if (art.type !== 'query_result') return false
      try {
        const payload = JSON.parse(art.content)
        return payload?.query?.trim() === sql.trim()
      } catch {
        return false
      }
    })
  }

  const orphanArtifacts = $derived.by(() => {
    if (message.role !== 'assistant') return []
    const sqlTexts = new Set(
      segments.filter(s => s.type === 'sql').map(s => s.content.trim())
    )
    return artifacts.filter(art => {
      if (art.type !== 'query_result') return true
      try {
        const payload = JSON.parse(art.content)
        return !sqlTexts.has(payload?.query?.trim())
      } catch {
        return true
      }
    })
  })
</script>

{#if message.role === 'user'}
  <div class="flex justify-end">
    <div class="max-w-[80%] bg-ch-blue text-white rounded-2xl rounded-br-sm px-4 py-2.5 text-sm whitespace-pre-wrap">{message.content}</div>
  </div>
{:else}
  <div class="flex justify-start">
    <div class="max-w-[85%] min-w-0">
      {#if phaseLabel}
        <div class="inline-flex items-center gap-2 mb-2 px-2 py-1 rounded-full bg-ch-blue/10 text-ch-blue text-[11px]">
          <span class="inline-block w-1.5 h-1.5 rounded-full bg-ch-blue animate-pulse"></span>
          {phaseLabel}
        </div>
      {/if}
      {#if message.toolCalls && message.toolCalls.length > 0}
        {#each message.toolCalls as tc (tc.id)}
          <BrainToolCallCard toolCall={tc} />
        {/each}
      {/if}

      {#if message.content}
        {#each segments as seg}
          {#if seg.type === 'markdown'}
            <div class="prose-brain text-sm text-gray-800 dark:text-gray-200">{@html seg.html}</div>
          {:else if seg.type === 'sql'}
            <BrainSqlBlock
              sql={seg.content}
              messageId={message.id}
              artifact={findArtifactForSql(seg.content)}
              running={runningSql === seg.content}
              onRun={onRunSql}
              onOpenInEditor={onOpenInEditor}
            />
          {/if}
        {/each}

        {#if orphanArtifacts.length > 0}
          {#each orphanArtifacts as art}
            <BrainArtifactCard artifact={art} />
          {/each}
        {/if}
      {:else if streaming && isLastMessage && (!message.toolCalls || message.toolCalls.length === 0)}
        <div class="bg-gray-100 dark:bg-gray-800 rounded-2xl rounded-bl-sm px-4 py-3">
          <Spinner size="sm" />
        </div>
      {/if}
    </div>
  </div>
{/if}
