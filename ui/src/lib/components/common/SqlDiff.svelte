<!-- SPDX-License-Identifier: BUSL-1.1 -->
<script lang="ts">
  import { lineDiff, type DiffRowKind } from '../../utils/lineDiff'

  /** Side-by-side, line-by-line diff of two SQL texts. */
  interface Props {
    before: string
    after: string
    beforeLabel?: string
    afterLabel?: string
  }

  let { before, after, beforeLabel = 'Current', afterLabel = 'Proposed' }: Props = $props()

  const rows = $derived(lineDiff(before, after))

  function leftClass(kind: DiffRowKind): string {
    return kind === 'removed' || kind === 'changed' ? 'bg-danger-soft text-fg' : 'text-fg-2'
  }
  function rightClass(kind: DiffRowKind): string {
    return kind === 'added' || kind === 'changed' ? 'bg-success-soft text-fg' : 'text-fg-2'
  }
</script>

<div class="overflow-x-auto">
  <table class="w-full table-fixed border-collapse font-mono text-[11px] leading-5">
    <thead>
      <tr class="text-left text-[11px] text-fg-3">
        <th class="w-1/2 border-b border-r border-edge-subtle px-3 py-1 font-sans font-medium">{beforeLabel}</th>
        <th class="w-1/2 border-b border-edge-subtle px-3 py-1 font-sans font-medium">{afterLabel}</th>
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
