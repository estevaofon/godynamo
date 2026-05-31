# GoDynamo — JSON Syntax Highlighting & Collapsible Brackets (GUI) Design Spec

- **Date:** 2026-05-31
- **Status:** Approved (design); ready for implementation planning.
- **Builds on:** the Electron desktop GUI renderer (`electron/renderer/{index.html,app.js,styles.css}`), on `develop`. Branch: `feat/json-highlight-fold`.
- **Scope:** in the **Electron GUI only**, give DynamoDB records JSON **syntax highlighting** and **collapsible `{…}` / `[…]` blocks that work while editing** — so a sub-tree can be folded, selected, and deleted as one unit (the DynamoBase workflow). Applies to **both** the read-only item detail viewer and the edit modal. Delivered by replacing the plain `<pre>` / `<textarea>` with **CodeMirror 6**, vendored as a single locally-built bundle. No Go/backend, TUI, export, or copy changes.

## 1. Goal

Today the renderer shows items two ways, both plain text:

- **Detail viewer** — `<pre id="detail-body">` holding `JSON.stringify(item, null, 2)`, with a find box (`#detail-search`) that `<mark>`-highlights literal, case-insensitive matches and counts them (`app.js:632-659`).
- **Edit modal** — `<textarea id="editor-text">`, validated with `JSON.parse` on save (`app.js:754-773`).

A `<textarea>` cannot syntax-highlight or fold, so the collapsible-while-editing requirement forces a real editor component. We adopt **CodeMirror 6** for both surfaces: colorized JSON, a fold gutter, and folded ranges that remain part of the document so **fold → select → delete** removes the whole sub-tree. The existing find/match-count UX and save-time validation are preserved.

## 2. Decisions locked during brainstorming

| Decision | Choice |
|---|---|
| Editor component | **CodeMirror 6** (MIT). Not CM5 (the user asked for the current major) and not Monaco (too heavy / AMD-awkward for a no-build app). |
| Distribution | **Vendored, locally built.** CM6 is ESM-only across `@codemirror/*` packages and is not drop-in; we bundle the needed packages **once** with esbuild into a single committed file. The app stays no-build at runtime. |
| Bundle format | **IIFE exposing `window.CM`** (`esbuild --format=iife --global-name=CM`), loaded as a classic local `<script>` before `app.js`. Keeps `app.js` a classic script (no module conversion). |
| Surfaces | **Both** the edit modal and the read-only detail viewer get highlighting + folding (matches DynamoBase). |
| Theme | **Tokyo Night**, matching the existing palette, defined inside the bundle as a CM `theme` + `HighlightStyle`. |
| Find/match-count | **Preserved.** `app.js` computes match ranges with a pure `findMatches(text, term)`; the viewer only renders them via `setMatches(ranges)`. Same literal, case-insensitive semantics and `#detail-matches` count. |
| Save validation | **Unchanged.** `JSON.parse` on save → `#editor-error`. No inline linting (out of scope). |
| Robustness | **Graceful fallback.** If `window.CM` is absent (bundle missing), `app.js` falls back to the original `<textarea>` / `<pre>` behavior so the GUI never breaks. |
| CSP | **No change.** Script is local (`script-src 'self'`); CM6 injects styles as `<style>` elements, already allowed by `style-src 'self' 'unsafe-inline'`. No `eval`, no network. |
| Verification | **No real AWS** (these are pure front-end; AWS is never touched). Pure helpers unit-tested with `node:test`; existing `go test ./...` stays green; CM integration verified by a manual smoke checklist. |

## 3. Vendoring layer (`electron/renderer/vendor/codemirror/`, new)

| File | Role | Committed |
|---|---|---|
| `cm-entry.js` | Entry module: imports only the `@codemirror/*` symbols we use and exports two factories (`createEditor`, `createViewer`) plus the shared theme/highlight. The **only** place that knows CM6. | ✅ (readable, auditable) |
| `package.json` | **Dev-only**: pinned devDependencies + `build` script. Not shipped with the app. | ✅ |
| `codemirror.bundle.js` | esbuild output (IIFE, minified, deps inlined) — the **only** file the app loads. | ✅ |
| `LICENSE` | CodeMirror MIT license text. | ✅ |
| `README.md` | "Run `npm install && npm run build` to regenerate `codemirror.bundle.js`." | ✅ |

