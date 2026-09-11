<script lang="ts">
  import Sheet from '../common/Sheet.svelte'
  import Button from '../common/Button.svelte'
  import Badge from '../common/Badge.svelte'
  import Spinner from '../common/Spinner.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import type { TelemetrySource } from '../../types/telemetry'

  interface Props {
    open: boolean
    loading: boolean
    proposals: TelemetrySource[]
    adding: boolean
    onadd: (selected: TelemetrySource[]) => void
    onclose: () => void
  }
  let { open, loading, proposals, adding, onadd, onclose }: Props = $props()

  let checked = $state<Set<number>>(new Set())

  $effect(() => {
    // Everything proposed starts selected.
    checked = new Set(proposals.map((_, i) => i))
  })

  function toggle(i: number) {
    const next = new Set(checked)
    if (next.has(i)) next.delete(i)
    else next.add(i)
    checked = next
  }

  function tableLabel(p: TelemetrySource): string {
    if (p.kind === 'metrics' && p.tables) {
      const present = Object.values(p.tables).filter(Boolean)
      return `${p.database} · ${present.length} table${present.length === 1 ? '' : 's'}`
    }
    return `${p.database}.${p.table}`
  }

  function mappedCount(p: TelemetrySource): number {
    const m = p.kind === 'logs' ? p.logs : p.kind === 'traces' ? p.traces : null
    return m ? Object.values(m).filter((v) => typeof v === 'string' && v !== '' && v !== 'ns').length : 0
  }
</script>

<Sheet {open} title="Detect sources" description="Tables on this connection that look like OpenTelemetry exporter output. Nothing is saved until you add it." size="md" {onclose}>
  {#if loading}
    <div class="flex items-center justify-center gap-2 py-10 text-xs text-fg-3"><Spinner size="sm" /> Scanning system.tables…</div>
  {:else if proposals.length === 0}
    <EmptyState size="compact" title="Nothing found" description="No table with the exporter's columns (Body + SeverityText, SpanId + Duration, or otel_metrics_*).">
      <a class="inline-flex h-7 items-center gap-1.5 rounded-md border border-edge px-2.5 text-xs font-medium text-fg-2 transition-colors hover:border-edge-strong hover:bg-hover hover:text-fg" href="https://github.com/caioricciuti/ch-ui/blob/main/docs/telemetry.md" target="_blank" rel="noopener noreferrer">Read the setup guide</a>
    </EmptyState>
  {:else}
    <div class="divide-y divide-edge-subtle rounded-lg border border-edge-subtle bg-surface">
      {#each proposals as p, i (i)}
        <label class="flex cursor-pointer items-center gap-3 px-3 py-2">
          <input type="checkbox" class="ds-checkbox" checked={checked.has(i)} onchange={() => toggle(i)} />
          <Badge tone={p.kind === 'logs' ? 'brand' : p.kind === 'traces' ? 'info' : 'success'}>{p.kind}</Badge>
          <span class="min-w-0 flex-1 truncate font-mono text-xs text-fg" title={tableLabel(p)}>{tableLabel(p)}</span>
          {#if p.kind !== 'metrics'}
            <span class="shrink-0 text-[11px] text-fg-4">{mappedCount(p)} columns mapped</span>
          {/if}
        </label>
      {/each}
    </div>
  {/if}
  {#snippet footer()}
    <Button variant="ghost" size="sm" onclick={onclose}>Cancel</Button>
    <Button size="sm" loading={adding} disabled={loading || checked.size === 0} onclick={() => onadd(proposals.filter((_, i) => checked.has(i)))}>
      Add selected
    </Button>
  {/snippet}
</Sheet>
