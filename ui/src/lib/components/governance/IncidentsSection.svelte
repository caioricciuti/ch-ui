<script lang="ts">
  import { onMount } from 'svelte'
  import {
    fetchIncidents,
    createIncident as apiCreateIncident,
    updateIncident as apiUpdateIncident,
    getIncident as apiGetIncident,
    fetchIncidentComments,
    createIncidentComment as apiCreateIncidentComment,
  } from '../../api/governance'
  import type { GovernanceIncident, GovernanceIncidentComment } from '../../types/governance'
  import { success as toastSuccess, error as toastError } from '../../stores/toast.svelte'
  import { formatDate, formatRelativeTime } from '../../utils/format'
  import Button from '../common/Button.svelte'
  import Badge from '../common/Badge.svelte'
  import Select from '../common/Select.svelte'
  import Input from '../common/Input.svelte'
  import Textarea from '../common/Textarea.svelte'
  import FormField from '../common/FormField.svelte'
  import Sheet from '../common/Sheet.svelte'
  import Tabs from '../common/Tabs.svelte'
  import Markdown from '../common/Markdown.svelte'
  import Spinner from '../common/Spinner.svelte'
  import DataTable, { type DataColumn } from '../common/DataTable.svelte'
  import { Plus } from 'lucide-svelte'

  // Incidents: a queue of things a person has to act on. Manual ones are
  // written here; the syncer promotes violations and over-permissions.
  type Row = Record<string, unknown>
  type BadgeTone = 'neutral' | 'success' | 'warning' | 'danger' | 'info' | 'brand'

  const SEVERITIES = ['info', 'warn', 'error', 'critical']
  const STATUSES = ['open', 'triaged', 'in_progress', 'resolved', 'dismissed']

  const severityFilterOptions = [{ value: '', label: 'All severities' }, ...SEVERITIES.map((v) => ({ value: v, label: v }))]
  const statusFilterOptions = [{ value: '', label: 'All statuses' }, ...STATUSES.map((v) => ({ value: v, label: v }))]
  const severityOptions = SEVERITIES.map((v) => ({ value: v, label: v }))
  const statusOptions = STATUSES.map((v) => ({ value: v, label: v.replace('_', ' ') }))

  let incidents = $state<GovernanceIncident[]>([])
  let loading = $state(false)
  let statusFilter = $state('')
  let severityFilter = $state('')

  // Create sheet
  let createOpen = $state(false)
  let creating = $state(false)
  let createTab = $state<'write' | 'preview'>('write')
  let form = $state({ title: '', severity: 'warn', status: 'open', assignee: '', details: '' })

  // Detail sheet
  let detailOpen = $state(false)
  let selected = $state<GovernanceIncident | null>(null)
  let detailLoading = $state(false)
  let saving = $state(false)
  let editing = $state(false)
  let editTab = $state<'write' | 'preview'>('write')
  let comments = $state<GovernanceIncidentComment[]>([])
  let commentDraft = $state('')
  let commentTab = $state<'write' | 'preview'>('write')
  let postingComment = $state(false)

  const columns: DataColumn<Row>[] = [
    { key: 'title', label: 'Title' },
    { key: 'severity', label: 'Severity', width: '110px' },
    { key: 'status', label: 'Status', width: '120px' },
    { key: 'assignee', label: 'Assignee', width: '160px', format: (v) => (v ? String(v) : '') },
    { key: 'occurrence_count', label: 'Seen', align: 'right', width: '80px' },
    { key: 'last_seen_at', label: 'Updated', width: '130px', format: (v) => formatRelativeTime(v), sortValue: (r) => Date.parse(String(r.last_seen_at)) || 0 },
  ]

  const editorTabs = [
    { id: 'write', label: 'Write' },
    { id: 'preview', label: 'Preview' },
  ]

  function severityTone(severity: string | null | undefined): BadgeTone {
    if (severity === 'critical' || severity === 'error') return 'danger'
    if (severity === 'warn') return 'warning'
    if (severity === 'info') return 'info'
    return 'neutral'
  }

  function statusTone(status: string | null | undefined): BadgeTone {
    if (status === 'resolved') return 'success'
    if (status === 'dismissed') return 'neutral'
    if (status === 'in_progress' || status === 'triaged') return 'info'
    return 'warning'
  }

  function asRows(list: GovernanceIncident[]): Row[] {
    return list as unknown as Row[]
  }

  async function load() {
    loading = true
    try {
      const res = await fetchIncidents({
        status: statusFilter || undefined,
        severity: severityFilter || undefined,
        limit: 200,
      })
      incidents = res?.incidents ?? []
    } catch (err: any) {
      toastError('Failed to load incidents: ' + err.message)
    } finally {
      loading = false
    }
  }

  onMount(load)

  // Filters apply as soon as they change.
  $effect(() => {
    statusFilter
    severityFilter
    void load()
  })

  function openCreate() {
    form = { title: '', severity: 'warn', status: 'open', assignee: '', details: '' }
    createTab = 'write'
    createOpen = true
  }

  async function create() {
    const title = form.title.trim()
    if (!title) {
      toastError('Incident title is required')
      return
    }
    creating = true
    try {
      await apiCreateIncident({
        title,
        severity: form.severity,
        status: form.status,
        assignee: form.assignee.trim() || undefined,
        details: form.details.trim() || undefined,
      })
      createOpen = false
      toastSuccess('Incident created')
      await load()
    } catch (err: any) {
      toastError('Failed to create incident: ' + err.message)
    } finally {
      creating = false
    }
  }

  function openDetail(incident: GovernanceIncident) {
    selected = incident
    comments = []
    commentDraft = ''
    editing = false
    editTab = 'write'
    commentTab = 'write'
    detailOpen = true
    void loadDetail(incident.id)
  }

  async function loadDetail(id: string) {
    detailLoading = true
    try {
      const [incidentRes, commentsRes] = await Promise.all([apiGetIncident(id), fetchIncidentComments(id)])
      selected = incidentRes?.incident ?? selected
      comments = commentsRes?.comments ?? []
    } catch (err: any) {
      toastError('Failed to load incident: ' + err.message)
    } finally {
      detailLoading = false
    }
  }

  function closeDetail() {
    detailOpen = false
    selected = null
    comments = []
    commentDraft = ''
    editing = false
  }

  async function patch(data: Parameters<typeof apiUpdateIncident>[1]) {
    if (!selected) return
    saving = true
    try {
      await apiUpdateIncident(selected.id, data)
      await loadDetail(selected.id)
      await load()
    } catch (err: any) {
      toastError('Failed to update incident: ' + err.message)
    } finally {
      saving = false
    }
  }

  async function saveEdits() {
    if (!selected) return
    await patch({
      title: selected.title,
      severity: selected.severity,
      status: selected.status,
      assignee: selected.assignee ?? '',
      details: selected.details ?? '',
      resolution_note: selected.resolution_note ?? '',
    })
    editing = false
    toastSuccess('Incident updated')
  }

  async function setStatus(status: string) {
    await patch({ status })
    toastSuccess(status === 'resolved' ? 'Incident resolved' : `Status set to ${status.replace('_', ' ')}`)
  }

  async function addComment() {
    if (!selected) return
    const text = commentDraft.trim()
    if (!text) return
    postingComment = true
    try {
      await apiCreateIncidentComment(selected.id, text)
      commentDraft = ''
      commentTab = 'write'
      await loadDetail(selected.id)
    } catch (err: any) {
      toastError('Failed to add comment: ' + err.message)
    } finally {
      postingComment = false
    }
  }
