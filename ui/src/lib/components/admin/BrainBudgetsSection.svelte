<script lang="ts">
  import { errorText } from '../../utils/errors'
  import { onMount } from 'svelte'
  import { Gauge, Lock, Pencil, Plus, RefreshCw, Trash2 } from 'lucide-svelte'
  import type { BrainBudgetRuleView } from '../../types/brain'
  import { apiGet } from '../../api/client'
  import { adminCreateBrainBudget, adminDeleteBrainBudget, adminListBrainBudgets, adminUpdateBrainBudget } from '../../api/brain'
  import { goTo } from '../../stores/router.svelte'
  import { success as toastSuccess, errorFrom as toastErrorFrom } from '../../stores/toast.svelte'
  import { formatCost, formatTokens } from '../brain/brain-usage'
  import { ROLES, draftFromRule, emptyRuleDraft, limitsText, parseRuleDraft, subjectLabel, type BudgetRuleDraft } from '../brain/brain-budget'
  import PageBody from '../common/PageBody.svelte'
  import SectionHeader from '../common/SectionHeader.svelte'
  import Panel from '../common/Panel.svelte'
  import Stat from '../common/Stat.svelte'
  import Badge from '../common/Badge.svelte'
  import Button from '../common/Button.svelte'
  import Spinner from '../common/Spinner.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import Sheet from '../common/Sheet.svelte'
  import FormField from '../common/FormField.svelte'
  import Select from '../common/Select.svelte'
  import Combobox from '../common/Combobox.svelte'
  import Input from '../common/Input.svelte'
  import ConfirmDialog from '../common/ConfirmDialog.svelte'
  import DataTable, { type DataColumn } from '../common/DataTable.svelte'

  // Admin > Brain budgets: token and dollar limits per person, per role and
  // for everyone, per UTC day or month. Each rule caps every person it
  // applies to on their own; the most specific rule wins.

  type AppUser = { username: string; display_name: string; user_role: string; via_sso: boolean }

  let loading = $state(true)
  let rules = $state<BrainBudgetRuleView[]>([])
  let pro = $state(false)
  let users = $state<AppUser[]>([])

  let sheetOpen = $state(false)
  let editing = $state<BrainBudgetRuleView | null>(null)
  let draft = $state<BudgetRuleDraft>(emptyRuleDraft(false))
  let formError = $state('')
  let saving = $state(false)
  let deleting = $state<BrainBudgetRuleView | null>(null)

  onMount(() => {
    void load()
    void loadUsers()
  })

  async function load() {
    loading = true
    try {
      const res = await adminListBrainBudgets()
      rules = res.budgets
      pro = res.pro
    } catch (e) {
      toastErrorFrom(e)
    } finally {
      loading = false
    }
  }

  async function loadUsers() {
    try {
      const res = await apiGet<{ users?: AppUser[] } | AppUser[]>('/api/admin/users')
      users = Array.isArray(res) ? res : (res.users ?? [])
    } catch {
      // The person picker falls back to the people already in the rules.
      users = []
    }
  }

  function openCreate() {
    editing = null
    draft = emptyRuleDraft(pro)
    formError = ''
    sheetOpen = true
  }

  function openEdit(rule: BrainBudgetRuleView) {
    editing = rule
    draft = draftFromRule(rule)
    formError = ''
    sheetOpen = true
  }

  async function save() {
    formError = ''
    const parsed = parseRuleDraft(draft, pro)
    if (!parsed.ok) {
      formError = parsed.error
      return
    }
    saving = true
    try {
      if (editing) await adminUpdateBrainBudget(editing.id, parsed.input)
      else await adminCreateBrainBudget(parsed.input)
      toastSuccess(editing ? 'Budget saved' : 'Budget added')
      sheetOpen = false
      await load()
    } catch (e) {
      formError = errorText(e)
    } finally {
      saving = false
    }
  }

  async function confirmDelete() {
    if (!deleting) return
    const rule = deleting
    deleting = null
    try {
      await adminDeleteBrainBudget(rule.id)
      toastSuccess('Budget deleted')
      await load()
    } catch (e) {
      toastErrorFrom(e)
    }
  }

  const scopeOptions = $derived([
    { value: 'global', label: 'Everyone (global)' },
    { value: 'role', label: 'A role', disabled: !pro },
    { value: 'user', label: 'A person', disabled: !pro },
  ])
  const periodOptions = [
    { value: 'day', label: 'Per UTC day' },
    { value: 'month', label: 'Per UTC month' },
  ]
  const roleOptions = ROLES.map((r) => ({ value: r, label: r }))
  const personOptions = $derived.by(() => {
    const seen = new Set<string>()
    const out: { value: string; label: string; hint?: string }[] = []
    for (const u of users) {
      if (seen.has(u.username)) continue
      seen.add(u.username)
      out.push({ value: u.username, label: u.display_name || subjectLabel('user', u.username), hint: [u.via_sso ? 'SSO' : '', u.user_role].filter(Boolean).join(', ') })
    }
    for (const r of rules) {
      if (r.scope === 'user' && !seen.has(r.subject)) {
        seen.add(r.subject)
        out.push({ value: r.subject, label: subjectLabel('user', r.subject) })
      }
    }
    return out
  })

  const scopeLabel: Record<string, string> = { global: 'Global', role: 'Role', user: 'Person' }
  const scopeOrder: Record<string, number> = { global: 0, role: 1, user: 2 }

  type Row = BrainBudgetRuleView & Record<string, unknown>
  const rows = $derived(rules as Row[])
  const columns: DataColumn<Row>[] = [
    { key: 'scope', label: 'Scope', width: '96px', sortValue: (r) => scopeOrder[r.scope] ?? 9 },
    { key: 'subject', label: 'Applies to', truncate: true, format: (_v, r) => subjectLabel(r.scope, r.subject) },
    { key: 'period', label: 'Period', width: '80px', format: (v) => (v === 'month' ? 'Month' : 'Day') },
    { key: 'token_limit', label: 'Tokens', align: 'right', width: '110px', format: (v) => (typeof v === 'number' ? v.toLocaleString('en-US') : 'No limit'), sortValue: (r) => r.token_limit ?? -1 },
    { key: 'usd_limit', label: 'Dollars', align: 'right', width: '96px', format: (v) => (typeof v === 'number' ? `$${v.toFixed(2)}` : 'No limit'), sortValue: (r) => r.usd_limit ?? -1 },
    { key: 'usage', label: 'Use this period', width: '34%', sortValue: (r) => r.usage.top_percent },
  ]

  const atLimit = $derived(rules.reduce((n, r) => n + r.usage.over_limit, 0))
  const inactive = $derived(rules.filter((r) => r.inactive).length)
