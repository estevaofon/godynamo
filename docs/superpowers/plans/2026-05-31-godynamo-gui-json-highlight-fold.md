# GUI JSON Syntax Highlighting & Collapsible Brackets Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give DynamoDB records JSON syntax highlighting and collapsible `{…}`/`[…]` blocks (foldable while editing) in the GoDynamo Electron GUI's item viewer and edit modal.

**Architecture:** Vendor CodeMirror 6 as a single locally-built esbuild IIFE bundle exposing `window.CM` with two factories (`createEditor`, `createViewer`). All CM6 detail is encapsulated in `cm-entry.js`; `app.js` only talks to `window.CM` and mounts the two singletons into `#editor-cm` / `#detail-body`, replacing the old `<textarea>`/`<pre>`. Find/match-count stays in a pure, tested `findMatches`; folded blocks remain real document text so a sub-tree can be minimized, selected, and deleted as one unit. A graceful fallback to `<textarea>`/`<pre>` runs if the bundle is absent.

**Tech Stack:** Vanilla JS Electron renderer (no runtime bundler), CodeMirror 6 (`@codemirror/*`, MIT), esbuild (dev-only), `node:test` for pure helpers, Go test suite stays green. Spec: `docs/superpowers/specs/2026-05-31-godynamo-gui-json-highlight-fold-design.md`.

---

## File Structure

| File | Responsibility | New/Modify |
|---|---|---|
| `electron/renderer/helpers.js` | Pure, DOM-free helpers (`findMatches`, `buildItemTemplate`), usable as browser globals **and** `require`-able in Node. | New |
| `electron/renderer/helpers.test.js` | `node:test` unit tests for the pure helpers. | New |
| `electron/renderer/vendor/codemirror/cm-entry.js` | The only file that knows CM6: `createEditor`, `createViewer`, Tokyo Night theme, folding, find decorations. | New |
| `electron/renderer/vendor/codemirror/package.json` | Dev-only pinned deps + esbuild `build` script. | New |
| `electron/renderer/vendor/codemirror/codemirror.bundle.js` | Generated IIFE bundle exposing `window.CM` (committed). | New (generated) |
| `electron/renderer/vendor/codemirror/demo.html` | Standalone, AWS-free harness to eyeball highlight/fold/find. | New |
| `electron/renderer/vendor/codemirror/LICENSE` | CodeMirror MIT license. | New |
| `electron/renderer/vendor/codemirror/README.md` | How to regenerate the bundle. | New |
| `electron/renderer/index.html` | Swap `<textarea>`→`#editor-cm`, `<pre>`→`#detail-body`; load `helpers.js` + bundle before `app.js`. | Modify |
| `electron/renderer/app.js` | Editor/viewer singletons + fallback glue; use `findMatches`/`buildItemTemplate`. | Modify |
| `electron/renderer/styles.css` | `.cm-editor` sizing; keep old rules for the fallback path. | Modify |
| `.gitignore` | Ignore the vendor `node_modules/`. | Modify |

**Task order keeps working software after every task:** helpers (1) → bundle + demo (2) → editor integration (3) → viewer integration (4) → full verification (5).

---

## Task 1: Pure helpers (TDD)

**Files:**
- Create: `electron/renderer/helpers.js`
- Test: `electron/renderer/helpers.test.js`

`helpers.js` is loaded as a classic `<script>` in the browser (defining globals) and `require`d in Node (CommonJS, via the UMD guard). The renderer runs with `contextIsolation` (no `module`), so the guard's `else` branch defines globals there.

- [ ] **Step 1: Write the failing test**

Create `electron/renderer/helpers.test.js`:

