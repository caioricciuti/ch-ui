import { Decoration, EditorView, ViewPlugin, hoverTooltip, type DecorationSet, type ViewUpdate } from '@codemirror/view'
import { RangeSetBuilder, StateEffect } from '@codemirror/state'
import { syntaxTree } from '@codemirror/language'
import { getDatabases } from '../stores/schema.svelte'
import { openDatabaseTab, openTableTab } from '../stores/tabs.svelte'
import { ensureDatabasesLoaded, ensureTablesCached, findTablesForDatabase, hasTablesCached } from './completions'

// Cmd/Ctrl+click on a database or table name opens its tab, like BigQuery.
// Links carry a faint dotted underline and a hover hint so they can be found;
// holding the modifier turns them into solid, clickable links.

const PART = '(?:`([^`\\n]+)`|"([^"\\n]+)"|([A-Za-z_][A-Za-z0-9_]*))'
const REF = new RegExp(`${PART}(?:\\s*\\.\\s*${PART})?`, 'g')
// Bare names are only links right after a keyword that takes a table or database.
const TABLE_CTX = /\b(?:FROM|JOIN|INTO|TABLE|UPDATE|DESCRIBE|DESC|EXISTS)\s+$/i
const DB_CTX = /\b(?:USE|DATABASE)\s+$/i

const refresh = StateEffect.define<null>()

/** Re-resolve links, e.g. after the schema store loaded. */
export function refreshSchemaLinks(view: EditorView): void {
  view.dispatch({ effects: refresh.of(null) })
}

function partLength(quoted1: string | undefined, quoted2: string | undefined, bare: string | undefined): number {
  if (quoted1 !== undefined) return quoted1.length + 2
  if (quoted2 !== undefined) return quoted2.length + 2
  return bare?.length ?? 0
}

function dbLink(name: string) {
  return Decoration.mark({ class: 'cm-schema-link', attributes: { 'data-db': name } })
}

function tableLink(db: string, table: string) {
  return Decoration.mark({ class: 'cm-schema-link', attributes: { 'data-db': db, 'data-table': table } })
}

function isModifier(e: MouseEvent | KeyboardEvent): boolean {
  return e.metaKey || e.ctrlKey
}

export const schemaLinks = ViewPlugin.fromClass(
  class {
    decorations: DecorationSet
    loading = new Set<string>()
    destroyed = false
    onKey: (e: KeyboardEvent) => void
    onBlur: () => void

    constructor(readonly view: EditorView) {
      this.decorations = this.build()
      this.onKey = (e) => this.setModDown(isModifier(e))
      this.onBlur = () => this.setModDown(false)
      window.addEventListener('keydown', this.onKey)
      window.addEventListener('keyup', this.onKey)
      window.addEventListener('blur', this.onBlur)
      if (getDatabases().length === 0) {
        ensureDatabasesLoaded().then(() => this.rerun())
      }
    }

    update(u: ViewUpdate) {
      if (u.docChanged || u.viewportChanged || u.transactions.some((tr) => tr.effects.some((e) => e.is(refresh)))) {
        this.decorations = this.build()
      }
    }

    destroy() {
      this.destroyed = true
      window.removeEventListener('keydown', this.onKey)
      window.removeEventListener('keyup', this.onKey)
      window.removeEventListener('blur', this.onBlur)
    }

    setModDown(down: boolean) {
      this.view.dom.classList.toggle('cm-mod-down', down)
    }

    rerun() {
      if (!this.destroyed) refreshSchemaLinks(this.view)
    }

    loadTables(dbs: string[]) {
      for (const db of dbs) {
        if (hasTablesCached(db) || this.loading.has(db)) continue
        this.loading.add(db)
        ensureTablesCached(db).finally(() => {
          this.loading.delete(db)
          this.rerun()
        })
      }
    }

    build(): DecorationSet {
      const builder = new RangeSetBuilder<Decoration>()
      const dbNames = new Set(getDatabases().map((d) => d.name))
      if (dbNames.size === 0) return builder.finish()

      const { state } = this.view
      const tree = syntaxTree(state)
      let needAllTables = false

      for (const { from, to } of this.view.visibleRanges) {
        const start = state.doc.lineAt(from).from
        const text = state.sliceDoc(start, state.doc.lineAt(to).to)
        REF.lastIndex = 0
        let m: RegExpExecArray | null
        while ((m = REF.exec(text)) !== null) {
          const pos = start + m.index
          const node = tree.resolveInner(pos, 1).name
          if (node.includes('String') || node.includes('Comment')) continue

          const first = m[1] ?? m[2] ?? m[3]
          const second = m[4] ?? m[5] ?? m[6]
          const firstLen = partLength(m[1], m[2], m[3])

          if (second !== undefined) {
            if (!dbNames.has(first)) continue
            if (!hasTablesCached(first)) {
              this.loadTables([first])
              continue
            }
            if (!findTablesForDatabase(first).includes(second)) continue
            const end = pos + m[0].length
            builder.add(pos, pos + firstLen, dbLink(first))
            builder.add(end - partLength(m[4], m[5], m[6]), end, tableLink(first, second))
            continue
          }

          const before = state.sliceDoc(Math.max(0, pos - 40), pos)
          if (DB_CTX.test(before)) {
            if (dbNames.has(first)) builder.add(pos, pos + firstLen, dbLink(first))
          } else if (TABLE_CTX.test(before)) {
            const owners = [...dbNames].filter((db) => hasTablesCached(db) && findTablesForDatabase(db).includes(first))
            if ([...dbNames].some((db) => !hasTablesCached(db))) needAllTables = true
            // Ambiguous or unknown bare names stay plain text.
            else if (owners.length === 1) builder.add(pos, pos + firstLen, tableLink(owners[0], first))
          }
        }
      }

      if (needAllTables) this.loadTables([...dbNames])
      return builder.finish()
    }
  },
  {
    decorations: (v) => v.decorations,
    eventHandlers: {
      mousemove(e) {
        this.setModDown(isModifier(e))
      },
      mouseleave() {
        this.setModDown(false)
      },
      mousedown(e) {
        if (e.button !== 0 || !isModifier(e)) return false
        const link = (e.target as HTMLElement).closest<HTMLElement>('.cm-schema-link')
        const db = link?.dataset.db
        if (!db) return false
        e.preventDefault()
        const table = link.dataset.table
        if (table) openTableTab(db, table)
        else openDatabaseTab(db)
        return true
      },
    },
  },
)

