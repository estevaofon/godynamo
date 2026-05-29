# GoDynamo GUI Profiles + Region-Grouped Sidebar Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the GUI profile-aware and multi-region like DynamoBase — no connect screen, pick an AWS profile from `~/.aws/credentials` (default selected), and browse a left-side tree of only the regions that have tables, with tabs from different regions open at once.

**Architecture:** Additive `internal/dynamo` changes (a `Profile` field + a `profile` param on discovery + a profile-file parser). The bridge replaces its single backend with a per-(profile, region) client registry keyed by region, exposes `/profiles` and `/discover`, and takes a `region` query param on every data endpoint. The renderer drops the connect screen, adds a profile dropdown + Refresh, renders a region tree, and makes tabs/data-calls region-aware.

**Tech Stack:** Go 1.x (`net/http`, `aws-sdk-go-v2/config`), vanilla Electron renderer (no framework).

**Source spec:** `docs/superpowers/specs/2026-05-29-godynamo-gui-profiles-regions-design.md`

---

## Conventions

- **No real AWS.** Estevao runs all live tests. Automated verification = `go build ./...`, `go test ./...` (with injected fakes — never hits AWS), and `node --check`. **Never** run `go run . gui` / `npm start` / `electron .` (they block / hit real AWS).
- **Commit trailer:** every commit ends with a blank line then `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`. No backticks in commit messages.
- **TUI safety:** `internal/dynamo` changes are additive; the only `internal/app` change is one call-site update that preserves behavior. Do not change TUI logic.
- **CSP unchanged:** `default-src 'self'; script-src 'self'; … style-src 'self' 'unsafe-inline'`. Renderer uses DOM APIs + inline `style.left/top` only (allowed); no inline `<script>`.
- **Field-name preservation:** the per-tab object keeps the names the untouched render/sort/detail/export functions read (`currentTable`, `keys`, `items`, …); this plan only **adds** `region`.

## File structure

```
internal/dynamo/client.go        # MODIFY — Profile field + WithSharedConfigProfile; profile param on DiscoverRegionsWithTables
internal/dynamo/profiles.go      # CREATE — ListProfiles + ListProfilesFromReader + orderProfiles
internal/dynamo/profiles_test.go # CREATE — parser tests
internal/app/app.go              # MODIFY — one DiscoverRegionsWithTables call gains "" profile arg
internal/gui/server.go           # MODIFY — per-(profile,region) registry; /profiles + /discover; region routing; remove /connect, GET /tables, local mode
internal/gui/server_test.go      # MODIFY — migrate to region-routed model; add profiles/discover/routing tests
electron/preload.js              # MODIFY — listProfiles/discover; region arg on data methods; remove connect
electron/renderer/index.html     # MODIFY — delete connect screen; profile row + region tree; create-table region select
electron/renderer/styles.css     # MODIFY — profile row, region tree, sidebar message; drop connect/disconnect rules
electron/renderer/app.js         # MODIFY — boot via profiles/discover; conn.profile/regions; region-aware tabs/data; region tree; profile switch + refresh
```

---

## Task 1: `internal/dynamo` — profile support + profile listing

**Files:**
- Modify: `internal/dynamo/client.go`
- Create: `internal/dynamo/profiles.go`, `internal/dynamo/profiles_test.go`
- Modify: `internal/app/app.go`

- [ ] **Step 1: Write the failing parser tests.** Create `internal/dynamo/profiles_test.go`:

```go
package dynamo

import (
	"reflect"
	"strings"
	"testing"
)

func TestListProfilesFromReaderParsesSections(t *testing.T) {
	in := "[default]\naws_access_key_id = A\n\n[work]\naws_access_key_id = B\n[ personal ]\n"
	names, def := ListProfilesFromReader(strings.NewReader(in))
	if def != "default" {
		t.Fatalf("want default, got %q", def)
	}
	want := []string{"default", "work", "personal"} // file order
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("want %v, got %v", want, names)
	}
}

func TestListProfilesFromReaderNoDefault(t *testing.T) {
	names, def := ListProfilesFromReader(strings.NewReader("[work]\n[home]\n"))
	if def != "" {
		t.Fatalf("want empty default, got %q", def)
	}
	if !reflect.DeepEqual(names, []string{"work", "home"}) {
		t.Fatalf("got %v", names)
	}
}

func TestListProfilesFromReaderIgnoresNoise(t *testing.T) {
	names, def := ListProfilesFromReader(strings.NewReader("# comment\n\n  \nkey = val\n"))
	if len(names) != 0 || def != "" {
		t.Fatalf("want empty, got %v / %q", names, def)
	}
}

func TestOrderProfilesDefaultFirstThenSorted(t *testing.T) {
	got := orderProfiles([]string{"work", "default", "alpha"}, "default")
	want := []string{"default", "alpha", "work"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail.**

Run: `go test ./internal/dynamo/ -run TestListProfiles -v`
Expected: compile error / FAIL — `ListProfilesFromReader` and `orderProfiles` undefined.

- [ ] **Step 3: Create the parser.** Create `internal/dynamo/profiles.go`:

```go
package dynamo

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var profileSectionRe = regexp.MustCompile(`^\s*\[([^\]]+)\]\s*$`)

