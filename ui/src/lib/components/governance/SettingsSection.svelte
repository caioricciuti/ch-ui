<script lang="ts">
  import { onMount } from 'svelte'
  import {
    fetchGovernanceSettings,
    updateGovernanceSettings,
    fetchQueryHarvestSettings,
    updateQueryHarvestMode,
  } from '../../api/governance'
  import type { GovernanceSettings, QueryHarvestMode, QueryHarvestSettings } from '../../types/governance'
  import { getSession } from '../../stores/session.svelte'
  import { success as toastSuccess, error as toastError } from '../../stores/toast.svelte'
  import { formatDate } from '../../utils/format'
  import PageBody from '../common/PageBody.svelte'
  import SectionHeader from '../common/SectionHeader.svelte'
  import Panel from '../common/Panel.svelte'
  import FormField from '../common/FormField.svelte'
  import Select from '../common/Select.svelte'
  import Badge from '../common/Badge.svelte'
  import Button from '../common/Button.svelte'
  import Spinner from '../common/Spinner.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import ConfirmDialog from '../common/ConfirmDialog.svelte'

  // Governance settings page: the sync switch, the query-harvest mode and
  // the disclosure of what the syncer touches. Owns its own state so the
  // page only has to mount it.

  let govSettings = $state<GovernanceSettings | null>(null)
  let govSettingsLoading = $state(false)
  let govToggleSaving = $state(false)
  let showEnableConfirm = $state(false)
  let showDisableConfirm = $state(false)

  let queryHarvest = $state<QueryHarvestSettings | null>(null)
  let harvestModeSaving = $state(false)

  const isAdmin = $derived(getSession()?.role === 'admin')

  const harvestModeOptions = [
    { value: 'auto', label: 'Auto (only with policies)' },
    { value: 'always', label: 'Always' },
    { value: 'off', label: 'Off' },
  ]

  const harvestModeHint: Record<QueryHarvestMode, string> = {
    auto: 'Queries are harvested only while at least one policy exists, so the log stays empty until there is something to check them against.',
    always: 'Every query that passes the filters is harvested, whether or not a policy exists.',
    off: 'Nothing is harvested. The query audit stays empty and policies cannot detect violations.',
  }

  onMount(() => {
    void loadGovernanceSettings()
    void loadHarvestSettings()
  })

  async function loadGovernanceSettings() {
    govSettingsLoading = true
    try {
      govSettings = await fetchGovernanceSettings()
    } catch (err: any) {
      toastError('Failed to load governance settings: ' + err.message)
    } finally {
      govSettingsLoading = false
    }
  }

  async function loadHarvestSettings() {
    // Informational; a failure must not block the page.
    try {
      queryHarvest = await fetchQueryHarvestSettings()
    } catch {
      queryHarvest = null
    }
  }

  async function persistGovernanceSettings(payload: { sync_enabled?: boolean; banner_dismissed?: boolean }) {
    govToggleSaving = true
    try {
      govSettings = await updateGovernanceSettings(payload)
      if (payload.sync_enabled === true) toastSuccess('Governance sync enabled')
      if (payload.sync_enabled === false) toastSuccess('Governance sync disabled')
    } catch (err: any) {
      toastError('Failed to update governance settings: ' + err.message)
    } finally {
      govToggleSaving = false
    }
  }

  async function confirmEnableGovernanceSync() {
    showEnableConfirm = false
    await persistGovernanceSettings({ sync_enabled: true })
  }

  async function confirmDisableGovernanceSync() {
    showDisableConfirm = false
    await persistGovernanceSettings({ sync_enabled: false })
  }

  async function changeHarvestMode(mode: QueryHarvestMode) {
    harvestModeSaving = true
    try {
      queryHarvest = await updateQueryHarvestMode(mode)
      toastSuccess(`Query harvest mode set to "${mode}"`)
    } catch (err: any) {
      toastError('Failed to update harvest mode: ' + err.message)
    } finally {
      harvestModeSaving = false
    }
  }
</script>

