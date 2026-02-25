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

<Modal {open} {title} onclose={oncancel}>
  <div class="space-y-4">
    {#if description}
      <p class="text-sm text-gray-600 dark:text-gray-300">{description}</p>
    {/if}

    <input
      bind:this={inputEl}
      bind:value
      {placeholder}
      onkeydown={handleKeydown}
      class="w-full rounded-lg border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-800 px-3 py-2 text-sm text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-orange-500 dark:focus:ring-orange-400"
      disabled={loading}
    />

    <div class="flex items-center justify-end gap-2 pt-1">
      <Button size="sm" variant="secondary" onclick={oncancel} disabled={loading}>{cancelLabel}</Button>
      <Button size="sm" variant="primary" onclick={() => onconfirm(value.trim())} {loading} disabled={!value.trim()}>{confirmLabel}</Button>
    </div>
  </div>
</Modal>