// ListProfilesFromReader parses INI section headers from r as profile names.
// It returns the names in file order plus the default profile name
// ("default" when a [default] section exists, else "").
func ListProfilesFromReader(r io.Reader) (names []string, def string) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		m := profileSectionRe.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		name := strings.TrimSpace(m[1])
		if name == "" {
			continue
		}
		names = append(names, name)
		if name == "default" {
			def = "default"
		}
	}
	return names, def
}

// orderProfiles returns the default profile first (when present), then the
// remaining names sorted alphabetically.
func orderProfiles(raw []string, def string) []string {
	ordered := []string{}
	if def != "" {
		ordered = append(ordered, def)
	}
	rest := []string{}
	for _, n := range raw {
		if n != def {
			rest = append(rest, n)
		}
	}
	sort.Strings(rest)
	return append(ordered, rest...)
}

// ListProfiles reads ~/.aws/credentials and returns profile names (default
// first, then sorted) plus the default name ("" if none). A missing file
// yields an empty slice and nil error.
func ListProfiles() (names []string, def string, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, "", err
	}
	f, err := os.Open(filepath.Join(home, ".aws", "credentials"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", nil
		}
		return nil, "", err
	}
	defer f.Close()
	raw, def := ListProfilesFromReader(f)
	return orderProfiles(raw, def), def, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass.**

Run: `go test ./internal/dynamo/ -run "TestListProfiles|TestOrderProfiles" -v`
Expected: PASS (4 tests).

- [ ] **Step 5: Add `Profile` to `ConnectionConfig` and apply it in `NewClient`.** In `internal/dynamo/client.go`, change the struct (currently lines ~149-156):

```go
// ConnectionConfig holds connection settings
type ConnectionConfig struct {
	Endpoint  string
	Region    string
	AccessKey string
	SecretKey string
	UseLocal  bool
	Profile   string
}
```

Then in `NewClient`, right after `opts = append(opts, config.WithRegion(cfg.Region))`, add:

```go
	if cfg.Profile != "" {
		opts = append(opts, config.WithSharedConfigProfile(cfg.Profile))
	}
```

- [ ] **Step 6: Add a `profile` param to `DiscoverRegionsWithTables`.** Change its signature (line ~57) to:

```go
func DiscoverRegionsWithTables(ctx context.Context, profile string, useLocal bool, endpoint string) ([]RegionInfo, error) {
```

In the per-region goroutine, replace the single-option load (currently `cfg, err := config.LoadDefaultConfig(regionCtx, config.WithRegion(r))`) with:

```go
			loadOpts := []func(*config.LoadOptions) error{config.WithRegion(r)}
			if profile != "" {
				loadOpts = append(loadOpts, config.WithSharedConfigProfile(profile))
			}
			cfg, err := config.LoadDefaultConfig(regionCtx, loadOpts...)
```

(The `useLocal` branch at the top is unchanged — local mode uses static creds and ignores `profile`.)

- [ ] **Step 7: Update the one TUI caller.** In `internal/app/app.go` (line ~263), change:

```go
		regions, err := dynamo.DiscoverRegionsWithTables(context.Background(), false, "")
```
to:
```go
		regions, err := dynamo.DiscoverRegionsWithTables(context.Background(), "", false, "")
```

- [ ] **Step 8: Build + test.**

Run: `go build ./... && go test ./internal/dynamo/`
Expected: build clean; dynamo tests PASS.

- [ ] **Step 9: Commit.**

