<script lang="ts">
  import { apiGet, apiPost, apiDel } from '../../api/client'
  import { success as toastSuccess, error as toastError } from '../../stores/toast.svelte'
  import { withBase } from '../../basePath'
  import Button from '../common/Button.svelte'
  import Sheet from '../common/Sheet.svelte'
  import Spinner from '../common/Spinner.svelte'
  import { Copy, Trash2, ExternalLink, Code, Link, Mail, Globe, Lock, Send, X } from 'lucide-svelte'

  interface DashboardShare {
    id: string
    dashboard_id: string
    token: string
    access_level: string
    visibility: string
    allowed_emails: string[]
    expires_at: string | null
    created_by: string | null
    created_at: string
  }

  interface Props {
    open: boolean
    dashboardId: string
    dashboardName: string
    onclose: () => void
  }

  let { open, dashboardId, dashboardName, onclose }: Props = $props()

  let shares = $state<DashboardShare[]>([])
  let loading = $state(false)
  let creating = $state(false)

  // Create form state
  let newVisibility = $state<'public' | 'private'>('public')
  let emailInput = $state('')
  let emailTags = $state<string[]>([])

  // Invite sending state
  let sendingInvites = $state<Record<string, boolean>>({})

  $effect(() => {
    if (open && dashboardId) {
      loadShares()
    }
  })

  async function loadShares() {
    loading = true
    try {
      const res = await apiGet<{ shares: DashboardShare[] }>(`/api/dashboards/${dashboardId}/shares`)
      shares = res.shares ?? []
    } catch (e: unknown) {
      toastError(e instanceof Error ? e.message : 'Failed to load shares')
    } finally {
      loading = false
    }
  }

  function addEmail() {
    const raw = emailInput.trim().toLowerCase()
    if (!raw) return
    const emails = raw.split(/[,;\s]+/).filter(e => e.includes('@'))
    const unique = emails.filter(e => !emailTags.includes(e))
    if (unique.length > 0) {
      emailTags = [...emailTags, ...unique]
    }
    emailInput = ''
  }

  function removeEmail(email: string) {
    emailTags = emailTags.filter(e => e !== email)
  }

  function handleEmailKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter' || e.key === ',' || e.key === 'Tab') {
      e.preventDefault()
      addEmail()
    }
    if (e.key === 'Backspace' && !emailInput && emailTags.length > 0) {
      emailTags = emailTags.slice(0, -1)
    }
  }

  async function createShare() {
    if (newVisibility === 'private' && emailTags.length === 0) {
      toastError('Add at least one email for a private share')
      return
    }
    creating = true
    try {
      const res = await apiPost<{ share: DashboardShare }>(`/api/dashboards/${dashboardId}/shares`, {
        visibility: newVisibility,
        allowed_emails: newVisibility === 'private' ? emailTags : [],
      })
      if (res.share) {
        shares = [res.share, ...shares]
        toastSuccess('Share link created')
        emailTags = []
        emailInput = ''
        newVisibility = 'public'
      }
    } catch (e: unknown) {
      toastError(e instanceof Error ? e.message : 'Failed to create share')
    } finally {
      creating = false
    }
  }

  async function deleteShare(shareId: string) {
    try {
      await apiDel(`/api/dashboards/${dashboardId}/shares/${shareId}`)
      shares = shares.filter(s => s.id !== shareId)
      toastSuccess('Share link revoked')
    } catch (e: unknown) {
      toastError(e instanceof Error ? e.message : 'Failed to revoke share')
    }
  }

  async function sendInvites(share: DashboardShare) {
    sendingInvites = { ...sendingInvites, [share.id]: true }
    try {
      const res = await apiPost<{ sent: string[]; failed: string[] }>(
        `/api/dashboards/${dashboardId}/shares/${share.id}/invite`, {},
      )
      const sentCount = res.sent?.length ?? 0
      const failedCount = res.failed?.length ?? 0
      if (failedCount > 0) {
        toastError(`${failedCount} invite(s) failed to send`)
      }
      if (sentCount > 0) {
        toastSuccess(`${sentCount} invite(s) sent`)
      }
    } catch (e: unknown) {
      toastError(e instanceof Error ? e.message : 'Failed to send invites')
    } finally {
      sendingInvites = { ...sendingInvites, [share.id]: false }
    }
  }

  function getPublicUrl(token: string): string {
    return `${window.location.origin}${withBase(`/public/d/${token}`)}`
  }

  function getEmbedSnippet(token: string): string {
    const url = getPublicUrl(token)
    return `<iframe src="${url}" width="100%" height="600" frameborder="0"></iframe>`
  }

  async function copyToClipboard(text: string, label: string) {
    try {
      await navigator.clipboard.writeText(text)
      toastSuccess(`${label} copied to clipboard`)
    } catch {
      toastError('Failed to copy')
    }
  }

  function formatDate(ts: string): string {
    try {
      return new Date(ts).toLocaleString()
    } catch {
      return ts
    }
  }
