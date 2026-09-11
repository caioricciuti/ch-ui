<script lang="ts">
  import { ChevronDown } from 'lucide-svelte'

  export interface SelectOption {
    value: string
    label: string
    disabled?: boolean
  }

  interface Props {
    value?: string
    options: SelectOption[]
    placeholder?: string
    size?: 'sm' | 'md'
    disabled?: boolean
    id?: string
    class?: string
    onchange?: (value: string) => void
  }

  let { value = $bindable(''), options, placeholder, size = 'md', disabled = false, id, class: cls = '', onchange }: Props = $props()
</script>

<span class="relative inline-flex items-center {/\bw-/.test(cls) ? '' : 'w-full'} {cls}">
  <select
    {id}
    {disabled}
    bind:value
    class="{size === 'sm' ? 'ds-select h-7 w-full' : 'ds-select h-8 w-full text-[13px]'} appearance-none pr-7 disabled:cursor-not-allowed disabled:opacity-50"
    onchange={(e) => onchange?.(e.currentTarget.value)}
  >
    {#if placeholder}
      <option value="" disabled>{placeholder}</option>
    {/if}
    {#each options as opt (opt.value)}
      <option value={opt.value} disabled={opt.disabled}>{opt.label}</option>
    {/each}
  </select>
  <ChevronDown size={13} class="pointer-events-none absolute right-2 text-fg-4" />
</span>
