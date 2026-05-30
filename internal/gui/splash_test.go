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

func TestSplashScriptEscapesSingleQuotes(t *testing.T) {
	// A single quote in the message must be doubled so it can't break out of the
	// PowerShell single-quoted string literal it is embedded in.
	s := splashScript("it's")
	if !strings.Contains(s, "it''s") {
		t.Errorf("splashScript should escape ' as '' (it's -> it''s); got:\n%s", s)
	}
}
