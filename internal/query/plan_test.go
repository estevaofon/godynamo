package query

import (
	"testing"

	"github.com/godynamo/internal/dynamo"
)

func planFor(t *testing.T, info *dynamo.TableInfo, conds []Condition) Plan {
	t.Helper()
	expr, names, values := BuildExpression(conds)
	return BuildPlan(info, expr, names, values)
}

func TestPlanPartitionKeyEqualsUsesQuery(t *testing.T) {
	p := planFor(t, &dynamo.TableInfo{PartitionKey: "id"},
		[]Condition{{Name: "id", Operator: OpEquals, Value: "1"}})
	if p.Mode != ModeQuery {
		t.Fatalf("want ModeQuery, got %v", p.Mode)
	}
	if p.IndexName != "" {
		t.Fatalf("want table query (no index), got %q", p.IndexName)
	}
	if p.KeyConditionExpression != "#pk = :val0" {
		t.Fatalf("keyCond=%q", p.KeyConditionExpression)
	}
	if p.Names["#pk"] != "id" {
		t.Fatalf("names=%v", p.Names)
	}
	if p.Values[":val0"] != float64(1) {
		t.Fatalf("values=%v", p.Values)
	}
	if p.FilterExpression != "" {
		t.Fatalf("filter=%q", p.FilterExpression)
	}
}

func TestPlanGSIPartitionKeyEqualsUsesIndexQuery(t *testing.T) {
	info := &dynamo.TableInfo{
		PartitionKey: "id",
		GSIs:         []dynamo.IndexInfo{{Name: "by-email", PartitionKey: "email"}},
	}
	p := planFor(t, info, []Condition{{Name: "email", Operator: OpEquals, Value: "a@b.com"}})
	if p.Mode != ModeQuery {
		t.Fatalf("want ModeQuery, got %v", p.Mode)
	}
	if p.IndexName != "by-email" {
		t.Fatalf("index=%q", p.IndexName)
	}
	if p.Names["#pk"] != "email" {
		t.Fatalf("names=%v", p.Names)
	}
}

func TestPlanQueryWithAdditionalFilter(t *testing.T) {
	p := planFor(t, &dynamo.TableInfo{PartitionKey: "id"}, []Condition{
		{Name: "id", Operator: OpEquals, Value: "1"},
		{Name: "status", Operator: OpEquals, Value: "active"},
	})
	if p.Mode != ModeQuery {
		t.Fatalf("want ModeQuery, got %v", p.Mode)
	}
	if p.KeyConditionExpression != "#pk = :val0" {
		t.Fatalf("keyCond=%q", p.KeyConditionExpression)
	}
	if p.FilterExpression != "#attr1 = :val1" {
		t.Fatalf("filter=%q", p.FilterExpression)
	}
	if p.Names["#pk"] != "id" || p.Names["#attr1"] != "status" {
		t.Fatalf("names=%v", p.Names)
	}
	if p.Values[":val0"] != float64(1) || p.Values[":val1"] != "active" {
		t.Fatalf("values=%v", p.Values)
	}
}

func TestPlanNonKeyAttributeUsesScan(t *testing.T) {
	p := planFor(t, &dynamo.TableInfo{PartitionKey: "id"},
		[]Condition{{Name: "status", Operator: OpEquals, Value: "active"}})
	if p.Mode != ModeScan {
		t.Fatalf("want ModeScan, got %v", p.Mode)
	}
	if p.FilterExpression != "#attr0 = :val0" {
		t.Fatalf("filter=%q", p.FilterExpression)
	}
	if p.Names["#attr0"] != "status" {
		t.Fatalf("names=%v", p.Names)
	}
}

func TestPlanNonEqualsFirstUsesScan(t *testing.T) {
	p := planFor(t, &dynamo.TableInfo{PartitionKey: "id"},
		[]Condition{{Name: "id", Operator: OpGreaterThan, Value: "1"}})
	if p.Mode != ModeScan {
		t.Fatalf("want ModeScan, got %v", p.Mode)
	}
}

func TestPlanNoFilterIsScan(t *testing.T) {
	p := BuildPlan(&dynamo.TableInfo{PartitionKey: "id"}, "", nil, nil)
	if p.Mode != ModeScan {
		t.Fatalf("want ModeScan, got %v", p.Mode)
	}
	if p.FilterExpression != "" {
		t.Fatalf("filter=%q", p.FilterExpression)
	}
}

func TestPlanNilInfoIsScan(t *testing.T) {
	expr, names, values := BuildExpression([]Condition{{Name: "id", Operator: OpEquals, Value: "1"}})
	p := BuildPlan(nil, expr, names, values)
	if p.Mode != ModeScan {
		t.Fatalf("want ModeScan, got %v", p.Mode)
	}
}

func TestPlanExistsFirstIsScan(t *testing.T) {
	p := planFor(t, &dynamo.TableInfo{PartitionKey: "id"},
		[]Condition{{Name: "id", Operator: OpExists, Value: ""}})
	if p.Mode != ModeScan {
		t.Fatalf("want ModeScan, got %v", p.Mode)
	}
}
