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
