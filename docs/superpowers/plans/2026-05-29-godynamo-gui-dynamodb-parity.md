# GoDynamo GUI DynamoDB-Console Parity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the GUI feel like the DynamoDB console — Enter runs the filter, column headers sort the loaded rows, the chosen Query/Scan strategy is shown with one-click index/scan overrides, and attribute names autocomplete.

**Architecture:** Two small additive Go changes (a new `query.PlanForIndex` and a `strategy` branch in the `/query` handler that also returns the chosen `index`) plus renderer-only work for sorting, autocomplete, Enter-to-search, and the strategy bar. The shared `query.BuildPlan` (used by the TUI) is left untouched.

**Tech Stack:** Go 1.x (`net/http`, `aws-sdk-go-v2` types), vanilla Electron renderer (no framework), native `<datalist>`.

**Source spec:** `docs/superpowers/specs/2026-05-29-godynamo-gui-dynamodb-parity-design.md`

---

## Conventions

- **No real AWS.** Estevao runs all live tests. Automated verification = `go build ./...`, `go test ./...`, `node --check`.
- **Never run `go run . gui` or `npm start`** (they block / hit real AWS).
- **Commit trailer:** every commit ends with a blank line then `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`. No backticks in commit messages.
- **`query.BuildPlan` is shared with the TUI** (`internal/app/app.go:1241`) — do **not** change it. The override path is a new function.
- **CSP unchanged:** `default-src 'self'; script-src 'self'; …` — the datalist and strategy bar are built with DOM APIs / static HTML, no inline scripts.

## File structure

```
internal/query/plan.go         # MODIFY — add PlanForIndex (BuildPlan untouched)
internal/query/plan_test.go    # MODIFY — add PlanForIndex tests
internal/gui/server.go         # MODIFY — queryStrategy + Strategy field; dispatch in handleQuery; index in response
internal/gui/server_test.go    # MODIFY — force-scan / force-index / response-index tests
electron/renderer/index.html   # MODIFY — <datalist> + strategy-bar container
electron/renderer/app.js       # MODIFY — Enter, sort, autocomplete, strategy overrides
electron/renderer/styles.css   # MODIFY — sortable header + strategy bar styles (append)
```

---

## Task 1: `query.PlanForIndex` (forced index/table planning)

**Files:**
- Modify: `internal/query/plan.go`
- Test: `internal/query/plan_test.go`

- [ ] **Step 1: Write the failing tests.** Append to `internal/query/plan_test.go` (the file already imports `testing` and `github.com/godynamo/internal/dynamo`):

