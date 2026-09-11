<script lang="ts">
  interface Props {
    value?: string
    placeholder?: string
    rows?: number
    invalid?: boolean
    disabled?: boolean
    id?: string
    mono?: boolean
    required?: boolean
    class?: string
    oninput?: (e: Event & { currentTarget: HTMLTextAreaElement }) => void
  }

  let { value = $bindable(''), placeholder = '', rows = 3, invalid = false, disabled = false, id, mono = false, required = false, class: cls = '', oninput }: Props = $props()
</script>

<textarea
  {id}
  {rows}
  {placeholder}
  {disabled}
  {required}
  bind:value
  aria-invalid={invalid || undefined}
  class="w-full rounded-md border border-edge bg-surface px-2.5 py-2 text-[13px] leading-relaxed text-fg placeholder:text-fg-4 transition-colors duration-100 hover:border-edge-strong focus:border-accent focus:outline-none {mono ? 'font-mono text-xs' : ''} {invalid ? 'border-danger focus:border-danger focus:ring-danger/30' : ''} disabled:cursor-not-allowed disabled:opacity-50 {cls}"
  {oninput}
></textarea>
