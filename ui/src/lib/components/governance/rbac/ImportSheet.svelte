<!-- SPDX-License-Identifier: BUSL-1.1 -->
<script lang="ts">
  import { Plus, Upload, X } from 'lucide-svelte'
  import type { RbacImportKind, RbacImportPlan, RbacOp, RbacPruneKind } from '../../../types/rbac'
  import { planRbacImport } from '../../../api/rbac'
  import { IMPORT_KINDS, buildImportRequest, fileProblem, importPlanFrom, summaryEntries, type RenameRow } from './importFile'
  import Sheet from '../../common/Sheet.svelte'
  import Button from '../../common/Button.svelte'
  import Badge from '../../common/Badge.svelte'
  import Input from '../../common/Input.svelte'
  import Textarea from '../../common/Textarea.svelte'
  import EmptyState from '../../common/EmptyState.svelte'

  /**
   * Import an access file exported from this or another connection. Preview
   * asks the server what would change; Stage hands the ops to the review
   * sheet, which plans, guards and applies them like any other change set.
   */
  interface Props {
    open: boolean
    /** Changes already staged; staging an import replaces them. */
    pendingCount: number
    onstage: (ops: RbacOp[]) => void
    onclose: () => void
  }

  let { open, pendingCount, onstage, onclose }: Props = $props()

  const MAX_BYTES = 900 * 1024

  let file = $state('')
  let fileName = $state('')
  let include = $state<Record<RbacImportKind, boolean>>({ roles: true, users: true, grants: true, role_grants: true, row_policies: true, quotas: true, profiles: true })
  let prune = $state<Record<RbacPruneKind, boolean>>({ roles: false, grants: false, role_grants: false, row_policies: false, quotas: false, profiles: false })
  let principals = $state<RenameRow[]>([])
  let databases = $state<RenameRow[]>([])
  let plan = $state<RbacImportPlan | null>(null)
  let previewing = $state(false)
  let error = $state('')
  let fileInput = $state<HTMLInputElement | null>(null)
  let request = 0

  const anyPrune = $derived(IMPORT_KINDS.some((k) => k.prunable && include[k.kind] && prune[k.kind as RbacPruneKind]))
  const problem = $derived(fileProblem(error))
  const canStage = $derived(!!plan && plan.ops.length > 0 && !plan.over_cap && !previewing)

  // Any edit makes the preview stale.
  function changed() {
    plan = null
    error = ''
    request++
  }

  async function pickFile(e: Event & { currentTarget: HTMLInputElement }) {
    const f = e.currentTarget.files?.[0]
    e.currentTarget.value = ''
    if (!f) return
    if (f.size > MAX_BYTES) {
      error = `${f.name} is ${Math.round(f.size / 1024)} KB; the limit is ${Math.round(MAX_BYTES / 1024)} KB.`
      return
    }
    file = await f.text()
    fileName = f.name
    changed()
  }

  async function preview() {
    const built = buildImportRequest(file, include, prune, principals, databases)
    if (!built.request) {
      error = built.error
      plan = null
      return
    }
    const id = ++request
    previewing = true
    error = ''
    try {
      const next = importPlanFrom(await planRbacImport(built.request))
      if (id === request) plan = next
    } catch (err: unknown) {
      if (id !== request) return
      plan = null
      error = err instanceof Error ? err.message : String(err)
    } finally {
      if (id === request) previewing = false
    }
  }

  function stage() {
    if (!plan || !canStage) return
    onstage(plan.ops)
  }

  function addRow(list: 'principals' | 'databases') {
    if (list === 'principals') principals = [...principals, { from: '', to: '' }]
    else databases = [...databases, { from: '', to: '' }]
    changed()
  }

  function removeRow(list: 'principals' | 'databases', index: number) {
    if (list === 'principals') principals = principals.filter((_, i) => i !== index)
    else databases = databases.filter((_, i) => i !== index)
    changed()
  }
