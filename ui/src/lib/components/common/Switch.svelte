<script lang="ts">
  /**
   * On/off control for a setting that applies immediately (enable a
   * schedule, create the table, require auth). A yes/no that needs a
   * Save button is a checkbox inside the form instead.
   */
  interface Props {
    checked: boolean
    onchange: (next: boolean) => void
    /** Accessible name; say what turning it on does ("Enable schedule"). */
    label: string
    disabled?: boolean
    size?: 'sm' | 'md'
    id?: string
    title?: string
    class?: string
  }

  let { checked, onchange, label, disabled = false, size = 'md', id, title, class: cls = '' }: Props = $props()

  const track: Record<string, string> = { sm: 'h-4 w-7', md: 'h-5 w-9' }
  const knob: Record<string, string> = { sm: 'h-3 w-3', md: 'h-4 w-4' }
  const travel: Record<string, string> = { sm: 'translate-x-3.5', md: 'translate-x-4' }
</script>

<button
  {id}
  type="button"
  role="switch"
  aria-checked={checked}
  aria-label={label}
  {title}
  {disabled}
  class="relative inline-flex shrink-0 items-center rounded-full transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/40 disabled:cursor-not-allowed disabled:opacity-50 {track[size]} {checked ? 'bg-accent' : 'bg-edge-strong'} {cls}"
  onclick={() => { if (!disabled) onchange(!checked) }}
>
  <span class="absolute left-0.5 rounded-full bg-fill-fg shadow-sm transition-transform {knob[size]} {checked ? travel[size] : ''}"></span>
</button>