```js
const test = require('node:test')
const assert = require('node:assert/strict')
const { findMatches, buildItemTemplate } = require('./helpers.js')

test('findMatches: empty term -> []', () => {
  assert.deepEqual(findMatches('{"a":1}', ''), [])
})

test('findMatches: single match returns one {from,to} range', () => {
  assert.deepEqual(findMatches('hello world', 'world'), [{ from: 6, to: 11 }])
})

test('findMatches: multiple matches, case-insensitive', () => {
  assert.deepEqual(findMatches('Aba aba ABA', 'aba'), [
    { from: 0, to: 3 }, { from: 4, to: 7 }, { from: 8, to: 11 },
  ])
})

test('findMatches: no match -> []', () => {
  assert.deepEqual(findMatches('abc', 'xyz'), [])
})

test('buildItemTemplate: partition only', () => {
  assert.deepEqual(buildItemTemplate({ partition: 'pk', sort: '' }), { pk: '' })
})

test('buildItemTemplate: partition + sort', () => {
  assert.deepEqual(buildItemTemplate({ partition: 'pk', sort: 'sk' }), { pk: '', sk: '' })
})

test('buildItemTemplate: no keys -> {}', () => {
  assert.deepEqual(buildItemTemplate({ partition: '', sort: '' }), {})
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `node --test electron/renderer/helpers.test.js`
Expected: FAIL — `Cannot find module './helpers.js'`.

- [ ] **Step 3: Write the minimal implementation**

Create `electron/renderer/helpers.js`:

```js
// Pure, DOM-free helpers shared by the renderer (browser globals) and node:test.
function findMatches(text, term) {
  if (!term) return []
  const lower = String(text).toLowerCase()
  const tl = String(term).toLowerCase()
  const out = []
  let i = 0
  for (;;) {
    const idx = lower.indexOf(tl, i)
    if (idx === -1) break
    out.push({ from: idx, to: idx + tl.length })
    i = idx + tl.length
  }
  return out
}

function buildItemTemplate(keys) {
  const tmpl = {}
  if (keys && keys.partition) tmpl[keys.partition] = ''
  if (keys && keys.sort) tmpl[keys.sort] = ''
  return tmpl
}

if (typeof module !== 'undefined' && module.exports) {
  module.exports = { findMatches, buildItemTemplate } // Node (tests)
}
// In the browser these are window globals, consumed by app.js.
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `node --test electron/renderer/helpers.test.js`
Expected: PASS — 7 tests passing.

- [ ] **Step 5: Commit**

```bash
git add electron/renderer/helpers.js electron/renderer/helpers.test.js
git commit -m "feat(gui): pure findMatches/buildItemTemplate helpers with node:test"
```

---

## Task 2: Vendor CodeMirror 6 bundle + demo

**Files:**
- Create: `electron/renderer/vendor/codemirror/package.json`
- Create: `electron/renderer/vendor/codemirror/cm-entry.js`
- Create: `electron/renderer/vendor/codemirror/demo.html`
- Create: `electron/renderer/vendor/codemirror/README.md`
- Create (generated): `electron/renderer/vendor/codemirror/codemirror.bundle.js`
- Create: `electron/renderer/vendor/codemirror/LICENSE`
- Modify: `.gitignore`

- [ ] **Step 1: Create the dev-only package manifest**

Create `electron/renderer/vendor/codemirror/package.json`:

```json
{
  "name": "godynamo-cm-vendor",
  "private": true,
  "scripts": {
    "build": "esbuild cm-entry.js --bundle --minify --format=iife --global-name=CM --outfile=codemirror.bundle.js"
  },
  "devDependencies": {
    "@codemirror/state": "^6.4.1",
    "@codemirror/view": "^6.26.3",
    "@codemirror/commands": "^6.5.0",
    "@codemirror/language": "^6.10.1",
    "@codemirror/lang-json": "^6.0.1",
    "@lezer/highlight": "^1.2.0",
    "esbuild": "^0.20.2"
  }
}
```

- [ ] **Step 2: Create the CM6 entry module**

Create `electron/renderer/vendor/codemirror/cm-entry.js`:

```js
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
```

- [ ] **Step 3: Create the standalone demo (AWS-free verification harness)**

Create `electron/renderer/vendor/codemirror/demo.html`:

```html
<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8" /><title>CM vendor demo</title>
<style>
  body { background:#0b0f1a; color:#c8d3f5; font-family:'Segoe UI',sans-serif; margin:16px; }
  h3 { color:#7aa2f7; }
  #ed, #vw { border:1px solid #2a3450; border-radius:6px; margin:8px 0; height:240px; overflow:hidden; display:flex; }
  #ed .cm-editor, #vw .cm-editor { width:100%; height:100%; }
  button, input { background:#2a3450; color:#c8d3f5; border:1px solid #2a3450; border-radius:6px; padding:6px; }
</style></head>
<body>
  <h3>Editor — createEditor (fold via gutter or Ctrl-Shift-[ )</h3>
  <div id="ed"></div>
  <button id="get">console.log(getValue())</button>
  <h3>Viewer — createViewer (read-only, foldable)</h3>
  <input id="q" placeholder="find…" /> <span id="cnt"></span>
  <div id="vw"></div>
  <script src="codemirror.bundle.js"></script>
  <script>
    const sample = JSON.stringify({
      id: "u#123", name: "Alice", active: true, score: 42, notes: null,
      tags: ["a", "b", "c"],
      address: { city: "NYC", zip: "10001", geo: { lat: 40.7, lng: -74.0 } },
    }, null, 2)
    const ed = CM.createEditor({ parent: document.getElementById('ed'), doc: sample })
    const vw = CM.createViewer({ parent: document.getElementById('vw') }); vw.setValue(sample)
    document.getElementById('get').onclick = () => console.log(ed.getValue())
    function findMatches(text, term){ if(!term) return []; const l=text.toLowerCase(),t=term.toLowerCase(),o=[]; let i=0; for(;;){const x=l.indexOf(t,i); if(x<0)break; o.push({from:x,to:x+t.length}); i=x+t.length;} return o }
    const q = document.getElementById('q')
    q.oninput = () => { const m = findMatches(sample, q.value); vw.setMatches(m); document.getElementById('cnt').textContent = q.value ? (m.length + ' matches') : '' }
  </script>
</body></html>
```

- [ ] **Step 4: Create the regenerate instructions**

Create `electron/renderer/vendor/codemirror/README.md`:

```markdown
# Vendored CodeMirror 6

`codemirror.bundle.js` is a generated, committed esbuild IIFE bundle that exposes
`window.CM = { createEditor, createViewer }`. It is the only file the app loads.

## Regenerate

```sh
cd electron/renderer/vendor/codemirror
npm install        # dev-only deps; produces node_modules/ (gitignored)
npm run build      # -> codemirror.bundle.js
```

`cm-entry.js` is the source of truth (theme, folding, find decorations). `demo.html`
is a standalone, AWS-free harness: open it in a browser to eyeball highlighting,
folding, and find marks. License: CodeMirror is MIT (see LICENSE).
```

- [ ] **Step 5: Ignore the vendor node_modules**

Add this line to `.gitignore` (append at end):

```
electron/renderer/vendor/codemirror/node_modules/
```

- [ ] **Step 6: Install deps and build the bundle**

Run:
```sh
cd electron/renderer/vendor/codemirror && npm install && npm run build
```
Expected: `npm install` completes; `npm run build` writes `codemirror.bundle.js`.

> If the npm registry is unreachable in this environment, stop and report it — do **not** switch approaches silently. (Fallback discussed in the spec: fetch a community prebuilt single file. Confirm with the user before using it.)

- [ ] **Step 7: Copy the CodeMirror MIT license**

Copy the license text from an installed package into the vendor dir:
```powershell
Copy-Item electron/renderer/vendor/codemirror/node_modules/@codemirror/state/LICENSE electron/renderer/vendor/codemirror/LICENSE
```
Expected: `electron/renderer/vendor/codemirror/LICENSE` exists (MIT, © Marijn Haverbeke and others).

