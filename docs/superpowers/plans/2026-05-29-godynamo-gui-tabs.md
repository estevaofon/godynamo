# GoDynamo GUI Multi-Table Tabs Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the GUI keep several DynamoDB tables open at once as browser-style tabs — left-click a sidebar table to open/focus its tab, right-click → "Open in new tab" to open another — each tab preserving its own rows, filter, sort, selection and scroll.

**Architecture:** Renderer-only. Today's single global `state` (the one open table) is split into a connection-level `conn` object (table list + open tabs + active id) and one per-tab state object per open table that **reuses today's exact field names**; the module-level `state` becomes a `let` that points at the active tab, so all per-table render/load logic stays unchanged. A tab strip, an empty state, and a right-click context menu are added on top. The Electron main/preload bridge is stateless per call and is **not** touched.

**Tech Stack:** Vanilla Electron renderer (no framework), DOM APIs, native CSS. No build step.

**Source spec:** `docs/superpowers/specs/2026-05-29-godynamo-gui-tabs-design.md`

---

## Conventions

- **No real AWS.** Estevao runs all live tests. The only automated gate for the renderer is `node --check electron/renderer/app.js` (syntax). Everything functional is verified manually by Estevao.
- **Never run `npm start` / `electron .` / `go run . gui`** — they block and/or hit real AWS. Do not launch the app.
- **Commit trailer:** every commit ends with a blank line then `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`. No backticks in commit messages.
- **No Go changes.** This feature is entirely in `electron/renderer/`. `go build ./...` / `go test ./...` are run once at the end only as a no-regression sanity check.
- **CSP unchanged:** `default-src 'self'; script-src 'self'; … style-src 'self' 'unsafe-inline'`. The context menu is built with DOM APIs and positioned via inline `style.left/top` (allowed by `style-src 'unsafe-inline'`); no inline `<script>`.
- **Field-name preservation is the whole trick.** Each tab object carries the same property names `state` has today (`currentTable`, `items`, `cursor`, `conditions`, `filterActive`, `sort`, `keys`, `indexes`, `schemaRaw`, `rendered`, `mode`, `scanned`, `strategy`, `override`, `selectedIdx`, `selectedItem`, `detailText`). Do not rename them, or the untouched functions break.

## File structure

```
electron/renderer/index.html   # MODIFY — add #tab-bar, #content-empty, #ctx-menu
electron/renderer/styles.css   # MODIFY — tab strip, empty state, context menu styles (append)
electron/renderer/app.js       # MODIFY — conn/state/newTab + tab mgmt + per-tab loads + context menu
```

No files are created or deleted.

---

## Task 1: DOM + CSS scaffolding

Add the three new elements and their styles. No JavaScript behavior yet — the elements stay hidden/empty, so the app keeps working through the existing code.

**Files:**
- Modify: `electron/renderer/index.html`
- Modify: `electron/renderer/styles.css`

- [ ] **Step 1: Add the tab strip above the toolbar.** In `index.html`, the `<main id="content">` block currently starts (line ~38):

```html
    <main id="content">
      <header id="toolbar">
```

Insert the tab strip between them so it reads:

```html
    <main id="content">
      <div id="tab-bar"></div>
      <header id="toolbar">
```

- [ ] **Step 2: Add the empty-state placeholder.** Still in `index.html`, the content area ends with the grid wrap:

```html
      <div id="grid-wrap">
        <table id="grid"><thead></thead><tbody></tbody></table>
      </div>
    </main>
```

Add a sibling placeholder right after `#grid-wrap` (still inside `<main id="content">`):

```html
      <div id="grid-wrap">
        <table id="grid"><thead></thead><tbody></tbody></table>
      </div>
      <div id="content-empty" class="hidden">Select a table to open it in a tab.</div>
    </main>
```

- [ ] **Step 3: Add the context menu.** In `index.html`, find the datalist near the bottom:

```html
  <datalist id="attr-suggestions"></datalist>
```

Insert the context menu immediately before it:

```html
  <div id="ctx-menu" class="hidden">
    <button id="ctx-open-new">Open in new tab</button>
  </div>

  <datalist id="attr-suggestions"></datalist>
```

- [ ] **Step 4: Append the styles.** Add to the end of `electron/renderer/styles.css`:

