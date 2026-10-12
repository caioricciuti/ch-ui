<!-- SPDX-License-Identifier: BUSL-1.1 -->
<script lang="ts">
  import { X, Sparkles } from 'lucide-svelte'
  import { onDestroy } from 'svelte'
  import { generateSQLStream } from '../../api/brainPro'
  import { error as toastError, success as toastSuccess } from '../../stores/toast.svelte'
  import Button from '../common/Button.svelte'

  interface Props {
    /** Draft question; bound so it survives closing and reopening the bar. */
    question: string
    /** Put the generated SQL into the editor. */
    oninsert: (sql: string) => void
    onclose: () => void
  }

  let { question = $bindable(), oninsert, onclose }: Props = $props()

  // Ask AI (text-to-SQL), streamed so the query shows up as it is written.
  let asking = $state(false)
  let preview = $state('')
  let controller: AbortController | null = null

  onDestroy(() => controller?.abort())

  function close() {
    controller?.abort()
    onclose()
  }

  async function handleAsk() {
    const q = question.trim()
    if (!q || asking) return
    asking = true
    preview = ''
    controller = new AbortController()
    try {
      const res = await generateSQLStream({ question: q }, (text) => (preview += text), controller.signal)
      const sql = (res.sql ?? '').trim()
      if (!sql) {
        toastError('The AI did not return a query. Try rephrasing.')
        return
      }
      oninsert(sql)
      const used = res.tables_used?.length ? `, ${res.tables_used.length} table(s)` : ''
      if (res.truncated) {
        toastError('The answer hit the output limit, so the SQL may be incomplete. Review it before running.')
      } else {
        toastSuccess(`Generated SQL${used}. Review before running.`)
      }
      onclose()
      question = ''
    } catch (e: unknown) {
      if (e instanceof DOMException && e.name === 'AbortError') return
      let message = e instanceof Error ? e.message : 'Failed to generate SQL'
      if (/no active ai model/i.test(message)) {
        message = 'No AI provider configured. Add one in Admin > Brain.'
      }
      toastError(message)
    } finally {
      asking = false
      preview = ''
      controller = null
    }
  }
</script>

<div class="flex items-center gap-2 px-3 py-2">
  <Sparkles size={15} class="text-ch-orange shrink-0" />
  <input
    class="ds-input-sm flex-1"
    placeholder="Describe the query in plain English, for example: top 10 users by orders last month"
    bind:value={question}
    spellcheck="false"
    disabled={asking}
    onkeydown={(e) => { if (e.key === 'Enter') handleAsk(); if (e.key === 'Escape') close() }}
  />
  <Button size="sm" onclick={handleAsk} loading={asking} disabled={!question.trim()}>
    Generate
  </Button>
  <Button icon variant="ghost" size="sm" onclick={close} title="Close" aria-label="Close Ask AI">
    <X size={14} />
  </Button>
</div>
{#if asking && preview}
  <pre class="mx-3 mb-2 max-h-40 overflow-auto rounded-md bg-surface-2 px-3 py-2 font-mono text-xs text-fg-2 whitespace-pre-wrap" aria-live="polite">{preview}</pre>
{/if}
<p class="px-3 pb-2 -mt-1 text-[11px] text-fg-3">
  Grounded in this connection's schema and your documented models. Always review generated SQL before running.
</p>
