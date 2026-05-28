# GoDynamo Electron GUI — Design Spec

- **Date:** 2026-05-28
- **Status:** Approved (design); ready for implementation planning
- **Scope of this spec:** v1 = **read-only slice**. Later phases (filtering, CRUD, export, packaging) are sketched under "Phasing" but are out of scope for v1.

## 1. Goal

Add a Windows-first `godynamo gui` subcommand that launches an Electron desktop UI backed by the existing Go DynamoDB logic, while preserving the current Bubble Tea terminal UI as the **default** (no-arg) mode.

v1 delivers an end-to-end vertical slice that proves the Go↔Electron bridge and the Windows launch path with the smallest possible surface area:

> Connect (AWS region **or** DynamoDB Local) → list tables → browse scanned data in a grid → inspect an item's JSON → view a table's schema JSON.

No writes, no filtering, no export in v1.

## 2. Decisions locked during brainstorming

| Decision | Choice | Consequence |
|---|---|---|
| v1 feature scope | Read-only slice | No CRUD/filter/export; minimal risk |
| Connection model | Connect screen: **AWS region OR DynamoDB Local endpoint** | Lets the user test against Local without real AWS; no all-region scan |
| Audience | **Just the author, dev-first, Windows** | No installer/signing/auto-update; assumes Node/npm present |
| Bridge transport | **Localhost HTTP, bound to 127.0.0.1, random port + one-time token** | Simple, debuggable, loopback avoids Windows Firewall prompts; WebSocket deferred |
| Who launches whom | **Go (`godynamo gui`) launches Electron** | Go owns lifecycle; renderer files ship inside the Electron app |

Hard constraint: **no real AWS commands are run by the assistant.** The author runs all live DynamoDB tests himself. Verification uses DynamoDB Local, fakes, or dry descriptions.

## 3. Architecture & process model

`godynamo gui` is a launcher + local API bridge in one Go process. It starts an HTTP server on loopback, then spawns Electron as a child and passes the port + token via environment variables. The Electron renderer calls the bridge with `fetch`.

```
godynamo gui   (Go process — parent)
│
├─ HTTP bridge  127.0.0.1:<random>  (Bearer <token>)
│     └─ reuses internal/dynamo + internal/models  (unchanged)
│
└─ spawns Electron (child), env: GODYNAMO_BRIDGE_PORT, GODYNAMO_BRIDGE_TOKEN
      ├─ main.js     → BrowserWindow (contextIsolation:true, nodeIntegration:false)
      ├─ preload.js  → exposes window.api.*  (token baked into a closure via IPC)
      └─ renderer    → connect screen · tables sidebar · data grid · JSON detail · schema
```

Lifecycle: Go blocks on the Electron child process. When the window closes (child exits), Go shuts down the HTTP server and `godynamo gui` returns. `SIGINT`/Ctrl+C also kills the Electron child and tears down cleanly.

The existing TUI is untouched: `godynamo` with no args runs `app.New()` through Bubble Tea exactly as today.

## 4. Repository layout

```
main.go                    # arg routing: ""→TUI (unchanged) · "gui"→gui.Run(args)
internal/
  gui/                     # NEW (Go)
    gui.go                 #   Run(): pick loopback port, mint token, start server,
                           #          spawn Electron, wait on child, shutdown
    server.go              #   HTTP mux, token middleware, CORS, handlers,
                           #          AttributeValue→JSON, cursor codec
    backend.go             #   Backend interface (read-only methods) + wiring
    electron.go            #   locate & spawn Electron (Windows-first), env, lifecycle
    server_test.go         #   handler/codec/middleware unit tests against a fake Backend
  dynamo/  models/  app/  ui/   # UNCHANGED
electron/                  # NEW (front-end, dev-first, plain HTML/CSS/JS)
  package.json             #   electron as devDependency; "start" script
  main.js                  #   Electron main process
  preload.js               #   contextBridge → window.api
  renderer/
    index.html
    app.js
    styles.css
```

`main.go` uses a plain `os.Args` check for the single subcommand — no CLI framework (YAGNI). A future `--help`/`version` can be added later.

