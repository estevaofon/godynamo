package ui

import (
	"strings"
	"testing"
)

func TestFormatJSONCompact(t *testing.T) {
	got := FormatJSONCompact(map[string]interface{}{"a": 1})
	if got != `{"a":1}` {
		t.Fatalf("got %q", got)
	}
}

func TestFormatJSONPretty(t *testing.T) {
	got := FormatJSONPretty(map[string]interface{}{"a": 1})
	if !strings.Contains(got, "\n") || !strings.Contains(got, `"a": 1`) {
		t.Fatalf("not pretty: %q", got)
	}
}

func TestJSONViewerRenderContainsKeysAndValues(t *testing.T) {
	jv := NewJSONViewer(map[string]interface{}{
		"name": "alice",
		"age":  int64(30),
	})
	out := jv.Render()
	if !strings.Contains(out, "name") || !strings.Contains(out, "alice") {
		t.Fatalf("render missing key/value:\n%s", out)
	}
	if !strings.Contains(out, "age") || !strings.Contains(out, "30") {
		t.Fatalf("render missing numeric field:\n%s", out)
	}
}

func TestJSONViewerToggle(t *testing.T) {
	jv := NewJSONViewer(map[string]interface{}{"a": 1})
	jv.Toggle("root.a")
	if !jv.Collapsed["root.a"] {
		t.Fatal("Toggle should set collapsed=true")
	}
	jv.Toggle("root.a")
	if jv.Collapsed["root.a"] {
		t.Fatal("Toggle should flip back to false")
	}
}

func TestJSONViewerExpandAllClearsState(t *testing.T) {
	jv := NewJSONViewer(map[string]interface{}{"a": 1})
	jv.Collapsed["root"] = true
	jv.Collapsed["root.a"] = true
	jv.ExpandAll()
	if len(jv.Collapsed) != 0 {
		t.Fatalf("ExpandAll should clear collapsed map, got %v", jv.Collapsed)
	}
}

func TestJSONViewerCollapseAllMarksContainers(t *testing.T) {
	jv := NewJSONViewer(map[string]interface{}{
		"list": []interface{}{1, 2},
		"obj":  map[string]interface{}{"k": "v"},
	})
	jv.CollapseAll()
	if !jv.Collapsed["root"] {
		t.Fatal("CollapseAll should collapse root")
	}
	if !jv.Collapsed["root.list"] {
		t.Fatal("CollapseAll should collapse nested list")
	}
	if !jv.Collapsed["root.obj"] {
		t.Fatal("CollapseAll should collapse nested object")
	}
}

func TestJSONViewerRenderDoesNotPanicOnNil(t *testing.T) {
	jv := NewJSONViewer(nil)
	_ = jv.Render()
}
