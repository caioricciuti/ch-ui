<!-- SPDX-License-Identifier: BUSL-1.1 -->
<script lang="ts">
  import { X } from 'lucide-svelte'
  import type { RbacAssignees } from '../../../types/rbac'
  import Select from '../../common/Select.svelte'
  import Combobox, { type ComboboxOption } from '../../common/Combobox.svelte'
  import Button from '../../common/Button.svelte'

  /**
   * The TO clause of a row policy, quota or settings profile: nobody, a
   * list of users and roles, everyone, or everyone except a list.
   */
  interface Props {
    id: string
    value: RbacAssignees
    /** Users and roles to pick from. */
    options: ComboboxOption[]
    disabled?: boolean
    onchange: (next: RbacAssignees) => void
  }

  let { id, value, options, disabled = false, onchange }: Props = $props()

  type Mode = 'none' | 'list' | 'all' | 'except'
  const mode = $derived<Mode>(value.all ? (value.except && value.except.length ? 'except' : 'all') : value.names && value.names.length ? 'list' : 'none')
  // Once the person picks a mode it sticks, so "Selected users and roles"
  // stays selected while its list is still empty.
  let pinned = $state<Mode | null>(null)
  const shown = $derived<Mode>(pinned ?? mode)

  const modes = [
    { value: 'none', label: 'Nobody' },
    { value: 'list', label: 'Selected users and roles' },
    { value: 'all', label: 'Everyone' },
    { value: 'except', label: 'Everyone except' },
  ]

  const listed = $derived(shown === 'except' ? (value.except ?? []) : (value.names ?? []))
  const available = $derived(options.filter((o) => !listed.includes(o.value)))

  function setMode(next: string) {
    pinned = next as Mode
    if (next === 'none') onchange({ all: false, names: [], except: [] })
    else if (next === 'all') onchange({ all: true, names: [], except: [] })
    else if (next === 'list') onchange({ all: false, names: value.names ?? [], except: [] })
    else onchange({ all: true, names: [], except: value.except ?? [] })
  }

  function add(name: string) {
    if (!name) return
    if (shown === 'except') onchange({ all: true, names: [], except: [...(value.except ?? []), name] })
    else onchange({ all: false, names: [...(value.names ?? []), name], except: [] })
  }

  function remove(name: string) {
    if (shown === 'except') onchange({ all: true, names: [], except: (value.except ?? []).filter((n) => n !== name) })
    else onchange({ all: false, names: (value.names ?? []).filter((n) => n !== name), except: [] })
  }
</script>

<div class="space-y-2">
  <Select {id} size="sm" options={modes} value={shown} {disabled} onchange={setMode} />
  {#if shown === 'list' || shown === 'except'}
    <div class="flex flex-wrap gap-1.5">
      {#each listed as name (name)}
        <span class="inline-flex items-center gap-1 rounded-md bg-surface-2 py-0.5 pl-2 pr-0.5 text-xs text-fg-2">
          {name}
          <Button icon variant="ghost" size="xs" aria-label={`Remove ${name}`} {disabled} onclick={() => remove(name)}><X size={12} /></Button>
        </span>
      {:else}
        <span class="text-xs text-fg-4">{shown === 'except' ? 'Nobody excluded yet.' : 'Nobody selected yet.'}</span>
      {/each}
    </div>
    <Combobox options={available} placeholder="Add a user or role" {disabled} onChange={add} />
  {/if}
</div>