</script>

<div class="flex h-full min-h-0 flex-col">
  <div class="flex h-10 shrink-0 items-center gap-2 px-5">
    <Select size="sm" class="w-40" options={statusFilterOptions} bind:value={statusFilter} />
    <Select size="sm" class="w-40" options={severityFilterOptions} bind:value={severityFilter} />
    <span class="ml-auto text-xs text-fg-4">{incidents.length} incident{incidents.length === 1 ? '' : 's'}</span>
    <Button size="sm" onclick={openCreate}>
      <Plus size={14} /> New incident
    </Button>
  </div>

  <div class="min-h-0 flex-1 px-5 pb-4">
    {#if loading && incidents.length === 0}
      <div class="flex h-full items-center justify-center"><Spinner /></div>
    {:else}
      <div class="h-full overflow-hidden rounded-lg border border-edge-subtle bg-surface">
        <DataTable
          fill
          columns={columns}
          rows={asRows(incidents)}
          rowKey={(r) => String(r.id)}
          sort={{ key: 'last_seen_at', dir: 'desc' }}
          emptyTitle="No incidents"
          emptyDescription={statusFilter || severityFilter ? 'Nothing matches the current filters.' : 'Violations and over-permissions promote themselves here; you can also open one by hand.'}
          onrowclick={(row) => openDetail(row as unknown as GovernanceIncident)}
        >
          {#snippet cell(row, col, value)}
            {#if col.key === 'severity'}
              <Badge tone={severityTone(value)}>{value}</Badge>
            {:else if col.key === 'status'}
              <Badge tone={statusTone(value)}>{value.replace('_', ' ')}</Badge>
            {:else if col.key === 'title'}
              <span class="font-medium text-fg">{value}</span>
            {:else if col.key === 'last_seen_at'}
              <span title={formatDate(row.last_seen_at)}>{value}</span>
            {:else}
              {value}
            {/if}
          {/snippet}
        </DataTable>
      </div>
    {/if}
  </div>
</div>

<Sheet open={createOpen} title="New incident" description="Something a person has to look at. Details take markdown." size="lg" onclose={() => (createOpen = false)}>
  <div class="space-y-4">
    <FormField label="Title" for="incident-title" required controlWidth="full">
      <Input id="incident-title" bind:value={form.title} placeholder="Short summary of what is wrong" />
    </FormField>
    <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
      <FormField label="Severity" for="incident-severity">
        <Select id="incident-severity" options={severityOptions} bind:value={form.severity} />
      </FormField>
      <FormField label="Status" for="incident-status">
        <Select id="incident-status" options={statusOptions} bind:value={form.status} />
      </FormField>
    </div>
    <FormField label="Assignee" for="incident-assignee" hint="Who is on it. Optional.">
      <Input id="incident-assignee" bind:value={form.assignee} placeholder="Username or team" />
    </FormField>
    <FormField label="Details" for="incident-details" controlWidth="full" hint="Markdown. Use ```sql fences for queries.">
      <Tabs variant="segmented" size="sm" class="mb-2" items={editorTabs} value={createTab} onchange={(id) => (createTab = id as 'write' | 'preview')} />
      {#if createTab === 'write'}
        <Textarea id="incident-details" mono rows={12} bind:value={form.details} placeholder="What happened, what it affects, what action is needed" />
      {:else}
        <div class="min-h-[16rem] rounded-md border border-edge-subtle bg-surface-2 p-3">
          {#if form.details.trim()}
            <Markdown content={form.details} />
          {:else}
            <p class="text-[13px] text-fg-4">Nothing to preview yet.</p>
          {/if}
        </div>
      {/if}
    </FormField>
  </div>
  {#snippet footer()}
    <Button variant="ghost" size="sm" onclick={() => (createOpen = false)}>Cancel</Button>
    <Button size="sm" loading={creating} onclick={create}>Create incident</Button>
  {/snippet}
</Sheet>

<Sheet open={detailOpen} title={selected?.title ?? 'Incident'} size="lg" onclose={closeDetail}>
  {#if selected}
    <div class="space-y-5">
      <div class="flex flex-wrap items-center gap-2">
        <Badge tone={severityTone(selected.severity)}>{selected.severity}</Badge>
        <Badge tone={statusTone(selected.status)}>{selected.status.replace('_', ' ')}</Badge>
        {#if selected.source_type && selected.source_type !== 'manual'}
          <Badge>from {selected.source_type.replace('_', ' ')}</Badge>
        {/if}
        {#if detailLoading}<Spinner size="sm" class="ml-1" />{/if}
      </div>

      <dl class="grid grid-cols-2 gap-x-6 gap-y-2 text-[13px] md:grid-cols-4">
        <div>
          <dt class="text-xs text-fg-3">Assignee</dt>
          <dd class="mt-0.5 text-fg">{selected.assignee || 'Unassigned'}</dd>
        </div>
        <div>
          <dt class="text-xs text-fg-3">Seen</dt>
          <dd class="mt-0.5 tabular-nums text-fg">{selected.occurrence_count} time{selected.occurrence_count === 1 ? '' : 's'}</dd>
        </div>
        <div>
          <dt class="text-xs text-fg-3">First seen</dt>
          <dd class="mt-0.5 text-fg" title={formatDate(selected.first_seen_at)}>{formatRelativeTime(selected.first_seen_at)}</dd>
        </div>
        <div>
          <dt class="text-xs text-fg-3">Last seen</dt>
          <dd class="mt-0.5 text-fg" title={formatDate(selected.last_seen_at)}>{formatRelativeTime(selected.last_seen_at)}</dd>
        </div>
      </dl>

      {#if editing}
        <div class="space-y-4 rounded-lg border border-edge-subtle p-4">
          <FormField label="Title" for="incident-edit-title" required>
            <Input id="incident-edit-title" bind:value={selected.title} />
          </FormField>
          <div class="grid grid-cols-1 gap-3 md:grid-cols-3">
            <FormField label="Severity" for="incident-edit-severity">
              <Select id="incident-edit-severity" options={severityOptions} bind:value={selected.severity} />
            </FormField>
            <FormField label="Status" for="incident-edit-status">
              <Select id="incident-edit-status" options={statusOptions} bind:value={selected.status} />
            </FormField>
            <FormField label="Assignee" for="incident-edit-assignee">
              <Input
                id="incident-edit-assignee"
                value={selected.assignee ?? ''}
                oninput={(e) => selected && (selected.assignee = e.currentTarget.value)}
              />
            </FormField>
          </div>
          <FormField label="Details" for="incident-edit-details" controlWidth="full" hint="Markdown. Use ```sql fences for queries.">
            <Tabs variant="segmented" size="sm" class="mb-2" items={editorTabs} value={editTab} onchange={(id) => (editTab = id as 'write' | 'preview')} />
            {#if editTab === 'write'}
              <Textarea
                id="incident-edit-details"
                mono
                rows={10}
                value={selected.details ?? ''}
                oninput={(e) => selected && (selected.details = e.currentTarget.value)}
              />
            {:else}
              <div class="min-h-[12rem] rounded-md border border-edge-subtle bg-surface-2 p-3">
                {#if selected.details?.trim()}
                  <Markdown content={selected.details} />
                {:else}
                  <p class="text-[13px] text-fg-4">Nothing to preview yet.</p>
                {/if}
              </div>
            {/if}
          </FormField>
          <FormField label="Resolution note" for="incident-edit-resolution" hint="What fixed it, for the next person.">
            <Textarea
              id="incident-edit-resolution"
              rows={3}
              value={selected.resolution_note ?? ''}
              oninput={(e) => selected && (selected.resolution_note = e.currentTarget.value)}
            />
          </FormField>
          <div class="flex justify-end gap-2">
            <Button variant="ghost" size="sm" onclick={() => { editing = false; void loadDetail(selected!.id) }}>Cancel</Button>
            <Button size="sm" loading={saving} onclick={saveEdits}>Save</Button>
          </div>
        </div>
      {:else}
        <section>
          <h3 class="mb-1.5 text-xs font-medium text-fg-3">Details</h3>
          {#if selected.details?.trim()}
            <Markdown content={selected.details} />
          {:else}
            <p class="text-[13px] text-fg-4">No details written.</p>
          {/if}
        </section>
        {#if selected.resolution_note?.trim()}
          <section>
            <h3 class="mb-1.5 text-xs font-medium text-fg-3">Resolution</h3>
            <Markdown content={selected.resolution_note} />
          </section>
        {/if}
      {/if}

      <section>
        <h3 class="mb-2 text-xs font-medium text-fg-3">Comments</h3>
        {#if comments.length > 0}
          <div class="space-y-2">
            {#each comments as comment (comment.id)}
              <div class="rounded-md bg-surface-2 px-3 py-2.5">
                <Markdown size="sm" content={comment.comment_text} />
                <p class="mt-1.5 text-[11px] text-fg-4">{comment.created_by || 'unknown'} · <span title={formatDate(comment.created_at)}>{formatRelativeTime(comment.created_at)}</span></p>
              </div>
            {/each}
          </div>
        {:else}
          <p class="text-[13px] text-fg-4">No comments yet.</p>
        {/if}
        <div class="mt-3">
          <Tabs variant="segmented" size="sm" class="mb-2" items={editorTabs} value={commentTab} onchange={(id) => (commentTab = id as 'write' | 'preview')} />
          {#if commentTab === 'write'}
            <Textarea rows={3} mono bind:value={commentDraft} placeholder="Add a comment (markdown)" />
          {:else}
            <div class="min-h-[5rem] rounded-md border border-edge-subtle bg-surface-2 p-3">
              {#if commentDraft.trim()}
                <Markdown size="sm" content={commentDraft} />
              {:else}
                <p class="text-[13px] text-fg-4">Nothing to preview yet.</p>
              {/if}
            </div>
          {/if}
          <div class="mt-2 flex justify-end">
            <Button size="sm" variant="outline" loading={postingComment} disabled={!commentDraft.trim()} onclick={addComment}>Comment</Button>
          </div>
        </div>
      </section>
    </div>
  {/if}
  {#snippet footer()}
    {#if selected && !editing}
      <div class="flex w-full flex-wrap items-center gap-2">
        <Button variant="outline" size="sm" onclick={() => (editing = true)}>Edit</Button>
        <div class="ml-auto flex items-center gap-2">
          {#if selected.status === 'open'}
            <Button variant="outline" size="sm" loading={saving} onclick={() => setStatus('triaged')}>Triage</Button>
          {/if}
          {#if selected.status !== 'in_progress' && selected.status !== 'resolved'}
            <Button variant="outline" size="sm" loading={saving} onclick={() => setStatus('in_progress')}>Start</Button>
          {/if}
          {#if selected.status !== 'dismissed' && selected.status !== 'resolved'}
            <Button variant="ghost" size="sm" loading={saving} onclick={() => setStatus('dismissed')}>Dismiss</Button>
          {/if}
          {#if selected.status !== 'resolved'}
            <Button size="sm" loading={saving} onclick={() => setStatus('resolved')}>Resolve</Button>
          {:else}
            <Button variant="outline" size="sm" loading={saving} onclick={() => setStatus('open')}>Reopen</Button>
          {/if}
        </div>
      </div>
    {/if}
  {/snippet}
</Sheet>