`package.json` (illustrative):

```json
{
  "name": "godynamo-cm-vendor",
  "private": true,
  "scripts": {
    "build": "esbuild cm-entry.js --bundle --minify --format=iife --global-name=CM --outfile=codemirror.bundle.js"
  },
  "devDependencies": {
    "@codemirror/state": "^6",
    "@codemirror/view": "^6",
    "@codemirror/commands": "^6",
    "@codemirror/language": "^6",
    "@codemirror/lang-json": "^6",
    "@lezer/highlight": "^1",
    "esbuild": "^0.x"
  }
}
```

`.gitignore`: add `electron/renderer/vendor/codemirror/node_modules/` (keep `codemirror.bundle.js` tracked).

## 4. Renderer interface (`window.CM`)

`app.js` never imports CM6 symbols — it talks only to two factories. All CM6 complexity (theme, `lang-json`, `codeFolding`/`foldGutter`/`foldKeymap`, `syntaxHighlighting`, decorations) is encapsulated in `cm-entry.js`.

```js
// editable JSON: highlight + folding + history + bracket matching/closing
CM.createEditor({ parent, doc })
  -> { getValue(): string, setValue(s: string): void, focus(): void }

// read-only JSON: highlight + folding + externally-supplied find marks
CM.createViewer({ parent })
  -> { setValue(s: string): void, setMatches(ranges: {from,to}[]): void, focus(): void }
```

`cm-entry.js` (illustrative shape):

```js
import { EditorState } from "@codemirror/state"
import { EditorView, keymap, lineNumbers, highlightActiveLine, Decoration } from "@codemirror/view"
import { defaultKeymap, history, historyKeymap, indentWithTab } from "@codemirror/commands"
import { json } from "@codemirror/lang-json"
import {
  syntaxHighlighting, HighlightStyle, bracketMatching,
  indentOnInput, foldGutter, codeFolding, foldKeymap,
} from "@codemirror/language"
import { tags as t } from "@lezer/highlight"

// Tokyo Night — matches electron/renderer/styles.css
const tokyoTheme = EditorView.theme({ /* bg #0b0f1a, fg #c8d3f5, gutter, .cm-find {bg #e0af68; color #0b0f1a} ... */ }, { dark: true })
const tokyoHighlight = HighlightStyle.define([
  { tag: t.propertyName, color: "#7aa2f7" },               // keys
  { tag: t.string,       color: "#9ece6a" },               // strings
  { tag: t.number,       color: "#ff9e64" },               // numbers
  { tag: [t.bool, t.null], color: "#bb9af7" },             // true/false/null
  { tag: [t.separator, t.punctuation, t.brace, t.squareBracket], color: "#828bb8" },
])

const folding = [codeFolding(), foldGutter(), keymap.of(foldKeymap)]
const base = [json(), syntaxHighlighting(tokyoHighlight), bracketMatching(), tokyoTheme, folding]

export function createEditor({ parent, doc }) {
  const view = new EditorView({
    parent,
    state: EditorState.create({
      doc,
      extensions: [
        lineNumbers(), highlightActiveLine(), history(), indentOnInput(),
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

// viewer: read-only; a StateField holds find-mark decorations updated via a StateEffect.
export function createViewer({ parent }) { /* EditorState.readOnly + EditorView.editable=false + base + findField */ }
```

**Find marks** live in a `StateField<DecorationSet>` updated by a `StateEffect`; `setMatches(ranges)` dispatches the effect with `Decoration.mark({ class: "cm-find" })` over each range; `[]` clears. Counting/scanning is **not** done here — `app.js` supplies the ranges.

## 5. Editor integration (`app.js`, `index.html`, `styles.css`)