## 5. Go bridge

### 5.1 Backend interface (testability without AWS)

The HTTP handlers depend on a narrow interface that `*dynamo.Client` already satisfies. This lets unit tests inject a fake with no AWS and no network.

```go
// internal/gui/backend.go
type Backend interface {
    ListTables(ctx context.Context) ([]string, error)
    DescribeTable(ctx context.Context, name string) (*dynamo.TableInfo, error)
    ScanTable(ctx context.Context, name string, limit int32,
        startKey map[string]types.AttributeValue,
        filterExpr string, names map[string]string, values map[string]interface{}) (*dynamo.ScanResult, error)
}
```

Connection creation stays with `dynamo.NewClient(...)`. The server holds the active `*dynamo.Client` (as a `Backend`) after a successful `/connect`, guarded by a mutex. v1 is single-connection, single-window.

### 5.2 HTTP API

- Base URL: `http://127.0.0.1:<port>`
- Auth: every request must carry `Authorization: Bearer <token>`; missing/incorrect → `401`.
- CORS: small middleware allows the Electron origin (`Access-Control-Allow-Origin: *`, `Access-Control-Allow-Headers: Authorization, Content-Type`) and answers `OPTIONS` preflight. Acceptable because the listener is loopback-only and token-gated, and no cookies are used.

| Method & path | Request | Response (200) |
|---|---|---|
| `POST /connect` | `{ "mode":"aws"\|"local", "region":"us-east-1", "endpoint":"http://localhost:8000" }` | `{ "tables":[...] }` |
| `GET /tables` | — | `{ "tables":[...] }` (sorted) |
| `GET /tables/{name}/schema` | — | `{ "info": <TableInfo>, "rawJSON": "..." }` |
| `GET /tables/{name}/scan?limit=500&cursor=<opaque>` | — | `{ "items":[ {plain JSON}... ], "keys": {"partition":"id","sort":"sk"}, "cursor":"<opaque or empty>", "count":N }` |

Notes:
- `/connect` validates by building the client and doing a quick `ListTables`. `mode:"local"` calls `dynamo.NewClient` with `UseLocal:true` and the given `endpoint`; `mode:"aws"` uses the chosen region with the default credential chain. (Connection errors → `400`/`502` with a message.)
- Items are converted to plain JSON via `models.AttributeValueToInterface` per attribute.
- `keys` carries the partition/sort key names from `DescribeTable` so the renderer can order columns PK/SK-first, mirroring the TUI's `itemsToTable`.
- **Item detail needs no endpoint** — the scan payload already includes full items; the renderer renders the selected item client-side.

### 5.3 Pagination cursor codec

`ScanResult.LastEvaluatedKey` is `map[string]types.AttributeValue`. DynamoDB key attributes are only `S`, `N`, or `B`. The cursor is:

```
cursor = base64( JSON of { attrName: {"S":...} | {"N":"..."} | {"B":"base64"} } )
```

Encoding/decoding handles exactly those three scalar types, so the key round-trips with the correct DynamoDB type (no string/number ambiguity). An empty `LastEvaluatedKey` → empty cursor (no more pages). This codec is dependency-free and unit-tested.

### 5.4 Launch & lifecycle (`gui.go`, `electron.go`)

