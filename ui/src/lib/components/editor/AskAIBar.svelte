<!-- SPDX-License-Identifier: BUSL-1.1 -->
<script lang="ts">
  import { X, Sparkles } from 'lucide-svelte'
  import { generateSQL } from '../../api/brainPro'
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

  // Ask AI (text-to-SQL)
  let asking = $state(false)

  async function handleAsk() {
    const q = question.trim()
    if (!q || asking) return
    asking = true
    try {
      const res = await generateSQL(q)
      const sql = (res.sql ?? '').trim()
      if (!sql) {
        toastError('The AI did not return a query. Try rephrasing.')
        return
      }
      oninsert(sql)
      const used = res.tables_used?.length ? ` · ${res.tables_used.length} table(s)` : ''
      toastSuccess(`Generated SQL${used}. Review before running.`)
      onclose()
      question = ''
    } catch (e: unknown) {
      let message = e instanceof Error ? e.message : 'Failed to generate SQL'
      if (/no active ai model/i.test(message)) {
        message = 'No AI provider configured — add one in Admin → Brain.'
      }
      toastError(message)
    } finally {
      asking = false
    }
  }
</script>

<div class="flex items-center gap-2 px-3 py-2">
  <Sparkles size={15} class="text-ch-orange shrink-0" />
  <input
    class="ds-input-sm flex-1"
    placeholder="Describe the query in plain English — e.g. “top 10 users by orders last month”"
    bind:value={question}
    spellcheck="false"
    disabled={asking}
    onkeydown={(e) => { if (e.key === 'Enter') handleAsk(); if (e.key === 'Escape') onclose() }}
  />
  <Button size="sm" onclick={handleAsk} loading={asking} disabled={!question.trim()}>
    Generate
  </Button>
  <Button icon variant="ghost" size="sm" onclick={onclose} title="Close" aria-label="Close Ask AI">
    <X size={14} />
  </Button>
</div>
<p class="px-3 pb-2 -mt-1 text-[11px] text-fg-3">
  Grounded in this connection's schema and your documented models. Always review generated SQL before running.
</p>
