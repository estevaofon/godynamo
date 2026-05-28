package query

import "testing"

func TestParseValue(t *testing.T) {
	if ParseValue("42") != float64(42) {
		t.Errorf("42 → %v", ParseValue("42"))
	}
	if ParseValue("true") != true {
		t.Errorf("true → %v", ParseValue("true"))
	}
	if ParseValue("false") != false {
		t.Errorf("false → %v", ParseValue("false"))
	}
	if ParseValue("null") != nil {
		t.Errorf("null → %v", ParseValue("null"))
	}
	if ParseValue("hello") != "hello" {
		t.Errorf("hello → %v", ParseValue("hello"))
	}
}

func TestBuildExpressionEquals(t *testing.T) {
	expr, names, values := BuildExpression([]Condition{{Name: "id", Operator: OpEquals, Value: "1"}})
	if expr != "#attr0 = :val0" {
		t.Fatalf("expr=%q", expr)
	}
	if names["#attr0"] != "id" {
		t.Fatalf("names=%v", names)
	}
	if values[":val0"] != float64(1) {
		t.Fatalf("values=%v", values)
	}
}

func TestBuildExpressionAllOperators(t *testing.T) {
	cases := []struct {
		op   Operator
		want string
	}{
		{OpEquals, "#attr0 = :val0"},
		{OpNotEquals, "#attr0 <> :val0"},
		{OpGreaterThan, "#attr0 > :val0"},
		{OpLessThan, "#attr0 < :val0"},
		{OpGreaterOrEqual, "#attr0 >= :val0"},
		{OpLessOrEqual, "#attr0 <= :val0"},
		{OpContains, "contains(#attr0, :val0)"},
		{OpNotContains, "NOT contains(#attr0, :val0)"},
		{OpBeginsWith, "begins_with(#attr0, :val0)"},
		{OpExists, "attribute_exists(#attr0)"},
		{OpNotExists, "attribute_not_exists(#attr0)"},
	}
	for _, c := range cases {
		expr, _, _ := BuildExpression([]Condition{{Name: "a", Operator: c.op, Value: "v"}})
		if expr != c.want {
			t.Errorf("op %d: expr=%q want %q", c.op, expr, c.want)
		}
	}
}

func TestBuildExpressionMultiAnd(t *testing.T) {
	expr, _, _ := BuildExpression([]Condition{
		{Name: "id", Operator: OpEquals, Value: "1"},
		{Name: "age", Operator: OpGreaterThan, Value: "18"},
	})
	if expr != "#attr0 = :val0 AND #attr1 > :val1" {
		t.Fatalf("expr=%q", expr)
	}
}

func TestBuildExpressionEmptyNameSkipped(t *testing.T) {
	expr, names, values := BuildExpression([]Condition{{Name: "", Operator: OpEquals, Value: "x"}})
	if expr != "" || names != nil || values != nil {
		t.Fatalf("expected empty, got %q %v %v", expr, names, values)
	}
}

func TestBuildExpressionExistsNoValuePlaceholder(t *testing.T) {
	expr, _, values := BuildExpression([]Condition{{Name: "a", Operator: OpExists, Value: ""}})
	if expr != "attribute_exists(#attr0)" {
		t.Fatalf("expr=%q", expr)
	}
	if len(values) != 0 {
		t.Fatalf("expected no values, got %v", values)
	}
}

func TestBuildExpressionNamedEmptyValueNoGhost(t *testing.T) {
	expr, names, values := BuildExpression([]Condition{{Name: "x", Operator: OpEquals, Value: ""}})
	if expr != "" || names != nil || values != nil {
		t.Fatalf("named+empty-value should yield empty result, got %q %v %v", expr, names, values)
	}
}

func TestBuildExpressionGhostBeforeValidIsClean(t *testing.T) {
	expr, names, values := BuildExpression([]Condition{
		{Name: "a", Operator: OpEquals, Value: ""},
		{Name: "b", Operator: OpEquals, Value: "2"},
	})
	if expr != "#attr0 = :val0" {
		t.Fatalf("expr=%q", expr)
	}
	if len(names) != 1 || names["#attr0"] != "b" {
		t.Fatalf("names should only contain the valid condition, got %v", names)
	}
	if values[":val0"] != float64(2) {
		t.Fatalf("values=%v", values)
	}
}