- [ ] **Step 8: Verify the bundle exposes the API**

Run:
```powershell
(Get-Item electron/renderer/vendor/codemirror/codemirror.bundle.js).Length
Select-String -Path electron/renderer/vendor/codemirror/codemirror.bundle.js -Pattern 'createEditor','createViewer' -SimpleMatch | Select-Object -First 2
```
Expected: size is non-trivial (hundreds of KB), and both `createEditor` and `createViewer` are found.

- [ ] **Step 9: Eyeball the demo (no AWS)**

Open `electron/renderer/vendor/codemirror/demo.html` in a browser (double-click, or `Start-Process electron/renderer/vendor/codemirror/demo.html`). Verify:
- JSON is colorized (keys blue, strings green, numbers orange, `true`/`null` purple).
- The fold gutter shows markers on `tags`, `address`, and nested `geo`.
- Folding `address` (gutter click or `Ctrl-Shift-[`) collapses it to a `{…}` placeholder; selecting that line and pressing Delete removes the whole `address` sub-tree (minimize → select → delete).
- Typing in "find…" highlights matches in the viewer with the amber mark and updates the count.

- [ ] **Step 10: Commit**

```bash
git add electron/renderer/vendor/codemirror/package.json \
        electron/renderer/vendor/codemirror/cm-entry.js \
        electron/renderer/vendor/codemirror/codemirror.bundle.js \
        electron/renderer/vendor/codemirror/demo.html \
        electron/renderer/vendor/codemirror/README.md \
        electron/renderer/vendor/codemirror/LICENSE \
        .gitignore
git commit -m "feat(gui): vendor CodeMirror 6 bundle (window.CM) with demo harness"
```

---

## Task 3: Editor integration (CM in the edit modal)

**Files:**
- Modify: `electron/renderer/index.html` (`:81`, `:137`)
- Modify: `electron/renderer/app.js` (`openNewItem` `:733`, `openEditItem` `:744`, `saveEditor` `:755`; new singleton block)
- Modify: `electron/renderer/styles.css` (`:55`)

- [ ] **Step 1: Swap the textarea for a CM container and load the scripts**

In `electron/renderer/index.html`, replace line 81:

```html
      <textarea id="editor-text" spellcheck="false"></textarea>
```
with:
```html
      <div id="editor-cm"></div>
```

Then replace the final script line (137):
```html
  <script src="app.js"></script>
```
with:
```html
  <script src="helpers.js"></script>
  <script src="vendor/codemirror/codemirror.bundle.js"></script>
  <script src="app.js"></script>
```

- [ ] **Step 2: Add the editor singleton + fallback glue to `app.js`**

In `electron/renderer/app.js`, immediately **before** `function openNewItem() {` (`:733`), insert:

```js
// --- CodeMirror editor singleton (falls back to a <textarea> if the bundle is absent) ---
let cmEditor = null
let editorFallback = null
function getEditor() {
  if (cmEditor) return cmEditor
  if (window.CM) { cmEditor = window.CM.createEditor({ parent: $('editor-cm'), doc: '' }) }
  return cmEditor
}
function fallbackEditor() {
  if (!editorFallback) {
    editorFallback = document.createElement('textarea')
    editorFallback.id = 'editor-text'
    editorFallback.spellcheck = false
    $('editor-cm').appendChild(editorFallback)
  }
  return editorFallback
}
function setEditorValue(s) {
  const ed = getEditor()
  if (ed) ed.setValue(s); else fallbackEditor().value = s
}
function getEditorValue() {
  const ed = getEditor()
  return ed ? ed.getValue() : fallbackEditor().value
}
function focusEditor() {
  const ed = getEditor()
  if (ed) ed.focus(); else fallbackEditor().focus()
}
```

