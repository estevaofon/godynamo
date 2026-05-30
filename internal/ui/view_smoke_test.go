package ui

import (
	"strings"
	"testing"
)

func TestDataTableViewRenders(t *testing.T) {
	dt := NewDataTable()
	dt.SetSize(80, 20)
	dt.SetData([]string{"id", "name"}, [][]string{{"1", "alice"}, {"2", "bob"}})
	out := dt.View()
	if out == "" {
		t.Fatal("DataTable.View returned empty")
	}
	if !strings.Contains(out, "id") || !strings.Contains(out, "alice") {
		t.Errorf("view missing content:\n%s", out)
	}
}

func TestDataTableViewEmpty(t *testing.T) {
	dt := NewDataTable()
	dt.SetSize(80, 20)
	_ = dt.View() // must not panic on empty
}

func TestDataTableViewHorizontalScroll(t *testing.T) {
	dt := NewDataTable()
	dt.SetSize(40, 20)
	dt.SetData([]string{"a", "b", "c", "d", "e", "f"},
		[][]string{{"1", "2", "3", "4", "5", "6"}})
	dt.MoveRight()
	dt.MoveRight()
	dt.MoveRight()
	dt.MoveRight() // force HorizontalOff > 0
	_ = dt.View()  // must not panic with scroll offset
}

func TestListViewRenders(t *testing.T) {
	l := NewList("Tables", []string{"users", "orders"})
	out := l.View()
	if !strings.Contains(out, "users") {
		t.Errorf("list view missing item:\n%s", out)
	}
}

func TestListViewEmpty(t *testing.T) {
	l := NewList("Empty", []string{})
	_ = l.View() // must not panic
}

func TestFilterBuilderViewRenders(t *testing.T) {
	fb := NewFilterBuilder()
	fb.SetWidth(80)
	out := fb.View()
	if out == "" {
		t.Fatal("FilterBuilder.View returned empty")
	}
}

func TestJSONViewerRenderCollapsedAndSearch(t *testing.T) {
	data := map[string]interface{}{
		"user": map[string]interface{}{"name": "alice", "age": int64(30)},
		"tags": []interface{}{"a", "b"},
	}
	jv := NewJSONViewer(data)
	jv.CollapseAll()
	if out := jv.Render(); out == "" {
		t.Fatal("collapsed render empty")
	}
	jv.ExpandAll()
	jv.SearchQuery = "alice"
	if out := jv.Render(); out == "" {
		t.Fatal("search render empty")
	}
}
