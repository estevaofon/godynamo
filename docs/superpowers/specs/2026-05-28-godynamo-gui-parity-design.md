# GoDynamo GUI — Feature Parity Design Spec

- **Date:** 2026-05-28
- **Status:** Approved (design); ready for implementation planning (Phase A first)
- **Builds on:** the v1 read-only GUI (`docs/superpowers/specs/2026-05-28-godynamo-electron-gui-design.md`), already on `develop` (`internal/gui` bridge + `electron/` app).
- **Scope:** bring the Electron GUI to feature parity with the terminal TUI, in three phases.

## 1. Goal

Port the remaining TUI features to the GUI: the visual filter builder + smart Query-vs-Scan, item CRUD, create-table, export, copy-to-clipboard, in-item JSON search, and region switching — reusing the existing Go logic and keeping the TUI behaviorally unchanged.

## 2. Decisions locked during brainstorming

| Decision | Choice |
|---|---|
| Shared logic | **Extract** the pure filter-expression builder + Query-vs-Scan planner into a new UI-agnostic `internal/query` package; refactor the TUI to use it; the bridge uses the same. |
| Filtered-scan UX | **DynamoDB-console model**: a configurable **page size** (= DynamoDB `Limit`, items *examined* per page) + a **"Resume fetching"** button that pulls the next page via the cursor. The GUI does **not** replicate the TUI's 3-min auto-accumulate loop. |
| Export | Export the **currently-loaded items** (matching the TUI) as JSON/CSV via a native Electron **Save dialog**. |
| Phasing | **A → B → C** (one shared spec; a separate plan + subagent build per phase). |
| Hard constraint | **The assistant runs no real AWS commands.** Estevao runs all live tests. Automated verification = pure unit tests, fake `Backend`, `go build`/`vet`, `node --check`. |

## 3. Shared `internal/query` package (the extraction)

UI-agnostic package both the TUI and the bridge depend on. **Characterization-test-first:** before moving any logic, write table-driven tests that capture the *current* TUI behavior (the effective Query/Scan inputs for representative condition sets), then refactor until they pass — guaranteeing behavior preservation.

```
internal/query/
  condition.go   # Operator enum (11 ops) + Condition{Name, Operator, Value} + ParseValue + BuildExpression
  plan.go        # Plan struct + BuildPlan(info, conds) → decide Query vs Scan
  condition_test.go
  plan_test.go
```

**`condition.go`** (moved out of `internal/ui/filter.go`, decoupled from `textinput`):
- `Operator` enum: `Equals, NotEquals, GreaterThan, LessThan, GreaterOrEqual, LessOrEqual, Contains, NotContains, BeginsWith, Exists, NotExists`.
- `Condition struct { Name string; Operator Operator; Value string }`.
- `ParseValue(string) interface{}` — number → `float64`, `true`/`false` → bool, `null` → nil, else string (verbatim from `ui.parseValue`).
- `BuildExpression(conds []Condition) (expr string, names map[string]string, values map[string]interface{})` — the operator→expression mapping verbatim from `ui.FilterBuilder.BuildExpression` (`#attr%d` / `:val%d` placeholders, `contains`/`begins_with`/`attribute_exists`/etc., `AND`-joined; empty-value conditions skipped; `Contains`/`NotContains`/`BeginsWith` keep string values).

**`plan.go`** (moved out of `app.go` `scanTable`):
- `Mode` = `ModeQuery | ModeScan`.
- `Plan struct { Mode Mode; IndexName string; KeyConditionExpression string; FilterExpression string; Names map[string]string; Values map[string]interface{} }`.
- `BuildPlan(info *dynamo.TableInfo, conds []Condition) Plan` — replicates the TUI rule: if the **first** condition is `Equals` on `info.PartitionKey` → `Query` on the table; else if `Equals` on a GSI's partition key → `Query` on that index; else `Scan`. In Query mode the first condition becomes the `KeyConditionExpression` and the remaining conditions become the `FilterExpression` (built via `BuildExpression`). In Scan mode all conditions become the `FilterExpression`. Improvement over the original: the planner works from structured `[]Condition` instead of re-parsing the built expression string (the original split the expression with `strings.SplitN(... " AND ")`); the characterization tests assert the resulting DynamoDB inputs are equivalent.

`dynamo.Client` (`QueryTable`, `ScanTable`, `ScanTableContinuous`) is **unchanged**.

## 4. TUI refactor (behavior-preserving)

