# GoDynamo GUI — Multi-Table Tabs Design Spec

- **Date:** 2026-05-29
- **Status:** Approved (design); ready for implementation planning.
- **Builds on:** the DynamoDB-console-parity GUI (`docs/superpowers/specs/2026-05-29-godynamo-gui-dynamodb-parity-design.md`), on `develop` (`electron/` app; `internal/gui` bridge unchanged).
- **Scope:** let the user view several tables at once via browser-style tabs, like DynamoBase. Left-click a sidebar table to open/focus its tab; right-click → "Open in new tab" to open another (duplicates allowed). Renderer-only — no Go/bridge changes.

## 1. Goal

Today the GUI shows exactly one table at a time: clicking a table in the sidebar wipes the view and loads the new one. Make it possible to keep multiple tables open simultaneously, each as a tab that preserves its own rows, filter, sort, selection and scroll position, and switch between them instantly without re-fetching.

## 2. Decisions locked during brainstorming

| Decision | Choice |
|---|---|
| Interaction model | **Tab-per-table (DynamoBase)**: left-click a sidebar table opens it in a tab, or **focuses** the first existing tab for that table; it never replaces another tab's content. |
| "Open in new tab" | Sidebar **right-click** → a one-item custom context menu; **always** opens a fresh tab. |
| Duplicates | **Allowed.** Right-click always makes a new tab even if that table is already open (compare two filters/sorts side by side). Left-click focuses the first existing tab. |
| Persistence | **Session-only.** Tabs live in memory; nothing is written to disk. (The connection itself isn't persisted either, so there's nothing to restore against.) |
| State architecture | **"State-pointer" refactor.** A connection-level `conn` object holds the table list + open tabs + active id; each tab is a per-table state object that **reuses today's exact field names**; the module-level `state` becomes a `let` that points at the active tab. Per-table logic is otherwise untouched. |
| Tab bar placement | Its own strip at the **top of `#content`, above `#toolbar`**. **No "+" button** — tabs are opened from the sidebar (the two flows above). The now-redundant `#current-table` label stops being updated. |
| Async correctness | `loadPage` / the initial load take an **explicit tab argument**, write results into that tab, and only touch the DOM when that tab is still active (multiple tabs can load concurrently). |
| Backend | **No Go changes.** The bridge is stateless per call (every IPC takes a table name); tabs are purely a renderer concern. |
| Hard constraint | **No real AWS.** Estevao runs live tests. Automated verification = `node --check electron/renderer/app.js`; everything else is manual, dev-first (no JS unit harness, consistent with prior phases). |

## 3. State model

Split today's single global `state` (`app.js:25`) into a connection-level object and one per-tab object per open table:

```js
const conn = { tables: [], tabs: [], activeId: null, nextId: 1 }  // connection-level
let state = null                                                   // alias → the active tab (null when none open)

function newTab(name) {
  return {
    id: conn.nextId++, currentTable: name,
    loaded: false, status: '', scrollTop: 0, filterOpen: false,   // tab-bookkeeping (new fields)
    // —— everything below keeps today's exact field names ——
    keys: { partition: '', sort: '' }, indexes: [], schemaRaw: '',
    cursor: '', items: [], rendered: [],
    conditions: [], filterActive: false, mode: '', scanned: 0,
    strategy: { mode: '', index: '' }, override: { mode: 'auto', index: '' },
    sort: { column: null, dir: 'asc' }, selectedIdx: -1, selectedItem: null, detailText: '',
  }
}
```

- The only **connection-level** field today is `state.tables`; its three references (`onConnect`, `renderTableList`, `refreshTables`) move to `conn.tables`.
- Every other `state.<field>` reference is **unchanged**, because `state` now points at the active tab and the field names are identical. This covers `loadPage`, `renderGrid`, `columnOrder`, `renderFilterRows`, `addCondition`/`removeCondition`, the sort functions, `renderStrategyBar`, `updateAttrSuggestions`, the detail/editor functions, the export functions, and item put/delete.
- New per-tab bookkeeping fields: `loaded` (initial schema+page fetched), `status` (last status-line text, for rehydration on activate), `scrollTop` (grid scroll, restored on activate), `filterOpen` (whether the filter panel is open for this tab).
- `selectTable(name)` (`app.js:135`) is **removed**; its job is split into `openTable` + `activate` + `loadTab` (§5). The old per-field reset logic is unnecessary — a new tab starts fresh from `newTab`.