<PageBody width="md">
  <div class="space-y-8">
    <section>
      <SectionHeader
        title="Background sync"
        description="The syncer polls your ClickHouse cluster every 5 minutes and keeps the governance data current. Off by default."
      />
      <Panel>
        {#if govSettingsLoading && !govSettings}
          <div class="flex items-center justify-center py-6"><Spinner /></div>
        {:else if govSettings}
          <FormField
            layout="row"
            label="Collect metadata, query history and access data"
            hint={govSettings.updated_at
              ? `Last changed ${formatDate(govSettings.updated_at)}${govSettings.updated_by ? ` by ${govSettings.updated_by}` : ''}`
              : 'Never configured.'}
          >
            <div class="flex flex-wrap items-center justify-between gap-3">
              <div class="flex items-center gap-1.5">
                {#if govSettings.sync_enabled}
                  <Badge tone="success">Enabled</Badge>
                {:else}
                  <Badge tone="neutral">Disabled</Badge>
                {/if}
                {#if govSettings.syncer_running}
                  <Badge tone="info">Running</Badge>
                {:else if govSettings.sync_enabled}
                  <Badge tone="warning">Idle</Badge>
                {/if}
              </div>
              {#if govSettings.sync_enabled}
                <Button size="sm" variant="outline" loading={govToggleSaving} onclick={() => (showDisableConfirm = true)}>Disable sync</Button>
              {:else}
                <Button size="sm" loading={govToggleSaving} onclick={() => (showEnableConfirm = true)}>Enable sync</Button>
              {/if}
            </div>
          </FormField>
        {:else}
          <EmptyState size="compact" title="Settings unavailable" />
        {/if}
      </Panel>
    </section>

    <section>
      <SectionHeader
        title="Query harvesting"
        description="Which queries from system.query_log are kept for the query audit and policy checks."
      />
      <Panel>
        {#if queryHarvest}
          <FormField
            layout="row"
            label="Harvest mode"
            hint={harvestModeHint[queryHarvest.mode]}
            controlWidth="md"
          >
            <div class="flex flex-wrap items-center justify-between gap-3">
              <div class="w-full sm:w-56">
                <Select
                  options={harvestModeOptions}
                  value={queryHarvest.mode}
                  disabled={!isAdmin || harvestModeSaving}
                  onchange={(v) => void changeHarvestMode(v as QueryHarvestMode)}
                />
              </div>
              <div class="flex items-center gap-1.5">
                {#if queryHarvest.harvesting}
                  <Badge tone="success">Harvesting</Badge>
                {:else}
                  <Badge tone="neutral">Paused</Badge>
                {/if}
                <Badge tone="neutral">{queryHarvest.policy_count} {queryHarvest.policy_count === 1 ? 'policy' : 'policies'}</Badge>
              </div>
            </div>
          </FormField>
        {:else}
          <EmptyState size="compact" title="Harvest settings unavailable" />
        {/if}
      </Panel>
    </section>

    <section>
      <SectionHeader title="What sync does" description="Read this before enabling it." />
      <Panel>
        <div class="grid gap-x-8 gap-y-6 md:grid-cols-2">
          <div>
            <h3 class="mb-1.5 text-[13px] font-semibold text-fg">What it collects</h3>
            <ul class="list-disc space-y-1.5 pl-5 text-[13px] leading-relaxed text-fg-2">
              <li>Table and column metadata from <code class="rounded-sm bg-surface-2 px-1 font-mono text-xs">system.tables</code> and <code class="rounded-sm bg-surface-2 px-1 font-mono text-xs">system.columns</code>.</li>
              <li>Recent queries from <code class="rounded-sm bg-surface-2 px-1 font-mono text-xs">system.query_log</code>, filtered to 10 ms and above, without its own polls.</li>
              <li>Users, roles and grants from <code class="rounded-sm bg-surface-2 px-1 font-mono text-xs">system.users</code> and <code class="rounded-sm bg-surface-2 px-1 font-mono text-xs">system.grants</code>.</li>
            </ul>
          </div>
          <div>
            <h3 class="mb-1.5 text-[13px] font-semibold text-fg">Where it is stored</h3>
            <p class="text-[13px] leading-relaxed text-fg-2">
              In the local SQLite database at <code class="rounded-sm bg-surface-2 px-1 font-mono text-xs">./data/ch-ui.db</code>. Nothing leaves this server.
            </p>
          </div>
          <div>
            <h3 class="mb-1.5 text-[13px] font-semibold text-fg">How it authenticates</h3>
            <p class="text-[13px] leading-relaxed text-fg-2">
              It borrows ClickHouse credentials from an active admin session. Each borrow is written to the audit log as
              <code class="rounded-sm bg-surface-2 px-1 font-mono text-xs">governance.credential_borrow</code>, at most once per connection per hour.
            </p>
          </div>
          <div>
            <h3 class="mb-1.5 text-[13px] font-semibold text-fg">Retention</h3>
            <p class="text-[13px] leading-relaxed text-fg-2">
              A 30-day rolling window. Older query log and violation rows are pruned at startup and every 5 minutes.
            </p>
          </div>
        </div>
      </Panel>
    </section>

    <p class="text-[13px] leading-relaxed text-fg-3">
      Every toggle is written to the audit log as <code class="rounded-sm bg-surface-2 px-1 font-mono text-xs">governance.sync_toggle</code>.
      Disabling stops the syncer immediately; the data collected so far is kept.
    </p>
  </div>
</PageBody>

<ConfirmDialog
  open={showEnableConfirm}
  title="Enable governance sync?"
  description="The syncer will poll your ClickHouse cluster every 5 minutes, borrowing credentials from your admin session. Each borrow is recorded in the audit log. Make sure you have read the disclosure on this page."
  confirmLabel="I understand, enable"
  loading={govToggleSaving}
  onconfirm={confirmEnableGovernanceSync}
  oncancel={() => (showEnableConfirm = false)}
/>

<ConfirmDialog
  open={showDisableConfirm}
  title="Disable governance sync?"
  description="The syncer stops immediately. Data collected so far is kept and stays visible."
  confirmLabel="Disable"
  destructive
  loading={govToggleSaving}
  onconfirm={confirmDisableGovernanceSync}
  oncancel={() => (showDisableConfirm = false)}
/>
