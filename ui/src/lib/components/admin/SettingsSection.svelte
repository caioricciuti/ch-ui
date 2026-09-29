<script lang="ts">
  import { onMount } from 'svelte'
  import { fetchRetentionSettings, updateRetentionSettings } from '../../api/retention'
  import type { RetentionConfig, RetentionSettings } from '../../api/retention'
  import { success as toastSuccess, error as toastError } from '../../stores/toast.svelte'
  import PageBody from '../common/PageBody.svelte'
  import SectionHeader from '../common/SectionHeader.svelte'
  import Panel from '../common/Panel.svelte'
  import FormField from '../common/FormField.svelte'
  import Input from '../common/Input.svelte'
  import Button from '../common/Button.svelte'
  import Badge from '../common/Badge.svelte'
  import Sheet from '../common/Sheet.svelte'
  import Spinner from '../common/Spinner.svelte'
  import DataTable, { type DataColumn } from '../common/DataTable.svelte'
  import { Pencil, RotateCcw } from 'lucide-svelte'
  import SSOSettingsSection from './SSOSettingsSection.svelte'

  // Admin > Settings: instance-wide knobs. Retention prunes the SQLite
  // history tables; SSO wires an OIDC provider to CH-UI roles. Both show
  // their current state on the page and edit through a Sheet.

  // ── Data retention ─────────────────────────────────────────
  type RetentionKey = keyof RetentionConfig
  const retentionTables: Array<{ key: RetentionKey; label: string; holds: string }> = [
    { key: 'audit_logs', label: 'Audit logs', holds: 'Every admin and query action, by user and IP' },
    { key: 'alert_events', label: 'Alert events', holds: 'Alerts that fired and what they said' },
    { key: 'alert_dispatch_jobs', label: 'Alert dispatch jobs', holds: 'Delivery attempts per channel' },
    { key: 'schedule_runs', label: 'Schedule runs', holds: 'Each run of a scheduled query' },
    { key: 'pipeline_runs', label: 'Pipeline runs', holds: 'Each pipeline execution and its outcome' },
    { key: 'pipeline_run_logs', label: 'Pipeline run logs', holds: 'Line-by-line output of pipeline runs' },
    { key: 'model_runs', label: 'Model runs', holds: 'Each model build in dependency order' },
    { key: 'model_run_results', label: 'Model run results', holds: 'Per-model result of a run' },
    { key: 'github_sync_logs', label: 'GitHub sync logs', holds: 'What each repository sync changed' },
    { key: 'gov_schema_changes', label: 'Schema changes', holds: 'Governance record of table and column changes' },
  ]

  type RetentionRow = {
    key: RetentionKey
    label: string
    holds: string
    days: number
    defaultDays: number
    deletedLastRun: number
  }

  let retention = $state<RetentionSettings | null>(null)
  let retentionLoading = $state(false)
  let retentionSaving = $state(false)
  let retentionSheetOpen = $state(false)
  let retentionDraft = $state<RetentionConfig | null>(null)

  const retentionRows = $derived.by<RetentionRow[]>(() => {
    if (!retention) return []
    const r = retention
    return retentionTables.map((t) => ({
      key: t.key,
      label: t.label,
      holds: t.holds,
      days: r.config[t.key],
      defaultDays: r.defaults[t.key],
      deletedLastRun: r.last_run?.rows_deleted[t.key] ?? 0,
    }))
  })

  const retentionColumns: DataColumn<RetentionRow>[] = [
    { key: 'label', label: 'Table', sortable: false },
    { key: 'days', label: 'Retention', width: '180px', sortable: false },
    { key: 'defaultDays', label: 'Default', width: '110px', align: 'right', sortable: false, format: (v) => `${Number(v)} d` },
    { key: 'deletedLastRun', label: 'Last cleanup', width: '130px', align: 'right', sortable: false, format: (v) => (Number(v) > 0 ? `${Number(v).toLocaleString()} rows` : '—') },
  ]

  const retentionDirty = $derived(
    !!retention && !!retentionDraft && JSON.stringify(retentionDraft) !== JSON.stringify(retention.config),
  )

  const lastCleanupSummary = $derived.by(() => {
    const run = retention?.last_run
    if (!run) return 'No cleanup yet'
    const at = new Date(run.last_run_at).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
    return `Last cleanup ${at} · ${run.total_deleted.toLocaleString()} row${run.total_deleted === 1 ? '' : 's'}`
  })

  async function loadRetention() {
    retentionLoading = true
    try {
      retention = await fetchRetentionSettings()
    } catch (e: any) {
      toastError(e.message)
    } finally {
      retentionLoading = false
    }
  }

  function openRetentionSheet() {
    if (!retention) return
    retentionDraft = { ...retention.config }
    retentionSheetOpen = true
  }

  function resetRetentionDraft() {
    if (!retention) return
    retentionDraft = { ...retention.defaults }
  }

  async function saveRetention() {
    if (!retentionDraft) return
    for (const t of retentionTables) {
      const v = Number(retentionDraft[t.key])
      if (!Number.isFinite(v) || v < 0 || v > 3650) {
        toastError(`${t.label}: enter 0 to 3650 days`)
        return
      }
      retentionDraft[t.key] = Math.round(v)
    }
    retentionSaving = true
    try {
      retention = await updateRetentionSettings(retentionDraft)
      retentionSheetOpen = false
      toastSuccess('Retention settings saved')
    } catch (e: any) {
      toastError(e.message)
    } finally {
      retentionSaving = false
    }
  }

  onMount(() => {
    void loadRetention()
  })
