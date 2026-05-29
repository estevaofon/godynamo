# GoDynamo GUI — Profiles + Region-Grouped Sidebar Design Spec

- **Date:** 2026-05-29
- **Status:** Approved (design); ready for implementation planning.
- **Builds on:** the multi-table tabs GUI (`docs/superpowers/specs/2026-05-29-godynamo-gui-tabs-design.md`), on `develop` (`internal/gui` bridge + `internal/dynamo` client + `electron/` app).
- **Scope:** make the GUI profile-aware and multi-region like DynamoBase: drop the initial connect screen (always AWS), pick an AWS profile (default selected on launch) from `~/.aws/credentials`, and show a left-side tree of **only the regions that have tables**, expandable to their tables. Tables from different regions can be open in tabs simultaneously.

## 1. Goal

Today the GUI opens on a connect screen where you choose AWS-vs-local and a single region, then browse that one region's tables. Replace that with a DynamoBase-style model: the app boots straight into the main view, lists AWS profiles from `~/.aws/credentials` (opening with `default`), scans all regions for tables, and presents the regions that have tables as collapsible groups in the sidebar. Selecting a different profile re-discovers. The connection is always AWS (local mode is dropped from the GUI; the TUI keeps it).

## 2. Decisions locked during brainstorming

| Decision | Choice |
|---|---|
| Region discovery | **Scan all ~28 regions automatically** on launch and on profile switch, reusing the existing `dynamo.DiscoverRegionsWithTables`. Show exactly the regions with tables. A **Refresh (⟳)** button re-runs it. |
| Multi-region architecture | **Per-(profile, region) client registry** in the bridge: one active profile + a lazily-built `map[region]→Backend`. Each data endpoint carries a `region`. (Required so tabs from different regions can coexist — approach B, "reconnect on switch", was rejected as incompatible with tabs.) |
| Profile switch | **Reset the session** — close all tabs, clear the tree, re-discover for the new profile. One active profile at a time. |
| Profile source | Parse **`~/.aws/credentials`** for `[section]` names; `default` flagged and auto-selected. (`~/.aws/config` / SSO profiles are out of scope.) |
| Local mode | **Removed from the GUI** (always AWS). The TUI keeps local mode untouched. |
| TUI safety | `internal/dynamo` changes are **additive**: a new `Profile` field on `ConnectionConfig` (TUI leaves it empty) and a new leading `profile` param on `DiscoverRegionsWithTables` (the one TUI caller passes `""`). No TUI behavior change. |
| Discovery completeness | `/discover` returns each region with its **full** (paginated) table list, so the renderer builds the whole tree from one response; expand/collapse is pure UI. |
| Hard constraint | **No real AWS.** Estevao runs live tests. Automated verification = `go build`/`go test` with injected fakes (no AWS calls) + `node --check`. Never launch `go run . gui` / `npm start`. |

## 3. Backend — `internal/dynamo` (additive, TUI-safe)

### 3.1 Profile on the client

`ConnectionConfig` gains `Profile string`. In `NewClient`, when `cfg.Profile != ""`, append `config.WithSharedConfigProfile(cfg.Profile)` to the load options (before `LoadDefaultConfig`). Empty profile → today's default-chain behavior (TUI unaffected).

### 3.2 Profile on discovery

`DiscoverRegionsWithTables` gains a **leading** `profile string` param:

```go
func DiscoverRegionsWithTables(ctx context.Context, profile string, useLocal bool, endpoint string) ([]RegionInfo, error)
```

Each per-region `config.LoadDefaultConfig` adds `config.WithSharedConfigProfile(profile)` when `profile != ""`. The fast 100-table probe and per-region 8s timeout are unchanged. The single TUI caller (`internal/app/app.go:263`) is updated from `(context.Background(), false, "")` to `(context.Background(), "", false, "")` — identical behavior.

### 3.3 Profile listing

New file `internal/dynamo/profiles.go`:

