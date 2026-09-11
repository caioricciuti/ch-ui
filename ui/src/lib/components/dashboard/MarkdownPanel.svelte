<script lang="ts">
  import { renderMarkdown } from '../brain/brain-markdown'

  interface Props {
    content: string
  }

  let { content }: Props = $props()

  const html = $derived(content ? renderMarkdown(content) : '<p class="text-fg-4">No content</p>')
</script>

<div class="w-full h-full overflow-y-auto px-4 py-3">
  <div class="prose-panel text-sm text-fg">
    {@html html}
  </div>
</div>
