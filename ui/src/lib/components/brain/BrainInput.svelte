<script lang="ts">
  import Button from '../common/Button.svelte'
  import { Send, Square } from 'lucide-svelte'
  import BrainMentionDropdown from './BrainMentionDropdown.svelte'
  import type { MentionRef } from '../../types/brain'

  interface Props {
    streaming: boolean
    onSend: (text: string, mentions: MentionRef[]) => void
    onStop?: () => void
  }

  let { streaming, onSend, onStop }: Props = $props()

  let editableEl: HTMLDivElement | undefined = $state()
  let mentionActive = $state(false)
  let mentionQuery = $state('')
  let isEmpty = $state(true)
  let dropdownRef: { handleKeydown: (e: KeyboardEvent) => boolean } | undefined = $state()

  const CHIP_COLORS: Record<string, { bg: string; text: string; border: string }> = {
    table: { bg: 'rgba(59,130,246,0.12)', text: '#3b82f6', border: 'rgba(59,130,246,0.25)' },
    dashboard: { bg: 'rgba(139,92,246,0.12)', text: '#8b5cf6', border: 'rgba(139,92,246,0.25)' },
    pipeline: { bg: 'rgba(16,185,129,0.12)', text: '#10b981', border: 'rgba(16,185,129,0.25)' },
    model: { bg: 'rgba(245,158,11,0.12)', text: '#f59e0b', border: 'rgba(245,158,11,0.25)' },
    saved_query: { bg: 'rgba(6,182,212,0.12)', text: '#06b6d4', border: 'rgba(6,182,212,0.25)' },
  }

  const TYPE_LABELS: Record<string, string> = {
    table: 'table',
    dashboard: 'dashboard',
    pipeline: 'pipeline',
    model: 'model',
    saved_query: 'query',
  }

  function chipDisplayName(mention: MentionRef): string {
    if (mention.type === 'table') return `${mention.database}.${mention.table}`
    return mention.name
  }

  function createChipElement(mention: MentionRef): HTMLSpanElement {
    const chip = document.createElement('span')
    chip.contentEditable = 'false'
    chip.className = 'brain-chip'
    chip.dataset.mentionType = mention.type

    if (mention.type === 'table') {
      chip.dataset.mentionDb = mention.database
      chip.dataset.mentionTable = mention.table
    } else {
      chip.dataset.mentionId = (mention as { id: string }).id
      chip.dataset.mentionName = (mention as { name: string }).name
    }

    const colors = CHIP_COLORS[mention.type] ?? CHIP_COLORS.table
    const prefix = TYPE_LABELS[mention.type] ?? mention.type

    Object.assign(chip.style, {
      display: 'inline-flex',
      alignItems: 'center',
      gap: '3px',
      padding: '0px 6px',
      borderRadius: '4px',
      fontSize: '13px',
      fontWeight: '500',
      lineHeight: '1.5',
      verticalAlign: 'baseline',
      userSelect: 'none',
      whiteSpace: 'nowrap',
      backgroundColor: colors.bg,
      color: colors.text,
      border: `1px solid ${colors.border}`,
    })

    const prefixSpan = document.createElement('span')
    prefixSpan.style.opacity = '0.6'
    prefixSpan.style.fontSize = '11px'
    prefixSpan.textContent = prefix + ':'
    chip.appendChild(prefixSpan)
    chip.appendChild(document.createTextNode(chipDisplayName(mention)))
    return chip
  }

  function getTextBeforeCursor(): string {
    const sel = window.getSelection()
    if (!sel || !sel.rangeCount || !editableEl) return ''
    const range = sel.getRangeAt(0)
    const preRange = document.createRange()
    preRange.selectNodeContents(editableEl)
    preRange.setEnd(range.startContainer, range.startOffset)
    return preRange.toString()
  }

  function detectMention() {
    const before = getTextBeforeCursor()
    const match = before.match(/(^|[\s])@([^\s]*)$/)
    if (match) {
      mentionActive = true
      mentionQuery = match[2]
    } else {
      mentionActive = false
      mentionQuery = ''
    }
  }

  function getContentText(): string {
    if (!editableEl) return ''
    let text = ''
    function walk(node: Node) {
      if (node.nodeType === Node.TEXT_NODE) {
        text += node.textContent ?? ''
      } else if (node instanceof HTMLBRElement) {
        text += '\n'
      } else if (node instanceof HTMLElement) {
        if (node.classList.contains('brain-chip')) {
          const type = node.dataset.mentionType
          if (type === 'table') {
            text += `@${node.dataset.mentionDb}.${node.dataset.mentionTable}`
          } else {
            text += `@${node.dataset.mentionName ?? ''}`
          }
        } else {
          for (const child of node.childNodes) walk(child)
        }
      }
    }
    for (const child of editableEl.childNodes) walk(child)
    return text.trim()
  }

  function extractMentions(): MentionRef[] {
    if (!editableEl) return []
    const mentions: MentionRef[] = []
    const seen = new Set<string>()
    const chips = editableEl.querySelectorAll('.brain-chip')
    for (const chip of chips) {
      const el = chip as HTMLElement
      const type = el.dataset.mentionType as MentionRef['type']
      let key: string
      if (type === 'table') {
        key = `table:${el.dataset.mentionDb}.${el.dataset.mentionTable}`
        if (!seen.has(key)) {
          seen.add(key)
          mentions.push({ type: 'table', database: el.dataset.mentionDb!, table: el.dataset.mentionTable! })
        }
      } else if (type === 'dashboard' || type === 'pipeline' || type === 'model' || type === 'saved_query') {
        key = `${type}:${el.dataset.mentionId}`
        if (!seen.has(key)) {
          seen.add(key)
          mentions.push({ type, id: el.dataset.mentionId!, name: el.dataset.mentionName! } as MentionRef)
        }
      }
    }
    return mentions
  }

  function send() {
    const text = getContentText()
    if (!text) return
    const mentions = extractMentions()
    onSend(text, mentions)
    if (editableEl) {
      editableEl.innerHTML = ''
      isEmpty = true
    }
  }

  function handleInput() {
    isEmpty = !getContentText()
    detectMention()
  }

  function handleKeydown(e: KeyboardEvent) {
    if (mentionActive && dropdownRef) {
      const handled = dropdownRef.handleKeydown(e)
      if (handled) return
    }

    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      send()
    }
  }

  function handlePaste(e: ClipboardEvent) {
    e.preventDefault()
    const text = e.clipboardData?.getData('text/plain') ?? ''
    const sel = window.getSelection()
    if (!sel || !sel.rangeCount) return
    const range = sel.getRangeAt(0)
    range.deleteContents()
    const textNode = document.createTextNode(text)
    range.insertNode(textNode)
    range.setStartAfter(textNode)
    range.collapse(true)
    sel.removeAllRanges()
    sel.addRange(range)
    handleInput()
  }

  function handleMentionSelect(mention: MentionRef) {
    insertChipAtMention(mention)
    mentionActive = false
    mentionQuery = ''
  }

  function handleMentionNavigate(newQuery: string) {
    replaceAtQuery(newQuery)
    mentionQuery = newQuery
  }

  function replaceAtQuery(newQuery: string) {
    const sel = window.getSelection()
    if (!sel || !sel.rangeCount || !editableEl) return

    const range = sel.getRangeAt(0)
    const container = range.startContainer
    if (container.nodeType !== Node.TEXT_NODE) return

    const text = container.textContent ?? ''
    const cursorOffset = range.startOffset
    const before = text.slice(0, cursorOffset)

    const match = before.match(/(^|[\s])@([^\s]*)$/)
    if (!match) return

    const prefix = match[1] || ''
    const atStart = cursorOffset - match[0].length + prefix.length
    const newText = text.slice(0, atStart) + '@' + newQuery + text.slice(cursorOffset)
    const textNode = container as Text
    textNode.textContent = newText

    const newCursorPos = atStart + 1 + newQuery.length
    const newRange = document.createRange()
    newRange.setStart(textNode, Math.min(newCursorPos, newText.length))
    newRange.collapse(true)
    sel.removeAllRanges()
    sel.addRange(newRange)
  }

  function insertChipAtMention(mention: MentionRef) {
    const sel = window.getSelection()
    if (!sel || !sel.rangeCount || !editableEl) return

    const range = sel.getRangeAt(0)
    const container = range.startContainer
    if (container.nodeType !== Node.TEXT_NODE) return

    const text = container.textContent ?? ''
    const cursorOffset = range.startOffset
    const before = text.slice(0, cursorOffset)

    const match = before.match(/(^|[\s])@([^\s]*)$/)
    if (!match) return

    const prefix = match[1] || ''
    const atStart = cursorOffset - match[0].length + prefix.length
    const textNode = container as Text

    const beforeText = text.slice(0, atStart)
    const afterText = text.slice(cursorOffset)

    textNode.textContent = beforeText

    const chip = createChipElement(mention)
    const space = document.createTextNode(' ')
    const afterNode = afterText ? document.createTextNode(afterText) : null

    const parent = textNode.parentNode!
    const insertRef = textNode.nextSibling

    parent.insertBefore(chip, insertRef)
    parent.insertBefore(space, insertRef)
    if (afterNode) parent.insertBefore(afterNode, insertRef)

    const newRange = document.createRange()
    newRange.setStartAfter(space)
    newRange.collapse(true)
    sel.removeAllRanges()
    sel.addRange(newRange)

    isEmpty = false
  }

  export function setText(text: string) {
    if (editableEl) {
      editableEl.textContent = text
      isEmpty = !text.trim()
      editableEl.focus()
    }
  }

  export function focus() {
    editableEl?.focus()
  }
