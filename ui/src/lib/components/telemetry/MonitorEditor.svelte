<script lang="ts">
  import Sheet from '../common/Sheet.svelte'
  import Button from '../common/Button.svelte'
  import FormField from '../common/FormField.svelte'
  import Input from '../common/Input.svelte'
  import Select from '../common/Select.svelte'
  import Tabs from '../common/Tabs.svelte'
  import { goTo } from '../../stores/router.svelte'
  import { setSection } from '../../stores/nav.svelte'
  import type { Monitor, MonitorInput, SearchKind, MonitorComparator, MonitorSeverity, TelemetrySource } from '../../types/telemetry'

  /** Create / edit Sheet for a monitor: a saved query with a threshold. */
  interface Props {
    open: boolean
    monitor: Monitor | null
    sources: TelemetrySource[]
    saving: boolean
    onsave: (input: MonitorInput) => void
    oncancel: () => void
  }

  let { open, monitor, sources, saving, onsave, oncancel }: Props = $props()

  const WINDOWS = [
    { value: '60', label: '1 minute' }, { value: '300', label: '5 minutes' }, { value: '900', label: '15 minutes' },
    { value: '3600', label: '1 hour' }, { value: '21600', label: '6 hours' }, { value: '86400', label: '24 hours' },
  ]
  const INTERVALS = [
    { value: '30', label: '30 seconds' }, { value: '60', label: '1 minute' }, { value: '300', label: '5 minutes' },
    { value: '900', label: '15 minutes' }, { value: '3600', label: '1 hour' },
  ]
  const COMPARATORS: { value: MonitorComparator; label: string; words: string }[] = [
    { value: 'gt', label: '>', words: 'more than' },
    { value: 'gte', label: '>=', words: 'at least' },
    { value: 'lt', label: '<', words: 'fewer than' },
    { value: 'lte', label: '<=', words: 'at most' },
  ]
  const SEVERITIES: { value: MonitorSeverity; label: string }[] = [
    { value: 'info', label: 'Info' }, { value: 'warn', label: 'Warning' }, { value: 'error', label: 'Error' }, { value: 'critical', label: 'Critical' },
  ]

  function blank(): MonitorInput {
    return {
      name: '', kind: 'logs', source_id: '', query: '', window_seconds: 300, interval_seconds: 60,
      comparator: 'gt', threshold: 10, severity: 'warn', enabled: true,
    }
  }

  let form = $state<MonitorInput>(blank())

  $effect(() => {
    if (!open) return
    form = monitor
      ? {
          name: monitor.name, kind: monitor.kind, source_id: monitor.source_id, query: monitor.query,
          window_seconds: monitor.window_seconds, interval_seconds: monitor.interval_seconds,
          comparator: monitor.comparator, threshold: monitor.threshold, severity: monitor.severity, enabled: monitor.enabled,
        }
      : blank()
  })

  const kindSources = $derived(sources.filter((s) => s.kind === form.kind && s.enabled))

  $effect(() => {
    if (!kindSources.some((s) => s.id === form.source_id)) form.source_id = kindSources[0]?.id ?? ''
  })

  function label(list: { value: string; label: string }[], v: string | number): string {
    return list.find((o) => o.value === String(v))?.label ?? String(v)
  }

  const sentence = $derived.by(() => {
    const cmp = COMPARATORS.find((c) => c.value === form.comparator)?.words ?? 'more than'
    const what = form.kind === 'logs' ? 'matching logs' : 'matching traces'
    return `Fires when ${cmp} ${form.threshold} ${what} appear in any ${label(WINDOWS, form.window_seconds).replace(/^1 /, '')} window, checked every ${label(INTERVALS, form.interval_seconds)}.`
  })

  const valid = $derived(form.name.trim() !== '' && form.source_id !== '' && Number.isFinite(form.threshold))

  function submit() {
    if (!valid) return
    onsave({ ...form, name: form.name.trim(), query: form.query.trim(), threshold: Number(form.threshold) })
  }
