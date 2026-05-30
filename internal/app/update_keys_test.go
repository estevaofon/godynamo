package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// keyRunes builds a rune key message (e.g. "f", "+") for driving Update.
// Reuses drive() (update_test.go) and populatedModel() (view_smoke_test.go).
func keyRunes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func TestUpdateTableDataVerticalNavigation(t *testing.T) {
	m := populatedModel()
	m.view = viewTableData
	m = drive(m, tea.KeyMsg{Type: tea.KeyDown})
	if m.dataTable.SelectedRow != 1 {
		t.Fatalf("down: row=%d want 1", m.dataTable.SelectedRow)
	}
	m = drive(m, tea.KeyMsg{Type: tea.KeyUp})
	if m.dataTable.SelectedRow != 0 {
		t.Fatalf("up: row=%d want 0", m.dataTable.SelectedRow)
	}
}

func TestUpdateTableDataEnterOpensItemDetail(t *testing.T) {
	m := populatedModel()
	m.view = viewTableData
	m = drive(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.view != viewItemDetail {
		t.Fatalf("enter should open item detail, view=%d", m.view)
	}
	if m.selectedItem == nil {
		t.Fatal("enter should set selectedItem")
	}
}

func TestUpdateTableDataViewTransitions(t *testing.T) {
	// Only pure, non-AWS, non-clipboard transitions: 'f' → query, 'x' → export.
	cases := []struct {
		key  string
		want viewMode
	}{
		{"f", viewQuery},
		{"x", viewExport},
	}
	for _, c := range cases {
		m := populatedModel()
		m.view = viewTableData
		m = drive(m, keyRunes(c.key))
		if m.view != c.want {
			t.Errorf("key %q: view=%d want %d", c.key, m.view, c.want)
		}
	}
}

func TestUpdateTableDataQuitReturnsToTables(t *testing.T) {
	m := populatedModel()
	m.view = viewTableData
	m = drive(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.view != viewTables {
		t.Fatalf("esc should return to tables, view=%d", m.view)
	}
	if m.items != nil {
		t.Fatal("items should be cleared on leaving the table")
	}
}

func TestUpdateTableDataPageSizeAdjust(t *testing.T) {
	m := populatedModel()
	m.view = viewTableData
	orig := m.pageSize
	m = drive(m, keyRunes("+"))
	if m.pageSize != orig+100 {
		t.Fatalf("'+' should increase page size: got %d want %d", m.pageSize, orig+100)
	}
	m = drive(m, keyRunes("-"))
	if m.pageSize != orig {
		t.Fatalf("'-' should decrease page size back to %d, got %d", orig, m.pageSize)
	}
}
