<!-- SPDX-License-Identifier: BUSL-1.1 -->
<script lang="ts">
  import { untrack } from 'svelte'
  import type { RbacApplyResponse, RbacOp, RbacPlan, RbacSnapshot } from '../../../types/rbac'
  import { applyRbac, fetchClusterNames, planRbac } from '../../../api/rbac'
  import { ApiError } from '../../../api/client'
  import { isProActive } from '../../../stores/license.svelte'
  import { describeOp, dropConfirmName, hasReplicatedStorage } from './changes'
  import Sheet from '../../common/Sheet.svelte'
  import Button from '../../common/Button.svelte'
  import Badge from '../../common/Badge.svelte'
  import Input from '../../common/Input.svelte'
  import Select from '../../common/Select.svelte'
  import Spinner from '../../common/Spinner.svelte'
  import Panel from '../../common/Panel.svelte'
  import EmptyState from '../../common/EmptyState.svelte'
  import DdlDiff from './DdlDiff.svelte'

  /**
   * Review of the staged change set: target cluster, per-entity diffs,
   * numbered statements, warnings to resolve, then apply and per-statement
   * results. The server builds every statement; this sheet only shows them.
   */
  interface Props {
    open: boolean
    ops: RbacOp[]
    snapshot: RbacSnapshot | null
    onunstage: (index: number) => void
    onclose: () => void
    onapplied: (res: RbacApplyResponse) => void
    onrollback: (ops: RbacOp[]) => void
    /** The access history row this change set reverts to, if any. */
    revertOf?: string
  }

  let { open, ops, snapshot, onunstage, onclose, onapplied, onrollback, revertOf = '' }: Props = $props()

  let clusters = $state<string[]>([])
  let cluster = $state('')
  let onCluster = $state(false)
  let plan = $state<RbacPlan | null>(null)
  let planning = $state(false)
  let planError = $state('')
  let acked = $state<Record<string, boolean>>({})
  let typed = $state<Record<number, string>>({})
  let applying = $state(false)
  let applyError = $state('')
  let result = $state<RbacApplyResponse | null>(null)
  let request = 0

  const replicated = $derived(hasReplicatedStorage(snapshot))
  const canCluster = $derived(snapshot?.capabilities.cluster ?? false)
  const effectiveCluster = $derived(onCluster && cluster ? cluster : '')

  $effect(() => {
    if (!open) return
    untrack(() => {
      result = null
      applyError = ''
      fetchClusterNames()
        .then((c) => {
          clusters = c
          if (!cluster && c.length) cluster = c[0]
        })
        .catch(() => (clusters = []))
    })
  })

  // Re-plan whenever the change set or the target changes.
  $effect(() => {
    if (!open || result) return
    const currentOps = ops
    const target = effectiveCluster
    untrack(() => void replan(currentOps, target))
  })

  async function replan(currentOps: RbacOp[], target: string) {
    const id = ++request
    if (currentOps.length === 0) {
      plan = null
      planError = ''
      return
    }
    planning = true
    planError = ''
    try {
      const next = await planRbac(currentOps, target, revertOf)
      if (id !== request) return
      plan = next
    } catch (err: unknown) {
      if (id !== request) return
      plan = null
      planError = err instanceof Error ? err.message : String(err)
    } finally {
      if (id === request) planning = false
    }
  }

  const blocks = $derived((plan?.warnings ?? []).filter((w) => w.severity === 'block'))
  const acks = $derived((plan?.warnings ?? []).filter((w) => w.severity === 'ack'))
  const ackCodes = $derived([...new Set(acks.map((w) => w.code))])
  const drops = $derived(ops.map((op, index) => ({ index, name: dropConfirmName(op) })).filter((d) => d.name !== null))
  const ready = $derived(
    !!plan && !planning && blocks.length === 0 && ackCodes.every((c) => acked[c]) && drops.every((d) => (typed[d.index] ?? '') === d.name),
  )

  async function apply() {
    if (!plan || !ready) return
    applying = true
    applyError = ''
    try {
      const res = await applyRbac(ops, effectiveCluster, plan.plan_hash, ackCodes, revertOf)
      result = res
      onapplied(res)
    } catch (err: unknown) {
      applyError = err instanceof Error ? err.message : String(err)
      if (err instanceof ApiError && err.status === 409) void replan(ops, effectiveCluster)
    } finally {
      applying = false
    }
  }

  function prepareRollback() {
    if (!result) return
    const rb = result.rollback_ops
    result = null
    acked = {}
    typed = {}
    onrollback(rb)
  }

  function statusTone(s: string): 'success' | 'danger' | 'neutral' | 'warning' {
    return s === 'applied' ? 'success' : s === 'failed' ? 'danger' : s === 'pending' ? 'warning' : 'neutral'
  }

  const clusterOptions = $derived(clusters.map((c) => ({ value: c, label: c })))
