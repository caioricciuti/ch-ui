<script lang="ts">
  import { onMount } from 'svelte'
  import type { AuditLog } from '../../types/api'
  import { apiGet } from '../../api/client'
  import { formatDate } from '../../utils/format'
  import { error as toastError } from '../../stores/toast.svelte'
  import Button from '../common/Button.svelte'
  import Input from '../common/Input.svelte'
  import Select from '../common/Select.svelte'
  import type { SelectOption } from '../common/Select.svelte'
  import Badge from '../common/Badge.svelte'
  import Spinner from '../common/Spinner.svelte'
  import Sheet from '../common/Sheet.svelte'
  import DataTable, { type DataColumn } from '../common/DataTable.svelte'
  import { RefreshCw, Search } from 'lucide-svelte'

  // The Governance audit log: every recorded action, newest first, with
  // the table filling the section and the filters in one toolbar row.
  type Row = Record<string, unknown>
  type BadgeTone = 'neutral' | 'success' | 'warning' | 'danger' | 'info' | 'brand'

  let logs = $state<AuditLog[]>([])
  let loading = $state(false)
  let limit = $state('100')
  let timeRange = $state('')
  let action = $state('')
  let username = $state('')
  let search = $state('')
  let selected = $state<AuditLog | null>(null)

  const limitOptions: SelectOption[] = [
    { value: '50', label: '50 entries' },
    { value: '100', label: '100 entries' },
    { value: '500', label: '500 entries' },
  ]
  const timeRangeOptions: SelectOption[] = [
    { value: '', label: 'All time' },
    { value: '15m', label: 'Last 15 min' },
    { value: '1h', label: 'Last hour' },
    { value: '6h', label: 'Last 6 hours' },
    { value: '24h', label: 'Last 24 hours' },
    { value: '7d', label: 'Last 7 days' },
    { value: '30d', label: 'Last 30 days' },
  ]
  const actionOptions = $derived.by((): SelectOption[] => {
    const values = new Set<string>()
    if (action.trim()) values.add(action)
    for (const log of logs) if (log.action?.trim()) values.add(log.action)
    const sorted = Array.from(values).sort((a, b) => a.localeCompare(b))
    return [{ value: '', label: 'All actions' }, ...sorted.map((value) => ({ value, label: value }))]
  })
  const userOptions = $derived.by((): SelectOption[] => {
    const values = new Set<string>()
    if (username.trim()) values.add(username)
    for (const log of logs) if (log.username?.trim()) values.add(log.username)
    const sorted = Array.from(values).sort((a, b) => a.localeCompare(b))
    return [{ value: '', label: 'All users' }, ...sorted.map((value) => ({ value, label: value }))]
  })

  const columns: DataColumn<Row>[] = [
    { key: 'created_at', label: 'Time', mono: true, width: '160px', format: (v) => formatDate(v) },
    { key: 'action', label: 'Action', width: '220px' },
    { key: 'username', label: 'User', format: (v) => (v ? String(v) : '—') },
    { key: 'details', label: 'Details', mono: true, truncate: true, width: '45%', sortable: false, format: (v) => (v ? String(v) : '—') },
    { key: 'ip_address', label: 'IP', mono: true, format: (v) => (v ? String(v) : '—') },
  ]

  function actionTone(value: string): BadgeTone {
    if (value.endsWith('.deleted') || value.includes('fail')) return 'danger'
    if (value.startsWith('user.')) return 'info'
    if (value.startsWith('governance.')) return 'brand'
    return 'neutral'
  }

  // Details are free text or a JSON payload; show JSON pretty-printed.
  const selectedDetails = $derived.by(() => {
    const raw = selected?.details?.trim()
    if (!raw) return null
    try {
      const parsed: unknown = JSON.parse(raw)
      if (parsed && typeof parsed === 'object') return { json: true, text: JSON.stringify(parsed, null, 2) }
    } catch {
      /* not JSON */
    }
    return { json: false, text: raw }
  })

  async function load() {
    loading = true
    try {
      const params = new URLSearchParams()
      params.set('limit', limit)
      if (timeRange) params.set('timeRange', timeRange)
      if (action) params.set('action', action)
      if (username) params.set('username', username)
      if (search.trim()) params.set('search', search.trim())
      const res = await apiGet<AuditLog[]>(`/api/governance/audit-logs?${params}`)
      logs = res ?? []
    } catch (e: any) {
      toastError(e.message)
    } finally {
      loading = false
    }
  }

  function setAndLoad(apply: () => void) {
    apply()
    void load()
  }

  onMount(() => {
    void load()
  })
