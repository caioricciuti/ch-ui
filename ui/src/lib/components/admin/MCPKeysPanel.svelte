<script lang="ts">
  import { onMount } from 'svelte'
  import { apiGet } from '../../api/client'
  import {
    listMCPKeys,
    createMCPKey,
    rotateMCPKey,
    revokeMCPKey,
    type MCPKey,
  } from '../../api/mcp'
  import { success, error as toastError } from '../../stores/toast.svelte'
  import ConfirmDialog from '../common/ConfirmDialog.svelte'
  import Panel from '../common/Panel.svelte'
  import Button from '../common/Button.svelte'
  import Badge from '../common/Badge.svelte'
  import Input from '../common/Input.svelte'
  import Select from '../common/Select.svelte'
  import FormField from '../common/FormField.svelte'
  import SectionHeader from '../common/SectionHeader.svelte'
  import Sheet from '../common/Sheet.svelte'
  import Stat from '../common/Stat.svelte'
  import Tabs from '../common/Tabs.svelte'
  import DataTable, { type DataColumn } from '../common/DataTable.svelte'
  import { Plus, Copy, Trash2, RefreshCw, ExternalLink, Search } from 'lucide-svelte'
  import { formatDate, formatRelativeTime } from '../../utils/format'

  interface ConnectionRow {
    id: string
    name: string
    online?: boolean
  }

  type Row = MCPKey & Record<string, unknown> & { display_name: string; connection_name: string }

  let keys = $state<MCPKey[]>([])
  let connections = $state<ConnectionRow[]>([])
  let loading = $state(true)

  // ── Table controls ────────────────────────────────────────────
  let view = $state<'api' | 'oauth'>('api')
  let search = $state('')
  let showRevoked = $state(false)

  // ── Create sheet ──────────────────────────────────────────────
  let sheetOpen = $state(false)
  let creating = $state(false)
  let form = $state({
    name: '',
    connectionId: '',
    chUser: '',
    chPassword: '',
    scopes: 'read' as 'read' | 'read_write',
    allowedDatabases: '',
    expiresInDays: 90,
  })

  // One-time reveal of a freshly created or rotated key, shown in the same sheet.
  let revealed = $state<{ secret: string; name: string } | null>(null)

  let revokeTarget = $state<MCPKey | null>(null)
  let revoking = $state(false)
  let rotateTarget = $state<MCPKey | null>(null)
  let rotating = $state(false)

  const expiryOptions = [
    { value: '30', label: '30 days' },
    { value: '90', label: '90 days' },
    { value: '365', label: '1 year' },
    { value: '0', label: 'Never' },
  ]
  const scopeOptions = [
    { value: 'read', label: 'Read' },
    { value: 'read_write', label: 'Read + write' },
  ]
  const connectionOptions = $derived(connections.map((c) => ({ value: c.id, label: c.name })))

  // ── Endpoint + client snippets ────────────────────────────────
  const mcpUrl = $derived(`${window.location.origin}/mcp`)
  type ClientId = 'claude' | 'cursor' | 'vscode' | 'generic'
  const clientTabs = [
    { id: 'claude', label: 'Claude Code' },
    { id: 'cursor', label: 'Cursor' },
    { id: 'vscode', label: 'VS Code' },
    { id: 'generic', label: 'Generic' },
  ]
  let endpointClient = $state<ClientId>('claude')
  let revealClient = $state<ClientId>('claude')

  function snippetFor(client: ClientId, key: string): string {
    switch (client) {
      case 'claude':
        return `claude mcp add ch-ui --transport http ${mcpUrl} --header "Authorization: Bearer ${key}"`
      case 'cursor':
      case 'vscode':
        return JSON.stringify(
          { mcpServers: { 'ch-ui': { url: mcpUrl, headers: { Authorization: `Bearer ${key}` } } } },
          null,
          2,
        )
      default:
        return `URL: ${mcpUrl}\nHeader: Authorization: Bearer ${key}`
    }
  }

  // ── Derived numbers ───────────────────────────────────────────
  const DAY = 24 * 60 * 60 * 1000
  function isExpired(k: MCPKey): boolean {
    return !!k.expires_at && new Date(k.expires_at).getTime() <= Date.now()
  }
  function isLive(k: MCPKey): boolean {
    return !k.revoked_at && !isExpired(k)
  }
  function expiresSoon(k: MCPKey): boolean {
    if (!k.expires_at || !isLive(k)) return false
    return new Date(k.expires_at).getTime() - Date.now() < 7 * DAY
  }
  const activeKeys = $derived(keys.filter((k) => k.kind === 'api' && isLive(k)).length)
  const oauthGrants = $derived(keys.filter((k) => k.kind === 'oauth' && isLive(k)).length)
  const expiringSoon = $derived(keys.filter(expiresSoon).length)
  const usedToday = $derived(
    keys.filter((k) => k.last_used_at && Date.now() - new Date(k.last_used_at).getTime() < DAY).length,
  )
  const apiCount = $derived(keys.filter((k) => k.kind === 'api' && (showRevoked || !k.revoked_at)).length)
  const oauthCount = $derived(keys.filter((k) => k.kind === 'oauth' && (showRevoked || !k.revoked_at)).length)

  const rows = $derived.by<Row[]>(() => {
    const term = search.trim().toLowerCase()
    return keys
      .filter((k) => k.kind === view)
      .filter((k) => showRevoked || !k.revoked_at)
      .map((k) => ({
        ...k,
        display_name: k.kind === 'oauth' ? k.subject || k.name : k.name,
        connection_name: connectionName(k.connection_id),
      }))
      .filter((r) => {
        if (!term) return true
        return [r.display_name, r.key_prefix, r.connection_name, r.ch_user, r.client_id ?? '', r.allowed_databases ?? '']
          .some((v) => String(v).toLowerCase().includes(term))
      })
  })

  const columns: DataColumn<Row>[] = [
    { key: 'display_name', label: 'Name', width: '24%' },
    { key: 'connection_name', label: 'Connection', truncate: true },
    { key: 'ch_user', label: 'ClickHouse user', mono: true },
    { key: 'scopes', label: 'Access', sortable: false },
    { key: 'allowed_databases', label: 'Databases', truncate: true, format: (v) => (v ? String(v) : 'all') },
    { key: 'expires_at', label: 'Expires', sortValue: (r) => (r.expires_at ? new Date(r.expires_at).getTime() : Number.MAX_SAFE_INTEGER) },
    { key: 'last_used_at', label: 'Last used', sortValue: (r) => (r.last_used_at ? new Date(r.last_used_at).getTime() : 0) },
  ]

  // ── Data ──────────────────────────────────────────────────────
  async function load() {
    loading = true
    try {
      const [keysRes, conns] = await Promise.all([
        listMCPKeys(),
        apiGet<ConnectionRow[]>('/api/connections'),
      ])
      keys = keysRes.keys
      connections = conns
      if (!form.connectionId && conns.length > 0) form.connectionId = conns[0].id
    } catch (e) {
      toastError(e instanceof Error ? e.message : 'Failed to load MCP keys')
    } finally {
      loading = false
    }
  }

  onMount(load)

  function connectionName(id: string): string {
    return connections.find((c) => c.id === id)?.name ?? id
  }

  function openCreate() {
    revealed = null
    revealClient = 'claude'
    sheetOpen = true
  }

  function closeSheet() {
    sheetOpen = false
    revealed = null
  }

  function resetForm() {
    form = {
      name: '',
      connectionId: connections[0]?.id ?? '',
      chUser: '',
      chPassword: '',
      scopes: 'read',
      allowedDatabases: '',
      expiresInDays: 90,
    }
  }

  async function submit() {
    if (!form.name.trim() || !form.connectionId || !form.chUser.trim()) {
      toastError('Name, connection and ClickHouse user are required')
      return
    }
    creating = true
    try {
      const res = await createMCPKey({
        name: form.name.trim(),
        connection_id: form.connectionId,
        ch_user: form.chUser.trim(),
        ch_password: form.chPassword,
        scopes: form.scopes,
        allowed_databases: form.allowedDatabases.trim(),
        expires_in_days: form.expiresInDays,
      })
      revealed = { secret: res.secret, name: res.key.name }
      resetForm()
      success('MCP key created')
      await load()
    } catch (e) {
      toastError(e instanceof Error ? e.message : 'Failed to create MCP key')
    } finally {
      creating = false
    }
  }

  async function confirmRevoke() {
    if (!revokeTarget) return
    revoking = true
    try {
      await revokeMCPKey(revokeTarget.id)
      success(`Key "${revokeTarget.name}" revoked`)
      revokeTarget = null
      await load()
    } catch (e) {
      toastError(e instanceof Error ? e.message : 'Failed to revoke key')
    } finally {
      revoking = false
    }
  }

  async function confirmRotate() {
    if (!rotateTarget) return
    rotating = true
    try {
      const res = await rotateMCPKey(rotateTarget.id)
      revealed = { secret: res.secret, name: res.key.name }
      revealClient = 'claude'
      sheetOpen = true
      success(`Key "${rotateTarget.name}" rotated; the old secret no longer works`)
      rotateTarget = null
      await load()
    } catch (e) {
      toastError(e instanceof Error ? e.message : 'Failed to rotate key')
    } finally {
      rotating = false
    }
  }

  async function copyText(value: string, label: string) {
    try {
      await navigator.clipboard.writeText(value)
      success(`${label} copied`)
    } catch {
      toastError('Clipboard unavailable')
    }
  }
