<script lang="ts">
  import { onMount, tick } from 'svelte'
  import { Check } from 'lucide-svelte'
  import Button from '../common/Button.svelte'
  import Badge from '../common/Badge.svelte'
  import Input from '../common/Input.svelte'
  import FormField from '../common/FormField.svelte'
  import Spinner from '../common/Spinner.svelte'
  import Tooltip from '../common/Tooltip.svelte'
  import ConfirmDialog from '../common/ConfirmDialog.svelte'
  import { listConnections } from '../../api/auth'
  import { getSession, switchConnection } from '../../stores/session.svelte'
  import { confirmUnload, hasUnpersistedChanges } from '../../stores/tabs.svelte'
  import { switchTarget, switchErrorView, type SwitchTarget } from './connection-switcher'
  import type { Connection } from '../../types/api'

  interface Props {
    /** Bounding box of the button that opened the switcher. */
    anchor: DOMRect | null
    onclose: () => void
  }

  let { anchor, onclose }: Props = $props()

  const PANEL_WIDTH = 300
  const session = $derived(getSession())
  const viaSSO = $derived(session?.viaSSO === true)

  let panelEl = $state<HTMLDivElement | null>(null)
  let passwordEl = $state<HTMLInputElement | undefined>()
  let connections = $state<Connection[]>([])
  let loading = $state(true)
  let loadError = $state('')
  let selectedId = $state<string | null>(null)
  let useCurrent = $state(true)
  let username = $state('')
  let password = $state('')
  let error = $state('')
  let busy = $state(false)
  let confirmOpen = $state(false)

  const targets = $derived(connections.map((c) => switchTarget(c, session)))
  const selected = $derived(targets.find((t) => t.connection.id === selectedId) ?? null)
  const left = $derived(anchor ? Math.min(anchor.right + 8, window.innerWidth - PANEL_WIDTH - 8) : 56)
  const bottom = $derived(anchor ? Math.max(8, window.innerHeight - anchor.bottom) : 8)

  async function load() {
    loading = true
    loadError = ''
    try {
      connections = await listConnections()
    } catch (e) {
      loadError = e instanceof Error ? e.message : 'Failed to load connections'
    } finally {
      loading = false
    }
  }

  function pick(t: SwitchTarget) {
    if (t.disabledReason || busy) return
    error = ''
    selectedId = t.connection.id
    // SSO people switch with the connection's service account: nothing to ask.
    if (viaSSO) void requestSwitch()
  }

  function requestSwitch() {
    if (!selected || busy) return
    if (hasUnpersistedChanges()) {
      confirmOpen = true
      return
    }
    void runSwitch()
  }

  async function runSwitch() {
    if (!selected) return
    // Only reached with unsaved tabs after the user confirmed: the browser
    // must not ask a second time on the reload.
    if (confirmOpen) confirmUnload()
    confirmOpen = false
    busy = true
    error = ''
    try {
      await switchConnection(
        viaSSO || useCurrent
          ? { connectionId: selected.connection.id }
          : { connectionId: selected.connection.id, username: username.trim(), password },
      )
      // switchConnection reloads the page; keep the busy state until it goes.
    } catch (e) {
      confirmUnload(false)
      const view = switchErrorView(e)
      error = view.message
      password = ''
      busy = false
      if (view.needsPassword && !viaSSO) {
        useCurrent = false
        await tick()
        passwordEl?.focus()
      }
    }
  }

  function handleWindowMousedown(e: MouseEvent) {
    if (confirmOpen) return
    if (panelEl && !panelEl.contains(e.target as Node)) onclose()
  }

  function handleWindowKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape' && !confirmOpen) onclose()
  }

  onMount(() => {
    username = session?.user ?? ''
    void load()
  })
</script>

<svelte:window onmousedown={handleWindowMousedown} onkeydown={handleWindowKeydown} />

<div
  bind:this={panelEl}
  class="surface-card fixed z-50 rounded-md py-2"
  style="left:{left}px;bottom:{bottom}px;width:{PANEL_WIDTH}px"
  role="dialog"
  aria-label="Switch connection"
