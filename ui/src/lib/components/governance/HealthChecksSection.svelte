<!-- SPDX-License-Identifier: BUSL-1.1 -->
<script lang="ts">
  import { onMount } from 'svelte'
  import { getSession } from '../../stores/session.svelte'
  import { goTo } from '../../stores/router.svelte'
  import { success as toastSuccess, error as toastError } from '../../stores/toast.svelte'
  import { fetchAlertEvaluations, fetchHealthSettings, fetchHealthStates, saveHealthSettings } from '../../api/alerting'
  import { createAck, deleteAck, listFiring } from '../../api/alerts'
  import type {
    AlertEvaluation, FiringIncident, HealthAlertState, HealthCheckKind, HealthCheckSetting, HealthCheckSettingInput,
  } from '../../types/alerts'
  import { formatDate, formatRelativeTime } from '../../utils/format'
  import {
    HEALTH_CHECK_LABELS, HEALTH_CHECK_ORDER, bytesToGb, formatHealthValue, gbToBytes, nodeLabel, parseOptionalNumber,
    severityTone, stateTone, summarizeStates,
  } from './alerting-format'
  import { ACK_EXPIRY_OPTIONS, ackTitle, healthFiringKey, indexFiring } from './alerting-silences'
  import PageBody from '../common/PageBody.svelte'
  import SectionHeader from '../common/SectionHeader.svelte'
  import Panel from '../common/Panel.svelte'
  import DataTable, { type DataColumn } from '../common/DataTable.svelte'
  import Badge from '../common/Badge.svelte'
  import Button from '../common/Button.svelte'
  import Sheet from '../common/Sheet.svelte'
  import FormField from '../common/FormField.svelte'
  import Input from '../common/Input.svelte'
  import Select from '../common/Select.svelte'
  import Textarea from '../common/Textarea.svelte'
  import Spinner from '../common/Spinner.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import { RefreshCw } from 'lucide-svelte'

  // Built-in checks run on every Cluster Health poll. Everyone can read
  // them; only admins change thresholds (server enforced).
  type Row = Record<string, unknown>
  const asRows = <T,>(rows: T[]): Row[] => rows as unknown as Row[]
  const isAdmin = $derived(getSession()?.role === 'admin')
  // Admins and analysts acknowledge firing alerts (server enforced).
  const canAck = $derived(getSession()?.role === 'admin' || getSession()?.role === 'analyst')

  let loading = $state(true)
  let refreshing = $state(false)
  let settings = $state<HealthCheckSetting[]>([])
  let collectionEnabled = $state(true)
  let states = $state<HealthAlertState[]>([])
  let firing = $state<FiringIncident[]>([])

  // Acknowledge form, shown in place of the check detail.
  let ackTarget = $state<FiringIncident | null>(null)
  let ackComment = $state('')
  let ackExpiry = $state('')
  let ackSaving = $state(false)
  let unacking = $state<string | null>(null)

  let selectedKind = $state<HealthCheckKind | null>(null)
  let evaluations = $state<AlertEvaluation[]>([])
  let evaluationsLoading = $state(false)
  // Bumped on every open so a late response for another check is dropped.
  let detailSeq = 0

  let editing = $state(false)
  let saving = $state(false)
  let saveError = $state('')
  let form = $state({
    enabled: true,
    warn: '' as string | number,
    crit: '' as string | number,
    floorGb: '' as string | number,
    forEvals: 1 as string | number,
    resolveEvals: 1 as string | number,
  })

  const summaries = $derived(summarizeStates(states))
  const firingIdx = $derived(indexFiring(firing))
  const incidentFor = (st: HealthAlertState): FiringIncident | null =>
    firingIdx.health.get(healthFiringKey(st.connection_id, st.check_kind, st.node)) ?? null
  const ackedCount = (kind: HealthCheckKind): number => states.filter((st) => st.check_kind === kind && incidentFor(st)?.ack).length
  const ordered = $derived(
    HEALTH_CHECK_ORDER.map((k) => settings.find((s) => s.check_kind === k)).filter((s): s is HealthCheckSetting => !!s),
  )
  const selected = $derived(selectedKind ? settings.find((s) => s.check_kind === selectedKind) ?? null : null)
  const selectedStates = $derived(selectedKind ? states.filter((s) => s.check_kind === selectedKind) : [])

  function critLabel(s: HealthCheckSetting): string {
    if (s.unit === 'down') return s.enabled ? 'When down' : '-'
    const base = s.crit_threshold == null ? '' : formatHealthValue(s.crit_threshold, s.unit)
    if (s.check_kind === 'disk' && s.free_bytes_floor != null) {
      const floor = `free < ${bytesToGb(s.free_bytes_floor)} GB`
      return base ? `${base} or ${floor}` : floor
    }
    return base || '-'
  }

  const rows = $derived(asRows(ordered.map((s) => {
    const live = summaries.get(s.check_kind) ?? { firing: 0, pending: 0 }
    return {
      ...s,
      label: HEALTH_CHECK_LABELS[s.check_kind] ?? s.check_kind,
      warn: s.supports_warn ? formatHealthValue(s.warn_threshold, s.unit) : '-',
      crit: critLabel(s),
      evals: `${s.for_evaluations} / ${s.resolve_evaluations}`,
      live_rank: live.firing * 1000 + live.pending,
    }
  })))

  const columns: DataColumn<Row>[] = [
    { key: 'label', label: 'Check' },
    { key: 'enabled', label: 'Enabled', width: '110px' },
    { key: 'warn', label: 'Warn', width: '110px', sortable: false },
    { key: 'crit', label: 'Critical', width: '200px', sortable: false },
    { key: 'evals', label: 'For / Resolve', width: '120px', sortable: false },
    { key: 'live_rank', label: 'Live state', width: '170px' },
  ]

  const nodeColumns: DataColumn<Row>[] = [
    { key: 'node', label: 'Node', mono: true, format: (v) => nodeLabel(v as string | null) },
    { key: 'state', label: 'State', width: '200px' },
    { key: 'severity', label: 'Severity', width: '100px' },
    { key: 'last_value', label: 'Value', width: '110px', align: 'right', sortable: false },
    { key: 'since', label: 'Since', width: '110px', format: (v) => (v ? formatRelativeTime(v) : '-') },
  ]

  const evaluationColumns: DataColumn<Row>[] = [
    { key: 'evaluated_at', label: 'Time', mono: true, width: '170px', format: (v) => formatDate(v) },
    { key: 'node', label: 'Node', mono: true, format: (v) => nodeLabel(v as string | null) },
    { key: 'value', label: 'Value', width: '100px', align: 'right', sortable: false },
    { key: 'state', label: 'State', width: '100px' },
    { key: 'duration_ms', label: 'Took', width: '80px', align: 'right', format: (v) => `${Number(v ?? 0)} ms` },
    { key: 'error', label: 'Error', truncate: true, sortable: false, format: (v) => (v ? String(v) : '') },
  ]

  async function load(initial = false) {
    if (initial) loading = true
    else refreshing = true
    try {
      const [s, st] = await Promise.all([fetchHealthSettings(), fetchHealthStates(), loadFiring()])
      settings = s.settings
      collectionEnabled = s.collection_enabled
      states = st
    } catch (e: unknown) {
      toastError(e instanceof Error ? e.message : String(e))
    } finally {
      loading = false
      refreshing = false
    }
  }

  // Ack state only decorates the rows; a failure here keeps the checks visible.
  async function loadFiring() {
    try {
      firing = await listFiring()
    } catch (e: unknown) {
      toastError(e instanceof Error ? e.message : String(e))
    }
  }

  function openAck(inc: FiringIncident) {
    ackTarget = inc
    ackComment = ''
    ackExpiry = ''
  }

  async function submitAck() {
    if (!ackTarget?.event_id || ackSaving) return
    ackSaving = true
    try {
      await createAck({
        event_id: ackTarget.event_id,
        comment: ackComment.trim() || undefined,
        expires_in: ackExpiry ? Number(ackExpiry) : undefined,
      })
      toastSuccess('Alert acknowledged')
      ackTarget = null
      await loadFiring()
    } catch (e: unknown) {
      toastError(e instanceof Error ? e.message : String(e))
    } finally {
      ackSaving = false
    }
  }

  async function unacknowledge(ackId: string) {
    unacking = ackId
    try {
      await deleteAck(ackId)
      toastSuccess('Acknowledgement removed')
      await loadFiring()
    } catch (e: unknown) {
      toastError(e instanceof Error ? e.message : String(e))
    } finally {
      unacking = null
    }
  }

  onMount(() => {
    void load(true)
  })

  async function openCheck(kind: HealthCheckKind) {
    selectedKind = kind
    editing = false
    saveError = ''
    evaluations = []
    evaluationsLoading = true
    const seq = ++detailSeq
    try {
      const evs = await fetchAlertEvaluations('health', kind)
      if (seq === detailSeq) evaluations = evs
    } catch (e: unknown) {
      if (seq === detailSeq) toastError(e instanceof Error ? e.message : String(e))
    } finally {
      if (seq === detailSeq) evaluationsLoading = false
    }
  }

  function closeCheck() {
    if (saving || ackSaving) return
    detailSeq++
    selectedKind = null
    editing = false
    ackTarget = null
  }

  function startEdit() {
    if (!selected) return
    form = {
      enabled: selected.enabled,
      warn: selected.warn_threshold ?? '',
      crit: selected.crit_threshold ?? '',
      floorGb: bytesToGb(selected.free_bytes_floor),
      forEvals: selected.for_evaluations,
      resolveEvals: selected.resolve_evaluations,
    }
    saveError = ''
    editing = true
  }

  function validEvals(v: number | null): boolean {
    return v != null && Number.isInteger(v) && v >= 1 && v <= 20
  }

  const formError = $derived.by(() => {
    if (!selected || !editing) return ''
    const warn = parseOptionalNumber(form.warn)
    const crit = parseOptionalNumber(form.crit)
    const floor = parseOptionalNumber(form.floorGb)
    if (!validEvals(parseOptionalNumber(form.forEvals)) || !validEvals(parseOptionalNumber(form.resolveEvals))) {
      return 'Evaluations must be whole numbers from 1 to 20.'
    }
    if (selected.unit === 'down') return ''
    if ((warn != null && warn < 0) || (crit != null && crit < 0) || (floor != null && floor < 0)) return 'Thresholds cannot be negative.'
    if (selected.unit === 'percent' && ((warn != null && warn > 100) || (crit != null && crit > 100))) return 'Percentages go up to 100.'
    if (selected.supports_warn && warn != null && crit != null && warn >= crit) return 'Warn must be lower than critical.'
    return ''
  })

  function toInput(s: HealthCheckSetting): HealthCheckSettingInput {
    return {
      check_kind: s.check_kind,
      enabled: s.enabled,
      warn_threshold: s.warn_threshold,
      crit_threshold: s.crit_threshold,
      free_bytes_floor: s.free_bytes_floor,
      for_evaluations: s.for_evaluations,
      resolve_evaluations: s.resolve_evaluations,
    }
  }

  async function save() {
    if (!selected || formError || saving) return
    const sel = selected
    const edited: HealthCheckSettingInput = {
      check_kind: sel.check_kind,
      enabled: form.enabled,
      warn_threshold: sel.supports_warn && sel.unit !== 'down' ? parseOptionalNumber(form.warn) : sel.warn_threshold,
      crit_threshold: sel.unit !== 'down' ? parseOptionalNumber(form.crit) : sel.crit_threshold,
      free_bytes_floor: sel.check_kind === 'disk' ? gbToBytes(parseOptionalNumber(form.floorGb)) : sel.free_bytes_floor,
      for_evaluations: Number(form.forEvals),
      resolve_evaluations: Number(form.resolveEvals),
    }
    saving = true
    saveError = ''
    try {
      const res = await saveHealthSettings(settings.map((s) => (s.check_kind === sel.check_kind ? edited : toInput(s))))
      settings = res.settings
      collectionEnabled = res.collection_enabled
      editing = false
      toastSuccess(`${HEALTH_CHECK_LABELS[sel.check_kind]} updated`)
    } catch (e: unknown) {
      saveError = e instanceof Error ? e.message : String(e)
    } finally {
      saving = false
    }
  }

  const unitSuffix: Record<string, string> = { seconds: 'seconds', percent: '%', count: 'count' }