</script>

<Sheet
  {open}
  title={result ? 'Results' : 'Review access changes'}
  description={result ? 'What ran, in order. A failure stops the run; later statements are skipped.' : 'Everything below runs as your ClickHouse user, in this order, when you apply.'}
  size="xl"
  {onclose}
>
  {#if result}
    <div class="space-y-3">
      {#each result.results as r (r.seq)}
        <div class="rounded-lg border border-edge-subtle bg-surface px-3 py-2">
          <div class="flex items-start gap-2">
            <span class="w-6 shrink-0 text-right text-xs tabular-nums text-fg-4">{r.seq}</span>
            <pre class="min-w-0 flex-1 whitespace-pre-wrap break-all font-mono text-[11px] text-fg-2">{r.sql}</pre>
            <Badge tone={statusTone(r.status)}>{r.status}</Badge>
          </div>
          {#if r.error}
            <p class="ml-8 mt-1 text-xs text-danger">{r.code === 'already_exists' ? 'Already exists. ' : ''}{r.error}</p>
          {/if}
          {#if r.hosts && r.hosts.length}
            <div class="ml-8 mt-2 divide-y divide-edge-subtle rounded-md bg-surface-2">
              {#each r.hosts as h (h.host)}
                <div class="flex items-center gap-2 px-2 py-1 text-xs">
                  <span class="flex-1 font-mono text-fg-2">{h.host}</span>
                  {#if h.error}<span class="truncate text-fg-3" title={h.error}>{h.error}</span>{/if}
                  <Badge tone={statusTone(h.status)}>{h.status}</Badge>
                </div>
              {/each}
            </div>
          {/if}
        </div>
      {/each}
      {#if result.rollback_ops.length}
        <p class="text-xs text-fg-3">Prepare rollback stages the inverse of every applied statement as a new change set. Nothing runs until you review and apply it.</p>
      {/if}
    </div>
  {:else if ops.length === 0}
    <EmptyState size="compact" title="No pending changes" description="Stage changes from a role, user, policy, quota or profile first." />
  {:else}
    <div class="space-y-6">
      <Panel title={`Pending changes (${ops.length})`}>
        <ol class="space-y-1">
          {#each ops as op, i (i)}
            <li class="flex items-center gap-2 text-[13px] text-fg-2">
              <span class="w-5 shrink-0 text-right text-xs tabular-nums text-fg-4">{i + 1}</span>
              <span class="flex-1">{describeOp(op)}</span>
              <Button size="xs" variant="ghost" onclick={() => onunstage(i)}>Remove</Button>
            </li>
          {/each}
        </ol>
      </Panel>

      <Panel title="Target" description={replicated ? 'Some entities use replicated access storage, which already reaches every node. Leave ON CLUSTER off for those.' : 'Runs on the connected node unless you choose a cluster.'}>
        <div class="flex flex-wrap items-center gap-3">
          <label class="ds-checkbox-label text-xs">
            <input type="checkbox" class="ds-checkbox" bind:checked={onCluster} disabled={clusters.length === 0 || !canCluster} />
            Run ON CLUSTER
          </label>
          <Select size="sm" class="w-56" options={clusterOptions} bind:value={cluster} disabled={!onCluster || clusters.length === 0} placeholder={clusters.length ? undefined : 'No clusters found'} />
          {#if !canCluster}<span class="text-xs text-fg-3">Your user lacks the CLUSTER privilege.</span>{/if}
        </div>
      </Panel>

      {#if planning && !plan}
        <div class="flex justify-center py-8"><Spinner /></div>
      {:else if planError}
        <div class="rounded-md bg-danger-soft px-3 py-2 text-xs text-danger">{planError}</div>
      {:else if plan}
        {#if blocks.length || acks.length}
          <section class="space-y-2">
            <h3 class="text-[13px] font-semibold text-fg">Warnings</h3>
            {#each blocks as w (w.code + w.message)}
              <div class="rounded-md bg-danger-soft px-3 py-2 text-[13px] text-danger">
                <span class="font-medium">Blocked.</span> {w.message}
              </div>
            {/each}
            {#each acks as w (w.code + w.message)}
              <div class="flex items-start gap-2 rounded-md bg-warning-soft px-3 py-2 text-[13px] text-fg">
                <input
                  id={`ack-${w.code}-${w.message.length}`}
                  type="checkbox"
                  class="ds-checkbox mt-0.5"
                  checked={!!acked[w.code]}
                  onchange={(e) => (acked = { ...acked, [w.code]: e.currentTarget.checked })}
                />
                <label for={`ack-${w.code}-${w.message.length}`}>{w.message}</label>
              </div>
            {/each}
          </section>
        {/if}

        {#if drops.length}
          <section class="space-y-2">
            <h3 class="text-[13px] font-semibold text-fg">Confirm drops</h3>
            {#each drops as d (d.index)}
              <div class="grid grid-cols-1 items-center gap-2 md:grid-cols-[1fr_240px]">
                <label for={`drop-${d.index}`} class="text-[13px] text-fg-2">Type <span class="font-mono text-fg">{d.name}</span> to {describeOp(ops[d.index]).toLowerCase()}</label>
                <Input id={`drop-${d.index}`} size="sm" mono value={typed[d.index] ?? ''} oninput={(e) => (typed = { ...typed, [d.index]: e.currentTarget.value })} />
              </div>
            {/each}
          </section>
        {/if}

        <section class="space-y-2">
          <h3 class="text-[13px] font-semibold text-fg">Changes by entity</h3>
          {#each plan.diffs as diff (diff.kind + diff.entity)}
            <DdlDiff {diff} />
          {/each}
        </section>

        <section class="space-y-2">
          <h3 class="text-[13px] font-semibold text-fg">Statements ({plan.statements.length})</h3>
          <div class="divide-y divide-edge-subtle rounded-lg border border-edge-subtle bg-surface">
            {#each plan.statements as s (s.seq)}
              <div class="flex items-start gap-2 px-3 py-2">
                <span class="w-6 shrink-0 text-right text-xs tabular-nums text-fg-4">{s.seq}</span>
                <div class="min-w-0 flex-1">
                  <pre class="whitespace-pre-wrap break-all font-mono text-[11px] text-fg">{s.sql}</pre>
                  {#if s.inverse_sql}
                    <pre class="mt-1 whitespace-pre-wrap break-all font-mono text-[11px] text-fg-4" title="How Prepare rollback would undo this statement">undo: {s.inverse_sql}</pre>
                  {/if}
                </div>
              </div>
            {/each}
          </div>
        </section>
      {/if}

      {#if applyError}
        <div class="rounded-md bg-danger-soft px-3 py-2 text-xs text-danger">{applyError}</div>
      {/if}
    </div>
  {/if}

  {#snippet footer()}
    {#if result}
      {#if result.rollback_ops.length}
        <Button size="sm" variant="outline" class="mr-auto" onclick={prepareRollback}>Prepare rollback</Button>
      {/if}
      <Button size="sm" onclick={onclose}>Done</Button>
    {:else}
      {#if !isProActive()}
        <span class="mr-auto text-xs text-fg-3">Applying needs an active Pro license.</span>
      {/if}
      <Button size="sm" variant="outline" onclick={onclose}>Keep editing</Button>
      <Button size="sm" disabled={!ready || !isProActive()} loading={applying} onclick={apply}>
        Apply {plan ? `${plan.statements.length} statement${plan.statements.length === 1 ? '' : 's'}` : ''}
      </Button>
    {/if}
  {/snippet}
</Sheet>