</script>

<Sheet {open} title={monitor ? 'Edit monitor' : 'New monitor'} description="A search that runs on a schedule and raises an alert event when its count crosses a threshold." size="lg" onclose={oncancel}>
  <form class="space-y-5" onsubmit={(e) => { e.preventDefault(); submit() }}>
    <FormField label="Name" for="mon-name" required controlWidth="full">
      <Input id="mon-name" placeholder="Checkout errors spike" bind:value={form.name} required />
    </FormField>

    <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
      <FormField label="Signal" for="mon-kind" controlWidth="full">
        <Tabs variant="segmented" size="sm" items={[{ id: 'logs', label: 'Logs' }, { id: 'traces', label: 'Traces' }]} value={form.kind} onchange={(id) => (form.kind = id as SearchKind)} />
      </FormField>
      <FormField label="Source" for="mon-source" required controlWidth="full">
        <Select id="mon-source" options={kindSources.map((s) => ({ value: s.id, label: s.name }))} placeholder={kindSources.length ? undefined : 'No source of this kind'} bind:value={form.source_id} disabled={kindSources.length === 0} />
      </FormField>
    </div>

    <FormField label="Query" for="mon-query" controlWidth="full" hint="Same syntax as the search bar. Empty counts everything.">
      <Input id="mon-query" mono placeholder={form.kind === 'logs' ? 'level:error service:checkout' : 'status:error duration:>500'} bind:value={form.query} spellcheck={false} autocomplete="off" />
    </FormField>

    <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
      <FormField label="Window" for="mon-window" controlWidth="full" hint="How far back each check looks.">
        <Select id="mon-window" options={WINDOWS} value={String(form.window_seconds)} onchange={(v) => (form.window_seconds = Number(v))} />
      </FormField>
      <FormField label="Check every" for="mon-interval" controlWidth="full">
        <Select id="mon-interval" options={INTERVALS} value={String(form.interval_seconds)} onchange={(v) => (form.interval_seconds = Number(v))} />
      </FormField>
    </div>

    <FormField label="Condition" for="mon-threshold" controlWidth="full" hint="Compared against the number of matching rows in the window.">
      <div class="flex items-center gap-2">
        <span class="text-[13px] text-fg-3">count</span>
        <Select size="md" class="w-24" options={COMPARATORS.map((c) => ({ value: c.value, label: c.label }))} value={form.comparator} onchange={(v) => (form.comparator = v as MonitorComparator)} />
        <Input id="mon-threshold" type="number" min="0" step="1" class="w-32" bind:value={form.threshold} />
      </div>
    </FormField>

    <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
      <FormField label="Severity" for="mon-severity" controlWidth="full">
        <Select id="mon-severity" options={SEVERITIES} value={form.severity} onchange={(v) => (form.severity = v as MonitorSeverity)} />
      </FormField>
      <label class="ds-checkbox-label mt-6">
        <input type="checkbox" class="ds-checkbox" bind:checked={form.enabled} />
        Enabled
      </label>
    </div>

    <div class="rounded-md bg-surface-2 px-3 py-2.5 text-[13px] text-fg-2">
      {sentence}
      <p class="mt-1 text-xs text-fg-4">
        Firing creates a <span class="font-mono">telemetry.monitor</span> alert event. Rules in
        <button type="button" class="text-accent hover:underline" onclick={() => { goTo('governance'); setSection('alerts') }}>Governance › Alerts</button>
        decide where it is delivered.
      </p>
    </div>
    <button type="submit" class="hidden" aria-hidden="true"></button>
  </form>
  {#snippet footer()}
    <Button variant="ghost" size="sm" onclick={oncancel}>Cancel</Button>
    <Button size="sm" loading={saving} disabled={!valid} onclick={submit}>{monitor ? 'Save changes' : 'Create monitor'}</Button>
  {/snippet}
</Sheet>
