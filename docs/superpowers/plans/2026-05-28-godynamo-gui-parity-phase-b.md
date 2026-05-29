# GoDynamo GUI Parity — Phase B (Writes) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add create/edit/delete items and create-table to the GUI, reusing the existing `dynamo.Client` write methods through three new bridge endpoints and a renderer JSON-editor modal + create-table form.

**Architecture:** Extend the `gui.Backend` interface with `PutItem`/`DeleteItem`/`CreateTable` (already on `*dynamo.Client`); add `POST /tables/{name}/item`, `DELETE /tables/{name}/item`, and `POST /tables` to the bridge; add an editor modal (new/edit), a delete confirm, and a create-table form to the renderer. No changes to `internal/dynamo`, `internal/models`, `internal/query`, or the TUI.

**Tech Stack:** Go 1.24 stdlib `net/http`; existing `internal/dynamo` (`PutItem`/`DeleteItem`/`CreateTable`/`CreateTableInput`) + `internal/models` (`JSONToItem`); Electron (vanilla renderer).

**Source spec:** `docs/superpowers/specs/2026-05-28-godynamo-gui-parity-design.md`

---

## Conventions

- **No new Go dependencies**; stdlib + existing modules. `go.mod`/`go.sum` untouched.
- **No real AWS in automated steps.** Go tests use the fake `Backend`. Estevao runs live tests.
- **Never run `go run . gui` / `npm start`** (they block). Verify with `go test ./...`, `go vet ./...`, `go build ./...`, `node --check`.
- **Commit trailer:** every commit ends with a blank line then `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`. No backticks in commit messages.

## File structure

```
internal/gui/backend.go      # MODIFY — add PutItem/DeleteItem/CreateTable to Backend
internal/gui/server.go       # MODIFY — add 3 write routes + handlers + request types
internal/gui/server_test.go  # MODIFY — fake gains write methods + capture fields; add write tests
electron/preload.js          # MODIFY — add saveItem/deleteItem/createTable
electron/renderer/index.html # REWRITE — New-item + New-table buttons, item Edit/Delete, editor + create-table + confirm modals
electron/renderer/app.js     # REWRITE — editor/create-table/delete logic + wiring
electron/renderer/styles.css # MODIFY — append modal/form styles
```

---

## Task 1: Bridge write endpoints (Backend + server + tests)

**Files:**
- Modify: `internal/gui/backend.go`
- Modify: `internal/gui/server.go`
- Test: `internal/gui/server_test.go`

- [ ] **Step 1: Write the failing tests** — in `internal/gui/server_test.go`:

(a) Replace the `fakeBackend` struct (currently 9 lines, fields `tables/info/scan/scanErr/query/queryErr`) with this extended version:
```go
type fakeBackend struct {
	tables   []string
	info     *dynamo.TableInfo
	scan     *dynamo.ScanResult
	scanErr  error
	query    *dynamo.QueryResult
	queryErr error
	putItem   map[string]types.AttributeValue
	putErr    error
	deleteKey map[string]types.AttributeValue
	deleteErr error
	createIn  dynamo.CreateTableInput
	createErr error
}
```

(b) Add these three methods right after the existing `QueryTable` method on `fakeBackend`:
```go
func (f *fakeBackend) PutItem(ctx context.Context, tableName string, item map[string]types.AttributeValue) error {
	f.putItem = item
	return f.putErr
}

func (f *fakeBackend) DeleteItem(ctx context.Context, tableName string, key map[string]types.AttributeValue) error {
	f.deleteKey = key
	return f.deleteErr
}

func (f *fakeBackend) CreateTable(ctx context.Context, input dynamo.CreateTableInput) error {
	f.createIn = input
	return f.createErr
}
```

(c) Append these tests to the END of the file:
```go
func TestPutItem(t *testing.T) {
	f := &fakeBackend{}
	s := newTestServer(f)
	rec := do(s, http.MethodPost, "/tables/t/item", `{"json":"{\"id\":\"1\",\"name\":\"Alice\"}"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if f.putItem["id"] == nil {
		t.Fatalf("expected item passed to PutItem, got %v", f.putItem)
	}
}