```go
// ListProfiles reads ~/.aws/credentials and returns profile names with "default"
// first (when present), plus the default profile name ("default" when that section
// exists, else ""). A missing file returns an empty slice and nil error.
func ListProfiles() (names []string, def string, err error)

// ListProfilesFromReader parses the INI sections from r (pure, unit-tested).
func ListProfilesFromReader(r io.Reader) (names []string, def string)
```

`ListProfilesFromReader` scans lines, matching `^\s*\[([^\]]+)\]\s*$` as a section (= profile name); `def` is `"default"` when a `[default]` section is present, else `""`. `ListProfiles` resolves `~/.aws/credentials` via `os.UserHomeDir()`, opens it (missing → empty), delegates to the reader form, and orders results: `default` first (if present), then the rest sorted. Profile names in the credentials file are bare (`[work]`, not `[profile work]`). So `profilesFn = dynamo.ListProfiles` directly (no adaptation needed).

## 4. Backend — bridge API (`internal/gui`)

### 4.1 Endpoints

| Route | Purpose |
|---|---|
| `GET /profiles` | `{ "profiles": ["default","work",…], "default": "default" }` (`default` is `""` when absent) |
| `POST /discover` `{ "profile": "X" }` | set active profile, reset client registry, return `{ "profile":"X", "regions":[{ "region":"us-east-1", "tables":["A","B"] }, …] }` |
| `GET /tables/{name}/schema?region=R` | region-routed DescribeTable |
| `GET /tables/{name}/scan?region=R&cursor=&limit=` | region-routed scan |
| `POST /tables/{name}/query?region=R` | region-routed query (body unchanged) |
| `POST /tables/{name}/item?region=R` | region-routed put (body unchanged) |
| `DELETE /tables/{name}/item?region=R` | region-routed delete (body unchanged) |
| `POST /tables?region=R` | create table in region (body unchanged) |

**Removed:** `POST /connect`, `GET /tables` (flat list), and all `mode:"local"`/endpoint handling. `connectRequest` and `defaultConnect`'s local branch are deleted.

### 4.2 Server state + seams

```go
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
    discoverFn    func(ctx context.Context, profile string) ([]string, error) // region names with tables
    profilesFn    func() (names []string, def string, err error)
    h             http.Handler
}
```

`newServer(token)` wires the three production seams:
- `connectFn` = `func(profile, region string) (Backend, error) { return dynamo.NewClient(dynamo.ConnectionConfig{Region: region, Profile: profile}) }`
- `discoverFn` = wraps `dynamo.DiscoverRegionsWithTables(ctx, profile, false, "")` and returns just the region names.
- `profilesFn` = wraps `dynamo.ListProfiles()` (returning `names`, the default name or `""`).

Tests override these to avoid AWS. `clients` starts as an empty map.

### 4.3 Region routing

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

Every data handler replaces the old `s.getBackend()` "not connected" guard with:

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

The remaining body of each handler (scan/query/put/delete/create/schema) is otherwise unchanged — it already takes `backend` and `name`.

### 4.4 `/discover` handler

```
decode {profile}
lock: s.activeProfile = profile; s.clients = map[string]Backend{}; unlock
regions, err := s.discoverFn(ctx, profile)      // 502 on err
sort regions
out := []RegionTables{}
for each region:
    b, err := s.backendFor(region)              // skip region on err
    tables, err := b.ListTables(ctx)            // full pagination; skip on err
    sort tables
    out = append(out, {region, tables})
writeJSON 200 {profile, regions: out}
```

Per-region client/list errors are **skipped** (partial tree), consistent with discovery's silent-skip philosophy; an all-fail yields `regions: []`, which the renderer renders as the "no tables / check credentials" state.

### 4.5 `GET /profiles` handler

Call `s.profilesFn()`; on error return 502; else `writeJSON 200 {profiles, default}`.

## 5. Renderer (`electron/`)

### 5.1 Boot flow (no connect screen)

On `DOMContentLoaded`: `listProfiles()` → populate the profile `<select>` (select `default`, else first) → `discover(selectedProfile)` → build the region tree. A loading indicator shows during discovery. `#main-screen` is visible from the start; `#connect-screen` is deleted.

### 5.2 Sidebar layout