- [ ] **Step 3: Rewrite `openNewItem` to use the editor glue**

Replace the whole `openNewItem` function (`:733-742`):

```js
function openNewItem() {
  const tmpl = {}
  if (state.keys.partition) tmpl[state.keys.partition] = ''
  if (state.keys.sort) tmpl[state.keys.sort] = ''
  $('editor-title').textContent = 'New item'
  $('editor-text').value = JSON.stringify(tmpl, null, 2)
  $('editor-error').textContent = ''
  show($('editor'))
  $('editor-text').focus()
}
```
with:
```js
function openNewItem() {
  $('editor-title').textContent = 'New item'
  $('editor-error').textContent = ''
  show($('editor'))                                    // show first so CM mounts visible
  setEditorValue(JSON.stringify(buildItemTemplate(state.keys), null, 2))
  focusEditor()
}
```

- [ ] **Step 4: Rewrite `openEditItem` to use the editor glue**

Replace the whole `openEditItem` function (`:744-752`):

```js
function openEditItem() {
  if (!state.selectedItem) return
  $('editor-title').textContent = 'Edit item'
  $('editor-text').value = JSON.stringify(state.selectedItem, null, 2)
  $('editor-error').textContent = ''
  hide($('detail'))
  show($('editor'))
  $('editor-text').focus()
}
```
with:
```js
function openEditItem() {
  if (!state.selectedItem) return
  $('editor-title').textContent = 'Edit item'
  $('editor-error').textContent = ''
  hide($('detail'))
  show($('editor'))                                    // show first so CM mounts visible
  setEditorValue(JSON.stringify(state.selectedItem, null, 2))
  focusEditor()
}
```

- [ ] **Step 5: Read the editor value from CM in `saveEditor`**

In `saveEditor` (`:755`), replace:
```js
  const text = $('editor-text').value
```
with:
```js
  const text = getEditorValue()
```

- [ ] **Step 6: Size the CM editor container in `styles.css`**

In `electron/renderer/styles.css`, immediately **after** the `#editor-text` rule (`:55`), add:

```css
#editor-cm { margin: 12px; min-height: 320px; max-height: 60vh; overflow: hidden; border: 1px solid #2a3450; border-radius: 6px; display: flex; }
#editor-cm .cm-editor { width: 100%; height: 100%; }
#editor-cm > textarea#editor-text { margin: 0; border: none; width: 100%; }  /* fallback only */
```
Keep the existing `#editor-text` rule (now used only by the fallback path).

- [ ] **Step 7: Manual smoke — editor (no AWS needed via the bundle demo already verified; app check optional)**

Build and run the GUI (`go run .`), open a table you consider safe, click **New item** and **Edit** on an item. Verify: JSON is colorized; the fold gutter folds objects/arrays; folding a sub-object then selecting its line + Delete removes the sub-tree; **Save** persists valid JSON and shows the error line for invalid JSON.
Expected: editor behaves as above; the viewer is unchanged (still the old `<pre>`), nothing else regresses.

> The core highlight/fold/delete behavior was already verified AWS-free in Task 2 Step 9; this app-level check is the integration wiring. If you have no safe table, rely on the Task 2 demo and skip the live AWS step.

- [ ] **Step 8: Commit**

```bash
git add electron/renderer/index.html electron/renderer/app.js electron/renderer/styles.css
git commit -m "feat(gui): CodeMirror editor with syntax highlight + folding in the edit modal"
```

---

## Task 4: Viewer integration (CM in the item detail)

**Files:**
- Modify: `electron/renderer/index.html` (`:71`)
- Modify: `electron/renderer/app.js` (`renderDetailBody` `:632`, `openDetail` `:661`; new viewer singleton)
- Modify: `electron/renderer/styles.css` (`:36`)

- [ ] **Step 1: Swap the `<pre>` for a CM container**

