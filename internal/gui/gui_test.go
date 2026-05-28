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