</script>

<Sheet {open} title="Share Dashboard" size="md" onclose={onclose}>
  <div class="flex flex-col gap-4">
    <p class="text-xs text-gray-500 dark:text-gray-400">
      Share <span class="font-medium text-gray-700 dark:text-gray-300">{dashboardName}</span> with a public link or invite specific people via email.
    </p>

    <!-- Create new share -->
    <div class="border border-gray-200 dark:border-gray-800 rounded-lg p-3 space-y-3">
      <div class="flex gap-2">
        <button
          class="flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-medium border transition-colors
            {newVisibility === 'public'
              ? 'border-ch-blue bg-orange-50 dark:bg-orange-900/20 text-ch-blue'
              : 'border-gray-200 dark:border-gray-700 text-gray-500 hover:border-gray-300'}"
          onclick={() => newVisibility = 'public'}
        >
          <Globe size={14} /> Public
        </button>
        <button
          class="flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-medium border transition-colors
            {newVisibility === 'private'
              ? 'border-ch-blue bg-orange-50 dark:bg-orange-900/20 text-ch-blue'
              : 'border-gray-200 dark:border-gray-700 text-gray-500 hover:border-gray-300'}"
          onclick={() => newVisibility = 'private'}
        >
          <Lock size={14} /> Invite Only
        </button>
      </div>

      {#if newVisibility === 'private'}
        <div>
          <label for="share-email-input" class="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">Invite emails</label>
          <div class="flex flex-wrap gap-1.5 p-2 border border-gray-300 dark:border-gray-700 rounded bg-transparent min-h-[36px] focus-within:border-ch-blue">
            {#each emailTags as email}
              <span class="inline-flex items-center gap-1 px-2 py-0.5 rounded-full bg-gray-200 dark:bg-gray-700 text-xs text-gray-700 dark:text-gray-300">
                {email}
                <button class="hover:text-red-400" onclick={() => removeEmail(email)}>
                  <X size={10} />
                </button>
              </span>
            {/each}
            <input
              id="share-email-input"
              type="text"
              class="flex-1 min-w-[140px] text-xs bg-transparent outline-none text-gray-800 dark:text-gray-200 placeholder-gray-400"
              placeholder={emailTags.length === 0 ? 'user@example.com, ...' : 'Add more...'}
              bind:value={emailInput}
              onkeydown={handleEmailKeydown}
              onblur={addEmail}
            />
          </div>
          <p class="text-[10px] text-gray-400 mt-1">Press Enter or comma to add. Magic links will be sent via your configured email channel.</p>
        </div>
      {:else}
        <p class="text-[11px] text-gray-400">Anyone with the link can view this dashboard without logging in.</p>
      {/if}

      <div class="flex justify-end">
        <Button size="sm" loading={creating} onclick={createShare}>
          <Link size={14} /> Create Share Link
        </Button>
      </div>
    </div>

    <!-- Existing shares -->
    {#if loading}
      <div class="flex justify-center py-6"><Spinner /></div>
    {:else if shares.length === 0}
      <div class="text-center py-6 text-gray-400 dark:text-gray-600">
        <Link size={24} class="mx-auto mb-2" />
        <p class="text-xs">No share links yet</p>
      </div>
    {:else}
      <div class="flex flex-col gap-3">
        {#each shares as share (share.id)}
          <div class="border border-gray-200 dark:border-gray-800 rounded-lg p-3">
            <div class="flex items-center justify-between mb-2">
              <div class="flex items-center gap-2">
                {#if share.visibility === 'private'}
                  <span class="inline-flex items-center gap-1 text-[10px] uppercase tracking-wider font-medium px-1.5 py-0.5 rounded bg-purple-100 dark:bg-purple-900/30 text-purple-700 dark:text-purple-400">
                    <Lock size={10} /> invite only
                  </span>
                {:else}
                  <span class="inline-flex items-center gap-1 text-[10px] uppercase tracking-wider font-medium px-1.5 py-0.5 rounded bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-400">
                    <Globe size={10} /> public
                  </span>
                {/if}
                {#if share.expires_at}
                  <span class="text-[10px] text-gray-400">expires {formatDate(share.expires_at)}</span>
                {/if}
              </div>
              <button
                class="p-1 rounded text-gray-400 hover:text-red-400 hover:bg-gray-100 dark:hover:bg-gray-800"
                onclick={() => deleteShare(share.id)}
                title="Revoke"
              >
                <Trash2 size={14} />
              </button>
            </div>

            <!-- Allowed emails for private shares -->
            {#if share.visibility === 'private' && share.allowed_emails?.length > 0}
              <div class="flex flex-wrap gap-1 mb-2">
                {#each share.allowed_emails as email}
                  <span class="inline-flex items-center gap-1 px-2 py-0.5 rounded-full bg-gray-100 dark:bg-gray-800 text-[10px] text-gray-600 dark:text-gray-400">
                    <Mail size={10} /> {email}
                  </span>
                {/each}
              </div>
              <button
                class="flex items-center gap-1.5 text-[11px] font-medium text-ch-blue hover:text-ch-orange mb-2 disabled:opacity-50"
                onclick={() => sendInvites(share)}
                disabled={sendingInvites[share.id]}
              >
                <Send size={12} />
                {sendingInvites[share.id] ? 'Sending...' : 'Send invite emails'}
              </button>
            {/if}

            <div class="flex items-center gap-1.5 mb-2">
              <input
                type="text"
                readonly
                value={getPublicUrl(share.token)}
                class="flex-1 text-xs bg-gray-100 dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded px-2 py-1.5 text-gray-600 dark:text-gray-400 font-mono"
              />
              <button
                class="p-1.5 rounded border border-gray-200 dark:border-gray-700 text-gray-500 hover:text-gray-700 dark:hover:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-800"
                onclick={() => copyToClipboard(getPublicUrl(share.token), 'Link')}
                title="Copy link"
              >
                <Copy size={14} />
              </button>
              <a
                href={getPublicUrl(share.token)}
                target="_blank"
                rel="noopener noreferrer"
                class="p-1.5 rounded border border-gray-200 dark:border-gray-700 text-gray-500 hover:text-gray-700 dark:hover:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-800"
                title="Open in new tab"
              >
                <ExternalLink size={14} />
              </a>
            </div>

            {#if share.visibility === 'public'}
              <button
                class="flex items-center gap-1.5 text-[11px] text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
                onclick={() => copyToClipboard(getEmbedSnippet(share.token), 'Embed code')}
              >
                <Code size={12} /> Copy embed snippet
              </button>
            {/if}

            <p class="text-[10px] text-gray-400 mt-1.5">
              Created {formatDate(share.created_at)}{share.created_by ? ` by ${share.created_by}` : ''}
            </p>
          </div>
        {/each}
      </div>
    {/if}
  </div>
</Sheet>
