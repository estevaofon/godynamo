# GoDynamo Electron GUI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a Windows-first `godynamo gui` subcommand that launches an Electron desktop UI for read-only DynamoDB browsing, backed by the existing Go logic, while leaving the terminal TUI as the default mode.

**Architecture:** `godynamo gui` (Go) starts a loopback HTTP bridge (127.0.0.1, random port, one-time token) that reuses `internal/dynamo` + `internal/models`, then spawns Electron as a child process and passes the port + token via env vars. The Electron renderer (plain HTML/CSS/JS) calls the bridge with `fetch` through a preload-exposed `window.api`. v1 is read-only: connect (AWS region **or** DynamoDB Local) → list tables → scan/browse grid → inspect item JSON → view schema JSON.

**Tech Stack:** Go 1.24 stdlib `net/http` (method-based `ServeMux` patterns, `r.PathValue`), `crypto/rand`/`crypto/subtle`; existing `internal/dynamo` + `internal/models`; Electron (dev dependency) with `contextIsolation`; vanilla front-end (no build step).

**Source spec:** `docs/superpowers/specs/2026-05-28-godynamo-electron-gui-design.md`

---

## Conventions

- **No new Go dependencies** — everything is stdlib + existing modules. `go.mod`/`go.sum` are not touched.
- **No real AWS in automated steps.** Every Go test uses a fake `Backend` (no network, no AWS). End-to-end testing against DynamoDB Local / real AWS is run by **you (the author)**, not the agent — see Task 8.
- **Do not run the no-arg TUI in checks** — `godynamo` with no args calls real AWS on startup (`DiscoverRegionsWithTables`). Only verify it *compiles*.
- **Commit trailer:** every `git commit` below must end with a blank line then:
  `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`
- **Prerequisites for Tasks 5–8:** Node.js + npm installed. DynamoDB Local (or AWS creds) only needed for Task 8.

## File structure

```
main.go                         # MODIFY: route "gui" subcommand → gui.Run()
internal/gui/                   # NEW Go package (launcher + HTTP bridge)
  backend.go                    #   Backend interface (*dynamo.Client satisfies it)
  cursor.go                     #   pagination cursor encode/decode (S/N/B keys)
  cursor_test.go                #   cursor round-trip tests
  server.go                     #   HTTP mux, token + CORS middleware, handlers, JSON conv
  server_test.go                #   handler/middleware tests against a fake Backend
  electron.go                   #   locate & spawn Electron (Windows-first)
  gui.go                        #   Run(): port, token, server, spawn, wait, shutdown
  gui_test.go                   #   token + bin-path unit tests
electron/                       # NEW front-end (dev-first, plain HTML/CSS/JS)
  package.json                  #   electron devDependency + "start" script
  main.js                       #   Electron main process + bridge-info IPC
  preload.js                    #   contextBridge → window.api (token in closure)
  renderer/index.html           #   connect screen + main view + detail overlay
  renderer/app.js               #   renderer logic (uses window.api)
  renderer/styles.css           #   dark theme
.gitignore                      # MODIFY: ignore electron/node_modules/
README.md                       # MODIFY: document the experimental GUI
```

---

## Task 1: Pagination cursor codec

DynamoDB key attributes are only `S`, `N`, or `B`. The cursor serializes `LastEvaluatedKey` to an opaque base64 token so the renderer can request the next page without understanding DynamoDB types.

**Files:**
- Create: `internal/gui/cursor.go`
- Test: `internal/gui/cursor_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/gui/cursor_test.go`:

```go
package gui

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

func TestCursorRoundTrip(t *testing.T) {
	key := map[string]types.AttributeValue{
		"pk": &types.AttributeValueMemberS{Value: "user#1"},
		"sk": &types.AttributeValueMemberN{Value: "42"},
	}
	enc, err := encodeCursor(key)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if enc == "" {
		t.Fatal("expected non-empty cursor")
	}
	dec, err := decodeCursor(enc)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	s, ok := dec["pk"].(*types.AttributeValueMemberS)
	if !ok || s.Value != "user#1" {
		t.Fatalf("pk mismatch: %#v", dec["pk"])
	}
	n, ok := dec["sk"].(*types.AttributeValueMemberN)
	if !ok || n.Value != "42" {
		t.Fatalf("sk mismatch: %#v", dec["sk"])
	}
}

func TestEmptyCursor(t *testing.T) {
	enc, err := encodeCursor(nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if enc != "" {
		t.Fatalf("want empty, got %q", enc)
	}
	dec, err := decodeCursor("")
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dec != nil {
		t.Fatalf("want nil, got %#v", dec)
	}
}

func TestDecodeInvalidCursor(t *testing.T) {
	if _, err := decodeCursor("!!!not-base64!!!"); err == nil {
		t.Fatal("expected error for invalid cursor")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/gui/ -run TestCursor -v` (and `TestEmptyCursor`, `TestDecodeInvalidCursor`)
Expected: build failure — `undefined: encodeCursor` / `undefined: decodeCursor`.

- [ ] **Step 3: Write the implementation**

Create `internal/gui/cursor.go`:

```go
package gui

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// encodeCursor serializes a DynamoDB LastEvaluatedKey into an opaque base64 token.
// DynamoDB key attributes are only S, N, or B, so only those are handled.
// An empty/nil key yields an empty string (meaning "no more pages").
func encodeCursor(key map[string]types.AttributeValue) (string, error) {
	if len(key) == 0 {
		return "", nil
	}
	wire := make(map[string]map[string]string, len(key))
	for name, av := range key {
		switch v := av.(type) {
		case *types.AttributeValueMemberS:
			wire[name] = map[string]string{"S": v.Value}
		case *types.AttributeValueMemberN:
			wire[name] = map[string]string{"N": v.Value}
		case *types.AttributeValueMemberB:
			wire[name] = map[string]string{"B": base64.StdEncoding.EncodeToString(v.Value)}
		default:
			return "", fmt.Errorf("unsupported key attribute type for %q", name)
		}
	}
	raw, err := json.Marshal(wire)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}

// decodeCursor reverses encodeCursor. An empty string yields a nil key.
func decodeCursor(cursor string) (map[string]types.AttributeValue, error) {
	if cursor == "" {
		return nil, nil
	}
	raw, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return nil, fmt.Errorf("invalid cursor: %w", err)
	}
	var wire map[string]map[string]string
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("invalid cursor: %w", err)
	}
	key := make(map[string]types.AttributeValue, len(wire))
	for name, typed := range wire {
		if v, ok := typed["S"]; ok {
			key[name] = &types.AttributeValueMemberS{Value: v}
		} else if v, ok := typed["N"]; ok {
			key[name] = &types.AttributeValueMemberN{Value: v}
		} else if v, ok := typed["B"]; ok {
			b, decErr := base64.StdEncoding.DecodeString(v)
			if decErr != nil {
				return nil, fmt.Errorf("invalid cursor binary for %q: %w", name, decErr)
			}
			key[name] = &types.AttributeValueMemberB{Value: b}
		} else {
			return nil, fmt.Errorf("unsupported cursor attribute for %q", name)
		}
	}
	return key, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/gui/ -v`
Expected: PASS (`TestCursorRoundTrip`, `TestEmptyCursor`, `TestDecodeInvalidCursor`).

- [ ] **Step 5: Commit**

```bash
git add internal/gui/cursor.go internal/gui/cursor_test.go
git commit -m "feat(gui): add pagination cursor codec for DynamoDB keys"
```

---

## Task 2: HTTP bridge server

The server holds one active `Backend` after `/connect`, gated by a Bearer token, with CORS for the Electron renderer.

**Files:**
- Create: `internal/gui/backend.go`
- Create: `internal/gui/server.go`
- Test: `internal/gui/server_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/gui/server_test.go`:

```go
package gui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/godynamo/internal/dynamo"
)

type fakeBackend struct {
	tables  []string
	info    *dynamo.TableInfo
	scan    *dynamo.ScanResult
	scanErr error
}

func (f *fakeBackend) ListTables(ctx context.Context) ([]string, error) { return f.tables, nil }

func (f *fakeBackend) DescribeTable(ctx context.Context, name string) (*dynamo.TableInfo, error) {
	return f.info, nil
}

func (f *fakeBackend) ScanTable(ctx context.Context, name string, limit int32,
	startKey map[string]types.AttributeValue, filterExpr string,
	names map[string]string, values map[string]interface{}) (*dynamo.ScanResult, error) {
	return f.scan, f.scanErr
}

func newTestServer(b Backend) *server {
	s := newServer("test-token")
	s.backend = b
	return s
}

func do(s *server, method, target string, body string) *httptest.ResponseRecorder {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
	}
	r.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, r)
	return rec
}

func TestRequiresToken(t *testing.T) {
	s := newTestServer(&fakeBackend{tables: []string{"a"}})
	r := httptest.NewRequest(http.MethodGet, "/tables", nil) // no Authorization header
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, r)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
}

func TestCORSPreflight(t *testing.T) {
	s := newServer("test-token")
	r := httptest.NewRequest(http.MethodOptions, "/connect", nil)
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, r)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") == "" {
		t.Fatal("missing CORS allow-origin header")
	}
}

func TestListTablesSorted(t *testing.T) {
	s := newTestServer(&fakeBackend{tables: []string{"b", "a"}})
	rec := do(s, http.MethodGet, "/tables", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Tables []string `json:"tables"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Tables) != 2 || resp.Tables[0] != "a" || resp.Tables[1] != "b" {
		t.Fatalf("want [a b], got %v", resp.Tables)
	}
}

func TestConnectStoresBackend(t *testing.T) {
	s := newServer("test-token")
	s.connectFn = func(req connectRequest) (Backend, error) {
		return &fakeBackend{tables: []string{"t2", "t1"}}, nil
	}
	rec := do(s, http.MethodPost, "/connect", `{"mode":"local","endpoint":"http://localhost:8000"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Tables []string `json:"tables"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Tables) != 2 || resp.Tables[0] != "t1" {
		t.Fatalf("want sorted tables, got %v", resp.Tables)
	}
	if _, ok := s.getBackend(); !ok {
		t.Fatal("backend not stored after connect")
	}
}

