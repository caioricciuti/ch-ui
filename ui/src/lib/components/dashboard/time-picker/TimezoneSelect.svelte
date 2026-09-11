<script lang="ts">
  import Select from '../../common/Select.svelte'
  interface Props {
    value: string
    onchange: (tz: string) => void
  }

  let { value, onchange }: Props = $props()

  const browserTz = Intl.DateTimeFormat().resolvedOptions().timeZone
  const options = [
    { value: 'UTC', label: 'UTC' },
    { value: 'local', label: browserTz },
  ]
</script>

<div class="flex items-center gap-2">
  <span class="text-xs text-fg-3 shrink-0">Timezone:</span>
  <Select size="sm" class="flex-1" {options} {value} {onchange} />
</div>
