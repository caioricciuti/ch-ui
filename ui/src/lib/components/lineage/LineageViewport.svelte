<script lang="ts">
  import { useSvelteFlow } from '@xyflow/svelte'
  import { NODE_HEIGHT, NODE_WIDTH, type LayoutNode } from '../../utils/lineage-layout'

  /**
   * Lives inside <SvelteFlow> to reach its viewport. A new layout fits the
   * view; a center request pans to one node instead.
   */
  interface Props {
    nodes: LayoutNode[]
    center: { id: string; seq: number } | null
  }

  let { nodes, center }: Props = $props()

  const flow = useSvelteFlow()

  $effect(() => {
    const target = center ? nodes.find((n) => n.id === center.id) : undefined
    // Wait a frame so xyflow has the new nodes before moving the camera.
    const raf = requestAnimationFrame(() => {
      if (target) {
        void flow.setCenter(target.x + NODE_WIDTH / 2, target.y + NODE_HEIGHT / 2, { zoom: Math.max(flow.getZoom(), 1), duration: 250 })
      } else {
        void flow.fitView({ padding: 0.15, maxZoom: 1.25, duration: 200 })
      }
    })
    return () => cancelAnimationFrame(raf)
  })
</script>
