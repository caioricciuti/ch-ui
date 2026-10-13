<script lang="ts">
  import type { SavedQuery, SavedQueryFolder, SavedQueryVisibility } from '../../types/api'
  import {
    updateSavedQuery, folderOptions, visibilityOptions, parseTags, validateSavedQueryDetails,
  } from '../../api/savedQueries'
  import { error as toastError, success as toastSuccess } from '../../stores/toast.svelte'
  import Sheet from '../common/Sheet.svelte'
  import Button from '../common/Button.svelte'
  import FormField from '../common/FormField.svelte'
  import Input from '../common/Input.svelte'
  import Textarea from '../common/Textarea.svelte'
  import Select from '../common/Select.svelte'

  /**
   * Edit details of a saved query: name, description, folder, tags and
   * visibility. The SQL itself is edited in the query workspace.
   */
  interface Props {
    query: SavedQuery | null
    folders: SavedQueryFolder[]
    isAdmin: boolean
    onclose: () => void
    onsaved: (updated: SavedQuery) => void
  }

  let { query, folders, isAdmin, onclose, onsaved }: Props = $props()

  let form = $state({ name: '', description: '', folder: '', tags: '', visibility: 'shared' as SavedQueryVisibility })
  let saving = $state(false)
  let touched = $state(false)

  // Reset the form whenever another query is opened.
  let loadedId = $state<string | null>(null)
  $effect(() => {
    const q = query
    if (!q || q.id === loadedId) return
    loadedId = q.id
    touched = false
    form = {
      name: q.name,
      description: q.description ?? '',
      folder: q.folder_id ?? '',
      tags: q.tags.join(', '),
      visibility: q.visibility,
    }
  })
  $effect(() => {
    if (!query) loadedId = null
  })

  const error = $derived(query ? validateSavedQueryDetails(form, isAdmin, form.visibility, query.visibility) : null)
  const readOnly = $derived(!query?.can_edit)

  async function save() {
    touched = true
    if (!query || error || readOnly) return
    saving = true
    try {
      const updated = await updateSavedQuery(query.id, {
        name: form.name.trim(),
        description: form.description.trim(),
        folder_id: form.folder || null,
        tags: parseTags(form.tags),
        visibility: form.visibility,
      })
      toastSuccess('Saved query updated')
      onsaved(updated)
    } catch (e: unknown) {
      toastError(e instanceof Error ? e.message : String(e))
    } finally {
      saving = false
    }
  }
</script>

<Sheet
  open={!!query}
  title="Edit details"
  description={readOnly ? 'Only the owner or an admin can change this query.' : 'Name, folder, tags and who can see it. Edit the SQL in the editor.'}
  size="md"
  onclose={onclose}
>
  <form class="space-y-4" onsubmit={(e) => { e.preventDefault(); void save() }}>
    <FormField label="Name" for="sq-name" required controlWidth="full" error={touched && !form.name.trim() ? 'Name is required' : undefined}>
      <Input id="sq-name" bind:value={form.name} disabled={readOnly} autocomplete="off" />
    </FormField>
    <FormField label="Description" for="sq-desc" controlWidth="full">
      <Textarea id="sq-desc" rows={3} bind:value={form.description} disabled={readOnly} placeholder="What this query answers" />
    </FormField>
    <FormField label="Folder" for="sq-folder" controlWidth="full">
      <Select id="sq-folder" options={folderOptions(folders)} bind:value={form.folder} disabled={readOnly} />
    </FormField>
    <FormField label="Tags" for="sq-tags" controlWidth="full" hint="Comma-separated, e.g. finance, daily. Up to 20 tags of 40 characters.">
      <Input id="sq-tags" bind:value={form.tags} disabled={readOnly} placeholder="finance, daily" autocomplete="off" />
    </FormField>
    <FormField
      label="Visible to"
      for="sq-visibility"
      controlWidth="full"
      hint="Everyone can see and run shared queries. Only you see private ones. Admins only is set by admins."
    >
      <Select id="sq-visibility" options={visibilityOptions(isAdmin, query?.visibility)} bind:value={form.visibility} disabled={readOnly} />
    </FormField>
    {#if touched && error && form.name.trim()}
      <p class="text-xs text-danger" role="alert">{error}</p>
    {/if}
    <button type="submit" class="hidden" aria-hidden="true"></button>
  </form>
  {#snippet footer()}
    <Button size="sm" variant="ghost" onclick={onclose}>Cancel</Button>
    <Button size="sm" loading={saving} disabled={readOnly || !!error} onclick={() => void save()}>Save</Button>
  {/snippet}
</Sheet>
