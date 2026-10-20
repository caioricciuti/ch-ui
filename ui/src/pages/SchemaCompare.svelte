<!-- SPDX-License-Identifier: BUSL-1.1 -->
<script lang="ts">
  import { errorText } from '../lib/utils/errors'
  import { onMount } from 'svelte'
  import { ArrowRight, Download, GitCompare, AlertTriangle } from 'lucide-svelte'
  import { compareSchemas, fetchSchemaConnections, type SchemaComparison, type SchemaConnection, type SchemaDifference, type SchemaEndpoint } from '../lib/api/schemaCompare'
  import { getSession } from '../lib/stores/session.svelte'
  import { success as toastSuccess, errorFrom as toastErrorFrom } from '../lib/stores/toast.svelte'
  import { copyToClipboard } from '../lib/utils/export'
  import PageHeader from '../lib/components/common/PageHeader.svelte'
  import { docsURL } from '../lib/routes'
  import PageBody from '../lib/components/common/PageBody.svelte'
  import Button from '../lib/components/common/Button.svelte'
  import Badge from '../lib/components/common/Badge.svelte'
  import Panel from '../lib/components/common/Panel.svelte'
  import Input from '../lib/components/common/Input.svelte'
  import Select from '../lib/components/common/Select.svelte'
  import FormField from '../lib/components/common/FormField.svelte'
  import EmptyState from '../lib/components/common/EmptyState.svelte'
  import Spinner from '../lib/components/common/Spinner.svelte'
  import Sheet from '../lib/components/common/Sheet.svelte'
  import DataTable, { type DataColumn } from '../lib/components/common/DataTable.svelte'

  type Row = Record<string, unknown> & SchemaDifference & { object: string; object_kind: string; change: string; summary: string }
  type Tone = 'success' | 'warning' | 'danger'

  let connections = $state<SchemaConnection[]>([])
  let source = $state<SchemaEndpoint>({ connection_id: '', database: 'default', username: '', password: '' })
  let target = $state<SchemaEndpoint>({ connection_id: '', database: 'default', username: '', password: '' })
  let comparison = $state<SchemaComparison | null>(null)
  let selected = $state<SchemaDifference | null>(null)
  let loading = $state(true)
  let comparing = $state(false)
  let error = $state('')
  let search = $state('')
  let kind = $state('all')
  const options = $derived(connections.map(c => ({ value: c.id, label: c.name + (c.online ? '' : ' · offline'), disabled: !c.online })))
  const differences = $derived((comparison?.result.differences ?? []).filter(d => (kind === 'all' || d.kind === kind) && `${d.table} ${d.field}`.toLowerCase().includes(search.toLowerCase())))
  const rows = $derived<Row[]>(differences.map(d => ({ ...d, object: `${comparison?.target.database ?? ''}.${d.table}`, object_kind: fieldKind(d.field), change: changeLabel[d.kind], summary: summarize(d.review_sql) })))
  const ownConnection = $derived(getSession()?.connectionId)
  const complete = $derived(source.connection_id && target.connection_id && source.database.trim() && target.database.trim() && (source.connection_id === ownConnection || source.username?.trim()) && (target.connection_id === ownConnection || target.username?.trim()))

  const changeLabel: Record<SchemaDifference['kind'], string> = { source_only: 'Missing in target', target_only: 'Only in target', changed: 'Changed' }
  const changeTone: Record<SchemaDifference['kind'], Tone> = { source_only: 'success', target_only: 'danger', changed: 'warning' }
  /** The backend names the field it compared; group those names into the object kind a reviewer thinks in. */
  function fieldKind(field: string): string {
    if (field === 'table') return 'Table'
    if (field.startsWith('column.')) return 'Column'
    if (field.endsWith('_key')) return 'Key'
    if (field === 'ttl') return 'TTL'
    if (field === 'view_definition') return 'View'
    if (field === 'engine') return 'Engine'
    return field
  }
  /** Review SQL is all comment lines; the first one says what to do. */
  function summarize(sql: string): string { return (sql.split('\n')[0] ?? '').replace(/^--\s?/, '') }
  const columns: DataColumn<Row>[] = [
    { key: 'object', label: 'Object', width: '30%' },
    { key: 'object_kind', label: 'Kind', width: '100px' },
    { key: 'change', label: 'Change', width: '140px' },
    { key: 'summary', label: 'Summary', truncate: true, sortable: false },
  ]

  async function load() {
    loading = true
    try {
      connections = (await fetchSchemaConnections()).connections
      source.connection_id = ownConnection ?? connections[0]?.id ?? ''
      target.connection_id = connections.find(c => c.id !== source.connection_id && c.online)?.id ?? source.connection_id
      error = ''
    } catch (e) { error = errorText(e) }
    finally { loading = false }
  }
  async function compare() {
    comparing = true; error = ''; comparison = null; selected = null
    try { comparison = await compareSchemas({ ...source }, { ...target }) }
    catch (e) { error = errorText(e) }
    finally { comparing = false; source.password = ''; target.password = '' }
  }
  function selectConnection(endpoint: SchemaEndpoint, value: string) { endpoint.connection_id = value; endpoint.username = ''; endpoint.password = ''; comparison = null; selected = null }
  function download() {
    if (!comparison) return
    const blob = new Blob([comparison.result.review_sql], { type: 'text/plain;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a'); link.href = url; link.download = 'schema-review.sql'; link.click()
    setTimeout(() => URL.revokeObjectURL(url), 1000)
  }
  async function copyReview(diff: SchemaDifference) {
    try { await copyToClipboard(diff.review_sql); toastSuccess('Review SQL copied') }
    catch (e) { toastErrorFrom(e, 'copy the review SQL') }
  }
  onMount(() => { void load(); return () => { source.password = ''; target.password = '' } })
</script>

<div class="flex h-full min-h-0 flex-col">
  <PageHeader title="Schema comparison" subtitle="Compare environments and review changes" docs={docsURL('schema-compare')}>
    {#snippet actions()}{#if comparison}<Button size="sm" variant="outline" onclick={download}><Download size={13} /> Download review SQL</Button>{/if}{/snippet}
  </PageHeader>
  {#if loading}<div class="flex flex-1 items-center justify-center"><Spinner /></div>
  {:else if !connections.length && error}<EmptyState icon={AlertTriangle} title="Couldn't load connections" description={error} primary={{ label: 'Retry', onclick: load }} />
  {:else}
    <PageBody width="lg">
      <div class="space-y-5">
        <p class="text-[13px] text-fg-3">Choose a source definition and a target to compare. Review suggestions bring the target toward the source. Comparison only reads metadata.</p>
        <form class="space-y-4" onsubmit={(e) => { e.preventDefault(); void compare() }}>
          <fieldset disabled={comparing} class="grid min-w-0 gap-4 md:grid-cols-2">
            {#each [{ key: 'source', label: 'Source · desired schema', endpoint: source }, { key: 'target', label: 'Target · compare against', endpoint: target }] as side (side.key)}
              <Panel title={side.label}>
                <div class="space-y-3">
                  <FormField label="Connection" for={`schema-${side.key}-connection`}><Select id={`schema-${side.key}-connection`} {options} value={side.endpoint.connection_id} onchange={(value) => selectConnection(side.endpoint, value)} /></FormField>
                  <FormField label="Database" for={`schema-${side.key}-database`}><Input id={`schema-${side.key}-database`} bind:value={side.endpoint.database} required placeholder="Database name" /></FormField>
                  {#if side.endpoint.connection_id !== ownConnection}
                    <p class="text-xs text-fg-3">Enter your credentials for this connection. They are used for this comparison and are never saved.</p>
                    <FormField label="ClickHouse username" for={`schema-${side.key}-user`}><Input id={`schema-${side.key}-user`} bind:value={side.endpoint.username} required autocomplete="off" /></FormField>
                    <FormField label="Password" for={`schema-${side.key}-password`}><Input id={`schema-${side.key}-password`} type="password" bind:value={side.endpoint.password} autocomplete="off" /></FormField>
                  {:else}<p class="text-xs text-fg-3">Uses your current session: {getSession()?.user}</p>{/if}
                </div>
              </Panel>
            {/each}
          </fieldset>
          <div class="flex items-center gap-3"><Button type="submit" size="sm" loading={comparing} disabled={!complete}><GitCompare size={14} /> Compare schemas</Button><span class="text-xs text-fg-3">Tables, columns, keys, TTLs and views</span></div>
        </form>
        {#if error}<p class="rounded-md border border-danger/30 p-3 text-[13px] text-danger" role="alert">{error}</p>{/if}
        {#if comparison}
          <Panel title={`${comparison.source.database} → ${comparison.target.database}`} description={`Captured ${new Date(comparison.captured_at).toLocaleString()}`}>
            <div class="space-y-3">
              <div class="flex flex-wrap gap-3 text-[13px]"><Badge tone={comparison.result.differences.length ? 'warning' : 'success'}>{comparison.result.differences.length} differences</Badge><span>{comparison.result.source_tables} source tables</span><ArrowRight size={14} /><span>{comparison.result.target_tables} target tables</span><span class="text-fg-3">{comparison.result.matching_tables} match</span></div>
              <p class="text-xs text-fg-3">{comparison.scope}</p>
              <p class="text-xs text-fg-3">SQL suggestions are commented for review. This page does not execute migrations.</p>
            </div>
          </Panel>
          {#if comparison.result.differences.length}
            <div>
              <div class="flex h-10 items-center gap-2">
                <label class="sr-only" for="schema-filter">Filter differences</label>
                <Input id="schema-filter" size="sm" type="search" class="w-64" placeholder="Filter tables or fields" bind:value={search} />
                <label class="sr-only" for="schema-kind">Difference type</label>
                <Select id="schema-kind" size="sm" class="w-44" bind:value={kind} options={[{ value: 'all', label: 'All differences' }, { value: 'changed', label: 'Changed definitions' }, { value: 'source_only', label: 'Missing in target' }, { value: 'target_only', label: 'Only in target' }]} />
                <span class="ml-auto text-xs text-fg-4">{rows.length} of {comparison.result.differences.length} differences</span>
              </div>
              <DataTable
                {columns}
                {rows}
                rowKey={(r) => `${r.table}:${r.field}:${r.kind}`}
                sort={{ key: 'object', dir: 'asc' }}
                emptyTitle="No differences match this filter"
                emptyDescription="Clear the search or pick another difference type to see the rest."
                onrowclick={(r) => (selected = r)}
              >
                {#snippet cell(row, col, value)}
                  {#if col.key === 'object'}
                    <span class="block truncate font-medium text-fg">{row.object}</span>
                    <span class="block truncate font-mono text-xs text-fg-4">{row.field}</span>
                  {:else if col.key === 'object_kind'}
                    <Badge>{row.object_kind}</Badge>
                  {:else if col.key === 'change'}
                    <Badge tone={changeTone[row.kind]}>{row.change}</Badge>
                  {:else}{value}{/if}
                {/snippet}
              </DataTable>
            </div>
          {:else}<EmptyState icon={GitCompare} title="Visible schemas match" description="No differences found in the compared fields and objects visible to these accounts." />{/if}
        {:else if !comparing && !error}<EmptyState icon={GitCompare} title="Compare two environments" description="Find schema drift before a deployment and export a reviewable SQL plan." />{/if}
      </div>
    </PageBody>
  {/if}
</div>

<Sheet open={selected !== null} size="lg" title={selected ? `${selected.table} · ${selected.field}` : ''} description={selected ? `${fieldKind(selected.field)} · ${changeLabel[selected.kind]}` : ''} onclose={() => (selected = null)}>
  {#if selected}
    <div class="space-y-4">
      <div class="grid min-w-0 gap-3 md:grid-cols-2">
        <div class="min-w-0"><p class="mb-1 text-xs text-fg-3">Source</p><pre class="overflow-x-auto whitespace-pre-wrap break-all rounded bg-surface-2 p-2 font-mono text-xs">{selected.source || '(absent)'}</pre></div>
        <div class="min-w-0"><p class="mb-1 text-xs text-fg-3">Target</p><pre class="overflow-x-auto whitespace-pre-wrap break-all rounded bg-surface-2 p-2 font-mono text-xs">{selected.target || '(absent)'}</pre></div>
      </div>
      <div><p class="mb-1 text-xs text-fg-3">Review suggestion</p><pre class="overflow-x-auto whitespace-pre-wrap break-all rounded bg-surface-2 p-3 font-mono text-xs">{selected.review_sql}</pre></div>
    </div>
  {/if}
  {#snippet footer()}
    <Button size="sm" variant="ghost" onclick={() => (selected = null)}>Close</Button>
    {#if selected}{@const diff = selected}<Button size="sm" variant="outline" onclick={() => void copyReview(diff)}>Copy review SQL</Button>{/if}
  {/snippet}
</Sheet>
