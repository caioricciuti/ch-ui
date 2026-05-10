<script lang="ts">
  import { SEVERITY_LEVELS, SEVERITY_COLORS, type SeverityLevel } from '../../types/telemetry'

  interface Props {
    selected: SeverityLevel[]
    onchange: (selected: SeverityLevel[]) => void
  }

  let { selected, onchange }: Props = $props()

  function toggle(level: SeverityLevel) {
    if (selected.includes(level)) {
      onchange(selected.filter(s => s !== level))
    } else {
      onchange([...selected, level])
    }
  }

  function isActive(level: SeverityLevel): boolean {
    return selected.length === 0 || selected.includes(level)
  }
</script>

<div class="flex items-center gap-1">
  {#each SEVERITY_LEVELS as level}
    <button
      class="px-2 py-0.5 text-[10px] font-semibold rounded-full border transition-all
        {isActive(level)
          ? 'border-current opacity-100'
          : 'border-gray-300 dark:border-gray-700 opacity-30 hover:opacity-60'}"
      style={isActive(level) ? `color: ${SEVERITY_COLORS[level]}; border-color: ${SEVERITY_COLORS[level]}; background: ${SEVERITY_COLORS[level]}15` : ''}
      onclick={() => toggle(level)}
    >
      {level}
    </button>
  {/each}
</div>
