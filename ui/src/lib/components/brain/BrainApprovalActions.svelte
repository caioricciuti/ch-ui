<!-- SPDX-License-Identifier: BUSL-1.1 -->
<script lang="ts">
  import { approveBrainToolCall, declineBrainToolCall } from '../../api/brainPro'
  import Button from '../common/Button.svelte'
  import ConfirmDialog from '../common/ConfirmDialog.svelte'

  interface Props {
    approvalId?: string
    label: string
    /** Irreversible actions (deletes) ask once more before running. */
    destructive?: boolean
    decision: 'approved' | 'declined' | null
  }

  let { approvalId, label, destructive = false, decision = $bindable() }: Props = $props()
  let deciding = $state(false)
  let confirming = $state(false)
  let error = $state('')

  function message(e: unknown): string {
    return e instanceof Error && e.message ? e.message : 'Request failed'
  }

  async function approve() {
    if (!approvalId || deciding) return
    deciding = true
    error = ''
    try {
      await approveBrainToolCall(approvalId)
      decision = 'approved'
    } catch (e) {
      error = message(e)
    } finally {
      deciding = false
      confirming = false
    }
  }

  function onApprove() {
    if (destructive) {
      confirming = true
      return
    }
    void approve()
  }

  async function onDecline() {
    if (!approvalId || deciding) return
    deciding = true
    error = ''
    try {
      await declineBrainToolCall(approvalId)
      decision = 'declined'
    } catch (e) {
      error = message(e)
    } finally {
      deciding = false
    }
  }
</script>

<div class="border-t border-warning/30 px-3 py-2 flex flex-wrap items-center gap-2">
  <p class="text-xs text-fg-3 flex-1 min-w-0">
    Brain wants to <strong class="text-fg">{label.toLowerCase()}</strong>. Check the details above before you approve.
  </p>
  <Button size="xs" variant="outline" onclick={onDecline} disabled={deciding}>Decline</Button>
  <Button size="xs" variant={destructive ? 'danger' : 'primary'} onclick={onApprove} loading={deciding}>Approve</Button>
  {#if error}
    <p class="w-full text-xs text-danger" role="alert">{error}</p>
  {/if}
</div>

<ConfirmDialog
  open={confirming}
  title="{label}?"
  description="This cannot be undone."
  confirmLabel={label}
  destructive
  loading={deciding}
  onconfirm={() => void approve()}
  oncancel={() => (confirming = false)}
/>
