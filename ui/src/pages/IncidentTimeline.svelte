<!-- SPDX-License-Identifier: BUSL-1.1 -->
<script lang="ts">
  import { errorText } from '../lib/utils/errors'
  import { isInModal } from '../lib/utils/shortcuts'
  import { onMount } from 'svelte'
  import { RefreshCw, Plus, AlertTriangle } from 'lucide-svelte'
  import PageHeader from '../lib/components/common/PageHeader.svelte'
  import { docsURL } from '../lib/routes'
  import Panel from '../lib/components/common/Panel.svelte'
  import Button from '../lib/components/common/Button.svelte'
  import Badge from '../lib/components/common/Badge.svelte'
  import Input from '../lib/components/common/Input.svelte'
  import Textarea from '../lib/components/common/Textarea.svelte'
  import FormField from '../lib/components/common/FormField.svelte'
  import Sheet from '../lib/components/common/Sheet.svelte'
  import Spinner from '../lib/components/common/Spinner.svelte'
  import EmptyState from '../lib/components/common/EmptyState.svelte'
  import TimeRangeSelector from '../lib/components/dashboard/TimeRangeSelector.svelte'
  import TimelineList from '../lib/components/incident-timeline/TimelineList.svelte'
  import { resolveRange, formatFull } from '../lib/components/telemetry/time'
  import { encodeAbsoluteDashboardRange } from '../lib/utils/dashboard-time'
  import { formatNumber } from '../lib/utils/format'
  import { fetchIncidentTimeline, createDeploymentAnnotation, type IncidentTimeline, type TimelineEvent, type TimelineSource } from '../lib/api/incidentTimeline'
  import { getIncident, fetchIncidentComments } from '../lib/api/governance'
  import type { GovernanceIncident, GovernanceIncidentComment } from '../lib/types/governance'
  import { getSession } from '../lib/stores/session.svelte'
  import { success as toastSuccess, errorFrom as toastErrorFrom } from '../lib/stores/toast.svelte'

  const sourceLabels: Record<TimelineSource, string> = {
    queries: 'Queries', parts: 'Merges / mutations', health: 'Cluster pressure',
    incidents: 'Incidents', comments: 'Comments', deployments: 'Deployments',
  }
  const sourceOrder = Object.keys(sourceLabels) as TimelineSource[]
  let range = $state('6h')
  let incidentId = $state('')
  let result = $state<IncidentTimeline | null>(null)
  let loading = $state(false)
  let error = $state('')
  let sources = $state<TimelineSource[]>([...sourceOrder])
  let seq = 0
  // Oldest first, as the server sorts: read top to bottom like a postmortem.
  const events = $derived(result?.events.filter((e) => sources.includes(e.source)) ?? [])
  const sourceCounts = $derived.by(() => {
    const counts: Partial<Record<TimelineSource, number>> = {}
    for (const e of result?.events ?? []) counts[e.source] = (counts[e.source] ?? 0) + 1
    return counts
  })
  const limited = $derived(result?.coverage.filter((c) => !c.available || c.truncated) ?? [])
  const multiDay = $derived(result ? new Date(result.from).toDateString() !== new Date(result.to).toDateString() : false)
  const isAdmin = $derived(getSession()?.role === 'admin')
  let annotationOpen = $state(false)
  let saving = $state(false)
  let title = $state('')
  let details = $state('')
  let occurredAt = $state('')
  let relatedOpen = $state(false)
  let relatedLoading = $state(false)
  let related = $state<GovernanceIncident | null>(null)
  let comments = $state<GovernanceIncidentComment[]>([])
  let coverageOpen = $state(false)
  let selectedIndex = $state(-1)
  let openEvent = $state<TimelineEvent | null>(null)
  let list = $state<TimelineList | null>(null)

  async function load() {
    const current = ++seq
    loading = true
    error = ''
    try {
      const bounds = resolveRange(range)
      if (Date.parse(bounds.to) - Date.parse(bounds.from) > 7 * 86400000) throw new Error('Choose a window of at most 7 days.')
      const data = await fetchIncidentTimeline(bounds.from, bounds.to, incidentId)
      if (current !== seq) return
      result = data
      selectedIndex = -1
      const url = new URL(window.location.href)
      url.searchParams.set('from', data.from)
      url.searchParams.set('to', data.to)
      if (incidentId) url.searchParams.set('incident_id', incidentId)
      else url.searchParams.delete('incident_id')
      window.history.replaceState(window.history.state, '', url)
    } catch (e) {
      if (current !== seq) return
      error = errorText(e)
      result = null
    } finally {
      if (current === seq) loading = false
    }
  }

  function toggleSource(source: TimelineSource) {
    sources = sources.includes(source) ? sources.filter((s) => s !== source) : [...sources, source]
    selectedIndex = -1
  }

  function openAt(i: number) {
    selectedIndex = i
    openEvent = events[i] ?? null
  }

  function onKey(e: KeyboardEvent) {
    const target = e.target as HTMLElement | null
    if (target && (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.isContentEditable)) return
    if (isInModal(target) || annotationOpen || relatedOpen || coverageOpen || openEvent || events.length === 0) return
    if (e.key === 'j' || e.key === 'ArrowDown') {
      e.preventDefault()
      selectedIndex = Math.min(events.length - 1, selectedIndex + 1)
      list?.scrollToIndex(selectedIndex)
    } else if (e.key === 'k' || e.key === 'ArrowUp') {
      e.preventDefault()
      selectedIndex = Math.max(0, selectedIndex - 1)
      list?.scrollToIndex(selectedIndex)
    } else if (e.key === 'Enter' && selectedIndex >= 0) {
      openAt(selectedIndex)
    }
  }

  function openAnnotation() {
    const now = new Date()
    occurredAt = new Date(now.getTime() - now.getTimezoneOffset() * 60000).toISOString().slice(0, 19)
    title = ''
    details = ''
    annotationOpen = true
  }

  async function saveAnnotation(event: SubmitEvent) {
    event.preventDefault()
    if (saving) return
    saving = true
    try {
      const at = new Date(occurredAt).toISOString()
      await createDeploymentAnnotation({ occurred_at: at, title: title.trim(), details: details.trim() })
      annotationOpen = false
      toastSuccess('Deployment recorded')
      // Include the new observation in view even when investigating an older window.
      if (result && (at < result.from || at >= result.to)) {
        range = encodeAbsoluteDashboardRange(new Date(Date.parse(at) - 1800000).toISOString(), new Date(Date.parse(at) + 1800000).toISOString())
      }
      await load()
    } catch (e) { toastErrorFrom(e) }
    finally { saving = false }
  }

  async function openIncident(id: string) {
    openEvent = null
    relatedOpen = true
    relatedLoading = true
    related = null
    comments = []
    try {
      const [incident, notes] = await Promise.all([getIncident(id), fetchIncidentComments(id)])
      related = incident.incident
      comments = notes.comments ?? []
    } catch (e) { toastErrorFrom(e) }
    finally { relatedLoading = false }
  }

  function formatValue(value: unknown): string {
    return typeof value === 'number' ? value.toLocaleString(undefined, { maximumFractionDigits: 1 }) : String(value)
  }

  onMount(() => {
    const params = new URLSearchParams(window.location.search)
    const from = params.get('from'), to = params.get('to')
    if (from && to) range = encodeAbsoluteDashboardRange(from, to)
    incidentId = params.get('incident_id') ?? ''
    if (isAdmin) void load()
  })
