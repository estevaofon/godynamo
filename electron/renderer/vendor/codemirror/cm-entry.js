import { EditorState, StateField, StateEffect } from "@codemirror/state"
import { EditorView, keymap, lineNumbers, highlightActiveLine, Decoration } from "@codemirror/view"
import { defaultKeymap, history, historyKeymap, indentWithTab } from "@codemirror/commands"
import { json } from "@codemirror/lang-json"
import {
  syntaxHighlighting, HighlightStyle, bracketMatching,
  indentOnInput, foldGutter, codeFolding, foldKeymap,
} from "@codemirror/language"
import { tags as t } from "@lezer/highlight"

// Tokyo Night — matches electron/renderer/styles.css.
const tokyoTheme = EditorView.theme({
  "&": { color: "#c8d3f5", backgroundColor: "#0b0f1a", height: "100%" },
  ".cm-content": { fontFamily: "'Cascadia Code', monospace", fontSize: "12px", caretColor: "#c8d3f5" },
  ".cm-scroller": { fontFamily: "'Cascadia Code', monospace" },
  ".cm-cursor, .cm-dropCursor": { borderLeftColor: "#c8d3f5" },
  "&.cm-focused .cm-selectionBackground, .cm-selectionBackground, ::selection": { backgroundColor: "#2a3450" },
  ".cm-gutters": { backgroundColor: "#131a2b", color: "#828bb8", border: "none" },
  ".cm-activeLine": { backgroundColor: "rgba(19,26,43,0.45)" },
  ".cm-activeLineGutter": { backgroundColor: "#131a2b" },
  ".cm-foldGutter .cm-gutterElement": { cursor: "pointer", color: "#7aa2f7", padding: "0 4px" },
  ".cm-foldPlaceholder": {
    backgroundColor: "#2a3450", color: "#c8d3f5", border: "none",
    borderRadius: "4px", padding: "0 4px", margin: "0 2px",
  },
  ".cm-find": { backgroundColor: "#e0af68", color: "#0b0f1a" },
}, { dark: true })

const tokyoHighlight = HighlightStyle.define([
  { tag: t.propertyName, color: "#7aa2f7" },                       // JSON keys
  { tag: t.string, color: "#9ece6a" },
  { tag: t.number, color: "#ff9e64" },
  { tag: [t.bool, t.null], color: "#bb9af7" },
  { tag: [t.separator, t.punctuation, t.brace, t.squareBracket], color: "#828bb8" },
])

const base = [
  json(),
  syntaxHighlighting(tokyoHighlight),
  bracketMatching(),
  codeFolding(),
  foldGutter(),
  keymap.of(foldKeymap),
  tokyoTheme,
]

export function createEditor({ parent, doc = "" }) {
  const view = new EditorView({
    parent,
    state: EditorState.create({
      doc,
      extensions: [
        lineNumbers(),
        highlightActiveLine(),
        history(),
        indentOnInput(),
        keymap.of([...defaultKeymap, ...historyKeymap, indentWithTab]),
        ...base,
      ],
    }),
  })
  return {
    getValue: () => view.state.doc.toString(),
    setValue: (s) => view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: s } }),
    focus: () => view.focus(),
  }
}

// Find marks for the read-only viewer, supplied externally (offsets from app.js).
const setFindMarks = StateEffect.define()
const findField = StateField.define({
  create: () => Decoration.none,
  update(deco, tr) {
    deco = deco.map(tr.changes)
    for (const e of tr.effects) if (e.is(setFindMarks)) deco = e.value
    return deco
  },
  provide: (f) => EditorView.decorations.from(f),
})
const findMark = Decoration.mark({ class: "cm-find" })

export function createViewer({ parent }) {
  const view = new EditorView({
    parent,
    state: EditorState.create({
      doc: "",
      extensions: [
        EditorState.readOnly.of(true),
        EditorView.editable.of(false),
        lineNumbers(),
        findField,
        ...base,
      ],
    }),
  })
  return {
    setValue: (s) => view.dispatch({
      changes: { from: 0, to: view.state.doc.length, insert: s },
      effects: setFindMarks.of(Decoration.none),
    }),
    setMatches: (ranges) => {
      const deco = ranges && ranges.length
        ? Decoration.set(ranges.map((r) => findMark.range(r.from, r.to)), true)
        : Decoration.none
      view.dispatch({ effects: setFindMarks.of(deco) })
    },
    focus: () => view.focus(),
  }
}
