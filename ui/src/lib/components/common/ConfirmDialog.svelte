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

<Modal {open} {title} size="sm" onclose={oncancel}>
  {#if description}
    <p class="text-[13px] leading-relaxed text-fg-2">{description}</p>
  {/if}
  {#snippet footer()}
    <Button size="sm" variant="outline" onclick={oncancel} disabled={loading}>{cancelLabel}</Button>
    <Button size="sm" variant={destructive ? 'danger' : 'primary'} onclick={onconfirm} {loading}>{confirmLabel}</Button>
  {/snippet}
</Modal>
