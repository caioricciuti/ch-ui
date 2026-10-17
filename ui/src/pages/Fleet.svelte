<!-- SPDX-License-Identifier: BUSL-1.1 -->
<script lang="ts">
  import { onMount } from 'svelte'
  import { AlertTriangle, RefreshCw } from 'lucide-svelte'
  import { fetchFleet, type FleetResult, type FleetConnection } from '../lib/api/fleet'
  import { getSession, login } from '../lib/stores/session.svelte'
  import { navigate } from '../lib/stores/router.svelte'
  import { getAuthConfig, type AuthConfig } from '../lib/api/auth'
  import { withBase } from '../lib/basePath'
  import { formatNumber } from '../lib/utils/format'
  import PageHeader from '../lib/components/common/PageHeader.svelte'
  import { docsURL } from '../lib/routes'
  import Button from '../lib/components/common/Button.svelte'
  import Badge from '../lib/components/common/Badge.svelte'
  import Stat from '../lib/components/common/Stat.svelte'
  import Spinner from '../lib/components/common/Spinner.svelte'
  import Input from '../lib/components/common/Input.svelte'
  import FormField from '../lib/components/common/FormField.svelte'
  import Sheet from '../lib/components/common/Sheet.svelte'
  import EmptyState from '../lib/components/common/EmptyState.svelte'
  import DataTable, { type DataColumn } from '../lib/components/common/DataTable.svelte'

  type Row = Record<string, unknown> & FleetConnection & { node_count: number }

  let fleet = $state<FleetResult | null>(null)
  let loading = $state(true)
  let error = $state('')
  let filter = $state('')
  let selected = $state<FleetConnection | null>(null)
  let destination = $state('/cluster-health')
  let username = $state('')
  let password = $state('')
  let signInError = $state('')
  let signingIn = $state(false)
  let authConfig = $state<AuthConfig | null>(null)
  const connections = $derived(fleet?.connections ?? [])
  const rows = $derived<Row[]>(connections.filter(c => c.name.toLowerCase().includes(filter.toLowerCase())).map(c => ({ ...c, node_count: c.nodes.length })))
  const needsAttention = $derived(connections.filter(c => c.status !== 'healthy').length)
  const hasRegressions = $derived(connections.some(c => c.regressions !== undefined))
  const labels: Record<FleetConnection['status'], string> = { healthy: 'Healthy', warning: 'Needs attention', critical: 'Critical', offline: 'Offline', disabled: 'Collection disabled', missing: 'No samples', stale: 'Stale samples' }
  const notes: Partial<Record<FleetConnection['status'], string>> = {
    offline: 'The connector is offline. Retained measurements are historical.',
    disabled: 'Background collection is disabled. Enable it in Cluster Health settings.',
    missing: 'No health samples yet. Check background monitoring credentials and system-table permissions.',
    stale: 'No recent samples. Check collector credentials and connectivity; these values are historical.',
  }
  function tone(status: FleetConnection['status']): 'success' | 'warning' | 'danger' | 'neutral' { return status === 'healthy' ? 'success' : status === 'critical' || status === 'offline' ? 'danger' : status === 'disabled' || status === 'missing' ? 'neutral' : 'warning' }
  /** Metrics only mean something once a node has been sampled. */
  const sampled = (fmt: (n: number) => string) => (v: unknown, row: Row) => row.node_count ? fmt(Number(v)) : '—'
  const columns = $derived<DataColumn<Row>[]>([
    { key: 'name', label: 'Name', width: '22%' },
    { key: 'status', label: 'Status', width: '150px', sortValue: (r) => labels[r.status] },
    { key: 'node_count', label: 'Nodes', align: 'right', width: '70px' },
    { key: 'max_replication_delay', label: 'Replication delay', align: 'right', format: sampled(n => `${formatNumber(n)} s`) },
    { key: 'parts_pressure_pct', label: 'Parts pressure', align: 'right', format: sampled(n => `${n.toFixed(1)}%`) },
    { key: 'readonly_replicas', label: 'Readonly replicas', align: 'right', format: sampled(formatNumber) },
    { key: 'replication_queue', label: 'Replication queue', align: 'right', format: sampled(formatNumber) },
    { key: 'pending_mutations', label: 'Pending mutations', align: 'right', format: sampled(formatNumber) },
    { key: 'long_queries', label: 'Long queries', align: 'right', format: sampled(formatNumber) },
    { key: 'open_incidents', label: 'Open incidents', align: 'right', format: (v) => formatNumber(Number(v)) },
    ...(hasRegressions ? [{ key: 'regressions', label: 'Query regressions', align: 'right', format: (v: unknown) => v === undefined ? '—' : formatNumber(Number(v)) } satisfies DataColumn<Row>] : []),
    { key: 'captured_at', label: 'Sampled', width: '170px', format: (v) => v ? new Date(String(v)).toLocaleString() : 'Awaiting first sample', sortValue: (r) => r.captured_at ? new Date(r.captured_at).getTime() : 0 },
  ])
  async function load() {
    if (!fleet) loading = true
    try { fleet = await fetchFleet(); error = '' } catch (e) { error = e instanceof Error ? e.message : 'Failed to load fleet' } finally { loading = false }
  }
  function inspect(connection: FleetConnection, path: string) {
    if (connection.id === getSession()?.connectionId) { navigate(path); return }
    selected = connection; destination = path; username = ''; password = ''; signInError = ''
  }
  async function signIn() {
    if (!selected) return
    signingIn = true
    try { await login(selected.id, username, password, undefined, destination); password = ''; selected = null; navigate(destination) }
    catch (e) { signInError = e instanceof Error ? e.message : 'Sign in failed'; password = '' }
    finally { signingIn = false }
  }
  onMount(() => {
    void load()
    void getAuthConfig().then(config => authConfig = config)
    const timer = setInterval(() => { if (document.visibilityState === 'visible') void load() }, 30000)
    return () => { clearInterval(timer); password = '' }
  })
