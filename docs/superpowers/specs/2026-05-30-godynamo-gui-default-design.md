# GoDynamo — GUI as Default, `tui` Subcommand, Auto-Install Design Spec

- **Date:** 2026-05-30
- **Status:** Approved (design); ready for implementation planning.
- **Builds on:** the Electron desktop GUI (`docs/superpowers/specs/2026-05-28-godynamo-electron-gui-design.md`) and the `gui`/TUI split in `main.go`, on `develop`.
- **Scope:** make the desktop GUI the **default** when running `godynamo` with no subcommand, move the terminal UI behind a new `tui` subcommand, and — because the GUI is now the default path — **auto-install Electron** on first run (showing a native "please wait" window) instead of erroring out. Go-side only; no renderer (`electron/`) changes.

## 1. Goal

Today `godynamo gui` launches the Electron desktop UI and everything else (including a bare `godynamo`) launches the Bubble Tea terminal UI (`main.go:14`). We are flipping that: the GUI becomes the default and the terminal UI is reached with `godynamo tui`. Because the default invocation now depends on the Electron install (`electron/node_modules`), a fresh machine running `godynamo` must not dead-end on the current "Electron is not set up" error (`electron.go:22`). Instead, the program installs the Electron dependencies automatically, shows a native wait window while it runs, and then opens the GUI.

## 2. Decisions locked during brainstorming

| Decision | Choice |
|---|---|
| Default interface | **GUI.** A bare `godynamo` launches the Electron desktop UI. |
| Terminal UI trigger | **`tui` subcommand.** `godynamo tui` runs the Bubble Tea terminal UI. |
| `gui` subcommand | **Kept as an alias.** `godynamo gui` still launches the GUI; the `gui` token is stripped so trailing flags pass through to `gui.Run`. |
| Unknown args | **Permissive → GUI.** Anything that is not `tui` (typos, future flags) launches the GUI, mirroring today's permissive dispatch. |
| Electron not installed | **Auto-install, no confirmation.** Run `npm install` in `electron/` automatically, then launch the GUI. |
| Wait UI during install | **Native Windows window** (PowerShell/WinForms): centered, top-most, with the text "Aguarde enquanto fazemos a instalação do Electron…" and a marquee progress bar; it closes when the install finishes and the GUI opens. **Fallback:** a one-line terminal message on macOS/Linux or if PowerShell can't start. |
| Node.js / npm missing | **Clear error, no silent fallback.** If `npm` is not on PATH we cannot auto-install, so print a message to install Node.js (and offer `godynamo tui`) and exit non-zero. |
| Verification | **No real AWS** (Estevão runs live tests). Automated checks are Go-only: `go build ./...`, `go vet ./...`, `go test ./...`. The npm/PowerShell paths are exercised through injectable seams, never by really running npm or opening a window in tests. The actual first-run install + window is verified manually by Estevão. |

## 3. Command dispatch (`main.go`)

The mode decision becomes a pure, testable function; `main` stays thin and the existing Bubble Tea startup moves into `runTUI`.

```go
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

func runTUI() {
	model := app.New()
	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error running GoDynamo: %v\n", err)
		os.Exit(1)
	}
}
```

Resulting surface:

| Command | Result |
|---|---|
| `godynamo` | GUI (default) |
| `godynamo gui` | GUI (alias; args after `gui` forwarded) |
| `godynamo tui` | terminal UI |
| `godynamo xyz` | GUI (permissive) |

## 4. Auto-install flow (`internal/gui`)

`gui.Run` resolves the Electron app dir once, ensures Electron is installed, then proceeds exactly as today (bridge + launch). Ordering: **dir → `ensureElectron(dir)` → bridge server → `startElectron(dir, …)`**, so we don't bind a port while a multi-minute install runs.

```go
func Run(args []string) error {
	dir, err := electronAppDir()
	if err != nil {
		return err
	}
	if err := ensureElectron(dir); err != nil {
		return err
	}
	// ... unchanged: bind loopback port, token, bridge server ...
	electron, err := startElectron(dir, port, token)
	// ... unchanged: signal handling, wait ...
}
```

New file `internal/gui/install.go`:

```go
// Injectable seams so the install path is unit-testable without a real npm
// or a real splash window.
var (
	lookNpm       = defaultLookNpm
	runNpmInstall = defaultRunNpmInstall
	startSplash   = defaultStartSplash
)

// ensureElectron makes sure the dev Electron binary exists under dir, installing
// the npm dependencies automatically (behind a wait window) on first run.
func ensureElectron(dir string) error {
	if _, err := os.Stat(electronBinPath(dir)); err == nil {
		return nil // already installed — normal path, no window, instant
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

func defaultLookNpm() (string, error) { return exec.LookPath(npmName()) }

func npmName() string {
	if runtime.GOOS == "windows" {
		return "npm.cmd"
	}
	return "npm"
}

func defaultRunNpmInstall(npm, dir string) error {
	cmd := exec.Command(npm, "install")
	cmd.Dir = dir
	cmd.Stdout = os.Stderr // keep stdout clean; surface progress on stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
```

