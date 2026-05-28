package ui

import "testing"

func TestFilterBuilderBuildExpressionDelegates(t *testing.T) {
	fb := NewFilterBuilder()
	fb.Conditions[0].AttributeName.SetValue("status")
	fb.Conditions[0].Operator = OpEquals
	fb.Conditions[0].AttributeValue.SetValue("active")

	expr, names, values := fb.BuildExpression()
	if expr != "#attr0 = :val0" {
		t.Fatalf("expr=%q", expr)
	}
	if names["#attr0"] != "status" {
		t.Fatalf("names=%v", names)
	}
	if values[":val0"] != "active" {
		t.Fatalf("values=%v", values)
	}
}
