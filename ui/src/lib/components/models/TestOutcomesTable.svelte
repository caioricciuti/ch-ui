<script lang="ts">
  import type { TestOutcome } from '../../types/models'
  import DataTable, { type DataColumn } from '../common/DataTable.svelte'
  import Badge from '../common/Badge.svelte'

  interface Props {
    outcomes: TestOutcome[]
    class?: string
  }

  let { outcomes, class: cls = '' }: Props = $props()

  type Row = Record<string, unknown>
  const asOutcome = (row: Row): TestOutcome => row as unknown as TestOutcome

  const columns: DataColumn<Row>[] = [
    { key: 'kind', label: 'Test', mono: true, format: (_v, r) => testLabel(asOutcome(r)) },
    { key: 'severity', label: 'Severity' },
    { key: 'status', label: 'Status' },
    { key: 'failures', label: 'Failing rows', align: 'right' },
    { key: 'error', label: 'Error', truncate: true, sortable: false },
  ]

  function testLabel(o: TestOutcome): string {
    return o.column ? `${o.kind}(${o.column})` : o.kind
  }

  function statusTone(o: TestOutcome): 'success' | 'warning' | 'danger' {
    if (o.status === 'pass') return 'success'
    return o.severity === 'warn' && o.status === 'fail' ? 'warning' : 'danger'
  }
</script>

<DataTable
  {columns}
  rows={outcomes as unknown as Row[]}
  rowKey={(r) => String(r.test_id)}
  emptyTitle="No data tests ran"
  class={cls}
>
  {#snippet cell(row, col, value)}
    {@const o = asOutcome(row)}
    {#if col.key === 'severity'}
      <Badge tone={o.severity === 'error' ? 'danger' : 'warning'}>{o.severity}</Badge>
    {:else if col.key === 'status'}
      <Badge tone={statusTone(o)}>{o.status}</Badge>
    {:else if col.key === 'error'}
      <span class="text-danger">{value}</span>
    {:else}
      {value}
    {/if}
  {/snippet}
</DataTable>