```css
#tab-bar { display: flex; overflow-x: auto; background: #0f1422; border-bottom: 1px solid #2a3450; }
#tab-bar:empty { display: none; }
.tab { flex: 0 0 auto; display: flex; align-items: center; gap: 6px; max-width: 220px; padding: 6px 10px; border-right: 1px solid #2a3450; cursor: pointer; font-size: 13px; color: #c8d3f5; }
.tab:hover { background: #1b2336; }
.tab.active { background: #131a2b; color: #7aa2f7; box-shadow: inset 0 -2px 0 #7aa2f7; }
.tab-label { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.tab-close { border: none; background: transparent; color: #828bb8; padding: 0 2px; font-size: 12px; line-height: 1; }
.tab-close:hover { color: #f7768e; background: transparent; }
#content-empty { flex: 1; display: flex; align-items: center; justify-content: center; color: #828bb8; font-size: 14px; }
#ctx-menu { position: fixed; z-index: 100; background: #131a2b; border: 1px solid #2a3450; border-radius: 6px; padding: 4px; box-shadow: 0 4px 16px rgba(0,0,0,0.5); }
#ctx-menu button { display: block; width: 100%; text-align: left; border: none; background: transparent; padding: 6px 12px; font-size: 13px; white-space: nowrap; }
#ctx-menu button:hover { background: #2a3450; }
#table-list li.open { color: #9aa5ce; }
#table-list li.open.active { color: #7aa2f7; }
```

- [ ] **Step 5: Syntax sanity.**

Run: `node --check electron/renderer/app.js`
Expected: no output, exit 0 (app.js is unchanged; this confirms nothing was edited by mistake).

- [ ] **Step 6: Manual check (Estevao).** Launch the app as usual, connect. Expected: behavior is unchanged from before; the empty-state and context menu are not visible; selecting a table still shows its grid. (A thin empty tab-bar line may appear above the toolbar — it disappears once Task 2 wires it via `#tab-bar:empty`.)

- [ ] **Step 7: Commit.**

```bash
git add electron/renderer/index.html electron/renderer/styles.css
git commit -m "feat(gui): add tab-bar, empty-state, and context-menu scaffolding

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: Core state-pointer refactor + tab bar

Split `state` into `conn` + per-tab objects, replace `selectTable` with `openTable`/`activate`/`loadTab`, make `loadPage` per-tab, render the tab strip, and wire close. After this task the feature works end-to-end except right-click "Open in new tab" (Task 3).

**Files:**
- Modify: `electron/renderer/app.js`

- [ ] **Step 1: Replace the `state` declaration with `conn` + `state` + `newTab`.** Replace the entire block currently at `app.js:25-44`:

```js
const state = {
  tables: [],
  currentTable: null,
  keys: { partition: '', sort: '' },
  indexes: [],
  schemaRaw: '',
  cursor: '',
  items: [],
  rendered: [],
  conditions: [],
  filterActive: false,
  mode: '',
  scanned: 0,
  strategy: { mode: '', index: '' },
  override: { mode: 'auto', index: '' },
  sort: { column: null, dir: 'asc' },
  selectedIdx: -1,
  selectedItem: null,
  detailText: '',
}
```

with:

```js
const conn = { tables: [], tabs: [], activeId: null, nextId: 1 }
let state = null // alias to the active tab object, or null when no tab is open

function newTab(name) {
  return {
    id: conn.nextId++, currentTable: name,
    loaded: false, status: '', scrollTop: 0, filterOpen: false,
    keys: { partition: '', sort: '' }, indexes: [], schemaRaw: '',
    cursor: '', items: [], rendered: [],
    conditions: [], filterActive: false, mode: '', scanned: 0,
    strategy: { mode: '', index: '' }, override: { mode: 'auto', index: '' },
    sort: { column: null, dir: 'asc' }, selectedIdx: -1, selectedItem: null, detailText: '',
  }
}
```

- [ ] **Step 2: Add the tab-management functions.** Insert these immediately after `newTab` (before `initConnectScreen`):

```js
function showEmptyState() {
  hide($('grid-wrap'))
  show($('content-empty'))
  hide($('filter-panel'))
  hide($('filter-strategy'))
  $('status').textContent = ''
  $('mode-badge').textContent = ''
  $('current-table').textContent = ''
  ;['schema-btn', 'filter-btn', 'new-item-btn', 'export-json', 'export-csv', 'more-btn']
    .forEach((id) => { $(id).disabled = true })
}