</script>

<div class="flex h-full min-h-0 flex-col">
  <div class="flex h-10 shrink-0 items-center gap-2 px-5">
    <div class="relative">
      <Search size={13} class="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-fg-4" />
      <Input
        size="sm"
        type="search"
        class="w-72 pl-8"
        placeholder="Search action, user, details or IP"
        bind:value={search}
        spellcheck={false}
        onkeydown={(e) => e.key === 'Enter' && void load()}
      />
    </div>
    <Select size="sm" class="w-44" options={actionOptions} value={action} onchange={(v) => setAndLoad(() => (action = v))} />
    <Select size="sm" class="w-36" options={userOptions} value={username} onchange={(v) => setAndLoad(() => (username = v))} />
    <Select size="sm" class="w-36" options={timeRangeOptions} value={timeRange} onchange={(v) => setAndLoad(() => (timeRange = v))} />
    <Select size="sm" class="w-32" options={limitOptions} value={limit} onchange={(v) => setAndLoad(() => (limit = v))} />
    <Button icon variant="ghost" size="sm" aria-label="Refresh" title="Refresh" loading={loading} onclick={() => void load()}>
      <RefreshCw size={14} />
    </Button>
    <span class="ml-auto text-xs tabular-nums text-fg-4">{logs.length} {logs.length === 1 ? 'entry' : 'entries'}</span>
  </div>

  <div class="min-h-0 flex-1 px-5 pb-4">
    {#if loading && logs.length === 0}
      <div class="flex h-full items-center justify-center"><Spinner /></div>
    {:else}
      <div class="h-full overflow-hidden rounded-lg border border-edge-subtle bg-surface">
        <DataTable
          fill
          {columns}
          rows={logs as unknown as Row[]}
          rowKey={(r) => String(r.id)}
          sort={{ key: 'created_at', dir: 'desc' }}
          emptyTitle="No audit entries"
          emptyDescription="Nothing matches the current filters."
          onrowclick={(r) => (selected = r as unknown as AuditLog)}
        >
          {#snippet cell(_row, col, value)}
            {#if col.key === 'action'}
              <Badge tone={actionTone(value)} class="font-mono">{value}</Badge>
            {:else}
              {value}
            {/if}
          {/snippet}
        </DataTable>
      </div>
    {/if}
  </div>
</div>

<Sheet open={!!selected} title="Audit entry" description={selected ? formatDate(selected.created_at) : ''} size="md" onclose={() => (selected = null)}>
  {#if selected}
    <div class="space-y-4">
      <dl class="divide-y divide-edge-subtle rounded-lg border border-edge-subtle">
        <div class="flex items-center justify-between gap-4 px-3 py-2 text-[13px]">
          <dt class="text-fg-3">Action</dt>
          <dd><Badge tone={actionTone(selected.action)} class="font-mono">{selected.action}</Badge></dd>
        </div>
        <div class="flex items-center justify-between gap-4 px-3 py-2 text-[13px]">
          <dt class="text-fg-3">User</dt>
          <dd class="text-fg">{selected.username || '—'}</dd>
        </div>
        <div class="flex items-center justify-between gap-4 px-3 py-2 text-[13px]">
          <dt class="text-fg-3">IP</dt>
          <dd class="font-mono text-xs text-fg">{selected.ip_address || '—'}</dd>
        </div>
        <div class="flex items-center justify-between gap-4 px-3 py-2 text-[13px]">
          <dt class="text-fg-3">Time</dt>
          <dd class="font-mono text-xs text-fg">{formatDate(selected.created_at)}</dd>
        </div>
      </dl>

      <div>
        <h3 class="mb-1.5 text-[13px] font-semibold text-fg">Details</h3>
        {#if !selectedDetails}
          <p class="text-[13px] text-fg-3">No details recorded.</p>
        {:else if selectedDetails.json}
          <pre class="overflow-x-auto whitespace-pre rounded-md bg-surface-2 p-3 font-mono text-xs leading-relaxed text-fg">{selectedDetails.text}</pre>
        {:else}
          <p class="whitespace-pre-wrap break-words rounded-md bg-surface-2 p-3 font-mono text-xs leading-relaxed text-fg">{selectedDetails.text}</p>
        {/if}
      </div>
    </div>
  {/if}
</Sheet>
