<script lang="ts">
  import type { BrainModelOption } from '../../types/brain'
  import type { ComboboxOption } from '../common/Combobox.svelte'
  import Combobox from '../common/Combobox.svelte'
  import { Brain } from 'lucide-svelte'

  interface Props {
    models: BrainModelOption[]
    selectedModelId: string
    onModelChange: (modelId: string) => void
  }

  let { models, selectedModelId, onModelChange }: Props = $props()

  const modelOptions = $derived.by<ComboboxOption[]>(() =>
    models.map(m => ({
      value: m.id,
      label: `${m.display_name || m.name}`,
      hint: `${m.provider_name} · ${m.provider_kind}`,
      keywords: `${m.name} ${m.display_name || ''} ${m.provider_name} ${m.provider_kind}`,
    }))
  )
</script>

<div class="border-b border-gray-200 dark:border-gray-800 px-4 py-2.5 flex items-center gap-3">
  <Brain size={18} class="text-ch-blue shrink-0" />
  <h1 class="text-lg font-semibold text-gray-900 dark:text-gray-100 shrink-0">Brain</h1>

  <p class="text-xs text-muted-foreground ml-1 hidden sm:block">Type <kbd class="px-1 py-0.5 rounded bg-gray-100 dark:bg-gray-800 text-[10px] font-mono">@</kbd> in the input to add context</p>

  <div class="ml-auto w-72 max-w-[35%] shrink-0">
    <Combobox
      options={modelOptions}
      value={selectedModelId}
      placeholder="Select model"
      onChange={(v) => onModelChange(v)}
    />
  </div>
</div>
