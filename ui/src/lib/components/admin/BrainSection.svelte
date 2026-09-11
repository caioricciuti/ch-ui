<script lang="ts">
  import { onMount } from 'svelte'
  import type { BrainModelOption, BrainProviderAdmin, BrainSkill } from '../../types/brain'
  import {
    adminBulkUpdateBrainModels,
    adminCreateBrainProvider,
    adminCreateBrainSkill,
    adminDeleteBrainProvider,
    adminListBrainModels,
    adminListBrainProviders,
    adminListBrainSkills,
    adminSyncBrainProviderModels,
    adminUpdateBrainModel,
    adminUpdateBrainProvider,
    adminUpdateBrainSkill,
  } from '../../api/brain'
  import { success as toastSuccess, error as toastError } from '../../stores/toast.svelte'
  import Spinner from '../common/Spinner.svelte'
  import Sheet from '../common/Sheet.svelte'
  import HelpTip from '../common/HelpTip.svelte'
  import SectionHeader from '../common/SectionHeader.svelte'
  import FormField from '../common/FormField.svelte'
  import Badge from '../common/Badge.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import PageBody from '../common/PageBody.svelte'
  import ConfirmDialog from '../common/ConfirmDialog.svelte'
  import Button from '../common/Button.svelte'
  import Panel from '../common/Panel.svelte'
  import Stat from '../common/Stat.svelte'
  import Input from '../common/Input.svelte'
  import Select from '../common/Select.svelte'
  import Textarea from '../common/Textarea.svelte'
  import DataTable, { type DataColumn } from '../common/DataTable.svelte'
  import { Brain, RefreshCw, Plus, Trash2, RefreshCcw } from 'lucide-svelte'

  // AI providers, the models they expose and the system prompt every Brain
  // chat starts from. Self-contained: loads on mount, reloads after writes.

  let loading = $state(false)
  let providers = $state<BrainProviderAdmin[]>([])
  let models = $state<BrainModelOption[]>([])
  let skills = $state<BrainSkill[]>([])
  let modelProviderFilter = $state('')
  let modelSearch = $state('')
  let modelShowOnlyActive = $state(false)
  let providerSheetOpen = $state(false)
  let skillSheetOpen = $state(false)
  let deletingProvider = $state<BrainProviderAdmin | null>(null)

  const providerKindOptions = [
    { value: 'openai', label: 'OpenAI' },
    { value: 'openai_compatible', label: 'OpenAI compatible' },
    { value: 'ollama', label: 'Ollama (local)' },
  ]
  const providerKindDescriptions: Record<string, string> = {
    openai: 'The official OpenAI API: GPT-4o, o3 and friends.',
    openai_compatible: 'Any provider with an OpenAI-compatible API: Together, Groq, Azure, DeepSeek.',
    ollama: 'A local Ollama instance. No API key needed.',
  }
  const providerBaseUrls: Record<string, string> = {
    openai: 'https://api.openai.com/v1',
    openai_compatible: '',
    ollama: 'http://localhost:11434',
  }

  let providerForm = $state({
    name: '',
    kind: 'openai',
    baseUrl: 'https://api.openai.com/v1',
    apiKey: '',
    isActive: true,
    isDefault: false,
  })
  let providerCreating = $state(false)
  let providerError = $state('')
  let skillForm = $state({
    name: 'Default Brain Skill',
    content: '',
    isActive: true,
    isDefault: true,
  })

  onMount(() => {
    void loadAll()
  })

  async function loadAll() {
    loading = true
    try {
      const [p, m, s] = await Promise.all([adminListBrainProviders(), adminListBrainModels(), adminListBrainSkills()])
      providers = p
      models = m
      skills = s
      if (!modelProviderFilter && p.length > 0) {
        modelProviderFilter = p[0].id
      } else if (modelProviderFilter && !p.some((x) => x.id === modelProviderFilter)) {
        modelProviderFilter = p[0]?.id ?? ''
      }
      if (s.length > 0 && !skillForm.content) {
        const active = s.find((x) => x.is_active) ?? s[0]
        skillForm = { name: active.name, content: active.content, isActive: active.is_active, isDefault: active.is_default }
      }
    } catch (e: any) {
      toastError(e.message)
    } finally {
      loading = false
    }
  }

  // ── Providers ─────────────────────────────────────────────
  function resetProviderForm() {
    providerForm = { name: '', kind: 'openai', baseUrl: 'https://api.openai.com/v1', apiKey: '', isActive: true, isDefault: false }
    providerError = ''
    providerCreating = false
  }

  function closeProviderSheet() {
    providerSheetOpen = false
    resetProviderForm()
  }

  async function createProvider() {
    providerError = ''
    providerCreating = true
    try {
      await adminCreateBrainProvider(providerForm)
      toastSuccess('Provider created')
      closeProviderSheet()
      await loadAll()
    } catch (e: any) {
      providerError = e.message || 'Failed to create provider'
    } finally {
      providerCreating = false
    }
  }

  async function toggleProvider(provider: BrainProviderAdmin, key: 'is_active' | 'is_default', value: boolean) {
    try {
      await adminUpdateBrainProvider(provider.id, {
        isActive: key === 'is_active' ? value : provider.is_active,
        isDefault: key === 'is_default' ? value : provider.is_default,
      })
      toastSuccess('Provider updated')
      await loadAll()
    } catch (e: any) {
      toastError(e.message)
    }
  }

  async function syncProviderModels(provider: BrainProviderAdmin) {
    try {
      await adminSyncBrainProviderModels(provider.id)
      toastSuccess(`Synced models for ${provider.name}. Recommended model auto-selected.`)
      await loadAll()
    } catch (e: any) {
      toastError(e.message)
    }
  }

  async function confirmDeleteProvider() {
    if (!deletingProvider) return
    const provider = deletingProvider
    deletingProvider = null
    try {
      await adminDeleteBrainProvider(provider.id)
      toastSuccess('Provider deleted')
      await loadAll()
    } catch (e: any) {
      toastError(e.message)
    }
  }

  type ProviderRow = BrainProviderAdmin & Record<string, unknown>
  const providerColumns: DataColumn<ProviderRow>[] = [
    { key: 'name', label: 'Provider', width: '26%' },
    { key: 'kind', label: 'Kind', width: '150px' },
    { key: 'base_url', label: 'Base URL', mono: true, truncate: true, format: (v) => (v ? String(v) : 'Default endpoint') },
    { key: 'has_api_key', label: 'Key', width: '90px', sortable: false },
    { key: 'is_active', label: 'Active', width: '70px', align: 'right', sortable: false },
    { key: 'is_default', label: 'Default', width: '80px', align: 'right', sortable: false },
  ]
  const providerRows = $derived(providers as ProviderRow[])

  // ── Models ────────────────────────────────────────────────
  async function updateModel(model: BrainModelOption, key: 'is_active' | 'is_default', value: boolean) {
    try {
      await adminUpdateBrainModel(model.id, {
        displayName: model.display_name || model.name,
        isActive: key === 'is_active' ? value : model.is_active,
        isDefault: key === 'is_default' ? value : model.is_default,
      })
      toastSuccess('Model updated')
      await loadAll()
    } catch (e: any) {
      toastError(e.message)
    }
  }

  function modelsForProvider(providerId: string): BrainModelOption[] {
    const term = modelSearch.trim().toLowerCase()
    return models.filter((model) => {
      if (model.provider_id !== providerId) return false
      if (modelShowOnlyActive && !model.is_active) return false
      if (!term) return true
      return `${model.display_name || ''} ${model.name} ${model.provider_name}`.toLowerCase().includes(term)
    })
  }

  const providerFilterOptions = $derived([
    { value: '', label: 'All providers' },
    ...providers.map((p) => ({ value: p.id, label: p.name })),
  ])
  const visibleProviders = $derived(modelProviderFilter ? providers.filter((p) => p.id === modelProviderFilter) : providers)

  async function runModelBulkAction(action: 'deactivate_all' | 'activate_all' | 'activate_recommended') {
    if (!modelProviderFilter) {
      toastError('Pick a provider first')
      return
    }
    try {
      await adminBulkUpdateBrainModels({ providerId: modelProviderFilter, action })
      if (action === 'deactivate_all') toastSuccess('All models deactivated for the selected provider')
      if (action === 'activate_all') toastSuccess('All models activated for the selected provider')
      if (action === 'activate_recommended') toastSuccess('Recommended model activated and set as default')
      await loadAll()
    } catch (e: any) {
      toastError(e.message)
    }
  }

  type ModelRow = BrainModelOption & Record<string, unknown>
  const modelColumns: DataColumn<ModelRow>[] = [
    { key: 'name', label: 'Model', mono: true, format: (_v, row) => row.display_name || row.name },
    { key: 'is_active', label: 'Active', width: '80px', align: 'right', sortable: false },
    { key: 'is_default', label: 'Default', width: '80px', align: 'right', sortable: false },
  ]

  // ── System prompt ─────────────────────────────────────────
  async function saveSkill() {
    if (!skillForm.content.trim() || !skillForm.name.trim()) {
      toastError('Skill name and content are required')
      return
    }
    try {
      const payload = { name: skillForm.name, content: skillForm.content, isActive: skillForm.isActive, isDefault: skillForm.isDefault }
      const active = skills.find((s) => s.is_active)
      if (active) await adminUpdateBrainSkill(active.id, payload)
      else await adminCreateBrainSkill(payload)
      toastSuccess('System prompt saved')
      skillSheetOpen = false
      await loadAll()
    } catch (e: any) {
      toastError(e.message)
    }
  }

  function openSkillSheet() {
    const active = skills.find((s) => s.is_active) ?? skills[0]
    if (active) {
      skillForm = { name: active.name, content: active.content, isActive: active.is_active, isDefault: active.is_default }
    }
    skillSheetOpen = true
  }

  function truncate(s: string, max = 1500): string {
    return s.length > max ? s.slice(0, max) + '...' : s
  }
