package ui

import "testing"

func TestFilterBuilderMultipleConditions(t *testing.T) {
	fb := NewFilterBuilder()
	fb.Conditions[0].AttributeName.SetValue("status")
	fb.Conditions[0].Operator = OpEquals
	fb.Conditions[0].AttributeValue.SetValue("active")
	fb.AddCondition()
	fb.Conditions[1].AttributeName.SetValue("age")
	fb.Conditions[1].Operator = OpGreaterThan
	fb.Conditions[1].AttributeValue.SetValue("18")

	expr, names, values := fb.BuildExpression()
	if expr != "#attr0 = :val0 AND #attr1 > :val1" {
		t.Fatalf("expr=%q", expr)
	}
	if names["#attr0"] != "status" || names["#attr1"] != "age" {
		t.Fatalf("names=%v", names)
	}
	if values[":val1"] != float64(18) {
		t.Fatalf("values=%v", values)
	}
}

func TestFilterBuilderExistsHasNoValuePlaceholder(t *testing.T) {
	fb := NewFilterBuilder()
	fb.Conditions[0].AttributeName.SetValue("email")
	fb.Conditions[0].Operator = OpExists

	expr, _, values := fb.BuildExpression()
	if expr != "attribute_exists(#attr0)" {
		t.Fatalf("expr=%q", expr)
	}
	if len(values) != 0 {
		t.Fatalf("Exists must not bind a value, got %v", values)
	}
}

func TestFilterBuilderEmptyNameSkipped(t *testing.T) {
	fb := NewFilterBuilder()
	fb.Conditions[0].Operator = OpEquals
	fb.Conditions[0].AttributeValue.SetValue("x")

	expr, _, _ := fb.BuildExpression()
	if expr != "" {
		t.Fatalf("empty name should yield empty expr, got %q", expr)
	}
}
