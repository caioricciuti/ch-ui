<script lang="ts">
  import { onMount } from 'svelte'
  import { apiGet, apiPut } from '../../api/client'
  import { success as toastSuccess, error as toastError } from '../../stores/toast.svelte'
  import Sheet from '../common/Sheet.svelte'
  import Button from '../common/Button.svelte'
  import Input from '../common/Input.svelte'
  import FormField from '../common/FormField.svelte'
  import Spinner from '../common/Spinner.svelte'

  let { connection, onclose }: {
    connection: { id: string; name: string }
    onclose: () => void
  } = $props()

  type Account = { worker: string; mode: string; username?: string; updated_at?: string }
  const labels: Record<string, string> = {
    schedule: 'Scheduled queries', model: 'Models', pipeline: 'Pipeline sink',
    governance: 'Governance', cluster_health: 'Cluster Health', 'telemetry.monitor': 'Telemetry monitors',
    performance: 'Performance regression monitor', 'operations.report': 'Weekly operations reports',
  }
  const permissions: Record<string, string> = {
    schedule: 'Grant only the operations required by the scheduled SQL.',
    model: 'Grant SELECT on model sources and the required CREATE, INSERT, ALTER and DROP permissions on model targets.',
    pipeline: 'Grant INSERT on pipeline targets. Grant CREATE TABLE only if the pipeline creates its destination.',
    governance: 'Grant access to the system tables used by governance and the metadata you want to collect.',
    cluster_health: 'Grant SELECT on the system tables used by Cluster Health.',
    'telemetry.monitor': 'Grant SELECT on the configured logs and traces tables.',
    performance: 'Grant SELECT on system.query_log. Unattended regression checks require a dedicated account.',
    'operations.report': 'Grant SELECT on system.query_log, system.parts and system.clusters. Weekly reports require a dedicated account.',
  }
  let accounts = $state<Account[]>([])
  let worker = $state('telemetry.monitor')
  let mode = $state('service_account')
  let username = $state('')
  let password = $state('')
  let loading = $state(true)
  let saving = $state(false)
  let loadError = $state('')
  const endpoint = $derived(`/api/connections/${encodeURIComponent(connection.id)}/background-credentials`)
  const dedicatedOnly = $derived(worker === 'performance' || worker === 'operations.report')

  function selectWorker() {
    const account = accounts.find((a) => a.worker === worker)
    mode = account?.mode ?? 'session'
    if ((worker === 'performance' || worker === 'operations.report') && mode === 'session') mode = 'disabled'
    username = account?.username ?? ''
    password = ''
  }

  onMount(async () => {
    try {
      accounts = await apiGet<Account[]>(endpoint)
      selectWorker()
    } catch (e) {
      loadError = e instanceof Error ? e.message : 'Could not load background accounts'
    } finally {
      loading = false
    }
  })

  async function save() {
    if (saving || loading || loadError) return
    saving = true
    try {
      const body = mode === 'service_account' ? { mode, username, password } : { mode }
      const saved = await apiPut<Account>(`${endpoint}/${encodeURIComponent(worker)}`, body)
      accounts = accounts.map((a) => a.worker === worker ? saved : a)
      password = ''
      toastSuccess(mode === 'service_account' ? 'Account verified and saved' : 'Background execution updated')
    } catch (e) {
      toastError(e instanceof Error ? e.message : 'Could not save background account')
    } finally {
      saving = false
    }
  }
</script>

<Sheet open title={`Background accounts · ${connection.name}`} description="Choose which ClickHouse account each unattended job uses." size="lg" onclose={() => { if (!saving) onclose() }}>
  {#if loading}
    <Spinner />
  {:else if loadError}
    <p role="alert" class="text-sm text-error">{loadError}</p>
  {:else}
    <form id="background-accounts-form" class="space-y-5" onsubmit={(e) => { e.preventDefault(); void save() }}>
      <FormField label="Background worker" for="background-worker">
        <select id="background-worker" class="ds-input w-full" bind:value={worker} onchange={selectWorker} disabled={saving}>
          {#each accounts as account}<option value={account.worker}>{labels[account.worker]}</option>{/each}
        </select>
      </FormField>
      <FormField label="Run using" for="background-mode">
        <select id="background-mode" class="ds-input w-full" bind:value={mode} disabled={saving}>
          <option value="service_account">Dedicated ClickHouse account</option>
          {#if !dedicatedOnly}<option value="session">An active user session</option>{/if}
          <option value="disabled">Disabled</option>
        </select>
      </FormField>
      {#if mode === 'service_account'}
        <p class="text-[13px] text-fg-3">Runs continue after everyone signs out. {permissions[worker]} {#if dedicatedOnly}Administrators configure these read-only checks and can view their saved results.{:else}All administrators and analysts in this CH-UI instance can use this account's grants through shared jobs, including jobs on other connections.{/if}</p>
        <FormField label="ClickHouse username" for="background-user" required>
          <Input id="background-user" bind:value={username} autocomplete="off" disabled={saving} maxlength={256} required />
        </FormField>
        <FormField label="ClickHouse password" for="background-password" hint="Enter the password each time you save. An empty value explicitly sets an empty password.">
          <Input id="background-password" type="password" bind:value={password} autocomplete="new-password" disabled={saving} />
        </FormField>
        <p class="text-xs text-fg-3">Saving verifies sign-in and replaces the stored account for the next run. Existing runs finish with the credentials they already received. To revoke access for future runs, choose Disabled.</p>
      {:else if mode === 'session'}
        <p class="text-[13px] text-fg-3">Uses credentials from an active session on this connection. Someone must stay signed in. Saving removes this worker's dedicated account.</p>
      {:else}
        <p class="text-[13px] text-fg-3">Prevents this worker from obtaining credentials and removes its saved account. It will not borrow a user session. Runs already in progress may finish.</p>
      {/if}
      {#if worker === 'schedule' || worker === 'telemetry.monitor' || worker === 'operations.report' || worker === 'performance'}
        <p class="text-xs text-fg-3">Manual runs use the signed-in user's ClickHouse account and remain available when background execution is disabled.</p>
      {:else if worker === 'model'}
        <p class="text-xs text-fg-3">This setting also applies to manual model runs. Disabled prevents both scheduled and manual model execution.</p>
      {/if}
    </form>
  {/if}
  {#snippet footer()}
    <Button variant="ghost" size="sm" disabled={saving} onclick={onclose}>Close</Button>
    <Button size="sm" type="submit" form="background-accounts-form" loading={saving} disabled={loading || !!loadError || (mode === 'service_account' && !username.trim())}>
      {mode === 'service_account' ? 'Verify and save' : 'Save'}
    </Button>
  {/snippet}
</Sheet>
