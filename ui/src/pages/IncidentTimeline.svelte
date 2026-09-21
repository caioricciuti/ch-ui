<!-- SPDX-License-Identifier: BUSL-1.1 -->
<script lang="ts">
  import { onMount } from 'svelte'
  import { RefreshCw, Plus, AlertTriangle } from 'lucide-svelte'
  import PageHeader from '../lib/components/common/PageHeader.svelte'
  import PageBody from '../lib/components/common/PageBody.svelte'
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
  import { resolveRange, formatFull } from '../lib/components/telemetry/time'
  import { encodeAbsoluteDashboardRange } from '../lib/utils/dashboard-time'
  import { fetchIncidentTimeline, createDeploymentAnnotation, type IncidentTimeline, type TimelineSource } from '../lib/api/incidentTimeline'
  import { getIncident, fetchIncidentComments } from '../lib/api/governance'
  import type { GovernanceIncident, GovernanceIncidentComment } from '../lib/types/governance'
  import { getSession } from '../lib/stores/session.svelte'
  import { success as toastSuccess, error as toastError } from '../lib/stores/toast.svelte'

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
  const events = $derived(result?.events.filter((e) => sources.includes(e.source)) ?? [])
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
      const url = new URL(window.location.href)
      url.searchParams.set('from', data.from)
      url.searchParams.set('to', data.to)
      if (incidentId) url.searchParams.set('incident_id', incidentId)
      else url.searchParams.delete('incident_id')
      window.history.replaceState(window.history.state, '', url)
    } catch (e) {
      if (current !== seq) return
      error = e instanceof Error ? e.message : String(e)
      result = null
    } finally {
      if (current === seq) loading = false
    }
  }

  function toggleSource(source: TimelineSource) {
    sources = sources.includes(source) ? sources.filter((s) => s !== source) : [...sources, source]
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
    } catch (e) { toastError(e instanceof Error ? e.message : String(e)) }
    finally { saving = false }
  }

  async function openIncident(id: string) {
    relatedOpen = true
    relatedLoading = true
    related = null
    comments = []
    try {
      const [incident, notes] = await Promise.all([getIncident(id), fetchIncidentComments(id)])
      related = incident.incident
      comments = notes.comments ?? []
    } catch (e) { toastError(e instanceof Error ? e.message : String(e)) }
    finally { relatedLoading = false }
  }

  onMount(() => {
    const params = new URLSearchParams(window.location.search)
    const from = params.get('from'), to = params.get('to')
    if (from && to) range = encodeAbsoluteDashboardRange(from, to)
    incidentId = params.get('incident_id') ?? ''
    if (isAdmin) void load()
  })
</script>