```go
func TestPlanForIndexForcesGSI(t *testing.T) {
	info := &dynamo.TableInfo{
		PartitionKey: "id",
		GSIs:         []dynamo.IndexInfo{{Name: "by-user", PartitionKey: "user_id"}},
	}
	conds := []Condition{
		{Name: "status", Operator: OpEquals, Value: "active"},
		{Name: "user_id", Operator: OpEquals, Value: "u1"},
	}
	p, err := PlanForIndex(info, conds, "by-user")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Mode != ModeQuery {
		t.Fatalf("want ModeQuery, got %v", p.Mode)
	}
	if p.IndexName != "by-user" {
		t.Fatalf("index=%q", p.IndexName)
	}
	if p.KeyConditionExpression != "#pk = :pkval" {
		t.Fatalf("keyCond=%q", p.KeyConditionExpression)
	}
	if p.Names["#pk"] != "user_id" {
		t.Fatalf("names=%v", p.Names)
	}
	if p.Values[":pkval"] != "u1" {
		t.Fatalf("values=%v", p.Values)
	}
	if p.FilterExpression != "#attr0 = :val0" {
		t.Fatalf("filter=%q", p.FilterExpression)
	}
	if p.Names["#attr0"] != "status" || p.Values[":val0"] != "active" {
		t.Fatalf("filter names/values=%v %v", p.Names, p.Values)
	}
}

func TestPlanForIndexForcesBaseTable(t *testing.T) {
	info := &dynamo.TableInfo{
		PartitionKey: "id",
		GSIs:         []dynamo.IndexInfo{{Name: "by-user", PartitionKey: "user_id"}},
	}
	// user_id matches the GSI, but forcing "" must use the table PK "id".
	conds := []Condition{
		{Name: "user_id", Operator: OpEquals, Value: "u1"},
		{Name: "id", Operator: OpEquals, Value: "42"},
	}
	p, err := PlanForIndex(info, conds, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.IndexName != "" {
		t.Fatalf("want base table, got index %q", p.IndexName)
	}
	if p.Names["#pk"] != "id" {
		t.Fatalf("names=%v", p.Names)
	}
	if p.Values[":pkval"] != float64(42) {
		t.Fatalf("pkval=%v (%T)", p.Values[":pkval"], p.Values[":pkval"])
	}
}

func TestPlanForIndexErrorsWithoutEqualityOnPK(t *testing.T) {
	info := &dynamo.TableInfo{
		PartitionKey: "id",
		GSIs:         []dynamo.IndexInfo{{Name: "by-user", PartitionKey: "user_id"}},
	}
	conds := []Condition{{Name: "user_id", Operator: OpBeginsWith, Value: "u"}}
	if _, err := PlanForIndex(info, conds, "by-user"); err == nil {
		t.Fatal("want error when no equality on the index PK")
	}
}

func TestPlanForIndexErrorsOnUnknownIndex(t *testing.T) {
	info := &dynamo.TableInfo{PartitionKey: "id"}
	conds := []Condition{{Name: "id", Operator: OpEquals, Value: "1"}}
	if _, err := PlanForIndex(info, conds, "nope"); err == nil {
		t.Fatal("want error on unknown index")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail.**

Run: `go test ./internal/query/ -run TestPlanForIndex -v`
Expected: FAIL — build error `undefined: PlanForIndex`.

- [ ] **Step 3: Implement `PlanForIndex`.** Append to `internal/query/plan.go` (the file already imports `fmt` and `strings`):

```go
// PlanForIndex builds a Query plan that targets a specific index, or the base
// table when indexName == "". The first equality (=) condition on that target's
// partition key becomes the key condition; the remaining conditions become the
// filter (mirroring BuildPlan: only the partition key enters the key condition,
// any sort-key condition stays in the filter). It returns an error when the
// schema is missing, the index is unknown, or there is no equality on the
// target's partition key.
func PlanForIndex(info *dynamo.TableInfo, conds []Condition, indexName string) (Plan, error) {
	if info == nil {
		return Plan{}, fmt.Errorf("table schema unavailable")
	}

	keyAttr := info.PartitionKey
	if indexName != "" {
		found := false
		for _, gsi := range info.GSIs {
			if gsi.Name == indexName {
				keyAttr = gsi.PartitionKey
				found = true
				break
			}
		}
		if !found {
			return Plan{}, fmt.Errorf("unknown index: %s", indexName)
		}
	}
	if keyAttr == "" {
		return Plan{}, fmt.Errorf("target has no partition key")
	}

	keyIdx := -1
	for i, c := range conds {
		if c.Name == keyAttr && c.Operator == OpEquals && strings.TrimSpace(c.Value) != "" {
			keyIdx = i
			break
		}
	}
	if keyIdx < 0 {
		target := "table"
		if indexName != "" {
			target = "index " + indexName
		}
		return Plan{}, fmt.Errorf("%s requires an equality (=) condition on its partition key %q", target, keyAttr)
	}

	rest := make([]Condition, 0, len(conds))
	for i, c := range conds {
		if i != keyIdx {
			rest = append(rest, c)
		}
	}

	names := map[string]string{"#pk": keyAttr}
	values := map[string]interface{}{":pkval": ParseValue(conds[keyIdx].Value)}

	filterExpr, fNames, fValues := BuildExpression(rest)
	for k, v := range fNames {
		names[k] = v
	}
	for k, v := range fValues {
		values[k] = v
	}

	return Plan{
		Mode:                   ModeQuery,
		IndexName:              indexName,
		KeyConditionExpression: "#pk = :pkval",
		FilterExpression:       filterExpr,
		Names:                  names,
		Values:                 values,
	}, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass.**

Run: `go test ./internal/query/ -v`
Expected: PASS (the four new tests + all existing `BuildPlan`/`BuildExpression` tests).

- [ ] **Step 5: Commit.**

```
git add internal/query/plan.go internal/query/plan_test.go
git commit -m "feat(query): add PlanForIndex for forced index/table planning"
```
(+ trailer)

---

## Task 2: `/query` strategy override + `index` in response

**Files:**
- Modify: `internal/gui/server.go`
- Test: `internal/gui/server_test.go`

- [ ] **Step 1: Write the failing tests.** Append to `internal/gui/server_test.go` (it already imports `net/http`, `encoding/json`, the `types` package, and `github.com/godynamo/internal/dynamo`):

```go
func TestQueryForceScanIgnoresIndexableEquality(t *testing.T) {
	s := newTestServer(&fakeBackend{
		info: &dynamo.TableInfo{Name: "t", PartitionKey: "id"},
		scan: &dynamo.ScanResult{
			Items: []map[string]types.AttributeValue{{"id": &types.AttributeValueMemberS{Value: "1"}}},
			Count: 1,
		},
	})
	// id = 1 would normally Query the table; strategy:scan must force a Scan.
	rec := do(s, http.MethodPost, "/tables/t/query",
		`{"conditions":[{"name":"id","op":"eq","value":"1"}],"strategy":{"mode":"scan"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Mode  string `json:"mode"`
		Index string `json:"index"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Mode != "scan" {
		t.Fatalf("want mode scan, got %q", resp.Mode)
	}
	if resp.Index != "" {
		t.Fatalf("want empty index for scan, got %q", resp.Index)
	}
}

func TestQueryForceIndexUsesGSI(t *testing.T) {
	s := newTestServer(&fakeBackend{
		info: &dynamo.TableInfo{
			Name: "t", PartitionKey: "id",
			GSIs: []dynamo.IndexInfo{{Name: "by-email", PartitionKey: "email"}},
		},
		query: &dynamo.QueryResult{
			Items: []map[string]types.AttributeValue{{"email": &types.AttributeValueMemberS{Value: "a@b.com"}}},
			Count: 1,
		},
	})
	rec := do(s, http.MethodPost, "/tables/t/query",
		`{"conditions":[{"name":"email","op":"eq","value":"a@b.com"}],"strategy":{"mode":"query","index":"by-email"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Mode  string `json:"mode"`
		Index string `json:"index"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Mode != "query" {
		t.Fatalf("want mode query, got %q", resp.Mode)
	}
	if resp.Index != "by-email" {
		t.Fatalf("want index by-email, got %q", resp.Index)
	}
}

func TestQueryForceIndexWithoutEqualityIs400(t *testing.T) {
	s := newTestServer(&fakeBackend{
		info: &dynamo.TableInfo{
			Name: "t", PartitionKey: "id",
			GSIs: []dynamo.IndexInfo{{Name: "by-email", PartitionKey: "email"}},
		},
	})
	// begins_with on email is not an equality, so forcing the index must 400.
	rec := do(s, http.MethodPost, "/tables/t/query",
		`{"conditions":[{"name":"email","op":"begins_with","value":"a"}],"strategy":{"mode":"query","index":"by-email"}}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestQueryAutoReturnsIndexName(t *testing.T) {
	s := newTestServer(&fakeBackend{
		info: &dynamo.TableInfo{
			Name: "t", PartitionKey: "id",
			GSIs: []dynamo.IndexInfo{{Name: "by-email", PartitionKey: "email"}},
		},
		query: &dynamo.QueryResult{
			Items: []map[string]types.AttributeValue{{"email": &types.AttributeValueMemberS{Value: "a@b.com"}}},
			Count: 1,
		},
	})
	// No strategy -> auto; the planner picks the GSI; the response must report it.
	rec := do(s, http.MethodPost, "/tables/t/query",
		`{"conditions":[{"name":"email","op":"eq","value":"a@b.com"}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Mode  string `json:"mode"`
		Index string `json:"index"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Mode != "query" || resp.Index != "by-email" {
		t.Fatalf("want query/by-email, got %q/%q", resp.Mode, resp.Index)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail.**

Run: `go test ./internal/gui/ -run "TestQueryForce|TestQueryAuto" -v`
Expected: FAIL — `strategy:scan` still returns `query`; the `index` field is absent (empty); the forced-index-without-equality case returns 200 instead of 400.

- [ ] **Step 3: Add the strategy types.** In `internal/gui/server.go`, replace the existing `queryRequest` struct:

```go
type queryRequest struct {
	Conditions []queryCondition `json:"conditions"`
	Limit      int32            `json:"limit"`
	Cursor     string           `json:"cursor"`
}
```

with:

```go
type queryRequest struct {
	Conditions []queryCondition `json:"conditions"`
	Limit      int32            `json:"limit"`
	Cursor     string           `json:"cursor"`
	Strategy   queryStrategy    `json:"strategy"`
}

// queryStrategy lets the GUI override the auto planner: Mode "" / "auto" lets
// the planner decide, "scan" forces a Scan with the full filter, and "query"
// forces a Query on Index ("" = base table, otherwise a GSI name).
type queryStrategy struct {
	Mode  string `json:"mode"`
	Index string `json:"index"`
}
```

- [ ] **Step 4: Dispatch on the strategy.** In `internal/gui/server.go`, inside `handleQuery`, replace this line:

```go
	plan := query.BuildPlan(info, expr, names, values)
```

with:

```go
	var plan query.Plan
	switch req.Strategy.Mode {
	case "scan":
		plan = query.Plan{Mode: query.ModeScan, FilterExpression: expr, Names: names, Values: values}
	case "query":
		p, perr := query.PlanForIndex(info, conds, req.Strategy.Index)
		if perr != nil {
			writeError(w, http.StatusBadRequest, perr.Error())
			return
		}
		plan = p
	default:
		plan = query.BuildPlan(info, expr, names, values)
	}
```

- [ ] **Step 5: Return the chosen index.** In `internal/gui/server.go`, in the final `writeJSON` of `handleQuery`, replace:

```go
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"mode":         mode,
		"items":        items,
		"cursor":       cursor,
		"count":        count,
		"scannedCount": scannedCount,
	})
```

with:

```go
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"mode":         mode,
		"index":        plan.IndexName,
		"items":        items,
		"cursor":       cursor,
		"count":        count,
		"scannedCount": scannedCount,
	})
```

- [ ] **Step 6: Run the tests to verify they pass.**

Run: `go test ./internal/gui/ -v`
Expected: PASS (the four new tests + all existing handler tests, which send no `strategy` and so hit the `default` branch unchanged).

- [ ] **Step 7: Full Go check.**

Run: `go build ./... && go test ./...`
Expected: clean build, all packages PASS.

- [ ] **Step 8: Commit.**

```
git add internal/gui/server.go internal/gui/server_test.go
git commit -m "feat(gui): query strategy override (force index or scan) and index in response"
```
(+ trailer)

---

## Task 3: Renderer HTML — datalist + strategy-bar container

**Files:**
- Modify: `electron/renderer/index.html`

- [ ] **Step 1: Add the strategy-bar container.** In `electron/renderer/index.html`, replace:

```html
      <section id="filter-panel" class="hidden">
        <div id="filter-rows"></div>
        <div id="filter-actions">
          <button id="filter-add">+ Condition</button>
          <button id="filter-apply">Apply</button>
          <button id="filter-clear">Clear</button>
        </div>
      </section>
```

with:

```html
      <section id="filter-panel" class="hidden">
        <div id="filter-rows"></div>
        <div id="filter-actions">
          <button id="filter-add">+ Condition</button>
          <button id="filter-apply">Apply</button>
          <button id="filter-clear">Clear</button>
        </div>
        <div id="filter-strategy" class="hidden"></div>
      </section>
```

- [ ] **Step 2: Add the attribute datalist.** In `electron/renderer/index.html`, replace:

```html
  <script src="app.js"></script>
```

with:

```html
  <datalist id="attr-suggestions"></datalist>

  <script src="app.js"></script>
```

- [ ] **Step 3: Commit.**

```
git add electron/renderer/index.html
git commit -m "feat(gui): add attribute datalist and strategy-bar container to renderer HTML"
```
(+ trailer)

---

## Task 4: Renderer JS — Enter, sort, autocomplete, strategy overrides

**Files:**
- Modify: `electron/renderer/app.js`

All steps are exact string replacements against the current committed `app.js`. Apply them in order.

- [ ] **Step 1: Extend the `state` object.** Replace:

```js
const state = {
  tables: [],
  currentTable: null,
  keys: { partition: '', sort: '' },
  schemaRaw: '',
  cursor: '',
  items: [],
  conditions: [],
  filterActive: false,
  mode: '',
  scanned: 0,
  selectedIdx: -1,
  selectedItem: null,
  detailText: '',
}
```

with:

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

- [ ] **Step 2: Add the helper functions block.** Replace this line (the start of `columnOrder`):

```js
function columnOrder() {
```

with the new helpers followed by the original line:

```js
function filterKeydown(e) {
  if (e.key === 'Enter') {
    e.preventDefault()
    applyFilter()
  }
}

function buildIndexList(info) {
  const list = []
  if (info.PartitionKey) {
    list.push({ name: '', kind: 'table', pk: info.PartitionKey, sk: info.SortKey || '' })
  }
  ;(info.GSIs || []).forEach((g) => {
    list.push({ name: g.Name, kind: 'gsi', pk: g.PartitionKey || '', sk: g.SortKey || '' })
  })
  ;(info.LSIs || []).forEach((l) => {
    list.push({ name: l.Name, kind: 'lsi', pk: l.PartitionKey || '', sk: l.SortKey || '' })
  })
  return list
}

function updateAttrSuggestions() {
  const names = new Set()
  state.indexes.forEach((ix) => {
    if (ix.pk) names.add(ix.pk)
    if (ix.sk) names.add(ix.sk)
  })
  state.items.forEach((it) => Object.keys(it).forEach((k) => names.add(k)))
  const dl = $('attr-suggestions')
  dl.innerHTML = ''
  ;[...names].sort().forEach((n) => {
    const opt = document.createElement('option')
    opt.value = n
    dl.appendChild(opt)
  })
}

const DATE_NAME_RE = /(_at$|date|time|timestamp|created|updated|modified)/i
const ISO_DATE_RE = /^\d{4}-\d{2}-\d{2}/

function isDateColumn(col) {
  if (DATE_NAME_RE.test(col)) return true
  let sawValue = false
  for (const it of state.items) {
    const v = it[col]
    if (v === null || v === undefined || v === '') continue
    sawValue = true
    if (typeof v !== 'string' || !ISO_DATE_RE.test(v)) return false
  }
  return sawValue
}

function firstDirFor(col) {
  return isDateColumn(col) ? 'desc' : 'asc'
}

function onHeaderClick(col) {
  if (state.sort.column === col) {
    state.sort.dir = state.sort.dir === 'asc' ? 'desc' : 'asc'
  } else {
    state.sort.column = col
    state.sort.dir = firstDirFor(col)
  }
  renderGrid()
}

function compareValues(a, b, dateCol) {
  const am = a === null || a === undefined || a === ''
  const bm = b === null || b === undefined || b === ''
  if (am && bm) return 0
  if (am) return 1
  if (bm) return -1
  if (typeof a === 'number' && typeof b === 'number') return a - b
  if (dateCol) {
    const at = Date.parse(a), bt = Date.parse(b)
    if (!isNaN(at) && !isNaN(bt)) return at - bt
  }
  return String(cellText(a)).localeCompare(String(cellText(b)))
}

function sortedItems() {
  const col = state.sort.column
  const dateCol = isDateColumn(col)
  const sign = state.sort.dir === 'asc' ? 1 : -1
  return state.items.slice().sort((x, y) => compareValues(x[col], y[col], dateCol) * sign)
}

function strategyTarget() {
  if (state.strategy.mode === 'scan') return { kind: 'scan' }
  if (state.strategy.index) return { kind: 'gsi', name: state.strategy.index }
  return { kind: 'table' }
}

function viableIndexes() {
  const eqAttrs = new Set(
    activeConditions().filter((c) => c.op === 'eq').map((c) => c.name)
  )
  return state.indexes.filter((ix) => (ix.kind === 'table' || ix.kind === 'gsi') && ix.pk && eqAttrs.has(ix.pk))
}

function renderStrategyBar() {
  const bar = $('filter-strategy')
  if (!state.filterActive || !state.strategy.mode) {
    hide(bar)
    return
  }
  bar.innerHTML = ''
  const target = strategyTarget()
  const label = document.createElement('span')
  label.className = 'strategy-label'
  let text = 'Strategy: '
  if (target.kind === 'scan') text += 'SCAN'
  else if (target.kind === 'gsi') text += 'QUERY · index: ' + target.name
  else text += 'QUERY · table'
  if (state.override.mode === 'auto') text += ' (auto)'
  label.textContent = text
  bar.appendChild(label)

  const addBtn = (caption, override) => {
    const b = document.createElement('button')
    b.className = 'strategy-override'
    b.textContent = caption
    b.addEventListener('click', () => {
      state.override = override
      state.cursor = ''
      loadPage(true)
    })
    bar.appendChild(b)
  }

  viableIndexes().forEach((ix) => {
    if (ix.kind === 'gsi' && target.name !== ix.name) {
      addBtn('Use ' + ix.name + ' instead', { mode: 'query', index: ix.name })
    }
    if (ix.kind === 'table' && target.kind !== 'table') {
      addBtn('Use Table instead', { mode: 'query', index: '' })
    }
  })
  if (target.kind !== 'scan') {
    addBtn('Use Scan instead', { mode: 'scan', index: '' })
  }
  show(bar)
}

function columnOrder() {
```

- [ ] **Step 3: Update `selectTable`.** Replace the whole function:

```js
async function selectTable(name) {
  state.currentTable = name
  state.cursor = ''
  state.items = []
  state.conditions = []
  state.filterActive = false
  state.mode = ''
  state.scanned = 0
  state.selectedIdx = -1
  state.selectedItem = null
  $('current-table').textContent = name
  $('status').textContent = 'Loading…'
  $('mode-badge').textContent = ''
  $('schema-btn').disabled = true
  $('filter-btn').disabled = true
  $('new-item-btn').disabled = true
  $('export-json').disabled = true
  $('export-csv').disabled = true
  $('more-btn').disabled = true
  hide($('filter-panel'))
  renderFilterRows()
  renderTableList()
  try {
    const schema = await window.api.schema(name)
    state.keys = {
      partition: (schema.info && schema.info.PartitionKey) || '',
      sort: (schema.info && schema.info.SortKey) || '',
    }
    state.schemaRaw = schema.rawJSON || JSON.stringify(schema.info, null, 2)
    $('schema-btn').disabled = false
    $('filter-btn').disabled = false
    $('new-item-btn').disabled = false
    $('export-json').disabled = false
    $('export-csv').disabled = false
    await loadPage(true)
  } catch (err) {
    $('status').textContent = 'Error: ' + err.message
  }
}
```

with:

```js
async function selectTable(name) {
  state.currentTable = name
  state.cursor = ''
  state.items = []
  state.rendered = []
  state.conditions = []
  state.filterActive = false
  state.mode = ''
  state.scanned = 0
  state.indexes = []
  state.strategy = { mode: '', index: '' }
  state.override = { mode: 'auto', index: '' }
  state.sort = { column: null, dir: 'asc' }
  state.selectedIdx = -1
  state.selectedItem = null
  $('current-table').textContent = name
  $('status').textContent = 'Loading…'
  $('mode-badge').textContent = ''
  $('schema-btn').disabled = true
  $('filter-btn').disabled = true
  $('new-item-btn').disabled = true
  $('export-json').disabled = true
  $('export-csv').disabled = true
  $('more-btn').disabled = true
  hide($('filter-panel'))
  hide($('filter-strategy'))
  renderFilterRows()
  renderTableList()
  try {
    const schema = await window.api.schema(name)
    const info = schema.info || {}
    state.keys = {
      partition: info.PartitionKey || '',
      sort: info.SortKey || '',
    }
    state.indexes = buildIndexList(info)
    state.schemaRaw = schema.rawJSON || JSON.stringify(info, null, 2)
    $('schema-btn').disabled = false
    $('filter-btn').disabled = false
    $('new-item-btn').disabled = false
    $('export-json').disabled = false
    $('export-csv').disabled = false
    await loadPage(true)
    updateAttrSuggestions()
  } catch (err) {
    $('status').textContent = 'Error: ' + err.message
  }
}
```

- [ ] **Step 4: Update `loadPage`.** Replace the whole function:

```js
async function loadPage(reset) {
  const cursor = reset ? '' : state.cursor
  try {
    let data
    if (state.filterActive) {
      data = await window.api.query(state.currentTable, {
        conditions: activeConditions(),
        limit: pageSize(),
        cursor,
      })
      state.mode = data.mode || ''
    } else {
      data = await window.api.scan(state.currentTable, cursor, pageSize())
      state.mode = ''
    }
    if (reset) {
      state.items = []
      state.scanned = 0
      state.selectedIdx = -1
      state.selectedItem = null
    }
    state.items = state.items.concat(data.items || [])
    state.cursor = data.cursor || ''
    if (state.filterActive) {
      state.scanned += data.scannedCount || 0
    }
    $('more-btn').disabled = !state.cursor
    updateStatus()
    renderGrid()
  } catch (err) {
    $('status').textContent = 'Error: ' + err.message
    $('more-btn').disabled = !state.cursor
  }
}
```

with:

```js
async function loadPage(reset) {
  const cursor = reset ? '' : state.cursor
  try {
    let data
    if (state.filterActive) {
      data = await window.api.query(state.currentTable, {
        conditions: activeConditions(),
        limit: pageSize(),
        cursor,
        strategy: state.override,
      })
      state.mode = data.mode || ''
      state.strategy = { mode: data.mode || '', index: data.index || '' }
    } else {
      data = await window.api.scan(state.currentTable, cursor, pageSize())
      state.mode = ''
    }
    if (reset) {
      state.items = []
      state.scanned = 0
      state.selectedIdx = -1
      state.selectedItem = null
    }
    state.items = state.items.concat(data.items || [])
    state.cursor = data.cursor || ''
    if (state.filterActive) {
      state.scanned += data.scannedCount || 0
    }
    $('more-btn').disabled = !state.cursor
    updateStatus()
    renderGrid()
    renderStrategyBar()
    updateAttrSuggestions()
  } catch (err) {
    $('status').textContent = 'Error: ' + err.message
    $('more-btn').disabled = !state.cursor
  }
}
```

- [ ] **Step 5: Reset the override on Apply/Clear.** Replace:

```js
async function applyFilter() {
  state.filterActive = activeConditions().length > 0
  state.cursor = ''
  await loadPage(true)
}

async function clearFilter() {
  state.conditions = []
  state.filterActive = false
  renderFilterRows()
  state.cursor = ''
  await loadPage(true)
}
```

with:

```js
async function applyFilter() {
  state.filterActive = activeConditions().length > 0
  state.override = { mode: 'auto', index: '' }
  state.cursor = ''
  await loadPage(true)
}

async function clearFilter() {
  state.conditions = []
  state.filterActive = false
  state.override = { mode: 'auto', index: '' }
  renderFilterRows()
  hide($('filter-strategy'))
  state.cursor = ''
  await loadPage(true)
}
```

- [ ] **Step 6: Wire the attribute input (Enter + autocomplete).** Replace:

```js
    const nameIn = document.createElement('input')
    nameIn.type = 'text'
    nameIn.placeholder = 'attribute'
    nameIn.value = cond.name
    nameIn.addEventListener('input', () => { state.conditions[i].name = nameIn.value })
```

with:

```js
    const nameIn = document.createElement('input')
    nameIn.type = 'text'
    nameIn.placeholder = 'attribute'
    nameIn.value = cond.name
    nameIn.setAttribute('list', 'attr-suggestions')
    nameIn.autocomplete = 'off'
    nameIn.addEventListener('input', () => { state.conditions[i].name = nameIn.value })
    nameIn.addEventListener('keydown', filterKeydown)
```

- [ ] **Step 7: Wire the value input (Enter).** Replace:

```js
    const valIn = document.createElement('input')
    valIn.type = 'text'
    valIn.placeholder = 'value'
    valIn.value = cond.value
    valIn.addEventListener('input', () => { state.conditions[i].value = valIn.value })
```

with:

```js
    const valIn = document.createElement('input')
    valIn.type = 'text'
    valIn.placeholder = 'value'
    valIn.value = cond.value
    valIn.autocomplete = 'off'
    valIn.addEventListener('input', () => { state.conditions[i].value = valIn.value })
    valIn.addEventListener('keydown', filterKeydown)
```

- [ ] **Step 8: Sort-aware `renderGrid`.** Replace the whole function:

```js
function renderGrid() {
  const cols = columnOrder()
  const thead = $('grid').querySelector('thead')
  const tbody = $('grid').querySelector('tbody')
  thead.innerHTML = ''
  tbody.innerHTML = ''

  const hr = document.createElement('tr')
  cols.forEach((c) => {
    const th = document.createElement('th')
    th.textContent = c
    hr.appendChild(th)
  })
  thead.appendChild(hr)

  state.items.forEach((item, idx) => {
    const tr = document.createElement('tr')
    cols.forEach((c) => {
      const td = document.createElement('td')
      const text = cellText(item[c])
      td.textContent = text.length > 80 ? text.slice(0, 77) + '…' : text
      tr.appendChild(td)
    })
    tr.addEventListener('click', () => showItem(idx))
    tbody.appendChild(tr)
  })
}
```

with:

```js
function renderGrid() {
  const cols = columnOrder()
  const thead = $('grid').querySelector('thead')
  const tbody = $('grid').querySelector('tbody')
  thead.innerHTML = ''
  tbody.innerHTML = ''

  const view = (state.sort.column && cols.includes(state.sort.column)) ? sortedItems() : state.items
  state.rendered = view

  const hr = document.createElement('tr')
  cols.forEach((c) => {
    const th = document.createElement('th')
    let label = c
    if (state.sort.column === c) label += state.sort.dir === 'asc' ? ' ▲' : ' ▼'
    th.textContent = label
    th.className = 'sortable'
    th.addEventListener('click', () => onHeaderClick(c))
    hr.appendChild(th)
  })
  thead.appendChild(hr)

  view.forEach((item, idx) => {
    const tr = document.createElement('tr')
    cols.forEach((c) => {
      const td = document.createElement('td')
      const text = cellText(item[c])
      td.textContent = text.length > 80 ? text.slice(0, 77) + '…' : text
      tr.appendChild(td)
    })
    tr.addEventListener('click', () => showItem(idx))
    tbody.appendChild(tr)
  })
}
```

- [ ] **Step 9: Make `showItem` read the rendered view.** Replace:

```js
function showItem(idx) {
  state.selectedIdx = idx
  state.selectedItem = state.items[idx]
  openDetail('Item', JSON.stringify(state.items[idx], null, 2), true)
}
```

with:

```js
function showItem(idx) {
  const item = state.rendered[idx]
  state.selectedIdx = idx
  state.selectedItem = item
  openDetail('Item', JSON.stringify(item, null, 2), true)
}
```

- [ ] **Step 10: Verify (do NOT launch the GUI).**

Run: `node --check electron/renderer/app.js`
Expected: no output (syntax OK).

- [ ] **Step 11: Commit.**

```
git add electron/renderer/app.js
git commit -m "feat(gui): Enter-to-search, column sort, GSI strategy overrides, attribute autocomplete"
```
(+ trailer)

---

## Task 5: Renderer CSS — sortable headers + strategy bar

**Files:**
- Modify: `electron/renderer/styles.css`

- [ ] **Step 1: Append the styles.** Add these rules to the END of `electron/renderer/styles.css` (`.hidden { display: none !important; }` already exists globally, so the strategy bar hides correctly without an extra rule):

```css
#grid th.sortable { cursor: pointer; user-select: none; }
#grid th.sortable:hover { color: #7aa2f7; }
#filter-strategy { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; margin-top: 10px; padding-top: 8px; border-top: 1px solid #2a3450; }
.strategy-label { font-size: 12px; color: #828bb8; }
.strategy-override { font-size: 12px; padding: 2px 8px; }
```

- [ ] **Step 2: Commit.**

```
git add electron/renderer/styles.css
git commit -m "style(gui): sortable headers and strategy-bar styling"
```
(+ trailer)

---

## Task 6: Final verification

**Files:** none (verification only).

- [ ] **Step 1: Go build + tests.**

Run: `go build ./... && go test ./...`
Expected: clean build; all packages PASS (includes the new `query` and `gui` tests).

- [ ] **Step 2: Renderer syntax check.**

Run: `node --check electron/renderer/app.js`
Expected: no output.

- [ ] **Step 3: Manual smoke test (Estevao — runs the live GUI).** Launch `go run . gui` against a table that has a GSI and a date/ISO-timestamp column, then confirm:
  - Type an attribute name (e.g. `to`) → the datalist suggests `topology_id`, `topology_name`, etc.
  - Fill a condition and press **Enter** → the search runs (no need to click Apply).
  - Click a **date** column header → rows sort newest→oldest; click again → oldest→newest (▼/▲ shown).
  - Click a **text** column header → A→Z, then Z→A.
  - Apply a filter whose first condition is `=` on a GSI partition key → the strategy bar shows `QUERY · index: <gsi> (auto)`.
  - Apply a filter whose first condition is a non-key attribute, but which also has `=` on a GSI PK → strategy shows `SCAN (auto)` with a `Use <gsi> instead` button; click it → becomes `QUERY · index: <gsi>`.
  - Click **Use Scan instead** → strategy becomes `SCAN`; **Resume fetching** keeps the same strategy.
  - Verify the TUI still filters/queries normally (`BuildPlan` untouched).

---

## Self-review (performed against the spec)

**1. Spec coverage**

| Spec section | Task(s) |
|---|---|
| §3 Enter to search | Task 4 (Steps 2, 6, 7 — `filterKeydown` on both inputs) |
| §4 Column sort (date-desc-first, type-aware, client-side, reset on table change) | Task 4 (Step 2 helpers: `isDateColumn`/`firstDirFor`/`compareValues`/`sortedItems`/`onHeaderClick`; Step 3 reset; Step 8 render; Step 9 `showItem`) + Task 5 |
| §5.2 HTTP contract (`strategy` in / `index` out) | Task 2 (Steps 3, 5) |
| §5.3 server dispatch | Task 2 (Step 4) |
| §5.4 `PlanForIndex` | Task 1 |
| §5.5 renderer strategy bar + overrides + override lifecycle | Task 3 (Step 1) + Task 4 (Steps 1, 2 `renderStrategyBar`/`viableIndexes`/`strategyTarget`, 3, 4, 5) + Task 5 |
| §6 attribute autocomplete | Task 3 (Step 2) + Task 4 (Steps 2 `updateAttrSuggestions`/`buildIndexList`, 3, 4, 6) |
| §9 testing (Go unit + node --check, no AWS) | Tasks 1, 2, 6 |

No gaps.

**2. Placeholder scan:** No TBD/TODO/"handle errors"/"similar to". Every code step contains the complete artifact (full function bodies for replacements, full new code for the helper block). ✓

**3. Type/name consistency:**
- Go: `queryStrategy{Mode,Index}` (JSON `mode`/`index`) ↔ renderer sends `strategy: state.override` where `override = {mode, index}` and reads `data.index`. `PlanForIndex(info, conds, indexName)` called in Task 2 Step 4 matches the signature defined in Task 1 Step 3. Response key `index` (Task 2 Step 5) ↔ `data.index` (Task 4 Step 4). ✓
- Renderer: `state.rendered` set in `renderGrid` (Task 4 Step 8) and read in `showItem` (Step 9). `state.indexes` built by `buildIndexList` (Step 2) in `selectTable` (Step 3), consumed by `viableIndexes`/`updateAttrSuggestions` (Step 2). `state.override`/`state.strategy` set in `loadPage` (Step 4) & `applyFilter`/`clearFilter` (Step 5), read by `renderStrategyBar` (Step 2). `filterKeydown` defined (Step 2) and attached (Steps 6, 7). New element IDs `filter-strategy` and `attr-suggestions` (Task 3) referenced by `renderStrategyBar`/`hide` and `updateAttrSuggestions`/`nameIn.list`. ✓
- `BuildPlan` signature unchanged → TUI (`internal/app/app.go:1241`) and existing tests unaffected. ✓

**4. Ambiguity:** `isDateColumn` epoch handling is name-gated (numeric values only count as dates when the name matches `DATE_NAME_RE`, since `compareValues` sorts equal numbers numerically and the date branch only applies to string parsing). Override targets are limited to the table + GSIs (LSIs are display-only via `buildIndexList` `kind:'lsi'`, excluded by `viableIndexes`). ✓
