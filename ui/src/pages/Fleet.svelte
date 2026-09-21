<!-- SPDX-License-Identifier: BUSL-1.1 -->
<script lang="ts">
  import { onMount } from 'svelte'
  import { AlertTriangle, RefreshCw, Server, ArrowRight } from 'lucide-svelte'
  import { fetchFleet, type FleetResult, type FleetConnection } from '../lib/api/fleet'
  import { getSession, login } from '../lib/stores/session.svelte'
  import { navigate } from '../lib/stores/router.svelte'
  import { getAuthConfig, type AuthConfig } from '../lib/api/auth'
  import { withBase } from '../lib/basePath'
  import PageHeader from '../lib/components/common/PageHeader.svelte'
  import PageBody from '../lib/components/common/PageBody.svelte'
  import Button from '../lib/components/common/Button.svelte'
  import Badge from '../lib/components/common/Badge.svelte'
  import Panel from '../lib/components/common/Panel.svelte'
  import Stat from '../lib/components/common/Stat.svelte'
  import Spinner from '../lib/components/common/Spinner.svelte'
  import Input from '../lib/components/common/Input.svelte'
  import FormField from '../lib/components/common/FormField.svelte'
  import Sheet from '../lib/components/common/Sheet.svelte'
  import EmptyState from '../lib/components/common/EmptyState.svelte'

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
  const entries = $derived((fleet?.connections ?? []).filter(c => c.name.toLowerCase().includes(filter.toLowerCase())))
  const needsAttention = $derived((fleet?.connections ?? []).filter(c => c.status !== 'healthy').length)
  const labels: Record<FleetConnection['status'], string> = { healthy: 'Healthy', warning: 'Needs attention', critical: 'Critical', offline: 'Offline', disabled: 'Collection disabled', missing: 'No samples', stale: 'Stale samples' }
  function tone(status: FleetConnection['status']): 'success' | 'warning' | 'danger' | 'neutral' { return status === 'healthy' ? 'success' : status === 'critical' || status === 'offline' ? 'danger' : status === 'disabled' || status === 'missing' ? 'neutral' : 'warning' }
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
    try { await login(selected.id, username, password); password = ''; selected = null; navigate(destination) }
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
  <PageHeader title="Fleet" subtitle="All ClickHouse environments">
    {#snippet actions()}<Button size="sm" variant="outline" onclick={load}><RefreshCw size={13} /> Refresh</Button>{/snippet}
  </PageHeader>
  {#if loading}<div class="flex flex-1 items-center justify-center"><Spinner /></div>
  {:else if error && !fleet}<EmptyState icon={AlertTriangle} title="Couldn't load the fleet" description={error} primary={{ label: 'Retry', onclick: load }} />
  {:else}
    <PageBody width="lg">
      <div class="space-y-5">
        {#if error}<p class="text-sm text-danger" role="alert">Refresh failed: {error}. The cards below show the last successful load.</p>{/if}
        <div class="grid grid-cols-2 gap-3 md:grid-cols-3">
          <Stat label="Environments" value={fleet?.connections.length ?? 0} />
          <Stat label="Online" value={fleet?.connections.filter(c => c.online).length ?? 0} />
          <Stat label="Needs attention" value={needsAttention} tone={needsAttention ? 'warning' : 'success'} />
        </div>
        <div class="flex flex-wrap items-center justify-between gap-3">
          <label for="fleet-filter" class="sr-only">Filter environments</label><Input id="fleet-filter" type="search" placeholder="Filter environments" class="w-64" bind:value={filter} />
          <p class="text-xs text-fg-3">Retained monitoring samples · refreshes every 30 seconds</p>
        </div>
        {#if !entries.length}<EmptyState icon={Server} title={filter ? 'No matching environments' : 'No connections configured'} />{/if}
        <div class="grid gap-4 lg:grid-cols-2">
          {#each entries as connection (connection.id)}
            <Panel title={connection.name} description={connection.cluster || 'ClickHouse environment'}>
              {#snippet actions()}<Badge tone={tone(connection.status)}>{labels[connection.status]}</Badge>{/snippet}
              <div class="space-y-4">
                {#if connection.status === 'offline'}<p class="text-xs text-fg-3">The connector is offline. Retained measurements below are historical.</p>
                {:else if connection.status === 'disabled'}<p class="text-xs text-fg-3">Background collection is disabled. Enable it in Cluster Health settings.</p>
                {:else if connection.status === 'missing'}<p class="text-xs text-fg-3">No health samples yet. Check background monitoring credentials and system-table permissions.</p>
                {:else if connection.status === 'stale'}<p class="text-xs text-warning">No recent samples. Check collector credentials and connectivity; these values are historical.</p>{/if}
                <div class="grid grid-cols-3 gap-x-3 gap-y-4 text-xs">
                  <div><p class="text-fg-3">Replication delay</p><p class="mt-1 font-mono text-sm">{connection.nodes.length ? `${connection.max_replication_delay.toLocaleString()} s` : '—'}</p></div>
                  <div><p class="text-fg-3">Parts pressure</p><p class="mt-1 font-mono text-sm">{connection.nodes.length ? `${connection.parts_pressure_pct.toFixed(1)}%` : '—'}</p></div>
                  <div><p class="text-fg-3">Readonly replicas</p><p class="mt-1 font-mono text-sm">{connection.nodes.length ? connection.readonly_replicas : '—'}</p></div>
                  <div><p class="text-fg-3">Replication queue</p><p class="mt-1 font-mono text-sm">{connection.nodes.length ? connection.replication_queue : '—'}</p></div>
                  <div><p class="text-fg-3">Pending mutations</p><p class="mt-1 font-mono text-sm">{connection.nodes.length ? connection.pending_mutations : '—'}</p></div>
                  <div><p class="text-fg-3">Long queries</p><p class="mt-1 font-mono text-sm">{connection.nodes.length ? connection.long_queries : '—'}</p></div>
                  <div><p class="text-fg-3">Open incidents</p><p class="mt-1 font-mono text-sm">{connection.open_incidents}</p></div>
                  {#if connection.regressions !== undefined}<div><p class="text-fg-3">Query regressions</p><p class="mt-1 font-mono text-sm">{connection.regressions}</p></div>{/if}
                </div>
                <div class="border-t border-edge-subtle pt-3 text-[11px] text-fg-3">
                  {connection.captured_at ? `Sampled ${new Date(connection.captured_at).toLocaleString()} · ${connection.nodes.length} nodes` : 'Awaiting first sample'}
                </div>
                <div class="flex flex-wrap gap-2">
                  <Button size="sm" variant="outline" onclick={() => inspect(connection, '/cluster-health')}><ArrowRight size={12} /> Inspect health</Button>
                  <Button size="sm" variant="ghost" onclick={() => inspect(connection, '/governance?section=incidents')}>Incidents</Button>
                  <Button size="sm" variant="ghost" onclick={() => inspect(connection, '/performance')}>Performance</Button>
                </div>
              </div>
            </Panel>
          {/each}
        </div>
      </div>
    </PageBody>
  {/if}
</div>

<Sheet open={selected !== null} title={`Inspect ${selected?.name ?? 'environment'}`} description="Sign in to this environment to inspect its live details. This switches your active connection." onclose={() => { selected = null; password = '' }}>
  {#if authConfig?.password_login !== false}
    <form id="fleet-signin" class="space-y-4" onsubmit={(e) => { e.preventDefault(); void signIn() }}>
      <FormField label="ClickHouse username" for="fleet-user"><Input id="fleet-user" bind:value={username} required autocomplete="username" /></FormField>
      <FormField label="Password" for="fleet-password"><Input id="fleet-password" type="password" bind:value={password} autocomplete="current-password" /></FormField>
      {#if signInError}<p class="text-sm text-danger" role="alert">{signInError}</p>{/if}
    </form>
  {:else}<p class="text-sm text-fg-3">Use your organization's sign-in page to select this environment.</p><a class="mt-3 inline-block text-sm text-accent" href={withBase('/login')}>Open sign in</a>{/if}
  {#snippet footer()}<Button size="sm" variant="ghost" onclick={() => { selected = null; password = '' }}>Cancel</Button>{#if authConfig?.password_login !== false}<Button size="sm" loading={signingIn} disabled={!username || !selected?.online} onclick={signIn}>Sign in and inspect</Button>{/if}{/snippet}
</Sheet>
