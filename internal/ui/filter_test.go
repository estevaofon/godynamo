package ui

import (
	"testing"

	"github.com/godynamo/internal/query"
)

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

func TestFilterBuilderBuildExpressionParsesNumber(t *testing.T) {
	fb := NewFilterBuilder()
	fb.Conditions[0].AttributeName.SetValue("age")
	fb.Conditions[0].Operator = OpGreaterThan
	fb.Conditions[0].AttributeValue.SetValue("42")

	expr, _, values := fb.BuildExpression()
	if expr != "#attr0 > :val0" {
		t.Fatalf("expr=%q", expr)
	}
	// Numeric coercion must flow through the delegated query.ParseValue.
	if values[":val0"] != float64(42) {
		t.Fatalf("values[:val0]=%v (want float64 42)", values[":val0"])
	}
}

// TestOperatorIotaSync guards the query.Operator(c.Operator) cast in
// BuildExpression: if either enum is ever reordered, this fails loudly.
func TestOperatorIotaSync(t *testing.T) {
	if int(OpEquals) != int(query.OpEquals) {
		t.Fatalf("OpEquals out of sync: ui=%d query=%d", OpEquals, query.OpEquals)
	}
	if int(OpNotExists) != int(query.OpNotExists) {
		t.Fatalf("OpNotExists out of sync: ui=%d query=%d", OpNotExists, query.OpNotExists)
	}
}
