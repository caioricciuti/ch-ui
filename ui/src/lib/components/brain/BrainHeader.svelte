<script lang="ts">
  import type { BrainModelOption } from '../../types/brain'
  import type { ComboboxOption } from '../common/Combobox.svelte'
  import Combobox from '../common/Combobox.svelte'
  import { Brain, X } from 'lucide-svelte'

  interface Props {
    models: BrainModelOption[]
    selectedModelId: string
    selectedDb: string
    selectedTable: string
    contextColumns: { name: string; type: string }[]
    databaseOptions: ComboboxOption[]
    tableOptions: ComboboxOption[]
    onModelChange: (modelId: string) => void
    onDbChange: (db: string) => void
    onTableChange: (table: string) => void
    onClearContext: () => void
  }

  let {
    models,
    selectedModelId,
    selectedDb,
    selectedTable,
    contextColumns,
    databaseOptions,
    tableOptions,
    onModelChange,
    onDbChange,
    onTableChange,
    onClearContext,
  }: Props = $props()

  const modelOptions = $derived.by<ComboboxOption[]>(() =>
    models.map(m => ({
      value: m.id,
      label: `${m.display_name || m.name}`,
      hint: `${m.provider_name} · ${m.provider_kind}`,
      keywords: `${m.name} ${m.display_name || ''} ${m.provider_name} ${m.provider_kind}`,
    }))
  )

  const hasContext = $derived(selectedDb && selectedTable && contextColumns.length > 0)
</script>

<div class="border-b border-gray-200 dark:border-gray-800 px-4 py-2.5 flex items-center gap-3">
  <Brain size={18} class="text-ch-blue shrink-0" />
  <h1 class="text-lg font-semibold text-gray-900 dark:text-gray-100 shrink-0">Brain</h1>

  <!-- Context controls -->
  <div class="flex items-center gap-2 ml-2">
    <div class="w-40">
      <Combobox
        options={databaseOptions}
        value={selectedDb}
        placeholder="Database..."
        onChange={(v) => onDbChange(v)}
      />
    </div>

    {#if selectedDb}
      <div class="w-40">
        <Combobox
          options={tableOptions}
          value={selectedTable}
          placeholder="Table..."
          onChange={(v) => onTableChange(v)}
        />
      </div>
    {/if}

    {#if hasContext}
      <span class="inline-flex items-center gap-1 px-2 py-1 rounded-full text-[11px] font-medium bg-ch-blue/10 text-ch-blue">
        {selectedDb}.{selectedTable}
        <span class="text-ch-blue/60">({contextColumns.length} cols)</span>
        <button
          class="ml-0.5 rounded-full p-0.5 hover:bg-ch-blue/20 transition-colors"
          onclick={onClearContext}
          title="Clear context"
        >
          <X size={10} />
        </button>
      </span>
    {/if}
  </div>

  <div class="ml-auto w-72 max-w-[35%] shrink-0">
    <Combobox
      options={modelOptions}
      value={selectedModelId}
      placeholder="Select model"
      onChange={(v) => onModelChange(v)}
    />
  </div>
</div>