func TestPutItemInvalidJSON(t *testing.T) {
	s := newTestServer(&fakeBackend{})
	rec := do(s, http.MethodPost, "/tables/t/item", `{"json":"not json"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestDeleteItemDerivesKey(t *testing.T) {
	f := &fakeBackend{info: &dynamo.TableInfo{PartitionKey: "id", SortKey: "sk"}}
	s := newTestServer(f)
	rec := do(s, http.MethodDelete, "/tables/t/item", `{"json":"{\"id\":\"1\",\"sk\":\"a\",\"extra\":\"x\"}"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if len(f.deleteKey) != 2 {
		t.Fatalf("expected key with pk+sk only, got %v", f.deleteKey)
	}
	if f.deleteKey["id"] == nil || f.deleteKey["sk"] == nil {
		t.Fatalf("key missing pk/sk: %v", f.deleteKey)
	}
}

func TestDeleteItemMissingKey(t *testing.T) {
	f := &fakeBackend{info: &dynamo.TableInfo{PartitionKey: "id"}}
	s := newTestServer(f)
	rec := do(s, http.MethodDelete, "/tables/t/item", `{"json":"{\"other\":\"x\"}"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestCreateTable(t *testing.T) {
	f := &fakeBackend{}
	s := newTestServer(f)
	rec := do(s, http.MethodPost, "/tables", `{"name":"NewT","pk":"id","pkType":"S","billingMode":"PAY_PER_REQUEST"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if f.createIn.TableName != "NewT" || f.createIn.PartitionKey != "id" {
		t.Fatalf("createIn=%+v", f.createIn)
	}
}

func TestCreateTableValidates(t *testing.T) {
	s := newTestServer(&fakeBackend{})
	rec := do(s, http.MethodPost, "/tables", `{"pk":"id"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestWriteNotConnected(t *testing.T) {
	s := newServer("test-token")
	rec := do(s, http.MethodPost, "/tables/t/item", `{"json":"{}"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d", rec.Code)
	}
}
```

- [ ] **Step 2: Run tests, verify they FAIL**

Run: `go test ./internal/gui/ -v`
Expected: red — `*fakeBackend` no longer satisfies `Backend` once the interface is extended, and the write routes 404. (Before Step 3/4 it fails to compile or routes are missing.)

- [ ] **Step 3: Extend the `Backend` interface** — in `internal/gui/backend.go`, add three methods after `QueryTable`:
```go
type Backend interface {
	ListTables(ctx context.Context) ([]string, error)
	DescribeTable(ctx context.Context, name string) (*dynamo.TableInfo, error)
	ScanTable(ctx context.Context, name string, limit int32,
		startKey map[string]types.AttributeValue,
		filterExpr string, names map[string]string, values map[string]interface{}) (*dynamo.ScanResult, error)
	QueryTable(ctx context.Context, input dynamo.QueryInput) (*dynamo.QueryResult, error)
	PutItem(ctx context.Context, tableName string, item map[string]types.AttributeValue) error
	DeleteItem(ctx context.Context, tableName string, key map[string]types.AttributeValue) error
	CreateTable(ctx context.Context, input dynamo.CreateTableInput) error
}
```
(The existing `var _ Backend = (*dynamo.Client)(nil)` now also verifies these three exist on `*dynamo.Client` — they do.)

- [ ] **Step 4: Add routes, request types, and handlers** — in `internal/gui/server.go`:

(a) In `buildHandler`, add three routes after the `/query` line:
```go
	mux.HandleFunc("POST /tables/{name}/query", s.handleQuery)
	mux.HandleFunc("POST /tables/{name}/item", s.handlePutItem)
	mux.HandleFunc("DELETE /tables/{name}/item", s.handleDeleteItem)
	mux.HandleFunc("POST /tables", s.handleCreateTable)
```

(b) Add these request types + handlers immediately BEFORE `func writeJSON`:
```go
type itemRequest struct {
	JSON string `json:"json"`
}

type createTableRequest struct {
	Name        string `json:"name"`
	PK          string `json:"pk"`
	PKType      string `json:"pkType"`
	SK          string `json:"sk"`
	SKType      string `json:"skType"`
	BillingMode string `json:"billingMode"`
	RCU         int64  `json:"rcu"`
	WCU         int64  `json:"wcu"`
}

func (s *server) handlePutItem(w http.ResponseWriter, r *http.Request) {
	backend, ok := s.getBackend()
	if !ok {
		writeError(w, http.StatusConflict, "not connected")
		return
	}
	name := r.PathValue("name")

	var req itemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	item, err := models.JSONToItem(req.JSON)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := backend.PutItem(r.Context(), name, item); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

func (s *server) handleDeleteItem(w http.ResponseWriter, r *http.Request) {
	backend, ok := s.getBackend()
	if !ok {
		writeError(w, http.StatusConflict, "not connected")
		return
	}
	name := r.PathValue("name")

	var req itemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	item, err := models.JSONToItem(req.JSON)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	info, err := backend.DescribeTable(r.Context(), name)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	key := make(map[string]types.AttributeValue)
	if info.PartitionKey != "" {
		v, present := item[info.PartitionKey]
		if !present {
			writeError(w, http.StatusBadRequest, "item is missing the partition key: "+info.PartitionKey)
			return
		}
		key[info.PartitionKey] = v
	}
	if info.SortKey != "" {
		v, present := item[info.SortKey]
		if !present {
			writeError(w, http.StatusBadRequest, "item is missing the sort key: "+info.SortKey)
			return
		}
		key[info.SortKey] = v
	}

	if err := backend.DeleteItem(r.Context(), name, key); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

func (s *server) handleCreateTable(w http.ResponseWriter, r *http.Request) {
	backend, ok := s.getBackend()
	if !ok {
		writeError(w, http.StatusConflict, "not connected")
		return
	}

	var req createTableRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" || req.PK == "" {
		writeError(w, http.StatusBadRequest, "table name and partition key are required")
		return
	}

	input := dynamo.CreateTableInput{
		TableName:     req.Name,
		PartitionKey:  req.PK,
		PartitionType: strings.ToUpper(req.PKType),
		SortKey:       req.SK,
		SortKeyType:   strings.ToUpper(req.SKType),
		BillingMode:   req.BillingMode,
		ReadCapacity:  req.RCU,
		WriteCapacity: req.WCU,
	}
	if err := backend.CreateTable(r.Context(), input); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}
```
(All imports needed — `json`, `http`, `strings`, `dynamo`, `models`, `types` — are already imported in `server.go`.)

- [ ] **Step 5: Run tests, verify PASS**

Run: `go test ./internal/gui/ -v` (all existing + 7 new pass). `go vet ./internal/gui/` (clean). `go build ./...` (clean).

- [ ] **Step 6: Commit**

```
git add internal/gui/backend.go internal/gui/server.go internal/gui/server_test.go
git commit -m "feat(gui): add item put/delete and create-table bridge endpoints"
```
(+ trailer)

---

## Task 2: Renderer write UI (editor, delete, create-table)

**Files:**
- Modify: `electron/preload.js`
- Modify: `electron/renderer/index.html` (rewrite)
- Modify: `electron/renderer/app.js` (rewrite)
- Modify: `electron/renderer/styles.css` (append)

- [ ] **Step 1: `electron/preload.js` — add write methods.** Replace the closing of the `exposeInMainWorld` object (currently):
```js
  query: (name, body) => call('POST', `/tables/${encodeURIComponent(name)}/query`, body),
})
```
with:
```js
  query: (name, body) => call('POST', `/tables/${encodeURIComponent(name)}/query`, body),
  saveItem: (name, json) => call('POST', `/tables/${encodeURIComponent(name)}/item`, { json }),
  deleteItem: (name, json) => call('DELETE', `/tables/${encodeURIComponent(name)}/item`, { json }),
  createTable: (form) => call('POST', '/tables', form),
})
```

- [ ] **Step 2: `electron/renderer/index.html` — REWRITE.** Read it first, then WRITE exactly:
```html
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta http-equiv="Content-Security-Policy"
        content="default-src 'self'; script-src 'self'; connect-src http://127.0.0.1:* http://localhost:*; style-src 'self' 'unsafe-inline';" />
  <title>GoDynamo</title>
  <link rel="stylesheet" href="styles.css" />
</head>
<body>
  <section id="connect-screen" class="screen">
    <div class="connect-card">
      <h1>⚡ GoDynamo</h1>
      <div class="field">
        <label><input type="radio" name="mode" value="aws" checked /> AWS</label>
        <label><input type="radio" name="mode" value="local" /> DynamoDB Local</label>
      </div>
      <div class="field" id="aws-fields">
        <label for="region">Region</label>
        <select id="region"></select>
      </div>
      <div class="field hidden" id="local-fields">
        <label for="endpoint">Endpoint</label>
        <input type="text" id="endpoint" value="http://localhost:8000" />
      </div>
      <button id="connect-btn">Connect</button>
      <p id="connect-error" class="error"></p>
    </div>
  </section>

  <section id="main-screen" class="screen hidden">
    <aside id="sidebar">
      <button id="create-table-btn">+ New table</button>
      <input type="text" id="table-filter" placeholder="Filter tables…" />
      <ul id="table-list"></ul>
    </aside>
    <main id="content">
      <header id="toolbar">
        <span id="current-table"></span>
        <button id="new-item-btn" disabled>New item</button>
        <button id="filter-btn" disabled>Filter</button>
        <button id="schema-btn" disabled>Schema</button>
        <label id="pagesize-label">Page size
          <select id="page-size">
            <option>50</option>
            <option>100</option>
            <option>300</option>
            <option selected>500</option>
            <option>1000</option>
          </select>
        </label>
        <button id="more-btn" disabled>Resume fetching</button>
        <span id="mode-badge"></span>
        <span id="status"></span>
      </header>
      <section id="filter-panel" class="hidden">
        <div id="filter-rows"></div>
        <div id="filter-actions">
          <button id="filter-add">+ Condition</button>
          <button id="filter-apply">Apply</button>
          <button id="filter-clear">Clear</button>
        </div>
      </section>
      <div id="grid-wrap">
        <table id="grid"><thead></thead><tbody></tbody></table>
      </div>
    </main>
  </section>

  <div id="detail" class="hidden">
    <div class="detail-card">
      <header>
        <span id="detail-title"></span>
        <button id="detail-edit" class="hidden">Edit</button>
        <button id="detail-delete" class="hidden">Delete</button>
        <button id="detail-close">✕</button>
      </header>
      <pre id="detail-body"></pre>
    </div>
  </div>

  <div id="editor" class="hidden">
    <div class="modal-card">
      <header>
        <span id="editor-title"></span>
        <button id="editor-close">✕</button>
      </header>
      <textarea id="editor-text" spellcheck="false"></textarea>
      <p id="editor-error" class="error"></p>
      <div class="modal-actions">
        <button id="editor-save">Save</button>
      </div>
    </div>
  </div>

  <div id="createtable" class="hidden">
    <div class="modal-card">
      <header>
        <span>Create table</span>
        <button id="ct-close">✕</button>
      </header>
      <div class="ct-form">
        <label>Table name <input type="text" id="ct-name" /></label>
        <label>Partition key <input type="text" id="ct-pk" /></label>
        <label>Partition key type
          <select id="ct-pktype"><option>S</option><option>N</option><option>B</option></select>
        </label>
        <label>Sort key (optional) <input type="text" id="ct-sk" /></label>
        <label>Sort key type
          <select id="ct-sktype"><option>S</option><option>N</option><option>B</option></select>
        </label>
        <label>Billing mode
          <select id="ct-billing"><option>PAY_PER_REQUEST</option><option>PROVISIONED</option></select>
        </label>
        <label>Read capacity <input type="number" id="ct-rcu" value="5" /></label>
        <label>Write capacity <input type="number" id="ct-wcu" value="5" /></label>
      </div>
      <p id="ct-error" class="error"></p>
      <div class="modal-actions">
        <button id="ct-create">Create</button>
      </div>
    </div>
  </div>

  <div id="confirm" class="hidden">
    <div class="modal-card small">
      <p id="confirm-text"></p>
      <div class="modal-actions">
        <button id="confirm-no">Cancel</button>
        <button id="confirm-yes">Delete</button>
      </div>
    </div>
  </div>

  <script src="app.js"></script>
</body>
</html>
```

- [ ] **Step 3: `electron/renderer/app.js` — REWRITE.** Read it first, then WRITE exactly:
```js
const AWS_REGIONS = [
  'us-east-1','us-east-2','us-west-1','us-west-2','af-south-1','ap-east-1',
  'ap-south-1','ap-south-2','ap-northeast-1','ap-northeast-2','ap-northeast-3',
  'ap-southeast-1','ap-southeast-2','ap-southeast-3','ap-southeast-4',
  'ca-central-1','eu-central-1','eu-central-2','eu-west-1','eu-west-2','eu-west-3',
  'eu-south-1','eu-south-2','eu-north-1','il-central-1','me-south-1','me-central-1','sa-east-1',
]

const OPERATORS = [
  { op: 'eq', label: '= Equals' },
  { op: 'ne', label: '≠ Not Equals' },
  { op: 'gt', label: '> Greater Than' },
  { op: 'lt', label: '< Less Than' },
  { op: 'ge', label: '≥ Greater or Equal' },
  { op: 'le', label: '≤ Less or Equal' },
  { op: 'contains', label: '∋ Contains' },
  { op: 'not_contains', label: '∌ Not Contains' },
  { op: 'begins_with', label: '^ Begins With' },
  { op: 'exists', label: '∃ Exists' },
  { op: 'not_exists', label: '∄ Not Exists' },
]

const VALUE_OPS = new Set(['eq', 'ne', 'gt', 'lt', 'ge', 'le', 'contains', 'not_contains', 'begins_with'])

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
}

const $ = (id) => document.getElementById(id)
const show = (el) => el.classList.remove('hidden')
const hide = (el) => el.classList.add('hidden')

function initConnectScreen() {
  const regionSel = $('region')
  AWS_REGIONS.forEach((r) => {
    const opt = document.createElement('option')
    opt.value = r
    opt.textContent = r
    regionSel.appendChild(opt)
  })
  document.querySelectorAll('input[name="mode"]').forEach((radio) => {
    radio.addEventListener('change', () => {
      const mode = document.querySelector('input[name="mode"]:checked').value
      if (mode === 'aws') { show($('aws-fields')); hide($('local-fields')) }
      else { hide($('aws-fields')); show($('local-fields')) }
    })
  })
  $('connect-btn').addEventListener('click', onConnect)
}

async function onConnect() {
  const mode = document.querySelector('input[name="mode"]:checked').value
  const cfg = { mode }
  if (mode === 'aws') cfg.region = $('region').value
  else cfg.endpoint = $('endpoint').value

  $('connect-error').textContent = ''
  $('connect-btn').disabled = true
  try {
    const data = await window.api.connect(cfg)
    state.tables = data.tables || []
    renderTableList()
    hide($('connect-screen'))
    show($('main-screen'))
  } catch (err) {
    $('connect-error').textContent = err.message
  } finally {
    $('connect-btn').disabled = false
  }
}

function renderTableList() {
  const filter = $('table-filter').value.toLowerCase()
  const ul = $('table-list')
  ul.innerHTML = ''
  state.tables
    .filter((t) => t.toLowerCase().includes(filter))
    .forEach((t) => {
      const li = document.createElement('li')
      li.textContent = t
      li.className = t === state.currentTable ? 'active' : ''
      li.addEventListener('click', () => selectTable(t))
      ul.appendChild(li)
    })
}

async function refreshTables() {
  const data = await window.api.listTables()
  state.tables = data.tables || []
  renderTableList()
}

async function selectTable(name) {
  state.currentTable = name
  state.cursor = ''
  state.items = []
  state.conditions = []
  state.filterActive = false
  state.mode = ''
  state.scanned = 0
  state.selectedIdx = -1
  $('current-table').textContent = name
  $('status').textContent = 'Loading…'
  $('mode-badge').textContent = ''
  $('schema-btn').disabled = true
  $('filter-btn').disabled = true
  $('new-item-btn').disabled = true
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
    await loadPage(true)
  } catch (err) {
    $('status').textContent = 'Error: ' + err.message
  }
}