function syncToolbar() {
  const ready = !!(state && state.loaded)
  ;['schema-btn', 'filter-btn', 'new-item-btn', 'export-json', 'export-csv']
    .forEach((id) => { $(id).disabled = !ready })
  $('more-btn').disabled = !(state && state.cursor)
}

function hideContextMenu() { hide($('ctx-menu')) }

function renderTabs() {
  const bar = $('tab-bar')
  bar.innerHTML = ''
  conn.tabs.forEach((t) => {
    const el = document.createElement('div')
    el.className = 'tab' + (t.id === conn.activeId ? ' active' : '')
    const label = document.createElement('span')
    label.className = 'tab-label'
    label.textContent = t.currentTable
    el.appendChild(label)
    const close = document.createElement('button')
    close.className = 'tab-close'
    close.textContent = '✕'
    close.title = 'Close tab'
    close.addEventListener('click', (e) => { e.stopPropagation(); closeTab(t.id) })
    el.appendChild(close)
    el.addEventListener('click', () => activate(t.id))
    bar.appendChild(el)
  })
}

function openTable(name, opts) {
  const forceNew = !!(opts && opts.forceNew)
  if (!forceNew) {
    const existing = conn.tabs.find((t) => t.currentTable === name)
    if (existing) { activate(existing.id); return }
  }
  const tab = newTab(name)
  conn.tabs.push(tab)
  activate(tab.id)
  loadTab(tab)
}

function activate(id) {
  conn.activeId = id
  state = conn.tabs.find((t) => t.id === id) || null
  hide($('detail'))
  hide($('editor'))
  hideContextMenu()
  if (!state) { showEmptyState(); renderTabs(); renderTableList(); return }
  show($('grid-wrap'))
  hide($('content-empty'))
  renderTabs()
  renderTableList()
  renderFilterRows()
  if (state.filterOpen) show($('filter-panel')); else hide($('filter-panel'))
  renderStrategyBar()
  updateAttrSuggestions()
  renderGrid()
  $('grid-wrap').scrollTop = state.scrollTop
  syncToolbar()
  if (state.loaded) updateStatus()
  else $('status').textContent = state.status || 'Loading…'
}

async function loadTab(tab) {
  tab.status = 'Loading…'
  if (tab.id === conn.activeId) $('status').textContent = tab.status
  try {
    const schema = await window.api.schema(tab.currentTable)
    const info = schema.info || {}
    tab.keys = { partition: info.PartitionKey || '', sort: info.SortKey || '' }
    tab.indexes = buildIndexList(info)
    tab.schemaRaw = schema.rawJSON || JSON.stringify(info, null, 2)
    tab.loaded = true
    if (tab.id === conn.activeId) syncToolbar()
    await loadPage(tab, true)
  } catch (err) {
    tab.status = 'Error: ' + err.message
    if (tab.id === conn.activeId) $('status').textContent = tab.status
  }
}