<div class="flex h-full min-h-0 flex-col">
  <PageHeader title="Incident timeline" subtitle="Events around an incident">
    {#snippet actions()}
      <TimeRangeSelector value={range} onchange={(value) => { range = value; void load() }} />
      <Button variant="ghost" icon title="Refresh" aria-label="Refresh timeline" disabled={!isAdmin || loading} onclick={() => load()}><RefreshCw size={14} class={loading ? 'animate-spin' : ''} /></Button>
      {#if isAdmin}<Button size="sm" onclick={openAnnotation}><Plus size={14} /> Record deployment</Button>{/if}
    {/snippet}
  </PageHeader>
  <PageBody width="lg">
    {#if !isAdmin}
      <EmptyState icon={AlertTriangle} title="Administrator access required" description="This timeline combines retained cluster observations and shared incident records." />
    {:else}
      <div class="space-y-5">
        <p class="text-[13px] text-fg-3">Compare query failures and latency, merge or mutation completions, cluster pressure and team notes in one window. Events occurring together do not establish a cause. All times use your browser's timezone.</p>
        {#if incidentId}<p class="text-xs text-fg-3">Investigating <button class="text-accent hover:underline" onclick={() => openIncident(incidentId)}>incident {incidentId}</button>. Nearby events from this connection are included.</p>{/if}
        <div class="flex flex-wrap gap-2" aria-label="Timeline sources">
          {#each sourceOrder as source}
            <button class="rounded-md border px-3 py-1.5 text-xs {sources.includes(source) ? 'border-accent text-accent bg-accent-soft' : 'border-edge text-fg-3'}" aria-pressed={sources.includes(source)} onclick={() => toggleSource(source)}>{sourceLabels[source]}</button>
          {/each}
        </div>
        {#if error}<div role="alert" class="rounded-md border border-danger/30 bg-danger-soft p-3 text-sm text-danger">{error}</div>{/if}
        {#if loading && !result}<div class="flex justify-center p-10"><Spinner /></div>{/if}
        {#if result}
          <Panel title="Source coverage" description="Missing or retained data limits what this window can show." padding="sm">
            <div class="grid gap-3 md:grid-cols-2">
              {#each result.coverage as coverage}
                <div class="space-y-1 text-xs">
                  <div class="flex gap-2"><strong class="text-fg">{sourceLabels[coverage.source]}</strong><Badge tone={!coverage.available || coverage.truncated ? 'warning' : 'neutral'}>{!coverage.available ? 'Unavailable' : coverage.truncated ? 'First 500 events' : `${coverage.events} observations`}</Badge></div>
                  <p class="text-fg-3">{coverage.message}</p>
                </div>
              {/each}
            </div>
          </Panel>
          <div class="flex items-center justify-between text-xs text-fg-3"><span>{formatFull(result.from)} – {formatFull(result.to)}</span><span>{events.length} visible observations</span></div>
          {#if events.length === 0}
            <EmptyState title="No observations in this view" description="Check the selected sources, source coverage and time window. Empty results do not establish that nothing happened." />
          {:else}
            <ol class="divide-y divide-edge-subtle rounded-lg border border-edge-subtle bg-surface">
              {#each events as event (event.id)}
                <li class="flex gap-4 px-4 py-3">
                  <time class="w-40 shrink-0 pt-0.5 font-mono text-[11px] text-fg-3" datetime={event.at}>{formatFull(event.at)}</time>
                  <div class="min-w-0 flex-1 space-y-1.5">
                    <div class="flex flex-wrap items-center gap-2"><Badge tone={['warn','error','critical'].includes(event.severity) ? 'warning' : 'neutral'}>{sourceLabels[event.source]}</Badge><span class="text-[13px] font-medium text-fg">{event.title}</span></div>
                    {#if event.details}<p class="whitespace-pre-wrap break-words text-xs leading-relaxed text-fg-3">{event.details}</p>{/if}
                    {#if event.source === 'health' && event.values}<div class="flex flex-wrap gap-x-4 gap-y-1 text-xs text-fg-3">{#each Object.entries(event.values) as [key, value]}<span>{key.replaceAll('_', ' ')}: <strong>{typeof value === 'number' ? value.toLocaleString(undefined, { maximumFractionDigits: 1 }) : String(value)}</strong></span>{/each}</div>{/if}
                    <div class="flex gap-3 text-xs text-fg-4">{#if event.actor}<span>{event.actor}</span>{/if}{#if event.incident_id}<button class="text-accent hover:underline" onclick={() => openIncident(event.incident_id!)}>Open incident</button>{/if}</div>
                  </div>
                </li>
              {/each}
            </ol>
          {/if}
        {/if}
      </div>
    {/if}
  </PageBody>
</div>

<Sheet open={annotationOpen} title="Record a deployment" description="Add a timestamped observation for this connection." onclose={() => { if (!saving) annotationOpen = false }}>
  <form class="space-y-4" onsubmit={saveAnnotation}>
    <FormField label="Occurred at (your local time)" for="deployment-time" required><input id="deployment-time" type="datetime-local" step="1" class="ds-input" required bind:value={occurredAt} /></FormField>
    <FormField label="Title" for="deployment-title" required><Input id="deployment-title" required maxlength={200} bind:value={title} placeholder="API v1.8 deployed" /></FormField>
    <FormField label="Details" for="deployment-details"><Textarea id="deployment-details" rows={5} bind:value={details} placeholder="Change reference and relevant context" /></FormField>
    <p class="text-xs text-fg-3">Your identity is recorded with this annotation. A deployment marker does not imply it caused nearby failures.</p>
    <Button type="submit" loading={saving} disabled={!title.trim()}>Save deployment</Button>
  </form>
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
  {:else}<p class="text-sm text-fg-3">Could not load the incident.</p>{/if}
</Sheet>