In `electron/renderer/index.html`, replace line 71:
```html
      <pre id="detail-body"></pre>
```
with:
```html
      <div id="detail-body"></div>
```

- [ ] **Step 2: Add the viewer singleton to `app.js`**

In `electron/renderer/app.js`, immediately **before** `function renderDetailBody() {` (`:632`), insert:

```js
// --- CodeMirror read-only viewer singleton (null if the bundle is absent) ---
let cmViewer = null
function getViewer() {
  if (cmViewer) return cmViewer
  if (window.CM) { cmViewer = window.CM.createViewer({ parent: $('detail-body') }) }
  return cmViewer
}
```

- [ ] **Step 3: Rewrite `renderDetailBody` (CM path + preserved fallback)**

Replace the whole `renderDetailBody` function (`:632-659`):

```js
function renderDetailBody() {
  const body = $('detail-body')
  const term = $('detail-search').value
  if (!term) {
    body.textContent = state.detailText
    $('detail-matches').textContent = ''
    return
  }
  const escaped = escapeHtml(state.detailText)
  const escTerm = escapeHtml(term)
  const lower = escaped.toLowerCase()
  const tlower = escTerm.toLowerCase()
  let result = ''
  let i = 0
  let count = 0
  while (true) {
    const idx = lower.indexOf(tlower, i)
    if (idx === -1) {
      result += escaped.slice(i)
      break
    }
    result += escaped.slice(i, idx) + '<mark>' + escaped.slice(idx, idx + escTerm.length) + '</mark>'
    i = idx + escTerm.length
    count++
  }
  body.innerHTML = result
  $('detail-matches').textContent = count + (count === 1 ? ' match' : ' matches')
}
```
with:
```js
function renderDetailBody() {
  const term = $('detail-search').value
  const v = getViewer()
  if (v) {                                             // CM path: value set in openDetail; here we only mark
    const matches = findMatches(state.detailText, term)
    v.setMatches(matches)
    $('detail-matches').textContent = term ? (matches.length + (matches.length === 1 ? ' match' : ' matches')) : ''
    return
  }
  // fallback: original <mark> overlay
  const body = $('detail-body')
  if (!term) {
    body.textContent = state.detailText
    $('detail-matches').textContent = ''
    return
  }
  const escaped = escapeHtml(state.detailText)
  const escTerm = escapeHtml(term)
  const lower = escaped.toLowerCase()
  const tlower = escTerm.toLowerCase()
  let result = ''
  let i = 0
  let count = 0
  while (true) {
    const idx = lower.indexOf(tlower, i)
    if (idx === -1) { result += escaped.slice(i); break }
    result += escaped.slice(i, idx) + '<mark>' + escaped.slice(idx, idx + escTerm.length) + '</mark>'
    i = idx + escTerm.length
    count++
  }
  body.innerHTML = result
  $('detail-matches').textContent = count + (count === 1 ? ' match' : ' matches')
}
```

- [ ] **Step 4: Rewrite `openDetail` to push the doc into CM first**

Replace the whole `openDetail` function (`:661-674`):

```js
function openDetail(title, text, withEditDelete) {
  state.detailText = text
  $('detail-title').textContent = title
  $('detail-search').value = ''
  renderDetailBody()
  if (withEditDelete) {
    show($('detail-edit'))
    show($('detail-delete'))
  } else {
    hide($('detail-edit'))
    hide($('detail-delete'))
  }
  show($('detail'))
}
```
with:
```js
function openDetail(title, text, withEditDelete) {
  state.detailText = text
  $('detail-title').textContent = title
  $('detail-search').value = ''
  show($('detail'))                                    // show first so CM mounts visible
  const v = getViewer()
  if (v) v.setValue(text)
  renderDetailBody()
  if (withEditDelete) {
    show($('detail-edit'))
    show($('detail-delete'))
  } else {
    hide($('detail-edit'))
    hide($('detail-delete'))
  }
}
```