```bash
git add internal/dynamo/client.go internal/dynamo/profiles.go internal/dynamo/profiles_test.go internal/app/app.go
git commit -m "feat(dynamo): profile-aware client + discovery and ~/.aws/credentials profile listing

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: `internal/gui` bridge — per-(profile, region) registry + /profiles + /discover

Replace the single-backend connect model with a region-routed registry. The server and its tests change together so `go test ./internal/gui/` stays green.

**Files:**
- Modify: `internal/gui/server.go`, `internal/gui/server_test.go`

- [ ] **Step 1: Replace the types + server struct + `newServer` + `defaultConnect`.** In `internal/gui/server.go`, replace the whole block from `type connectRequest struct {` through the end of `func defaultConnect(...)` (currently lines ~18-57) with:

```go
type errorResponse struct {
	Error string `json:"error"`
}

// RegionTables is one region's full table list, returned by /discover.
type RegionTables struct {
	Region string   `json:"region"`
	Tables []string `json:"tables"`
}

type server struct {
	token         string
	mu            sync.RWMutex
	activeProfile string
	clients       map[string]Backend // key: region (for the active profile)
	connectFn     func(profile, region string) (Backend, error)
	discoverFn    func(ctx context.Context, profile string) ([]string, error)
	profilesFn    func() (names []string, def string, err error)
	h             http.Handler
}

func newServer(token string) *server {
	s := &server{
		token:      token,
		clients:    map[string]Backend{},
		connectFn:  defaultConnectFn,
		discoverFn: defaultDiscoverFn,
		profilesFn: dynamo.ListProfiles,
	}
	s.h = s.buildHandler()
	return s
}

// defaultConnectFn builds a real *dynamo.Client for one profile+region.
func defaultConnectFn(profile, region string) (Backend, error) {
	return dynamo.NewClient(dynamo.ConnectionConfig{Region: region, Profile: profile})
}

// defaultDiscoverFn returns the region names that have tables for the profile.
func defaultDiscoverFn(ctx context.Context, profile string) ([]string, error) {
	infos, err := dynamo.DiscoverRegionsWithTables(ctx, profile, false, "")
	if err != nil {
		return nil, err
	}
	regions := make([]string, 0, len(infos))
	for _, ri := range infos {
		regions = append(regions, ri.Region)
	}
	return regions, nil
}
```

- [ ] **Step 2: Add the `context` import.** In `server.go`'s import block, add `"context"` (the other imports — `crypto/subtle`, `encoding/json`, `net/http`, `sort`, `strconv`, `strings`, `sync`, the aws `types`, and the `dynamo`/`models`/`query` packages — stay).

- [ ] **Step 3: Update the route table.** In `buildHandler`, replace the two lines:

```go
	mux.HandleFunc("POST /connect", s.handleConnect)
	mux.HandleFunc("GET /tables", s.handleListTables)
```
with:
```go
	mux.HandleFunc("GET /profiles", s.handleProfiles)
	mux.HandleFunc("POST /discover", s.handleDiscover)
```

(The remaining `/tables/{name}/...` and `POST /tables` routes stay.)

- [ ] **Step 4: Replace `getBackend`/`setBackend` with `backendFor`.** Delete the `getBackend` and `setBackend` methods (currently ~lines 100-110) and add:

```go
func (s *server) backendFor(region string) (Backend, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if b, ok := s.clients[region]; ok {
		return b, nil
	}
	b, err := s.connectFn(s.activeProfile, region)
	if err != nil {
		return nil, err
	}
	s.clients[region] = b
	return b, nil
}
```

- [ ] **Step 5: Replace `handleConnect` with `handleProfiles` + `handleDiscover`, and delete `handleListTables`.** Delete the entire `handleConnect` function and the entire `handleListTables` function. Add:

```go
func (s *server) handleProfiles(w http.ResponseWriter, r *http.Request) {
	names, def, err := s.profilesFn()
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if names == nil {
		names = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"profiles": names, "default": def})
}

type discoverRequest struct {
	Profile string `json:"profile"`
}

func (s *server) handleDiscover(w http.ResponseWriter, r *http.Request) {
	var req discoverRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Switching profile resets the per-region client cache.
	s.mu.Lock()
	s.activeProfile = req.Profile
	s.clients = map[string]Backend{}
	s.mu.Unlock()

	regions, err := s.discoverFn(r.Context(), req.Profile)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to discover regions: "+err.Error())
		return
	}
	sort.Strings(regions)

	out := make([]RegionTables, 0, len(regions))
	for _, region := range regions {
		backend, berr := s.backendFor(region)
		if berr != nil {
			continue // skip a region whose client can't be built
		}
		tables, terr := backend.ListTables(r.Context())
		if terr != nil {
			continue // skip a region we can't list
		}
		tables = append([]string(nil), tables...)
		sort.Strings(tables)
		out = append(out, RegionTables{Region: region, Tables: tables})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"profile": req.Profile, "regions": out})
}
```

- [ ] **Step 6: Region-route every data handler.** In each of `handleSchema`, `handleScan`, `handleQuery`, `handlePutItem`, `handleDeleteItem`, `handleCreateTable`, replace the opening guard

```go
	backend, ok := s.getBackend()
	if !ok {
		writeError(w, http.StatusConflict, "not connected")
		return
	}
```
with
```go
	region := r.URL.Query().Get("region")
	if region == "" {
		writeError(w, http.StatusBadRequest, "region is required")
		return
	}
	backend, err := s.backendFor(region)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to connect: "+err.Error())
		return
	}
