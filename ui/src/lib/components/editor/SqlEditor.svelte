<script lang="ts">
  import { onMount, onDestroy } from 'svelte'
  import { EditorView, keymap, Decoration, ViewPlugin, type DecorationSet, type KeyBinding, type ViewUpdate } from '@codemirror/view'
  import { EditorState, Compartment, RangeSetBuilder } from '@codemirror/state'
  import { sql, StandardSQL } from '@codemirror/lang-sql'
  import { autocompletion } from '@codemirror/autocomplete'
  import { defaultKeymap, history, historyKeymap, indentWithTab, isolateHistory } from '@codemirror/commands'
  import { bracketMatching, HighlightStyle, syntaxHighlighting } from '@codemirror/language'
  import { highlightSelectionMatches, searchKeymap } from '@codemirror/search'
  import { closeBrackets, closeBracketsKeymap } from '@codemirror/autocomplete'
  import { tags as t } from '@lezer/highlight'
  import { clickhouseCompletionSource } from '../../editor/completions'
  import { splitStatements, statementAt } from '../../editor/statements'
  import { schemaLinks, schemaLinkHint, schemaLinkTheme } from '../../editor/schemaLinks'
  import { getTheme } from '../../stores/theme.svelte'

  interface Props {
    value?: string
    /** sql is trimmed; from is its offset in the document (for error mapping). */
    onrun?: (sql: string, from?: number) => void
    /** Shift-Mod-Enter: every statement in the document (or the selection), in order. */
    onrunall?: (statements: { text: string; from: number }[]) => void
    onchange?: (sql: string) => void
    /** Mod-S. The browser Save Page dialog is suppressed while the editor has focus. */
    onsave?: () => void
    /** Shift-Alt-F. */
    onformat?: () => void
    /** Escape or Mod-. while running is true. */
    oncancel?: () => void
    running?: boolean
  }

  let { value = '', onrun, onrunall, onchange, onsave, onformat, oncancel, running = false }: Props = $props()

  let container: HTMLDivElement
  let view: EditorView | undefined
  const themeCompartment = new Compartment()

  const lightTheme = EditorView.theme({
    '&': { backgroundColor: 'var(--surface)' },
    '.cm-gutters': { backgroundColor: 'var(--surface)', borderRight: '1px solid var(--edge-subtle)', color: 'var(--fg-4)' },
    '.cm-activeLineGutter': { backgroundColor: 'var(--hover)' },
    '.cm-activeLine': { backgroundColor: 'var(--hover)' },
    '.cm-selectionBackground': { backgroundColor: '#fed7aa !important' },
    '&.cm-focused .cm-selectionBackground': { backgroundColor: '#fdba74 !important' },
    '.cm-cursor': { borderLeftColor: '#1f2126' },
    '.cm-matchingBracket': { backgroundColor: '#ffedd5', outline: '1px solid #f97316' },
    '.cm-tooltip-autocomplete': {
      border: '1px solid #d4d4d8',
      borderRadius: '10px',
      backgroundColor: '#ffffff',
      boxShadow: '0 14px 30px rgba(0,0,0,0.12)',
      overflow: 'hidden',
    },
    '.cm-tooltip-autocomplete > ul': {
      maxHeight: '320px',
      fontFamily: 'var(--font-sans)',
    },
    '.cm-tooltip-autocomplete > ul > li': {
      fontSize: '12px',
      padding: '6px 10px',
    },
    '.cm-tooltip-autocomplete > ul > li[aria-selected]': {
      backgroundColor: 'rgba(249,115,22,0.16)',
      color: '#1f2126',
    },
  }, { dark: false })

  const darkTheme = EditorView.theme({
    '&': { backgroundColor: 'var(--canvas)', color: 'var(--fg)' },
    '.cm-gutters': { backgroundColor: 'var(--canvas)', borderRight: '1px solid var(--edge-subtle)', color: 'var(--fg-4)' },
    '.cm-activeLine': { backgroundColor: 'var(--hover)' },
    '.cm-activeLineGutter': { backgroundColor: 'var(--hover)' },
    '.cm-selectionBackground': { backgroundColor: 'rgba(251,146,60,0.32) !important' },
    '.cm-matchingBracket': { backgroundColor: 'rgba(249,115,22,0.22)', outline: '1px solid rgba(251,146,60,0.9)' },
    '.cm-panels': { backgroundColor: '#18191f', color: '#fed7aa' },
    '.cm-cursor': { borderLeftColor: '#f3f4f6' },
    '.cm-tooltip-autocomplete': {
      border: '1px solid #3f434c',
      borderRadius: '10px',
      backgroundColor: '#1b1d22',
      boxShadow: '0 16px 38px rgba(0,0,0,0.5)',
      overflow: 'hidden',
    },
    '.cm-tooltip-autocomplete > ul': {
      maxHeight: '320px',
      fontFamily: 'var(--font-sans)',
    },
    '.cm-tooltip-autocomplete > ul > li': {
      fontSize: '12px',
      padding: '6px 10px',
    },
    '.cm-tooltip-autocomplete > ul > li[aria-selected]': {
      backgroundColor: 'rgba(249,115,22,0.22)',
      color: '#f8fafc',
    },
  }, { dark: true })

  const lightHighlight = HighlightStyle.define([
    { tag: [t.keyword, t.operatorKeyword, t.controlKeyword, t.definitionKeyword, t.moduleKeyword], color: '#c2410c', fontWeight: '600' },
    { tag: [t.function(t.variableName), t.function(t.propertyName)], color: '#ea580c' },
    { tag: [t.variableName, t.definition(t.variableName), t.definition(t.name), t.special(t.variableName)], color: '#27272a' },
    { tag: [t.propertyName], color: '#166534' },
    { tag: [t.typeName, t.className], color: '#374151', fontWeight: '500' },
    { tag: [t.string, t.special(t.string)], color: '#15803d' },
    { tag: [t.number, t.integer, t.float, t.atom], color: '#b45309' },
    { tag: [t.bool, t.null], color: '#a16207', fontWeight: '600' },
    { tag: [t.comment], color: '#71717a', fontStyle: 'italic' },
    { tag: [t.operator, t.punctuation, t.separator, t.bracket], color: '#52525b' },
    { tag: t.invalid, color: '#b91c1c', textDecoration: 'underline wavy' },
  ])

  const darkHighlight = HighlightStyle.define([
    { tag: [t.keyword, t.operatorKeyword, t.controlKeyword, t.definitionKeyword, t.moduleKeyword], color: '#fb923c', fontWeight: '600' },
    { tag: [t.function(t.variableName), t.function(t.propertyName)], color: '#fdba74' },
    { tag: [t.variableName, t.definition(t.variableName), t.definition(t.name), t.special(t.variableName)], color: '#f4f4f5' },
    { tag: [t.propertyName], color: '#86efac' },
    { tag: [t.typeName, t.className], color: '#d4d4d8', fontWeight: '500' },
    { tag: [t.string, t.special(t.string)], color: '#4ade80' },
    { tag: [t.number, t.integer, t.float, t.atom], color: '#fbbf24' },
    { tag: [t.bool, t.null], color: '#f59e0b', fontWeight: '600' },
    { tag: [t.comment], color: '#9ca3af', fontStyle: 'italic' },
    { tag: [t.operator, t.punctuation, t.separator, t.bracket], color: '#d4d4d8' },
    { tag: t.invalid, color: '#f87171', textDecoration: 'underline wavy' },
  ])

  function getThemeExtension() {
    return getTheme() === 'dark'
      ? [darkTheme, syntaxHighlighting(darkHighlight)]
      : [lightTheme, syntaxHighlighting(lightHighlight)]
  }

  // Statement detection is skipped on very large documents (pasted dumps).
  const MAX_SPLIT_CHARS = 500_000

  // The backend trims the SQL before execution, so error positions index the
  // trimmed text — return trimmed text plus its document offset to keep
  // jump-to-error mapping exact. With no selection and several statements in
  // the document, the statement under the cursor is the runnable one.
  function pickRunnable(state: EditorState): { text: string; from: number } {
    const main = state.selection.main
    const selected = state.sliceDoc(main.from, main.to)
    if (selected.trim()) {
      const lead = selected.length - selected.trimStart().length
      return { text: selected.trim(), from: main.from + lead }
    }
    const doc = state.doc.toString()
    const active = activeStatement(state)
    if (active) return { text: active.text, from: active.from }
    // A lone statement drops its ';' and a comment after it ("SELECT 1; -- note").
    if (doc.length <= MAX_SPLIT_CHARS) {
      const statements = splitStatements(doc)
      if (statements.length === 1) return { text: statements[0].text, from: statements[0].from }
    }
    const lead = doc.length - doc.trimStart().length
    return { text: doc.trim(), from: lead }
  }

  /** The statement under the cursor when the document holds more than one and nothing is selected. */
  function activeStatement(state: EditorState): { text: string; from: number; to: number } | null {
    const main = state.selection.main
    if (!main.empty || state.doc.length > MAX_SPLIT_CHARS) return null
    const statements = splitStatements(state.doc.toString())
    if (statements.length < 2) return null
    return statementAt(statements, main.head)
  }

  /** All statements of the selection (or the whole document), with document offsets. */
  function allStatements(state: EditorState): { text: string; from: number }[] {
    const main = state.selection.main
    const base = main.empty ? 0 : main.from
    const text = main.empty ? state.doc.toString() : state.sliceDoc(main.from, main.to)
    return splitStatements(text).map((st) => ({ text: st.text, from: base + st.from }))
  }

  // Shade the statement Mod-Enter would run, so it is clear which one that is.
  const activeStatementMark = Decoration.mark({ class: 'cm-active-statement' })
  const activeStatementHighlight = ViewPlugin.fromClass(
    class {
      decorations: DecorationSet
      constructor(view: EditorView) {
        this.decorations = this.build(view)
      }
      update(update: ViewUpdate) {
        if (update.docChanged || update.selectionSet) this.decorations = this.build(update.view)
      }
      build(view: EditorView): DecorationSet {
        const builder = new RangeSetBuilder<Decoration>()
        const active = activeStatement(view.state)
        if (active && active.to > active.from) builder.add(active.from, active.to, activeStatementMark)
        return builder.finish()
      }
    },
    { decorations: (v) => v.decorations },
  )

  // Escape and Mod-. cancel only while a query runs; otherwise they fall
  // through (Escape still closes the completion list, which binds it first).
  function cancelIfRunning(): boolean {
    if (!running || !oncancel) return false
    oncancel()
    return true
  }

  const editorBindings: readonly KeyBinding[] = [
    {
      key: 'Mod-Enter',
      run: (v) => {
        const picked = pickRunnable(v.state)
        onrun?.(picked.text, picked.from)
        return true
      },
    },
    {
      key: 'Shift-Mod-Enter',
      run: (v) => {
        if (!onrunall) return false
        const statements = allStatements(v.state)
        if (statements.length > 0) onrunall(statements)
        return true
      },
    },
    {
      key: 'Mod-s',
      // Always handled, so the browser never opens Save Page from the editor.
      run: () => {
        onsave?.()
        return true
      },
    },
    {
      key: 'Shift-Alt-f',
      run: () => {
        if (!onformat) return false
        onformat()
        return true
      },
    },
    { key: 'Escape', run: cancelIfRunning },
    { key: 'Mod-.', run: cancelIfRunning },
  ]

  const editorKeymaps: readonly KeyBinding[] = [
    ...defaultKeymap,
    ...historyKeymap,
    ...searchKeymap,
    ...closeBracketsKeymap,
    indentWithTab,
  ]

  onMount(() => {
    const state = EditorState.create({
      doc: value,
      extensions: [
        keymap.of(editorBindings),
        keymap.of(editorKeymaps),
        history(),
        bracketMatching(),
        closeBrackets(),
        highlightSelectionMatches(),
        sql({ dialect: StandardSQL }),
        autocompletion({
          override: [clickhouseCompletionSource],
          activateOnTyping: true,
          maxRenderedOptions: 300,
          defaultKeymap: true,
          selectOnOpen: true,
        }),
        schemaLinks,
        schemaLinkHint,
        activeStatementHighlight,
        schemaLinkTheme,
        themeCompartment.of(getThemeExtension()),
        EditorView.theme({
          '&': { height: '100%', fontSize: '13px' },
          '.cm-scroller': { overflow: 'auto', fontFamily: 'var(--font-mono)' },
          '.cm-active-statement': { backgroundColor: 'var(--accent-soft)' },
        }),
        EditorView.updateListener.of((update) => {
          if (update.docChanged) {
            onchange?.(update.state.doc.toString())
          }
        }),
        EditorView.lineWrapping,
        // SQL is not prose: no browser spell-check squiggles under identifiers
        EditorView.contentAttributes.of({ spellcheck: 'false', autocorrect: 'off', autocapitalize: 'off' }),
      ],
    })

    view = new EditorView({ state, parent: container })

    // Watch for theme changes via MutationObserver on <html> class
    const observer = new MutationObserver(() => {
      if (view) {
        view.dispatch({
          effects: themeCompartment.reconfigure(getThemeExtension()),
        })
      }
    })
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })

    return () => observer.disconnect()
  })

  onDestroy(() => {
    view?.destroy()
  })

  // The document is read from value only on mount. When value changes from
  // outside (a saved query re-opened over this tab), replace the document.
  // Changes typed here come back as the same value, so they never match.
  $effect(() => {
    const next = value
    if (view && next !== view.state.doc.toString()) setValue(next)
  })

  /** Set the editor content programmatically */
  export function setValue(text: string) {
    if (view) {
      view.dispatch({
        changes: { from: 0, to: view.state.doc.length, insert: text },
      })
    }
  }

  /** Get the current editor content */
  export function getValue(): string {
    return view?.state.doc.toString() ?? ''
  }

  /** The runnable text (selection, else the statement under the cursor when
   * there are several, else the whole document), trimmed, with its document
   * offset — needed to map server error positions into the editor. */
  export function getRunnable(): { text: string; from: number } {
    if (!view) return { text: '', from: 0 }
    return pickRunnable(view.state)
  }

  /** Replace a range (clamped to the document) as one undoable change of
   * its own, then select the new text and focus. */
  export function replaceRange(from: number, to: number, text: string) {
    if (!view) return
    const len = view.state.doc.length
    const start = Math.max(0, Math.min(from, len))
    const end = Math.max(start, Math.min(to, len))
    view.dispatch({
      changes: { from: start, to: end, insert: text },
      selection: { anchor: start, head: start + text.length },
      annotations: isolateHistory.of('full'),
      userEvent: 'input.replace',
      effects: EditorView.scrollIntoView(start, { y: 'center' }),
    })
    view.focus()
  }

  /** Select a range (clamped to the document), scroll it into view and focus. */
  export function selectRange(from: number, to: number) {
    if (!view) return
    const len = view.state.doc.length
    const anchor = Math.max(0, Math.min(from, len))
    const head = Math.max(anchor, Math.min(to, len))
    view.dispatch({
      selection: { anchor, head },
      effects: EditorView.scrollIntoView(anchor, { y: 'center' }),
    })
    view.focus()
  }
</script>

<div bind:this={container} class="h-full w-full"></div>