## 4. UI / DOM additions

### 4.1 `index.html`

- A tab strip at the top of `#content`, immediately before `#toolbar`:
  ```html
  <div id="tab-bar"></div>
  ```
- An empty-state placeholder inside `#content` (shown when no tab is open), e.g. a centered `<div id="content-empty">Select a table to open it in a tab.</div>`.
- A custom context menu (the native one is suppressed via `preventDefault`):
  ```html
  <div id="ctx-menu" class="hidden"><button id="ctx-open-new">Open in new tab</button></div>
  ```

### 4.2 `styles.css`

- `#tab-bar { display:flex; overflow-x:auto; border-bottom:1px solid #2a3450; }` with the existing dark tokens.
- `.tab` — `flex:0 0 auto`, padding, `max-width` + `text-overflow:ellipsis` on the label, `cursor:pointer`; `.tab.active` highlighted like `#table-list li.active` (`#2a3450` / `#7aa2f7`); `.tab-close` a small ✕ that brightens on hover.
- `#ctx-menu` — `position:fixed; z-index` above the grid, card background/border matching `.modal-card`; its `<button>` styled like the sidebar buttons.
- `#table-list li.open` — a subtle marker (e.g. left accent / dimmer `#7aa2f7`) for tables that have at least one tab open.
- `#content-empty` — centered, muted (`#828bb8`), fills the content area.

## 5. Interaction flows

### 5.1 Opening / focusing

```js
function openTable(name, { forceNew } = {}) {
  if (!forceNew) {
    const existing = conn.tabs.find((t) => t.currentTable === name)
    if (existing) { activate(existing.id); return }
  }
  const tab = newTab(name)
  conn.tabs.push(tab)
  activate(tab.id)          // shows the (empty) tab immediately, status "Loading…"
  loadTab(tab)              // async: schema + first page (writes into `tab`)
}
```

- **Sidebar left-click** → `openTable(name)` (focus-or-open).
- **Sidebar right-click** → context menu → `openTable(name, { forceNew: true })` (always new; duplicates allowed).

### 5.2 Activating (rehydrate the DOM from a tab)

```js
function activate(id) {
  conn.activeId = id
  state = conn.tabs.find((t) => t.id === id) || null
  hide($('detail')); hide($('editor'))        // avoid showing the previous tab's modal
  hideContextMenu()
  if (!state) { showEmptyState(); renderTabs(); renderTableList(); return }
  // rehydrate every shared DOM region from the active tab:
  show($('grid-wrap')); hide($('content-empty'))
  renderTabs(); renderTableList()
  renderFilterRows(); state.filterOpen ? show($('filter-panel')) : hide($('filter-panel'))
  renderStrategyBar(); updateAttrSuggestions(); renderGrid()
  $('grid-wrap').scrollTop = state.scrollTop
  syncToolbar()                                // enable/disable from state.loaded / state.cursor
  state.loaded ? updateStatus() : ($('status').textContent = state.status || 'Loading…')
}
```

- `renderTabs()` rebuilds `#tab-bar`: one `.tab` per `conn.tabs` entry (`currentTable` label + `.tab-close`), `.active` on `conn.activeId`. Click a tab → `activate(id)`; click its ✕ (with `stopPropagation`) → `closeTab(id)`.
- `renderTableList` highlights the active tab's table (`.active`) and marks any table with an open tab (`.open`).
- `syncToolbar()` sets `schema-btn`/`filter-btn`/`new-item-btn`/`export-json`/`export-csv` disabled from `!state.loaded`, and `more-btn` from `!state.cursor`.
- `showEmptyState()` (no active tab) hides `#grid-wrap`, shows `#content-empty`, hides the filter panel/strategy bar, clears `#status`/`#mode-badge`, and disables every toolbar button.
- `#grid-wrap` `scroll` listener stores `state.scrollTop` (guarded by `state &&`), so it survives tab switches.

### 5.3 Initial load (per-tab, async-safe)

```js
async function loadTab(tab) {
  tab.status = 'Loading…'; if (tab.id === conn.activeId) $('status').textContent = tab.status
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
```

### 5.4 Closing