</script>

{#snippet renameList(list: 'principals' | 'databases', rows: RenameRow[], title: string, hint: string)}
  <div class="space-y-2">
    <div class="flex items-center justify-between gap-2">
      <div>
        <h4 class="text-xs font-medium text-fg-2">{title}</h4>
        <p class="text-xs text-fg-3">{hint}</p>
      </div>
      <Button size="xs" variant="ghost" onclick={() => addRow(list)}><Plus size={13} /> Add</Button>
    </div>
    {#each rows as row, i (i)}
      <div class="flex items-center gap-2">
        <label class="sr-only" for={`import-${list}-${i}-from`}>{title}: name in the file</label>
        <Input id={`import-${list}-${i}-from`} size="sm" mono class="flex-1" placeholder="name in the file" bind:value={row.from} oninput={changed} spellcheck={false} />
        <span class="text-xs text-fg-4">to</span>
        <label class="sr-only" for={`import-${list}-${i}-to`}>{title}: name on this connection</label>
        <Input id={`import-${list}-${i}-to`} size="sm" mono class="flex-1" placeholder="name here" bind:value={row.to} oninput={changed} spellcheck={false} />
        <Button icon size="sm" variant="ghost" aria-label="Remove rename" title="Remove" onclick={() => removeRow(list, i)}><X size={13} /></Button>
      </div>
    {/each}
  </div>
{/snippet}

<Sheet {open} title="Import access" description="Make this connection match an access file. Preview first; nothing changes until you review and apply." size="lg" {onclose}>
  <div class="space-y-6 text-[13px]">
    <section class="space-y-2">
      <div class="flex items-center justify-between gap-2">
        <h3 class="text-[13px] font-semibold text-fg">File</h3>
        <div class="flex items-center gap-2">
          {#if fileName}<span class="truncate text-xs text-fg-3">{fileName}</span>{/if}
          <Button size="sm" variant="outline" onclick={() => fileInput?.click()}><Upload size={13} /> Choose file</Button>
          <input bind:this={fileInput} type="file" accept=".yaml,.yml,.json,application/json,application/yaml,text/yaml" class="hidden" onchange={pickFile} />
        </div>
      </div>
      <Textarea
        rows={10}
        mono
        bind:value={file}
        invalid={!!problem}
        placeholder={'chui_access: 1\nroles:\n  analyst: {profile: readonly_p}\ngrants:\n  analyst:\n    - {access: [SELECT], on: "analytics.*"}'}
        oninput={() => {
          fileName = ''
          changed()
        }}
      />
      {#if problem}
        <p class="text-xs text-danger" role="alert">{problem}</p>
      {:else}
        <p class="text-xs text-fg-3">YAML or JSON, as written by Export. Users are never created, and passwords are never part of the file.</p>
      {/if}
    </section>

    <section class="space-y-2">
      <h3 class="text-[13px] font-semibold text-fg">What to import</h3>
      <div class="divide-y divide-edge-subtle rounded-lg border border-edge-subtle bg-surface">
        <div class="flex items-center gap-3 px-3 py-1.5 text-xs text-fg-4">
          <span class="flex-1">Part of the file</span>
          <span class="w-16 text-center">Import</span>
          <span class="w-16 text-center">Prune</span>
        </div>
        {#each IMPORT_KINDS as k (k.kind)}
          <div class="flex items-center gap-3 px-3 py-1.5">
            <span class="flex-1 text-fg-2">{k.label}</span>
            <span class="flex w-16 justify-center">
              <input type="checkbox" class="ds-checkbox ds-checkbox-sm" aria-label={`Import ${k.label}`} bind:checked={include[k.kind]} onchange={changed} />
            </span>
            <span class="flex w-16 justify-center">
              {#if k.prunable}
                <input
                  type="checkbox"
                  class="ds-checkbox ds-checkbox-sm"
                  aria-label={`Prune ${k.label}`}
                  disabled={!include[k.kind]}
                  bind:checked={prune[k.kind as RbacPruneKind]}
                  onchange={changed}
                />
              {:else}
                <span class="text-xs text-fg-4">-</span>
              {/if}
            </span>
          </div>
        {/each}
      </div>
      <p class="text-xs {anyPrune ? 'text-warning' : 'text-fg-3'}">
        Prune revokes or drops what the file does not list, for the parts ticked. Without it, anything missing from the file is left alone. Entities in users.xml are never changed.
      </p>
    </section>

    <section class="space-y-4">
      <h3 class="text-[13px] font-semibold text-fg">Rename</h3>
      {@render renameList('principals', principals, 'Users and roles', 'Applied to entries, grantees, granted roles, default roles and TO lists.')}
      {@render renameList('databases', databases, 'Databases', 'Applied to the database of privileges and row policies.')}
    </section>

    <section class="space-y-3">
      <h3 class="text-[13px] font-semibold text-fg">Preview</h3>
      {#if error && !problem}
        <div class="rounded-md bg-danger-soft px-3 py-2 text-xs text-danger" role="alert">{error}</div>
      {/if}
      {#if !plan}
        <EmptyState size="compact" title="No preview yet" description="Preview asks the server what this file would change here. It changes nothing." />
      {:else}
        {#if plan.source?.connection || plan.source?.exported_at}
          <p class="text-xs text-fg-3">
            Exported from {plan.source.connection || 'an unnamed connection'}{plan.source.host ? ` (${plan.source.host})` : ''}{plan.source.exported_at ? ` at ${plan.source.exported_at}` : ''}.
          </p>
        {/if}
        {#if plan.ops.length === 0}
          <EmptyState size="compact" title="Nothing to change" description="This connection already matches the parts of the file you picked." />
        {:else}
          <div class="flex flex-wrap items-center gap-2">
            <Badge tone="info">{plan.ops.length} change{plan.ops.length === 1 ? '' : 's'}</Badge>
            {#each summaryEntries(plan.summary) as s (s.kind)}
              <Badge>{s.label}: {s.count}</Badge>
            {/each}
          </div>
        {/if}
        {#if plan.over_cap}
          <div class="rounded-md bg-warning-soft px-3 py-2 text-xs text-fg">{plan.message || 'Too many changes for one plan. Import fewer parts at a time.'}</div>
        {/if}
        {#if plan.skipped.length}
          <div class="space-y-1">
            <h4 class="text-xs font-medium text-fg-2">Skipped ({plan.skipped.length})</h4>
            <ul class="max-h-60 divide-y divide-edge-subtle overflow-auto rounded-lg border border-edge-subtle bg-surface">
              {#each plan.skipped as s, i (i)}
                <li class="flex items-start gap-2 px-3 py-1.5 text-xs">
                  <Badge tone="warning">{s.kind.split('_').join(' ')}</Badge>
                  <span class="min-w-0 flex-1 text-fg-2"><span class="font-mono text-fg">{s.name}</span>{s.name ? ': ' : ''}{s.reason}</span>
                </li>
              {/each}
            </ul>
          </div>
        {/if}
      {/if}
    </section>
  </div>

  {#snippet footer()}
    {#if canStage && pendingCount > 0}
      <span class="mr-auto text-xs text-fg-3">Replaces the {pendingCount} pending change{pendingCount === 1 ? '' : 's'}.</span>
    {/if}
    <Button size="sm" variant="ghost" onclick={onclose}>Cancel</Button>
    <Button size="sm" variant="outline" loading={previewing} disabled={!file.trim()} onclick={() => void preview()}>Preview</Button>
    <Button size="sm" disabled={!canStage} onclick={stage}>Stage {plan && plan.ops.length ? `${plan.ops.length} change${plan.ops.length === 1 ? '' : 's'}` : 'changes'}</Button>
  {/snippet}
</Sheet>