function closeTab(id) {
  const i = conn.tabs.findIndex((t) => t.id === id)
  if (i === -1) return
  conn.tabs.splice(i, 1)
  if (conn.activeId === id) {
    const next = conn.tabs[i] || conn.tabs[i - 1]
    if (next) { activate(next.id) }
    else { conn.activeId = null; state = null; showEmptyState(); renderTabs(); renderTableList() }
  } else {
    renderTabs()
    renderTableList()
  }
}
```

- [ ] **Step 3: Rewrite `onConnect`.** Replace the body of `onConnect` (`app.js:68-87`) — only the `try` block changes (`state.tables` → `conn.tables`, plus `showEmptyState()`):

```js
async function onConnect() {
  const mode = document.querySelector('input[name="mode"]:checked').value
  const cfg = { mode }
  if (mode === 'aws') cfg.region = $('region').value
  else cfg.endpoint = $('endpoint').value

  $('connect-error').textContent = ''
  $('connect-btn').disabled = true
  try {
    const data = await window.api.connect(cfg)
    conn.tables = data.tables || []
    renderTableList()
    showEmptyState()
    hide($('connect-screen'))
    show($('main-screen'))
  } catch (err) {
    $('connect-error').textContent = err.message
  } finally {
    $('connect-btn').disabled = false
  }
}
```

- [ ] **Step 4: Rewrite `disconnect`.** Replace `disconnect` (`app.js:89-112`) with a version that resets `conn` and the new DOM:

```js
function disconnect() {
  conn.tables = []
  conn.tabs = []
  conn.activeId = null
  conn.nextId = 1
  state = null
  renderTabs()
  $('grid').querySelector('thead').innerHTML = ''
  $('grid').querySelector('tbody').innerHTML = ''
  $('current-table').textContent = ''
  $('status').textContent = ''
  $('mode-badge').textContent = ''
  hide($('filter-panel'))
  hide($('filter-strategy'))
  hide($('detail'))
  hide($('editor'))
  hideContextMenu()
  hide($('main-screen'))
  $('connect-error').textContent = ''
  show($('connect-screen'))
}
```

- [ ] **Step 5: Rewrite `renderTableList`.** Replace `renderTableList` (`app.js:114-127`) — uses `conn.tables`, opens a tab on click, and marks active/open tables:

```js
function renderTableList() {
  const filter = $('table-filter').value.toLowerCase()
  const ul = $('table-list')
  ul.innerHTML = ''
  const activeName = state ? state.currentTable : null
  const openNames = new Set(conn.tabs.map((t) => t.currentTable))
  conn.tables
    .filter((t) => t.toLowerCase().includes(filter))
    .forEach((t) => {
      const li = document.createElement('li')
      li.textContent = t
      li.className = (t === activeName ? 'active' : '') + (openNames.has(t) ? ' open' : '')
      li.addEventListener('click', () => openTable(t))
      ul.appendChild(li)
    })
}
```

- [ ] **Step 6: Update `refreshTables` and remove `selectTable`.** In `refreshTables` (`app.js:129-133`) change `state.tables` → `conn.tables`:

```js
async function refreshTables() {
  const data = await window.api.listTables()
  conn.tables = data.tables || []
  renderTableList()
}
```

Then **delete the entire `selectTable` function** (`app.js:135-181`). Its work is now done by `openTable` + `activate` + `loadTab`.

- [ ] **Step 7: Make `activeConditions` and `loadPage` per-tab.** Replace `activeConditions` (`app.js:187-191`) and `loadPage` (`app.js:193-230`) with tab-parameterised versions:

```js
function activeConditions(tab) {
  return tab.conditions
    .filter((c) => c.name.trim() !== '' && (!VALUE_OPS.has(c.op) || c.value.trim() !== ''))
    .map((c) => ({ name: c.name, op: c.op, value: c.value }))
}

async function loadPage(tab, reset) {
  const cursor = reset ? '' : tab.cursor
  try {
    let data
    if (tab.filterActive) {
      data = await window.api.query(tab.currentTable, {
        conditions: activeConditions(tab),
        limit: pageSize(),
        cursor,
        strategy: tab.override,
      })
      tab.mode = data.mode || ''
      tab.strategy = { mode: data.mode || '', index: data.index || '' }
    } else {
      data = await window.api.scan(tab.currentTable, cursor, pageSize())
      tab.mode = ''
    }
    if (reset) {
      tab.items = []
      tab.scanned = 0
      tab.selectedIdx = -1
      tab.selectedItem = null
    }
    tab.items = tab.items.concat(data.items || [])
    tab.cursor = data.cursor || ''
    if (tab.filterActive) tab.scanned += data.scannedCount || 0
    if (tab.id === conn.activeId) {
      syncToolbar()
      updateStatus()
      renderGrid()
      renderStrategyBar()
      updateAttrSuggestions()
    }
  } catch (err) {
    tab.status = 'Error: ' + err.message
    if (tab.id === conn.activeId) { $('status').textContent = tab.status; syncToolbar() }
  }
}
```

- [ ] **Step 8: Store status on the tab in `updateStatus`.** Replace `updateStatus` (`app.js:232-242`) — identical except it also saves `state.status`:

```js
function updateStatus() {
  let s = `${state.items.length} returned`
  if (state.filterActive) {
    s += ` · scanned ${state.scanned}`
    $('mode-badge').textContent = state.mode ? state.mode.toUpperCase() : ''
  } else {
    $('mode-badge').textContent = ''
  }
  if (state.cursor) s += ' · more available'
  state.status = s
  $('status').textContent = s
}
```

- [ ] **Step 9: Update `toggleFilter`, `applyFilter`, `clearFilter`.** Replace these three functions (`app.js:301-326`):

```js
function toggleFilter() {
  const panel = $('filter-panel')
  if (panel.classList.contains('hidden')) {
    if (state.conditions.length === 0) addCondition()
    show(panel)
    state.filterOpen = true
  } else {
    hide(panel)
    state.filterOpen = false
  }
}