Notes:
- The binary-existence check reuses the existing `electronBinPath`/`electronAppDir` helpers (`electron.go:44-65`) — single source of truth for where Electron lives.
- The install streams npm output to **stderr** (so it's visible in the terminal too) while the native window provides the visual "please wait"; stdout stays clean.
- The error strings are constants we control (no user input), so no shell/PowerShell injection surface.

## 5. Wait window (`internal/gui/splash.go`, new)

`startSplash(message) func()` returns a closer. On Windows it launches a WinForms window via PowerShell; elsewhere (or on any failure to start PowerShell) it degrades to a single stderr line with the same signature, so callers never branch.

```go
func defaultStartSplash(message string) func() {
	if runtime.GOOS != "windows" {
		fmt.Fprintln(os.Stderr, message)
		return func() {}
	}
	enc := encodePowershell(splashScript(message))
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-EncodedCommand", enc)
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, message)
		return func() {}
	}
	return func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill() // closes the window when the install completes
		}
	}
}
```

- `splashScript(message)` returns a small WinForms script: a `Form` (width ~380, height ~150, `StartPosition=CenterScreen`, `FormBorderStyle=FixedDialog`, `ControlBox=$false`, `TopMost=$true`) containing a label with `message`, a second label "(pode levar alguns minutos)", and a `ProgressBar` with `Style=Marquee`; it ends with `[void]$f.ShowDialog()` so the process blocks on the message loop until Go kills it.
- `encodePowershell(script)` encodes the script as **UTF-16LE → base64** for `-EncodedCommand`. This avoids all quoting/code-page problems with the accented text ("instalação") and the ellipsis — the reason we don't pass the script via `-Command`:

```go
func encodePowershell(script string) string {
	u := utf16.Encode([]rune(script))
	buf := make([]byte, 0, len(u)*2)
	for _, r := range u {
		buf = append(buf, byte(r), byte(r>>8))
	}
	return base64.StdEncoding.EncodeToString(buf)
}
```

- Killing the PowerShell process is what dismisses the window; if the user closes it manually mid-install, the install (a separate Go-managed process) keeps running and the GUI still opens when it finishes.
- `splashScript` and `encodePowershell` are pure and unit-tested; `defaultStartSplash` itself (which spawns PowerShell) is not unit-tested.

## 6. Wording / safety-net changes (`internal/gui/electron.go`)

- `startElectron` gains a `dir string` parameter (passed from `Run`) and drops its internal `electronAppDir()` call.
- Its `os.Stat(bin)` check stays as a defensive guard but is now normally unreachable (because `ensureElectron` ran first). Update its message from `…re-run `godynamo gui`` to `…re-run `godynamo``.

## 7. Docs (`README.md`)

- **Quick Start:** `./godynamo` opens the **desktop GUI by default**; add `./godynamo tui` for the terminal UI. Note that on first run without Electron installed, GoDynamo installs it automatically and shows a wait window.
- **Desktop GUI section:** reverse the line "The terminal UI remains the default; the GUI is launched with the `gui` subcommand." to state the GUI is the default and the terminal UI is `godynamo tui`. Keep `go run . gui` documented as a still-working alias; soften the manual `cd electron && npm install` step to "optional / done automatically on first run."

## 8. Testing

`internal/gui/install_test.go` (drives §4, using the seams):

- **already installed** → create `electronBinPath(dir)` on disk; `ensureElectron` returns nil and `runNpmInstall` is **not** called.
- **npm missing** → `lookNpm` overridden to return an error, no binary on disk; `ensureElectron` returns an error containing "Node.js" and "tui"; install not attempted.
- **install succeeds** → `lookNpm` returns a fake path, `startSplash` records open/close, `runNpmInstall` creates the binary file and returns nil; `ensureElectron` returns nil, splash opened **and** closed, install called once.
- **install fails** → `runNpmInstall` returns an error; `ensureElectron` returns a wrapped error and the splash is still closed (deferred).

`internal/gui/splash_test.go`:

- `encodePowershell` round-trips a known string to the expected UTF-16LE/base64 (decode and compare).
- `splashScript` contains the message and the marquee/topmost markers.

`main_test.go` (drives §3): table test of `selectMode` for `[]`, `["gui"]`, `["gui","--port","9"]`, `["tui"]`, `["tui","x"]`, `["xyz"]`.

Each seam is saved and restored (`t.Cleanup`) so overrides don't leak between tests.

## 9. Files touched

| File | Change |
|---|---|
| `main.go` | `selectMode` + `mode` consts; `main` dispatches; extract `runTUI`. |
| `main_test.go` | **new** — `selectMode` table test. |
| `internal/gui/gui.go` | `Run` resolves `dir`, calls `ensureElectron`, passes `dir` to `startElectron`. |
| `internal/gui/electron.go` | `startElectron(dir, port, token)`; stale-message wording fix. |
| `internal/gui/install.go` | **new** — `ensureElectron`, npm lookup/run, seams. |
| `internal/gui/splash.go` | **new** — `startSplash`, `splashScript`, `encodePowershell`. |
| `internal/gui/install_test.go` | **new** — install-path tests via seams. |
| `internal/gui/splash_test.go` | **new** — encode/script tests. |
| `README.md` | default = GUI; `tui` subcommand; auto-install note. |

## 10. Verification

- `go build ./...`, `go vet ./...`, `go test ./...` all green.
- Manual (Estevão): `godynamo tui` opens the terminal UI; `godynamo` opens the GUI; with `electron/node_modules` removed, `godynamo` shows the wait window, runs the install, and then opens the GUI; with `npm` off PATH, `godynamo` prints the Node.js message and exits non-zero.
