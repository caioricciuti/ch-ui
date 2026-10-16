<script lang="ts">
  import Button from '../common/Button.svelte'
  import { success, error as toastError } from '../../stores/toast.svelte'
  import { Copy } from 'lucide-svelte'

  interface Props {
    name: string
    secret: string
  }

  let { name, secret }: Props = $props()

  async function copy() {
    try {
      await navigator.clipboard.writeText(secret)
      success('Key copied')
    } catch {
      toastError('Clipboard unavailable')
    }
  }
</script>

<div class="space-y-2">
  <div class="rounded-md border border-warning/40 bg-warning-soft px-3 py-2 text-xs text-fg">
    Key "{name}" created. Copy it now: you will not see it again. Only its prefix is kept.
  </div>
  <div class="flex items-center gap-2">
    <pre class="flex-1 overflow-x-auto whitespace-pre-wrap break-all rounded-md bg-surface-2 p-3 font-mono text-xs text-fg">{secret}</pre>
    <Button size="md" variant="outline" icon aria-label="Copy key" title="Copy key" onclick={copy}>
      <Copy size={14} />
    </Button>
  </div>
</div>
