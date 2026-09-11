<script lang="ts">
  interface Props {
    value?: string | number
    type?: 'text' | 'password' | 'number' | 'url' | 'email' | 'search'
    placeholder?: string
    size?: 'sm' | 'md'
    invalid?: boolean
    disabled?: boolean
    readonly?: boolean
    id?: string
    name?: string
    autocomplete?: HTMLInputElement['autocomplete']
    spellcheck?: boolean
    required?: boolean
    min?: number | string
    max?: number | string
    step?: number | string
    maxlength?: number
    /** Passed straight to the element so callers can focus it. */
    ref?: HTMLInputElement
    mono?: boolean
    class?: string
    oninput?: (e: Event & { currentTarget: HTMLInputElement }) => void
    onchange?: (e: Event & { currentTarget: HTMLInputElement }) => void
    onkeydown?: (e: KeyboardEvent) => void
  }

  let {
    value = $bindable(''),
    type = 'text',
    placeholder = '',
    size = 'md',
    invalid = false,
    disabled = false,
    readonly = false,
    id,
    name,
    autocomplete,
    spellcheck,
    required = false,
    min,
    max,
    step,
    maxlength,
    ref = $bindable(),
    mono = false,
    class: cls = '',
    oninput,
    onchange,
    onkeydown,
  }: Props = $props()
</script>

<input
  {id}
  {name}
  {type}
  {placeholder}
  {disabled}
  {readonly}
  {autocomplete}
  {spellcheck}
  {required}
  {min}
  {max}
  {step}
  {maxlength}
  bind:this={ref}
  bind:value
  aria-invalid={invalid || undefined}
  class="{size === 'sm' ? 'ds-input-sm' : 'ds-input'} {mono ? 'font-mono' : ''} {invalid ? 'border-danger focus:border-danger focus:ring-danger/30' : ''} disabled:cursor-not-allowed disabled:opacity-50 {cls}"
  {oninput}
  {onchange}
  {onkeydown}
/>
