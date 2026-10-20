<!-- SPDX-License-Identifier: BUSL-1.1 -->
<script lang="ts">
  import { errorText } from '../../utils/errors'
  import Sheet from '../common/Sheet.svelte'
  import Button from '../common/Button.svelte'
  import Badge from '../common/Badge.svelte'
  import Panel from '../common/Panel.svelte'
  import FormField from '../common/FormField.svelte'
  import Input from '../common/Input.svelte'
  import Select from '../common/Select.svelte'
  import Spinner from '../common/Spinner.svelte'
  import ConfirmDialog from '../common/ConfirmDialog.svelte'
  import DataTable, { type DataColumn } from '../common/DataTable.svelte'
  import SqlEditor from '../editor/SqlEditor.svelte'
  import { goTo } from '../../stores/router.svelte'
  import { setSection } from '../../stores/nav.svelte'
  import { success as toastSuccess } from '../../stores/toast.svelte'
  import { formatDate, formatRelativeTime } from '../../utils/format'
  import {
    createSqlAlert, deleteSqlAlert, fetchAlertEvaluations, runSqlAlert, testSqlAlert, updateSqlAlert,
  } from '../../api/alerting'
  import type {
    AlertEvaluation, AlertSeverity, SqlAlert, SqlAlertComparator, SqlAlertInput, SqlAlertMode, SqlAlertTestResult,
  } from '../../types/alerts'
  import { COMPARATOR_SYMBOLS, formatNumber, formatSeconds, parseOptionalNumber, stateTone } from './alerting-format'

  /** Create / edit Sheet for a SQL threshold alert. */
  interface Props {
    open: boolean
    alert: SqlAlert | null
    /** Can save and delete this alert (writer, and creator or admin for an existing one). */
    canEdit: boolean
    /** Can test and run (any writer); a run records only when canEdit and the alert is on. */
    canRun: boolean
    onclose: () => void
    /** Saved or run: the fresh row. */
    onchanged: (alert: SqlAlert) => void
    ondeleted: (id: string) => void
  }

  let { open, alert, canEdit, canRun, onclose, onchanged, ondeleted }: Props = $props()

  /** Run now records only for the creator or an admin on an enabled alert; anything else is a dry run. */
  const runRecords = $derived(!!alert && alert.enabled && canEdit)

  type Row = Record<string, unknown>
  const asRows = <T,>(rows: T[]): Row[] => rows as unknown as Row[]

  const INTERVALS = [
    { value: '60', label: '1 minute' }, { value: '300', label: '5 minutes' }, { value: '900', label: '15 minutes' },
    { value: '3600', label: '1 hour' }, { value: '21600', label: '6 hours' }, { value: '86400', label: '24 hours' },
  ]
  const MODES: { value: SqlAlertMode; label: string }[] = [
    { value: 'value', label: 'Value: first column of the first row' },
    { value: 'rows', label: 'Row count' },
  ]
  const COMPARATORS = (Object.keys(COMPARATOR_SYMBOLS) as SqlAlertComparator[]).map((c) => ({ value: c, label: COMPARATOR_SYMBOLS[c] }))
  const SEVERITIES: { value: AlertSeverity; label: string }[] = [
    { value: 'info', label: 'Info' }, { value: 'warn', label: 'Warning' }, { value: 'error', label: 'Error' }, { value: 'critical', label: 'Critical' },
  ]

  type Form = {
    name: string
    sql: string
    mode: SqlAlertMode
    comparator: SqlAlertComparator
    threshold: string | number
    severity: AlertSeverity
    interval_seconds: number
    timeout_seconds: string | number
    for_evaluations: string | number
    resolve_evaluations: string | number
    enabled: boolean
  }

  function blank(): Form {
    return {
      name: '', sql: '', mode: 'value', comparator: 'gt', threshold: 0, severity: 'warn', interval_seconds: 300,
      timeout_seconds: 30, for_evaluations: 1, resolve_evaluations: 1, enabled: true,
    }
  }

  let form = $state<Form>(blank())
  let saving = $state(false)
  let running = $state(false)
  let testing = $state(false)
  let deleting = $state(false)
  let confirmDelete = $state(false)
  let error = $state('')
  let testError = $state('')
  let testResult = $state<SqlAlertTestResult | null>(null)
  let evaluations = $state<AlertEvaluation[]>([])
  let evaluationsLoading = $state(false)
  let evalSeq = 0

  async function loadEvaluations(id: string) {
    const seq = ++evalSeq
    evaluationsLoading = true
    try {
      const evs = await fetchAlertEvaluations('sql', id)
      if (seq === evalSeq) evaluations = evs
    } catch (e: unknown) {
      if (seq === evalSeq) error = e instanceof Error ? e.message : String(e)
    } finally {
      if (seq === evalSeq) evaluationsLoading = false
    }
  }

  // Reset the form each time the sheet opens, for the alert it opens on.
  let openedFor = $state<string | null | undefined>(undefined)
  $effect(() => {
    if (!open) {
      openedFor = undefined
      evalSeq++
      return
    }
    const id = alert?.id ?? null
    if (openedFor === id) return
    openedFor = id
    form = alert
      ? {
          name: alert.name, sql: alert.sql, mode: alert.mode, comparator: alert.comparator, threshold: alert.threshold,
          severity: alert.severity, interval_seconds: alert.interval_seconds, timeout_seconds: alert.timeout_seconds,
          for_evaluations: alert.for_evaluations, resolve_evaluations: alert.resolve_evaluations, enabled: alert.enabled,
        }
      : blank()
    error = ''
    testError = ''
    testResult = null
    evaluations = []
    if (alert) void loadEvaluations(alert.id)
  })

  const intervalOptions = $derived(
    INTERVALS.some((o) => o.value === String(form.interval_seconds))
      ? INTERVALS
      : [...INTERVALS, { value: String(form.interval_seconds), label: formatSeconds(form.interval_seconds) }],
  )

  function inRange(v: string | number, min: number, max: number): boolean {
    const n = parseOptionalNumber(v)
    return n != null && Number.isInteger(n) && n >= min && n <= max
  }

  const validationError = $derived.by(() => {
    if (parseOptionalNumber(form.threshold) == null) return 'Threshold must be a number.'
    if (!inRange(form.timeout_seconds, 5, 60)) return 'Timeout must be a whole number of seconds from 5 to 60.'
    if (!inRange(form.for_evaluations, 1, 20) || !inRange(form.resolve_evaluations, 1, 20)) return 'Evaluations must be whole numbers from 1 to 20.'
    return ''
  })
  const valid = $derived(form.name.trim() !== '' && form.sql.trim() !== '' && !validationError)

  const sentence = $derived.by(() => {
    const subject = form.mode === 'rows' ? 'the query returns' : 'the first column of the first row is'
    const what = form.mode === 'rows' ? ' rows' : ''
    const threshold = parseOptionalNumber(form.threshold)
    const runs = Number(form.for_evaluations) > 1 ? ` for ${form.for_evaluations} runs in a row` : ''
    const interval = INTERVALS.find((o) => o.value === String(form.interval_seconds))?.label ?? formatSeconds(form.interval_seconds)
    return `Fires when ${subject} ${COMPARATOR_SYMBOLS[form.comparator]} ${formatNumber(threshold)}${what}${runs}, checked every ${interval}.`
  })

  function input(): SqlAlertInput {
    return {
      name: form.name.trim(),
      sql: form.sql.trim(),
      mode: form.mode,
      comparator: form.comparator,
      threshold: Number(form.threshold),
      severity: form.severity,
      interval_seconds: Number(form.interval_seconds),
      timeout_seconds: Number(form.timeout_seconds),
      for_evaluations: Number(form.for_evaluations),
      resolve_evaluations: Number(form.resolve_evaluations),
      enabled: form.enabled,
    }
  }

  async function save() {
    if (!valid || saving || !canEdit) return
    saving = true
    error = ''
    try {
      const saved = alert ? await updateSqlAlert(alert.id, input()) : await createSqlAlert(input())
      toastSuccess(alert ? 'SQL alert updated' : 'SQL alert created')
      onchanged(saved)
      onclose()
    } catch (e: unknown) {
      error = errorText(e)
    } finally {
      saving = false
    }
  }

  async function test() {
    if (!canRun || testing || !form.sql.trim() || validationError) return
    testing = true
    testError = ''
    testResult = null
    try {
      const i = input()
      testResult = await testSqlAlert({ sql: i.sql, mode: i.mode, comparator: i.comparator, threshold: i.threshold, timeout_seconds: i.timeout_seconds })
    } catch (e: unknown) {
      testError = e instanceof Error ? e.message : String(e)
    } finally {
      testing = false
    }
  }

  async function runNow() {
    if (!alert || !canRun || running) return
    running = true
    error = ''
    try {
      const res = await runSqlAlert(alert.id)
      const outcome = `${res.firing ? 'firing' : 'not firing'}, value ${formatNumber(res.value)}`
      if (res.recorded) {
        onchanged(res.alert)
        toastSuccess(`Ran: ${outcome}`)
        void loadEvaluations(alert.id)
      } else {
        toastSuccess(`Dry run, not recorded: ${outcome}`)
      }
    } catch (e: unknown) {
      error = e instanceof Error ? e.message : String(e)
    } finally {
      running = false
    }
  }

  async function remove() {
    if (!alert || deleting) return
    deleting = true
    try {
      await deleteSqlAlert(alert.id)
      confirmDelete = false
      toastSuccess('SQL alert deleted')
      ondeleted(alert.id)
      onclose()
    } catch (e: unknown) {
      confirmDelete = false
      error = errorText(e)
    } finally {
      deleting = false
    }
  }

  function close() {
    if (saving || deleting) return
    onclose()
  }

  const previewColumns = $derived<DataColumn<Row>[]>(
    Object.keys(testResult?.rows_preview[0] ?? {}).map((k) => ({
      key: k,
      label: k,
      mono: true,
      truncate: true,
      sortable: false,
      format: (v) => (v == null ? 'NULL' : typeof v === 'object' ? JSON.stringify(v) : String(v)),
    })),
  )

  const evaluationColumns: DataColumn<Row>[] = [
    { key: 'evaluated_at', label: 'Time', mono: true, width: '170px', format: (v) => formatDate(v) },
    { key: 'value', label: 'Value', width: '110px', align: 'right', format: (v) => formatNumber(v as number | null) },
    { key: 'state', label: 'State', width: '100px' },
    { key: 'duration_ms', label: 'Took', width: '80px', align: 'right', format: (v) => `${Number(v ?? 0)} ms` },
    { key: 'error', label: 'Error', truncate: true, sortable: false, format: (v) => (v ? String(v) : '') },
  ]