function pageSize() {
  return parseInt($('page-size').value, 10) || 500
}

function activeConditions() {
  return state.conditions
    .filter((c) => c.name.trim() !== '' && (!VALUE_OPS.has(c.op) || c.value.trim() !== ''))
    .map((c) => ({ name: c.name, op: c.op, value: c.value }))
}

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

function updateStatus() {
  let s = `${state.items.length} returned`
  if (state.filterActive) {
    s += ` · scanned ${state.scanned}`
    $('mode-badge').textContent = state.mode ? state.mode.toUpperCase() : ''
  } else {
    $('mode-badge').textContent = ''
  }
  if (state.cursor) s += ' · more available'
  $('status').textContent = s
}

function renderFilterRows() {
  const wrap = $('filter-rows')
  wrap.innerHTML = ''
  state.conditions.forEach((cond, i) => {
    const row = document.createElement('div')
    row.className = 'filter-row'

    const nameIn = document.createElement('input')
    nameIn.type = 'text'
    nameIn.placeholder = 'attribute'
    nameIn.value = cond.name
    nameIn.addEventListener('input', () => { state.conditions[i].name = nameIn.value })

    const opSel = document.createElement('select')
    OPERATORS.forEach((o) => {
      const opt = document.createElement('option')
      opt.value = o.op
      opt.textContent = o.label
      if (o.op === cond.op) opt.selected = true
      opSel.appendChild(opt)
    })
    opSel.addEventListener('change', () => { state.conditions[i].op = opSel.value })

    const valIn = document.createElement('input')
    valIn.type = 'text'
    valIn.placeholder = 'value'
    valIn.value = cond.value
    valIn.addEventListener('input', () => { state.conditions[i].value = valIn.value })

    const rm = document.createElement('button')
    rm.textContent = '✕'
    rm.className = 'filter-remove'
    rm.addEventListener('click', () => removeCondition(i))

    row.appendChild(nameIn)
    row.appendChild(opSel)
    row.appendChild(valIn)
    row.appendChild(rm)
    wrap.appendChild(row)
  })
}