</script>

<PageBody width="md">
  <div class="space-y-8">
    <!-- ── Data retention ─────────────────────────────────── -->
    <section>
      <SectionHeader
        title="Data retention"
        description="History tables are pruned automatically after the configured number of days to keep the metadata database small. 0 keeps rows forever."
      >
        {#snippet actions()}
          {#if retention}
            <Badge title={retention.last_run ? new Date(retention.last_run.last_run_at).toLocaleString() : undefined}>{lastCleanupSummary}</Badge>
            <Button size="sm" variant="outline" onclick={openRetentionSheet}>
              <Pencil size={13} /> Edit retention
            </Button>
          {/if}
        {/snippet}
      </SectionHeader>

      {#if retentionLoading}
        <div class="flex items-center justify-center py-6"><Spinner /></div>
      {:else if retention}
        <div class="space-y-3">
          <DataTable columns={retentionColumns} rows={retentionRows} rowKey={(r) => r.key} emptyTitle="No tables">
            {#snippet cell(row, col, value)}
              {#if col.key === 'label'}
                <div class="min-w-0">
                  <div class="font-medium text-fg">{row.label}</div>
                  <div class="truncate text-xs text-fg-3">{row.holds}</div>
                </div>
              {:else if col.key === 'days'}
                <span class="inline-flex items-center gap-2">
                  {#if row.days === 0}
                    <Badge>Forever</Badge>
                  {:else}
                    <Badge tone="brand">{row.days} day{row.days === 1 ? '' : 's'}</Badge>
                  {/if}
                  {#if row.days === row.defaultDays}
                    <span class="text-[11px] text-fg-4">default</span>
                  {/if}
                </span>
              {:else}
                <span class="text-fg-3">{value}</span>
              {/if}
            {/snippet}
          </DataTable>

          <Panel variant="muted" padding="sm">
            <p class="text-xs text-fg-3">
              Cleanup runs every hour.
              {#if retention.last_run}
                Last run {new Date(retention.last_run.last_run_at).toLocaleString()} ·
                {retention.last_run.total_deleted.toLocaleString()} row{retention.last_run.total_deleted === 1 ? '' : 's'} deleted in {retention.last_run.duration_ms} ms.
                {#if retention.last_run.last_error}<span class="ml-1 text-danger">{retention.last_run.last_error}</span>{/if}
              {:else}
                No cleanup has run yet.
              {/if}
            </p>
          </Panel>
        </div>
      {:else}
        <p class="text-[13px] text-fg-3">Could not load retention settings.</p>
      {/if}
    </section>

    <!-- ── Single sign-on ─────────────────────────────────── -->
    <SSOSettingsSection />
  </div>
</PageBody>

<!-- Edit retention -->
<Sheet
  open={retentionSheetOpen}
  title="Edit retention"
  description="Days to keep rows in each history table. 0 keeps them forever."
  size="md"
  onclose={() => (retentionSheetOpen = false)}
>
  {#if retentionDraft}
    <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
      {#each retentionTables as t (t.key)}
        <FormField label={t.label} for={'retention-' + t.key} hint={t.holds} controlWidth="full">
          <div class="flex items-center gap-2">
            <Input id={'retention-' + t.key} class="tabular-nums" type="number" min={0} max={3650} bind:value={retentionDraft[t.key]} />
            <span class="shrink-0 text-xs text-fg-3">days</span>
          </div>
        </FormField>
      {/each}
    </div>
  {/if}
  {#snippet footer()}
    <div class="flex w-full items-center gap-2">
      <Button size="sm" variant="ghost" onclick={resetRetentionDraft}>
        <RotateCcw size={13} /> Reset to defaults
      </Button>
      <div class="ml-auto flex items-center gap-2">
        <Button size="sm" variant="ghost" onclick={() => (retentionSheetOpen = false)}>Cancel</Button>
        <Button size="sm" onclick={() => saveRetention()} loading={retentionSaving} disabled={!retentionDirty}>Save changes</Button>
      </div>
    </div>
  {/snippet}
</Sheet>

