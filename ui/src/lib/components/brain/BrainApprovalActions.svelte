<!-- SPDX-License-Identifier: BUSL-1.1 -->
<script lang="ts">
  import { approveBrainToolCall, declineBrainToolCall } from '../../api/brainPro'

  interface Props {
    approvalId?: string
    label: string
    decision: 'approved' | 'declined' | null
  }

  let { approvalId, label, decision = $bindable() }: Props = $props()
  let deciding = $state(false)

  async function onApprove() {
    if (!approvalId || deciding) return
    deciding = true
    try {
      await approveBrainToolCall(approvalId)
      decision = 'approved'
    } catch (e) {
      console.error('approve failed', e)
    } finally {
      deciding = false
    }
  }

  async function onDecline() {
    if (!approvalId || deciding) return
    deciding = true
    try {
      await declineBrainToolCall(approvalId)
      decision = 'declined'
    } catch (e) {
      console.error('decline failed', e)
    } finally {
      deciding = false
    }
  }
</script>

<div class="border-t border-warning/30 px-3 py-2 flex flex-wrap items-center gap-2">
  <p class="text-[11px] text-muted-foreground flex-1 min-w-0">
    Brain wants to <strong class="text-foreground">{label.toLowerCase()}</strong>. Review the details and approve to run.
  </p>
  <button
    type="button"
    onclick={onDecline}
    disabled={deciding}
    class="px-3 py-1 rounded-md text-[11px] font-medium border border-border bg-background hover:bg-secondary disabled:opacity-50"
  >Decline</button>
  <button
    type="button"
    onclick={onApprove}
    disabled={deciding}
    class="px-3 py-1 rounded-md text-[11px] font-medium bg-warning hover:brightness-110 text-white disabled:opacity-50"
  >Approve</button>
</div>