- `internal/ui/filter.go`: `FilterBuilder` keeps its `textinput` widgets and rendering, but `BuildExpression` now converts its conditions to `[]query.Condition` and delegates to `query.BuildExpression`. `ui.FilterOperator` maps 1:1 to `query.Operator` via a small conversion helper (the 11 operators correspond exactly).
- `internal/app/app.go`: `scanTable` calls `query.BuildPlan(m.tableInfo, conds)` and dispatches to `m.client.QueryTable` / `ScanTableContinuous` based on `Plan.Mode`, instead of the inline string-splitting branch. The 3-min continuous-scan + "continue?" flow is **unchanged**.
- After the refactor, **Estevao manually confirms the TUI still behaves identically** (the assistant can't run it — it hits real AWS on startup).

## 5. Bridge API additions

The `Backend` interface grows per phase (the fake in tests grows with it). The bridge fetches table info via `DescribeTable` (cached per active table) for planning.

**Phase A**
| Method & path | Request | Response |
|---|---|---|
| `POST /tables/{name}/query` | `{ "conditions":[{"name","op","value"}...], "limit":N, "cursor":"<opaque>" }` | `{ "mode":"query"\|"scan", "items":[...], "cursor":"<opaque or empty>", "count":N, "scannedCount":M }` |

`op` is a stable string token per operator (e.g. `eq, ne, gt, lt, ge, le, contains, not_contains, begins_with, exists, not_exists`). The handler builds `[]query.Condition`, calls `query.BuildPlan`, then runs **one** `QueryTable` page or **one** `ScanTable` page (filter applied), returning the page + cursor + `scannedCount`. (`GET /scan` remains for unfiltered browse.)

**Phase B**
| Method & path | Request | Response |
|---|---|---|
| `POST /tables/{name}/item` | `{ "json": "<item JSON>" }` | `{ "ok": true }` (validates via `models.JSONToItem`, then `PutItem`) |
| `DELETE /tables/{name}/item` | `{ "json": "<item JSON>" }` | `{ "ok": true }` (derives PK/SK key from `tableInfo` + `DeleteItem`) |
| `POST /tables` | `{ "name","pk","pkType","sk","skType","billingMode","rcu","wcu" }` | `{ "ok": true }` (`CreateTable`) |

**Phase C**: no new Go endpoints — export/copy/item-search are client-side; **region switch reuses `POST /connect`** (it already swaps the active backend and clears cached table info).

## 6. Renderer additions

**Phase A — Filtering**
- A collapsible **filter panel**: rows of (attribute text input · operator `<select>` of the 11 ops · value input); add/remove condition buttons; Apply / Clear.
- A toolbar **page-size** control (e.g. 50 / 100 / 300 / 500 / 1000; default 500).
- The existing "Load more" becomes **"Resume fetching"** (enabled while `cursor != ""`).
- Status line: `returned N · scanned M` (+ a `Query`/`Scan` mode badge) so console-style filtered paging is legible.

**Phase B — Writes**
- A JSON-editor modal for **New item** (`n`) and **Edit item** (`e`) with client-side `JSON.parse` validation before POST; **Delete** with a confirm; a **Create-table** form (name, PK + type, optional SK + type, billing mode + RCU/WCU). Refresh the current page after a successful write.

**Phase C — Productivity**
- **Export** currently-loaded items to JSON/CSV via Electron `dialog.showSaveDialog` (wired through `main.js` + `preload.js` IPC; renderer builds the file contents).
- **Copy** selected cell value and row-as-JSON via `navigator.clipboard.writeText`.
- **Search within item JSON**: a find box in the detail overlay with match highlight + next/prev.
- **Change connection** button: returns to the connect screen and re-`POST /connect` for a different region/endpoint.

## 7. Data flow (filtered query, Phase A)

Filter panel **Apply** → `POST /tables/{name}/query {conditions, limit:pageSize, cursor:""}` → bridge: `DescribeTable` (cached) → `query.BuildPlan` → one `QueryTable`/`ScanTable` page → `{mode, items, cursor, count, scannedCount}` → grid renders, status shows counts + mode. **Resume fetching** repeats with the returned `cursor` until it's empty.

## 8. Error handling

- Item-editor JSON errors caught client-side (`JSON.parse`) before any request; bridge validation errors (`models.JSONToItem`) returned as `400` and shown inline.
- Write/connect/query failures surfaced as toasts (`502`/`400` JSON `{error}`).
- Invalid filter values coerce via `ParseValue` (same as the TUI) — no hard failure.
- A failed page leaves **Resume fetching** clickable to retry (same pattern as v1's Load More fix).

## 9. Testing (no real AWS)

- `internal/query`: **characterization tests first** (lock TUI behavior), then table-driven unit tests for `ParseValue`, `BuildExpression` (all 11 operators, multi-condition, empty-value skip), and `BuildPlan` (PK-equals → Query; GSI-PK-equals → Query-on-index; non-key / non-equals → Scan; first-condition split).
- Bridge: handler tests against the fake `Backend` (extended with `QueryTable`, `PutItem`, `DeleteItem`, `CreateTable`) — query planning dispatch, item validation (`400` on bad JSON), delete key derivation, create-table mapping, auth/CORS unchanged.
- TUI: Estevao manually confirms parity after the refactor.
- Renderer: manual (dev-first).

## 10. Phase breakdown (one plan + subagent build each, in order)

- **Phase A — Querying & filtering:** `internal/query` extraction + characterization tests + TUI refactor + `Backend.QueryTable` + `POST /query` + filter panel + page-size + Resume fetching. *(largest; proves the shared package)*
- **Phase B — Writes:** `Backend.{PutItem,DeleteItem,CreateTable}` + item/create-table endpoints + JSON-editor modal + delete confirm + create-table form.
- **Phase C — Productivity:** export (Save dialog), copy cell/row, in-item JSON search, change-connection/region switch.

## 11. Out of scope / deferred

- GUI does not replicate the TUI's 3-min continuous-scan auto-accumulate (replaced by console-style paging, by choice).
- All-region auto-scan on launch (still a connect-screen region pick).
- Packaging/installer/signing; WebSocket streaming; multi-connection/multi-window.
- No changes to `dynamo.Client` or `models` beyond what already exists.
