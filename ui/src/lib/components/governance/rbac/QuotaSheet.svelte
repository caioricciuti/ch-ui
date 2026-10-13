<!-- SPDX-License-Identifier: BUSL-1.1 -->
<script lang="ts">
  import { Plus, Trash2 } from 'lucide-svelte'
  import type { RbacAssignees, RbacOp, RbacQuota } from '../../../types/rbac'
  import { QUOTA_KEYS, QUOTA_RESOURCES, QUOTA_UNITS } from '../../../types/rbac'
  import Sheet from '../../common/Sheet.svelte'
  import Button from '../../common/Button.svelte'
  import Input from '../../common/Input.svelte'
  import Select from '../../common/Select.svelte'
  import FormField from '../../common/FormField.svelte'
  import Panel from '../../common/Panel.svelte'
  import type { ComboboxOption } from '../../common/Combobox.svelte'
  import AssigneesField from './AssigneesField.svelte'

  /** Create or change one quota: key, intervals with limits, assignees. */
  interface Props {
    open: boolean
    quota: RbacQuota | null
    assigneeOptions: ComboboxOption[]
    readOnly: boolean
    canEdit: boolean
    onstage: (ops: RbacOp[]) => void
    onclose: () => void
  }

  let { open, quota, assigneeOptions, readOnly, canEdit, onstage, onclose }: Props = $props()

  interface IntervalDraft {
    duration: string
    unit: string
    randomized: boolean
    limits: Record<string, string>
  }

  const initial = () => ({
    name: quota?.name ?? '',
    keyed_by: quota?.keyed_by ?? 'user_name',
    intervals: (quota?.intervals ?? [{ duration: 1, unit: 'hour', limits: {} }]).map<IntervalDraft>((iv) => ({
      duration: String(iv.duration),
      unit: iv.unit,
      randomized: !!iv.randomized,
      limits: Object.fromEntries(Object.entries(iv.limits ?? {}).map(([k, v]) => [k, String(v)])),
    })),
    to: { all: quota?.to.all ?? false, names: [...(quota?.to.names ?? [])], except: [...(quota?.to.except ?? [])] } as RbacAssignees,
  })
  let form = $state(initial())
  const locked = $derived(readOnly || !canEdit)
  const isNew = $derived(quota === null)

  const unitOptions = QUOTA_UNITS.map((u) => ({ value: u, label: u }))
  const resourceLabel = (r: string) => r.replaceAll('_', ' ')

  function parseLimit(v: string | undefined): number | null {
    const t = (v ?? '').trim()
    if (t === '') return 0
    return /^[0-9]+$/.test(t) ? Number(t) : null
  }

  const errors = $derived.by(() => {
    const out: string[] = []
    if (!form.name.trim()) out.push('A name is required.')
    form.intervals.forEach((iv, i) => {
      if (!/^[1-9][0-9]*$/.test(iv.duration.trim())) out.push(`Interval ${i + 1}: length must be a whole number above 0.`)
      for (const r of QUOTA_RESOURCES) {
        if (parseLimit(iv.limits[r]) === null) out.push(`Interval ${i + 1}: ${resourceLabel(r)} must be a whole number.`)
      }
    })
    return out
  })

  function addInterval() {
    form.intervals = [...form.intervals, { duration: '1', unit: 'day', randomized: false, limits: {} }]
  }

  function stage() {
    if (errors.length) return
    onstage([{
      kind: isNew ? 'create_quota' : 'alter_quota',
      quota: {
        name: form.name.trim(),
        keyed_by: form.keyed_by,
        intervals: form.intervals.map((iv) => {
          const limits: Record<string, number> = {}
          for (const r of QUOTA_RESOURCES) {
            const n = parseLimit(iv.limits[r])
            if (n) limits[r] = n
          }
          return { duration: Number(iv.duration), unit: iv.unit, randomized: iv.randomized, limits }
        }),
        to: form.to,
      },
    }])
    onclose()
  }

  function stageDrop() {
    if (!quota) return
    onstage([{ kind: 'drop_quota', name: quota.name }])
    onclose()
  }
