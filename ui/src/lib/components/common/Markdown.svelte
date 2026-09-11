<script lang="ts">
  import { renderMarkdown } from '../brain/brain-markdown'

  /**
   * Renders trusted-ish markdown (incident notes, policy descriptions)
   * with the same renderer and styles as Brain replies. The renderer
   * escapes HTML; SQL fences are highlighted.
   */
  interface Props {
    content: string
    /** Slightly smaller type for compact places (table cells, sheets). */
    size?: 'sm' | 'md'
    class?: string
  }

  let { content, size = 'md', class: cls = '' }: Props = $props()
  const html = $derived(renderMarkdown(content ?? ''))
</script>

<div class="prose-brain {size === 'sm' ? 'text-xs' : 'text-[13px]'} leading-relaxed text-fg-2 {cls}">
  {@html html}
</div>
