<!-- SPDX-License-Identifier: BUSL-1.1 -->
<script lang="ts">
  import { Plus, Trash2, X } from 'lucide-svelte'
  import type { RbacAssignees, RbacOp, RbacSettingsProfile } from '../../../types/rbac'
  import { WRITABILITIES } from '../../../types/rbac'
  import Sheet from '../../common/Sheet.svelte'
  import Button from '../../common/Button.svelte'
  import Input from '../../common/Input.svelte'
  import Select from '../../common/Select.svelte'
  import FormField from '../../common/FormField.svelte'
  import Combobox, { type ComboboxOption } from '../../common/Combobox.svelte'
  import AssigneesField from './AssigneesField.svelte'

  /** Create or change one settings profile: settings with bounds, inherited profiles, assignees. */
  interface Props {
    open: boolean
    profile: RbacSettingsProfile | null
    profileNames: string[]
    assigneeOptions: ComboboxOption[]
    readOnly: boolean
    canEdit: boolean
    onstage: (ops: RbacOp[]) => void
    onclose: () => void
  }

  let { open, profile, profileNames, assigneeOptions, readOnly, canEdit, onstage, onclose }: Props = $props()

  interface SettingDraft {
    name: string
    value: string
    min: string
    max: string
    writability: string
  }

  const initial = () => ({
    name: profile?.name ?? '',
    settings: (profile?.settings ?? []).map<SettingDraft>((s) => ({
      name: s.name, value: s.value ?? '', min: s.min ?? '', max: s.max ?? '', writability: s.writability ?? '',
    })),
    inherit: [...(profile?.inherit ?? [])],
    to: { all: profile?.to.all ?? false, names: [...(profile?.to.names ?? [])], except: [...(profile?.to.except ?? [])] } as RbacAssignees,
  })
  let form = $state(initial())
  const locked = $derived(readOnly || !canEdit)
  const isNew = $derived(profile === null)

  const inheritOptions = $derived<ComboboxOption[]>(
    profileNames.filter((p) => p !== form.name && !form.inherit.includes(p)).map((p) => ({ value: p, label: p })),
  )

  const errors = $derived.by(() => {
    const out: string[] = []
    if (!form.name.trim()) out.push('A name is required.')
    const seen = new Set<string>()
    form.settings.forEach((s, i) => {
      const n = s.name.trim()
      if (!/^[A-Za-z0-9_]+$/.test(n)) out.push(`Setting ${i + 1}: use letters, digits and underscores only.`)
      if (seen.has(n)) out.push(`Setting ${n} appears twice.`)
      seen.add(n)
      if (!s.value.trim() && !s.min.trim() && !s.max.trim() && !s.writability) out.push(`Setting ${n || i + 1}: give a value, a bound or a mode.`)
    })
    return out
  })

  const opt = (v: string) => (v.trim() === '' ? null : v.trim())

  function stage() {
    if (errors.length) return
    onstage([{
      kind: isNew ? 'create_settings_profile' : 'alter_settings_profile',
      settings_profile: {
        name: form.name.trim(),
        settings: form.settings.map((s) => ({ name: s.name.trim(), value: opt(s.value), min: opt(s.min), max: opt(s.max), writability: s.writability })),
        inherit: form.inherit,
        to: form.to,
      },
    }])
    onclose()
  }

  function stageDrop() {
    if (!profile) return
    onstage([{ kind: 'drop_settings_profile', name: profile.name }])
    onclose()
  }
</script>

<Sheet
  {open}
  title={isNew ? 'New settings profile' : `Settings profile ${profile?.name}`}
  description={readOnly ? 'Defined in users.xml, so it is read-only here.' : 'Settings, their bounds and whether users may change them. Staged until you apply the review.'}
  size="lg"
  {onclose}
