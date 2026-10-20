<script lang="ts">
  import { onMount } from 'svelte'
  import { BarChart3, RefreshCw } from 'lucide-svelte'
  import type { BrainUsageGroupBy, BrainUsageReport, BrainUsageRow } from '../../types/brain'
  import { adminGetBrainUsage } from '../../api/brain'
  import { setSection } from '../../stores/nav.svelte'
  import { errorFrom as toastErrorFrom } from '../../stores/toast.svelte'
  import { formatCost, formatTokens } from '../brain/brain-usage'
  import { subjectLabel } from '../brain/brain-budget'
  import SectionHeader from '../common/SectionHeader.svelte'
  import Select from '../common/Select.svelte'
  import Stat from '../common/Stat.svelte'
  import Panel from '../common/Panel.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import Badge from '../common/Badge.svelte'
  import Button from '../common/Button.svelte'
  import Spinner from '../common/Spinner.svelte'
  import DataTable, { type DataColumn } from '../common/DataTable.svelte'

  // Admin > Brain usage: the token ledger (chat turns, Ask AI and Fix with
  // AI requests and their SQL repairs) summed per person or role and model
  // for a period, with the cost stored when each request ran.

  let days = $state('30')
  let groupBy = $state<BrainUsageGroupBy>('person')
  let loading = $state(false)
  let report = $state<BrainUsageReport | null>(null)

  const periodOptions = [
    { value: '1', label: 'Today' },
    { value: '7', label: 'Last 7 days' },
    { value: '30', label: 'Last 30 days' },
    { value: '90', label: 'Last 90 days' },
  ]
  const groupOptions = [
    { value: 'person', label: 'By person' },
    { value: 'role', label: 'By role' },
  ]

  onMount(() => {
    void load()
  })

  async function load() {
    loading = true
    try {
      report = await adminGetBrainUsage(Number(days), groupBy)
    } catch (e) {
      toastErrorFrom(e)
    } finally {
      loading = false
    }
  }

  /** Opens the price editor in the Brain section. */
  function openPriceEditor() {
    const url = new URL(window.location.href)
    url.searchParams.set('edit', 'prices')
    history.replaceState(null, '', `${url.pathname}?${url.searchParams.toString()}`)
    setSection('brain')
  }

  type Row = BrainUsageRow & Record<string, unknown>
  const rows = $derived((report?.rows ?? []) as Row[])
  const tokens = (v: unknown) => (typeof v === 'number' ? formatTokens(v) : '')
  const columns = $derived<DataColumn<Row>[]>([
    {
      key: 'group',
      label: report?.group_by === 'role' ? 'Role' : 'Person',
      truncate: true,
      format: (v) => (report?.group_by === 'role' ? String(v) || 'Not recorded' : subjectLabel('user', String(v))),
    },
    { key: 'model', label: 'Model', mono: true, truncate: true },
    { key: 'requests', label: 'Requests', align: 'right', width: '90px' },
    { key: 'repairs', label: 'Repairs', align: 'right', width: '80px' },
    { key: 'input_tokens', label: 'Input', align: 'right', width: '80px', format: tokens },
    { key: 'output_tokens', label: 'Output', align: 'right', width: '80px', format: tokens },
    { key: 'cache_read_tokens', label: 'Cache read', align: 'right', width: '96px', format: tokens },
    { key: 'cache_write_tokens', label: 'Cache write', align: 'right', width: '96px', format: tokens },
    {
      key: 'cost_usd',
      label: 'Cost',
      align: 'right',
      width: '120px',
      format: (v) => (typeof v === 'number' ? formatCost(v) : ''),
      sortValue: (r) => (typeof r.cost_usd === 'number' ? r.cost_usd : -1),
    },
  ])

  const totals = $derived(report?.totals)
  const unpricedModels = $derived(report?.unpriced_models ?? [])
  const groupNoun = $derived(report?.group_by === 'role' ? ['role', 'roles'] : ['person', 'people'])
</script>

<div class="space-y-8">
  <SectionHeader title="Brain usage" description="Tokens and cost per person or role and model, from Brain chat, Ask AI and Fix with AI. Cost is stored when each request runs, so a price change never rewrites the past.">
    {#snippet actions()}
      <Select size="sm" class="w-32" options={groupOptions} bind:value={groupBy} onchange={(v) => { groupBy = v as BrainUsageGroupBy; void load() }} />
      <Select size="sm" class="w-36" options={periodOptions} bind:value={days} onchange={(v) => { days = v; void load() }} />
      <Button icon size="sm" variant="ghost" aria-label="Refresh" title="Refresh" onclick={() => void load()}>
        <RefreshCw size={14} />
      </Button>
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
      <div class="grid grid-cols-2 gap-3 md:grid-cols-4">
        <Stat label="Requests" value={totals.requests.toLocaleString('en-US')} hint={`${totals.groups} ${totals.groups === 1 ? groupNoun[0] : groupNoun[1]}`} />
        <Stat label="Input tokens" value={formatTokens(totals.input_tokens + totals.cache_write_tokens)} hint={`${formatTokens(totals.cache_read_tokens)} read from cache`} />
        <Stat label="Output tokens" value={formatTokens(totals.output_tokens)} />
        <Stat
          label="Cost"
          value={formatCost(totals.cost_usd)}
          hint={totals.unpriced_requests > 0
            ? `${totals.unpriced_requests.toLocaleString('en-US')} request(s) without a price`
            : totals.cost_estimated ? 'Includes estimates for older requests' : 'All requests priced'}
          tone={totals.unpriced_requests > 0 ? 'warning' : 'default'}
        />
      </div>
    {/if}

    {#if unpricedModels.length > 0}
      <Panel variant="muted">
        <div class="flex flex-wrap items-start gap-3">
          <div class="min-w-0 flex-1">
            <p class="text-[13px] font-medium text-fg">Models without a price</p>
            <p class="mt-1 text-[13px] leading-relaxed text-fg-3">
              These requests count toward token budgets but not dollar budgets, and show no cost. A price set now applies to new requests only.
            </p>
            <div class="mt-2 flex flex-wrap gap-1.5">
              {#each unpricedModels as m (m.provider + m.model)}
                <Badge tone="warning" title={`${m.provider}: ${m.requests} request(s)`}>{m.model}</Badge>
              {/each}
            </div>
          </div>
          <Button size="sm" variant="outline" onclick={openPriceEditor}>Set prices</Button>
        </div>
      </Panel>
    {/if}

    <DataTable {columns} {rows} rowKey={(r) => `${r.group}|${r.provider}|${r.model}`} sort={{ key: 'cost_usd', dir: 'desc' }} emptyTitle="No usage">
      {#snippet cell(row, col, value)}
        {#if col.key === 'cost_usd' && row.cost_usd === null}
          <Badge>No price</Badge>
        {:else if col.key === 'cost_usd'}
          <span class="inline-flex items-center gap-1.5">
            {#if row.estimated}<span class="text-[11px] text-fg-3" title="Part of this cost was estimated at upgrade time from the prices then in effect">est.</span>{/if}
            {value}
            {#if row.unpriced_requests > 0}<span class="text-[11px] text-fg-3" title={`${row.unpriced_requests} request(s) had no price`}>+{row.unpriced_requests} unpriced</span>{/if}
          </span>
        {:else if col.key === 'requests'}
          <span title={`${row.chat_turns} chat turn(s), ${row.ask_ai} Ask AI or Fix with AI, ${row.repairs} SQL repair(s)`}>{value}</span>
        {:else}
          {value}
        {/if}
      {/snippet}
    </DataTable>
  {/if}
</div>
