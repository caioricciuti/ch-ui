<script lang="ts">
  import { onMount } from 'svelte'
  import { BarChart3 } from 'lucide-svelte'
  import type { BrainUsageReport, BrainUsageRow } from '../../types/brain'
  import { adminGetBrainUsage } from '../../api/brain'
  import { error as toastError } from '../../stores/toast.svelte'
  import { formatCost, formatTokens } from '../brain/brain-usage'
  import SectionHeader from '../common/SectionHeader.svelte'
  import Select from '../common/Select.svelte'
  import Stat from '../common/Stat.svelte'
  import Panel from '../common/Panel.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import Badge from '../common/Badge.svelte'
  import Spinner from '../common/Spinner.svelte'
  import DataTable, { type DataColumn } from '../common/DataTable.svelte'

  // The Brain token ledger: chat turns, Ask AI and Fix with AI requests and
  // their SQL repairs, summed per person and model for a period, with
  // estimated cost where priced.

  let days = $state('30')
  let loading = $state(false)
  let report = $state<BrainUsageReport | null>(null)

  const periodOptions = [
    { value: '1', label: 'Today' },
    { value: '7', label: 'Last 7 days' },
    { value: '30', label: 'Last 30 days' },
    { value: '90', label: 'Last 90 days' },
  ]

  onMount(() => {
    void load()
  })

  async function load() {
    loading = true
    try {
      report = await adminGetBrainUsage(Number(days))
    } catch (e) {
      toastError(e instanceof Error ? e.message : String(e))
    } finally {
      loading = false
    }
  }

  type Row = BrainUsageRow & Record<string, unknown>
  const rows = $derived((report?.rows ?? []) as Row[])
  const tokens = (v: unknown) => (typeof v === 'number' ? formatTokens(v) : '')
  const columns: DataColumn<Row>[] = [
    { key: 'username', label: 'User', truncate: true },
    { key: 'model', label: 'Model', mono: true, truncate: true },
    { key: 'requests', label: 'Requests', align: 'right', width: '90px' },
    { key: 'repairs', label: 'Repairs', align: 'right', width: '80px' },
    { key: 'input_tokens', label: 'Input', align: 'right', width: '80px', format: tokens },
    { key: 'output_tokens', label: 'Output', align: 'right', width: '80px', format: tokens },
    { key: 'cache_read_tokens', label: 'Cache read', align: 'right', width: '96px', format: tokens },
    { key: 'cache_write_tokens', label: 'Cache write', align: 'right', width: '96px', format: tokens },
    {
      key: 'cost_usd',
      label: 'Est. cost',
      align: 'right',
      width: '96px',
      format: (v) => (typeof v === 'number' ? formatCost(v) : ''),
      sortValue: (r) => (typeof r.cost_usd === 'number' ? r.cost_usd : -1),
    },
  ]

  const totals = $derived(report?.totals)
</script>

<section>
  <SectionHeader title="Usage" description="Tokens per person and model from Brain chat and Ask AI. Cost is an estimate from the price table.">
    {#snippet actions()}
      <Select size="sm" class="w-36" options={periodOptions} bind:value={days} onchange={(v) => { days = v; void load() }} />
    {/snippet}
  </SectionHeader>

  {#if loading && !report}
    <div class="flex items-center justify-center py-8"><Spinner /></div>
  {:else if !report || report.rows.length === 0}
    <Panel padding="none">
      <EmptyState size="compact" icon={BarChart3} title="No usage in this period" description="Token counts appear here after people use Brain chat or Ask AI." />
    </Panel>
  {:else}
    {#if totals}
      <div class="mb-3 grid grid-cols-2 gap-3 md:grid-cols-4">
        <Stat label="Requests" value={totals.requests.toLocaleString('en-US')} hint={`${totals.users} ${totals.users === 1 ? 'person' : 'people'}`} />
        <Stat label="Input tokens" value={formatTokens(totals.input_tokens + totals.cache_write_tokens)} hint={`${formatTokens(totals.cache_read_tokens)} read from cache`} />
        <Stat label="Output tokens" value={formatTokens(totals.output_tokens)} />
        <Stat
          label="Estimated cost"
          value={formatCost(totals.cost_usd)}
          hint={totals.unpriced_rows > 0 ? `${totals.unpriced_rows} row(s) without a price` : 'All rows priced'}
          tone={totals.unpriced_rows > 0 ? 'warning' : 'default'}
        />
      </div>
    {/if}
    <DataTable {columns} {rows} rowKey={(r) => `${r.username}|${r.provider}|${r.model}`} sort={{ key: 'cost_usd', dir: 'desc' }} emptyTitle="No usage">
      {#snippet cell(row, col, value)}
        {#if col.key === 'cost_usd' && row.cost_usd === null}
          <Badge>No price</Badge>
        {:else if col.key === 'requests'}
          <span title={`${row.chat_turns} chat turn(s), ${row.ask_ai} Ask AI or Fix with AI, ${row.repairs} SQL repair(s)`}>{value}</span>
        {:else}
          {value}
        {/if}
      {/snippet}
    </DataTable>
  {/if}
</section>