async function applyFilter() {
  state.filterActive = activeConditions(state).length > 0
  state.override = { mode: 'auto', index: '' }
  state.cursor = ''
  await loadPage(state, true)
}

async function clearFilter() {
  state.conditions = []
  state.filterActive = false
  state.override = { mode: 'auto', index: '' }
  renderFilterRows()
  hide($('filter-strategy'))
  state.cursor = ''
  await loadPage(state, true)
}
```

- [ ] **Step 10: Fix the remaining `activeConditions()` / `loadPage()` call sites.**

In `viableIndexes` (`app.js:425-430`) change `activeConditions()` → `activeConditions(state)`:

```js
function viableIndexes() {
  const eqAttrs = new Set(
    activeConditions(state).filter((c) => c.op === 'eq').map((c) => c.name)
  )
  return state.indexes.filter((ix) => (ix.kind === 'table' || ix.kind === 'gsi') && ix.pk && eqAttrs.has(ix.pk))
}
```

In `renderStrategyBar`'s override button handler (`app.js:454-458`) change `loadPage(true)` → `loadPage(state, true)`:

```js
    b.addEventListener('click', () => {
      state.override = override
      state.cursor = ''
      loadPage(state, true)
    })
```

In `saveEditor` (`app.js:666`) and `doDelete` (`app.js:687`) change `await loadPage(true)` → `await loadPage(state, true)`.

- [ ] **Step 11: Update the `DOMContentLoaded` wiring.** In the listener block (`app.js:734-760`) change the `more-btn` and `page-size` handlers and add a scroll listener. Replace these two lines:

```js
  $('more-btn').addEventListener('click', () => loadPage(false))
  $('page-size').addEventListener('change', () => { if (state.currentTable) loadPage(true) })
```

with:

```js
  $('more-btn').addEventListener('click', () => loadPage(state, false))
  $('page-size').addEventListener('change', () => { if (state) loadPage(state, true) })
  $('grid-wrap').addEventListener('scroll', () => { if (state) state.scrollTop = $('grid-wrap').scrollTop })
```

- [ ] **Step 12: Syntax gate.**

Run: `node --check electron/renderer/app.js`
Expected: no output, exit 0. If it errors, fix the reported line before continuing.

- [ ] **Step 13: Grep for stragglers.** Confirm no old call signatures remain:

Run: `grep -n "loadPage(true)\|loadPage(false)\|state.tables\|selectTable\|activeConditions()" electron/renderer/app.js`
Expected: no matches (empty output).

- [ ] **Step 14: Manual check (Estevao).** Launch + connect. Expected:
  - After connect, the content area shows "Select a table to open it in a tab." and the toolbar buttons are disabled.
  - Left-click a table → a tab appears in the strip, the grid loads, the tab is highlighted, and the sidebar row is marked open/active.
  - Left-click a second table → a second tab; the first tab stays. Click the first tab (or its sidebar row) → it re-activates **instantly** with its own rows (cached, no reload).
  - Set a filter and a column sort in one tab, switch to the other and back → the filter/sort/scroll are preserved per tab and the two tabs are independent.
  - Click a tab's ✕ → it closes and a neighbor activates; close the last tab → the empty state returns.
  - New/edit/delete an item and export act on the active tab; "Change connection" returns to the connect screen with all tabs cleared.

- [ ] **Step 15: Commit.**

```bash
git add electron/renderer/app.js
git commit -m "feat(gui): per-tab table state with tab bar (open, switch, close)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: Right-click "Open in new tab" (context menu)

Add the sidebar context menu that always opens a fresh tab (duplicates allowed).

**Files:**
- Modify: `electron/renderer/app.js`

- [ ] **Step 1: Add the context-menu state + show helper.** Insert just above `hideContextMenu` (added in Task 2):

```js
let ctxTable = null

function showContextMenu(x, y, name) {
  ctxTable = name
  const menu = $('ctx-menu')
  menu.style.left = x + 'px'
  menu.style.top = y + 'px'
  show(menu)
}
```

