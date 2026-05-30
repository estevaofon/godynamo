# GoDynamo GUI-as-Default Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the Electron desktop GUI the default `godynamo` command, move the terminal UI behind a `tui` subcommand (keeping `gui` as an alias), and auto-install Electron on first run behind a native "please wait" window.

**Architecture:** A pure `selectMode` function in `package main` picks GUI (default) vs TUI from the args. `gui.Run` resolves the Electron app dir once, calls a new `ensureElectron` that installs npm deps (with a native splash window) when the Electron binary is missing, then launches as before. The install/lookup/splash are wired through package-level function variables ("seams") so the logic is unit-tested without running npm or opening a real window.

**Tech Stack:** Go (stdlib `os/exec`, `runtime`, `unicode/utf16`, `encoding/base64`), Bubble Tea (existing TUI), Electron (existing GUI), PowerShell/WinForms (Windows wait window).

**Spec:** `docs/superpowers/specs/2026-05-30-godynamo-gui-default-design.md`

**Constraints:** No real AWS (Estevão runs live tests). Tests never run npm or open a real window — they swap the seams. Repo uses Conventional Commits (`feat(...)`, `docs:`). Run commands from the repo root in PowerShell; `go` commands are identical cross-shell.

---

## File Structure

| File | Responsibility |
|---|---|
| `main.go` | `mode` type + consts, pure `selectMode`, thin `main`, extracted `runTUI`. |
| `main_test.go` | **new** — table test for `selectMode`. |
| `internal/gui/splash.go` | **new** — `defaultStartSplash` (native window / terminal fallback), `splashScript`, `encodePowershell`. |
| `internal/gui/splash_test.go` | **new** — pure tests for `encodePowershell` + `splashScript`. |
| `internal/gui/install.go` | **new** — `ensureElectron`, npm lookup/run, the three seams. |
| `internal/gui/install_test.go` | **new** — install-path tests via the seams. |
| `internal/gui/electron.go` | `startElectron` takes `dir`; stale message wording fix. |
| `internal/gui/gui.go` | `Run` resolves `dir`, calls `ensureElectron`, passes `dir` to `startElectron`. |
| `internal/gui/gui_test.go` | add a `startElectron` missing-binary test (locks wording + new signature). |
| `README.md` | GUI = default; document `tui`; auto-install note. |

---

## Task 1: CLI dispatch — GUI default, `tui` subcommand, `gui` alias

**Files:**
- Modify: `main.go` (whole file)
- Test: `main_test.go` (new)

- [ ] **Step 1: Write the failing test**

Create `main_test.go`:

```go
package main

import (
	"reflect"
	"testing"
)

func TestSelectMode(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantMode mode
		wantRest []string
	}{
		{"bare default", nil, modeGUI, nil},
		{"empty slice", []string{}, modeGUI, []string{}},
		{"gui alias", []string{"gui"}, modeGUI, []string{}},
		{"gui with flags", []string{"gui", "--port", "9"}, modeGUI, []string{"--port", "9"}},
		{"tui", []string{"tui"}, modeTUI, []string{}},
		{"tui with extra", []string{"tui", "x"}, modeTUI, []string{"x"}},
		{"unknown arg", []string{"xyz"}, modeGUI, []string{"xyz"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMode, gotRest := selectMode(tt.args)
			if gotMode != tt.wantMode {
				t.Errorf("mode = %v, want %v", gotMode, tt.wantMode)
			}
			if !reflect.DeepEqual(gotRest, tt.wantRest) {
				t.Errorf("rest = %#v, want %#v", gotRest, tt.wantRest)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test . -run TestSelectMode -v`
Expected: build failure — `undefined: mode`, `undefined: modeGUI`, `undefined: selectMode`.

- [ ] **Step 3: Rewrite `main.go`**

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

type mode int

const (
	modeGUI mode = iota
	modeTUI
)

// selectMode decides which interface to launch from the CLI args (os.Args[1:]).
// Default is the GUI; `tui` selects the terminal UI; `gui` is an accepted alias
// for the default and is stripped so trailing flags pass through to gui.Run.
func selectMode(args []string) (mode, []string) {
	if len(args) > 0 && args[0] == "tui" {
		return modeTUI, args[1:]
	}
	if len(args) > 0 && args[0] == "gui" {
		return modeGUI, args[1:]
	}
	return modeGUI, args
}

func main() {
	m, rest := selectMode(os.Args[1:])
	if m == modeTUI {
		runTUI()
		return
	}
	if err := gui.Run(rest); err != nil {
		fmt.Fprintf(os.Stderr, "Error running GoDynamo GUI: %v\n", err)
		os.Exit(1)
	}
}

