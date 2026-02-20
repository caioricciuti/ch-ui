<script lang="ts">
  import { onMount } from 'svelte'
  import { initSession, isAuthenticated, isLoading } from './lib/stores/session.svelte'
  import { initRouter } from './lib/stores/router.svelte'

  import Shell from './lib/components/layout/Shell.svelte'
  import Toast from './lib/components/common/Toast.svelte'
  import Spinner from './lib/components/common/Spinner.svelte'
  import Login from './pages/Login.svelte'
  import logo from './assets/logo.png'

  onMount(async () => {
    await initSession()
    initRouter()
  })

  const authenticated = $derived(isAuthenticated())
  const loading = $derived(isLoading())
</script>

<Toast />

{#if loading}
  <div class="flex flex-col items-center justify-center h-full gap-3">
    <img src={logo} alt="CH-UI" class="w-12 h-12 rounded-xl ring-1 ring-white/20" />
    <Spinner size="lg" />
    <p class="text-xs text-gray-500 dark:text-gray-400">Loading CH-UI workspace...</p>
  </div>
{:else if !authenticated}
  <Login />
{:else}
  <Shell />
{/if}