- [ ] **Step 2: Attach the `contextmenu` listener to sidebar rows.** In `renderTableList` (rewritten in Task 2), add a `contextmenu` listener next to the existing `click` listener. Replace the `forEach` body so it reads:

```js
    .forEach((t) => {
      const li = document.createElement('li')
      li.textContent = t
      li.className = (t === activeName ? 'active' : '') + (openNames.has(t) ? ' open' : '')
      li.addEventListener('click', () => openTable(t))
      li.addEventListener('contextmenu', (e) => {
        e.preventDefault()
        showContextMenu(e.clientX, e.clientY, t)
      })
      ul.appendChild(li)
    })
```

- [ ] **Step 3: Wire the menu button and dismissal.** In the `DOMContentLoaded` listener block, after the existing wiring, add:

```js
  $('ctx-open-new').addEventListener('click', () => {
    if (ctxTable) openTable(ctxTable, { forceNew: true })
    hideContextMenu()
  })
  document.addEventListener('click', (e) => {
    if (!$('ctx-menu').contains(e.target)) hideContextMenu()
  })
  document.addEventListener('scroll', hideContextMenu, true)
  document.addEventListener('keydown', (e) => { if (e.key === 'Escape') hideContextMenu() })
```

- [ ] **Step 4: Syntax gate.**

Run: `node --check electron/renderer/app.js`
Expected: no output, exit 0.

- [ ] **Step 5: Manual check (Estevao).** Launch + connect. Expected:
  - Right-click a sidebar table → a small "Open in new tab" menu appears at the cursor.
  - Click it → a new tab opens for that table, **even if that table is already open** (a duplicate tab), each with independent filter/sort.
  - The menu closes on outside-click, on Escape, and when scrolling.

- [ ] **Step 6: Commit.**

```bash
git add electron/renderer/app.js
git commit -m "feat(gui): right-click sidebar table to open in a new tab

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: Final verification

No code changes — a clean-tree confirmation that the renderer parses and the Go build/tests still pass.

**Files:** none.

- [ ] **Step 1: Renderer syntax gate.**

Run: `node --check electron/renderer/app.js`
Expected: no output, exit 0.

- [ ] **Step 2: Go no-regression sanity.** (No Go files changed; this just confirms the repo still builds and the existing tests pass — `go test` uses fakes, not real AWS.)

Run: `go build ./... && go test ./...`
Expected: build succeeds; tests pass (`ok` lines), no failures.

- [ ] **Step 3: Full manual verification (Estevao).** Run through the spec §12 checklist end to end:
  1. Connect → empty state shown, no tab open.
  2. Left-click a table → one tab opens, loads, becomes active.
  3. Left-click a second table → second tab; left-click the first again → focuses it (no duplicate).
  4. Right-click a table → "Open in new tab" → a new tab even if already open (duplicate).
  5. Set a filter/sort/scroll in one tab, switch away and back → state preserved; the other tab is independent.
  6. Open 3 tabs rapidly → each ends up with its own correct rows (no cross-contamination).
  7. Close a middle tab, the active tab, and the last tab → neighbor activates / empty state appears.
  8. New/edit/delete item and export act on the active tab; create-table refreshes the sidebar without opening a tab.
  9. Disconnect → returns to connect screen with all tabs cleared.

- [ ] **Step 4 (optional): Update the README / GUI docs** if they enumerate GUI features, to mention tabs. Skip if no such list exists.

---

## Notes for the implementer

- **Why `state` is a `let` alias:** every existing render/load function (`renderGrid`, `columnOrder`, `renderFilterRows`, `sortedItems`, `renderStrategyBar`, `updateAttrSuggestions`, the detail/editor/export/put/delete functions) reads `state.<field>`. Because each tab reuses those field names and `state` is repointed in `activate`, those functions keep working untouched. Only `state.tables` (now `conn.tables`) and the `loadPage`/`activeConditions` signatures changed.
- **Concurrency:** `loadPage`/`loadTab` write into the tab object passed in and only touch the DOM when `tab.id === conn.activeId`, so opening several tabs quickly never lands one table's rows in another's grid.
- **Do not** add a "+" tab button, persist tabs to disk, or add keyboard shortcuts — all explicitly out of scope (spec §14).