1. `net.Listen("tcp", "127.0.0.1:0")` → OS assigns a free port.
2. Mint a token with `crypto/rand` (e.g. 32 bytes hex).
3. Start `http.Server` on the listener with the mux + token/CORS middleware.
4. Spawn Electron:
   - Windows dev path: `electron/node_modules/.bin/electron.cmd .` run with `Cmd.Dir = electron/`.
   - Pass `GODYNAMO_BRIDGE_PORT` and `GODYNAMO_BRIDGE_TOKEN` via `Cmd.Env` (env, **not** argv, so the token isn't visible in process listings).
   - If `electron/node_modules` is missing, exit with a clear message: `"Electron app not set up. Run: cd electron && npm install"`.
5. `cmd.Wait()` on the child; on return (or on `SIGINT`), `server.Shutdown(ctx)` and kill the child if still alive.

## 6. Electron front-end

### 6.1 Stack

**Plain HTML/CSS/JS, no build step.** Rationale: dev-first, read-only UI is simple (sidebar list, data grid, JSON viewer, schema viewer); zero toolchain; instant iteration. Renderer files load via `loadFile('renderer/index.html')`. Upgrade path: introduce Vite + a framework when the CRUD/filter UI arrives. Styling echoes the TUI's cyberpunk theme.

### 6.2 Security wiring

- `webPreferences: { contextIsolation: true, nodeIntegration: false, preload }`.
- `main.js` reads `GODYNAMO_BRIDGE_PORT`/`GODYNAMO_BRIDGE_TOKEN` from `process.env` and exposes them to the preload via `ipcMain.handle('bridge-info', …)`.
- `preload.js` calls `ipcRenderer.invoke('bridge-info')` once, then uses `contextBridge.exposeInMainWorld('api', { connect, listTables, scan, schema })`. Each method `fetch`es the loopback base URL with the `Authorization` header. The token is captured in a closure and **never** placed on `window` or in a URL.
- The renderer only ever calls `window.api.*`; the token is captured in a preload closure and is never exposed to renderer code (not on `window`, not in a URL).

### 6.3 Screens (v1)

1. **Connect screen** — radio: AWS / Local. AWS → region dropdown (the renderer hardcodes the region list, mirroring `dynamo.AWSRegions`). Local → endpoint field (default `http://localhost:8000`). "Connect" → `POST /connect`; errors shown inline.
2. **Main view** — left sidebar: table list with a client-side fuzzy/substring filter. Main area: data grid for the selected table (PK/SK columns first), with a "Load more" button driven by the cursor.
3. **Item detail** — clicking a grid row opens a panel with the item's pretty-printed, syntax-highlighted JSON.
4. **Schema** — a toggle/button shows the `DescribeTable` JSON for the current table.

## 7. Error handling

- Bridge returns JSON `{ "error": "message" }` with an appropriate status; the renderer shows it inline (connect screen) or as a toast (main view).
- `401` for bad/missing token.
- `godynamo gui` writes clear stderr messages for: Electron not installed, spawn failure, port bind failure.
- A request error never kills the bridge; only the window closing ends the session.

## 8. Testing

Per the no-real-AWS constraint, automated tests never touch AWS.

- **Go unit tests** (`server_test.go`) against a fake `Backend`:
  - token middleware (200 with token, 401 without),
  - CORS/OPTIONS handling,
  - cursor encode/decode round-trip for `S`/`N`/`B` keys,
  - `AttributeValue`→JSON conversion of representative items,
  - handler routing & JSON response shapes for `/tables`, `/schema`, `/scan`.
- **Optional local integration:** point a test at DynamoDB Local when desired (not required for CI).
- **Front-end:** manual for v1 (dev-first).
- **End-to-end:** the assistant provides explicit step-by-step instructions; **the author** runs them against DynamoDB Local and/or real AWS and reports results.

## 9. Phasing (post-v1, out of scope here)

1. **Filtering phase** — port the visual filter builder to the renderer; extract the smart Query-vs-Scan planner + continuous-scan + pure filter-expression builder out of `internal/app/app.go` into a shared UI-agnostic package consumed by both the TUI and the bridge; add a WebSocket channel for live scan progress.
2. **Write phase** — create/edit/delete items (JSON editor), create-table; reuse `PutItem`/`DeleteItem`/`CreateTable`.
3. **Export phase** — JSON/CSV export via a native save dialog.
4. **Packaging phase** — bundle a distributable Electron app (electron-builder), optional `go:embed` of renderer assets for a single-binary serve-from-Go option, signing/auto-update.

## 10. Open items / non-goals for v1

- No region auto-scan (the connect screen replaces it for v1).
- No WebSocket (read-only scans are one-shot).
- No changes to `internal/app`, `internal/ui`, `internal/dynamo`, or `internal/models`.
- Cross-platform packaging is deferred; the dev launch targets Windows first (`electron.cmd`). The spawn helper should isolate the platform-specific path so non-Windows support is a small later change.