```js
function closeTab(id) {
  const i = conn.tabs.findIndex((t) => t.id === id)
  if (i === -1) return
  conn.tabs.splice(i, 1)
  if (conn.activeId === id) {
    const next = conn.tabs[i] || conn.tabs[i - 1]   // prefer right neighbor, else left
    next ? activate(next.id) : (conn.activeId = null, state = null, showEmptyState(), renderTabs(), renderTableList())
  } else {
    renderTabs(); renderTableList()
  }
}
```

## 6. Per-tab async loads (correctness)

Opening several tabs quickly means several `schema`/`scan`/`query` fetches in flight at once. To stop a slow load from writing into the wrong tab, `loadPage` takes the tab explicitly, writes only into that tab, and touches the DOM only when the tab is still active:

```js
async function loadPage(tab, reset) {
  const cursor = reset ? '' : tab.cursor
  try {
    let data
    if (tab.filterActive) {
      data = await window.api.query(tab.currentTable, { conditions: activeConditions(tab), limit: pageSize(), cursor, strategy: tab.override })
      tab.mode = data.mode || ''; tab.strategy = { mode: data.mode || '', index: data.index || '' }
    } else {
      data = await window.api.scan(tab.currentTable, cursor, pageSize()); tab.mode = ''
    }
    if (reset) { tab.items = []; tab.scanned = 0; tab.selectedIdx = -1; tab.selectedItem = null }
    tab.items = tab.items.concat(data.items || [])
    tab.cursor = data.cursor || ''
    if (tab.filterActive) tab.scanned += data.scannedCount || 0
    if (tab.id === conn.activeId) { syncToolbar(); updateStatus(); renderGrid(); renderStrategyBar(); updateAttrSuggestions() }
  } catch (err) {
    tab.status = 'Error: ' + err.message
    if (tab.id === conn.activeId) { $('status').textContent = tab.status; syncToolbar() }
  }
}
```

- `activeConditions` is reworked to take the tab (`activeConditions(tab)`); existing callers pass the active tab. (Alternatively it keeps reading `state` — but threading the tab keeps it consistent with `loadPage`.)
- **Call sites** all pass the active tab (`state`): `more-btn` → `loadPage(state, false)`; `applyFilter`/`clearFilter`/page-size-change/`saveEditor`/`doDelete` → `loadPage(state, true)`.
- `updateStatus`, `renderGrid`, `renderStrategyBar`, `updateAttrSuggestions`, `syncToolbar` read the active tab via `state`; they're only invoked from the guarded branch (where `state === tab`) or from `activate`. `updateStatus` also stores `state.status = s` so `activate` can rehydrate the status line.

## 7. Filter panel per tab

`toggleFilter` records visibility on the active tab so it's restored on switch:

```js
function toggleFilter() {
  const panel = $('filter-panel')
  if (panel.classList.contains('hidden')) { if (state.conditions.length === 0) addCondition(); show(panel); state.filterOpen = true }
  else { hide(panel); state.filterOpen = false }
}
```

`applyFilter`/`clearFilter` keep their current bodies except `loadPage(true)` → `loadPage(state, true)` and they continue to reset `state.override` to `{mode:'auto'}` and `state.cursor` to `''` on the active tab.

## 8. Sidebar + context menu

- `renderTableList` (`app.js:114`): `state.tables` → `conn.tables`; the `<li>` click handler → `openTable(t)`; add `li.addEventListener('contextmenu', e => { e.preventDefault(); showContextMenu(e.clientX, e.clientY, t) })`; active/open classes per §5.2.
- `showContextMenu(x, y, name)` positions `#ctx-menu` (`style.left/top`), stores `name`, unhides it. `#ctx-open-new` click → `openTable(name, { forceNew:true }); hideContextMenu()`.
- `hideContextMenu()` is wired to a document `click` (capture), `scroll`, `keydown` Escape, and is also called from `activate`/`closeTab`/`disconnect`. Setting `style.left/top` is an inline style on the element — allowed under the existing CSP (`style-src 'self' 'unsafe-inline'`); no injected `<script>`, so `script-src 'self'` is satisfied.

## 9. Connect / disconnect

