<!-- SPDX-License-Identifier: BUSL-1.1 -->
<script lang="ts">
  import { onMount } from 'svelte'
  import { ArrowRight, Download, GitCompare, AlertTriangle } from 'lucide-svelte'
  import { compareSchemas, fetchSchemaConnections, type SchemaComparison, type SchemaConnection, type SchemaEndpoint } from '../lib/api/schemaCompare'
  import { getSession } from '../lib/stores/session.svelte'
  import PageHeader from '../lib/components/common/PageHeader.svelte'
  import PageBody from '../lib/components/common/PageBody.svelte'
  import Button from '../lib/components/common/Button.svelte'
  import Badge from '../lib/components/common/Badge.svelte'
  import Panel from '../lib/components/common/Panel.svelte'
  import Input from '../lib/components/common/Input.svelte'
  import Select from '../lib/components/common/Select.svelte'
  import FormField from '../lib/components/common/FormField.svelte'
  import EmptyState from '../lib/components/common/EmptyState.svelte'
  import Spinner from '../lib/components/common/Spinner.svelte'

  let connections = $state<SchemaConnection[]>([])
  let source = $state<SchemaEndpoint>({ connection_id: '', database: 'default', username: '', password: '' })
  let target = $state<SchemaEndpoint>({ connection_id: '', database: 'default', username: '', password: '' })
  let comparison = $state<SchemaComparison | null>(null)
  let loading = $state(true)
  let comparing = $state(false)
  let error = $state('')
  let search = $state('')
  let kind = $state('all')
  const options = $derived(connections.map(c => ({ value: c.id, label: c.name + (c.online ? '' : ' · offline'), disabled: !c.online })))
  const differences = $derived((comparison?.result.differences ?? []).filter(d => (kind === 'all' || d.kind === kind) && `${d.table} ${d.field}`.toLowerCase().includes(search.toLowerCase())))
  const ownConnection = $derived(getSession()?.connectionId)
  const complete = $derived(source.connection_id && target.connection_id && source.database.trim() && target.database.trim() && (source.connection_id === ownConnection || source.username?.trim()) && (target.connection_id === ownConnection || target.username?.trim()))

  async function load() {
    loading = true
    try {
      connections = (await fetchSchemaConnections()).connections
      source.connection_id = ownConnection ?? connections[0]?.id ?? ''
      target.connection_id = connections.find(c => c.id !== source.connection_id && c.online)?.id ?? source.connection_id
      error = ''
    } catch (e) { error = e instanceof Error ? e.message : 'Failed to load connections' }
    finally { loading = false }
  }
  async function compare() {
    comparing = true; error = ''; comparison = null
    try { comparison = await compareSchemas({ ...source }, { ...target }) }
    catch (e) { error = e instanceof Error ? e.message : 'Comparison failed' }
    finally { comparing = false; source.password = ''; target.password = '' }
  }
  function selectConnection(endpoint: SchemaEndpoint, value: string) { endpoint.connection_id = value; endpoint.username = ''; endpoint.password = ''; comparison = null }
  function download() {
    if (!comparison) return
    const blob = new Blob([comparison.result.review_sql], { type: 'text/plain;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a'); link.href = url; link.download = 'schema-review.sql'; link.click()
    setTimeout(() => URL.revokeObjectURL(url), 1000)
  }
  onMount(() => { void load(); return () => { source.password = ''; target.password = '' } })
</script>

<div class="flex h-full min-h-0 flex-col">
  <PageHeader title="Schema comparison" subtitle="Compare environments and review changes">
    {#snippet actions()}{#if comparison}<Button size="sm" variant="outline" onclick={download}><Download size={13} /> Download review SQL</Button>{/if}{/snippet}
  </PageHeader>
  {#if loading}<div class="flex flex-1 items-center justify-center"><Spinner /></div>
  {:else if !connections.length && error}<EmptyState icon={AlertTriangle} title="Couldn't load connections" description={error} primary={{ label: 'Retry', onclick: load }} />
  {:else}
    <PageBody width="lg">
      <div class="space-y-5">
        <p class="text-sm text-fg-3">Choose a source definition and a target to compare. Review suggestions bring the target toward the source. Comparison only reads metadata.</p>
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
        {#if error}<p class="rounded-md border border-danger/30 p-3 text-sm text-danger" role="alert">{error}</p>{/if}
        {#if comparison}
          <Panel title={`${comparison.source.database} → ${comparison.target.database}`} description={`Captured ${new Date(comparison.captured_at).toLocaleString()}`}>
            <div class="space-y-3">
              <div class="flex flex-wrap gap-3 text-sm"><Badge tone={comparison.result.differences.length ? 'warning' : 'success'}>{comparison.result.differences.length} differences</Badge><span>{comparison.result.source_tables} source tables</span><ArrowRight size={14} /><span>{comparison.result.target_tables} target tables</span><span class="text-fg-3">{comparison.result.matching_tables} match</span></div>
              <p class="text-xs text-fg-3">{comparison.scope}</p>
              <p class="text-xs text-fg-3">SQL suggestions are commented for review. This page does not execute migrations.</p>
            </div>
          </Panel>
          {#if comparison.result.differences.length}
            <div class="flex flex-wrap gap-3"><label class="sr-only" for="schema-filter">Filter differences</label><Input id="schema-filter" type="search" class="w-64" placeholder="Filter tables or fields" bind:value={search} /><label class="sr-only" for="schema-kind">Difference type</label><Select id="schema-kind" class="w-44" bind:value={kind} options={[{ value: 'all', label: 'All differences' }, { value: 'changed', label: 'Changed definitions' }, { value: 'source_only', label: 'Missing in target' }, { value: 'target_only', label: 'Only in target' }]} /></div>
            <div class="space-y-3">
              {#each differences as diff}
                <Panel title={`${diff.table} · ${diff.field}`}>
                  {#snippet actions()}<Badge tone={diff.kind === 'changed' ? 'warning' : 'neutral'}>{diff.kind === 'source_only' ? 'Missing in target' : diff.kind === 'target_only' ? 'Only in target' : 'Changed'}</Badge>{/snippet}
                  <div class="space-y-3">
                    <div class="grid min-w-0 gap-3 md:grid-cols-2">
                      <div class="min-w-0"><p class="mb-1 text-xs text-fg-3">Source</p><pre class="overflow-x-auto whitespace-pre-wrap break-all rounded bg-surface-2 p-2 font-mono text-xs">{diff.source || '(absent)'}</pre></div>
                      <div class="min-w-0"><p class="mb-1 text-xs text-fg-3">Target</p><pre class="overflow-x-auto whitespace-pre-wrap break-all rounded bg-surface-2 p-2 font-mono text-xs">{diff.target || '(absent)'}</pre></div>
                    </div>
                    <details><summary class="cursor-pointer text-xs text-accent">Review suggestion</summary><pre class="mt-2 overflow-x-auto whitespace-pre-wrap break-all rounded bg-surface-2 p-3 font-mono text-xs">{diff.review_sql}</pre></details>
                  </div>
                </Panel>
              {/each}
              {#if !differences.length}<p class="text-sm text-fg-3">No differences match this filter.</p>{/if}
            </div>
          {:else}<EmptyState icon={GitCompare} title="Visible schemas match" description="No differences found in the compared fields and objects visible to these accounts." />{/if}
        {:else if !comparing && !error}<EmptyState icon={GitCompare} title="Compare two environments" description="Find schema drift before a deployment and export a reviewable SQL plan." />{/if}
      </div>
    </PageBody>
  {/if}
</div>
