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