- `onConnect` (`app.js:68`): `state.tables` → `conn.tables`; after `renderTableList()`, do **not** open a tab — show the empty state. `state` stays `null` until the user opens a table.
- `disconnect` (`app.js:89`) resets the whole session: `conn.tables=[]; conn.tabs=[]; conn.activeId=null; conn.nextId=1; state=null`, clear `#tab-bar`, clear the grid, hide filter panel/strategy/detail/editor/ctx-menu, show the connect screen.
- `refreshTables` (`app.js:129`): `state.tables` → `conn.tables` (used by create-table; does not open a tab).

## 10. Data flow

**Left-click `Users`** → `openTable('Users')` → no existing tab → `newTab` pushed → `activate` (empty grid, "Loading…") → `loadTab` → `schema('Users')` then `loadPage(tab,true)` → `scan` → rows land in the tab → tab is active → grid + status render.

**Right-click `Users` → Open in new tab** (while a `Users` tab exists) → `openTable('Users',{forceNew:true})` → second `Users` tab created and activated → independent `loadTab`. Filtering/sorting in one `Users` tab leaves the other untouched.

**Switch tabs** → `activate(id)` repoints `state`, re-renders the grid/filter/strategy/status from that tab's cached data and restores its scroll — no network call.

**Close active tab** → `closeTab` activates the right neighbor (else left), or shows the empty state when none remain.

## 11. Error handling

- A failed `schema`/`scan`/`query` for a tab stores the message in `tab.status` and shows it in the status line only if that tab is active; other tabs are unaffected. `more-btn` stays clickable per the existing pattern.
- Background tabs that error while inactive surface the message when next activated (via `state.status`).
- Detail/editor modals are hidden on activate/close, so an item from a closed/!switched tab is never shown against the wrong table.
- All existing per-table error handling (invalid-JSON editor, delete confirm, create-table validation, forced-index `400`) is unchanged — it now simply runs against the active tab.

## 12. Testing (no real AWS)

- **`node --check electron/renderer/app.js`** must pass (syntax gate; the only automated check for the renderer, consistent with prior phases).
- **Go** is untouched — `go build ./... && go test ./...` should remain clean (sanity only; no changes expected).
- **Manual verification by Estevao** (checklist to accompany the plan):
  1. Connect → empty state shown, no tab open.
  2. Left-click a table → one tab opens, loads, becomes active.
  3. Left-click a second table → second tab; left-click the first again → **focuses** it (no duplicate).
  4. Right-click a table → "Open in new tab" → a new tab even if already open (duplicate).
  5. Set a filter/sort/scroll in one tab, switch away and back → state preserved; the other tab is independent.
  6. Open 3 tabs rapidly → each ends up with its own correct rows (no cross-contamination).
  7. Close a middle tab, the active tab, and the last tab → neighbor activates / empty state appears.
  8. New/edit/delete item and export act on the active tab; create-table refreshes the sidebar without opening a tab.
  9. Disconnect → returns to connect screen with all tabs cleared.

## 13. File-change summary

| File | Change |
|---|---|
| `electron/renderer/app.js` | `conn` + `let state`; `newTab`; remove `selectTable`; add `openTable`/`activate`/`loadTab`/`closeTab`/`renderTabs`/`syncToolbar`/`showEmptyState`; `loadPage(tab,reset)` + `activeConditions(tab)` and updated call sites; per-tab `filterOpen`/`scrollTop`/`status`; `renderTableList` left+right click; `showContextMenu`/`hideContextMenu`; `conn.tables` in `onConnect`/`refreshTables`/`renderTableList`; reset `conn` in `disconnect`; new DOM wiring in `DOMContentLoaded`. |
| `electron/renderer/index.html` | `#tab-bar`, `#content-empty`, `#ctx-menu` (with `#ctx-open-new`). |
| `electron/renderer/styles.css` | `#tab-bar`, `.tab`/`.tab.active`/`.tab-close`, `#ctx-menu`, `#table-list li.open`, `#content-empty` (existing dark-theme tokens). |

## 14. Out of scope / deferred

- Persisting open tabs / active tab / connection across app restarts.
- A "+" new-tab button or tab picker (tabs are opened from the sidebar only).
- Drag-to-reorder tabs; keyboard tab navigation (Ctrl+W / Ctrl+Tab); middle-click close.
- Per-tab page-size (the page-size control stays global; it applies on the next fetch).
- Any Go / `internal/gui` / bridge / IPC changes; no new JS test framework.
