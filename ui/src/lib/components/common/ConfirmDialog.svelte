<script lang="ts">
  import Modal from './Modal.svelte'
  import Button from './Button.svelte'

  interface Props {
    open: boolean
    title: string
    description?: string
    confirmLabel?: string
    cancelLabel?: string
    loading?: boolean
    destructive?: boolean
    onconfirm: () => void
    oncancel: () => void
  }

  let {
    open,
    title,
    description = '',
    confirmLabel = 'Confirm',
    cancelLabel = 'Cancel',
    loading = false,
    destructive = false,
    onconfirm,
    oncancel,
  }: Props = $props()
</script>

<Modal {open} title={title} onclose={oncancel}>
  <div class="space-y-4">
    {#if description}
      <p class="text-sm text-gray-600 dark:text-gray-300">{description}</p>
    {/if}

    <div class="flex items-center justify-end gap-2 pt-1">
      <Button size="sm" variant="secondary" onclick={oncancel} disabled={loading}>{cancelLabel}</Button>
      <Button size="sm" variant={destructive ? 'danger' : 'primary'} onclick={onconfirm} {loading}>{confirmLabel}</Button>
    </div>
  </div>
</Modal>
