<script lang="ts">
  import Sheet from '../common/Sheet.svelte'
  import { closeShortcutHelp, isShortcutHelpOpen } from '../../stores/shortcut-help.svelte'
  import { shortcutGroups } from '../../utils/shortcuts'

  const groups = shortcutGroups()
</script>

<Sheet
  open={isShortcutHelpOpen()}
  title="Keyboard shortcuts"
  description="Press ? anywhere outside a text field to open this list."
  size="sm"
  onclose={closeShortcutHelp}
>
  <div class="space-y-6">
    {#each groups as group (group.title)}
      <section>
        <h3 class="mb-2 text-[11px] font-medium uppercase tracking-[0.08em] text-fg-4">{group.title}</h3>
        <dl class="divide-y divide-edge-subtle rounded-md border border-edge-subtle">
          {#each group.items as item (item.label + item.keys.join('+'))}
            <div class="flex items-center justify-between gap-4 px-3 py-2">
              <dt class="text-[13px] text-fg-2">{item.label}</dt>
              <dd class="flex shrink-0 items-center gap-1">
                {#each item.keys as key, i (i)}
                  <kbd class="min-w-[22px] rounded border border-edge bg-surface-2 px-1.5 py-0.5 text-center font-mono text-[11px] text-fg">{key}</kbd>
                {/each}
              </dd>
            </div>
          {/each}
        </dl>
      </section>
    {/each}
  </div>
</Sheet>
