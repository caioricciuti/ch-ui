<!-- SPDX-License-Identifier: BUSL-1.1 -->
<script lang="ts">
  import { errorText } from '../../../utils/errors'
  import { untrack } from 'svelte'
  import type { RbacHistoryDiff, RbacHistoryRow, RbacRevertPlan } from '../../../types/rbac'
  import { fetchRbacHistoryDiff, planRbacRevert } from '../../../api/rbac'
  import { setSection } from '../../../stores/nav.svelte'
  import { success as toastSuccess, errorFrom as toastErrorFrom } from '../../../stores/toast.svelte'
  import { formatDate } from '../../../utils/format'
  import { changeLine, changeTone, driftLabel, driftTone, effectiveLine, entityKeyLabel, sourceLabel, stageRevert } from './history'
  import Sheet from '../../common/Sheet.svelte'
  import Button from '../../common/Button.svelte'
  import Badge from '../../common/Badge.svelte'
  import Select from '../../common/Select.svelte'
  import Spinner from '../../common/Spinner.svelte'
  import Panel from '../../common/Panel.svelte'
  import EmptyState from '../../common/EmptyState.svelte'
  import ConfirmDialog from '../../common/ConfirmDialog.svelte'
  import DdlDiff from './DdlDiff.svelte'

  /**
   * One access history row: what changed against the previous snapshot (or
   * another one, or live), who gains or loses access through it, and the
   * way back. Revert builds the ops on the server and stages them in Access
   * control's review, where they plan and apply like any other change.
   */
  interface Props {
    open: boolean
    row: RbacHistoryRow | null
    history: RbacHistoryRow[]
    onclose: () => void
  }

  let { open, row, history, onclose }: Props = $props()

  let against = $state('previous')
  let diff = $state<RbacHistoryDiff | null>(null)
  let diffLoading = $state(false)
  let diffError = $state('')
  let request = 0

  let revert = $state<RbacRevertPlan | null>(null)
  let reverting = $state(false)
  let picked = $state<Record<string, boolean>>({})
  let confirmOpen = $state(false)

  $effect(() => {
    if (!open || !row) return
    const id = row.id
    const target = against
    untrack(() => void loadDiff(id, target))
  })

  async function loadDiff(id: string, target: string) {
    const req = ++request
    diffLoading = true
    diffError = ''
    try {
      const next = await fetchRbacHistoryDiff(id, target)
      if (req === request) diff = next
    } catch (err: unknown) {
      if (req !== request) return
      diff = null
      diffError = errorText(err)
    } finally {
      if (req === request) diffLoading = false
    }
  }

  const compareOptions = $derived([
    { value: 'previous', label: 'Previous snapshot' },
    { value: 'live', label: 'Live' },
    ...history.filter((h) => h.id !== row?.id).map((h) => ({ value: h.id, label: `${formatDate(h.first_seen_at)} (${sourceLabel(h.source)})` })),
  ])

  const changes = $derived(diff?.diff.changes ?? [])
  const effective = $derived(diff?.diff.effective ?? [])
  const entities = $derived(diff?.diff.entities ?? [])
  const summary = $derived(row?.summary ?? {})
  const liveCompare = $derived(!!diff?.to.live)

  const pickedOps = $derived((revert?.entities ?? []).filter((e) => picked[e.name]).reduce((n, e) => n + e.ops, 0))

  async function prepareRevert(only: string[] = []) {
    if (!row) return
    reverting = true
    try {
      const plan = await planRbacRevert(row.id, only)
      revert = plan
      if (plan.over_cap) {
        picked = {}
        return
      }
      if (plan.ops.length === 0) {
        toastSuccess(plan.unrevertable.length ? 'Nothing CH-UI can change; see what cannot be reverted' : 'Access control already matches this snapshot')
        return
      }
      if (plan.unrevertable.length > 0) {
        confirmOpen = true
        return
      }
      stage(plan)
    } catch (err: unknown) {
      toastErrorFrom(err)
    } finally {
      reverting = false
    }
  }

  function stage(plan: RbacRevertPlan) {
    stageRevert({ ops: plan.ops, revertOf: plan.snapshot_id })
    confirmOpen = false
    onclose()
    setSection('rbac')
  }

  function sideLabel(side: RbacHistoryDiff['from']): string {
    if (side.live) return 'Live'
    if (!side.id) return side.label
    return formatDate(side.at)
  }
</script>

<Sheet
  {open}
  title={row ? `Snapshot of ${formatDate(row.first_seen_at)}` : 'Snapshot'}
  description="What changed, who it affects, and the way back."
  size="xl"
  {onclose}
