<script lang="ts">
  /** A small text link to one of the "how do I" guides on ch-ui.com. */
  import { ExternalLink } from 'lucide-svelte'
  import { guideURL, type Guide } from '../../routes'

  interface Props {
    guide: Guide
    label?: string
    class?: string
  }

  let { guide, label = 'Read the guide', class: cls = '' }: Props = $props()
</script>

<a
  class="inline-flex items-center gap-1 text-xs text-accent hover:underline {cls}"
  href={guideURL(guide)}
  target="_blank"
  rel="noreferrer"
>
  {label} <ExternalLink size={11} />
</a>
