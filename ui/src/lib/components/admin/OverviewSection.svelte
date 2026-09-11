<script lang="ts">
  import { onMount } from 'svelte'
  import type { AdminStats } from '../../types/api'
  import { apiGet } from '../../api/client'
  import { fetchClusterInfo, fetchNodeInfo } from '../../api/query'
  import { formatDate, formatRelativeTime } from '../../utils/format'
  import { error as toastError } from '../../stores/toast.svelte'
  import { setSection } from '../../stores/nav.svelte'
  import PageBody from '../common/PageBody.svelte'
  import SectionHeader from '../common/SectionHeader.svelte'
  import Stat from '../common/Stat.svelte'
  import Badge from '../common/Badge.svelte'
  import Button from '../common/Button.svelte'
  import Spinner from '../common/Spinner.svelte'
  import Panel from '../common/Panel.svelte'
  import DataTable, { type DataColumn } from '../common/DataTable.svelte'
  import { RefreshCw } from 'lucide-svelte'

  // Admin overview: instance stats, every connection with its live status,
  // and the cluster topology. Loads on mount; nothing here writes.

  type ConnectionRow = {
    id: string
    name: string
    status: string
    online: boolean
    created_at: string
    last_seen_at?: string | null
    [key: string]: unknown
  }
  type ClusterNode = { shard_num: number; replica_num: number; host_name: string; host_address: string; port: number; is_local: number }
  type ClusterDetail = { name: string; shards: number; replicas: number; total_nodes: number; nodes: ClusterNode[] }
  type NodeRow = ClusterNode & { [key: string]: unknown }

  let stats = $state<AdminStats | null>(null)
  let statsLoading = $state(true)
  let connections = $state<ConnectionRow[]>([])
  let clusterInfo = $state<{ is_cluster: boolean; clusters: ClusterDetail[] }>({ is_cluster: false, clusters: [] })
  let currentNode = $state<{ hostname: string; version: string } | null>(null)
  let clusterLoading = $state(false)

  async function loadStats() {
    statsLoading = true
    try {
      stats = await apiGet<AdminStats>('/api/admin/stats')
    } catch (e: any) {
      toastError(e.message)
    } finally {
      statsLoading = false
    }
  }

  async function loadConnections() {
    try {
      connections = await apiGet<ConnectionRow[]>('/api/admin/connections')
    } catch (e: any) {
      toastError(e.message)
    }
  }

  async function loadClusterInfo() {
    clusterLoading = true
    try {
      const [cluster, node] = await Promise.all([fetchClusterInfo(), fetchNodeInfo()])
      clusterInfo = cluster
      currentNode = node.node ?? null
    } catch {
      clusterInfo = { is_cluster: false, clusters: [] }
      currentNode = null
    } finally {
      clusterLoading = false
    }
  }

  onMount(() => {
    void loadStats()
    void loadConnections()
    void loadClusterInfo()
  })

  const connectionColumns: DataColumn<ConnectionRow>[] = [
    { key: 'online', label: 'Status', width: '110px', sortValue: (r) => (r.online ? 1 : 0) },
    { key: 'name', label: 'Name' },
    { key: 'id', label: 'ID', mono: true, truncate: true, width: '30%' },
    { key: 'last_seen_at', label: 'Last seen', width: '150px', format: (v) => (v ? formatRelativeTime(v) : 'never') },
    { key: 'created_at', label: 'Created', width: '170px', format: (v) => formatDate(v) },
  ]

  const nodeColumns: DataColumn<NodeRow>[] = [
    { key: 'host_name', label: 'Host', mono: true },
    { key: 'host_address', label: 'Address', mono: true },
    { key: 'port', label: 'Port', mono: true, align: 'right', width: '90px' },
    { key: 'shard_num', label: 'Shard', align: 'right', width: '90px' },
    { key: 'replica_num', label: 'Replica', align: 'right', width: '90px' },
    { key: 'is_local', label: 'Local', width: '80px', sortable: false },
  ]

  function nodeRows(cluster: ClusterDetail): NodeRow[] {
    return cluster.nodes.map((n) => ({ ...n }))
  }
