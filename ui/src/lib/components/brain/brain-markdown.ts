import { Marked } from 'marked'
import DOMPurify from 'dompurify'

export interface MessageSegment {
  type: 'markdown' | 'sql'
  content: string
  html?: string
}

const SQL_FENCE_RE = /```sql\n([\s\S]*?)```/g

const marked = new Marked({
  breaks: true,
  gfm: true,
})

// Force external links to open safely and never leak the opener window.
DOMPurify.addHook('afterSanitizeAttributes', (node) => {
  if (node instanceof HTMLElement && node.tagName === 'A' && node.hasAttribute('href')) {
    node.setAttribute('target', '_blank')
    node.setAttribute('rel', 'noopener noreferrer nofollow')
  }
})

/**
 * Render a markdown string to sanitized HTML.
 *
 * Output is fed to `{@html}` in BrainMessage and MarkdownPanel (the latter is
 * reachable on unauthenticated public dashboard share links), so the markdown
 * may originate from untrusted sources — AI output influenced by table/column
 * names and query data, or dashboard text authored by another user. We must
 * never emit raw HTML; DOMPurify strips scripts, event handlers, and
 * javascript:/data: URLs while leaving normal formatting intact.
 */
export function renderMarkdown(content: string): string {
  const dirty = marked.parse(content) as string
  return DOMPurify.sanitize(dirty, { USE_PROFILES: { html: true } })
}

/**
 * Split assistant message content into alternating markdown and sql segments.
 * SQL segments are extracted from ```sql fences so they can be rendered
 * as interactive Svelte components instead of static HTML.
 */
export function parseMessageSegments(content: string): MessageSegment[] {
  const segments: MessageSegment[] = []
  let lastIndex = 0

  for (const match of content.matchAll(SQL_FENCE_RE)) {
    const matchStart = match.index!
    // Markdown text before this sql block
    if (matchStart > lastIndex) {
      const md = content.slice(lastIndex, matchStart)
      segments.push({ type: 'markdown', content: md, html: renderMarkdown(md) })
    }
    // The sql block itself
    segments.push({ type: 'sql', content: match[1].trim() })
    lastIndex = matchStart + match[0].length
  }

  // Trailing markdown after the last sql block
  if (lastIndex < content.length) {
    const md = content.slice(lastIndex)
    segments.push({ type: 'markdown', content: md, html: renderMarkdown(md) })
  }

  return segments
}

/** Extract raw SQL strings from ```sql fences. */
export function extractSqlBlocks(content: string): string[] {
  const blocks: string[] = []
  for (const match of content.matchAll(SQL_FENCE_RE)) {
    blocks.push(match[1].trim())
  }
  return blocks
}

const SQL_KEYWORDS = new Set([
  'SELECT','FROM','WHERE','JOIN','LEFT','RIGHT','INNER','OUTER','CROSS','FULL',
  'ON','AND','OR','NOT','IN','IS','NULL','LIKE','BETWEEN','EXISTS',
  'GROUP','BY','ORDER','ASC','DESC','LIMIT','OFFSET','HAVING',
  'INSERT','INTO','VALUES','UPDATE','SET','DELETE','CREATE','ALTER','DROP',
  'TABLE','INDEX','VIEW','AS','WITH','UNION','ALL','DISTINCT','CASE','WHEN',
  'THEN','ELSE','END','CAST','IF','ARRAY','MAP','TUPLE',
  'FORMAT','USING','ENGINE','PARTITION','SAMPLE','PREWHERE','GLOBAL',
  'ANY','ANTI','SEMI','MATERIALIZED','FINAL','SETTINGS',
  'TRUE','FALSE','COUNT','SUM','AVG','MIN','MAX','UNIQ',
])

/** Lightweight SQL syntax highlighting — returns HTML with colored spans. */
export function highlightSQL(sql: string): string {
  return sql.replace(
    // The final ([\s\S]) group matches every character the named groups do
    // not, so it reaches the escapeHtml fallthrough below. Without it, a raw
    // "<" between tokens went straight into {@html}.
    /('(?:[^'\\]|\\.)*')|("(?:[^"\\]|\\.)*")|(--[^\n]*)|(\b\d+(?:\.\d+)?\b)|(\b[A-Za-z_]\w*\b)|([\s\S])/g,
    (match, singleStr: string, doubleStr: string, comment: string, num: string, word: string) => {
      if (singleStr || doubleStr)
        return `<span class="text-green-600 dark:text-green-400">${escapeHtml(match)}</span>`
      if (comment)
        return `<span class="text-gray-400 italic">${escapeHtml(match)}</span>`
      if (num)
        return `<span class="text-amber-600 dark:text-amber-400">${escapeHtml(match)}</span>`
      if (word && SQL_KEYWORDS.has(word.toUpperCase()))
        return `<span class="text-blue-500 dark:text-blue-400 font-semibold">${escapeHtml(match)}</span>`
      return escapeHtml(match)
    },
  )
}

function escapeHtml(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;')
}
