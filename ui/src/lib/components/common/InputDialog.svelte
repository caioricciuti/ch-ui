<script lang="ts">
  import Modal from './Modal.svelte'
  import Button from './Button.svelte'

  interface Props {
    open: boolean
    title: string
    description?: string
    placeholder?: string
    value: string
    confirmLabel?: string
    cancelLabel?: string
    loading?: boolean
    onconfirm: (value: string) => void
    oncancel: () => void
  }

  let {
    open,
    title,
    description = '',
    placeholder = '',
    value = $bindable(''),
    confirmLabel = 'Save',
    cancelLabel = 'Cancel',
    loading = false,
    onconfirm,
    oncancel,
  }: Props = $props()

  let inputEl: HTMLInputElement | undefined = $state()

  $effect(() => {
    if (open && inputEl) {
      inputEl.focus()
      inputEl.select()
    }
  })

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter' && value.trim()) {
      e.preventDefault()
      onconfirm(value.trim())
    }
  }
</script>

<Modal {open} {title} {description} size="sm" onclose={oncancel}>
  <input
    bind:this={inputEl}
    bind:value
    {placeholder}
    onkeydown={handleKeydown}
    class="ds-input"
    disabled={loading}
  />
  {#snippet footer()}
    <Button size="sm" variant="outline" onclick={oncancel} disabled={loading}>{cancelLabel}</Button>
    <Button size="sm" variant="primary" onclick={() => onconfirm(value.trim())} {loading} disabled={!value.trim()}>{confirmLabel}</Button>
  {/snippet}
</Modal>