function addCondition() {
  state.conditions.push({ name: '', op: 'eq', value: '' })
  renderFilterRows()
}

function removeCondition(i) {
  state.conditions.splice(i, 1)
  renderFilterRows()
}

function toggleFilter() {
  const panel = $('filter-panel')
  if (panel.classList.contains('hidden')) {
    if (state.conditions.length === 0) addCondition()
    show(panel)
  } else {
    hide(panel)
  }
}

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

function columnOrder() {
  const cols = new Set()
  state.items.forEach((it) => Object.keys(it).forEach((k) => cols.add(k)))
  const { partition, sort } = state.keys
  const ordered = []
  if (partition && cols.has(partition)) { ordered.push(partition); cols.delete(partition) }
  if (sort && cols.has(sort)) { ordered.push(sort); cols.delete(sort) }
  return ordered.concat([...cols].sort())
}

function cellText(value) {
  if (value === null || value === undefined) return ''
  if (typeof value === 'object') return JSON.stringify(value)
  return String(value)
}

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

function showItem(idx) {
  state.selectedIdx = idx
  $('detail-title').textContent = 'Item'
  $('detail-body').textContent = JSON.stringify(state.items[idx], null, 2)
  show($('detail-edit'))
  show($('detail-delete'))
  show($('detail'))
}

