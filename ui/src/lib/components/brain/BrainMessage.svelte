<script lang="ts">
  import type { BrainArtifact, BrainMessage as BrainMessageType } from '../../types/brain'
  import { parseMessageSegments } from './brain-markdown'
  import BrainSqlBlock from './BrainSqlBlock.svelte'
  import BrainArtifactCard from './BrainArtifactCard.svelte'
  import Spinner from '../common/Spinner.svelte'

  interface Props {
    message: BrainMessageType
    artifacts: BrainArtifact[]
    streaming: boolean
    isLastMessage: boolean
    runningSql: string | null
    onRunSql: (sql: string, messageId?: string) => void
    onOpenInEditor: (sql: string) => void
  }

  let { message, artifacts, streaming, isLastMessage, runningSql, onRunSql, onOpenInEditor }: Props = $props()

  const segments = $derived(
    message.role === 'assistant' && message.content
      ? parseMessageSegments(message.content)
      : []
  )

  /** Find artifact whose query matches this sql block */
  function findArtifactForSql(sql: string): BrainArtifact | undefined {
    return artifacts.find(art => {
      if (art.type !== 'query_result') return false
      try {
        const payload = JSON.parse(art.content)
        return payload?.query?.trim() === sql.trim()
      } catch {
        return false
      }
    })
  }

  /** Get artifacts not associated with any specific SQL block */
  const orphanArtifacts = $derived.by(() => {
    if (message.role !== 'assistant') return []
    const sqlTexts = new Set(
      segments.filter(s => s.type === 'sql').map(s => s.content.trim())
    )
    return artifacts.filter(art => {
      if (art.type !== 'query_result') return true
      try {
        const payload = JSON.parse(art.content)
        return !sqlTexts.has(payload?.query?.trim())
      } catch {
        return true
      }
    })
  })
</script>

{#if message.role === 'user'}
  <div class="flex justify-end">
    <div class="max-w-[80%] bg-ch-blue text-white rounded-2xl rounded-br-sm px-4 py-2.5 text-sm whitespace-pre-wrap">{message.content}</div>
  </div>
{:else}
  <div class="flex justify-start">
    <div class="max-w-[85%] min-w-0">
      {#if message.content}
        {#each segments as seg}
          {#if seg.type === 'markdown'}
            <div class="prose-brain text-sm text-gray-800 dark:text-gray-200">{@html seg.html}</div>
          {:else if seg.type === 'sql'}
            <BrainSqlBlock
              sql={seg.content}
              messageId={message.id}
              artifact={findArtifactForSql(seg.content)}
              running={runningSql === seg.content}
              onRun={onRunSql}
              onOpenInEditor={onOpenInEditor}
            />
          {/if}
        {/each}

        {#if orphanArtifacts.length > 0}
          {#each orphanArtifacts as art}
            <BrainArtifactCard artifact={art} />
          {/each}
        {/if}
      {:else if streaming && isLastMessage}
        <div class="bg-gray-100 dark:bg-gray-800 rounded-2xl rounded-bl-sm px-4 py-3">
          <Spinner size="sm" />
        </div>
      {/if}
    </div>
  </div>
{/if}