</script>

{#snippet snippetBlock(client: ClientId, key: string)}
  <div class="relative">
    <pre class="overflow-x-auto whitespace-pre-wrap break-all rounded-md bg-surface-2 p-3 pr-10 font-mono text-xs leading-relaxed text-fg-2">{snippetFor(client, key)}</pre>
    <Button
      icon
      variant="ghost"
      size="xs"
      class="absolute right-1.5 top-1.5"
      aria-label="Copy snippet"
      title="Copy"
      onclick={() => copyText(snippetFor(client, key), 'Snippet')}
    >
      <Copy size={13} />
    </Button>
  </div>
{/snippet}

<div class="space-y-8">
  <SectionHeader
    title="MCP Server"
    description="AI clients connect here with a key and get row-capped, read-only tools. Every call is recorded in query history and the audit log."
  >
    {#snippet actions()}
      <a
        class="inline-flex h-7 items-center gap-1.5 rounded-md border border-edge px-2.5 text-xs font-medium text-fg-2 transition-colors hover:border-edge-strong hover:bg-hover hover:text-fg"
        href="https://ch-ui.com/docs/mcp"
        target="_blank"
        rel="noreferrer"
      >
        Docs <ExternalLink size={12} />
      </a>
    {/snippet}
  </SectionHeader>

  <div class="grid grid-cols-2 gap-3 lg:grid-cols-4">
    <Stat label="Active keys" value={activeKeys} />
    <Stat label="OAuth grants" value={oauthGrants} />
    <Stat label="Expiring in 7 days" value={expiringSoon} tone={expiringSoon > 0 ? 'warning' : 'default'} />
    <Stat label="Used in last 24h" value={usedToday} />
  </div>

  <Panel title="Endpoint" description="Point any MCP client at this URL with a key as the bearer token.">
    <div class="space-y-3">
      <div class="flex items-center gap-2">
        <Input mono readonly value={mcpUrl} class="flex-1" />
        <Button icon variant="outline" size="md" aria-label="Copy endpoint URL" title="Copy URL" onclick={() => copyText(mcpUrl, 'Endpoint URL')}>
          <Copy size={14} />
        </Button>
      </div>
      <Tabs variant="segmented" size="sm" items={clientTabs} value={endpointClient} onchange={(id) => (endpointClient = id as ClientId)} />
      {@render snippetBlock(endpointClient, '<KEY>')}
    </div>
  </Panel>

  <div class="space-y-3">
    <div class="flex flex-wrap items-center gap-2">
      <Tabs
        variant="segmented"
        size="sm"
        items={[
          { id: 'api', label: 'API keys', count: apiCount },
          { id: 'oauth', label: 'OAuth grants', count: oauthCount },
        ]}
        value={view}
        onchange={(id) => (view = id as 'api' | 'oauth')}
      />
      <div class="relative w-64">
        <Search size={13} class="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-fg-4" />
        <Input size="sm" type="search" class="pl-8" placeholder="Search keys" bind:value={search} spellcheck={false} />
      </div>
      <label class="ds-checkbox-label text-xs">
        <input type="checkbox" class="ds-checkbox ds-checkbox-sm" bind:checked={showRevoked} />
        Show revoked
      </label>
      <div class="flex-1"></div>
      <Button size="sm" onclick={openCreate}>
        <Plus size={14} /> New key
      </Button>
    </div>

    {#if loading}
      <p class="py-6 text-center text-xs text-fg-3">Loading keys…</p>
    {:else}
      <DataTable
        {columns}
        {rows}
        rowKey={(r) => r.id}
        sort={{ key: 'last_used_at', dir: 'desc' }}
        emptyTitle={view === 'api' ? 'No API keys yet' : 'No OAuth grants yet'}
        emptyDescription={view === 'api'
          ? 'Create a key to connect Claude Code, Cursor, VS Code or any MCP client.'
          : 'Grants appear here when a person signs in to an AI client through CH-UI.'}
      >
        {#snippet cell(row, col, value)}
          {#if col.key === 'display_name'}
            <div class="min-w-0">
              <div class="flex items-center gap-1.5">
                <span class="truncate font-medium text-fg">{value}</span>
                {#if row.revoked_at}<Badge tone="danger">revoked</Badge>{/if}
              </div>
              <div class="truncate font-mono text-[11px] text-fg-4">
                {#if row.kind === 'oauth'}{row.client_id || 'oauth client'}{:else}{row.key_prefix}{/if}
              </div>
            </div>
          {:else if col.key === 'scopes'}
            {#if row.scopes === 'read_write'}
              <Badge tone="brand">read + write</Badge>
            {:else}
              <Badge>read</Badge>
            {/if}
          {:else if col.key === 'allowed_databases'}
            {#if row.allowed_databases}
              <span class="font-mono text-xs text-fg-2">{row.allowed_databases}</span>
            {:else}
              <span class="text-fg-4">all</span>
            {/if}
          {:else if col.key === 'expires_at'}
            {#if !row.expires_at}
              <Badge>never</Badge>
            {:else if isExpired(row)}
              <Badge tone="danger">expired</Badge>
            {:else if expiresSoon(row)}
              <Badge tone="warning" title={formatDate(row.expires_at)}>{formatRelativeTime(row.expires_at)}</Badge>
            {:else}
              <span title={formatDate(row.expires_at)}>{formatDate(row.expires_at)}</span>
            {/if}
          {:else if col.key === 'last_used_at'}
            {#if row.last_used_at}
              <span title={formatDate(row.last_used_at)}>{formatRelativeTime(row.last_used_at)}</span>
            {:else}
              <span class="text-fg-4">never</span>
            {/if}
          {:else}
            {value}
          {/if}
        {/snippet}
        {#snippet actions(row)}
          {#if !row.revoked_at}
            {#if row.kind !== 'oauth'}
              <Button size="xs" variant="ghost" icon aria-label="Rotate key" title="Rotate key (new secret, same binding)" onclick={() => (rotateTarget = row)}>
                <RefreshCw size={13} />
              </Button>
            {/if}
            <Button size="xs" variant="ghost" icon aria-label="Revoke key" title="Revoke key" class="hover:text-danger" onclick={() => (revokeTarget = row)}>
              <Trash2 size={13} />
            </Button>
          {/if}
        {/snippet}
      </DataTable>
    {/if}
  </div>
</div>

<Sheet
  open={sheetOpen}
  title={revealed ? `Key "${revealed.name}"` : 'New MCP key'}
  description={revealed
    ? 'Rotate it from the keys table if it ever leaks.'
    : 'The key runs as a ClickHouse user on one connection. Its grants are the permission boundary.'}
  size="lg"
  onclose={closeSheet}
>
  {#if revealed}
    <div class="space-y-5">
      <div class="rounded-md border border-warning/40 bg-warning-soft px-3 py-2 text-xs text-fg">
        Shown once. Copy it now into your client's secret store; it cannot be retrieved later.
      </div>
      <div class="flex items-center gap-2">
        <pre class="flex-1 overflow-x-auto whitespace-pre-wrap break-all rounded-md bg-surface-2 p-3 font-mono text-xs text-fg">{revealed.secret}</pre>
        <Button size="md" variant="outline" icon aria-label="Copy key" title="Copy key" onclick={() => copyText(revealed!.secret, 'MCP key')}>
          <Copy size={14} />
        </Button>
      </div>
      <div class="space-y-2">
        <p class="text-[13px] font-semibold text-fg">Connect a client</p>
        <Tabs variant="segmented" size="sm" items={clientTabs} value={revealClient} onchange={(id) => (revealClient = id as ClientId)} />
        {@render snippetBlock(revealClient, revealed.secret)}
      </div>
    </div>
  {:else}
    <form
      class="space-y-4"
      onsubmit={(e) => { e.preventDefault(); void submit() }}
    >
      <FormField label="Name" for="mcp-key-name" required controlWidth="full">
        <Input id="mcp-key-name" bind:value={form.name} placeholder="claude-code laptop" />
      </FormField>
      <FormField label="Connection" for="mcp-key-connection" required controlWidth="full">
        <Select id="mcp-key-connection" options={connectionOptions} bind:value={form.connectionId} />
      </FormField>
      <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
        <FormField label="ClickHouse user" for="mcp-key-user" required controlWidth="full" hint="The key runs as this user. Use a locked-down account.">
          <Input id="mcp-key-user" bind:value={form.chUser} placeholder="mcp_readonly" autocomplete="off" />
        </FormField>
        <FormField label="ClickHouse password" for="mcp-key-password" controlWidth="full">
          <Input id="mcp-key-password" type="password" bind:value={form.chPassword} autocomplete="new-password" />
        </FormField>
      </div>
      <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
        <FormField
          label="Access"
          for="mcp-key-scope"
          controlWidth="full"
          hint={form.scopes === 'read_write'
            ? 'Read tools plus creating saved queries, dashboards and draft models or pipelines in CH-UI. Nothing writes to ClickHouse.'
            : 'Schema browsing, SELECT queries and query plans, row-capped.'}
        >
          <Select id="mcp-key-scope" options={scopeOptions} value={form.scopes} onchange={(v) => (form.scopes = v as 'read' | 'read_write')} />
        </FormField>
        <FormField label="Expiry" for="mcp-key-expiry" controlWidth="full" hint="Expired keys are rejected; rotate to extend.">
          <Select
            id="mcp-key-expiry"
            options={expiryOptions}
            value={String(form.expiresInDays)}
            onchange={(v) => (form.expiresInDays = parseInt(v, 10) || 0)}
          />
        </FormField>
      </div>
      <FormField label="Allowed databases" for="mcp-key-dbs" controlWidth="full" hint="Comma-separated. Empty = all databases the user can see.">
        <Input id="mcp-key-dbs" mono bind:value={form.allowedDatabases} placeholder="analytics, logs" />
      </FormField>
      <button type="submit" class="hidden" aria-hidden="true"></button>
    </form>
  {/if}
  {#snippet footer()}
    {#if revealed}
      <Button size="sm" onclick={closeSheet}>Done</Button>
    {:else}
      <Button size="sm" variant="ghost" onclick={closeSheet}>Cancel</Button>
      <Button size="sm" loading={creating} onclick={submit}>Create key</Button>
    {/if}
  {/snippet}
</Sheet>

<ConfirmDialog
  open={rotateTarget !== null}
  title="Rotate MCP key"
  description={`Rotate "${rotateTarget?.name ?? ''}"? A new secret is issued with the same connection, user, scope and expiry; the current secret stops working immediately. Update every client that uses it.`}
  confirmLabel="Rotate"
  destructive={false}
  loading={rotating}
  onconfirm={confirmRotate}
  oncancel={() => (rotateTarget = null)}
/>

<ConfirmDialog
  open={revokeTarget !== null}
  title="Revoke MCP key"
  description={`Revoke "${revokeTarget?.name ?? ''}"? Clients using it lose access immediately. This cannot be undone.`}
  confirmLabel="Revoke"
  destructive={true}
  loading={revoking}
  onconfirm={confirmRevoke}
  oncancel={() => (revokeTarget = null)}
/>