</script>

<PageBody width="md">
  <div class="space-y-8">
    {#if statsLoading}
      <div class="flex items-center justify-center py-12"><Spinner /></div>
    {:else if stats}
      <div class="grid grid-cols-2 gap-3 md:grid-cols-4">
        <Stat label="Users" value={stats.users_count} />
        <Stat label="Connections" value={stats.online} hint={`of ${stats.connections} online`} />
        <Stat label="Queries" value={stats.query_count} />
        <Stat label="Logins" value={stats.login_count} />
      </div>
    {/if}

    <section>
      <SectionHeader title="Connections" description="Every ClickHouse this instance can reach, with its live status. Click one to manage it." />
      <DataTable
        columns={connectionColumns}
        rows={connections}
        rowKey={(r) => r.id}
        sort={{ key: 'name', dir: 'asc' }}
        emptyTitle="No connections yet"
        emptyDescription="Add one under Connections."
        onrowclick={() => setSection('tunnels')}
      >
        {#snippet cell(row, col, value)}
          {#if col.key === 'online'}
            <Badge dot tone={row.online ? 'success' : 'neutral'}>{row.online ? 'Online' : 'Offline'}</Badge>
          {:else if col.key === 'name'}
            <span class="font-medium text-fg">{value}</span>
          {:else}
            {value}
          {/if}
        {/snippet}
      </DataTable>
    </section>

    <section>
      <SectionHeader title="Cluster topology" description="Shards, replicas and the node this session runs on.">
        {#snippet actions()}
          <Button icon size="sm" variant="ghost" aria-label="Refresh cluster info" title="Refresh cluster info" loading={clusterLoading} onclick={() => loadClusterInfo()}>
            <RefreshCw size={13} />
          </Button>
        {/snippet}
      </SectionHeader>

      {#if clusterLoading && !currentNode}
        <div class="flex items-center justify-center py-6"><Spinner /></div>
      {:else}
        <div class="space-y-3">
          {#if currentNode}
            <Panel padding="sm">
              <div class="flex items-center gap-3 text-[13px]">
                <span class="h-2 w-2 shrink-0 rounded-full bg-success"></span>
                <span class="text-fg-3">Current node</span>
                <span class="font-mono font-semibold text-fg">{currentNode.hostname}</span>
                <span class="text-xs text-fg-4">v{currentNode.version}</span>
                {#if !clusterInfo.is_cluster}
                  <span class="ml-auto text-xs text-fg-3">Single node, no cluster configured</span>
                {/if}
              </div>
            </Panel>
          {:else if !clusterInfo.is_cluster}
            <Panel padding="sm">
              <p class="text-[13px] text-fg-3">Single-node setup detected, no cluster configuration found.</p>
            </Panel>
          {/if}

          {#each clusterInfo.clusters as cluster (cluster.name)}
            <div>
              <div class="mb-2 flex items-center gap-2">
                <span class="text-[13px] font-semibold text-fg">{cluster.name}</span>
                <Badge tone="brand">{cluster.shards} shard{cluster.shards !== 1 ? 's' : ''}</Badge>
                <Badge tone="brand">{cluster.replicas} replica{cluster.replicas !== 1 ? 's' : ''}</Badge>
                <span class="text-xs text-fg-4">{cluster.total_nodes} node{cluster.total_nodes !== 1 ? 's' : ''}</span>
              </div>
              <DataTable
                columns={nodeColumns}
                rows={nodeRows(cluster)}
                rowKey={(r) => `${r.host_name}:${r.port}`}
                sort={{ key: 'shard_num', dir: 'asc' }}
                emptyTitle="No nodes reported"
              >
                {#snippet cell(row, col, value)}
                  {#if col.key === 'is_local'}
                    {#if row.is_local}
                      <span class="inline-block h-2 w-2 rounded-full bg-success" title="This node"></span>
                    {:else}
                      <span class="text-fg-4">—</span>
                    {/if}
                  {:else}
                    {value}
                  {/if}
                {/snippet}
              </DataTable>
            </div>
          {/each}
        </div>
      {/if}
    </section>
  </div>
</PageBody>