</script>

{#snippet liveBadge(kind: HealthCheckKind, enabled: boolean)}
  {@const live = summaries.get(kind) ?? { firing: 0, pending: 0 }}
  <div class="flex items-center gap-1">
    {#if live.firing > 0}<Badge tone="danger">{live.firing} firing</Badge>{/if}
    {#if live.pending > 0}<Badge tone="warning">{live.pending} pending</Badge>{/if}
    {#if ackedCount(kind) > 0}<Badge tone="info">{ackedCount(kind)} acknowledged</Badge>{/if}
    {#if live.firing === 0 && live.pending === 0}
      <Badge tone="neutral">{enabled ? 'OK' : 'Not evaluated'}</Badge>
    {/if}
  </div>
{/snippet}

<PageBody width="md">
  <div class="space-y-8">
    <section>
      <SectionHeader
        title="Health checks"
        description="Built-in checks evaluated on every Cluster Health poll. Events go to your alert rules."
      >
        {#snippet actions()}
          <Button icon variant="ghost" size="sm" aria-label="Refresh" title="Refresh" onclick={() => void load()}>
            <RefreshCw size={14} class={refreshing ? 'animate-spin' : ''} />
          </Button>
        {/snippet}
      </SectionHeader>

      {#if !loading && !collectionEnabled}
        <Panel variant="muted" padding="sm" class="mb-3">
          <p class="text-[13px] text-fg-2">
            Cluster Health collection is off for this connection, so these checks are not evaluated.
            <button type="button" class="font-medium text-accent hover:underline" onclick={() => goTo('cluster-health')}>Open Cluster Health</button>
            and turn collection on in its Settings.
          </p>
        </Panel>
      {/if}

      {#if loading}
        <div class="flex justify-center py-10"><Spinner /></div>
      {:else if ordered.length === 0}
        <EmptyState size="compact" title="No health checks" description="The server returned no check settings for this connection." />
      {:else}
        <DataTable
          {columns}
          {rows}
          rowKey={(r) => String(r.check_kind)}
          onrowclick={(r) => void openCheck(r.check_kind as HealthCheckKind)}
        >
          {#snippet cell(row, col, value)}
            {#if col.key === 'label'}
              <span class="font-medium text-fg">{value}</span>
            {:else if col.key === 'enabled'}
              <Badge dot tone={row.enabled ? 'success' : 'neutral'}>{row.enabled ? 'Enabled' : 'Off'}</Badge>
            {:else if col.key === 'live_rank'}
              {@render liveBadge(row.check_kind as HealthCheckKind, row.enabled === true)}
            {:else}
              {value}
            {/if}
          {/snippet}
        </DataTable>
      {/if}
    </section>
  </div>
</PageBody>

<Sheet
  open={selected !== null}
  title={selected ? HEALTH_CHECK_LABELS[selected.check_kind] : ''}
  description={ackTarget ? 'Acknowledge a firing alert.' : editing ? 'Thresholds apply to every node of this connection.' : selected?.updated_at ? `Thresholds last changed ${formatDate(selected.updated_at)}` : 'Default thresholds.'}
  size="lg"
  onclose={closeCheck}
>
  {#if selected}
    {#if ackTarget}
      <form id="health-ack-form" class="space-y-4" onsubmit={(e) => { e.preventDefault(); void submitAck() }}>
        <p class="text-[13px] text-fg-2">
          Acknowledge {HEALTH_CHECK_LABELS[selected.check_kind]} on <span class="font-mono">{nodeLabel(ackTarget.node)}</span>.
          Notifications for it stop until it resolves or the acknowledgement expires.
        </p>
        <FormField label="Comment" for="hc-ack-comment" hint="Optional. Shown to everyone who sees this alert.">
          <Textarea id="hc-ack-comment" rows={3} placeholder="Looking into it" bind:value={ackComment} />
        </FormField>
        <FormField label="Expires" for="hc-ack-expiry">
          <Select id="hc-ack-expiry" options={ACK_EXPIRY_OPTIONS} bind:value={ackExpiry} />
        </FormField>
      </form>
    {:else if editing}
      <form id="health-check-form" class="space-y-4" onsubmit={(e) => { e.preventDefault(); void save() }}>
        <label class="ds-checkbox-label">
          <input type="checkbox" class="ds-checkbox" bind:checked={form.enabled} />
          Enabled
        </label>

        {#if selected.unit !== 'down'}
          <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
            {#if selected.supports_warn}
              <FormField label={`Warn (${unitSuffix[selected.unit]})`} for="hc-warn" hint={`Empty means no warn level. ${form.warn !== '' ? formatHealthValue(parseOptionalNumber(form.warn), selected.unit) : ''}`}>
                <Input id="hc-warn" type="number" min="0" step="any" bind:value={form.warn} />
              </FormField>
            {/if}
            <FormField label={`Critical (${unitSuffix[selected.unit]})`} for="hc-crit" hint={`Empty means no critical level. ${form.crit !== '' ? formatHealthValue(parseOptionalNumber(form.crit), selected.unit) : ''}`}>
              <Input id="hc-crit" type="number" min="0" step="any" bind:value={form.crit} />
            </FormField>
            {#if selected.check_kind === 'disk'}
              <FormField label="Free space floor (GB)" for="hc-floor" hint="Critical when the emptiest disk has less free space than this. Empty turns it off.">
                <Input id="hc-floor" type="number" min="0" step="any" bind:value={form.floorGb} />
              </FormField>
            {/if}
          </div>
        {:else}
          <p class="text-[13px] text-fg-3">
            {selected.check_kind === 'connection_unreachable'
              ? 'Fires as critical when the connection does not answer the Cluster Health poll.'
              : 'Fires as critical when a node does not answer the Cluster Health poll.'}
          </p>
        {/if}

        <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
          <FormField label="Fire after" for="hc-for" hint="Consecutive breaching evaluations, 1 to 20.">
            <Input id="hc-for" type="number" min="1" max="20" step="1" bind:value={form.forEvals} />
          </FormField>
          <FormField label="Resolve after" for="hc-resolve" hint="Consecutive OK evaluations, 1 to 20.">
            <Input id="hc-resolve" type="number" min="1" max="20" step="1" bind:value={form.resolveEvals} />
          </FormField>
        </div>

        {#if formError}<p class="text-xs text-danger" role="alert">{formError}</p>{/if}
        {#if saveError}<p class="text-xs text-danger" role="alert">{saveError}</p>{/if}
      </form>
    {:else}
      <div class="space-y-5">
        <div class="flex flex-wrap items-center gap-2 text-[13px] text-fg-2">
          <Badge dot tone={selected.enabled ? 'success' : 'neutral'}>{selected.enabled ? 'Enabled' : 'Off'}</Badge>
          {#if selected.supports_warn}<span>Warn {formatHealthValue(selected.warn_threshold, selected.unit)}</span>{/if}
          <span>Critical {critLabel(selected)}</span>
          <span class="text-fg-3">Fires after {selected.for_evaluations}, resolves after {selected.resolve_evaluations} evaluations</span>
        </div>

        <Panel title="Nodes not OK" padding="none">
          {#if selectedStates.length === 0}
            <EmptyState size="compact" title="All nodes OK" description="No node is pending or firing for this check." />
          {:else}
            <DataTable columns={nodeColumns} rows={asRows(selectedStates)} rowKey={(r) => String(r.node)} framed={false}>
              {#snippet cell(row, col, value)}
                {#if col.key === 'state'}
                  {@const inc = incidentFor(row as unknown as HealthAlertState)}
                  <span class="inline-flex items-center gap-1">
                    <Badge tone={stateTone(String(row.state))}>{value}</Badge>
                    {#if inc?.ack}<Badge tone="info" title={ackTitle(inc.ack)}>Acknowledged</Badge>{/if}
                  </span>
                {:else if col.key === 'severity'}
                  {#if row.severity}<Badge tone={severityTone(String(row.severity))}>{value}</Badge>{:else}<span class="text-fg-4">-</span>{/if}
                {:else if col.key === 'last_value'}
                  {formatHealthValue(row.last_value as number | null, selected.unit)}
                {:else}
                  {value}
                {/if}
              {/snippet}
              {#snippet actions(row)}
                {@const inc = incidentFor(row as unknown as HealthAlertState)}
                {#if canAck && inc?.ack}
                  {@const ackId = inc.ack.id}
                  <Button size="xs" variant="ghost" loading={unacking === ackId} onclick={() => void unacknowledge(ackId)}>Unacknowledge</Button>
                {:else if canAck && inc?.event_id}
                  <Button size="xs" variant="ghost" onclick={() => openAck(inc)}>Acknowledge</Button>
                {/if}
              {/snippet}
            </DataTable>
          {/if}
        </Panel>

        <Panel title="Evaluation history" description="Most recent 50 evaluations." padding="none">
          {#if evaluationsLoading}
            <div class="flex justify-center py-8"><Spinner /></div>
          {:else}
            <DataTable
              columns={evaluationColumns}
              rows={asRows(evaluations)}
              rowKey={(r) => String(r.id)}
              sort={{ key: 'evaluated_at', dir: 'desc' }}
              framed={false}
              emptyTitle="No evaluations yet"
              emptyDescription="Evaluations appear after the next Cluster Health poll."
            >
              {#snippet cell(row, col, value)}
                {#if col.key === 'state'}
                  <Badge tone={stateTone(String(row.state))}>{value}</Badge>
                {:else if col.key === 'value'}
                  {formatHealthValue(row.value as number | null, selected.unit)}
                {:else if col.key === 'error'}
                  <span class="text-danger">{value}</span>
                {:else}
                  {value}
                {/if}
              {/snippet}
            </DataTable>
          {/if}
        </Panel>
      </div>
    {/if}
  {/if}
  {#snippet footer()}
    {#if ackTarget}
      <Button variant="ghost" size="sm" disabled={ackSaving} onclick={() => (ackTarget = null)}>Cancel</Button>
      <Button size="sm" type="submit" form="health-ack-form" loading={ackSaving}>Acknowledge</Button>
    {:else if editing}
      <Button variant="ghost" size="sm" disabled={saving} onclick={() => (editing = false)}>Cancel</Button>
      <Button size="sm" type="submit" form="health-check-form" loading={saving} disabled={!!formError}>Save</Button>
    {:else}
      <Button variant="ghost" size="sm" onclick={closeCheck}>Close</Button>
      {#if isAdmin}
        <Button size="sm" onclick={startEdit}>Edit thresholds</Button>
      {/if}
    {/if}
  {/snippet}
</Sheet>
