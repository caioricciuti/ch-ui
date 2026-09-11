<script lang="ts">
  import { HelpCircle } from 'lucide-svelte'
  import Tooltip from './Tooltip.svelte'

  interface Props {
    text: string
    side?: 'top' | 'bottom'
  }

  let { text, side = 'top' }: Props = $props()
</script>

<Tooltip {text} {side} delay={150} class="align-middle">
  <button type="button" class="inline-flex h-4 w-4 items-center justify-center rounded-full text-fg-4 hover:text-fg-2" aria-label="Help">
    <HelpCircle size={13} />
  </button>
</Tooltip>