const modLabel = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform) ? '⌘' : 'Ctrl'

// Hover hint: says what the name is and how to open it.
export const schemaLinkHint = hoverTooltip(
  (view, pos) => {
    const plugin = view.plugin(schemaLinks)
    if (!plugin) return null
    let hit: { from: number; to: number; db: string; table?: string } | null = null
    plugin.decorations.between(pos, pos, (from, to, deco) => {
      const attrs = deco.spec.attributes as Record<string, string> | undefined
      if (attrs?.['data-db']) hit = { from, to, db: attrs['data-db'], table: attrs['data-table'] }
    })
    if (!hit) return null
    const { from, to, db, table } = hit
    return {
      pos: from,
      end: to,
      above: true,
      create() {
        const dom = document.createElement('div')
        dom.className = 'cm-schema-tip'
        const name = document.createElement('span')
        name.className = 'cm-schema-tip-name'
        name.textContent = table ? `${db}.${table}` : db
        const hint = document.createElement('span')
        hint.className = 'cm-schema-tip-hint'
        hint.textContent = `${modLabel} click to open ${table ? 'table' : 'database'}`
        dom.append(name, hint)
        return { dom }
      },
    }
  },
  { hoverTime: 350 },
)

export const schemaLinkTheme = EditorView.baseTheme({
  '.cm-schema-link': {
    textDecoration: 'underline dotted',
    textDecorationColor: 'var(--fg-4)',
    textUnderlineOffset: '3px',
  },
  '&.cm-mod-down .cm-schema-link': {
    textDecorationStyle: 'solid',
    cursor: 'pointer',
  },
  '&.cm-mod-down .cm-schema-link:hover': {
    textDecorationColor: 'var(--accent)',
    color: 'var(--accent)',
  },
  '.cm-tooltip.cm-tooltip-hover:has(.cm-schema-tip)': {
    backgroundColor: 'var(--elevated)',
    border: '1px solid var(--edge)',
    borderRadius: '6px',
  },
  '.cm-schema-tip': {
    display: 'flex',
    gap: '8px',
    alignItems: 'baseline',
    padding: '4px 8px',
    fontFamily: 'var(--font-sans)',
    fontSize: '12px',
  },
  '.cm-schema-tip-name': { color: 'var(--fg)', fontFamily: 'var(--font-mono)' },
  '.cm-schema-tip-hint': { color: 'var(--fg-3)' },
})