func TestConnectValidatesMode(t *testing.T) {
	s := newServer("test-token")
	rec := do(s, http.MethodPost, "/connect", `{"mode":"bogus"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestSchemaHandler(t *testing.T) {
	s := newTestServer(&fakeBackend{info: &dynamo.TableInfo{
		Name: "mytable", PartitionKey: "id", SortKey: "sk", RawJSON: `{"TableName":"mytable"}`,
	}})
	rec := do(s, http.MethodGet, "/tables/mytable/schema", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var resp struct {
		Info    map[string]interface{} `json:"info"`
		RawJSON string                 `json:"rawJSON"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Info["PartitionKey"] != "id" {
		t.Fatalf("want PartitionKey=id, got %v", resp.Info["PartitionKey"])
	}
	if resp.RawJSON == "" {
		t.Fatal("want rawJSON")
	}
}

func TestScanHandlerConvertsItemsAndCursor(t *testing.T) {
	s := newTestServer(&fakeBackend{
		scan: &dynamo.ScanResult{
			Items: []map[string]types.AttributeValue{
				{
					"id": &types.AttributeValueMemberS{Value: "x"},
					"n":  &types.AttributeValueMemberN{Value: "5"},
				},
			},
			Count: 1,
			LastEvaluatedKey: map[string]types.AttributeValue{
				"id": &types.AttributeValueMemberS{Value: "x"},
			},
		},
	})
	rec := do(s, http.MethodGet, "/tables/mytable/scan?limit=10", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items  []map[string]interface{} `json:"items"`
		Cursor string                   `json:"cursor"`
		Count  int                      `json:"count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("want 1 item, got %d", len(resp.Items))
	}
	if resp.Items[0]["id"] != "x" {
		t.Fatalf("want id=x, got %v", resp.Items[0]["id"])
	}
	if resp.Items[0]["n"] != float64(5) {
		t.Fatalf("want n=5, got %v (%T)", resp.Items[0]["n"], resp.Items[0]["n"])
	}
	if resp.Cursor == "" {
		t.Fatal("want non-empty cursor")
	}
}

func TestScanNotConnected(t *testing.T) {
	s := newServer("test-token") // no backend set
	rec := do(s, http.MethodGet, "/tables/x/scan", "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d", rec.Code)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/gui/ -v`
Expected: build failure — `undefined: Backend`, `undefined: newServer`, `undefined: connectRequest`, etc.

- [ ] **Step 3a: Write the Backend interface**

Create `internal/gui/backend.go`:

```go
package gui

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/godynamo/internal/dynamo"
)

// Backend is the narrow set of read-only DynamoDB operations the bridge needs.
// *dynamo.Client satisfies this interface; tests supply a fake.
type Backend interface {
	ListTables(ctx context.Context) ([]string, error)
	DescribeTable(ctx context.Context, name string) (*dynamo.TableInfo, error)
	ScanTable(ctx context.Context, name string, limit int32,
		startKey map[string]types.AttributeValue,
		filterExpr string, names map[string]string, values map[string]interface{}) (*dynamo.ScanResult, error)
}
```

- [ ] **Step 3b: Write the server**

Create `internal/gui/server.go`:

```go
package gui

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/godynamo/internal/dynamo"
	"github.com/godynamo/internal/models"
)

type connectRequest struct {
	Mode     string `json:"mode"` // "aws" or "local"
	Region   string `json:"region"`
	Endpoint string `json:"endpoint"`
}

type errorResponse struct {
	Error string `json:"error"`
}

type server struct {
	token     string
	mu        sync.RWMutex
	backend   Backend // nil until /connect succeeds
	connectFn func(req connectRequest) (Backend, error)
}

func newServer(token string) *server {
	return &server{token: token, connectFn: defaultConnect}
}

// defaultConnect builds a real *dynamo.Client from the request.
func defaultConnect(req connectRequest) (Backend, error) {
	cfg := dynamo.ConnectionConfig{
		Region:   req.Region,
		Endpoint: req.Endpoint,
		UseLocal: req.Mode == "local",
	}
	if req.Mode == "local" {
		if cfg.Region == "" {
			cfg.Region = "us-east-1"
		}
		cfg.AccessKey = "local"
		cfg.SecretKey = "local"
	}
	return dynamo.NewClient(cfg)
}

func (s *server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /connect", s.handleConnect)
	mux.HandleFunc("GET /tables", s.handleListTables)
	mux.HandleFunc("GET /tables/{name}/schema", s.handleSchema)
	mux.HandleFunc("GET /tables/{name}/scan", s.handleScan)
	return s.withMiddleware(mux)
}

func (s *server) withMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if !s.authorized(r) {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) authorized(r *http.Request) bool {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, prefix) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(h[len(prefix):]), []byte(s.token)) == 1
}

func (s *server) getBackend() (Backend, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.backend, s.backend != nil
}

func (s *server) handleConnect(w http.ResponseWriter, r *http.Request) {
	var req connectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	switch req.Mode {
	case "aws":
		if req.Region == "" {
			writeError(w, http.StatusBadRequest, "region is required for aws mode")
			return
		}
	case "local":
		if req.Endpoint == "" {
			writeError(w, http.StatusBadRequest, "endpoint is required for local mode")
			return
		}
	default:
		writeError(w, http.StatusBadRequest, `mode must be "aws" or "local"`)
		return
	}

	backend, err := s.connectFn(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to connect: "+err.Error())
		return
	}
	tables, err := backend.ListTables(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "connected but failed to list tables: "+err.Error())
		return
	}
	sort.Strings(tables)

	s.mu.Lock()
	s.backend = backend
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]interface{}{"tables": tables})
}

func (s *server) handleListTables(w http.ResponseWriter, r *http.Request) {
	backend, ok := s.getBackend()
	if !ok {
		writeError(w, http.StatusConflict, "not connected")
		return
	}
	tables, err := backend.ListTables(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	sort.Strings(tables)
	writeJSON(w, http.StatusOK, map[string]interface{}{"tables": tables})
}

func (s *server) handleSchema(w http.ResponseWriter, r *http.Request) {
	backend, ok := s.getBackend()
	if !ok {
		writeError(w, http.StatusConflict, "not connected")
		return
	}
	info, err := backend.DescribeTable(r.Context(), r.PathValue("name"))
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"info":    info,
		"rawJSON": info.RawJSON,
	})
}

func (s *server) handleScan(w http.ResponseWriter, r *http.Request) {
	backend, ok := s.getBackend()
	if !ok {
		writeError(w, http.StatusConflict, "not connected")
		return
	}
	name := r.PathValue("name")

	limit := int32(500)
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 1000 {
			limit = int32(parsed)
		}
	}

	startKey, err := decodeCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	result, err := backend.ScanTable(r.Context(), name, limit, startKey, "", nil, nil)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	cursor, err := encodeCursor(result.LastEvaluatedKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	items := make([]map[string]interface{}, len(result.Items))
	for i, item := range result.Items {
		converted := make(map[string]interface{}, len(item))
		for k, v := range item {
			converted[k] = models.AttributeValueToInterface(v)
		}
		items[i] = converted
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"items":  items,
		"cursor": cursor,
		"count":  result.Count,
	})
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/gui/ -v`
Expected: PASS for all tests (Task 1 + Task 2). Also run `go vet ./internal/gui/` → no output.

- [ ] **Step 5: Commit**

```bash
git add internal/gui/backend.go internal/gui/server.go internal/gui/server_test.go
git commit -m "feat(gui): add loopback HTTP bridge (connect/tables/schema/scan)"
```

---

## Task 3: GUI launcher + Electron spawner

Glue that binds a loopback port, mints a token, starts the server, and spawns Electron. `Run()` itself is verified end-to-end in Task 8; here we unit-test the pure helpers.

**Files:**
- Create: `internal/gui/electron.go`
- Create: `internal/gui/gui.go`
- Test: `internal/gui/gui_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/gui/gui_test.go`:

```go
package gui

import (
	"encoding/hex"
	"runtime"
	"strings"
	"testing"
)

func TestNewTokenUniqueAndHex(t *testing.T) {
	a, err := newToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := newToken()
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("tokens should be unique")
	}
	if len(a) != 64 { // 32 bytes hex-encoded
		t.Fatalf("want 64 hex chars, got %d", len(a))
	}
	if _, err := hex.DecodeString(a); err != nil {
		t.Fatalf("token is not valid hex: %v", err)
	}
}

func TestElectronBinPath(t *testing.T) {
	got := electronBinPath("/some/dir")
	want := "electron"
	if runtime.GOOS == "windows" {
		want = "electron.cmd"
	}
	if !strings.HasSuffix(got, want) {
		t.Fatalf("want path ending in %q, got %q", want, got)
	}
	if !strings.Contains(got, "node_modules") {
		t.Fatalf("expected node_modules in path, got %q", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/gui/ -run "TestNewToken|TestElectronBinPath" -v`
Expected: build failure — `undefined: newToken`, `undefined: electronBinPath`.

- [ ] **Step 3a: Write the Electron spawner**

Create `internal/gui/electron.go`:

```go
package gui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// startElectron locates the dev Electron binary and launches the app in ./electron,
// passing the bridge port and token via environment variables (not argv, so the
// token is not visible in process listings).
func startElectron(port int, token string) (*exec.Cmd, error) {
	dir, err := electronAppDir()
	if err != nil {
		return nil, err
	}
	bin := electronBinPath(dir)
	if _, statErr := os.Stat(bin); statErr != nil {
		return nil, fmt.Errorf(
			"Electron is not set up. Run:\n  cd %s\n  npm install\nthen re-run `godynamo gui`", dir)
	}

	cmd := exec.Command(bin, ".")
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("GODYNAMO_BRIDGE_PORT=%d", port),
		"GODYNAMO_BRIDGE_TOKEN="+token,
	)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start Electron: %w", err)
	}
	return cmd, nil
}

// electronAppDir returns the path to the ./electron app folder, preferring a path
// next to the executable and falling back to the current working directory (dev).
func electronAppDir() (string, error) {
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "electron")
		if _, statErr := os.Stat(candidate); statErr == nil {
			return candidate, nil
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Join(cwd, "electron"), nil
}

// electronBinPath returns the dev Electron binary inside the app's node_modules.
func electronBinPath(electronDir string) string {
	base := filepath.Join(electronDir, "node_modules", ".bin")
	if runtime.GOOS == "windows" {
		return filepath.Join(base, "electron.cmd")
	}
	return filepath.Join(base, "electron")
}
```

- [ ] **Step 3b: Write the launcher**

Create `internal/gui/gui.go`:

```go
package gui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"time"
)

// Run starts the loopback HTTP bridge and launches the Electron desktop app.
// It blocks until the Electron window is closed or the process is interrupted.
func Run(args []string) error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("failed to bind loopback port: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port

	token, err := newToken()
	if err != nil {
		return err
	}

	srv := &http.Server{Handler: newServer(token).handler()}
	go func() {
		if serveErr := srv.Serve(ln); serveErr != nil && serveErr != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "bridge server error: %v\n", serveErr)
		}
	}()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	fmt.Printf("GoDynamo GUI bridge listening on http://127.0.0.1:%d\n", port)

	electron, err := startElectron(port, token)
	if err != nil {
		return err
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	defer signal.Stop(sigCh)

	done := make(chan struct{})
	go func() {
		_ = electron.Wait()
		close(done)
	}()

	select {
	case <-sigCh:
		_ = electron.Process.Kill()
	case <-done:
	}
	return nil
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate token: %w", err)
	}
	return hex.EncodeToString(b), nil
}
```

- [ ] **Step 4: Run the tests and build**

Run: `go test ./internal/gui/ -v` → all PASS.
Run: `go build ./...` → no errors. `go vet ./internal/gui/` → no output.

- [ ] **Step 5: Commit**

```bash
git add internal/gui/electron.go internal/gui/gui.go internal/gui/gui_test.go
git commit -m "feat(gui): add launcher that starts the bridge and spawns Electron"
```

---

## Task 4: Route the `gui` subcommand in main.go

**Files:**
- Modify: `main.go`

- [ ] **Step 1: Replace `main.go`**

Replace the entire contents of `main.go` with:

```go
package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/godynamo/internal/app"
	"github.com/godynamo/internal/gui"
)

func main() {
	// `godynamo gui` launches the Electron desktop UI; everything else runs the TUI.
	if len(os.Args) > 1 && os.Args[1] == "gui" {
		if err := gui.Run(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "Error running GoDynamo GUI: %v\n", err)
			os.Exit(1)
		}
		return
	}

	model := app.New()

	// Note: Mouse capture is disabled to allow text selection in terminal
	p := tea.NewProgram(
		model,
		tea.WithAltScreen(),
	)

	if _, err := p.Run(); err != nil {
		fmt.Printf("Error running GoDynamo: %v\n", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 2: Build and vet**

Run: `go build ./...` → no errors. `go vet ./...` → no output.
(Do **not** run `godynamo` with no args — the TUI calls real AWS on startup.)

- [ ] **Step 3: Verify the gui route's error path (no AWS, Electron not yet installed)**

Run: `go run . gui`
Expected output: a line `GoDynamo GUI bridge listening on http://127.0.0.1:<port>` followed by the error:
`Error running GoDynamo GUI: Electron is not set up. Run: ... npm install ...` and a non-zero exit. This confirms routing + bridge startup + the friendly "not set up" message all work before Electron exists.

- [ ] **Step 4: Commit**

```bash
git add main.go
git commit -m "feat: route `godynamo gui` to the Electron launcher"
```

---

## Task 5: Electron shell (main process + preload + placeholder page)

Stand up the Electron app so a window opens and `window.api` is wired through the preload. A placeholder page proves the handshake before the real UI lands in Task 6.

**Files:**
- Create: `electron/package.json`
- Create: `electron/main.js`
- Create: `electron/preload.js`
- Create: `electron/renderer/index.html` (placeholder; replaced in Task 6)
- Modify: `.gitignore`

- [ ] **Step 1: Ignore node_modules**

Edit `.gitignore` — replace this block:

```
# Local config
.env
.env.local
```

with:

```
# Local config
.env
.env.local

# Electron front-end
electron/node_modules/
electron/dist/
```

- [ ] **Step 2: Create `electron/package.json`**

```json
{
  "name": "godynamo-gui",
  "version": "0.1.0",
  "description": "GoDynamo desktop GUI (Electron front-end)",
  "main": "main.js",
  "private": true,
  "scripts": {
    "start": "electron ."
  },
  "devDependencies": {
    "electron": "^33.0.0"
  }
}
```

- [ ] **Step 3: Create `electron/main.js`**

```js
const { app, BrowserWindow, ipcMain } = require('electron')
const path = require('path')

const PORT = process.env.GODYNAMO_BRIDGE_PORT
const TOKEN = process.env.GODYNAMO_BRIDGE_TOKEN

function createWindow() {
  const win = new BrowserWindow({
    width: 1200,
    height: 800,
    title: 'GoDynamo',
    backgroundColor: '#0b0f1a',
    webPreferences: {
      preload: path.join(__dirname, 'preload.js'),
      contextIsolation: true,
      nodeIntegration: false,
    },
  })
  win.loadFile(path.join(__dirname, 'renderer', 'index.html'))
}

// The renderer never sees the token directly; the preload fetches it once via IPC.
ipcMain.handle('bridge-info', () => ({
  baseUrl: `http://127.0.0.1:${PORT}`,
  token: TOKEN,
}))

