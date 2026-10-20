<script lang="ts">
  import { ExternalLink } from 'lucide-svelte'

  /**
   * The "Docs" pill that links a screen to its ch-ui.com page. PageHeader
   * renders it from its `docs` prop; the query workspace tab bar renders
   * it directly.
   */
  interface Props {
    /** Absolute docs URL, usually from `docsURL` in routes.ts. */
    href: string
    class?: string
  }

  let { href, class: cls = '' }: Props = $props()
</script>

<a
  class="ds-badge border border-edge bg-transparent transition-colors hover:border-edge-strong hover:bg-hover hover:text-fg {cls}"
  {href}
  target="_blank"
  rel="noreferrer"
  title="Open the docs for this page"
>
  Docs <ExternalLink size={11} />
</a>