</script>

<Sheet
  {open}
  title={alert ? (canEdit ? 'Edit SQL alert' : alert.name) : 'New SQL alert'}
  description="A query that runs on a schedule and raises an alert event when its result crosses a threshold."
  size="lg"
  onclose={close}
>
  <form id="sql-alert-form" class="space-y-5" onsubmit={(e) => { e.preventDefault(); void save() }}>
    {#if alert}
      <div class="flex flex-wrap items-center gap-2 text-xs text-fg-3">
        <Badge tone={stateTone(alert.state)}>{alert.state}</Badge>
        <span>Last value {formatNumber(alert.last_value)}</span>
        <span>Last run {alert.last_run_at ? formatRelativeTime(alert.last_run_at) : 'never'}</span>
        {#if alert.created_by}<span>Created by {alert.created_by}</span>{/if}
        {#if alert.error_count > 0}<span class="text-danger">{alert.error_count} consecutive {alert.error_count === 1 ? 'error' : 'errors'}</span>{/if}
      </div>
      {#if alert.last_error}
        <p class="rounded-md bg-danger-soft px-3 py-2 font-mono text-xs text-danger">{alert.last_error}</p>
      {/if}
    {/if}

    <FormField label="Name" for="sqla-name" required controlWidth="full">
      <Input id="sqla-name" placeholder="Failed inserts in the last hour" bind:value={form.name} disabled={!canEdit} required />
    </FormField>

    <FormField
      label="SQL"
      controlWidth="full"
      hint="One SELECT or WITH statement, without SETTINGS or FORMAT. CH-UI wraps it in a 101-row limit and runs it with readonly=1. After 5 consecutive errors the alert disables itself."
    >
      {#if canEdit}
        <div class="h-44 overflow-hidden rounded-md border border-edge">
          <SqlEditor value={form.sql} onchange={(v) => (form.sql = v)} onrun={() => void test()} />
        </div>
      {:else}
        <pre class="max-h-60 overflow-auto rounded-md border border-edge-subtle bg-surface-2 p-3 font-mono text-xs text-fg">{form.sql}</pre>
      {/if}
    </FormField>

    <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
      <FormField label="Measure" for="sqla-mode" controlWidth="full">
        <Select id="sqla-mode" options={MODES} value={form.mode} disabled={!canEdit} onchange={(v) => (form.mode = v as SqlAlertMode)} />
      </FormField>
      <FormField label="Condition" for="sqla-threshold" controlWidth="full">
        <div class="flex items-center gap-2">
          <span class="text-[13px] text-fg-3">{form.mode === 'rows' ? 'rows' : 'value'}</span>
          <Select class="w-20" options={COMPARATORS} value={form.comparator} disabled={!canEdit} onchange={(v) => (form.comparator = v as SqlAlertComparator)} />
          <Input id="sqla-threshold" type="number" step="any" class="w-32" bind:value={form.threshold} disabled={!canEdit} />
        </div>
      </FormField>
      <FormField label="Severity" for="sqla-severity" controlWidth="full">
        <Select id="sqla-severity" options={SEVERITIES} value={form.severity} disabled={!canEdit} onchange={(v) => (form.severity = v as AlertSeverity)} />
      </FormField>
      <FormField label="Check every" for="sqla-interval" controlWidth="full">
        <Select id="sqla-interval" options={intervalOptions} value={String(form.interval_seconds)} disabled={!canEdit} onchange={(v) => (form.interval_seconds = Number(v))} />
      </FormField>
      <FormField label="Timeout (seconds)" for="sqla-timeout" controlWidth="full" hint="5 to 60.">
        <Input id="sqla-timeout" type="number" min="5" max="60" step="1" bind:value={form.timeout_seconds} disabled={!canEdit} />
      </FormField>
      <div class="grid grid-cols-2 gap-3">
        <FormField label="Fire after" for="sqla-for" controlWidth="full" hint="Runs in a row, 1 to 20.">
          <Input id="sqla-for" type="number" min="1" max="20" step="1" bind:value={form.for_evaluations} disabled={!canEdit} />
        </FormField>
        <FormField label="Resolve after" for="sqla-resolve" controlWidth="full" hint="Runs in a row, 1 to 20.">
          <Input id="sqla-resolve" type="number" min="1" max="20" step="1" bind:value={form.resolve_evaluations} disabled={!canEdit} />
        </FormField>
      </div>
    </div>

    <label class="ds-checkbox-label">
      <input type="checkbox" class="ds-checkbox" bind:checked={form.enabled} disabled={!canEdit} />
      Enabled
    </label>

    <div class="rounded-md bg-surface-2 px-3 py-2.5 text-[13px] text-fg-2">
      {sentence}
      <p class="mt-1 text-xs text-fg-4">
        Firing creates a <span class="font-mono">sql.alert</span> event. Rules in
        <button type="button" class="text-accent hover:underline" onclick={() => { goTo('governance'); setSection('alert-rules') }}>Governance, Alert rules</button>
        decide where it is delivered.
      </p>
    </div>

    {#if validationError}<p class="text-xs text-danger" role="alert">{validationError}</p>{/if}
    {#if error}<p class="text-xs text-danger" role="alert">{error}</p>{/if}

    {#if canRun}
      <Panel title="Test run" description="Runs the SQL above with your own ClickHouse account. Nothing is saved." padding="sm">
        {#snippet actions()}
          <Button size="sm" variant="outline" loading={testing} disabled={!form.sql.trim() || !!validationError} onclick={() => void test()}>Test run</Button>
        {/snippet}
        {#if testing}
          <div class="flex justify-center py-4"><Spinner /></div>
        {:else if testError}
          <p class="font-mono text-xs text-danger" role="alert">{testError}</p>
        {:else if testResult}
          <div class="space-y-3">
            <div class="flex flex-wrap items-center gap-3 text-[13px] text-fg-2">
              <Badge tone={testResult.firing ? 'danger' : 'success'}>{testResult.firing ? 'Would fire' : 'Would not fire'}</Badge>
              <span>Value <span class="font-medium text-fg tabular-nums">{formatNumber(testResult.value)}</span></span>
              <span>Rows <span class="font-medium text-fg tabular-nums">{testResult.rows}</span></span>
              <span class="text-fg-3">{testResult.duration_ms} ms</span>
            </div>
            {#if testResult.rows_preview.length > 0}
              <DataTable columns={previewColumns} rows={testResult.rows_preview} />
            {/if}
          </div>
        {:else}
          <p class="text-xs text-fg-3">Shows the measured value, whether it would fire, and up to 10 rows.</p>
        {/if}
      </Panel>
    {/if}

    {#if alert}
      <Panel title="Evaluation history" description="Most recent 50 runs." padding="none">
        {#if evaluationsLoading}
          <div class="flex justify-center py-8"><Spinner /></div>
        {:else}
          <DataTable
            columns={evaluationColumns}
            rows={asRows(evaluations)}
            rowKey={(r) => String(r.id)}
            sort={{ key: 'evaluated_at', dir: 'desc' }}
            framed={false}
            emptyTitle="No runs yet"
          >
            {#snippet cell(row, col, value)}
              {#if col.key === 'state'}
                <Badge tone={stateTone(String(row.state))}>{value}</Badge>
              {:else if col.key === 'error'}
                <span class="text-danger">{value}</span>
              {:else}
                {value}
              {/if}
            {/snippet}
          </DataTable>
        {/if}
      </Panel>
    {/if}
  </form>
  {#snippet footer()}
    {#if alert && canEdit}
      <Button variant="danger" size="sm" class="mr-auto" disabled={saving} onclick={() => (confirmDelete = true)}>Delete</Button>
    {/if}
    {#if alert && canRun}
      <Button variant="outline" size="sm" loading={running} disabled={saving} title={runRecords ? 'Runs the saved version now and records the result' : 'Runs the saved version now without recording it: the alert is off, or you are not its creator or an admin'} onclick={() => void runNow()}>{runRecords ? 'Run now' : 'Dry run'}</Button>
    {/if}
    <Button variant="ghost" size="sm" disabled={saving} onclick={close}>{canEdit ? 'Cancel' : 'Close'}</Button>
    {#if canEdit}
      <Button size="sm" type="submit" form="sql-alert-form" loading={saving} disabled={!valid}>{alert ? 'Save changes' : 'Create alert'}</Button>
    {/if}
  {/snippet}
</Sheet>

<ConfirmDialog
  open={confirmDelete}
  title="Delete SQL alert?"
  description={alert ? `"${alert.name}" and its evaluation history will be removed.` : ''}
  confirmLabel="Delete"
  destructive
  loading={deleting}
  onconfirm={() => void remove()}
  oncancel={() => (confirmDelete = false)}
/>
