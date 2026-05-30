package ui

import "testing"

func TestDataTableSetDataResetsCursor(t *testing.T) {
	dt := NewDataTable()
	dt.SelectedRow, dt.SelectedCol, dt.Offset = 5, 3, 2
	dt.SetData([]string{"a", "b"}, [][]string{{"1", "2"}, {"3", "4"}})
	if dt.SelectedRow != 0 || dt.SelectedCol != 0 || dt.Offset != 0 {
		t.Fatalf("cursor not reset: row=%d col=%d off=%d", dt.SelectedRow, dt.SelectedCol, dt.Offset)
	}
}

func TestDataTableColWidthsFitContent(t *testing.T) {
	dt := NewDataTable()
	dt.SetData([]string{"id", "name"}, [][]string{{"1", "alice"}})
	if len(dt.ColWidths) != 2 {
		t.Fatalf("want 2 col widths, got %v", dt.ColWidths)
	}
	if dt.ColWidths[0] != 2 {
		t.Errorf("col0 width=%d want 2", dt.ColWidths[0])
	}
	if dt.ColWidths[1] != 5 {
		t.Errorf("col1 width=%d want 5", dt.ColWidths[1])
	}
}

func TestDataTableColWidthsCappedAt40(t *testing.T) {
	long := ""
	for i := 0; i < 60; i++ {
		long += "x"
	}
	dt := NewDataTable()
	dt.SetData([]string{"c"}, [][]string{{long}})
	if dt.ColWidths[0] != 40 {
		t.Fatalf("width not capped: %d", dt.ColWidths[0])
	}
}

func TestDataTableVerticalNavBounds(t *testing.T) {
	dt := NewDataTable()
	dt.Height = 20
	dt.SetData([]string{"c"}, [][]string{{"a"}, {"b"}, {"c"}})
	dt.MoveUp()
	if dt.SelectedRow != 0 {
		t.Fatalf("MoveUp past top: %d", dt.SelectedRow)
	}
	dt.MoveDown()
	dt.MoveDown()
	dt.MoveDown()
	if dt.SelectedRow != 2 {
		t.Fatalf("MoveDown past bottom: %d", dt.SelectedRow)
	}
}

func TestDataTableHorizontalNavBounds(t *testing.T) {
	dt := NewDataTable()
	dt.SetData([]string{"a", "b", "c"}, [][]string{{"1", "2", "3"}})
	dt.MoveLeft()
	if dt.SelectedCol != 0 {
		t.Fatalf("MoveLeft past left: %d", dt.SelectedCol)
	}
	dt.MoveRight()
	dt.MoveRight()
	dt.MoveRight()
	if dt.SelectedCol != 2 {
		t.Fatalf("MoveRight past right: %d", dt.SelectedCol)
	}
}

func TestDataTableGetSelectedRow(t *testing.T) {
	dt := NewDataTable()
	dt.SetData([]string{"a"}, [][]string{{"x"}, {"y"}})
	dt.MoveDown()
	got := dt.GetSelectedRow()
	if len(got) != 1 || got[0] != "y" {
		t.Fatalf("got %v want [y]", got)
	}
}

func TestDataTableGetSelectedRowEmpty(t *testing.T) {
	dt := NewDataTable()
	if dt.GetSelectedRow() != nil {
		t.Fatal("empty table should return nil row")
	}
}

func TestListNavigationAndSelection(t *testing.T) {
	l := NewList("Tables", []string{"a", "b", "c"})
	if l.GetSelected() != "a" {
		t.Fatalf("initial selected=%q", l.GetSelected())
	}
	l.MoveUp()
	if l.Selected != 0 {
		t.Fatalf("MoveUp past top: %d", l.Selected)
	}
	l.MoveDown()
	l.MoveDown()
	l.MoveDown()
	if l.Selected != 2 || l.GetSelected() != "c" {
		t.Fatalf("MoveDown end: idx=%d sel=%q", l.Selected, l.GetSelected())
	}
}

func TestListSetItemsResets(t *testing.T) {
	l := NewList("X", []string{"a", "b"})
	l.MoveDown()
	l.SetItems([]string{"p", "q", "r"})
	if l.Selected != 0 || l.Offset != 0 {
		t.Fatalf("SetItems should reset: sel=%d off=%d", l.Selected, l.Offset)
	}
	if l.GetSelected() != "p" {
		t.Fatalf("selected=%q", l.GetSelected())
	}
}

func TestListGetSelectedEmpty(t *testing.T) {
	l := NewList("X", []string{})
	if l.GetSelected() != "" {
		t.Fatal("empty list should return empty string")
	}
}