</script>

<div class="border-t border-gray-200 dark:border-gray-800 p-4">
  <div class="relative flex items-end gap-2">
    {#if mentionActive}
      <BrainMentionDropdown
        query={mentionQuery}
        onSelect={handleMentionSelect}
        onNavigate={handleMentionNavigate}
        onDismiss={() => { mentionActive = false; mentionQuery = '' }}
        bind:this={dropdownRef}
      />
    {/if}
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div
      class="brain-editable flex-1 text-sm bg-transparent border border-gray-300 dark:border-gray-700 rounded-lg px-3 py-2 text-gray-800 dark:text-gray-200 min-h-[44px] max-h-[200px] overflow-auto focus:outline-none focus:ring-2 focus:ring-ch-blue/40 focus:border-ch-blue/50 transition-colors"
      contenteditable={!streaming}
      role="textbox"
      data-placeholder="Ask Brain about your data... (type @ to mention tables, dashboards, pipelines, models)"
      oninput={handleInput}
      onkeydown={handleKeydown}
      onpaste={handlePaste}
      bind:this={editableEl}
    ></div>
    {#if streaming && onStop}
      <Button size="sm" variant="secondary" onclick={onStop} title="Stop generating">
        <Square size={14} /> Stop
      </Button>
    {:else}
      <Button size="sm" onclick={send} disabled={isEmpty || streaming}>
        <Send size={14} />
      </Button>
    {/if}
  </div>
</div>

<style>
  .brain-editable:empty::before {
    content: attr(data-placeholder);
    color: #9ca3af;
    pointer-events: none;
  }
  .brain-editable:focus:empty::before {
    color: #b0b0b0;
  }
</style>