function showSchema() {
  $('detail-title').textContent = 'Schema: ' + state.currentTable
  $('detail-body').textContent = state.schemaRaw || ''
  hide($('detail-edit'))
  hide($('detail-delete'))
  show($('detail'))
}

function openNewItem() {
  $('editor-title').textContent = 'New item'
  $('editor-text').value = '{\n  \n}'
  $('editor-error').textContent = ''
  show($('editor'))
}

function openEditItem() {
  if (state.selectedIdx < 0) return
  $('editor-title').textContent = 'Edit item'
  $('editor-text').value = JSON.stringify(state.items[state.selectedIdx], null, 2)
  $('editor-error').textContent = ''
  hide($('detail'))
  show($('editor'))
}

async function saveEditor() {
  const text = $('editor-text').value
  try {
    JSON.parse(text)
  } catch (e) {
    $('editor-error').textContent = 'Invalid JSON: ' + e.message
    return
  }
  $('editor-error').textContent = ''
  $('editor-save').disabled = true
  try {
    await window.api.saveItem(state.currentTable, text)
    hide($('editor'))
    await loadPage(true)
  } catch (err) {
    $('editor-error').textContent = err.message
  } finally {
    $('editor-save').disabled = false
  }
}

function confirmDelete() {
  if (state.selectedIdx < 0) return
  $('confirm-text').textContent = 'Delete this item? This cannot be undone.'
  show($('confirm'))
}