</script>

<Sheet
  {open}
  title={isNew ? 'New quota' : `Quota ${quota?.name}`}
  description={readOnly ? 'Defined in users.xml, so it is read-only here.' : 'Limits resource use per interval. Empty or 0 means no limit. Staged until you apply the review.'}
  size="lg"
  {onclose}
>
  <div class="space-y-4">
    <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
      <FormField label="Name" for="rbac-q-name" required>
        <Input id="rbac-q-name" size="sm" bind:value={form.name} disabled={locked || !isNew} spellcheck={false} placeholder="analyst_hourly" />
      </FormField>
      <FormField label="Counted" for="rbac-q-key">
        <Select id="rbac-q-key" size="sm" options={QUOTA_KEYS} bind:value={form.keyed_by} disabled={locked} />
      </FormField>
    </div>

    {#each form.intervals as iv, i (i)}
      <Panel variant="muted" padding="sm">
        <div class="space-y-3">
          <div class="flex flex-wrap items-end gap-2">
            <FormField label="Every" for={`rbac-q-dur-${i}`} controlWidth="sm">
              <Input id={`rbac-q-dur-${i}`} size="sm" bind:value={iv.duration} disabled={locked} />
            </FormField>
            <FormField label="Unit" for={`rbac-q-unit-${i}`} controlWidth="sm">
              <Select id={`rbac-q-unit-${i}`} size="sm" options={unitOptions} bind:value={iv.unit} disabled={locked} />
            </FormField>
            <label class="ds-checkbox-label mb-1.5 text-xs">
              <input type="checkbox" class="ds-checkbox" bind:checked={iv.randomized} disabled={locked} />
              Randomized start
            </label>
            <div class="flex-1"></div>
            <Button icon variant="ghost" size="sm" aria-label="Remove interval" title="Remove interval" disabled={locked} onclick={() => (form.intervals = form.intervals.filter((_, j) => j !== i))}>
              <Trash2 size={13} />
            </Button>
          </div>
          <div class="grid grid-cols-2 gap-2 md:grid-cols-3">
            {#each QUOTA_RESOURCES as r (r)}
              <FormField label={resourceLabel(r)} for={`rbac-q-${i}-${r}`} controlWidth="full">
                <Input
                  id={`rbac-q-${i}-${r}`}
                  size="sm"
                  value={iv.limits[r] ?? ''}
                  placeholder="no limit"
                  disabled={locked}
                  oninput={(e) => (iv.limits = { ...iv.limits, [r]: e.currentTarget.value })}
                />
              </FormField>
            {/each}
          </div>
        </div>
      </Panel>
    {/each}
    <Button size="sm" variant="outline" disabled={locked} onclick={addInterval}><Plus size={13} /> Add interval</Button>

    <FormField label="Applies to" for="rbac-q-to" controlWidth="full">
      <AssigneesField id="rbac-q-to" value={form.to} options={assigneeOptions} disabled={locked} onchange={(next) => (form.to = next)} />
    </FormField>

    {#if errors.length && form.name.trim()}
      <ul class="space-y-0.5 text-xs text-danger">
        {#each errors as e (e)}<li>{e}</li>{/each}
      </ul>
    {/if}
  </div>

  {#snippet footer()}
    {#if !isNew && !locked}
      <Button size="sm" variant="ghost" class="mr-auto text-danger hover:text-danger" onclick={stageDrop}>Drop quota</Button>
    {/if}
    <Button size="sm" variant="outline" onclick={onclose}>Cancel</Button>
    <Button size="sm" disabled={locked || errors.length > 0} onclick={stage}>{isNew ? 'Stage quota' : 'Stage change'}</Button>
  {/snippet}
</Sheet>