- **`index.html`**: replace `<textarea id="editor-text" …>` with `<div id="editor-cm"></div>`; add `<script src="vendor/codemirror/codemirror.bundle.js"></script>` **before** `<script src="app.js">`.
- **`app.js`** — a lazily-created singleton:

```js
let cmEditor = null
function getEditor() {
  if (!cmEditor && window.CM) cmEditor = window.CM.createEditor({ parent: $('editor-cm'), doc: '' })
  return cmEditor
}
function setEditorValue(s) {
  const ed = getEditor()
  if (ed) ed.setValue(s); else $('editor-cm').textContent = s  // fallback handled in §7
}
function getEditorValue() {
  const ed = getEditor()
  return ed ? ed.getValue() : /* fallback textarea */ ''
}
```

  - `openNewItem` (`app.js:733`): `setEditorValue(JSON.stringify(buildItemTemplate(state.keys), null, 2))`, show, focus.
  - `openEditItem` (`app.js:744`): `setEditorValue(JSON.stringify(state.selectedItem, null, 2))`, show, focus.
  - `saveEditor` (`app.js:754`): `const text = getEditorValue()` → unchanged `JSON.parse` validation and `window.api.saveItem`.
  - The singleton is created on first open (container visible), avoiding hidden-container measurement issues; `focus()` on show.

