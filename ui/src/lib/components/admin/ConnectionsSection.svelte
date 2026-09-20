<script lang="ts">
  import { onMount } from 'svelte'
  import { apiGet, apiPost, apiDel } from '../../api/client'
  import { formatDate, formatRelativeTime } from '../../utils/format'
  import { success as toastSuccess, error as toastError } from '../../stores/toast.svelte'
  import Button from '../common/Button.svelte'
  import Input from '../common/Input.svelte'
  import Badge from '../common/Badge.svelte'
  import Sheet from '../common/Sheet.svelte'
  import Tabs from '../common/Tabs.svelte'
  import FormField from '../common/FormField.svelte'
  import ConfirmDialog from '../common/ConfirmDialog.svelte'
  import Spinner from '../common/Spinner.svelte'
  import BackgroundAccountsSheet from './BackgroundAccountsSheet.svelte'
  import DataTable, { type DataColumn } from '../common/DataTable.svelte'
  import { Plus, RefreshCw, Copy, KeyRound, Trash2, Search, AlertTriangle } from 'lucide-svelte'

  // Every ClickHouse this instance can reach: direct URLs the server dials,
  // and remote agents that dial in with a token. Self-contained: loads on
  // mount, owns its create sheet, token reveal and delete confirm.
  type TunnelConnection = {
    id: string
    name: string
    type?: 'direct' | 'tunnel'
    clickhouse_url?: string
    is_embedded?: boolean
    status: string
    online: boolean
    created_at: string
    last_seen?: string
    host_info?: { hostname?: string; os?: string } | null
  }
  type TunnelTokenResponse = {
    tunnel_token: string
    setup_instructions?: { connect?: string; service?: string }
    message?: string
    connection?: { id?: string; name?: string }
  }
  type TokenPreview = {
    connectionId: string
    connectionName: string
    token: string
    connectCmd: string
    serviceCmd: string
  }
  type Row = Record<string, unknown>

  let tunnels = $state<TunnelConnection[]>([])
  let tunnelsLoading = $state(false)
  let search = $state('')

  let createOpen = $state(false)
  let createMode = $state<'direct' | 'tunnel'>('direct')
  let createName = $state('')
  let createUrl = $state('')
  let createLoading = $state(false)

  // Token reveal lives in its own sheet: shown right after creating an agent
  // connection, and again from the row actions (view / regenerate).
  let tokenPreview = $state<TokenPreview | null>(null)
  let tokenOpen = $state(false)

  let deleteTarget = $state<TunnelConnection | null>(null)
  let deleteLoading = $state(false)
  let backgroundTarget = $state<TunnelConnection | null>(null)

  onMount(() => {
    void loadTunnels()
  })

  async function loadTunnels() {
    tunnelsLoading = true
    try {
      tunnels = await apiGet<TunnelConnection[]>('/api/connections')
    } catch (e: any) {
      toastError(e.message)
    } finally {
      tunnelsLoading = false
    }
  }

  function setTokenPreview(connection: { id: string; name: string }, payload: TunnelTokenResponse) {
    tokenPreview = {
      connectionId: connection.id,
      connectionName: connection.name,
      token: payload.tunnel_token ?? '',
      connectCmd: payload.setup_instructions?.connect ?? '',
      serviceCmd: payload.setup_instructions?.service ?? '',
    }
    tokenOpen = true
  }

  function openCreate() {
    createName = ''
    createUrl = ''
    createMode = 'direct'
    createOpen = true
  }

  async function createTunnel() {
    const name = createName.trim()
    if (!name) {
      toastError('Connection name is required')
      return
    }
    const url = createUrl.trim()
    if (createMode === 'direct' && !url) {
      toastError('ClickHouse URL is required for a direct connection')
      return
    }
    createLoading = true
    try {
      const body = createMode === 'direct' ? { name, type: 'direct', clickhouse_url: url } : { name }
      const res = await apiPost<TunnelTokenResponse>('/api/connections', body)
      toastSuccess(createMode === 'direct' ? `Connection "${name}" created and connecting to ${url}` : `Agent connection "${name}" created`)
      createOpen = false
      await loadTunnels()
      const created = res.connection
      if (created?.id && created?.name && res.tunnel_token) {
        setTokenPreview({ id: created.id, name: created.name }, res)
      }
    } catch (e: any) {
      toastError(e.message)
    } finally {
      createLoading = false
    }
  }

  async function confirmDelete() {
    if (!deleteTarget) return
    const target = deleteTarget
    deleteLoading = true
    try {
      await apiDel(`/api/connections/${encodeURIComponent(target.id)}`)
      toastSuccess(`Connection "${target.name}" deleted`)
      if (tokenPreview?.connectionId === target.id) {
        tokenPreview = null
        tokenOpen = false
      }
      deleteTarget = null
      await loadTunnels()
    } catch (e: any) {
      toastError(e.message)
    } finally {
      deleteLoading = false
    }
  }

  async function viewToken(conn: TunnelConnection) {
    try {
      const res = await apiGet<TunnelTokenResponse>(`/api/connections/${encodeURIComponent(conn.id)}/token`)
      setTokenPreview(conn, res)
    } catch (e: any) {
      toastError(e.message)
    }
  }

  async function regenerateToken(conn: TunnelConnection) {
    try {
      const res = await apiPost<TunnelTokenResponse>(`/api/connections/${encodeURIComponent(conn.id)}/regenerate-token`)
      setTokenPreview(conn, res)
      toastSuccess(res.message || `Token regenerated for "${conn.name}"`)
    } catch (e: any) {
      toastError(e.message)
    }
  }

  async function copyText(value: string, label: string) {
    if (!value) {
      toastError(`${label} is empty`)
      return
    }
    try {
      await navigator.clipboard.writeText(value)
      toastSuccess(`${label} copied`)
    } catch {
      toastError('Clipboard unavailable')
    }
  }

  function typeLabel(conn: TunnelConnection): string {
    if (conn.is_embedded) return 'Embedded'
    return conn.type === 'direct' ? 'Direct' : 'Agent'
  }

  function target(conn: TunnelConnection): string {
    return conn.clickhouse_url || conn.host_info?.hostname || '—'
  }

  const rows = $derived.by<Row[]>(() => {
    const term = search.trim().toLowerCase()
    return tunnels
      .filter((c) => {
        if (!term) return true
        return (
          c.name.toLowerCase().includes(term) ||
          (c.clickhouse_url ?? '').toLowerCase().includes(term) ||
          (c.host_info?.hostname ?? '').toLowerCase().includes(term) ||
          typeLabel(c).toLowerCase().includes(term)
        )
      })
      .map((c) => ({ ...c, type_label: typeLabel(c), target_label: target(c), online_label: c.online ? 'Online' : 'Offline' }))
  })

  const columns: DataColumn<Row>[] = [
    { key: 'online_label', label: 'Status', width: '110px' },
    { key: 'name', label: 'Name' },
    { key: 'type_label', label: 'Type', width: '110px' },
    { key: 'target_label', label: 'Target', mono: true, truncate: true, width: '32%' },
    { key: 'last_seen', label: 'Last seen', width: '130px', format: (v) => (v ? formatRelativeTime(v) : '—') },
    { key: 'created_at', label: 'Created', width: '160px', format: (v) => formatDate(v) },
  ]

  function asConn(row: Row): TunnelConnection {
    return row as unknown as TunnelConnection
  }
