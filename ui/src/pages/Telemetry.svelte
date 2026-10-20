<script lang="ts">
  import { errorText } from '../lib/utils/errors'
  import { onMount } from 'svelte'
  import { takePageIntent } from '../lib/stores/page-intent.svelte'
  import PageHeader from '../lib/components/common/PageHeader.svelte'
  import Spinner from '../lib/components/common/Spinner.svelte'
  import EmptyState from '../lib/components/common/EmptyState.svelte'
  import ProRequired from '../lib/components/common/ProRequired.svelte'
  import LogsSection from '../lib/components/telemetry/LogsSection.svelte'
  import SourcesSection from '../lib/components/telemetry/SourcesSection.svelte'
  import TracesSection from '../lib/components/telemetry/TracesSection.svelte'
  import MetricsSection from '../lib/components/telemetry/MetricsSection.svelte'
  import ServiceMapSection from '../lib/components/telemetry/ServiceMapSection.svelte'
  import MonitorsSection from '../lib/components/telemetry/MonitorsSection.svelte'
  import SendDataSheet from '../lib/components/ingest/SendDataSheet.svelte'
  import Button from '../lib/components/common/Button.svelte'
  import { listSources } from '../lib/api/telemetry'
  import type { TelemetrySource } from '../lib/types/telemetry'
  import { getSection, setSection } from '../lib/stores/nav.svelte'
  import { hasProReadAccess } from '../lib/stores/license.svelte'
  import { PAGE_SECTIONS, docsURL } from '../lib/routes'
  import { ServerCrash } from 'lucide-svelte'

  const sections = PAGE_SECTIONS.telemetry ?? []
  const active = $derived.by(() => {
    const s = getSection()
    return sections.some((x) => x.id === s) ? (s as string) : 'logs'
  })
  const activeLabel = $derived(sections.find((s) => s.id === active)?.label ?? 'Logs')
  const gated = $derived(!!sections.find((s) => s.id === active)?.pro && !hasProReadAccess())

  let sources = $state<TelemetrySource[]>([])
  let loading = $state(true)
  let error = $state<string | null>(null)
  let sendData = $state(false)

  async function load() {
    error = null
    try {
      sources = await listSources()
    } catch (e: unknown) {
      error = errorText(e)
    } finally {
      loading = false
    }
  }

  // The sheet's setup registers a source; pick it up without a page refresh.
  function closeSendData() {
    sendData = false
    void load()
  }

  onMount(() => {
    if (!getSection()) setSection('logs')
    void load()
  })

  // "Send data to Telemetry" from the command palette.
  $effect(() => {
    if (takePageIntent('send-data')) sendData = true
  })
</script>

<div class="flex h-full min-h-0 flex-col">
  <PageHeader title="Telemetry" subtitle={activeLabel} docs={docsURL('telemetry', active)}>
    {#snippet actions()}
      <Button size="sm" variant="outline" onclick={() => { sendData = true }}>Send data here</Button>
    {/snippet}
  </PageHeader>

  {#if loading}
    <div class="flex flex-1 items-center justify-center gap-2 text-[13px] text-fg-3"><Spinner size="sm" /> Loading sources…</div>
  {:else if error}
    <EmptyState icon={ServerCrash} title="Could not load telemetry sources" description={error} primary={{ label: 'Retry', onclick: () => { loading = true; void load() } }} />
  {:else}
    {#key active}
      {#if gated}
        <ProRequired feature={`Telemetry ${activeLabel}`} />
      {:else if active === 'logs'}
        <LogsSection {sources} onsenddata={() => { sendData = true }} />
      {:else if active === 'traces'}
        <TracesSection {sources} onsenddata={() => { sendData = true }} />
      {:else if active === 'metrics'}
        <MetricsSection {sources} />
      {:else if active === 'service-map'}
        <ServiceMapSection {sources} />
      {:else if active === 'monitors'}
        <MonitorsSection {sources} />
      {:else}
        <SourcesSection {sources} {loading} onreload={load} />
      {/if}
    {/key}
  {/if}
</div>

<SendDataSheet open={sendData} signal={active === 'traces' ? 'traces' : 'logs'} onclose={closeSendData} />