app.whenReady().then(() => {
  createWindow()
  app.on('activate', () => {
    if (BrowserWindow.getAllWindows().length === 0) createWindow()
  })
})

app.on('window-all-closed', () => {
  app.quit()
})
```

- [ ] **Step 4: Create `electron/preload.js`**

```js
const { contextBridge, ipcRenderer } = require('electron')

let info = null
async function bridge() {
  if (!info) info = await ipcRenderer.invoke('bridge-info')
  return info
}

async function call(method, pathName, body) {
  const { baseUrl, token } = await bridge()
  const opts = { method, headers: { Authorization: `Bearer ${token}` } }
  if (body !== undefined) {
    opts.headers['Content-Type'] = 'application/json'
    opts.body = JSON.stringify(body)
  }
  const res = await fetch(baseUrl + pathName, opts)
  const text = await res.text()
  let data = {}
  try {
    data = text ? JSON.parse(text) : {}
  } catch {
    data = {}
  }
  if (!res.ok) {
    throw new Error(data.error || `request failed (${res.status})`)
  }
  return data
}

// The token is captured in this closure and never placed on window or in a URL.
contextBridge.exposeInMainWorld('api', {
  connect: (cfg) => call('POST', '/connect', cfg),
  listTables: () => call('GET', '/tables'),
  schema: (name) => call('GET', `/tables/${encodeURIComponent(name)}/schema`),
  scan: (name, cursor) => {
    const q = cursor ? `?cursor=${encodeURIComponent(cursor)}` : ''
    return call('GET', `/tables/${encodeURIComponent(name)}/scan${q}`)
  },
})
```

- [ ] **Step 5: Create the placeholder `electron/renderer/index.html`**

```html
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <title>GoDynamo</title>
</head>
<body style="background:#0b0f1a;color:#c8d3f5;font-family:sans-serif;padding:24px;">
  <h1>⚡ GoDynamo — bridge handshake</h1>
  <p id="status">Checking window.api…</p>
  <script>
    document.getElementById('status').textContent =
      window.api ? 'window.api is available ✓ (ready for the real UI)' : 'window.api MISSING ✗'
  </script>