</script>

<div class="flex h-full min-h-0 flex-col">
  <div class="flex h-10 shrink-0 items-center gap-2 px-5">
    <div class="relative">
      <Search size={13} class="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-fg-4" />
      <Input size="sm" type="search" class="w-64 pl-8" placeholder="Search connections" bind:value={search} spellcheck={false} />
    </div>
    <Button icon variant="ghost" size="sm" aria-label="Refresh connections" title="Refresh" onclick={() => void loadTunnels()}>
      <RefreshCw size={14} />
    </Button>
    <span class="ml-auto text-xs text-fg-4">{rows.length} connection{rows.length === 1 ? '' : 's'}</span>
    <Button size="sm" onclick={openCreate}>
      <Plus size={14} /> Add connection
    </Button>
  </div>

  <div class="min-h-0 flex-1 px-5 pb-4">
    {#if tunnelsLoading && tunnels.length === 0}
      <div class="flex h-full items-center justify-center"><Spinner /></div>
    {:else}
      <div class="h-full overflow-hidden rounded-lg border border-edge-subtle bg-surface">
        <DataTable
          fill
          {columns}
          {rows}
          rowKey={(r) => String(r.id)}
          sort={{ key: 'name', dir: 'asc' }}
          emptyTitle={search ? 'Nothing matches' : 'No connections yet'}
          emptyDescription={search ? 'Try another search.' : 'Add a direct URL, or create a remote agent that dials in from behind a firewall.'}
        >
          {#snippet cell(row, col, value)}
            {#if col.key === 'online_label'}
              <Badge dot tone={row.online ? 'success' : 'neutral'}>{value}</Badge>
            {:else if col.key === 'name'}
              <span class="font-medium text-fg">{value}</span>
            {:else if col.key === 'type_label'}
              <Badge tone={value === 'Embedded' ? 'brand' : 'neutral'}>{value}</Badge>
            {:else}
              {value}
            {/if}
          {/snippet}
          {#snippet actions(row)}
            {@const conn = asConn(row)}
            <Button variant="ghost" size="xs" onclick={() => (backgroundTarget = conn)}>Background accounts</Button>
            {#if conn.type !== 'direct' && !conn.is_embedded}
              <Button icon variant="ghost" size="xs" aria-label="Show agent token" title="Show token" onclick={() => void viewToken(conn)}>
                <KeyRound size={13} />
              </Button>
              <Button icon variant="ghost" size="xs" aria-label="Regenerate agent token" title="Regenerate token" onclick={() => void regenerateToken(conn)}>
                <RefreshCw size={13} />
              </Button>
            {/if}
            {#if conn.is_embedded}
              <span class="px-1 text-[11px] text-fg-4">Server config</span>
            {:else}
              <Button icon variant="ghost" size="xs" aria-label="Delete connection" title="Delete" onclick={() => (deleteTarget = conn)}>
                <Trash2 size={13} />
              </Button>
            {/if}
          {/snippet}
        </DataTable>
      </div>
    {/if}
  </div>
</div>

{#if backgroundTarget}
  <BackgroundAccountsSheet connection={backgroundTarget} onclose={() => (backgroundTarget = null)} />
{/if}

<Sheet
  open={createOpen}
  title="Add connection"
  description="Direct connections talk to ClickHouse over HTTP from this server. Remote agents run next to a ClickHouse behind a firewall and dial in."
  size="lg"
  onclose={() => (createOpen = false)}
>
  <form
    class="space-y-4"
    onsubmit={(e) => {
      e.preventDefault()
      void createTunnel()
    }}
  >
    <Tabs
      variant="segmented"
      items={[{ id: 'direct', label: 'Direct URL' }, { id: 'tunnel', label: 'Remote agent' }]}
      value={createMode}
      onchange={(id) => (createMode = id as typeof createMode)}
    />
    <p class="text-[13px] leading-relaxed text-fg-3">
      {createMode === 'direct'
        ? 'Connect to a ClickHouse server this CH-UI instance can reach over HTTP(S). No agent needed; it connects immediately.'
        : 'For ClickHouse servers behind a firewall: CH-UI issues a token, then you run the ch-ui agent next to that server.'}
    </p>
    <FormField label="Connection name" for="conn-name" required controlWidth="lg">
      <Input id="conn-name" placeholder="warehouse-prod" bind:value={createName} required />
    </FormField>
    {#if createMode === 'direct'}
      <FormField label="ClickHouse URL" for="conn-url" required controlWidth="lg" hint="HTTP interface, usually port 8123.">
        <Input id="conn-url" mono placeholder="http://clickhouse-host:8123" bind:value={createUrl} required />
      </FormField>
    {/if}
  </form>
  {#snippet footer()}
    <Button variant="ghost" size="sm" onclick={() => (createOpen = false)}>Cancel</Button>
    <Button
      size="sm"
      loading={createLoading}
      disabled={!createName.trim() || (createMode === 'direct' && !createUrl.trim())}
      onclick={() => void createTunnel()}
    >
      {createMode === 'direct' ? 'Add connection' : 'Create agent token'}
    </Button>
  {/snippet}
</Sheet>

<Sheet
  open={tokenOpen && !!tokenPreview}
  title={tokenPreview ? `Agent token for ${tokenPreview.connectionName}` : 'Agent token'}
  description="Run the ch-ui agent next to the ClickHouse server with this token."
  size="lg"
  onclose={() => (tokenOpen = false)}
>
  {#if tokenPreview}
    <div class="space-y-4">
      <div class="flex items-start gap-2 rounded-md bg-warning-soft px-3 py-2 text-[13px] text-fg">
        <AlertTriangle size={14} class="mt-0.5 shrink-0 text-warning" />
        <span>Copy the token now. Regenerating it invalidates the one the agent is using.</span>
      </div>
      <div>
        <div class="mb-1 flex items-center justify-between">
          <span class="text-xs font-medium text-fg-2">Token</span>
          <Button size="xs" variant="outline" onclick={() => copyText(tokenPreview?.token ?? '', 'Token')}>
            <Copy size={12} /> Copy
          </Button>
        </div>
        <pre class="overflow-x-auto rounded-md border border-edge-subtle bg-surface-2 p-2.5 font-mono text-[11px] text-fg">{tokenPreview.token}</pre>
      </div>
      {#if tokenPreview.connectCmd}
        <div>
          <div class="mb-1 flex items-center justify-between">
            <span class="text-xs font-medium text-fg-2">Connect command</span>
            <Button size="xs" variant="outline" onclick={() => copyText(tokenPreview?.connectCmd ?? '', 'Connect command')}>
              <Copy size={12} /> Copy
            </Button>
          </div>
          <pre class="overflow-x-auto rounded-md border border-edge-subtle bg-surface-2 p-2.5 font-mono text-[11px] text-fg">{tokenPreview.connectCmd}</pre>
        </div>
      {/if}
      {#if tokenPreview.serviceCmd}
        <div>
          <div class="mb-1 flex items-center justify-between">
            <span class="text-xs font-medium text-fg-2">Run as a service</span>
            <Button size="xs" variant="outline" onclick={() => copyText(tokenPreview?.serviceCmd ?? '', 'Service command')}>
              <Copy size={12} /> Copy
            </Button>
          </div>
          <pre class="overflow-x-auto rounded-md border border-edge-subtle bg-surface-2 p-2.5 font-mono text-[11px] text-fg">{tokenPreview.serviceCmd}</pre>
        </div>
      {/if}
    </div>
  {/if}
  {#snippet footer()}
    <Button size="sm" onclick={() => (tokenOpen = false)}>Done</Button>
  {/snippet}
</Sheet>

<ConfirmDialog
  open={deleteTarget !== null}
  title="Delete connection?"
  description={deleteTarget ? `Delete "${deleteTarget.name}"? Any agent using it is disconnected. This cannot be undone.` : ''}
  confirmLabel="Delete"
  destructive={true}
  loading={deleteLoading}
  onconfirm={confirmDelete}
  oncancel={() => (deleteTarget = null)}
/>
