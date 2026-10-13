<script lang="ts">
  import { onMount } from 'svelte'
  import type { CompileResult, PlanStep } from '../../types/models'
  import * as api from '../../api/models'
  import Button from '../common/Button.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import { AlertCircle } from 'lucide-svelte'

  interface Props {
    modelId: string
    /** The editor's current SQL, so unsaved edits compile too. */
    getSql: () => string
  }

  let { modelId, getSql }: Props = $props()

  let result = $state<CompileResult | null>(null)
  let compiling = $state(false)
  let requestError = $state('')

  const WHEN_ORDER: PlanStep['when'][] = ['first build', 'rebuild', 'incremental run', 'full refresh', 'every run']

  const groups = $derived.by(() => {
    const steps = result?.statements ?? []
    return WHEN_ORDER
      .map((when) => ({ when, sql: steps.filter((s) => s.when === when).map((s) => s.sql) }))
      .filter((g) => g.sql.length > 0)
  })

  // The tab mounts this panel, so opening the tab compiles.
  onMount(() => {
    compile()
  })

  async function compile() {
    compiling = true
    requestError = ''
    try {
      result = await api.compileModel(modelId, getSql())
    } catch (e: unknown) {
      requestError = (e instanceof Error ? e.message : '') || 'Failed to compile model'
    } finally {
      compiling = false
    }
  }
</script>

<div class="flex h-full min-h-0 flex-col">
  <div class="flex h-10 shrink-0 items-center gap-2 border-b border-edge-subtle px-3 text-xs text-fg-3">
    <span class="flex-1">The SQL a run sends, with $ref and $this resolved. Uses the editor's SQL, saved or not.</span>
    <Button size="xs" variant="outline" onclick={compile} loading={compiling}>Compile</Button>
  </div>
  <div class="min-h-0 flex-1 space-y-3 overflow-auto p-3">
    {#if requestError || result?.error}
      <div class="flex items-start gap-2 rounded-md bg-danger-soft px-3 py-2">
        <AlertCircle size={14} class="mt-0.5 shrink-0 text-danger" />
        <pre class="flex-1 whitespace-pre-wrap break-all text-xs text-danger">{requestError || result?.error}</pre>
      </div>
    {/if}
    {#if result}
      {#if result.resolved_sql}
        <div class="space-y-1">
          <div class="text-xs font-medium text-fg-3">Resolved SQL</div>
          <pre class="max-h-48 overflow-auto whitespace-pre-wrap break-all rounded bg-surface-2 p-3 font-mono text-xs text-fg-2">{result.resolved_sql}</pre>
        </div>
      {/if}
      {#each groups as group (group.when)}
        <div class="space-y-1">
          <div class="text-xs font-medium text-fg-3">On {group.when}</div>
          {#each group.sql as sql, i (i)}
            <pre class="max-h-48 overflow-auto whitespace-pre-wrap break-all rounded bg-surface-2 p-3 font-mono text-xs text-fg-2">{sql}</pre>
          {/each}
        </div>
      {/each}
    {:else if !compiling && !requestError}
      <EmptyState size="compact" title="Not compiled yet" description="Compile to see the statements a run sends." />
    {/if}
  </div>
</div>
