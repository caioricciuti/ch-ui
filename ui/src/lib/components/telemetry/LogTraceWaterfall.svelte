<!-- SPDX-License-Identifier: BUSL-1.1 -->
<script lang="ts">
  import type { TraceDetail } from '../../types/telemetryPro'
  import { getTrace } from '../../api/telemetryPro'
  import TraceWaterfall from './TraceWaterfall.svelte'

  interface Props {
    /** Traces source to draw the waterfall from; '' when none is configured. */
    traceSourceId: string
    traceId: string
    highlightSpanId: string
    /** RFC3339 time of the log line; bounds the span lookup. */
    timeHint?: string
  }

  let { traceSourceId, traceId, highlightSpanId, timeHint = '' }: Props = $props()

  let traceDetail = $state<TraceDetail | null>(null)
  let traceDetailFor = $state('')
  const waterfallServices = $derived([...new Set((traceDetail?.spans ?? []).map((s) => s.service))])

  $effect(() => {
    if (traceId && traceSourceId && traceDetailFor !== traceId) {
      const id = traceId
      traceDetailFor = id
      traceDetail = null
      getTrace(traceSourceId, id, timeHint || undefined)
        .then((d) => {
          // Ignore a late answer for a trace that is no longer shown.
          if (traceDetailFor === id) traceDetail = d
        })
        .catch(() => {
          if (traceDetailFor === id) traceDetail = null
        })
    }
  })
</script>

{#if traceDetail && traceDetail.spans.length}
  <div class="mb-3 max-h-64 overflow-auto rounded-md border border-edge-subtle">
    <TraceWaterfall spans={traceDetail.spans} services={waterfallServices} highlightSpanId={highlightSpanId} compact />
  </div>
{/if}