>
  {#if row}
    <div class="space-y-6">
      <div class="flex flex-wrap items-center gap-2 text-[13px] text-fg-2">
        <Badge tone={row.source === 'apply' || row.source === 'revert' ? 'info' : 'neutral'}>{sourceLabel(row.source)}</Badge>
        {#if row.drift !== 'none'}<Badge tone={driftTone(row.drift)}>{driftLabel(row.drift)}</Badge>{/if}
        {#if !row.complete}<Badge tone="warning" title={(summary.incomplete ?? []).join('\n')}>Partial read</Badge>{/if}
        <span>by {row.actor || row.ch_user}{row.ch_user && row.ch_user !== row.actor ? ` as ${row.ch_user}` : ''}</span>
        {#if row.host}<span class="text-fg-4">on {row.host}</span>{/if}
        {#if row.seen_count > 1}<span class="text-fg-4">seen {row.seen_count} times, last {formatDate(row.last_seen_at)}</span>{/if}
      </div>

      {#if summary.note || summary.host_changed || summary.unexplained?.length}
        <div class="space-y-1 rounded-md bg-surface-2 px-3 py-2 text-[13px] text-fg-2">
          {#if summary.note}<p>{summary.note}.</p>{/if}
          {#if summary.host_changed}<p>Read from {row.host}, the previous snapshot from {summary.previous_host}.</p>{/if}
          {#if summary.unexplained?.length}
            <p>Changed during this apply but not by it: {summary.unexplained.map(entityKeyLabel).join(', ')}.</p>
          {/if}
        </div>
      {/if}

      {#if revert?.over_cap}
        <Panel title="Pick what to revert" description={`Going back needs ${revert.ops.length} changes; one review takes at most ${revert.max_ops}. Pick entities to revert now and repeat for the rest.`}>
          <div class="max-h-72 space-y-1 overflow-auto">
            {#each revert.entities as e (e.name)}
              <label class="ds-checkbox-label flex items-center gap-2 text-[13px]">
                <input type="checkbox" class="ds-checkbox" checked={!!picked[e.name]} onchange={(ev) => (picked = { ...picked, [e.name]: ev.currentTarget.checked })} />
                <span class="flex-1 truncate text-fg">{e.name}</span>
                <span class="text-xs tabular-nums text-fg-4">{e.ops}</span>
              </label>
            {/each}
          </div>
          <p class="mt-2 text-xs text-fg-3">{pickedOps} of at most {revert.max_ops} changes picked.</p>
        </Panel>
      {/if}

      {#if revert && revert.unrevertable.length > 0}
        <Panel title={`Cannot be reverted (${revert.unrevertable.length})`} description="CH-UI never creates or drops users and never changes entities defined in users.xml. These stay as they are.">
          <ul class="space-y-1 text-[13px] text-fg-2">
            {#each revert.unrevertable as u, i (i)}
              <li><span class="text-fg">{u.kind} {u.name}</span>: {u.reason}</li>
            {/each}
          </ul>
        </Panel>
      {/if}

      <div class="flex items-center gap-2">
        <span class="text-xs text-fg-3">Compare with</span>
        <Select size="sm" class="w-72" options={compareOptions} bind:value={against} />
        {#if diff}
          <span class="text-xs text-fg-4">{sideLabel(diff.from)} to {sideLabel(diff.to)}</span>
        {/if}
      </div>

      {#if diffLoading && !diff}
        <div class="flex justify-center py-8"><Spinner /></div>
      {:else if diffError}
        <div class="rounded-md bg-danger-soft px-3 py-2 text-xs text-danger">{diffError}</div>
      {:else if diff && changes.length === 0}
        <EmptyState size="compact" title="No differences" description={diff.from.id || diff.from.live ? 'Both sides hold the same access control state.' : 'This is the first snapshot recorded for this ClickHouse user.'} />
      {:else if diff}
        <section class="space-y-2">
          <h3 class="text-[13px] font-semibold text-fg">Changes ({changes.length})</h3>
          <ul class="divide-y divide-edge-subtle rounded-lg border border-edge-subtle bg-surface">
            {#each changes as c, i (i)}
              <li class="flex items-start gap-2 px-3 py-1.5 text-[13px]">
                <Badge tone={changeTone(c)}>{c.change}</Badge>
                <span class="min-w-0 flex-1 break-words font-mono text-[12px] text-fg-2">{changeLine(c)}</span>
              </li>
            {/each}
          </ul>
        </section>

        {#if effective.length}
          <section class="space-y-2">
            <h3 class="text-[13px] font-semibold text-fg">Effective access ({effective.length})</h3>
            <ul class="divide-y divide-edge-subtle rounded-lg border border-edge-subtle bg-surface">
              {#each effective as e, i (i)}
                <li class="flex items-start gap-2 px-3 py-1.5 text-[13px]">
                  <Badge tone={e.change === 'gained' ? 'success' : 'danger'}>{e.change}</Badge>
                  <span class="min-w-0 flex-1 break-words text-fg-2">{effectiveLine(e)}</span>
                </li>
              {/each}
            </ul>
          </section>
        {/if}

        <section class="space-y-2">
          <h3 class="text-[13px] font-semibold text-fg">Changes by entity</h3>
          {#each entities as d (d.kind + d.entity)}
            <DdlDiff diff={d} beforeLabel={liveCompare ? 'Snapshot' : 'Before'} afterLabel={liveCompare ? 'Live' : 'After'} />
          {/each}
        </section>
      {/if}
    </div>
  {/if}

  {#snippet footer()}
    <span class="mr-auto text-xs text-fg-3">A revert opens in Access control for review; nothing runs until you apply it.</span>
    <Button size="sm" variant="outline" onclick={onclose}>Close</Button>
    {#if revert?.over_cap}
      <Button size="sm" disabled={pickedOps === 0 || pickedOps > (revert?.max_ops ?? 0)} loading={reverting} onclick={() => void prepareRevert(Object.keys(picked).filter((k) => picked[k]))}>
        Revert picked
      </Button>
    {:else}
      <Button size="sm" disabled={!row} loading={reverting} onclick={() => void prepareRevert()}>Revert to this state</Button>
    {/if}
  {/snippet}
</Sheet>

<ConfirmDialog
  open={confirmOpen}
  title="Stage a partial revert?"
  description={revert ? `${revert.ops.length} change${revert.ops.length === 1 ? '' : 's'} can be staged. ${revert.unrevertable.length} difference${revert.unrevertable.length === 1 ? '' : 's'} cannot be reverted and will stay as they are.` : ''}
  confirmLabel="Stage revert"
  onconfirm={() => revert && stage(revert)}
  oncancel={() => (confirmOpen = false)}
/>
