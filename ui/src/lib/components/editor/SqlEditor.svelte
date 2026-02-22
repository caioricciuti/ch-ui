<script lang="ts">
  import { onMount, onDestroy } from 'svelte'
  import { EditorView, keymap } from '@codemirror/view'
  import { EditorState, Compartment } from '@codemirror/state'
  import { sql, StandardSQL } from '@codemirror/lang-sql'
  import { autocompletion } from '@codemirror/autocomplete'
  import { defaultKeymap, history, historyKeymap } from '@codemirror/commands'
  import { bracketMatching, HighlightStyle, syntaxHighlighting } from '@codemirror/language'
  import { highlightSelectionMatches, searchKeymap } from '@codemirror/search'
  import { closeBrackets, closeBracketsKeymap } from '@codemirror/autocomplete'
  import { tags as t } from '@lezer/highlight'
  import { clickhouseCompletionSource } from '../../editor/completions'
  import { getTheme } from '../../stores/theme.svelte'

  interface Props {
    value?: string
    onrun?: (sql: string) => void
    onchange?: (sql: string) => void
  }

  let { value = '', onrun, onchange }: Props = $props()

  let container: HTMLDivElement
  let view: EditorView | undefined
  const themeCompartment = new Compartment()

  const lightTheme = EditorView.theme({
    '&': { backgroundColor: 'rgba(255,255,255,0.94)' },
    '.cm-gutters': { backgroundColor: '#f4f4f5', borderRight: '1px solid #d4d4d8' },
    '.cm-activeLineGutter': { backgroundColor: '#ececef' },
    '.cm-activeLine': { backgroundColor: '#f3f4f6' },
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
    '&': { backgroundColor: 'rgba(22,23,28,0.96)', color: '#f3f4f6' },
    '.cm-gutters': { backgroundColor: 'rgba(28,30,36,0.96)', borderRight: '1px solid #42454f', color: '#a5a8b2' },
    '.cm-activeLine': { backgroundColor: 'rgba(249,115,22,0.12)' },
    '.cm-activeLineGutter': { backgroundColor: 'rgba(249,115,22,0.18)' },
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

  const runKeyBinding = keymap.of([
    {
      key: 'Mod-Enter',
      run: (v) => {
        const main = v.state.selection.main
        const selected = v.state.sliceDoc(main.from, main.to).trim()
        onrun?.(selected || v.state.doc.toString())
        return true
      },
    },
  ])

  // Bun can install a second @codemirror/view copy under transitive deps;
  // normalize keymaps to one array type for TS while runtime dedupe is handled by Vite.
  const editorKeymaps = [...defaultKeymap, ...historyKeymap, ...searchKeymap, ...closeBracketsKeymap] as any

  onMount(() => {
    const state = EditorState.create({
      doc: value,
      extensions: [
        runKeyBinding,
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
        themeCompartment.of(getThemeExtension()),
        EditorView.theme({
          '&': { height: '100%', fontSize: '13px' },
          '.cm-scroller': { overflow: 'auto', fontFamily: 'var(--font-mono)' },
        }),
        EditorView.updateListener.of((update) => {
          if (update.docChanged) {
            onchange?.(update.state.doc.toString())
          }
        }),
        EditorView.lineWrapping,
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
</script>

<div bind:this={container} class="h-full w-full"></div>