```

Notes that keep this compiling:
- `handleSchema`'s next line becomes `info, err := backend.DescribeTable(...)` — `info` is new so `:=` is fine (reusing `err`).
- `handleScan` already does `startKey, err := decodeCursor(...)` and `result, err := backend.ScanTable(...)` — both keep `:=` (each introduces a new var).
- `handleQuery`, `handlePutItem`, `handleDeleteItem` decode the body with `if err := json.NewDecoder(...)` (block-scoped `err`) — unaffected. `handleDeleteItem`'s `info, err := backend.DescribeTable(...)` keeps `:=` (new `info`).
- `handleCreateTable` has no `{name}`; just drop in the region guard before `var req createTableRequest`.

- [ ] **Step 7: Migrate the test scaffolding.** In `internal/gui/server_test.go`, replace `newTestServer` (currently lines ~62-66) with:

```go
func newTestServer(b Backend) *server {
	s := newServer("test-token")
	s.activeProfile = "test"
	s.connectFn = func(profile, region string) (Backend, error) { return b, nil }
	return s
}
```

- [ ] **Step 8: Delete obsolete tests and retarget the CORS/token tests.** In `server_test.go`:
  - **Delete** `TestConnectStoresBackend`, `TestConnectValidatesMode`, `TestListTablesSorted` (the `/connect` and flat `GET /tables` routes are gone).
  - In `TestRequiresToken`, change the target `"/tables"` to `"/profiles"`.
  - In `TestCORSPreflight`, change the target `"/connect"` to `"/discover"`.

- [ ] **Step 9: Convert the three "not connected" tests to "missing region".** Replace `TestScanNotConnected`, `TestQueryNotConnected`, and `TestWriteNotConnected` with:

```go
func TestScanMissingRegion(t *testing.T) {
	s := newTestServer(&fakeBackend{})
	rec := do(s, http.MethodGet, "/tables/x/scan", "") // no region param
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestQueryMissingRegion(t *testing.T) {
	s := newTestServer(&fakeBackend{info: &dynamo.TableInfo{PartitionKey: "id"}})
	rec := do(s, http.MethodPost, "/tables/t/query", `{"conditions":[]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestPutItemMissingRegion(t *testing.T) {
	s := newTestServer(&fakeBackend{})
	rec := do(s, http.MethodPost, "/tables/t/item", `{"json":"{}"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}
```

- [ ] **Step 10: Add `?region=us-east-1` to every remaining data-endpoint test target.** These tests keep their bodies; only the request target changes (the region guard runs before their existing assertions). Apply exactly:

| Test | Old target | New target |
|---|---|---|
| `TestSchemaHandler` | `/tables/mytable/schema` | `/tables/mytable/schema?region=us-east-1` |
| `TestScanHandlerConvertsItemsAndCursor` | `/tables/mytable/scan?limit=10` | `/tables/mytable/scan?limit=10&region=us-east-1` |
| `TestScanBackendError` | `/tables/x/scan` | `/tables/x/scan?region=us-east-1` |
| `TestQueryModeForPartitionKeyEquals` | `/tables/t/query` | `/tables/t/query?region=us-east-1` |
| `TestQueryFallsBackToScanForNonKey` | `/tables/t/query` | `/tables/t/query?region=us-east-1` |
| `TestQueryUnknownOperator` | `/tables/t/query` | `/tables/t/query?region=us-east-1` |
| `TestQueryNoEffectiveFilter` | `/tables/t/query` | `/tables/t/query?region=us-east-1` |
| `TestPutItem` | `/tables/t/item` | `/tables/t/item?region=us-east-1` |
| `TestPutItemInvalidJSON` | `/tables/t/item` | `/tables/t/item?region=us-east-1` |
| `TestDeleteItemDerivesKey` | `/tables/t/item` | `/tables/t/item?region=us-east-1` |
| `TestDeleteItemMissingKey` | `/tables/t/item` | `/tables/t/item?region=us-east-1` |
| `TestCreateTable` | `/tables` | `/tables?region=us-east-1` |
| `TestCreateTableValidates` | `/tables` | `/tables?region=us-east-1` |
| `TestCreateTableRequiresPKType` | `/tables` | `/tables?region=us-east-1` |
| `TestQueryForceScanIgnoresIndexableEquality` | `/tables/t/query` | `/tables/t/query?region=us-east-1` |
| `TestQueryForceIndexUsesGSI` | `/tables/t/query` | `/tables/t/query?region=us-east-1` |
| `TestQueryForceIndexWithoutEqualityIs400` | `/tables/t/query` | `/tables/t/query?region=us-east-1` |
| `TestQueryAutoReturnsIndexName` | `/tables/t/query` | `/tables/t/query?region=us-east-1` |

- [ ] **Step 11: Add profiles/discover/region-routing tests.** Append to `server_test.go`:

```go
func TestProfilesHandler(t *testing.T) {
	s := newServer("test-token")
	s.profilesFn = func() ([]string, string, error) {
		return []string{"default", "work"}, "default", nil
	}
	rec := do(s, http.MethodGet, "/profiles", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Profiles []string `json:"profiles"`
		Default  string   `json:"default"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Default != "default" || len(resp.Profiles) != 2 || resp.Profiles[0] != "default" {
		t.Fatalf("got %+v", resp)
	}
}

func TestDiscoverReturnsRegionsAndResetsCache(t *testing.T) {
	s := newServer("test-token")
	s.discoverFn = func(ctx context.Context, profile string) ([]string, error) {
		return []string{"sa-east-1", "us-east-1"}, nil
	}
	s.connectFn = func(profile, region string) (Backend, error) {
		return &fakeBackend{tables: []string{region + "-tableB", region + "-tableA"}}, nil
	}
	rec := do(s, http.MethodPost, "/discover", `{"profile":"work"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Profile string `json:"profile"`
		Regions []struct {
			Region string   `json:"region"`
			Tables []string `json:"tables"`
		} `json:"regions"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Profile != "work" {
		t.Fatalf("want profile work, got %q", resp.Profile)
	}
	if len(resp.Regions) != 2 || resp.Regions[0].Region != "sa-east-1" {
		t.Fatalf("regions not sorted/returned: %+v", resp.Regions)
	}
	// tables sorted within a region
	if resp.Regions[0].Tables[0] != "sa-east-1-tableA" {
		t.Fatalf("tables not sorted: %+v", resp.Regions[0].Tables)
	}
	if s.activeProfile != "work" {
		t.Fatalf("active profile not set: %q", s.activeProfile)
	}
}

func TestRegionRoutingCachesPerRegion(t *testing.T) {
	calls := map[string]int{}
	s := newServer("test-token")
	s.activeProfile = "work"
	s.connectFn = func(profile, region string) (Backend, error) {
		calls[region]++
		return &fakeBackend{scan: &dynamo.ScanResult{
			Items: []map[string]types.AttributeValue{
				{"r": &types.AttributeValueMemberS{Value: region}},
			},
			Count: 1,
		}}, nil
	}
	// First scan in sa-east-1 builds + caches that client.
	rec := do(s, http.MethodGet, "/tables/t/scan?region=sa-east-1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []map[string]interface{} `json:"items"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Items) != 1 || resp.Items[0]["r"] != "sa-east-1" {
		t.Fatalf("routed to wrong region: %+v", resp.Items)
	}
	// Second scan in the same region reuses the cached client.
	_ = do(s, http.MethodGet, "/tables/t/scan?region=sa-east-1", "")
	if calls["sa-east-1"] != 1 {
		t.Fatalf("want connectFn called once for sa-east-1, got %d", calls["sa-east-1"])
	}
}
```

- [ ] **Step 12: Build + test.**

Run: `go build ./... && go test ./...`
Expected: build clean; all packages PASS (notably `internal/gui` and `internal/dynamo`).

- [ ] **Step 13: Commit.**

```bash
git add internal/gui/server.go internal/gui/server_test.go
git commit -m "feat(gui): per-(profile,region) client registry with /profiles and /discover

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: `electron/preload.js` — region params + profiles/discover

**Files:**
- Modify: `electron/preload.js`

- [ ] **Step 1: Rewrite the exposed API.** Replace the entire `contextBridge.exposeInMainWorld('api', { … })` call (currently lines ~31-47) with:

```js
contextBridge.exposeInMainWorld('api', {
  listProfiles: () => call('GET', '/profiles'),
  discover: (profile) => call('POST', '/discover', { profile }),
  schema: (name, region) =>
    call('GET', `/tables/${encodeURIComponent(name)}/schema?region=${encodeURIComponent(region)}`),
  scan: (name, region, cursor, limit) => {
    const params = new URLSearchParams({ region })
    if (cursor) params.set('cursor', cursor)
    if (limit) params.set('limit', String(limit))
    return call('GET', `/tables/${encodeURIComponent(name)}/scan?${params.toString()}`)
  },
  query: (name, region, body) =>
    call('POST', `/tables/${encodeURIComponent(name)}/query?region=${encodeURIComponent(region)}`, body),
  saveItem: (name, region, json) =>
    call('POST', `/tables/${encodeURIComponent(name)}/item?region=${encodeURIComponent(region)}`, { json }),
  deleteItem: (name, region, json) =>
    call('DELETE', `/tables/${encodeURIComponent(name)}/item?region=${encodeURIComponent(region)}`, { json }),
  createTable: (form, region) =>
    call('POST', `/tables?region=${encodeURIComponent(region)}`, form),
  exportFile: (defaultName, content) => ipcRenderer.invoke('export-file', { defaultName, content }),
})
```

(`connect` and `listTables` are removed; `call()` and `bridge()` above are unchanged.)

- [ ] **Step 2: Syntax gate.**

Run: `node --check electron/preload.js`
Expected: no output, exit 0.

- [ ] **Step 3: Commit.**

```bash
git add electron/preload.js
git commit -m "feat(gui): region-aware preload API with listProfiles/discover

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: renderer HTML + CSS — connect screen out, profile row + region tree in

**Files:**
- Modify: `electron/renderer/index.html`, `electron/renderer/styles.css`

- [ ] **Step 1: Delete the connect screen.** In `index.html`, remove the entire `<section id="connect-screen" …> … </section>` block (currently lines ~11-29).

- [ ] **Step 2: Show the main screen on boot.** Change `<section id="main-screen" class="screen hidden">` to `<section id="main-screen" class="screen">`.

- [ ] **Step 3: Rebuild the sidebar.** Replace the current `<aside id="sidebar"> … </aside>` contents:

```html
    <aside id="sidebar">
      <button id="create-table-btn">+ New table</button>
      <button id="disconnect-btn">Change connection</button>
      <input type="text" id="table-filter" placeholder="Filter tables…" />
      <ul id="table-list"></ul>
    </aside>
```
with:
```html
    <aside id="sidebar">
      <div id="profile-row">
        <select id="profile-select"></select>
        <button id="refresh-btn" title="Refresh regions">⟳</button>
      </div>
      <button id="create-table-btn">+ New table</button>
      <input type="text" id="table-filter" placeholder="Filter tables…" />
      <div id="sidebar-msg" class="sidebar-msg hidden"></div>
      <ul id="table-list"></ul>
    </aside>
```

- [ ] **Step 4: Add a region picker to the create-table modal.** In the `.ct-form` div, immediately after the `Table name` label, add:

```html
        <label>Region
          <select id="ct-region"></select>
        </label>
```

- [ ] **Step 5: Append CSS and drop dead connect rules.** In `styles.css`, **delete** these now-dead rules: `#connect-screen { … }`, `.connect-card { … }`, `.connect-card h1 { … }` (lines ~6-8), and `#disconnect-btn { … }` (line ~68). Then append:

```css
#profile-row { display: flex; gap: 6px; margin-bottom: 8px; }
#profile-row select { flex: 1; margin-bottom: 0; }
#refresh-btn { padding: 6px 10px; margin-bottom: 0; }
.sidebar-msg { font-size: 12px; color: #828bb8; padding: 6px 8px; line-height: 1.4; }
#table-list li.region-head { font-weight: bold; color: #c8d3f5; user-select: none; }
#table-list li.region-head:hover { background: #1b2336; }
#table-list li.table-row { padding-left: 22px; }
```

- [ ] **Step 6: Syntax sanity.**

Run: `node --check electron/renderer/app.js`
Expected: exit 0 (app.js unchanged here; this just confirms nothing was edited by mistake).

- [ ] **Step 7: Commit.**

```bash
git add electron/renderer/index.html electron/renderer/styles.css
git commit -m "feat(gui): replace connect screen with profile row and region-tree sidebar markup

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: `electron/renderer/app.js` — boot, region tree, region-aware tabs

Make the renderer drive profiles/discover, render the region tree, and route every tab/data call by region. This is the largest renderer change; apply each edit exactly.

**Files:**
- Modify: `electron/renderer/app.js`

- [ ] **Step 1: Expand `conn` and make `newTab` region-aware.** Replace the `conn` declaration (line ~25):

```js
const conn = { tables: [], tabs: [], activeId: null, nextId: 1 }
```
with:
```js
const conn = { profile: '', profiles: [], regions: [], tabs: [], activeId: null, nextId: 1 }
```

Then change `newTab(name)` (line ~28) to take a region and store it:

```js
function newTab(name, region) {
  return {
    id: conn.nextId++, currentTable: name, region,
    loaded: false, status: '', scrollTop: 0, filterOpen: false,
    keys: { partition: '', sort: '' }, indexes: [], schemaRaw: '',
    cursor: '', items: [], rendered: [],
    conditions: [], filterActive: false, mode: '', scanned: 0,
    strategy: { mode: '', index: '' }, override: { mode: 'auto', index: '' },
    sort: { column: null, dir: 'asc' }, selectedIdx: -1, selectedItem: null, detailText: '',
  }
}
```

- [ ] **Step 2: Make `renderTabs` show the region in the tooltip.** In `renderTabs`, right after `label.textContent = t.currentTable`, add:

```js
    el.title = t.currentTable + ' · ' + t.region
```

- [ ] **Step 3: Make the context menu and `openTable` region-aware.** Replace `let ctxTable = null` and `showContextMenu` (lines ~59-67) with:

```js
let ctxTarget = null

function showContextMenu(x, y, name, region) {
  ctxTarget = { name, region }
  const menu = $('ctx-menu')
  menu.style.left = x + 'px'
  menu.style.top = y + 'px'
  show(menu)
}
```

Replace `openTable` (lines ~92-102) with:

```js
function openTable(name, region, opts) {
  const forceNew = !!(opts && opts.forceNew)
  if (!forceNew) {
    const existing = conn.tabs.find((t) => t.currentTable === name && t.region === region)
    if (existing) { activate(existing.id); return }
  }
  const tab = newTab(name, region)
  conn.tabs.push(tab)
  activate(tab.id)
  loadTab(tab)
}
```

- [ ] **Step 4: Route `loadTab` and `loadPage` data calls by region.** In `loadTab`, change `const schema = await window.api.schema(tab.currentTable)` to `const schema = await window.api.schema(tab.currentTable, tab.region)`. In `loadPage`, change the query call to `await window.api.query(tab.currentTable, tab.region, { … })` (keep the body object) and the scan call to `await window.api.scan(tab.currentTable, tab.region, cursor, pageSize())`.

- [ ] **Step 5: Replace the connect-screen code with the boot/discovery flow.** Replace the whole block `function initConnectScreen() { … }` through `function disconnect() { … }` (currently lines ~166-226) with:

```js
function populateRegionPicker() {
  const sel = $('ct-region')
  sel.innerHTML = ''
  AWS_REGIONS.forEach((r) => {
    const opt = document.createElement('option')
    opt.value = r
    opt.textContent = r
    sel.appendChild(opt)
  })
}

function renderProfileSelect(selected) {
  const sel = $('profile-select')
  sel.innerHTML = ''
  conn.profiles.forEach((p) => {
    const opt = document.createElement('option')
    opt.value = p
    opt.textContent = p
    if (p === selected) opt.selected = true
    sel.appendChild(opt)
  })
}

function showSidebarMessage(text) {
  $('table-list').innerHTML = ''
  const m = $('sidebar-msg')
  m.textContent = text
  show(m)
}

async function init() {
  populateRegionPicker()
  try {
    const data = await window.api.listProfiles()
    conn.profiles = data.profiles || []
    if (conn.profiles.length === 0) {
      renderProfileSelect('')
      showSidebarMessage('No AWS profiles found at ~/.aws/credentials')
      return
    }
    const def = conn.profiles.includes(data.default) ? data.default : conn.profiles[0]
    renderProfileSelect(def)
    await discoverInto(def, true)
  } catch (err) {
    showSidebarMessage('Failed to load profiles: ' + err.message)
  }
}

async function discoverInto(profile, resetTabs) {
  conn.profile = profile
  if (resetTabs) {
    conn.tabs = []
    conn.activeId = null
    state = null
    hide($('detail')); hide($('editor')); hideContextMenu()
    showEmptyState(); renderTabs()
  }
  const wasExpanded = new Set(conn.regions.filter((r) => r.expanded).map((r) => r.region))
  showSidebarMessage('Discovering regions…')
  try {
    const data = await window.api.discover(profile)
    conn.regions = (data.regions || []).map((r) => ({
      region: r.region,
      tables: r.tables || [],
      expanded: resetTabs ? false : wasExpanded.has(r.region),
    }))
    if (conn.regions.length === 1) conn.regions[0].expanded = true
    if (conn.regions.length === 0) {
      showSidebarMessage('No tables found in any region — check this profile’s credentials, then Refresh (⟳)')
      return
    }
    renderSidebar()
  } catch (err) {
    showSidebarMessage('Discovery failed: ' + err.message)
  }
}
```

- [ ] **Step 6: Replace `renderTableList` with the region tree, and delete `refreshTables`.** Replace `renderTableList` (lines ~228-247) and the following `refreshTables` (lines ~249-253) with a single `renderSidebar`:

```js
function renderSidebar() {
  hide($('sidebar-msg'))
  const filter = $('table-filter').value.toLowerCase()
  const ul = $('table-list')
  ul.innerHTML = ''
  const sep = ' '
  const activeKey = state ? state.region + sep + state.currentTable : null
  const openKeys = new Set(conn.tabs.map((t) => t.region + sep + t.currentTable))
  conn.regions.forEach((rg) => {
    const matches = rg.tables.filter((t) => t.toLowerCase().includes(filter))
    if (filter && matches.length === 0) return
    const expanded = rg.expanded || (filter !== '' && matches.length > 0)

    const head = document.createElement('li')
    head.className = 'region-head'
    head.textContent = (expanded ? '▾ ' : '▸ ') + rg.region + ' (' + rg.tables.length + ')'
    head.addEventListener('click', () => { rg.expanded = !rg.expanded; renderSidebar() })
    ul.appendChild(head)
    if (!expanded) return

    matches.forEach((t) => {
      const li = document.createElement('li')
      li.className = 'table-row'
      const key = rg.region + sep + t
      if (key === activeKey) li.classList.add('active')
      if (openKeys.has(key)) li.classList.add('open')
      li.textContent = t
      li.addEventListener('click', () => openTable(t, rg.region))
      li.addEventListener('contextmenu', (e) => {
        e.preventDefault()
        showContextMenu(e.clientX, e.clientY, t, rg.region)
      })
      ul.appendChild(li)
    })
  })
}
```

- [ ] **Step 7: Update the remaining `renderTableList()` call sites to `renderSidebar()`.** In `activate` (two calls, in the `!state` branch and the main body) and `closeTab` (two calls), replace every `renderTableList()` with `renderSidebar()`.

- [ ] **Step 8: Route save/delete by region.** In `saveEditor`, change `await window.api.saveItem(state.currentTable, text)` to `await window.api.saveItem(state.currentTable, state.region, text)`. In `doDelete`, change `await window.api.deleteItem(state.currentTable, json)` to `await window.api.deleteItem(state.currentTable, state.region, json)`.

- [ ] **Step 9: Region on create-table.** Replace `openCreateTable` (lines ~768-779) and `submitCreateTable` (lines ~781-807) with:

```js
function openCreateTable() {
  $('ct-name').value = ''
  $('ct-pk').value = ''
  $('ct-pktype').value = 'S'
  $('ct-sk').value = ''
  $('ct-sktype').value = 'S'
  $('ct-billing').value = 'PAY_PER_REQUEST'
  $('ct-rcu').value = '5'
  $('ct-wcu').value = '5'
  const defRegion = (state && state.region) || (conn.regions[0] && conn.regions[0].region) || AWS_REGIONS[0]
  $('ct-region').value = defRegion
  $('ct-error').textContent = ''
  show($('createtable'))
}

async function submitCreateTable() {
  const region = $('ct-region').value
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
    await window.api.createTable(form, region)
    hide($('createtable'))
    await discoverInto(conn.profile, false)
  } catch (err) {
    $('ct-error').textContent = err.message
  } finally {
    $('ct-create').disabled = false
  }
}
```

- [ ] **Step 10: Rewire `DOMContentLoaded`.** Replace the listener body (lines ~809-845). Remove `initConnectScreen()`, the `disconnect-btn` listener, and update the table-filter + context-menu wiring; add profile-select / refresh / init:

```js
window.addEventListener('DOMContentLoaded', () => {
  $('table-filter').addEventListener('input', renderSidebar)
  $('profile-select').addEventListener('change', (e) => discoverInto(e.target.value, true))
  $('refresh-btn').addEventListener('click', () => { if (conn.profile) discoverInto(conn.profile, false) })
  $('schema-btn').addEventListener('click', showSchema)
  $('filter-btn').addEventListener('click', toggleFilter)
  $('filter-add').addEventListener('click', addCondition)
  $('filter-apply').addEventListener('click', applyFilter)
  $('filter-clear').addEventListener('click', clearFilter)
  $('more-btn').addEventListener('click', () => loadPage(state, false))
  $('page-size').addEventListener('change', () => { if (state) loadPage(state, true) })
  $('grid-wrap').addEventListener('scroll', () => { if (state) state.scrollTop = $('grid-wrap').scrollTop })
  $('detail-close').addEventListener('click', () => hide($('detail')))
  $('detail-search').addEventListener('input', renderDetailBody)
  $('detail-copy').addEventListener('click', copyDetail)
  $('new-item-btn').addEventListener('click', openNewItem)
  $('create-table-btn').addEventListener('click', openCreateTable)
  $('export-json').addEventListener('click', exportJSON)
  $('export-csv').addEventListener('click', exportCSV)
  $('detail-edit').addEventListener('click', openEditItem)
  $('detail-delete').addEventListener('click', confirmDelete)
  $('editor-close').addEventListener('click', () => hide($('editor')))
  $('editor-save').addEventListener('click', saveEditor)
  $('ct-close').addEventListener('click', () => hide($('createtable')))
  $('ct-create').addEventListener('click', submitCreateTable)
  $('confirm-no').addEventListener('click', () => hide($('confirm')))
  $('confirm-yes').addEventListener('click', doDelete)
  $('ctx-open-new').addEventListener('click', () => {
    if (ctxTarget) openTable(ctxTarget.name, ctxTarget.region, { forceNew: true })
    hideContextMenu()
  })
  document.addEventListener('click', (e) => {
    if (!$('ctx-menu').contains(e.target)) hideContextMenu()
  })
  document.addEventListener('scroll', hideContextMenu, true)
  document.addEventListener('keydown', (e) => { if (e.key === 'Escape') hideContextMenu() })
  init()
})
```

- [ ] **Step 11: Syntax gate + straggler grep.**

Run: `node --check electron/renderer/app.js`
Expected: exit 0.

Run: `grep -n "window.api.connect\|window.api.listTables\|renderTableList\|refreshTables\|initConnectScreen\|disconnect-btn\|ctxTable\b" electron/renderer/app.js`
Expected: no matches (empty). If any match, you missed a rename/removal — fix it.

- [ ] **Step 12: Manual check (Estevao).** Launch + the app should boot straight to the main screen, the profile dropdown shows your profiles (default selected), the sidebar lists regions-with-tables, expanding shows tables, clicking opens a region-aware tab, right-click opens a new tab, switching the profile resets tabs and re-discovers, Refresh (⟳) re-discovers, and create-table creates in the chosen region then refreshes the tree.

- [ ] **Step 13: Commit.**

```bash
git add electron/renderer/app.js
git commit -m "feat(gui): profile dropdown + region-tree sidebar with region-aware tabs

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 6: Final verification

**Files:** none.

- [ ] **Step 1: Go build + tests.**

Run: `go build ./... && go test ./...`
Expected: build clean; all packages PASS (`internal/dynamo`, `internal/gui`, `internal/query`, `internal/ui` `ok`; others `[no test files]`).

- [ ] **Step 2: Renderer syntax gates.**

Run: `node --check electron/renderer/app.js && node --check electron/preload.js`
Expected: exit 0.

- [ ] **Step 3: Full manual verification (Estevao)** — spec §8 / the checklist below:
  1. App boots to the main screen (no connect screen); profile dropdown shows credentials profiles with `default` selected.
  2. Sidebar shows only regions that have tables; expanding a region lists its tables (single region auto-expands).
  3. Left-click a table opens/focuses a region-aware tab and loads its rows; right-click → "Open in new tab" (duplicate allowed).
  4. Open tables from two different regions at once → each tab queries its own region.
  5. Table filter narrows tables across regions; matching groups expand.
  6. Switch profile → tabs close, tree re-discovers for the new profile.
  7. Refresh (⟳) re-discovers the current profile (tabs preserved).
  8. Create table with a chosen region → succeeds and the tree refreshes.
  9. No `~/.aws/credentials` → "No AWS profiles…" message; a profile with no tables anywhere → "No tables found in any region…" message.

---

## Notes for the implementer

- **Region routing invariant:** the bridge keeps ONE active profile; each data call carries `region`; `backendFor(region)` lazily builds+caches a client. `/discover` resets that cache and the active profile. The renderer only ever sends `region` (never the profile) on data calls — the profile is server-side state.
- **Why tabs stay valid across Refresh:** `/discover` clears the server's client cache, but `backendFor` rebuilds lazily on the next call, so an open tab's next scan/query just re-creates its region client. Only a profile *switch* (resetTabs=true) closes tabs.
- **Do not** re-introduce local mode, persist anything to disk, read `~/.aws/config`, or add a region allow-list — all out of scope (spec §10).