</script>

<svelte:window onkeydown={onKey} />

<div class="flex h-full min-h-0 flex-col">
  <PageHeader title="Incident timeline" subtitle="Events around an incident" docs={docsURL('incident-timeline')}>
    {#snippet meta()}
      {#if result && limited.length > 0}<Badge tone="warning" title="Some sources are unavailable or capped. Open Coverage for details.">Partial coverage</Badge>{/if}
    {/snippet}
    {#snippet actions()}
      <TimeRangeSelector value={range} onchange={(value) => { range = value; void load() }} />
      <Button variant="ghost" icon title="Refresh" aria-label="Refresh timeline" disabled={!isAdmin || loading} onclick={() => load()}><RefreshCw size={14} class={loading ? 'animate-spin' : ''} /></Button>
      {#if isAdmin}<Button size="sm" onclick={openAnnotation}><Plus size={14} /> Record deployment</Button>{/if}
    {/snippet}
  </PageHeader>

  {#if !isAdmin}
    <EmptyState icon={AlertTriangle} title="Administrator access required" description="This timeline combines retained cluster observations and shared incident records." />
  {:else}
    <div class="flex h-10 shrink-0 items-center gap-1.5 border-b border-edge-subtle px-5" role="group" aria-label="Timeline sources">
      {#each sourceOrder as source}
        {@const on = sources.includes(source)}
        <button class="inline-flex h-6 items-center gap-1.5 rounded-md border px-2 text-xs transition-colors {on ? 'border-accent text-accent bg-accent-soft' : 'border-edge text-fg-3 hover:text-fg'}" aria-pressed={on} onclick={() => toggleSource(source)}>
          {sourceLabels[source]}
          {#if result}<span class="tabular-nums {on ? '' : 'text-fg-4'}">{formatNumber(sourceCounts[source] ?? 0)}</span>{/if}
        </button>
      {/each}
      <div class="ml-auto flex shrink-0 items-center gap-3 text-xs text-fg-3">
        {#if loading}
          <span class="inline-flex items-center gap-2"><Spinner size="sm" class="h-3 w-3" /> Loading…</span>
        {:else if result}
          <span class="tabular-nums" title="{formatFull(result.from)} to {formatFull(result.to)}">{formatNumber(events.length)} of {formatNumber(result.events.length)} observations</span>
        {/if}
        <Button size="xs" variant="outline" aria-label="Source coverage" onclick={() => (coverageOpen = true)}>
          Coverage{#if limited.length > 0}<span class="text-warning">({limited.length} limited)</span>{/if}
        </Button>
      </div>
    </div>

    {#if error}
      <div role="alert" class="shrink-0 border-b border-edge-subtle bg-danger-soft px-5 py-1.5 text-xs text-danger">{error}</div>
    {/if}

    {#if result}
      <div class="flex h-8 shrink-0 items-center gap-3 px-5 text-xs text-fg-4">
        {#if incidentId}
          <span class="text-fg-3">Investigating <button class="text-accent hover:underline" onclick={() => openIncident(incidentId)}>incident {incidentId}</button></span>
        {/if}
        <span class="truncate">{formatFull(result.from)} to {formatFull(result.to)}, browser timezone. Events occurring together do not establish a cause.</span>
        <span class="ml-auto shrink-0">j/k move · Enter opens</span>
      </div>
    {/if}

    {#if loading && !result}
      <div class="flex flex-1 items-center justify-center"><Spinner /></div>
    {:else if result && events.length === 0}
      <EmptyState title="No observations in this view" description="Check the selected sources, source coverage and time window. Empty results do not establish that nothing happened." primary={{ label: 'Open coverage', onclick: () => (coverageOpen = true) }} />
    {:else if result}
      <TimelineList bind:this={list} {events} labels={sourceLabels} {selectedIndex} withDate={multiDay} onopen={openAt} />
    {/if}
  {/if}
</div>

<Sheet open={coverageOpen} title="Source coverage" description="Missing or retained data limits what this window can show." onclose={() => (coverageOpen = false)}>
  <div class="space-y-4 text-[13px]">
    <p class="text-fg-3">Compare query failures and latency, merge or mutation completions, cluster pressure and team notes in one window. Events occurring together do not establish a cause. All times use your browser's timezone.</p>
    {#if result}
      {#each result.coverage as coverage}
        <div class="space-y-1">
          <div class="flex items-center gap-2"><strong class="text-fg">{sourceLabels[coverage.source]}</strong><Badge tone={!coverage.available || coverage.truncated ? 'warning' : 'neutral'}>{!coverage.available ? 'Unavailable' : coverage.truncated ? 'First 500 events' : `${coverage.events} observations`}</Badge></div>
          <p class="text-xs text-fg-3">{coverage.message}</p>
        </div>
      {/each}
      {#if result.bucket_seconds}<p class="text-xs text-fg-3">Query activity is grouped in {result.bucket_seconds}-second buckets for this window.</p>{/if}
    {:else}
      <p class="text-fg-3">Load a window to see coverage.</p>
    {/if}
  </div>
</Sheet>

<Sheet open={openEvent !== null} title={openEvent?.title ?? 'Observation'} onclose={() => { openEvent = null }}>
  {#if openEvent}
    <div class="space-y-4 text-[13px]">
      <div class="flex flex-wrap items-center gap-2">
        <Badge tone={['warn', 'error', 'critical'].includes(openEvent.severity) ? 'warning' : 'neutral'}>{sourceLabels[openEvent.source]}</Badge>
        <time class="font-mono text-xs tabular-nums text-fg-3" datetime={openEvent.at}>{formatFull(openEvent.at)}</time>
      </div>
      {#if openEvent.details}<p class="whitespace-pre-wrap break-words text-fg-2">{openEvent.details}</p>{/if}
      {#if openEvent.source === 'health' && openEvent.values}
        <Panel padding="sm">
          <dl class="grid grid-cols-2 gap-x-4 gap-y-1 text-xs">
            {#each Object.entries(openEvent.values) as [key, value]}
              <dt class="text-fg-3">{key.replaceAll('_', ' ')}</dt><dd class="font-medium tabular-nums text-fg">{formatValue(value)}</dd>
            {/each}
          </dl>
        </Panel>
      {/if}
      {#if openEvent.actor}<p class="text-xs text-fg-3">Recorded by {openEvent.actor}</p>{/if}
    </div>
  {/if}
  {#snippet footer()}
    {#if openEvent?.incident_id}
      {@const id = openEvent.incident_id}
      <Button variant="outline" size="sm" onclick={() => openIncident(id)}>Open incident</Button>
    {/if}
    <Button variant="ghost" size="sm" onclick={() => { openEvent = null }}>Close</Button>
  {/snippet}
</Sheet>

<Sheet open={annotationOpen} title="Record a deployment" description="Add a timestamped observation for this connection." onclose={() => { if (!saving) annotationOpen = false }}>
  <form id="deployment-form" class="space-y-4" onsubmit={saveAnnotation}>
    <FormField label="Occurred at (your local time)" for="deployment-time" required><input id="deployment-time" type="datetime-local" step="1" class="ds-input" required bind:value={occurredAt} /></FormField>
    <FormField label="Title" for="deployment-title" required><Input id="deployment-title" required maxlength={200} bind:value={title} placeholder="API v1.8 deployed" /></FormField>
    <FormField label="Details" for="deployment-details"><Textarea id="deployment-details" rows={5} bind:value={details} placeholder="Change reference and relevant context" /></FormField>
    <p class="text-xs text-fg-3">Your identity is recorded with this annotation. A deployment marker does not imply it caused nearby failures.</p>
  </form>
  {#snippet footer()}
    <Button variant="ghost" size="sm" onclick={() => (annotationOpen = false)} disabled={saving}>Cancel</Button>
    <Button size="sm" type="submit" form="deployment-form" loading={saving} disabled={!title.trim()}>Save deployment</Button>
  {/snippet}
</Sheet>

<Sheet open={relatedOpen} title={related?.title ?? 'Related incident'} onclose={() => relatedOpen = false}>
  {#if relatedLoading}<Spinner />{:else if related}
    <div class="space-y-4 text-[13px]">
      <div class="flex gap-2"><Badge>{related.status}</Badge><Badge tone="warning">{related.severity}</Badge></div>
      <p class="whitespace-pre-wrap text-fg-2">{related.details ?? 'No incident details.'}</p>
      <p class="text-xs text-fg-3">First seen {formatFull(related.first_seen_at)} · Last seen {formatFull(related.last_seen_at)}</p>
      {#if related.resolution_note}<Panel title="Resolution">{related.resolution_note}</Panel>{/if}
      {#each comments as comment}<Panel padding="sm"><p class="whitespace-pre-wrap">{comment.comment_text}</p><p class="mt-2 text-xs text-fg-4">{comment.created_by ?? 'Unknown'} · {formatFull(comment.created_at)}</p></Panel>{/each}
    </div>
  {:else}<p class="text-fg-3">Could not load the incident.</p>{/if}
</Sheet>