</body>
</html>
```

- [ ] **Step 6: Install Electron**

Run: `cd electron && npm install` (installs Electron into `electron/node_modules`).
Expected: completes without errors; `electron/node_modules/.bin/electron.cmd` exists (Windows).

- [ ] **Step 7: Verify the window opens and the handshake works**

From the repo root, run: `go run . gui`
Expected: an Electron window titled **GoDynamo** opens showing *"window.api is available ✓"*. Closing the window returns the terminal to a prompt (the Go process exits cleanly). No AWS is contacted.

- [ ] **Step 8: Commit**

```bash
git add .gitignore electron/package.json electron/main.js electron/preload.js electron/renderer/index.html
git commit -m "feat(gui): add Electron shell with preloaded window.api bridge"
```

---

## Task 6: Renderer UI (connect → tables → grid → detail/schema)

Replace the placeholder with the real read-only UI.

**Files:**
- Modify: `electron/renderer/index.html`
- Create: `electron/renderer/styles.css`
- Create: `electron/renderer/app.js`

- [ ] **Step 1: Replace `electron/renderer/index.html`**

```html
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta http-equiv="Content-Security-Policy"
        content="default-src 'self'; connect-src http://127.0.0.1:* http://localhost:*; style-src 'self' 'unsafe-inline';" />
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
      <input type="text" id="table-filter" placeholder="Filter tables…" />
      <ul id="table-list"></ul>
    </aside>
    <main id="content">
      <header id="toolbar">
        <span id="current-table"></span>
        <button id="schema-btn" disabled>Schema</button>
        <button id="more-btn" disabled>Load more</button>
        <span id="status"></span>
      </header>
      <div id="grid-wrap">
        <table id="grid"><thead></thead><tbody></tbody></table>
      </div>
    </main>
  </section>

  <div id="detail" class="hidden">
    <div class="detail-card">
      <header>
        <span id="detail-title"></span>
        <button id="detail-close">✕</button>
      </header>
      <pre id="detail-body"></pre>
    </div>
  </div>

  <script src="app.js"></script>
