<script lang="ts">
  import { useNodesInitialized, useSvelteFlow, useViewportInitialized } from '@xyflow/svelte'
  import { FIT_VIEW_OPTIONS } from './transforms/graph'

  /**
   * Lives inside <SvelteFlow>. Fits the graph once, after every node has been
   * measured; a fit on mount would use unmeasured nodes and miss the wide ones.
   */
  interface Props {
    /** Off when the pipeline opens on a saved viewport. */
    enabled: boolean
  }

  let { enabled }: Props = $props()

  const flow = useSvelteFlow()
  const nodesReady = useNodesInitialized()
  const viewportReady = useViewportInitialized()
  let done = false

  $effect(() => {
    if (done || !enabled || !nodesReady.current || !viewportReady.current) return
    done = true
    // A frame later the canvas has its final width (the side panel included).
    const raf = requestAnimationFrame(() => { void flow.fitView(FIT_VIEW_OPTIONS) })
    return () => cancelAnimationFrame(raf)
  })
</script>
