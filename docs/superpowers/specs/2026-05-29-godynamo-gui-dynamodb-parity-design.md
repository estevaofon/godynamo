# GoDynamo GUI — DynamoDB-Console Parity Design Spec

- **Date:** 2026-05-29
- **Status:** Approved (design); ready for implementation planning.
- **Builds on:** the parity GUI (`docs/superpowers/specs/2026-05-28-godynamo-gui-parity-design.md`), on `develop` (`internal/gui` bridge + `internal/query` planner + `electron/` app).
- **Scope:** four UX refinements that bring the GUI closer to the DynamoDB console / DynamoBase: Enter-to-search, click-to-sort columns, GSI strategy display with overrides, and attribute-name autocomplete.

## 1. Goal

Make the filter/browse experience feel like the AWS console: pressing Enter runs the filter; clicking a column header sorts the loaded rows; the chosen Query-vs-Scan strategy (and which index) is shown with one-click overrides ("use GSI X instead", "use Scan instead"); and typing an attribute name autocompletes from the schema + loaded records.

## 2. Decisions locked during brainstorming

| Decision | Choice |
|---|---|
| GSI feature shape | **DynamoBase model**: the planner auto-selects the strategy, the GUI **shows** it, and offers **overrides** (force a specific index / force Scan). Requires small Go changes. |
| Sort first-click direction | **Date columns descending-first** (most-recent first); all other columns ascending-first. Two-state toggle (asc/desc) like Windows Explorer. |
| Sort scope | **Client-side, over loaded rows only** (DynamoDB can't sort arbitrary attributes server-side; the console also sorts client-side). Re-applied as more pages load; reset on table change. |
| Autocomplete source | Union of schema keys (table + every GSI/LSI PK/SK) **+** attribute names from loaded records, via a native `<datalist>`. |
| `BuildPlan` | **Untouched** — it is shared with the TUI (`internal/app/app.go:1241`). The override path adds a **new** `query.PlanForIndex`; force-Scan is assembled inline in the bridge. |
| Hard constraint | **No real AWS.** Estevao runs live tests. Automated verification = Go unit tests + fake `Backend` + `go build`/`go test`, and `node --check` for the renderer. Never launch `go run . gui` / `npm start` (they block). |

## 3. Feature 1 — Enter to search

Each filter `<input>` (attribute **and** value) gets a `keydown` listener: `Enter` → `applyFilter()` (identical to the existing **Apply** button; also `blur()`s nothing, just runs the search). No backend change. `applyFilter` continues to reset the cursor and reload page 1, and additionally resets the strategy override to `auto` (see §5).

## 4. Feature 2 — Column sort (client-side)

State: `state.sort = { column: null, dir: 'asc' }`.

- **Header click** (`renderGrid` binds a click handler per `<th>`):
  - clicking the **active** column toggles `dir` (`asc` ⇄ `desc`);
  - clicking a **different** column sets `column = c`, `dir = firstDirFor(c)`.
- **`firstDirFor(c)`** → `'desc'` if `isDateColumn(c)`, else `'asc'`.
- **`isDateColumn(c)`** (heuristic): true if the column **name** matches `/(_at$|date|time|timestamp|created|updated|modified)/i`, **or** every non-empty value for that column among the loaded rows is an ISO date string (`/^\d{4}-\d{2}-\d{2}/`). A purely numeric column counts as a date **only** when its name is date-ish (avoids treating `price`/`count`/`qty` as dates). When undecided → not a date (ascending-first).
- **Comparator** (applied to a shallow copy; `dir` flips the sign):
  - missing / `null` / `undefined` → always sorted **last**, both directions;
  - both numbers → numeric compare;
  - date column & both parse as dates → compare by epoch ms;
  - otherwise → `String(cellText(a)).localeCompare(String(cellText(b)))` (locale-aware; handles pt-BR accents).
- **Indicator:** the active column's `<th>` shows ` ▲` (asc) / ` ▼` (desc); all sortable headers get `cursor: pointer` + a hover style.
- **Render integration:** `renderGrid` computes `view = (state.sort.column && cols.includes(state.sort.column)) ? sortedItems() : state.items`, renders `view`, and stores `state.rendered = view`. Row click → `showItem(idx)` reads `state.rendered[idx]` (today it reads `state.items[idx]`; this is the only behavioral refactor to existing code). `selectedItem`/edit/delete stay by-reference, unaffected.
- **Lifecycle:** sort persists across `loadPage` (so "Resume fetching" merges new pages into the sorted view) and across filter Apply/Clear within a table; it is **reset** (`column = null`) on `selectTable`. There is no "unsorted" toggle state — to return to natural order, reload (matches Explorer).

## 5. Feature 3 — GSI strategy display + overrides

### 5.1 Panel layout (after Apply)

```
 [created_at v] [ = v] [2026-05-01]              [x]
 [+ Condition]  [Apply]  [Clear]
 ─────────────────────────────────────────────────
 Strategy:  QUERY · index: gsi_by_date  (auto)
   [ Use gsi_by_user instead ]   [ Use Scan instead ]
```

### 5.2 HTTP contract (Go change)

`queryRequest` (in `internal/gui/server.go`) gains one optional object:

```go
type queryStrategy struct {
    Mode  string `json:"mode"`  // "" | "auto" -> planner decides; "scan" -> force Scan; "query" -> force index
    Index string `json:"index"` // when Mode=="query": "" = base table, otherwise the GSI name
}
// queryRequest gains:
Strategy queryStrategy `json:"strategy"`
```

The `/query` **response** gains `"index"` (the index actually used; `""` for base-table query or Scan) alongside the existing `"mode"`.

### 5.3 Server dispatch (`handleQuery`)

After building `conds`, `expr/names/values`, the existing empty-expression `400`, and `DescribeTable`:

```go
var plan query.Plan
switch req.Strategy.Mode {
case "scan":
    plan = query.Plan{Mode: query.ModeScan, FilterExpression: expr, Names: names, Values: values}
case "query":
    p, perr := query.PlanForIndex(info, conds, req.Strategy.Index)
    if perr != nil { writeError(w, http.StatusBadRequest, perr.Error()); return }
    plan = p
default: // "" / "auto"
    plan = query.BuildPlan(info, expr, names, values)
}
```

The existing execution block (Query vs Scan on `plan.Mode`) is unchanged; the response adds `"index": plan.IndexName`.

### 5.4 `query.PlanForIndex` (new, condition-level)

```go
// PlanForIndex builds a Query plan that targets a specific index, or the base
// table when indexName == "". It uses the first equality (=) condition on that
// target's partition key as the key condition and the remaining conditions as
// the filter, mirroring BuildPlan's semantics (only the PK enters the key
// condition; any sort-key condition stays in the filter). It returns an error
// when the schema is missing or there is no equality on the target's PK.
func PlanForIndex(info *dynamo.TableInfo, conds []Condition, indexName string) (Plan, error)
```

- Resolve the target's partition-key attribute: `indexName == ""` → `info.PartitionKey`; otherwise the matching `info.GSIs[i].PartitionKey` (error `unknown index: X` if none).
- Find the first `Condition` with `Name == keyAttr`, `Operator == OpEquals`, non-empty `Value`; error if none (`index %q requires an equality (=) condition on its partition key %q`).
- Key condition uses fixed placeholders `#pk` / `:pkval`; the remaining conditions go through `BuildExpression` (which emits `#attrN` / `:valN` — no collision) to form `FilterExpression`. Merge the name/value maps.
- Return `Plan{Mode: ModeQuery, IndexName: indexName, KeyConditionExpression: "#pk = :pkval", FilterExpression: ..., Names, Values}`.

`BuildPlan` is **not modified** (TUI safety). LSIs are not query targets here (with PK-only key conditions an LSI query is equivalent to the base-table query); they are display-only.

### 5.5 Renderer

- **`state.indexes`** built in `selectTable` from `schema.info.GSIs` (and `info.LSIs` for display): `[{ name, kind:'table'|'gsi'|'lsi', pk, sk }]`, with a synthetic `Table` entry from `state.keys`.
- **`state.strategy`** = `{ mode, index }` set from each `/query` response.
- **`state.override`** = `{ mode:'auto'|'scan'|'query', index:'' }`; default `{mode:'auto'}`. Reset to `auto` on `applyFilter`, `clearFilter`, and `selectTable`. Preserved across "Resume fetching".
- **`loadPage`** includes `strategy: state.override` in the `/query` body when `filterActive`.
- **`renderStrategyBar()`** (shown only when `filterActive`): label `Strategy: QUERY · index: <name>` / `QUERY · table` / `SCAN`, with ` (auto)` appended when `state.override.mode === 'auto'`. Override buttons are computed from `activeConditions()` + `state.indexes`:
  - a GSI is a viable target iff some active condition is `op:'eq'` on that GSI's `pk`; the **Table** is viable iff some active condition is `op:'eq'` on the table PK;
  - render `Use <gsi> instead` for each viable GSI that isn't the current target, `Use Table instead` if the table is viable and not current, and `Use Scan instead` whenever the current strategy isn't already Scan;
  - a button sets `state.override` accordingly and calls `loadPage(true)`.
- The bridge still validates (a forced index without its `=` returns `400`, surfaced in the status line) — the renderer just avoids offering non-viable targets.

## 6. Feature 4 — Attribute-name autocomplete

- A single native `<datalist id="attr-suggestions">` in `index.html`; every attribute `<input>` created in `renderFilterRows` sets `list="attr-suggestions"`. CSP-safe (pure HTML, no injected script).
- **`updateAttrSuggestions()`** rebuilds the datalist `<option>`s from the union of: table PK/SK, every GSI/LSI PK/SK, and all attribute names across `state.items`. Called after each `loadPage` and on `selectTable`. Typing `to` surfaces `topology_id`, `topology_name`, etc. (native substring match).

## 7. Data flow (filtered query with override)

Apply / Enter → `state.override = {mode:'auto'}` → `POST /query {conditions, limit, cursor:"", strategy:{mode:'auto'}}` → bridge `DescribeTable` → `BuildPlan` → page → `{mode, index, items, cursor, count, scannedCount}` → grid renders (re-sorted if a sort is active), `renderStrategyBar()` shows `… (auto)` + viable override buttons. Clicking `Use gsi_by_user instead` → `state.override = {mode:'query', index:'gsi_by_user'}` → `loadPage(true)` → bridge `PlanForIndex` → `{mode:'query', index:'gsi_by_user', …}`. "Resume fetching" repeats with the stored `cursor` and the **same** `state.override`.

## 8. Error handling

- Forced index without an `=` on its PK, or an unknown index → bridge `400` `{error}`, shown in the status line; the panel stays usable.
- A failed page still leaves "Resume fetching" clickable (existing pattern).
- Sorting/autocomplete are pure client-side and cannot fail a request; non-comparable/missing values sort last rather than throwing.
- Existing query/scan/connect error handling is unchanged.

## 9. Testing (no real AWS)

- **`internal/query/plan_test.go`** — new cases for `PlanForIndex`: force a GSI (key + remaining filter), force the base table (`index:""`), error when no `=` on the target PK, error on unknown index. Existing `BuildPlan` tests stay green (function unchanged).
- **`internal/gui/server_test.go`** — new cases: `strategy:{mode:"scan"}` forces Scan even when the first condition is an indexable equality; `strategy:{mode:"query", index:"by-email"}` queries that GSI; the `/query` response includes `index`; a forced index lacking its `=` returns `400`. Reuse the existing `fakeBackend`.
- **Renderer** — `node --check electron/renderer/app.js`; manual verification by Estevao (dev-first, no JS unit harness — consistent with prior phases).
- `go build ./... && go test ./...` must be clean.

## 10. File-change summary

| File | Change |
|---|---|
| `internal/query/plan.go` | **add** `PlanForIndex` (keep `BuildPlan`) |
| `internal/query/plan_test.go` | **add** `PlanForIndex` tests |
| `internal/gui/server.go` | `queryStrategy` + `queryRequest.Strategy`; strategy dispatch in `handleQuery`; `index` in the response |
| `internal/gui/server_test.go` | **add** force-scan / force-index / response-`index` tests |
| `electron/renderer/index.html` | `<datalist id="attr-suggestions">`; strategy-bar container in the filter panel |
| `electron/renderer/app.js` | Enter-to-search; sort state + comparator + header handlers + render refactor; `state.indexes`/`strategy`/`override`; `renderStrategyBar`; `updateAttrSuggestions`; `loadPage`/`applyFilter`/`clearFilter`/`selectTable` updates |
| `electron/renderer/styles.css` | sortable header + ▲▼ indicator; strategy bar + override buttons (existing dark-theme tokens) |

## 11. Out of scope / deferred

- Pushing sort-key conditions into the Query key condition (key-condition stays PK-only, matching `BuildPlan`); LSI querying.
- Server-side / full-table sort (sort stays client-side over loaded rows).
- Persisting sort/override/connection preferences across sessions.
- Multi-attribute autocomplete ranking, value autocomplete (attribute names only).
- No changes to `dynamo.Client`, `models`, or the TUI.