- [ ] **Step 5: Size the CM viewer container in `styles.css`**

In `electron/renderer/styles.css`, replace the `#detail-body` rule (`:36`):
```css
#detail-body { overflow: auto; padding: 16px; margin: 0; font-family: 'Cascadia Code', monospace; font-size: 12px; }
```
with:
```css
#detail-body { overflow: auto; flex: 1; min-height: 0; padding: 8px; margin: 0; font-family: 'Cascadia Code', monospace; font-size: 12px; white-space: pre; }
#detail-body .cm-editor { width: 100%; height: 100%; }
```
Keep the existing `#detail-body mark` rule (`:65`) — it styles the fallback overlay. (`white-space: pre` only affects the fallback text; CM manages its own content layout.)

- [ ] **Step 6: Manual smoke — viewer**

Run the GUI (`go run .`), open an item detail and the Schema view. Verify: JSON is colorized; objects/arrays fold in the read-only view; the **Find…** box highlights matches (amber) and the count matches the old behavior; clearing the box clears marks; **Copy** still copies the full item.
Expected: viewer behaves as above; editor (Task 3) still works.

- [ ] **Step 7: Commit**

```bash
git add electron/renderer/index.html electron/renderer/app.js electron/renderer/styles.css
git commit -m "feat(gui): CodeMirror read-only viewer with highlight, folding, preserved find"
```

---

## Task 5: Full verification & wrap-up

**Files:** none (verification), optional `README.md`.

- [ ] **Step 1: Run the pure-helper tests**

Run: `node --test electron/renderer/helpers.test.js`
Expected: PASS (7 tests).

- [ ] **Step 2: Confirm the Go suite is unaffected**

Run: `go vet ./...` then `go test ./...`
Expected: both green (renderer JS is not exercised by Go tests).

- [ ] **Step 3: Fallback check (no bundle)**

In the running GUI, rename `electron/renderer/vendor/codemirror/codemirror.bundle.js` to `.bak`, relaunch the GUI, then open an item detail and the editor.
Expected: the GUI still works — viewer shows plain text with `<mark>` find, editor is a plain `<textarea>` that saves. Restore the file afterward.

- [ ] **Step 4: Full manual smoke checklist (spec §10)**

In the running GUI confirm, end to end: editor colorized; fold/unfold object and array (gutter + `Ctrl-Shift-[`); fold sub-object → select line → Delete removes sub-tree; save valid JSON persists; save invalid JSON shows `#editor-error`; item detail colorized + foldable; find highlights + counts, clears on empty; Copy copies the full item.
Expected: all pass.

- [ ] **Step 5: Optional — note the feature in `README.md`**

If the README lists GUI features, add a bullet: "JSON syntax highlighting and collapsible `{…}`/`[…]` blocks (foldable while editing) in the item viewer and editor (CodeMirror 6)." Commit if changed:
```bash
git add README.md && git commit -m "docs: note GUI JSON highlighting + folding"
```

---

## Self-Review (completed by plan author)

- **Spec coverage:** §3 vendoring → Task 2; §4 `window.CM` interface → Task 2 (`cm-entry.js`); §5 editor → Task 3; §6 viewer + `findMatches` → Tasks 1 & 4; §7 fallback → Tasks 3,4 & Task 5 Step 3; §9 theme → Task 2 Step 2; §10 testing → Tasks 1 & 5; §11 files → all tasks; §12 verification → Task 5. No gaps.
- **Type/name consistency:** `createEditor`/`createViewer` (cm-entry ↔ app.js getEditor/getViewer), `getValue`/`setValue`/`focus`, `setMatches(ranges:{from,to}[])` ↔ `findMatches` returns `{from,to}` ↔ demo. `buildItemTemplate(keys)` ↔ `state.keys` (`{partition,sort}`). Consistent.
- **Placeholders:** none — every code step is complete.
