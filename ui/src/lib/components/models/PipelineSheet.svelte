<script lang="ts">
  import type { ModelPipeline, PipelineInput } from '../../types/models'
  import * as api from '../../api/models'
  import { formatDate } from '../../utils/format'
  import { success as toastSuccess } from '../../stores/toast.svelte'
  import Sheet from '../common/Sheet.svelte'
  import FormField from '../common/FormField.svelte'
  import Input from '../common/Input.svelte'
  import Button from '../common/Button.svelte'
  import Switch from '../common/Switch.svelte'

  interface Props {
    open: boolean
    /** The pipeline to edit; null creates one. */
    pipeline: ModelPipeline | null
    onclose: () => void
    onsaved: () => Promise<void> | void
  }

  let { open, pipeline, onclose, onsaved }: Props = $props()

  let name = $state('')
  let selector = $state('')
  let cron = $state('')
  let timezone = $state('UTC')
  let enabled = $state(true)
  let formError = $state('')
  let saving = $state(false)

  let nextRuns = $state<string[]>([])
  let previewError = $state('')
  let previewing = $state(false)

  const localZone = Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'

  const CRON_PRESETS = [
    { label: 'Every hour', cron: '0 * * * *' },
    { label: 'Every 6h', cron: '0 */6 * * *' },
    { label: 'Daily midnight', cron: '0 0 * * *' },
    { label: 'Weekly Mon 2am', cron: '0 2 * * 1' },
  ]

  // Each time the sheet opens, start from the pipeline (or a blank form).
  $effect(() => {
    if (!open) return
    name = pipeline?.name ?? ''
    selector = pipeline?.selector ?? '*'
    cron = pipeline?.cron ?? ''
    timezone = pipeline?.timezone || 'UTC'
    enabled = pipeline ? pipeline.enabled || !pipeline.cron : true
    formError = ''
  })

  // Live "next runs", debounced while typing.
  $effect(() => {
    const c = cron.trim()
    const tz = timezone.trim() || 'UTC'
    if (!open || !c) {
      nextRuns = []
      previewError = ''
      return
    }
    let cancelled = false
    const timer = setTimeout(async () => {
      previewing = true
      try {
        const res = await api.previewSchedule(c, tz)
        if (cancelled) return
        nextRuns = res.next_runs ?? []
        previewError = ''
      } catch (e: unknown) {
        if (cancelled) return
        nextRuns = []
        previewError = (e instanceof Error ? e.message : '') || 'Invalid schedule'
      } finally {
        if (!cancelled) previewing = false
      }
    }, 350)
    return () => {
      cancelled = true
      clearTimeout(timer)
    }
  })

  function utcText(iso: string): string {
    const d = new Date(iso)
    return Number.isNaN(d.getTime()) ? iso : `${d.toISOString().slice(0, 16).replace('T', ' ')} UTC`
  }

  async function submit() {
    if (!name.trim()) {
      formError = 'Name is required.'
      return
    }
    saving = true
    formError = ''
    const input: PipelineInput = {
      name: name.trim(),
      selector: selector.trim(),
      cron: cron.trim(),
      timezone: timezone.trim() || 'UTC',
    }
    if (input.cron) input.enabled = enabled
    try {
      if (pipeline) {
        await api.updatePipeline(pipeline.id, input)
        toastSuccess('Pipeline saved')
      } else {
        await api.createPipeline(input)
        toastSuccess('Pipeline created')
      }
      onclose()
      await onsaved()
    } catch (e: unknown) {
      formError = (e instanceof Error ? e.message : '') || 'Failed to save pipeline'
    } finally {
      saving = false
    }
  }
</script>

<Sheet
  {open}
  title={pipeline ? 'Edit pipeline' : 'New pipeline'}
  description="A named group of models picked by a selector, run in dependency order, optionally on a schedule."
  size="sm"
  {onclose}
>
  <form id="pipeline-form" class="space-y-4" onsubmit={(e) => { e.preventDefault(); submit() }}>
    <FormField label="Name" for="pipeline-name" required>
      <Input id="pipeline-name" bind:value={name} placeholder="nightly_marts" maxlength={100} />
    </FormField>

    <FormField
      label="Selector"
      for="pipeline-selector"
      hint="name, +name (with upstream), name+ (with downstream), * ; comma-separated"
    >
      <Input id="pipeline-selector" bind:value={selector} mono spellcheck={false} placeholder="+orders_daily" />
    </FormField>
    {#if pipeline?.selector_error && selector.trim() === pipeline.selector}
      <p class="text-xs text-danger" role="alert">
        {pipeline.selector_error}. Scheduled runs fail until the selector is fixed.
      </p>
    {/if}

    <FormField label="Cron (5-field)" for="pipeline-cron" hint="Leave empty to run only by hand.">
      <Input id="pipeline-cron" bind:value={cron} mono spellcheck={false} placeholder="0 */6 * * *" />
    </FormField>
    <div class="flex flex-wrap gap-1.5">
      {#each CRON_PRESETS as preset (preset.cron)}
        <Button size="xs" variant={cron === preset.cron ? 'secondary' : 'outline'} onclick={() => { cron = preset.cron }}>
          {preset.label}
        </Button>
      {/each}
    </div>

    <FormField label="Timezone" for="pipeline-tz" hint="IANA name the cron is read in, e.g. Europe/Madrid.">
      <div class="flex items-center gap-2">
        <Input id="pipeline-tz" bind:value={timezone} mono spellcheck={false} placeholder="UTC" />
        {#if localZone !== timezone}
          <Button size="sm" variant="outline" onclick={() => { timezone = localZone }} title="Use this browser's timezone">
            {localZone}
          </Button>
        {/if}
        {#if timezone !== 'UTC'}
          <Button size="sm" variant="outline" onclick={() => { timezone = 'UTC' }}>UTC</Button>
        {/if}
      </div>
    </FormField>

    {#if cron.trim()}
      <div class="flex items-center justify-between gap-4">
        <div>
          <div class="text-xs font-medium text-fg-2">Enabled</div>
          <p class="mt-0.5 text-xs text-fg-3">Paused pipelines keep their schedule but do not run.</p>
        </div>
        <Switch checked={enabled} label={enabled ? 'Pause schedule' : 'Enable schedule'} onchange={(next) => { enabled = next }} />
      </div>

      <div class="space-y-1 rounded-md bg-surface-2 p-3 text-xs">
        <div class="font-medium text-fg-2">Next 3 runs</div>
        {#if previewError}
          <p class="text-danger" role="alert">{previewError}</p>
        {:else if nextRuns.length === 0}
          <p class="text-fg-3">{previewing ? 'Checking...' : 'No upcoming runs'}</p>
        {:else}
          {#each nextRuns as run (run)}
            <div class="flex justify-between gap-3">
              <span class="tabular-nums text-fg-2">{formatDate(run)}</span>
              <span class="font-mono text-fg-3">{utcText(run)}</span>
            </div>
          {/each}
        {/if}
      </div>
    {/if}

    {#if formError}
      <p class="text-xs text-danger" role="alert">{formError}</p>
    {/if}
  </form>

  {#snippet footer()}
    <Button size="sm" variant="outline" onclick={onclose}>Cancel</Button>
    <Button size="sm" type="submit" form="pipeline-form" loading={saving}>
      {pipeline ? 'Save' : 'Create pipeline'}
    </Button>
  {/snippet}
</Sheet>