>
  <div class="space-y-4">
    <FormField label="Name" for="rbac-p-name" required>
      <Input id="rbac-p-name" size="sm" bind:value={form.name} disabled={locked || !isNew} spellcheck={false} placeholder="analyst_limits" />
    </FormField>

    <section class="space-y-2">
      <h3 class="text-[13px] font-semibold text-fg">Settings</h3>
      {#if form.settings.length === 0}
        <p class="text-xs text-fg-3">No settings yet.</p>
      {:else}
        <div class="overflow-x-auto rounded-lg border border-edge-subtle bg-surface">
          <table class="w-full text-[13px]">
            <thead>
              <tr class="text-left text-xs text-fg-3">
                <th class="px-3 py-1.5 font-medium">Setting</th>
                <th class="px-2 py-1.5 font-medium">Value</th>
                <th class="px-2 py-1.5 font-medium">Min</th>
                <th class="px-2 py-1.5 font-medium">Max</th>
                <th class="px-2 py-1.5 font-medium">Mode</th>
                <th class="w-8"><span class="sr-only">Remove</span></th>
              </tr>
            </thead>
            <tbody>
              {#each form.settings as s, i (i)}
                <tr class="border-t border-edge-subtle">
                  <td class="px-3 py-1"><Input size="sm" mono bind:value={s.name} disabled={locked} placeholder="max_threads" /></td>
                  <td class="px-2 py-1"><Input size="sm" mono bind:value={s.value} disabled={locked} /></td>
                  <td class="px-2 py-1"><Input size="sm" mono bind:value={s.min} disabled={locked} /></td>
                  <td class="px-2 py-1"><Input size="sm" mono bind:value={s.max} disabled={locked} /></td>
                  <td class="px-2 py-1"><Select size="sm" options={WRITABILITIES} bind:value={s.writability} disabled={locked} /></td>
                  <td class="pr-2">
                    <Button icon variant="ghost" size="xs" aria-label="Remove setting" disabled={locked} onclick={() => (form.settings = form.settings.filter((_, j) => j !== i))}>
                      <Trash2 size={13} />
                    </Button>
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
      <Button size="sm" variant="outline" disabled={locked} onclick={() => (form.settings = [...form.settings, { name: '', value: '', min: '', max: '', writability: '' }])}>
        <Plus size={13} /> Add setting
      </Button>
    </section>

    <FormField label="Inherits from" for="rbac-p-inherit" controlWidth="full" hint="Settings of these profiles apply first.">
      <div class="space-y-2">
        <div class="flex flex-wrap gap-1.5">
          {#each form.inherit as p (p)}
            <span class="inline-flex items-center gap-1 rounded-md bg-surface-2 py-0.5 pl-2 pr-0.5 text-xs text-fg-2">
              {p}
              <Button icon variant="ghost" size="xs" aria-label={`Remove ${p}`} disabled={locked} onclick={() => (form.inherit = form.inherit.filter((x) => x !== p))}><X size={12} /></Button>
            </span>
          {/each}
        </div>
        <Combobox id="rbac-p-inherit" options={inheritOptions} placeholder="Add a profile" disabled={locked} onChange={(v) => { if (v) form.inherit = [...form.inherit, v] }} />
      </div>
    </FormField>

    <FormField label="Applies to" for="rbac-p-to" controlWidth="full">
      <AssigneesField id="rbac-p-to" value={form.to} options={assigneeOptions} disabled={locked} onchange={(next) => (form.to = next)} />
    </FormField>

    {#if errors.length && form.name.trim()}
      <ul class="space-y-0.5 text-xs text-danger">
        {#each errors as e (e)}<li>{e}</li>{/each}
      </ul>
    {/if}
  </div>

  {#snippet footer()}
    {#if !isNew && !locked}
      <Button size="sm" variant="ghost" class="mr-auto text-danger hover:text-danger" onclick={stageDrop}>Drop profile</Button>
    {/if}
    <Button size="sm" variant="outline" onclick={onclose}>Cancel</Button>
    <Button size="sm" disabled={locked || errors.length > 0} onclick={stage}>{isNew ? 'Stage profile' : 'Stage change'}</Button>
  {/snippet}
</Sheet>
