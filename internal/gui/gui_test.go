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
