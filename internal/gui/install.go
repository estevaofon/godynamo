package gui

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

// Seams: swapped in tests so the install path runs without a real npm or window.
// startSplash defaults to defaultStartSplash (defined in splash.go).
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