</script>

{#if loading && rules.length === 0}
  <div class="flex h-full items-center justify-center py-12"><Spinner /></div>
{:else}
  <PageBody width="md">
    <div class="space-y-8">
      <SectionHeader
        title="Brain budgets"
        description="Limits on Brain chat and Ask AI per person, per role or for everyone. Each rule caps every person it applies to; a person's own rule wins over their role's, which wins over the global one. Days and months are UTC."
      >
        {#snippet actions()}
          <Button icon size="sm" variant="ghost" aria-label="Refresh" title="Refresh" onclick={() => void load()}>
            <RefreshCw size={14} />
          </Button>
          <Button size="sm" onclick={openCreate}><Plus size={14} /> Add budget</Button>
        {/snippet}
      </SectionHeader>

      {#if !pro}
        <Panel variant="muted">
          <div class="flex items-start gap-3">
            <Lock size={16} class="mt-0.5 shrink-0 text-accent" />
            <div class="min-w-0 flex-1">
              <div class="flex items-center gap-2">
                <span class="text-[13px] font-medium text-fg">Role, person and dollar budgets need a Pro license</span>
                <Badge tone="brand">Pro</Badge>
              </div>
              <p class="mt-1 text-[13px] leading-relaxed text-fg-3">
                Without it, only global token limits apply. Role and person rules stay listed but are not enforced, and dollar limits are ignored.
              </p>
            </div>
            <Button size="sm" variant="outline" onclick={() => goTo('settings', 'License')}>Manage license</Button>
          </div>
        </Panel>
      {/if}

      <div class="grid grid-cols-2 gap-3">
        <Stat label="Rules" value={rules.length} hint={inactive > 0 ? `${inactive} not enforced` : 'All enforced'} tone={inactive > 0 ? 'warning' : 'default'} />
        <Stat label="People at a limit" value={atLimit} hint="In the current day or month" tone={atLimit > 0 ? 'danger' : 'default'} />
      </div>

      <section>
        <SectionHeader title="Rules" description="The usage column shows the person closest to the limit among those the rule governs." />
        {#if rules.length === 0}
          <Panel padding="none">
            <EmptyState
              size="compact"
              icon={Gauge}
              title="No budgets"
              description="Brain use is unlimited. Add a global daily token budget to start, then role or person rules for exceptions."
              primary={{ label: 'Add budget', onclick: openCreate }}
            />
          </Panel>
        {:else}
          <DataTable {columns} {rows} rowKey={(r) => r.id} sort={{ key: 'scope', dir: 'asc' }} onrowclick={(r) => openEdit(r)} emptyTitle="No budgets">
            {#snippet cell(row, col, value)}
              {#if col.key === 'scope'}
                <Badge tone={row.scope === 'global' ? 'neutral' : 'info'}>{scopeLabel[row.scope]}</Badge>
              {:else if col.key === 'subject'}
                <span class="flex min-w-0 items-center gap-2">
                  <span class="truncate" title={row.subject}>{value}</span>
                  {#if row.inactive}<Badge tone="warning" title="Needs a Pro license to be enforced">Not enforced</Badge>{/if}
                </span>
              {:else if col.key === 'usd_limit' && row.usd_limit !== null && row.ignored_usd}
                <span class="text-fg-4 line-through" title="Dollar limits need a Pro license">{value}</span>
              {:else if col.key === 'usd_limit' && row.block_unpriced}
                <span title="Unpriced models are blocked under this rule">{value} <Badge>Priced only</Badge></span>
              {:else if col.key === 'usage'}
                {#if row.usage.people === 0}
                  <span class="text-fg-4">No use yet</span>
                {:else}
                  <span class="flex min-w-0 items-center gap-2">
                    <span class="truncate tabular-nums" title={`${subjectLabel('user', row.usage.top_subject ?? '')}: ${formatTokens(row.usage.top_tokens)} tokens, ${formatCost(row.usage.top_usd)}`}>
                      {row.token_limit === null && row.usd_limit === null ? 'Exempt' : `${Math.round(row.usage.top_percent)}%`}
                      <span class="text-fg-3">top of {row.usage.people} {row.usage.people === 1 ? 'person' : 'people'}</span>
                    </span>
                    {#if row.usage.over_limit > 0}<Badge tone="danger">{row.usage.over_limit} at limit</Badge>{/if}
                  </span>
                {/if}
              {:else}
                {value}
              {/if}
            {/snippet}
            {#snippet actions(row)}
              <Button icon size="xs" variant="ghost" aria-label="Edit budget" title="Edit budget" onclick={(e: MouseEvent) => { e.stopPropagation(); openEdit(row) }}>
                <Pencil size={13} />
              </Button>
              <Button icon size="xs" variant="ghost" class="text-danger hover:bg-danger-soft hover:text-danger" aria-label="Delete budget" title="Delete budget" onclick={(e: MouseEvent) => { e.stopPropagation(); deleting = row }}>
                <Trash2 size={13} />
              </Button>
            {/snippet}
          </DataTable>
        {/if}
      </section>
    </div>
  </PageBody>
{/if}

<Sheet
  open={sheetOpen}
  title={editing ? 'Edit budget' : 'Add budget'}
  description="Leave a limit empty for no limit. A person or role rule without limits exempts them from broader rules."
  size="md"
  onclose={() => (sheetOpen = false)}
>
  <form
    class="space-y-4"
    onsubmit={(e) => {
      e.preventDefault()
      void save()
    }}
  >
    <FormField label="Applies to" for="budget-scope" controlWidth="full" hint={pro ? undefined : 'Role and person budgets need a Pro license.'}>
      <Select
        id="budget-scope"
        options={scopeOptions}
        bind:value={draft.scope}
        onchange={(v) => {
          draft.subject = v === 'role' ? 'analyst' : ''
        }}
      />
    </FormField>
    {#if draft.scope === 'role'}
      <FormField label="Role" for="budget-role" controlWidth="full">
        <Select id="budget-role" options={roleOptions} bind:value={draft.subject} />
      </FormField>
    {:else if draft.scope === 'user'}
      <FormField label="Person" for="budget-person" controlWidth="full" hint="People who signed in at least once, or have a role set in Admin > Users.">
        <Combobox id="budget-person" options={personOptions} value={draft.subject} placeholder="Pick a person" onChange={(v) => (draft.subject = v)} />
      </FormField>
    {/if}
    <FormField label="Period" for="budget-period" controlWidth="full">
      <Select id="budget-period" options={periodOptions} bind:value={draft.period} />
    </FormField>
    <FormField label="Token limit" for="budget-tokens" controlWidth="full" hint="Input, output and cache-write tokens. Unpriced models always count here.">
      <Input id="budget-tokens" type="number" min="1" step="1000" placeholder="No limit" bind:value={draft.tokenLimit} />
    </FormField>
    <FormField label="Dollar limit" for="budget-usd" controlWidth="full" hint={pro ? 'US dollars, from the price each request had when it ran.' : 'Dollar limits need a Pro license.'}>
      <Input id="budget-usd" type="number" min="0.01" step="0.01" placeholder="No limit" disabled={!pro} bind:value={draft.usdLimit} />
    </FormField>
    <label class="ds-checkbox-label text-xs">
      <input type="checkbox" class="ds-checkbox" disabled={!pro || !String(draft.usdLimit ?? '').trim()} bind:checked={draft.blockUnpriced} />
      Block models without a price while this dollar limit applies
    </label>
    <p class="text-xs text-fg-3">Preview: {limitsText({ token_limit: Number(draft.tokenLimit) > 0 ? Number(draft.tokenLimit) : null, usd_limit: Number(draft.usdLimit) > 0 ? Number(draft.usdLimit) : null, period: draft.period })}</p>
    {#if formError}
      <div class="rounded-md bg-danger-soft p-3"><p class="text-xs font-medium text-danger">{formError}</p></div>
    {/if}
  </form>
  {#snippet footer()}
    <Button size="sm" variant="ghost" onclick={() => (sheetOpen = false)}>Cancel</Button>
    <Button size="sm" loading={saving} onclick={() => void save()}>{editing ? 'Save budget' : 'Add budget'}</Button>
  {/snippet}
</Sheet>

<ConfirmDialog
  open={deleting !== null}
  title="Delete budget?"
  description={deleting ? `Delete the ${limitsText(deleting)} budget for ${subjectLabel(deleting.scope, deleting.subject)}? People it covered fall back to the next broader rule.` : ''}
  confirmLabel="Delete budget"
  destructive
  onconfirm={confirmDelete}
  oncancel={() => (deleting = null)}
/>