</script>

{#if loading && providers.length === 0}
  <div class="flex h-full items-center justify-center py-12"><Spinner /></div>
{:else}
  <PageBody width="md">
    <div class="space-y-8">
      <SectionHeader level="page" title="Brain" description="AI providers, the models they expose and the system prompt every chat starts from.">
        {#snippet actions()}
          <Button icon size="sm" variant="ghost" aria-label="Refresh" title="Refresh" onclick={() => loadAll()}>
            <RefreshCw size={14} />
          </Button>
        {/snippet}
      </SectionHeader>

      <div class="grid grid-cols-2 gap-3 md:grid-cols-4">
        <Stat label="Providers" value={providers.length} hint={`${providers.filter((p) => p.is_active).length} active`} />
        <Stat label="Models" value={models.length} hint={`${models.filter((m) => m.is_active).length} active`} />
        <Stat size="md" label="Default provider" value={providers.find((p) => p.is_default)?.name || 'None'} tone={providers.some((p) => p.is_default) ? 'default' : 'warning'} />
        <Stat size="md" label="System prompt" value={skillForm.isActive ? 'Active' : 'Inactive'} tone={skillForm.content && skillForm.isActive ? 'success' : 'warning'} />
      </div>

      <section>
        <SectionHeader title="Providers" description="Where Brain sends its requests. One provider is the default.">
          {#snippet actions()}
            <Button size="sm" onclick={() => (providerSheetOpen = true)}>
              <Plus size={14} /> Add provider
            </Button>
          {/snippet}
        </SectionHeader>
        {#if providers.length === 0}
          <Panel padding="none">
            <EmptyState
              size="compact"
              icon={Brain}
              title="No providers yet"
              description="Add an OpenAI-compatible provider or a local Ollama to enable Brain."
              primary={{ label: 'Add provider', onclick: () => (providerSheetOpen = true) }}
            />
          </Panel>
        {:else}
          <DataTable columns={providerColumns} rows={providerRows} rowKey={(r) => r.id} sort={{ key: 'name', dir: 'asc' }} emptyTitle="No providers">
            {#snippet cell(row, col, value)}
              {#if col.key === 'name'}
                <span class="flex items-center gap-2.5">
                  <span class="flex h-7 w-7 shrink-0 items-center justify-center rounded-md bg-surface-2 text-xs font-bold text-fg-2">{row.name.charAt(0).toUpperCase()}</span>
                  <span class="truncate font-medium text-fg">{row.name}</span>
                  {#if row.is_default}<Badge tone="brand">Default</Badge>{/if}
                </span>
              {:else if col.key === 'kind'}
                <Badge>{row.kind}</Badge>
              {:else if col.key === 'has_api_key'}
                {#if row.kind === 'ollama'}
                  <Badge>Not needed</Badge>
                {:else if row.has_api_key}
                  <Badge tone="success">Key set</Badge>
                {:else}
                  <Badge tone="danger">No key</Badge>
                {/if}
              {:else if col.key === 'is_active'}
                <input
                  type="checkbox"
                  class="ds-checkbox"
                  aria-label={`${row.name} active`}
                  checked={row.is_active}
                  onclick={(e) => e.stopPropagation()}
                  onchange={(e) => toggleProvider(row, 'is_active', (e.target as HTMLInputElement).checked)}
                />
              {:else if col.key === 'is_default'}
                <input
                  type="radio"
                  class="ds-radio"
                  name="default-brain-provider"
                  aria-label={`${row.name} default`}
                  checked={row.is_default}
                  onclick={(e) => e.stopPropagation()}
                  onchange={() => toggleProvider(row, 'is_default', true)}
                />
              {:else}
                {value}
              {/if}
            {/snippet}
            {#snippet actions(row)}
              <Button size="xs" variant="outline" onclick={() => syncProviderModels(row)}>
                <RefreshCcw size={12} /> Sync models
              </Button>
              <Button icon size="xs" variant="ghost" class="text-danger hover:bg-danger-soft hover:text-danger" aria-label="Delete provider" title="Delete provider" onclick={() => (deletingProvider = row)}>
                <Trash2 size={13} />
              </Button>
            {/snippet}
          </DataTable>
        {/if}
      </section>

      <section>
        <SectionHeader title="Models" description="Activate the models people can pick in Brain chat. One default per provider.">
          {#snippet actions()}
            <Button size="xs" variant="outline" onclick={() => runModelBulkAction('activate_recommended')}>Activate recommended</Button>
            <Button size="xs" variant="outline" onclick={() => runModelBulkAction('activate_all')}>Activate all</Button>
            <Button size="xs" variant="outline" onclick={() => runModelBulkAction('deactivate_all')}>Deactivate all</Button>
          {/snippet}
        </SectionHeader>

        <div class="mb-3 flex items-center gap-2">
          <Select size="sm" class="w-44" options={providerFilterOptions} bind:value={modelProviderFilter} />
          <Input size="sm" class="flex-1" type="search" placeholder="Search models" bind:value={modelSearch} />
          <label class="ds-checkbox-label whitespace-nowrap text-xs">
            <input type="checkbox" class="ds-checkbox ds-checkbox-sm" bind:checked={modelShowOnlyActive} />
            Active only
          </label>
        </div>

        {#if models.length === 0}
          <Panel padding="none">
            <EmptyState size="compact" title="No models synced yet" description="Add a provider and click Sync models." />
          </Panel>
        {:else}
          <div class="space-y-3">
            {#each visibleProviders as provider (provider.id)}
              {@const providerModels = modelsForProvider(provider.id) as ModelRow[]}
              <div>
                <div class="mb-1.5 flex items-center gap-2 px-1 text-[13px]">
                  <span class="font-medium text-fg">{provider.name}</span>
                  <Badge>{provider.kind}</Badge>
                  <span class="ml-auto text-xs text-fg-3">
                    {providerModels.filter((m) => m.is_active).length} active of {providerModels.length}
                  </span>
                </div>
                <DataTable
                  columns={modelColumns}
                  rows={providerModels}
                  rowKey={(r) => r.id}
                  sort={{ key: 'name', dir: 'asc' }}
                  emptyTitle="No models match"
                >
                  {#snippet cell(row, col, value)}
                    {#if col.key === 'is_active'}
                      <input
                        type="checkbox"
                        class="ds-checkbox"
                        aria-label={`${row.name} active`}
                        checked={row.is_active}
                        onchange={(e) => updateModel(row, 'is_active', (e.target as HTMLInputElement).checked)}
                      />
                    {:else if col.key === 'is_default'}
                      <input
                        type="radio"
                        class="ds-radio"
                        name={'default-model-' + row.provider_id}
                        aria-label={`${row.name} default`}
                        checked={row.is_default}
                        onchange={() => updateModel(row, 'is_default', true)}
                      />
                    {:else}
                      {value}
                    {/if}
                  {/snippet}
                </DataTable>
              </div>
            {/each}
          </div>
        {/if}
      </section>

      <section>
        <SectionHeader title="System prompt" description="Steers SQL safety, artifacts and tool behavior for every chat.">
          {#snippet actions()}
            {#if skillForm.isActive}
              <Badge tone="success">Active</Badge>
            {:else}
              <Badge>Inactive</Badge>
            {/if}
            <Button size="sm" variant="outline" onclick={openSkillSheet}>Edit prompt</Button>
          {/snippet}
        </SectionHeader>
        <Panel padding="none" title={skillForm.name || 'Default skill'}>
          <pre class="max-h-56 overflow-auto whitespace-pre-wrap px-4 pb-4 pt-3 font-mono text-xs leading-relaxed text-fg-2">{truncate(skillForm.content || 'No system prompt configured.')}</pre>
        </Panel>
      </section>
    </div>
  </PageBody>
{/if}

<Sheet open={providerSheetOpen} title="Add provider" description="Brain talks to this API with the key you store here." size="lg" onclose={closeProviderSheet}>
  <form
    id="brain-provider-form"
    class="space-y-5"
    onsubmit={(e) => {
      e.preventDefault()
      void createProvider()
    }}
  >
    <div>
      <span class="mb-1.5 block text-xs font-medium text-fg-2">Provider type</span>
      <div class="grid grid-cols-1 gap-2 md:grid-cols-3">
        {#each providerKindOptions as opt (opt.value)}
          <button
            type="button"
            class="rounded-md border p-3 text-left transition-colors {providerForm.kind === opt.value ? 'border-accent bg-accent-soft' : 'border-edge-subtle hover:border-edge'}"
            onclick={() => {
              providerForm = { ...providerForm, kind: opt.value, baseUrl: providerBaseUrls[opt.value] ?? '' }
              providerError = ''
            }}
          >
            <span class="text-[13px] font-medium text-fg">{opt.label}</span>
            <p class="mt-0.5 text-[11px] leading-snug text-fg-3">{providerKindDescriptions[opt.value]}</p>
          </button>
        {/each}
      </div>
    </div>

    <div class="space-y-3">
      <FormField label="Display name" for="provider-name" required controlWidth="full">
        <Input id="provider-name" placeholder={providerForm.kind === 'ollama' ? 'Local Ollama' : providerForm.kind === 'openai' ? 'OpenAI' : 'My provider'} bind:value={providerForm.name} required />
      </FormField>
      <FormField
        label="Base URL"
        for="provider-base-url"
        controlWidth="full"
        hint={providerForm.kind !== 'openai_compatible'
          ? `Pre-filled for ${providerKindOptions.find((o) => o.value === providerForm.kind)?.label}. Change it only for a proxy.`
          : 'The OpenAI-compatible /v1 endpoint of your provider.'}
      >
        <Input id="provider-base-url" mono placeholder={providerForm.kind === 'openai_compatible' ? 'https://your-gateway.example.com/v1' : ''} bind:value={providerForm.baseUrl} />
      </FormField>
      {#if providerForm.kind !== 'ollama'}
        <FormField label="API key" for="provider-api-key" controlWidth="full" hint="Stored encrypted. Never shown again after saving.">
          <Input id="provider-api-key" mono type="password" placeholder="sk-..." autocomplete="new-password" bind:value={providerForm.apiKey} />
        </FormField>
      {:else}
        <Panel variant="muted" padding="sm">
          <p class="text-xs text-fg-3">Ollama runs locally and needs no API key. Make sure it is listening on the URL above.</p>
        </Panel>
      {/if}
    </div>

    <div class="flex items-center gap-5">
      <label class="ds-checkbox-label text-xs">
        <input type="checkbox" class="ds-checkbox" bind:checked={providerForm.isActive} />
        Active
      </label>
      <label class="ds-checkbox-label text-xs">
        <input type="checkbox" class="ds-checkbox" bind:checked={providerForm.isDefault} />
        Set as default
      </label>
    </div>

    {#if providerError}
      <div class="rounded-md bg-danger-soft p-3">
        <p class="text-xs font-medium text-danger">{providerError}</p>
      </div>
    {/if}
  </form>
  {#snippet footer()}
    <Button size="sm" variant="ghost" onclick={closeProviderSheet}>Cancel</Button>
    <Button size="sm" type="submit" disabled={!providerForm.name.trim()} loading={providerCreating} onclick={() => void createProvider()}>Create provider</Button>
  {/snippet}
</Sheet>

<Sheet open={skillSheetOpen} title="System prompt" description="The instructions every Brain chat starts from." size="xl" onclose={() => (skillSheetOpen = false)}>
  <form
    class="space-y-4"
    onsubmit={(e) => {
      e.preventDefault()
      void saveSkill()
    }}
  >
    <div class="flex items-center gap-2">
      <p class="text-xs text-fg-3">Keep it practical: SQL guardrails, artifact expectations, when to ask before acting.</p>
      <HelpTip text="This text is prepended to every conversation. Shorter prompts leave more room for schema context." />
    </div>
    <FormField label="Name" for="skill-name" required controlWidth="full">
      <Input id="skill-name" bind:value={skillForm.name} required />
    </FormField>
    <FormField label="Content" for="skill-content" controlWidth="full" hint="Markdown is fine. Use ```sql fences for example queries.">
      <Textarea id="skill-content" mono class="min-h-[56vh] resize-y" bind:value={skillForm.content} placeholder="You are Brain, a senior ClickHouse copilot..." />
    </FormField>
    <div class="flex flex-wrap items-center gap-4">
      <label class="ds-checkbox-label text-xs">
        <input type="checkbox" class="ds-checkbox" bind:checked={skillForm.isActive} />
        Active
      </label>
      <label class="ds-checkbox-label text-xs">
        <input type="checkbox" class="ds-checkbox" bind:checked={skillForm.isDefault} />
        Default
      </label>
    </div>
  </form>
  {#snippet footer()}
    <Button size="sm" variant="ghost" onclick={() => (skillSheetOpen = false)}>Cancel</Button>
    <Button size="sm" disabled={!skillForm.name.trim() || !skillForm.content.trim()} onclick={() => void saveSkill()}>Save prompt</Button>
  {/snippet}
</Sheet>

<ConfirmDialog
  open={deletingProvider !== null}
  title="Delete provider?"
  description={deletingProvider ? `Delete "${deletingProvider.name}" and all its models? This cannot be undone.` : ''}
  confirmLabel="Delete provider"
  destructive
  onconfirm={confirmDeleteProvider}
  oncancel={() => (deletingProvider = null)}
/>