</script>

<div class="flex h-full min-h-0 flex-col">
  <PageHeader title="Fleet" subtitle="All ClickHouse environments" docs={docsURL('fleet')}>
    {#snippet actions()}<Button size="sm" variant="outline" onclick={load}><RefreshCw size={13} /> Refresh</Button>{/snippet}
  </PageHeader>
  {#if loading}<div class="flex flex-1 items-center justify-center"><Spinner /></div>
  {:else if error && !fleet}<EmptyState icon={AlertTriangle} title="Couldn't load the fleet" description={error} primary={{ label: 'Retry', onclick: load }} />
  {:else}
    {#if error}<p class="shrink-0 border-b border-edge-subtle px-4 py-2 text-[13px] text-danger" role="alert">Refresh failed: {error}. The table shows the last successful load.</p>{/if}
    <div class="grid shrink-0 grid-cols-3 gap-3 border-b border-edge-subtle px-4 py-3">
      <Stat label="Environments" value={connections.length} />
      <Stat label="Online" value={connections.filter(c => c.online).length} />
      <Stat label="Needs attention" value={needsAttention} tone={needsAttention ? 'warning' : 'success'} />
    </div>
    <div class="flex h-10 shrink-0 items-center gap-2 border-b border-edge-subtle px-4">
      <label for="fleet-filter" class="sr-only">Filter environments</label>
      <Input id="fleet-filter" size="sm" type="search" placeholder="Filter environments" class="w-64" bind:value={filter} />
      <span class="ml-auto text-xs text-fg-4">{rows.length} of {connections.length} environments · retained monitoring samples, refreshes every 30 seconds</span>
    </div>
    <div class="min-h-0 flex-1">
      <DataTable
        fill
        {columns}
        {rows}
        rowKey={(r) => r.id}
        sort={{ key: 'name', dir: 'asc' }}
        emptyTitle={filter ? 'No matching environments' : 'No connections configured'}
        emptyDescription={filter ? 'Try another name.' : undefined}
        onrowclick={(r) => inspect(r, '/cluster-health')}
      >
        {#snippet cell(row, col, value)}
          {#if col.key === 'name'}
            <span class="block truncate font-medium text-fg">{row.name}</span>
            <span class="block truncate text-xs text-fg-4">{row.cluster || 'ClickHouse environment'}</span>
          {:else if col.key === 'status'}
            <Badge tone={tone(row.status)} title={notes[row.status]}>{labels[row.status]}</Badge>
          {:else}{value}{/if}
        {/snippet}
        {#snippet actions(row)}
          <Button size="xs" variant="ghost" onclick={() => inspect(row, '/governance?section=incidents')}>Incidents</Button>
          <Button size="xs" variant="ghost" onclick={() => inspect(row, '/performance')}>Performance</Button>
        {/snippet}
      </DataTable>
    </div>
  {/if}
</div>

<Sheet open={selected !== null} title={`Inspect ${selected?.name ?? 'environment'}`} description="Sign in to this environment to inspect its live details. This switches your active connection." onclose={() => { selected = null; password = '' }}>
  {#if authConfig?.password_login !== false}
    <form id="fleet-signin" class="space-y-4" onsubmit={(e) => { e.preventDefault(); void signIn() }}>
      <FormField label="ClickHouse username" for="fleet-user"><Input id="fleet-user" bind:value={username} required autocomplete="username" /></FormField>
      <FormField label="Password" for="fleet-password"><Input id="fleet-password" type="password" bind:value={password} autocomplete="current-password" /></FormField>
      {#if signInError}<p class="text-[13px] text-danger" role="alert">{signInError}</p>{/if}
    </form>
  {:else}<p class="text-[13px] text-fg-3">Use your organization's sign-in page to select this environment.</p><a class="mt-3 inline-block text-[13px] text-accent" href={withBase('/login')}>Open sign in</a>{/if}
  {#snippet footer()}<Button size="sm" variant="ghost" onclick={() => { selected = null; password = '' }}>Cancel</Button>{#if authConfig?.password_login !== false}<Button size="sm" loading={signingIn} disabled={!username || !selected?.online} onclick={signIn}>Sign in and inspect</Button>{/if}{/snippet}
</Sheet>
