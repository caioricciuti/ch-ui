<script lang="ts">
  import { onMount, untrack, type Component } from 'svelte'
  import { initSession, isAuthenticated, isInitializing } from './lib/stores/session.svelte'
  import { initRouter } from './lib/stores/router.svelte'
  import { stripBase, withBase } from './lib/basePath'
  import { safeNextPath } from './lib/api/client'

  import Toast from './lib/components/common/Toast.svelte'
  import Spinner from './lib/components/common/Spinner.svelte'
  import Login from './pages/Login.svelte'
  import logo from './assets/logo.png'

  // The workspace, the public dashboard and the OAuth consent page load on
  // demand, so the sign-in page does not download the editor, charts and
  // pipeline canvas.
  let Shell = $state<Component | null>(null)
  let PublicDashboard = $state<Component<{ token: string }> | null>(null)
  let OAuthConsent = $state<Component<{ requestId: string }> | null>(null)
  let loadError = $state<string | null>(null)

  function getPublicDashboardToken(): string | null {
    const path = stripBase(window.location.pathname)
    const match = path.match(/^\/public\/d\/([a-f0-9-]+)$/)
    return match?.[1] ?? null
  }

  let publicToken = $state(getPublicDashboardToken())

  // /oauth/consent?request=<id>: the MCP OAuth consent page. It needs a
  // session (Login renders first when there is none) but not the workspace.
  function getOAuthRequestId(): string | null {
    if (stripBase(window.location.pathname) !== '/oauth/consent') return null
    return new URLSearchParams(window.location.search).get('request')
  }
  let oauthRequestId = $state(getOAuthRequestId())

  function reportLoadError(e: unknown): void {
    loadError = e instanceof Error ? e.message : 'Failed to load the app'
  }

  onMount(() => {
    if (publicToken) {
      import('./pages/PublicDashboard.svelte').then((m) => (PublicDashboard = m.default), reportLoadError)
      return
    }
    void initSession()
  })

  const authenticated = $derived(isAuthenticated())
  const initializing = $derived(isInitializing())

  /**
   * After sign-in on /login?next=..., go back to the page the expired
   * session left. Any other URL is already the right place.
   */
  function restoreNext(): void {
    if (stripBase(window.location.pathname) !== '/login') return
    const next = safeNextPath(new URLSearchParams(window.location.search).get('next'))
    history.replaceState(null, '', withBase(next ?? '/'))
  }

  // While the sign-in form is up, fetch the workspace in the background so
  // the first screen after sign-in does not wait for it. The form renders
  // first; this never blocks it.
  $effect(() => {
    if (publicToken || initializing || authenticated || oauthRequestId) return
    const timer = setTimeout(() => {
      import('./lib/components/layout/Shell.svelte').catch(() => {})
    }, 1500)
    return () => clearTimeout(timer)
  })

  $effect(() => {
    if (publicToken || initializing || !authenticated) return
    untrack(() => {
      if (oauthRequestId) {
        if (!OAuthConsent) import('./pages/OAuthConsent.svelte').then((m) => (OAuthConsent = m.default), reportLoadError)
        return
      }
      restoreNext()
      initRouter()
      if (!Shell) import('./lib/components/layout/Shell.svelte').then((m) => (Shell = m.default), reportLoadError)
    })
  })
</script>

<Toast />

{#snippet splash()}
  <div class="flex flex-col items-center justify-center h-full gap-3">
    <img src={logo} alt="CH-UI" class="w-12 h-12 rounded-xl ring-1 ring-white/20" />
    {#if loadError}
      <p class="text-[13px] text-danger" role="alert">{loadError}</p>
      <button class="text-[13px] text-accent hover:underline" onclick={() => window.location.reload()}>Reload</button>
    {:else}
      <Spinner size="lg" />
      <p class="text-xs text-fg-3">Loading CH-UI workspace...</p>
    {/if}
  </div>
{/snippet}

{#if publicToken}
  {#if PublicDashboard}
    <PublicDashboard token={publicToken} />
  {:else}
    {@render splash()}
  {/if}
{:else if initializing}
  {@render splash()}
{:else if !authenticated}
  <Login />
{:else if oauthRequestId}
  {#if OAuthConsent}
    <OAuthConsent requestId={oauthRequestId} />
  {:else}
    {@render splash()}
  {/if}
{:else if Shell}
  <Shell />
{:else}
  {@render splash()}
{/if}
