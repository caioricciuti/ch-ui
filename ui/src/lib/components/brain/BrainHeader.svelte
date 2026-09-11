<script lang="ts">
  import type { BrainModelOption } from '../../types/brain'
  import type { ComboboxOption } from '../common/Combobox.svelte'
  import Combobox from '../common/Combobox.svelte'
  import PageHeader from '../common/PageHeader.svelte'

  interface Props {
    models: BrainModelOption[]
    selectedModelId: string
    /** Title of the open conversation, shown after the page name. */
    chatTitle?: string
    onModelChange: (modelId: string) => void
  }

  let { models, selectedModelId, chatTitle, onModelChange }: Props = $props()

  const modelOptions = $derived.by<ComboboxOption[]>(() =>
    models.map(m => ({
      value: m.id,
      label: `${m.display_name || m.name}`,
      hint: `${m.provider_name} · ${m.provider_kind}`,
      keywords: `${m.name} ${m.display_name || ''} ${m.provider_name} ${m.provider_kind}`,
    }))
  )
</script>

<PageHeader title="Brain" subtitle={chatTitle}>
  {#snippet actions()}
    <span class="hidden text-xs text-fg-4 md:block">
      Type <kbd class="rounded-sm bg-surface-2 px-1 py-0.5 font-mono text-[10px] text-fg-3">@</kbd> to add context
    </span>
    <div class="w-64">
      <Combobox
        options={modelOptions}
        value={selectedModelId}
        placeholder="Select model"
        onChange={(v) => onModelChange(v)}
      />
    </div>
  {/snippet}
</PageHeader>
