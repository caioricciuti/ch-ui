<script lang="ts">
  import { EXTENDED_PRESETS } from '../../../utils/chart-transform'

  interface Props {
    currentValue: string
    onselect: (value: string) => void
  }

  let { currentValue, onselect }: Props = $props()

  const recentPresets = EXTENDED_PRESETS.filter((p) => p.group === 'recent')
  const namedPresets = EXTENDED_PRESETS.filter((p) => p.group === 'named')
  const durationPresets = EXTENDED_PRESETS.filter((p) => p.group === 'duration')
</script>

<div class="flex flex-col h-full overflow-y-auto px-1.5">
  {#each recentPresets as preset}
    <button
      type="button"
      class="text-left px-3 py-1.5 text-xs rounded transition-colors whitespace-nowrap
        {currentValue === preset.value
          ? 'text-ch-orange bg-orange-100/60 dark:bg-orange-900/20 font-medium'
          : 'text-gray-600 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-800 hover:text-gray-800 dark:hover:text-gray-200'}"
      onclick={() => onselect(preset.value)}
    >
      {preset.label}
    </button>
  {/each}

  <div class="border-t border-gray-200 dark:border-gray-700 my-1.5"></div>

  {#each namedPresets as preset}
    <button
      type="button"
      class="text-left px-3 py-1.5 text-xs rounded transition-colors whitespace-nowrap
        {currentValue === preset.value
          ? 'text-ch-orange bg-orange-100/60 dark:bg-orange-900/20 font-medium'
          : 'text-gray-600 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-800 hover:text-gray-800 dark:hover:text-gray-200'}"
      onclick={() => onselect(preset.value)}
    >
      {preset.label}
    </button>
  {/each}

  <div class="border-t border-gray-200 dark:border-gray-700 my-1.5"></div>

  {#each durationPresets as preset}
    <button
      type="button"
      class="text-left px-3 py-1.5 text-xs rounded transition-colors whitespace-nowrap
        {currentValue === preset.value
          ? 'text-ch-orange bg-orange-100/60 dark:bg-orange-900/20 font-medium'
          : 'text-gray-600 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-800 hover:text-gray-800 dark:hover:text-gray-200'}"
      onclick={() => onselect(preset.value)}
    >
      {preset.label}
    </button>
  {/each}
</div>
