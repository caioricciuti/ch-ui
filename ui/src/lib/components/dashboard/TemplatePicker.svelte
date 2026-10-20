<script lang="ts">
  import type { DashboardTemplate } from '../../types/api'
  import { listDashboardTemplates, createDashboardFromTemplate, type DashboardDetail } from '../../api/dashboards'
  import { success as toastSuccess, errorFrom as toastErrorFrom } from '../../stores/toast.svelte'
  import Sheet from '../common/Sheet.svelte'
  import Button from '../common/Button.svelte'
  import Badge from '../common/Badge.svelte'
  import Input from '../common/Input.svelte'
  import Select, { type SelectOption } from '../common/Select.svelte'
  import FormField from '../common/FormField.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import Spinner from '../common/Spinner.svelte'

  interface Props {
    open: boolean
    /** Folder the dashboard is created in unless the user picks another. */
    folderId: string | null
    folderOptions: SelectOption[]
    onclose: () => void
    oncreated: (detail: DashboardDetail) => void
  }

  let { open, folderId, folderOptions, onclose, oncreated }: Props = $props()

  let templates = $state<DashboardTemplate[]>([])
  let loading = $state(false)
  let creating = $state(false)
  let selected = $state<string | null>(null)
  let name = $state('')
  let folder = $state('')

  const picked = $derived(templates.find((t) => t.id === selected) ?? null)

  async function load() {
    loading = true
    try {
      templates = await listDashboardTemplates()
      if (!templates.some((t) => t.id === selected)) selected = templates[0]?.id ?? null
    } catch (e: unknown) {
      toastErrorFrom(e)
    } finally {
      loading = false
    }
  }

  let wasOpen = false
  $effect(() => {
    if (open && !wasOpen) {
      name = ''
      folder = folderId ?? ''
      void load()
    }
    wasOpen = open
  })

  async function submit() {
    if (!picked) return
    creating = true
    try {
      const detail = await createDashboardFromTemplate({
        template_id: picked.id,
        name: name.trim() || undefined,
        folder_id: folder || null,
      })
      toastSuccess(`Created "${detail.dashboard.name}" from ${picked.name}`)
      oncreated(detail)
    } catch (e: unknown) {
      toastErrorFrom(e)
    } finally {
      creating = false
    }
  }
</script>

<Sheet {open} title="New from template" description="Start from a ready-made set of panels and variables." size="lg" {onclose}>
  {#if loading && templates.length === 0}
    <div class="flex items-center justify-center py-16"><Spinner /></div>
  {:else if templates.length === 0}
    <EmptyState size="compact" title="No templates available" description="The server did not return any dashboard templates." />
  {:else}
    <div class="space-y-5">
      <div class="grid grid-cols-1 gap-2 md:grid-cols-2" role="radiogroup" aria-label="Templates">
        {#each templates as t (t.id)}
          {@const active = selected === t.id}
          <button
            class="flex flex-col items-start gap-2 rounded-lg border p-3 text-left transition-colors {active ? 'border-accent bg-surface' : 'border-edge-subtle bg-surface hover:border-edge-strong'}"
            role="radio"
            aria-checked={active}
            onclick={() => (selected = t.id)}
          >
            <div class="flex w-full items-start justify-between gap-2">
              <span class="text-[13px] font-semibold text-fg">{t.name}</span>
              <span class="shrink-0 text-xs tabular-nums text-fg-3">{t.panel_count} {t.panel_count === 1 ? 'panel' : 'panels'}</span>
            </div>
            {#if t.description}
              <p class="text-xs leading-relaxed text-fg-3">{t.description}</p>
            {/if}
            {#if t.tags.length > 0}
              <span class="inline-flex flex-wrap gap-1">
                {#each t.tags as tag (tag)}<Badge>{tag}</Badge>{/each}
              </span>
            {/if}
            {#if t.variables.length > 0}
              <span class="font-mono text-[11px] text-fg-4">{t.variables.map((v) => '$' + v).join(' ')}</span>
            {/if}
          </button>
        {/each}
      </div>

      <FormField label="Name" for="tpl-name" controlWidth="full" hint={picked ? `Leave empty to use "${picked.name}".` : undefined}>
        <Input id="tpl-name" placeholder={picked?.name ?? 'Dashboard name'} bind:value={name} />
      </FormField>
      <FormField label="Folder" for="tpl-folder" controlWidth="full">
        <Select id="tpl-folder" options={folderOptions} bind:value={folder} />
      </FormField>
    </div>
  {/if}
  {#snippet footer()}
    <Button variant="ghost" size="sm" onclick={onclose}>Cancel</Button>
    <Button size="sm" loading={creating} disabled={!picked} onclick={submit}>Create</Button>
  {/snippet}
</Sheet>