// runTUI launches the Bubble Tea terminal UI (mouse capture stays off so text
// selection works in the terminal).
func runTUI() {
	model := app.New()
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

- [ ] **Step 4: Run test to verify it passes**

Run: `go test . -run TestSelectMode -v`
Expected: PASS (all 7 subtests).

- [ ] **Step 5: Verify the package still builds**

Run: `go build ./...`
Expected: no output, exit 0.

- [ ] **Step 6: Commit**

```bash
git add main.go main_test.go
git commit -m "feat(cli): default to GUI, add tui subcommand, keep gui alias"
```

---

## Task 2: Native wait-window splash

**Files:**
- Create: `internal/gui/splash.go`
- Test: `internal/gui/splash_test.go` (new)

- [ ] **Step 1: Write the failing test**

Create `internal/gui/splash_test.go`:

```go
package gui

import (
	"encoding/base64"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestEncodePowershell(t *testing.T) {
	got := encodePowershell("Hi")
	// "Hi" as UTF-16LE bytes: 0x48 0x00 0x69 0x00
	want := base64.StdEncoding.EncodeToString([]byte{0x48, 0x00, 0x69, 0x00})
	if got != want {
		t.Fatalf("encodePowershell = %q, want %q", got, want)
	}

	// Round-trips back to the original through UTF-16LE.
	raw, err := base64.StdEncoding.DecodeString(got)
	if err != nil {
		t.Fatal(err)
	}
	u := make([]uint16, 0, len(raw)/2)
	for i := 0; i+1 < len(raw); i += 2 {
		u = append(u, uint16(raw[i])|uint16(raw[i+1])<<8)
	}
	if back := string(utf16.Decode(u)); back != "Hi" {
		t.Fatalf("round-trip mismatch: %q", back)
	}
}

func TestSplashScriptContainsMarkers(t *testing.T) {
	s := splashScript("WAIT_MARKER")
	for _, want := range []string{"WAIT_MARKER", "Marquee", "TopMost", "ShowDialog"} {
		if !strings.Contains(s, want) {
			t.Errorf("splashScript missing %q", want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/gui/ -run "TestEncodePowershell|TestSplashScriptContainsMarkers" -v`
Expected: build failure — `undefined: encodePowershell`, `undefined: splashScript`.

- [ ] **Step 3: Create `internal/gui/splash.go`**

```go
package gui

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"unicode/utf16"
)

// startSplash shows a "please wait" indicator during the one-time Electron
// install and returns a function that dismisses it. It is a package variable so
// tests can swap it out. See defaultStartSplash for the real behavior.
//
// (The variable itself is declared in install.go alongside the other seams.)

// defaultStartSplash pops a native WinForms window on Windows (via PowerShell)
// and returns a closer that kills it. On other OSes — or if PowerShell can't be
// started — it degrades to a single stderr line with the same signature.
func defaultStartSplash(message string) func() {
	if runtime.GOOS != "windows" {
		fmt.Fprintln(os.Stderr, message)
		return func() {}
	}
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive",
		"-EncodedCommand", encodePowershell(splashScript(message)))
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, message)
		return func() {}
	}
	return func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill() // closes the window when the install finishes
		}
	}
}

// splashScript builds a small WinForms script: a centered, top-most dialog with
// the message, a "(pode levar alguns minutos)" hint, and an indeterminate
// marquee progress bar. ShowDialog blocks until the process is killed.
func splashScript(message string) string {
	msg := strings.ReplaceAll(message, "'", "''") // escape for PS single-quoted string
	return `Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
$f = New-Object System.Windows.Forms.Form
$f.Text = 'GoDynamo'
$f.Width = 380
$f.Height = 150
$f.StartPosition = 'CenterScreen'
$f.FormBorderStyle = 'FixedDialog'
$f.ControlBox = $false
$f.TopMost = $true
$l = New-Object System.Windows.Forms.Label
$l.Text = '` + msg + `'
$l.AutoSize = $false
$l.SetBounds(20, 20, 340, 30)
$s = New-Object System.Windows.Forms.Label
$s.Text = '(pode levar alguns minutos)'
$s.AutoSize = $false
$s.SetBounds(20, 50, 340, 20)
$p = New-Object System.Windows.Forms.ProgressBar
$p.Style = 'Marquee'
$p.MarqueeAnimationSpeed = 30
$p.SetBounds(20, 80, 340, 20)
$f.Controls.Add($l)
$f.Controls.Add($s)
$f.Controls.Add($p)
[void]$f.ShowDialog()`
}

// encodePowershell encodes a script as UTF-16LE then base64 for PowerShell's
// -EncodedCommand. This sidesteps console code-page and quoting problems with
// the accented message ("instalação") and the ellipsis.
func encodePowershell(script string) string {
	u := utf16.Encode([]rune(script))
	buf := make([]byte, 0, len(u)*2)
	for _, r := range u {
		buf = append(buf, byte(r), byte(r>>8)) // little-endian
	}
	return base64.StdEncoding.EncodeToString(buf)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/gui/ -run "TestEncodePowershell|TestSplashScriptContainsMarkers" -v`
Expected: PASS. (`go vet` note: `defaultStartSplash` is currently unused at package level — that is fine in Go and it gets wired up in Task 3.)

- [ ] **Step 5: Verify the package builds**

Run: `go build ./...`
Expected: no output, exit 0.

- [ ] **Step 6: Commit**

```bash
git add internal/gui/splash.go internal/gui/splash_test.go
git commit -m "feat(gui): native wait-window splash for Electron install"
```

---

## Task 3: Auto-install Electron (`ensureElectron`) with seams

**Files:**
- Create: `internal/gui/install.go`
- Test: `internal/gui/install_test.go` (new)

Reuses existing helpers `electronBinPath` (`internal/gui/electron.go:59`) and `defaultStartSplash` (Task 2).

- [ ] **Step 1: Write the failing test**

Create `internal/gui/install_test.go`:

```go
package gui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeBin creates a fake Electron binary so electronBinPath(dir) exists.
func makeBin(t *testing.T, dir string) {
	t.Helper()
	bin := electronBinPath(dir)
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func swapLookNpm(t *testing.T, f func() (string, error)) {
	t.Helper()
	orig := lookNpm
	lookNpm = f
	t.Cleanup(func() { lookNpm = orig })
}

func swapInstall(t *testing.T, f func(npm, dir string) error) {
	t.Helper()
	orig := runNpmInstall
	runNpmInstall = f
	t.Cleanup(func() { runNpmInstall = orig })
}

func swapSplash(t *testing.T, f func(string) func()) {
	t.Helper()
	orig := startSplash
	startSplash = f
	t.Cleanup(func() { startSplash = orig })
}

func TestEnsureElectronAlreadyInstalled(t *testing.T) {
	dir := t.TempDir()
	makeBin(t, dir)

	installed := false
	swapInstall(t, func(npm, dir string) error { installed = true; return nil })

	if err := ensureElectron(dir); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
	if installed {
		t.Fatal("npm install must not run when Electron is already present")
	}
}

func TestEnsureElectronNpmMissing(t *testing.T) {
	dir := t.TempDir() // no binary
	swapLookNpm(t, func() (string, error) { return "", os.ErrNotExist })

	err := ensureElectron(dir)
	if err == nil {
		t.Fatal("want error when npm is missing")
	}
	for _, want := range []string{"Node.js", "godynamo tui"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err.Error(), want)
		}
	}
}

func TestEnsureElectronInstallSucceeds(t *testing.T) {
	dir := t.TempDir() // no binary yet
	swapLookNpm(t, func() (string, error) { return "npm", nil })
	opened, closed := false, false
	swapSplash(t, func(string) func() {
		opened = true
		return func() { closed = true }
	})
	swapInstall(t, func(npm, d string) error { makeBin(t, d); return nil })

	if err := ensureElectron(dir); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
	if !opened || !closed {
		t.Fatalf("splash open=%v close=%v, want both true", opened, closed)
	}
}

func TestEnsureElectronInstallFails(t *testing.T) {
	dir := t.TempDir()
	swapLookNpm(t, func() (string, error) { return "npm", nil })
	closed := false
	swapSplash(t, func(string) func() { return func() { closed = true } })
	swapInstall(t, func(npm, d string) error { return os.ErrPermission })

	if err := ensureElectron(dir); err == nil {
		t.Fatal("want error when install fails")
	}
	if !closed {
		t.Fatal("splash must be closed even when install fails")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/gui/ -run TestEnsureElectron -v`
Expected: build failure — `undefined: lookNpm`, `undefined: runNpmInstall`, `undefined: startSplash`, `undefined: ensureElectron`.

- [ ] **Step 3: Create `internal/gui/install.go`**

```go
package gui

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

// Seams: swapped in tests so the install path runs without a real npm or window.
var (
	lookNpm       = defaultLookNpm
	runNpmInstall = defaultRunNpmInstall
	startSplash   = defaultStartSplash
)

// ensureElectron makes sure the dev Electron binary exists under dir, installing
// the npm dependencies automatically (behind a wait window) on first run. When
// the binary is already present it returns immediately — the normal fast path.
func ensureElectron(dir string) error {
	if _, err := os.Stat(electronBinPath(dir)); err == nil {
		return nil
	}
	npm, err := lookNpm()
	if err != nil {
		return fmt.Errorf(
			"the desktop GUI needs Electron, but Node.js/npm was not found.\n" +
				"Install Node.js (https://nodejs.org) and re-run `godynamo`,\n" +
				"or use the terminal UI instead: `godynamo tui`")
	}
	stop := startSplash("Aguarde enquanto fazemos a instalação do Electron…")
	defer stop()
	if err := runNpmInstall(npm, dir); err != nil {
		return fmt.Errorf("installing Electron dependencies failed: %w", err)
	}
	if _, err := os.Stat(electronBinPath(dir)); err != nil {
		return fmt.Errorf("Electron install finished but %s is still missing", electronBinPath(dir))
	}
	return nil
}

// defaultLookNpm finds the npm executable on PATH.
func defaultLookNpm() (string, error) { return exec.LookPath(npmName()) }

func npmName() string {
	if runtime.GOOS == "windows" {
		return "npm.cmd"
	}
	return "npm"
}

// defaultRunNpmInstall runs `npm install` in dir, streaming progress to stderr
// so stdout stays clean.
func defaultRunNpmInstall(npm, dir string) error {
	cmd := exec.Command(npm, "install")
	cmd.Dir = dir
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/gui/ -run TestEnsureElectron -v`
Expected: PASS (all 4 tests).

- [ ] **Step 5: Run the whole gui package + build**

Run: `go test ./internal/gui/ -v` then `go build ./...`
Expected: all gui tests PASS; build clean. (`ensureElectron` is still unused by non-test code until Task 4 — fine in Go.)

- [ ] **Step 6: Commit**

```bash
git add internal/gui/install.go internal/gui/install_test.go
git commit -m "feat(gui): auto-install Electron deps on first GUI run"
```

---

## Task 4: Wire `ensureElectron` into `Run`; pass `dir` to `startElectron`

**Files:**
- Modify: `internal/gui/electron.go:14-26` (signature + wording)
- Modify: `internal/gui/gui.go:18-47` (resolve dir, ensure, pass dir)
- Test: `internal/gui/gui_test.go` (add missing-binary test)

- [ ] **Step 1: Write the failing test**

Append this function to `internal/gui/gui_test.go`. Its import block already has `strings` and `testing`, and `t.TempDir()` needs no new import, so imports are unchanged.

```go
func TestStartElectronMissingBinaryMessage(t *testing.T) {
	dir := t.TempDir() // no node_modules/.bin/electron under here
	_, err := startElectron(dir, 0, "token")
	if err == nil {
		t.Fatal("want error when the Electron binary is missing")
	}
	if !strings.Contains(err.Error(), "not set up") {
		t.Errorf("error %q should mention 'not set up'", err.Error())
	}
	if !strings.Contains(err.Error(), "re-run `godynamo`") {
		t.Errorf("error %q should tell the user to re-run `godynamo`", err.Error())
	}
	if strings.Contains(err.Error(), "godynamo gui") {
		t.Errorf("error %q should no longer reference `godynamo gui`", err.Error())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/gui/ -run TestStartElectronMissingBinaryMessage -v`
Expected: build failure — `too many arguments in call to startElectron` (it still has the old 2-arg signature).

- [ ] **Step 3: Update `startElectron` in `internal/gui/electron.go`**

Replace the function header and the binary-check block (lines 14-26). Change the signature to take `dir`, delete the internal `electronAppDir()` call, and fix the message. The new top of the function:

```go
// startElectron launches the dev Electron app in dir, passing the bridge port
// and token via environment variables (not argv, so the token is not visible in
// process listings). dir is resolved by the caller (see Run).
func startElectron(dir string, port int, token string) (*exec.Cmd, error) {
	bin := electronBinPath(dir)
	if _, statErr := os.Stat(bin); statErr != nil {
		if os.IsNotExist(statErr) {
			return nil, fmt.Errorf(
				"Electron is not set up. Run:\n  cd %s\n  npm install\nthen re-run `godynamo`", dir)
		}
		return nil, fmt.Errorf("checking Electron binary: %w", statErr)
	}
```

Leave everything from `cmd := exec.Command(bin, ".")` onward unchanged. The `electronAppDir` and `electronBinPath` helpers stay in the file (still used by `Run` and `electronBinPath` here).

- [ ] **Step 4: Update `Run` in `internal/gui/gui.go`**

Replace the start of `Run` (the body up to and including the `net.Listen` call). Old (lines 18-23):

```go
func Run(args []string) error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("failed to bind loopback port: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
```

New:

```go
func Run(args []string) error {
	dir, err := electronAppDir()
	if err != nil {
		return err
	}
	if err := ensureElectron(dir); err != nil {
		return err
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("failed to bind loopback port: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
```

Then update the `startElectron` call (line 44) from:

```go
	electron, err := startElectron(port, token)
```

to:

```go
	electron, err := startElectron(dir, port, token)
```

- [ ] **Step 5: Run the new test + full gui package**

Run: `go test ./internal/gui/ -v`
Expected: PASS, including `TestStartElectronMissingBinaryMessage` and the existing `TestNewTokenUniqueAndHex`, `TestElectronBinPath`.

- [ ] **Step 6: Build + vet the whole module**

Run: `go build ./...` then `go vet ./...`
Expected: both clean, exit 0. (`ensureElectron` is now used by `Run`; no unused-symbol concerns remain.)

- [ ] **Step 7: Commit**

```bash
git add internal/gui/gui.go internal/gui/electron.go internal/gui/gui_test.go
git commit -m "feat(gui): wire ensureElectron into Run; pass dir to startElectron"
```

---

## Task 5: Documentation — GUI is default, `tui`, auto-install

**Files:**
- Modify: `README.md` (Quick Start + Desktop GUI section)

No tests (docs only). Make these four exact edits.

- [ ] **Step 1: Quick Start run block**

Replace:

```
2. **Run GoDynamo**:
```bash
./godynamo
```
```

with:

```
2. **Run GoDynamo**:
```bash
./godynamo        # opens the desktop GUI (default)
./godynamo tui    # opens the terminal UI instead
```

On first run, if the desktop GUI's Electron dependencies aren't installed yet,
GoDynamo installs them automatically (showing a wait window) and then opens.
```

- [ ] **Step 2: Desktop GUI intro line**

Replace:

```
The terminal UI remains the default; the GUI is launched with the `gui` subcommand.
```

with:

```
The desktop GUI is the default (`godynamo`); run `godynamo tui` for the terminal UI.
`godynamo gui` still works as an explicit alias.
```

- [ ] **Step 3: Setup section (now automatic)**

Replace:

```
### One-time setup (requires Node.js + npm)

```bash
cd electron
npm install
cd ..
```
```

with:

```
### Setup (automatic on first run)

The first time you launch the GUI, GoDynamo runs `npm install` in `electron/`
for you (showing a wait window) if it hasn't been done yet — you just need
**Node.js + npm** on your PATH. To do it manually instead:

```bash
cd electron
npm install
cd ..
```
```

- [ ] **Step 4: Launch block**

Replace:

```
```bash
go run . gui
# or, after building:
go build -o godynamo.exe .
./godynamo.exe gui
```
```

with:

```
```bash
go run .            # GUI is the default
go run . tui        # terminal UI instead
# or, after building:
go build -o godynamo.exe .
./godynamo.exe      # GUI (use `./godynamo.exe tui` for the terminal UI)
```
```

- [ ] **Step 5: Sanity-check the edits**

Run: `git diff --stat README.md`
Expected: `README.md` shows changes (roughly +12/-7 lines). Skim `git diff README.md` to confirm no stray fences were broken.

- [ ] **Step 6: Commit**

```bash
git add README.md
git commit -m "docs: GUI is default; document tui subcommand and auto-install"
```

---

## Final Verification

Run from the repo root; all must be green before calling the work done:

- [ ] `go build ./...` — exit 0, no output.
- [ ] `go vet ./...` — exit 0, no findings.
- [ ] `go test ./...` — all packages PASS (new: `TestSelectMode`, `TestEncodePowershell`, `TestSplashScriptContainsMarkers`, `TestEnsureElectron*`, `TestStartElectronMissingBinaryMessage`).

Manual checks (Estevão — not automated, no real AWS needed for the dispatch/install paths):

- [ ] `go run . tui` opens the terminal UI.
- [ ] `go run .` (with `electron/node_modules` present) opens the GUI directly, no window.
- [ ] Remove `electron/node_modules`, run `go run .` → native wait window appears, `npm install` runs, window closes, GUI opens.
- [ ] Temporarily remove `npm` from PATH, run `go run .` → prints the Node.js message mentioning `godynamo tui` and exits non-zero (no partial install).
- [ ] `go run . gui` still launches the GUI (alias).
