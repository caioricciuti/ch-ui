<script lang="ts">
  import Sheet from '../common/Sheet.svelte'
  import Button from '../common/Button.svelte'
  import Badge from '../common/Badge.svelte'
  import Spinner from '../common/Spinner.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import ConfirmDialog from '../common/ConfirmDialog.svelte'
  import FormField from '../common/FormField.svelte'
  import Input from '../common/Input.svelte'
  import DataTable, { type DataColumn } from '../common/DataTable.svelte'
  import IngestSecret from './IngestSecret.svelte'
  import { listIngestKeys, createIngestKey, revokeIngestKey } from '../../api/ingest'
  import type { IngestKey } from '../../types/ingest'
  import { getSession } from '../../stores/session.svelte'
  import { success, error as toastError } from '../../stores/toast.svelte'
  import { formatDate, formatRelativeTime } from '../../utils/format'
  import { Plus, Trash2, AlertTriangle } from 'lucide-svelte'

  interface Props {
    open: boolean
    pipelineId: string
    onclose: () => void
    /** Called after the list changes, with the active key count. */
    onchange?: (activeKeys: number) => void
    /** Opens the shipper snippets for a key just created. */
    onsetup?: (secret: string) => void
  }

  let { open, pipelineId, onclose, onchange, onsetup }: Props = $props()

  type Row = IngestKey & Record<string, unknown>

  let keys = $state<IngestKey[]>([])
  let loading = $state(false)
  let error = $state('')
  let naming = $state(false)
  let newName = $state('')
  let creating = $state(false)
  let nameInput = $state<HTMLInputElement>()
  let revealed = $state<{ name: string; secret: string } | null>(null)
  let revokeTarget = $state<IngestKey | null>(null)
  let revoking = $state(false)

  const canWrite = $derived((getSession()?.role ?? 'viewer') !== 'viewer')
  const rows = $derived(keys as Row[])

  const columns: DataColumn<Row>[] = [
    { key: 'name', label: 'Name', width: '28%' },
    { key: 'prefix', label: 'Prefix', mono: true },
    { key: 'created_at', label: 'Created', sortValue: (r) => new Date(r.created_at).getTime() },
    { key: 'last_used_at', label: 'Last used', sortValue: (r) => (r.last_used_at ? new Date(r.last_used_at).getTime() : 0) },
  ]

  $effect(() => {
    if (open) {
      revealed = null
      void load()
    }
  })

  $effect(() => {
    if (naming) nameInput?.focus()
  })

  function startNaming() {
    newName = ''
    naming = true
  }

  function cancelNaming() {
    naming = false
    newName = ''
  }

  function submitCreate(e: SubmitEvent) {
    e.preventDefault()
    void create(newName.trim())
  }

  async function load() {
    loading = true
    error = ''
    try {
      const res = await listIngestKeys(pipelineId)
      keys = res.keys ?? []
      onchange?.(keys.filter((k) => !k.revoked_at).length)
    } catch (e: unknown) {
      error = e instanceof Error ? e.message : String(e)
    } finally {
      loading = false
    }
  }

  async function create(name: string) {
    if (!name) return
    creating = true
    try {
      const res = await createIngestKey(pipelineId, name)
      revealed = { name: res.key.name, secret: res.secret }
      naming = false
      newName = ''
      await load()
    } catch (e: unknown) {
      toastError(e instanceof Error ? e.message : String(e))
    } finally {
      creating = false
    }
  }

  async function revoke() {
    if (!revokeTarget) return
    revoking = true
    try {
      await revokeIngestKey(pipelineId, revokeTarget.id)
      success(`Key "${revokeTarget.name}" revoked`)
      revokeTarget = null
      await load()
    } catch (e: unknown) {
      toastError(e instanceof Error ? e.message : String(e))
    } finally {
      revoking = false
    }
  }
</script>

<Sheet {open} {onclose} size="lg" title="Ingest keys" description="Shippers send a key as Authorization: Bearer. A key only works for this pipeline and its signal.">
  <div class="space-y-4">
    {#if revealed}
      <IngestSecret name={revealed.name} secret={revealed.secret} />
      {#if onsetup}
        <Button size="sm" variant="outline" onclick={() => onsetup?.(revealed!.secret)}>Set up a shipper with this key</Button>
      {/if}
    {/if}

    {#if naming}
      <form id="ingest-key-form" class="rounded-md bg-surface-2 p-3" onsubmit={submitCreate}>
        <FormField label="Key name" for="ingest-key-name" required controlWidth="full" hint="Also the name shown in the key list. Use it to recognise the shipper, e.g. prod-collector.">
          <Input id="ingest-key-name" placeholder="prod-collector" bind:value={newName} bind:ref={nameInput} disabled={creating} />
        </FormField>
      </form>
    {/if}

    {#if error}
      <EmptyState size="compact" icon={AlertTriangle} title="Could not load keys" description={error} primary={{ label: 'Retry', onclick: () => void load() }} />
    {:else if loading && keys.length === 0}
      <div class="flex justify-center py-10"><Spinner /></div>
    {:else}
      <DataTable
        {columns}
        {rows}
        rowKey={(r) => r.id}
        sort={{ key: 'created_at', dir: 'desc' }}
        emptyTitle="No keys yet"
        emptyDescription="Create a key, then give it to the shipper that sends data here."
      >
        {#snippet cell(row, col, value)}
          {#if col.key === 'name'}
            <span class="flex min-w-0 items-center gap-1.5">
              <span class="truncate font-medium text-fg">{value}</span>
              {#if row.revoked_at}<Badge tone="danger" title={formatDate(row.revoked_at)}>revoked</Badge>{/if}
            </span>
          {:else if col.key === 'created_at'}
            <span title={row.created_by ? `by ${row.created_by}` : undefined}>{formatDate(row.created_at)}</span>
          {:else if col.key === 'last_used_at'}
            {#if row.last_used_at}
              <span title={formatDate(row.last_used_at)}>{formatRelativeTime(row.last_used_at)}</span>
            {:else}
              <span class="text-fg-4">never</span>
            {/if}
          {:else if col.key === 'prefix'}
            <span class="font-mono text-fg-2">chi_{value}...</span>
          {:else}{value}{/if}
        {/snippet}
        {#snippet actions(row)}
          {#if canWrite && !row.revoked_at}
            <Button size="xs" variant="ghost" icon aria-label="Revoke key" title="Revoke key" class="hover:text-danger" onclick={() => (revokeTarget = row)}>
              <Trash2 size={13} />
            </Button>
          {/if}
        {/snippet}
      </DataTable>
    {/if}
  </div>
  {#snippet footer()}
    <Button size="sm" variant="ghost" onclick={onclose}>Close</Button>
    {#if canWrite}
      {#if naming}
        <Button size="sm" variant="outline" onclick={cancelNaming} disabled={creating}>Cancel</Button>
        <Button size="sm" type="submit" form="ingest-key-form" loading={creating} disabled={!newName.trim()}>Create key</Button>
      {:else}
        <Button size="sm" onclick={startNaming}><Plus size={14} /> Create key</Button>
      {/if}
    {/if}
  {/snippet}
</Sheet>

<ConfirmDialog
  open={revokeTarget !== null}
  title="Revoke ingest key"
  description={`Revoke "${revokeTarget?.name ?? ''}"? Shippers using it get 401 from the next request. This cannot be undone.`}
  confirmLabel="Revoke"
  destructive
  loading={revoking}
  onconfirm={revoke}
  oncancel={() => (revokeTarget = null)}
/>
