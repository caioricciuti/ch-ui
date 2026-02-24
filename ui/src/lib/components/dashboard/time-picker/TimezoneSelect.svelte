<script lang="ts">
  interface Props {
    value: string
    onchange: (tz: string) => void
  }

  let { value, onchange }: Props = $props()

  const browserTz = Intl.DateTimeFormat().resolvedOptions().timeZone
</script>

<div class="flex items-center gap-2">
  <span class="text-xs text-gray-500 dark:text-gray-400 shrink-0">Timezone:</span>
  <select
    class="ds-select text-xs flex-1"
    {value}
    onchange={(e) => onchange(e.currentTarget.value)}
  >
    <option value="UTC">UTC</option>
    <option value="local">{browserTz}</option>
  </select>
</div>
