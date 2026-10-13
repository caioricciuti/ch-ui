<!-- SPDX-License-Identifier: BUSL-1.1 -->
<script lang="ts">
  import type { RbacDiff } from '../../../types/rbac'
  import { lineDiff, type DiffRowKind } from './lineDiff'
  import Badge from '../../common/Badge.svelte'

  /** Side-by-side before and after of one entity, line by line. */
  interface Props {
    diff: RbacDiff
  }

  let { diff }: Props = $props()

  const rows = $derived(lineDiff(diff.current, diff.proposed))
  const status = $derived(!diff.current ? 'new' : !diff.proposed ? 'removed' : 'changed')
  const kindLabel: Record<string, string> = {
    role: 'Role', user: 'User', row_policy: 'Row policy', quota: 'Quota', settings_profile: 'Settings profile',
  }

  function leftClass(kind: DiffRowKind): string {
    return kind === 'removed' || kind === 'changed' ? 'bg-danger-soft text-fg' : 'text-fg-2'
  }
  function rightClass(kind: DiffRowKind): string {
    return kind === 'added' || kind === 'changed' ? 'bg-success-soft text-fg' : 'text-fg-2'
  }
</script>

<section class="overflow-hidden rounded-lg border border-edge-subtle bg-surface">
  <header class="flex items-center gap-2 border-b border-edge-subtle px-3 py-2">
    <span class="text-xs text-fg-3">{kindLabel[diff.kind] ?? diff.kind}</span>
    <span class="truncate text-[13px] font-medium text-fg">{diff.entity}</span>
    <Badge tone={status === 'new' ? 'success' : status === 'removed' ? 'danger' : 'info'}>{status}</Badge>
  </header>
  <div class="overflow-x-auto">
    <table class="w-full table-fixed border-collapse font-mono text-[11px] leading-5">
      <thead>
        <tr class="text-left text-[11px] text-fg-3">
          <th class="w-1/2 border-b border-r border-edge-subtle px-3 py-1 font-sans font-medium">Current</th>
          <th class="w-1/2 border-b border-edge-subtle px-3 py-1 font-sans font-medium">Proposed</th>
        </tr>
      </thead>
      <tbody>
        {#each rows as row, i (i)}
          <tr>
            <td class="whitespace-pre-wrap break-all border-r border-edge-subtle px-3 align-top {leftClass(row.kind)}">{row.left ?? ''}</td>
            <td class="whitespace-pre-wrap break-all px-3 align-top {rightClass(row.kind)}">{row.right ?? ''}</td>
          </tr>
        {:else}
          <tr><td colspan="2" class="px-3 py-2 font-sans text-xs text-fg-3">No visible change.</td></tr>
        {/each}
      </tbody>
    </table>
  </div>
</section>