```
┌─────────────────────────────┐
│ Profile: [ default ▼ ]   ⟳  │  profile <select> + Refresh
│ [ Filter tables… ]          │
│ ▾ us-east-1 (3)             │  region group (count); click toggles expand
│     Users                   │    left-click table → open/focus tab
│     Orders                  │    right-click → "Open in new tab"
│ ▸ sa-east-1 (1)             │  collapsed region
│ [ + New table ]             │
└─────────────────────────────┘
```

- Region groups are collapsible; **auto-expand when there is exactly one** region. Count badge = its table count.
- The existing table filter filters table rows across all regions (a region with no matching tables hides its group).
- `index.html`: delete the entire `#connect-screen` section and its inputs; remove the AWS/local radios, region `<select>`, and endpoint field. Add the profile `<select>`, Refresh button, and a region-tree container to `#sidebar`. `#main-screen` loses its initial `hidden`.
- `styles.css`: region-group header (caret + name + count), nested table rows, profile-row layout, loading/empty states (existing dark tokens).

### 5.3 State + region-aware tabs

- `conn` gains `profile` (active) and `regions` (`[{ region, tables, expanded }]`).
- `newTab(name)` → `newTab(name, region)`; each tab carries `region`. Tab **focus/duplicate identity is `(region, currentTable)`**.
- Every data call passes the tab's region: `loadPage`/`loadTab` call `window.api.scan(tab.currentTable, tab.region, …)`, `query(tab.currentTable, tab.region, body)`, `schema(tab.currentTable, tab.region)`; `saveEditor`/`doDelete` pass `state.region`; create-table passes the region chosen in its form (see §5.4). The active **profile stays server-side** (set by `discover`), so per-call signatures only add `region`.
- Tab label = table name; the tab's `title` tooltip includes the region to disambiguate same-named tables across regions.
- `openTable(name, region, { forceNew })`: focus the first tab matching `(region, name)` unless `forceNew`.

### 5.4 Profile switch + refresh

- Profile `<select>` change → **reset session**: close all tabs (`conn.tabs = []`, `activeId = null`, hide modals/detail/editor), clear the tree, then `discover(newProfile)` and rebuild. (Replaces the removed "Change connection" / `disconnect`-to-connect-screen flow.)
- Refresh (⟳) → re-run `discover(activeProfile)`, rebuilding the tree (open tabs are left as-is; their regions still route correctly). Also called after a successful create-table.
- The connect-screen handlers (`initConnectScreen`, `onConnect`, mode radios) are removed from `app.js`. The `AWS_REGIONS` constant is **retained**, used only to populate a region `<select>` added to the create-table modal — you can create a table in a region that has no tables yet, so the picker spans all regions, not just discovered ones. It defaults to the active tab's region when present, else the first discovered region, else the list's first entry. After a successful create, the renderer runs `discover(activeProfile)` to refresh the tree (replacing the removed `refreshTables` / `GET /tables`).

### 5.5 `preload.js`

Add `listProfiles()` (`GET /profiles`) and `discover(profile)` (`POST /discover {profile}`); add a `region` argument to `schema`, `scan`, `query`, `saveItem`, `deleteItem`, and `createTable` (appended as `?region=`); remove `connect`. `main.js` is unchanged (bridge-info + export only).

## 6. Data flow

Launch → `GET /profiles` → `{profiles, default}` → select `default` → `POST /discover {profile:"default"}` → bridge sets active profile, resets registry, probes all regions, full-lists each region-with-tables (warming the registry) → `{regions:[…]}` → tree rendered. Left-click `us-east-1 › Users` → `openTable("Users","us-east-1")` → `newTab` → `GET /tables/Users/schema?region=us-east-1` then `GET /tables/Users/scan?region=us-east-1` (bridge routes via `backendFor("us-east-1")`). Switch profile in the dropdown → tabs closed, tree cleared → `POST /discover {profile:"work"}` resets the registry and rebuilds.

## 7. Error handling