</body>
</html>
```

- [ ] **Step 2: Create `electron/renderer/styles.css`**

```css
* { box-sizing: border-box; }
body { margin: 0; font-family: 'Segoe UI', sans-serif; background: #0b0f1a; color: #c8d3f5; height: 100vh; }
.hidden { display: none !important; }
.screen { height: 100vh; }

#connect-screen { display: flex; align-items: center; justify-content: center; }
.connect-card { background: #131a2b; padding: 32px; border-radius: 12px; border: 1px solid #2a3450; width: 360px; }
.connect-card h1 { margin-top: 0; color: #7aa2f7; }
.field { margin: 16px 0; display: flex; flex-direction: column; gap: 6px; }
.field label { font-size: 14px; }
select, input[type="text"], button {
  background: #0b0f1a; color: #c8d3f5; border: 1px solid #2a3450; border-radius: 6px; padding: 8px; font-size: 14px;
}
button { cursor: pointer; background: #2a3450; }
button:hover:not(:disabled) { background: #3a4670; }
button:disabled { opacity: 0.5; cursor: default; }
.error { color: #f7768e; min-height: 18px; }

#main-screen { display: flex; }
#sidebar { width: 260px; border-right: 1px solid #2a3450; padding: 12px; overflow-y: auto; }
#sidebar input { width: 100%; margin-bottom: 8px; }
#table-list { list-style: none; margin: 0; padding: 0; }
#table-list li { padding: 6px 8px; border-radius: 4px; cursor: pointer; font-size: 13px; }
#table-list li:hover { background: #1b2336; }
#table-list li.active { background: #2a3450; color: #7aa2f7; }
#content { flex: 1; display: flex; flex-direction: column; min-width: 0; }
#toolbar { display: flex; align-items: center; gap: 12px; padding: 12px; border-bottom: 1px solid #2a3450; }
#current-table { font-weight: bold; color: #7aa2f7; }
#status { margin-left: auto; font-size: 12px; color: #828bb8; }
#grid-wrap { flex: 1; overflow: auto; }
table#grid { border-collapse: collapse; width: 100%; font-size: 12px; }
#grid th, #grid td { border: 1px solid #2a3450; padding: 4px 8px; text-align: left; white-space: nowrap; }
#grid th { position: sticky; top: 0; background: #131a2b; }
#grid tbody tr:hover { background: #1b2336; cursor: pointer; }

#detail { position: fixed; inset: 0; background: rgba(0,0,0,0.6); display: flex; align-items: center; justify-content: center; }
.detail-card { background: #131a2b; border: 1px solid #2a3450; border-radius: 8px; width: 70%; max-height: 80%; display: flex; flex-direction: column; }
.detail-card header { display: flex; align-items: center; padding: 12px; border-bottom: 1px solid #2a3450; }
.detail-card header span { font-weight: bold; }
.detail-card header button { margin-left: auto; }
#detail-body { overflow: auto; padding: 16px; margin: 0; font-family: 'Cascadia Code', monospace; font-size: 12px; }
```

- [ ] **Step 3: Create `electron/renderer/app.js`**

```js
const AWS_REGIONS = [
  'us-east-1','us-east-2','us-west-1','us-west-2','af-south-1','ap-east-1',
  'ap-south-1','ap-south-2','ap-northeast-1','ap-northeast-2','ap-northeast-3',
  'ap-southeast-1','ap-southeast-2','ap-southeast-3','ap-southeast-4',
  'ca-central-1','eu-central-1','eu-central-2','eu-west-1','eu-west-2','eu-west-3',
  'eu-south-1','eu-south-2','eu-north-1','il-central-1','me-south-1','me-central-1','sa-east-1',
]

const state = {
  tables: [],
  currentTable: null,
  keys: { partition: '', sort: '' },
  schemaRaw: '',
  cursor: '',
  items: [],
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

async function selectTable(name) {
  state.currentTable = name
  state.cursor = ''
  state.items = []
  $('current-table').textContent = name
  $('status').textContent = 'Loading…'
  renderTableList()
  try {
    const schema = await window.api.schema(name)
    state.keys = {
      partition: (schema.info && schema.info.PartitionKey) || '',
      sort: (schema.info && schema.info.SortKey) || '',
    }
    state.schemaRaw = schema.rawJSON || JSON.stringify(schema.info, null, 2)
    $('schema-btn').disabled = false
    await loadPage(true)
  } catch (err) {
    $('status').textContent = 'Error: ' + err.message
  }
}

async function loadPage(reset) {
  try {
    const data = await window.api.scan(state.currentTable, reset ? '' : state.cursor)
    if (reset) state.items = []
    state.items = state.items.concat(data.items || [])
    state.cursor = data.cursor || ''
    $('more-btn').disabled = !state.cursor
    $('status').textContent = `${state.items.length} items` + (state.cursor ? ' (more available)' : '')
    renderGrid()
  } catch (err) {
    $('status').textContent = 'Error: ' + err.message
  }
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
  $('detail-title').textContent = 'Item'
  $('detail-body').textContent = JSON.stringify(state.items[idx], null, 2)
  show($('detail'))
}

function showSchema() {
  $('detail-title').textContent = 'Schema: ' + state.currentTable
  $('detail-body').textContent = state.schemaRaw || ''
  show($('detail'))
}

window.addEventListener('DOMContentLoaded', () => {
  initConnectScreen()
  $('table-filter').addEventListener('input', renderTableList)
  $('schema-btn').addEventListener('click', showSchema)
  $('more-btn').addEventListener('click', () => loadPage(false))
  $('detail-close').addEventListener('click', () => hide($('detail')))
})
```

- [ ] **Step 4: Smoke-check that the app loads (no data needed)**

From the repo root, run: `go run . gui`
Expected: the Electron window shows the **connect screen** (AWS/Local radios, region dropdown, Connect button). Toggling to "DynamoDB Local" reveals the endpoint field. No console errors in DevTools (`Ctrl+Shift+I`). Don't click Connect yet (that needs a live backend — Task 8).

- [ ] **Step 5: Commit**

```bash
git add electron/renderer/index.html electron/renderer/styles.css electron/renderer/app.js
git commit -m "feat(gui): add read-only renderer UI (connect, tables, grid, detail, schema)"
```

---

## Task 7: Document the GUI in the README

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Add a GUI section**

Insert the following block in `README.md` immediately **after** the "## 🚀 Quick Start" section (before "## 🎯 Filter Builder"):

```markdown
---

## 🖥️ Desktop GUI (experimental, Windows-first)

GoDynamo ships an optional Electron desktop UI for **read-only** browsing
(connect → list tables → scan/browse → inspect item JSON → view schema).
The terminal UI remains the default; the GUI is launched with the `gui` subcommand.

### One-time setup (requires Node.js + npm)

```bash
cd electron
npm install
cd ..
```

### Launch

```bash
go run . gui
# or, after building:
go build -o godynamo.exe .
./godynamo.exe gui
```

On launch you choose **AWS** (pick a region; uses your default credentials) or
**DynamoDB Local** (default endpoint `http://localhost:8000`). The Go process starts
a loopback-only HTTP bridge (127.0.0.1, random port, one-time token) and opens the
Electron window; closing the window shuts everything down.

> Status: read-only v1. CRUD, the visual filter builder, export, and packaged
> installers are planned for later phases.
```

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs: document the experimental godynamo gui mode"
```

---

## Task 8: End-to-end manual verification (run by the author)

These steps exercise live data and are **run by you**, not the agent (per the no-real-AWS constraint). Use DynamoDB Local to avoid AWS entirely, and/or a real region if you choose.

- [ ] **Step 1 (Local path): start DynamoDB Local and seed a table**

In one terminal (example using the official Docker image):

```bash
docker run -p 8000:8000 amazon/dynamodb-local
```

In another terminal, create a table + a couple of items (this hits **Local**, not AWS):

```bash
aws dynamodb create-table --endpoint-url http://localhost:8000 \
  --table-name Demo --billing-mode PAY_PER_REQUEST \
  --attribute-definitions AttributeName=id,AttributeType=S \
  --key-schema AttributeName=id,KeyType=HASH

aws dynamodb put-item --endpoint-url http://localhost:8000 \
  --table-name Demo --item '{"id":{"S":"1"},"name":{"S":"Alice"}}'
aws dynamodb put-item --endpoint-url http://localhost:8000 \
  --table-name Demo --item '{"id":{"S":"2"},"name":{"S":"Bob"}}'
```

- [ ] **Step 2: Launch the GUI and connect to Local**

Run `go run . gui`. In the window: choose **DynamoDB Local**, keep `http://localhost:8000`, click **Connect**.
Expected: the sidebar lists `Demo`.

- [ ] **Step 3: Browse, inspect, schema**

- Click `Demo` → grid shows the items with `id` as the first column.
- Click a row → the detail overlay shows that item's pretty JSON.
- Click **Schema** → the overlay shows the `DescribeTable` JSON.
- If the table has many items, **Load more** is enabled and pages via the cursor.

- [ ] **Step 4 (optional, real AWS — your call): connect to a region**

Run `go run . gui`, choose **AWS**, pick your region, click **Connect**, and confirm your real tables list and browse. (You run this; the assistant does not.)

- [ ] **Step 5: Lifecycle check**

Close the Electron window → the terminal returns to a prompt with no lingering Go process. Re-run and press `Ctrl+C` in the terminal → the window closes and the process exits.

- [ ] **Step 6: Report results**

Note anything off (errors, mis-ordered columns, blank cells) so the next phase can address it. No commit for this task unless fixes are made.

---

## Self-review (performed against the spec)

**1. Spec coverage**

| Spec requirement | Task |
|---|---|
| `godynamo gui` subcommand, TUI stays default | Task 4 |
| Loopback HTTP bridge, 127.0.0.1, random port | Task 3 (`gui.go`) |
| One-time token, Bearer auth, constant-time compare | Task 2 (`authorized`) + Task 3 (`newToken`) |
| CORS for renderer + OPTIONS preflight | Task 2 (`withMiddleware`) |
| `POST /connect` (aws/local), validate via ListTables | Task 2 (`handleConnect`) |
| `GET /tables` sorted | Task 2 (`handleListTables`) |
| `GET /tables/{name}/schema` (info + rawJSON) | Task 2 (`handleSchema`) |
| `GET /tables/{name}/scan` (items as JSON, cursor, count) | Task 2 (`handleScan`) |
| Pagination cursor (S/N/B), opaque base64 | Task 1 |
| AttributeValue→JSON via `models` | Task 2 (`handleScan`) |
| Spawn Electron, env-passed port+token, Windows path | Task 3 (`electron.go`) |
| "Electron not set up" friendly error | Task 3 + verified Task 4 Step 3 |
| Electron `contextIsolation`, preload `window.api`, token in closure | Task 5 |
| Connect screen (AWS region / Local endpoint) | Task 6 |
| Tables sidebar + client-side filter | Task 6 |
| Data grid, PK/SK columns first | Task 6 (`columnOrder`) |
| Item JSON detail (no endpoint) | Task 6 (`showItem`) |
| Schema JSON view | Task 6 (`showSchema`) |
| "Load more" via cursor | Task 6 (`loadPage`) |
| No new Go deps; internal packages untouched | All (verified `go build ./...`) |
| No-real-AWS testing; author runs e2e | Tasks 1–3 (fakes) + Task 8 |
| `.gitignore` electron/node_modules | Task 5 |
| README documents the mode | Task 7 |

No gaps found.

**2. Placeholder scan:** No "TBD"/"TODO"/"handle edge cases"/"similar to Task N". Every code step contains complete file contents. ✓

**3. Type/name consistency:** `Backend`, `server`, `newServer`, `connectFn`, `connectRequest{Mode,Region,Endpoint}`, `getBackend`, `encodeCursor`/`decodeCursor`, `newToken`, `electronBinPath`/`electronAppDir`/`startElectron`, and `gui.Run` are used identically across tasks. JSON keys (`tables`, `info`, `rawJSON`, `items`, `cursor`, `count`) match between Go handlers (Task 2), preload (`window.api`, Task 5), and renderer (`schema.info.PartitionKey`, `data.cursor`, etc., Task 6). ✓