>
  <div class="px-3 pb-1.5 text-xs font-medium text-fg-2">Switch connection</div>

  {#if loading}
    <div class="flex justify-center py-4"><Spinner size="sm" /></div>
  {:else if loadError}
    <div class="px-3 py-2 text-[13px] text-danger" role="alert">{loadError}</div>
    <div class="px-3"><Button size="sm" variant="outline" onclick={load}>Retry</Button></div>
  {:else if targets.length === 0}
    <div class="px-3 py-2 text-[13px] text-fg-3">No connections configured.</div>
  {:else}
    <ul class="max-h-64 overflow-y-auto px-1" role="listbox" aria-label="Connections">
      {#each targets as t (t.connection.id)}
        {@const isSelected = selectedId === t.connection.id}
        {@const blocked = t.disabledReason !== null && !t.current}
        <li role="option" aria-selected={t.current || isSelected} aria-disabled={t.disabledReason !== null}>
          {#snippet row()}
            <button
              type="button"
              class="flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-[13px] transition-colors
                {blocked ? 'pointer-events-none text-fg-4' : t.current ? 'cursor-default text-fg' : isSelected ? 'bg-active text-fg' : 'text-fg-2 hover:bg-hover'}"
              disabled={t.disabledReason !== null || busy}
              onclick={() => pick(t)}
            >
              <span class="flex w-3.5 shrink-0 justify-center text-accent">
                {#if t.current}<Check size={13} />{/if}
              </span>
              <span class="min-w-0 flex-1 truncate">{t.connection.name}</span>
              {#if t.connection.kind}<span class="shrink-0 text-[11px] text-fg-4">{t.connection.kind}</span>{/if}
              {#if blocked && t.disabledReason === 'SSO not set up'}
                <Badge tone="neutral">SSO not set up</Badge>
              {:else}
                <Badge dot tone={t.connection.online ? 'success' : 'danger'}>{t.connection.online ? 'Online' : 'Offline'}</Badge>
              {/if}
            </button>
          {/snippet}
          {#if blocked}
            <Tooltip text={t.disabledReason ?? ''} side="top" class="w-full cursor-not-allowed">{@render row()}</Tooltip>
          {:else}
            {@render row()}
          {/if}
        </li>
      {/each}
    </ul>

    {#if selected && !viaSSO}
      <form
        class="mt-2 space-y-2.5 border-t border-edge-subtle px-3 pt-2.5"
        onsubmit={(e) => { e.preventDefault(); requestSwitch() }}
      >
        <label class="ds-checkbox-label">
          <input type="checkbox" class="ds-checkbox ds-checkbox-sm" bind:checked={useCurrent} disabled={busy} />
          Use my current password
        </label>
        {#if useCurrent}
          <p class="text-xs text-fg-3">Signs in to {selected.connection.name} as {session?.user}.</p>
        {:else}
          <FormField label="ClickHouse username" for="switch-user">
            <Input id="switch-user" size="sm" bind:value={username} autocomplete="username" required disabled={busy} />
          </FormField>
          <FormField label="Password" for="switch-password">
            <Input id="switch-password" size="sm" type="password" bind:value={password} bind:ref={passwordEl} autocomplete="current-password" disabled={busy} />
          </FormField>
        {/if}
        {#if error}<p class="text-[13px] text-danger" role="alert">{error}</p>{/if}
        <div class="flex justify-end gap-2">
          <Button size="sm" variant="ghost" onclick={onclose} disabled={busy}>Cancel</Button>
          <Button size="sm" type="submit" loading={busy} disabled={!useCurrent && !username.trim()}>Switch</Button>
        </div>
      </form>
    {:else if error}
      <p class="mt-2 border-t border-edge-subtle px-3 pt-2 text-[13px] text-danger" role="alert">{error}</p>
    {:else if busy}
      <div class="mt-2 flex items-center gap-2 border-t border-edge-subtle px-3 pt-2 text-xs text-fg-3"><Spinner size="sm" /> Switching</div>
    {/if}
  {/if}
</div>

<ConfirmDialog
  open={confirmOpen}
  title="Switch with unsaved tabs?"
  description="Some edited tabs could not be saved in this browser (storage is full or unavailable). Switching reloads the page and loses them."
  confirmLabel="Switch anyway"
  destructive
  onconfirm={runSwitch}
  oncancel={() => (confirmOpen = false)}
/>