**Editing UX (the requested feature):** the fold gutter marks every foldable object/array (from `lang-json`'s fold info). Clicking the marker, or `Ctrl-Shift-[` / `Ctrl-Shift-]`, collapses/expands. A folded block renders as a `{…}` placeholder **over the real text**, so selecting its line (Home → Shift-End) and pressing Delete removes the entire sub-tree — minimize → select → delete.

## 6. Viewer integration (`app.js`, `index.html`)

- **`index.html`**: replace `<pre id="detail-body"></pre>` with `<div id="detail-body"></div>`.
- **`app.js`** — singleton viewer + pure match logic:

```js
let cmViewer = null
function getViewer() {
  if (!cmViewer && window.CM) cmViewer = window.CM.createViewer({ parent: $('detail-body') })
  return cmViewer
}

// pure, DOM-free, unit-tested — same semantics as today's loop
function findMatches(text, term) {
  if (!term) return []
  const lower = text.toLowerCase(), tl = term.toLowerCase(), out = []
  let i = 0
  while ((i = lower.indexOf(tl, i)) !== -1) { out.push({ from: i, to: i + tl.length }); i += tl.length || 1 }
  return out
}
```

  - `openDetail` (`app.js:661`): set `state.detailText`, title, clear `#detail-search`; `getViewer().setValue(text)`; show. Folding available here too.
  - `renderDetailBody` (`app.js:632`) becomes: `const m = findMatches(state.detailText, term); v.setMatches(m); $('detail-matches').textContent = term ? (m.length + (m.length === 1 ? ' match' : ' matches')) : ''`.
  - `copyDetail` (`app.js:687`): unchanged — still copies `state.detailText`.
  - `showItem` / `showSchema`: unchanged callers of `openDetail`.

## 7. Error handling & graceful fallback

- **Bundle missing / `window.CM` undefined:** `getEditor`/`getViewer` return `null`. Fallback path renders a plain `<textarea>` (editor) / `<pre>` text (viewer) inside the same containers, and `renderDetailBody` falls back to the current `<mark>` overlay. The GUI degrades but never breaks. Because the bundle is committed, this is a safety net, not the normal path.
- **Invalid JSON on save:** unchanged (`JSON.parse` → `#editor-error`).
- **Large items:** CM6 handles large docs; folding helps. No special handling.
- **Find:** literal (non-regex), case-insensitive, identical to today; empty term clears marks and the count label.

## 8. Scope / non-goals (YAGNI)

- No TUI changes; no Go/backend changes; no export/CSV/copy changes.
- No inline JSON linting/diagnostics, no attribute autocomplete (possible follow-ups).
- No new runtime dependency for the app itself — only dev-time devDependencies under `vendor/`, producing one committed file.

## 9. Theme (Tokyo Night)

`HighlightStyle` mapping (matching `styles.css`): keys `#7aa2f7`, strings `#9ece6a`, numbers `#ff9e64`, `true`/`false`/`null` `#bb9af7`, punctuation/braces/brackets `#828bb8`. Editor chrome themed to bg `#0b0f1a`, fg `#c8d3f5`, gutter `#131a2b`/`#828bb8`, active line subtle `#131a2b`, find mark `#e0af68` on `#0b0f1a`, font `'Cascadia Code', monospace` 12px — consistent with the existing `#detail-body` / `#editor-text` styles. A few `.cm-editor` sizing rules added to `styles.css` (min-height in the modal, fill the detail card, scroll).

## 10. Testing

Proportional to the repo's conventions (Go stdlib suite + CI; **AWS always mocked — irrelevant here, no AWS is touched**):

- **`electron/renderer/helpers.js` (new) + `node:test`:** extract the two pure, DOM-free helpers — `findMatches(text, term)` and `buildItemTemplate(keys)` — and unit-test them (zero deps, `node --test`). `app.js` uses them via a `window`-guarded include so it stays a classic script.
  - `findMatches`: no term → `[]`; single/multiple matches return correct `{from,to}` ranges; case-insensitive; overlapping/empty-term guarded; count = `ranges.length` matches the old behavior.
  - `buildItemTemplate`: partition-only, partition+sort, and no-keys produce the expected template objects.
- **Go suite unchanged:** `go vet ./...` and `go test ./...` stay green (renderer JS is not exercised by Go tests).
- **Manual smoke (Estevão), documented checklist:** open editor → JSON is colorized; fold/unfold an object and an array (gutter + `Ctrl-Shift-[`); fold a sub-object, select its line, Delete → whole sub-tree removed; save valid JSON → persists; save invalid JSON → `#editor-error`; open item detail → colorized + foldable; type in find box → matches highlighted and counted, clearing on empty; Copy still copies full item; remove the bundle → GUI still works via fallback.
- **Out of scope:** no jsdom/Playwright/Electron E2E harness is introduced for the CM integration (YAGNI) unless requested later.

## 11. Files touched

| File | Change |
|---|---|
| `electron/renderer/vendor/codemirror/cm-entry.js` | **new** — CM6 entry: `createEditor`, `createViewer`, theme, folding, find decorations. |
| `electron/renderer/vendor/codemirror/package.json` | **new** — dev-only deps + esbuild `build` script. |
| `electron/renderer/vendor/codemirror/codemirror.bundle.js` | **new (generated, committed)** — IIFE bundle exposing `window.CM`. |
| `electron/renderer/vendor/codemirror/LICENSE` | **new** — CodeMirror MIT license. |
| `electron/renderer/vendor/codemirror/README.md` | **new** — regenerate instructions. |
| `electron/renderer/index.html` | `<textarea>`→`<div id="editor-cm">`, `<pre>`→`<div id="detail-body">`, add bundle `<script>`. |
| `electron/renderer/app.js` | editor/viewer singletons + glue; `findMatches`/`buildItemTemplate`; fallback guards. |
| `electron/renderer/helpers.js` | **new** — pure `findMatches`, `buildItemTemplate` (shared by `app.js`, tested by `node:test`). |
| `electron/renderer/helpers.test.js` | **new** — `node:test` for the pure helpers. |
| `electron/renderer/styles.css` | add `.cm-editor` sizing within the modal/detail card; keep the existing `#editor-text`/`#detail-body mark` rules (now used only by the fallback path). |
| `.gitignore` | ignore `electron/renderer/vendor/codemirror/node_modules/`. |

## 12. Verification

- `npm install && npm run build` in `vendor/codemirror/` produces a non-empty `codemirror.bundle.js` defining `window.CM` with `createEditor`/`createViewer`.
- `node --test electron/renderer/` passes (pure helpers).
- `go vet ./...` and `go test ./...` stay green.
- Manual (Estevão): the §10 smoke checklist in the running GUI.
