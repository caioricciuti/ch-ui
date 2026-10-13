<!-- SPDX-License-Identifier: BUSL-1.1 -->
<script lang="ts">
  import type { RbacDiff } from '../../../types/rbac'
  import Badge from '../../common/Badge.svelte'
  import SqlDiff from '../../common/SqlDiff.svelte'

  /** Side-by-side before and after of one entity, line by line. */
  interface Props {
    diff: RbacDiff
  }

  let { diff }: Props = $props()

  const status = $derived(!diff.current ? 'new' : !diff.proposed ? 'removed' : 'changed')
  const kindLabel: Record<string, string> = {
    role: 'Role', user: 'User', row_policy: 'Row policy', quota: 'Quota', settings_profile: 'Settings profile',
  }
</script>

<section class="overflow-hidden rounded-lg border border-edge-subtle bg-surface">
  <header class="flex items-center gap-2 border-b border-edge-subtle px-3 py-2">
    <span class="text-xs text-fg-3">{kindLabel[diff.kind] ?? diff.kind}</span>
    <span class="truncate text-[13px] font-medium text-fg">{diff.entity}</span>
    <Badge tone={status === 'new' ? 'success' : status === 'removed' ? 'danger' : 'info'}>{status}</Badge>
  </header>
  <SqlDiff before={diff.current} after={diff.proposed} />
</section>