async function doDelete() {
  hide($('confirm'))
  if (state.selectedIdx < 0) return
  const json = JSON.stringify(state.items[state.selectedIdx])
  try {
    await window.api.deleteItem(state.currentTable, json)
    hide($('detail'))
    await loadPage(true)
  } catch (err) {
    $('status').textContent = 'Error: ' + err.message
  }
}

function openCreateTable() {
  $('ct-name').value = ''
  $('ct-pk').value = ''
  $('ct-pktype').value = 'S'
  $('ct-sk').value = ''
  $('ct-sktype').value = 'S'
  $('ct-billing').value = 'PAY_PER_REQUEST'
  $('ct-rcu').value = '5'
  $('ct-wcu').value = '5'
  $('ct-error').textContent = ''
  show($('createtable'))
}

async function submitCreateTable() {
  const form = {
    name: $('ct-name').value.trim(),
    pk: $('ct-pk').value.trim(),
    pkType: $('ct-pktype').value,
    sk: $('ct-sk').value.trim(),
    skType: $('ct-sktype').value,
    billingMode: $('ct-billing').value,
    rcu: parseInt($('ct-rcu').value, 10) || 0,
    wcu: parseInt($('ct-wcu').value, 10) || 0,
  }
  if (!form.name || !form.pk) {
    $('ct-error').textContent = 'Table name and partition key are required.'
    return
  }
  $('ct-error').textContent = ''
  $('ct-create').disabled = true
  try {
    await window.api.createTable(form)
    hide($('createtable'))
    await refreshTables()
  } catch (err) {
    $('ct-error').textContent = err.message
  } finally {
    $('ct-create').disabled = false
  }
}