- No `~/.aws/credentials` / no profiles → `/profiles` returns `{profiles:[], default:""}`; the dropdown is empty and the sidebar shows "No AWS profiles found at ~/.aws/credentials".
- Discovery returns zero regions (no tables, or the selected profile's credentials are invalid — failing regions are silently skipped by the probe) → sidebar shows "No tables found in any region — check this profile's credentials, then Refresh (⟳)."
- A data op failing (including per-region client creation: 502) is surfaced in that tab's status line, exactly as in the tabs feature.
- A missing `region` query param on any data endpoint → 400 `region is required`.

## 8. Testing (no real AWS)

- **`internal/dynamo/profiles_test.go`** — `ListProfilesFromReader`: multiple sections incl. `default` (ordered first), no `default` present, empty/whitespace input, malformed lines ignored.
- **`internal/gui/server_test.go`** (extend, reusing the `fakeBackend`):
  - `GET /profiles` with an injected `profilesFn` returns the names + default.
  - `POST /discover` with injected `discoverFn` (e.g. `["us-east-1","sa-east-1"]`) + a `connectFn(profile, region)` returning region-tagged fake backends → response lists both regions with their fake tables; asserts `activeProfile` is set and `clients` was reset.
  - **Region routing:** a `connectFn` that records `(profile, region)` and returns a backend whose `ListTables`/`ScanTable` echo the region → a `scan?region=sa-east-1` hits the `sa-east-1` backend; a second call to the same region reuses the cached client (connectFn called once per region).
  - **Missing region** → 400 on scan/schema/query/item/create.
  - Existing scan/query/item tests updated to include `?region=us-east-1` and to seed the server with a matching `connectFn`.
- **Renderer** — `node --check electron/renderer/app.js`; manual verification by Estevao.
- `go build ./... && go test ./...` clean. Discovery against real AWS and the live tree are verified manually by Estevao.

## 9. File-change summary

| File | Change |
|---|---|
| `internal/dynamo/client.go` | `Profile` field on `ConnectionConfig` + `WithSharedConfigProfile` in `NewClient`; leading `profile` param on `DiscoverRegionsWithTables` |
| `internal/dynamo/profiles.go` | **new** — `ListProfiles` + `ListProfilesFromReader` |
| `internal/dynamo/profiles_test.go` | **new** — reader-parsing tests |
| `internal/app/app.go` | update the one `DiscoverRegionsWithTables(ctx, false, "")` call to `(ctx, "", false, "")` |
| `internal/gui/server.go` | per-(profile,region) registry (`activeProfile`, `clients`, `connectFn`, `discoverFn`, `profilesFn`, `backendFor`); `/profiles` + `/discover` handlers; `region` query param on all data handlers; remove `/connect`, `GET /tables`, local mode |
| `internal/gui/server_test.go` | **add** profiles/discover/region-routing/missing-region tests; update existing data tests to pass `region` |
| `electron/preload.js` | `listProfiles`/`discover`; `region` arg on data methods; remove `connect` |
| `electron/renderer/index.html` | delete `#connect-screen`; add profile `<select>` + Refresh + region-tree container; add a region `<select>` to the create-table modal; `#main-screen` not hidden |
| `electron/renderer/app.js` | boot via profiles/discover; `conn.profile`/`conn.regions`; region-aware `newTab`/`openTable`/`loadPage`/`loadTab` + data calls; region-tree sidebar render; profile-switch reset + Refresh; create-table region picker; replace `refreshTables` with `discover`; remove connect-screen code (retain `AWS_REGIONS` for the create-table picker) |
| `electron/renderer/styles.css` | region tree (caret/name/count/nested rows), profile row, loading/empty states |

## 10. Out of scope / deferred

- Persisting the selected profile, open tabs, or expand state across launches.
- Reading profiles from `~/.aws/config` or SSO sections (credentials file only).
- A user-maintained region allow-list (we always scan all regions).
- Distinguishing "no tables" from "auth error" in discovery (shown as one message; the probe silently skips failing regions).
- Per-region table-count accuracy on the probe vs the full list (the tree uses the full `ListTables` per region; the probe is only used to find which regions have tables).
- Any TUI changes beyond the two additive `internal/dynamo` signature updates. No changes to `models`, `query`, or `dynamo.Client`'s data methods.