window.addEventListener('DOMContentLoaded', () => {
  initConnectScreen()
  $('table-filter').addEventListener('input', renderTableList)
  $('schema-btn').addEventListener('click', showSchema)
  $('filter-btn').addEventListener('click', toggleFilter)
  $('filter-add').addEventListener('click', addCondition)
  $('filter-apply').addEventListener('click', applyFilter)
  $('filter-clear').addEventListener('click', clearFilter)
  $('more-btn').addEventListener('click', () => loadPage(false))
  $('page-size').addEventListener('change', () => { if (state.currentTable) loadPage(true) })
  $('detail-close').addEventListener('click', () => hide($('detail')))
  $('new-item-btn').addEventListener('click', openNewItem)
  $('create-table-btn').addEventListener('click', openCreateTable)
  $('detail-edit').addEventListener('click', openEditItem)
  $('detail-delete').addEventListener('click', confirmDelete)
  $('editor-close').addEventListener('click', () => hide($('editor')))
  $('editor-save').addEventListener('click', saveEditor)
  $('ct-close').addEventListener('click', () => hide($('createtable')))
  $('ct-create').addEventListener('click', submitCreateTable)
  $('confirm-no').addEventListener('click', () => hide($('confirm')))
  $('confirm-yes').addEventListener('click', doDelete)
})
```

- [ ] **Step 4: `electron/renderer/styles.css` — APPEND** these rules to the END of the file:
```css
#create-table-btn { width: 100%; margin-bottom: 8px; }
#editor, #createtable, #confirm { position: fixed; inset: 0; background: rgba(0,0,0,0.6); display: flex; align-items: center; justify-content: center; }
.modal-card { background: #131a2b; border: 1px solid #2a3450; border-radius: 8px; width: 60%; max-width: 720px; max-height: 85%; display: flex; flex-direction: column; }
.modal-card.small { width: 360px; max-width: 360px; padding: 20px; }
.modal-card header { display: flex; align-items: center; padding: 12px; border-bottom: 1px solid #2a3450; }
.modal-card header span { font-weight: bold; }
.modal-card header button { margin-left: auto; }
#editor-text { min-height: 320px; margin: 12px; background: #0b0f1a; color: #c8d3f5; border: 1px solid #2a3450; border-radius: 6px; padding: 8px; font-family: 'Cascadia Code', monospace; font-size: 12px; resize: vertical; }
.modal-actions { display: flex; gap: 8px; justify-content: flex-end; padding: 12px; }
.ct-form { display: flex; flex-direction: column; gap: 8px; padding: 16px; overflow-y: auto; }
.ct-form label { display: flex; flex-direction: column; gap: 4px; font-size: 13px; }
#editor .error, #createtable .error { padding: 0 12px; }
.detail-card header { gap: 8px; }
#detail-title { margin-right: auto; }
.detail-card header button { margin-left: 0; }
```

- [ ] **Step 5: Verify (do NOT launch the GUI)**

Run: `node --check electron/renderer/app.js` (no output), `node --check electron/preload.js` (no output), `go build ./...` (clean).

- [ ] **Step 6: Commit**

```
git add electron/preload.js electron/renderer/index.html electron/renderer/app.js electron/renderer/styles.css
git commit -m "feat(gui): add new/edit/delete item editor and create-table form to the renderer"
```
(+ trailer)

---

## Self-review (performed against the spec)

**1. Spec coverage**

| Spec requirement | Task |
|---|---|
| `Backend.PutItem/DeleteItem/CreateTable` + fake | Task 1 |
| `POST /tables/{name}/item` (JSONToItem → PutItem; 400 on bad JSON) | Task 1 (`handlePutItem`) |
| `DELETE /tables/{name}/item` (key from tableInfo PK/SK → DeleteItem) | Task 1 (`handleDeleteItem`) |
| `POST /tables` (create-table form → CreateTable) | Task 1 (`handleCreateTable`) |
| `{ok:true}` responses; handler tests | Task 1 |
| preload `saveItem/deleteItem/createTable` | Task 2 (Step 1) |
| JSON-editor modal (new + edit) with client-side validation | Task 2 (`openNewItem`/`openEditItem`/`saveEditor`) |
| Delete confirmation | Task 2 (`confirmDelete`/`doDelete`) |
| Create-table form | Task 2 (`openCreateTable`/`submitCreateTable`) |
| Reload page after put/delete; reload tables after create | Task 2 (`loadPage(true)` / `refreshTables`) |
| New-item + Create-table buttons; Edit/Delete in item detail | Task 2 (index.html + wiring) |

No gaps.

**2. Placeholder scan:** No TBD/TODO/"handle errors". Complete file contents / full handlers in every step. ✓

**3. Type/name consistency:** `itemRequest{JSON}` / `createTableRequest{Name,PK,PKType,SK,SKType,BillingMode,RCU,WCU}` JSON keys (`json`, `name`,`pk`,`pkType`,`sk`,`skType`,`billingMode`,`rcu`,`wcu`) match the preload `saveItem(name,json)→{json}` / `createTable(form)` and the renderer `submitCreateTable` form object. `dynamo.CreateTableInput` fields (`TableName,PartitionKey,PartitionType,SortKey,SortKeyType,BillingMode,ReadCapacity,WriteCapacity`) match `internal/dynamo/client.go`. Element IDs added in index.html (`new-item-btn, create-table-btn, detail-edit, detail-delete, editor*, ct-*, confirm*`) all match app.js references. ✓
