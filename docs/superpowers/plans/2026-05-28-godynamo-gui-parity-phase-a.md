# GoDynamo GUI Parity — Phase A (Querying & Filtering) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bring the visual filter builder + smart Query-vs-Scan to the Electron GUI by extracting the pure logic from `internal/app/app.go` and `internal/ui/filter.go` into a new shared `internal/query` package that both the TUI and the bridge use, then adding a `POST /query` endpoint and a filter-panel UI with console-style paging.

**Architecture:** New UI-agnostic `internal/query` package holds `BuildExpression` (filter expression) and `BuildPlan` (Query-vs-Scan decision). The TUI is refactored to delegate to it (behavior-preserving, guarded by characterization tests). The bridge gains `POST /tables/{name}/query` that runs `BuildExpression`→`BuildPlan`→one `QueryTable` or one filtered `ScanTable` page. The renderer gets a filter panel, a page-size control, and a "Resume fetching" button (DynamoDB-console paging).

**Tech Stack:** Go 1.24 stdlib; existing `internal/dynamo` (`QueryTable`/`ScanTable`/`QueryInput`/`QueryResult`/`TableInfo`); Electron (vanilla renderer).

**Source spec:** `docs/superpowers/specs/2026-05-28-godynamo-gui-parity-design.md`

---

## Conventions

- **No new Go dependencies** — all stdlib + existing modules. `go.mod`/`go.sum` untouched.
- **No real AWS in automated steps.** Go tests use pure logic + the fake `Backend`. Estevao runs all live tests.
- **Never run `go run . gui` / `npm start`** — they open a blocking GUI window. Verify with `go test`, `go vet`, `go build ./...`, `node --check`.
- **Behavior-preserving refactor:** the TUI (`internal/app`, `internal/ui`) must keep its exact current behavior; Estevao confirms manually after the refactor. Do NOT run the no-arg TUI in checks (it hits real AWS).
- **Commit trailer:** every commit ends with a blank line then `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`. Avoid backticks in commit messages (PowerShell).

## File structure

```
internal/query/              # NEW — shared, UI-agnostic
  condition.go               #   Operator, Condition, ParseValue, BuildExpression  (from internal/ui/filter.go)
  condition_test.go
  plan.go                    #   Mode, Plan, BuildPlan  (from internal/app/app.go scanTable)
  plan_test.go
internal/ui/filter.go        # MODIFY — BuildExpression delegates to query; remove parseValue + strconv import
internal/ui/filter_test.go   # NEW — delegation characterization test
internal/app/app.go          # MODIFY — scanTable uses query.BuildPlan
internal/gui/backend.go      # MODIFY — add QueryTable to Backend interface
internal/gui/server.go       # MODIFY — add POST /query route + handler + request types + op-token map
internal/gui/server_test.go  # MODIFY — fake gains QueryTable; add /query handler tests
electron/preload.js          # MODIFY — scan() takes limit; add query()
electron/renderer/index.html # MODIFY — toolbar (filter btn, page-size, Resume fetching, mode badge) + filter panel
electron/renderer/app.js     # REWRITE — filtered query + paging + filter panel
electron/renderer/styles.css # MODIFY — append filter-panel / badge styles
```

---

## Task 1: `internal/query` — Condition + BuildExpression

Verbatim extraction of the TUI's filter-expression logic, decoupled from `textinput`.

**Files:**
- Create: `internal/query/condition.go`
- Test: `internal/query/condition_test.go`

- [ ] **Step 1: Write the failing tests** — create `internal/query/condition_test.go`:

```go
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
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/query/ -v`
Expected: build failure — `undefined: ParseValue`, `undefined: BuildExpression`, `undefined: Condition`, `undefined: OpEquals`, etc.

- [ ] **Step 3: Write the implementation** — create `internal/query/condition.go`:

```go
package query

import (
	"fmt"
	"strconv"
	"strings"
)

// Operator is a filter comparison operator. The order matches the TUI's
// ui.FilterOperator exactly, so an int conversion between the two is valid.
type Operator int

const (
	OpEquals Operator = iota
	OpNotEquals
	OpGreaterThan
	OpLessThan
	OpGreaterOrEqual
	OpLessOrEqual
	OpContains
	OpNotContains
	OpBeginsWith
	OpExists
	OpNotExists
)

// Condition is one filter row: an attribute name, an operator, and a raw value.
type Condition struct {
	Name     string
	Operator Operator
	Value    string
}

// ParseValue coerces a raw string to number, bool, null, or string.
// Verbatim port of the TUI's parseValue.
func ParseValue(value string) interface{} {
	if f, err := strconv.ParseFloat(value, 64); err == nil {
		return f
	}
	if strings.ToLower(value) == "true" {
		return true
	}
	if strings.ToLower(value) == "false" {
		return false
	}
	if strings.ToLower(value) == "null" {
		return nil
	}
	return value
}

// BuildExpression builds a DynamoDB filter expression from conditions.
// Verbatim port of the TUI's FilterBuilder.BuildExpression, operating on
// []Condition instead of textinput widgets (same placeholders, same skips).
func BuildExpression(conds []Condition) (string, map[string]string, map[string]interface{}) {
	var expressions []string
	attrNames := make(map[string]string)
	attrValues := make(map[string]interface{})
	valueCounter := 0

	for _, cond := range conds {
		name := strings.TrimSpace(cond.Name)
		value := strings.TrimSpace(cond.Value)

		if name == "" {
			continue
		}

		namePlaceholder := fmt.Sprintf("#attr%d", len(attrNames))
		attrNames[namePlaceholder] = name

		var expr string

		switch cond.Operator {
		case OpEquals:
			if value == "" {
				continue
			}
			valuePlaceholder := fmt.Sprintf(":val%d", valueCounter)
			attrValues[valuePlaceholder] = ParseValue(value)
			expr = fmt.Sprintf("%s = %s", namePlaceholder, valuePlaceholder)
			valueCounter++
		case OpNotEquals:
			if value == "" {
				continue
			}
			valuePlaceholder := fmt.Sprintf(":val%d", valueCounter)
			attrValues[valuePlaceholder] = ParseValue(value)
			expr = fmt.Sprintf("%s <> %s", namePlaceholder, valuePlaceholder)
			valueCounter++
		case OpGreaterThan:
			if value == "" {
				continue
			}
			valuePlaceholder := fmt.Sprintf(":val%d", valueCounter)
			attrValues[valuePlaceholder] = ParseValue(value)
			expr = fmt.Sprintf("%s > %s", namePlaceholder, valuePlaceholder)
			valueCounter++
		case OpLessThan:
			if value == "" {
				continue
			}
			valuePlaceholder := fmt.Sprintf(":val%d", valueCounter)
			attrValues[valuePlaceholder] = ParseValue(value)
			expr = fmt.Sprintf("%s < %s", namePlaceholder, valuePlaceholder)
			valueCounter++
		case OpGreaterOrEqual:
			if value == "" {
				continue
			}
			valuePlaceholder := fmt.Sprintf(":val%d", valueCounter)
			attrValues[valuePlaceholder] = ParseValue(value)
			expr = fmt.Sprintf("%s >= %s", namePlaceholder, valuePlaceholder)
			valueCounter++
		case OpLessOrEqual:
			if value == "" {
				continue
			}
			valuePlaceholder := fmt.Sprintf(":val%d", valueCounter)
			attrValues[valuePlaceholder] = ParseValue(value)
			expr = fmt.Sprintf("%s <= %s", namePlaceholder, valuePlaceholder)
			valueCounter++
		case OpContains:
			if value == "" {
				continue
			}
			valuePlaceholder := fmt.Sprintf(":val%d", valueCounter)
			attrValues[valuePlaceholder] = value
			expr = fmt.Sprintf("contains(%s, %s)", namePlaceholder, valuePlaceholder)
			valueCounter++
		case OpNotContains:
			if value == "" {
				continue
			}
			valuePlaceholder := fmt.Sprintf(":val%d", valueCounter)
			attrValues[valuePlaceholder] = value
			expr = fmt.Sprintf("NOT contains(%s, %s)", namePlaceholder, valuePlaceholder)
			valueCounter++
		case OpBeginsWith:
			if value == "" {
				continue
			}
			valuePlaceholder := fmt.Sprintf(":val%d", valueCounter)
			attrValues[valuePlaceholder] = value
			expr = fmt.Sprintf("begins_with(%s, %s)", namePlaceholder, valuePlaceholder)
			valueCounter++
		case OpExists:
			expr = fmt.Sprintf("attribute_exists(%s)", namePlaceholder)
		case OpNotExists:
			expr = fmt.Sprintf("attribute_not_exists(%s)", namePlaceholder)
		}

		if expr != "" {
			expressions = append(expressions, expr)
		}
	}

	if len(expressions) == 0 {
		return "", nil, nil
	}

	return strings.Join(expressions, " AND "), attrNames, attrValues
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/query/ -v` → all PASS. `go vet ./internal/query/` → no output.

- [ ] **Step 5: Commit**

```
git add internal/query/condition.go internal/query/condition_test.go
git commit -m "feat(query): add shared filter-expression builder (extracted from TUI)"
```
(+ trailer)

---

## Task 2: `internal/query` — Query-vs-Scan planner

Extraction of `app.go scanTable`'s decision. Operates on the built `(expr, names, values)` for an exact, behavior-preserving port.

**Files:**
- Create: `internal/query/plan.go`
- Test: `internal/query/plan_test.go`

- [ ] **Step 1: Write the failing tests** — create `internal/query/plan_test.go`:

```go
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
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/query/ -run TestPlan -v`
Expected: build failure — `undefined: Plan`, `undefined: BuildPlan`, `undefined: ModeQuery`, `undefined: ModeScan`.

- [ ] **Step 3: Write the implementation** — create `internal/query/plan.go`:

```go
package query

import (
	"fmt"
	"strings"

	"github.com/godynamo/internal/dynamo"
)

// Mode is the chosen read strategy.
type Mode int

const (
	ModeScan Mode = iota
	ModeQuery
)

// Plan is the resolved read strategy plus the expression pieces needed to
// build a dynamo.QueryInput (Query mode) or a ScanTable call (Scan mode).
type Plan struct {
	Mode                   Mode
	IndexName              string                 // Query mode; "" = table (not a GSI)
	KeyConditionExpression string                 // Query mode
	FilterExpression       string                 // Query: remaining conditions; Scan: full filter
	Names                  map[string]string      // ExpressionAttributeNames
	Values                 map[string]interface{} // ExpressionAttributeValues
}

// BuildPlan decides Query vs Scan from an already-built filter expression.
// It is a behavior-preserving extraction of the TUI's scanTable logic: a Query
// is used only when the first condition is an equality on the table partition
// key or a GSI partition key; otherwise a Scan. The first condition becomes the
// key condition and the remaining conditions the (additional) filter.
func BuildPlan(info *dynamo.TableInfo, expr string, names map[string]string, values map[string]interface{}) Plan {
	if expr == "" {
		return Plan{Mode: ModeScan}
	}

	scanPlan := Plan{Mode: ModeScan, FilterExpression: expr, Names: names, Values: values}
	if info == nil {
		return scanPlan
	}

	attrName, ok := names["#attr0"]
	if !ok {
		return scanPlan
	}

	firstConditionIsEquals := strings.Contains(expr, "#attr0 = :") ||
		(strings.Contains(expr, "#attr0 =") && !strings.Contains(expr, "#attr0 <>"))
	if !firstConditionIsEquals {
		return scanPlan
	}

	var firstPlaceholder string
	for p := range values {
		if strings.HasPrefix(p, ":val0") {
			firstPlaceholder = p
			break
		}
	}
	if firstPlaceholder == "" {
		for p := range values {
			firstPlaceholder = p
			break
		}
	}
	value := values[firstPlaceholder]

	var additionalFilterExpr string
	additionalNames := make(map[string]string)
	additionalValues := make(map[string]interface{})
	if strings.Contains(expr, " AND ") {
		parts := strings.SplitN(expr, " AND ", 2)
		if len(parts) > 1 {
			additionalFilterExpr = parts[1]
			for k, v := range names {
				if k != "#attr0" {
					additionalNames[k] = v
				}
			}
			for k, v := range values {
				if k != firstPlaceholder {
					additionalValues[k] = v
				}
			}
		}
	}

	indexName := ""
	if attrName != info.PartitionKey {
		found := false
		for _, gsi := range info.GSIs {
			if gsi.PartitionKey == attrName {
				indexName = gsi.Name
				found = true
				break
			}
		}
		if !found {
			return scanPlan
		}
	}

	qNames := map[string]string{"#pk": attrName}
	for k, v := range additionalNames {
		qNames[k] = v
	}
	qValues := map[string]interface{}{firstPlaceholder: value}
	for k, v := range additionalValues {
		qValues[k] = v
	}

	return Plan{
		Mode:                   ModeQuery,
		IndexName:              indexName,
		KeyConditionExpression: fmt.Sprintf("#pk = %s", firstPlaceholder),
		FilterExpression:       additionalFilterExpr,
		Names:                  qNames,
		Values:                 qValues,
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/query/ -v` → all PASS (Task 1 + Task 2). `go vet ./internal/query/` → clean. `go build ./...` → clean.

- [ ] **Step 5: Commit**

```
git add internal/query/plan.go internal/query/plan_test.go
git commit -m "feat(query): add Query-vs-Scan planner (extracted from TUI scanTable)"
```
(+ trailer)

---

## Task 3: Refactor `internal/ui/filter.go` to delegate

Behavior-preserving: `FilterBuilder.BuildExpression` now converts its widgets to `[]query.Condition` and calls `query.BuildExpression`; the now-duplicate `parseValue` is removed.

**Files:**
- Modify: `internal/ui/filter.go`
- Test: `internal/ui/filter_test.go` (new)

- [ ] **Step 1: Write the failing test** — create `internal/ui/filter_test.go`:

```go
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
```

- [ ] **Step 2: Run the test (it should PASS already — the current code produces this)**

Run: `go test ./internal/ui/ -run TestFilterBuilderBuildExpressionDelegates -v`
Expected: PASS (this test characterizes the current behavior; we keep it green through the refactor).

- [ ] **Step 3: Refactor the imports** — in `internal/ui/filter.go`, the import block currently is:

```go
import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)
```

Replace it with (drop `strconv`, add `internal/query`):

```go
import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/godynamo/internal/query"
)
```

- [ ] **Step 4: Remove the duplicate `parseValue`** — delete this entire function from `internal/ui/filter.go`:

```go
// parseValue tries to parse a string value into the appropriate type
// Returns the parsed value (float64, bool, or string)
func parseValue(value string) interface{} {
	// Try to parse as number
	if f, err := strconv.ParseFloat(value, 64); err == nil {
		return f
	}

	// Try to parse as boolean
	if strings.ToLower(value) == "true" {
		return true
	}
	if strings.ToLower(value) == "false" {
		return false
	}

	// Try to parse as null
	if strings.ToLower(value) == "null" {
		return nil
	}

	// Return as string
	return value
}
```

- [ ] **Step 5: Replace `BuildExpression`** — replace the entire `func (f *FilterBuilder) BuildExpression()` method (the big switch) with this delegating version:

```go
// BuildExpression builds a DynamoDB filter expression by delegating to the
// shared query package (single source of truth with the GUI bridge).
func (f *FilterBuilder) BuildExpression() (string, map[string]string, map[string]interface{}) {
	conds := make([]query.Condition, len(f.Conditions))
	for i, c := range f.Conditions {
		conds[i] = query.Condition{
			Name: c.AttributeName.Value(),
			// ui.FilterOperator and query.Operator share the same iota order.
			Operator: query.Operator(c.Operator),
			Value:    c.AttributeValue.Value(),
		}
	}
	return query.BuildExpression(conds)
}
```

- [ ] **Step 6: Verify**

Run: `go test ./internal/ui/ -v` → PASS (incl. the delegation test). `go vet ./internal/ui/` → clean. `go build ./...` → clean.
(If `go vet` reports `strconv` imported and not used, confirm Step 3 removed it; if it reports `parseValue` declared and not used, confirm Step 4 removed it.)

- [ ] **Step 7: Commit**

```
git add internal/ui/filter.go internal/ui/filter_test.go
git commit -m "refactor(ui): delegate filter-expression building to internal/query"
```
(+ trailer)

---

## Task 4: Refactor `internal/app/app.go` scanTable to use the planner

Behavior-preserving: the inline Query-vs-Scan branch becomes `query.BuildPlan` + dispatch. The continuous-scan path is unchanged.

**Files:**
- Modify: `internal/app/app.go`

- [ ] **Step 1: Add the import** — in `internal/app/app.go`, the import block ends with:

```go
	"github.com/godynamo/internal/dynamo"
	"github.com/godynamo/internal/models"
	"github.com/godynamo/internal/ui"
	"github.com/godynamo/internal/ui/textarea"
)
```

Add the `query` import so it becomes:

```go
	"github.com/godynamo/internal/dynamo"
	"github.com/godynamo/internal/models"
	"github.com/godynamo/internal/query"
	"github.com/godynamo/internal/ui"
	"github.com/godynamo/internal/ui/textarea"
)
```

- [ ] **Step 2: Replace `scanTable`** — replace the entire current `func (m *Model) scanTable() tea.Cmd { ... }` (the long version with the inline `firstConditionIsEquals` block) with:

```go
func (m *Model) scanTable() tea.Cmd {
	return func() tea.Msg {
		plan := query.BuildPlan(m.tableInfo, m.filterExpr, m.filterNames, m.filterValues)

		// Query mode: filter's first condition is an equals on the PK / GSI PK.
		if plan.Mode == query.ModeQuery {
			queryInput := dynamo.QueryInput{
				TableName:                m.currentTable,
				IndexName:                plan.IndexName,
				KeyConditionExpression:   plan.KeyConditionExpression,
				FilterExpression:         plan.FilterExpression,
				ExpressionAttributeNames: plan.Names,
				ExpressionValues:         plan.Values,
				Limit:                    m.pageSize,
				ScanIndexForward:         true,
			}
			result, err := m.client.QueryTable(context.Background(), queryInput)
			if err != nil {
				return errMsg{err}
			}
			return queryResultMsg{result}
		}

		// Scan mode with a filter: continuous scan with a 3-minute timeout.
		if m.filterExpr != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			m.scanCancel = cancel

			result, err := m.client.ScanTableContinuous(ctx, m.currentTable, int(m.pageSize), nil, m.filterExpr, m.filterNames, m.filterValues)
			cancel()

			if err != nil {
				return errMsg{err}
			}
			return continuousScanMsg{result: result, totalScanned: result.TotalScanned}
		}

		// No filter: simple scan.
		result, err := m.client.ScanTable(context.Background(), m.currentTable, m.pageSize, nil, m.filterExpr, m.filterNames, m.filterValues)
		if err != nil {
			return errMsg{err}
		}
		return scanResultMsg{result}
	}
}
```

- [ ] **Step 3: Verify**

Run: `go build ./...` → clean. `go vet ./...` → clean. `go test ./... -count=1` → PASS (existing packages still compile/pass; no AWS hit).
Do NOT run the TUI (`go run .`) — it calls real AWS on startup. (Estevao will confirm TUI parity manually.)

- [ ] **Step 4: Commit**

```
git add internal/app/app.go
git commit -m "refactor(app): use internal/query planner in scanTable (behavior-preserving)"
```
(+ trailer)

---

## Task 5: Bridge — `QueryTable` on Backend + `POST /query` endpoint

**Files:**
- Modify: `internal/gui/backend.go`
- Modify: `internal/gui/server.go`
- Test: `internal/gui/server_test.go`

- [ ] **Step 1: Write the failing tests** — in `internal/gui/server_test.go`, (a) add `query`/`queryErr` fields + a `QueryTable` method to `fakeBackend`, and (b) append the new tests.

Change the `fakeBackend` struct from:
```go
type fakeBackend struct {
	tables  []string
	info    *dynamo.TableInfo
	scan    *dynamo.ScanResult
	scanErr error
}
```
to:
```go
type fakeBackend struct {
	tables   []string
	info     *dynamo.TableInfo
	scan     *dynamo.ScanResult
	scanErr  error
	query    *dynamo.QueryResult
	queryErr error
}
```

Add this method right after the existing `ScanTable` method on `fakeBackend`:
```go
func (f *fakeBackend) QueryTable(ctx context.Context, input dynamo.QueryInput) (*dynamo.QueryResult, error) {
	return f.query, f.queryErr
}
```

Append these tests to the end of the file:
```go
func TestQueryModeForPartitionKeyEquals(t *testing.T) {
	s := newTestServer(&fakeBackend{
		info: &dynamo.TableInfo{Name: "t", PartitionKey: "id"},
		query: &dynamo.QueryResult{
			Items: []map[string]types.AttributeValue{{"id": &types.AttributeValueMemberS{Value: "1"}}},
			Count: 1,
		},
	})
	rec := do(s, http.MethodPost, "/tables/t/query", `{"conditions":[{"name":"id","op":"eq","value":"1"}],"limit":10}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Mode  string                   `json:"mode"`
		Items []map[string]interface{} `json:"items"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Mode != "query" {
		t.Fatalf("want mode query, got %q", resp.Mode)
	}
	if len(resp.Items) != 1 || resp.Items[0]["id"] != "1" {
		t.Fatalf("items=%v", resp.Items)
	}
}

func TestQueryFallsBackToScanForNonKey(t *testing.T) {
	s := newTestServer(&fakeBackend{
		info: &dynamo.TableInfo{Name: "t", PartitionKey: "id"},
		scan: &dynamo.ScanResult{
			Items: []map[string]types.AttributeValue{{"status": &types.AttributeValueMemberS{Value: "active"}}},
			Count: 1,
		},
	})
	rec := do(s, http.MethodPost, "/tables/t/query", `{"conditions":[{"name":"status","op":"eq","value":"active"}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Mode string `json:"mode"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Mode != "scan" {
		t.Fatalf("want mode scan, got %q", resp.Mode)
	}
}

func TestQueryUnknownOperator(t *testing.T) {
	s := newTestServer(&fakeBackend{info: &dynamo.TableInfo{PartitionKey: "id"}})
	rec := do(s, http.MethodPost, "/tables/t/query", `{"conditions":[{"name":"id","op":"bogus","value":"1"}]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestQueryNotConnected(t *testing.T) {
	s := newServer("test-token")
	rec := do(s, http.MethodPost, "/tables/t/query", `{"conditions":[]}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d", rec.Code)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/gui/ -v`
Expected: failure — `fakeBackend` does not implement `Backend` (missing `QueryTable`) once the interface is updated, and `404`/route-missing on `/query`. (Before Step 3/4 it may fail to compile because `QueryTable` isn't on the interface yet, or the route 404s.)

- [ ] **Step 3: Add `QueryTable` to the Backend interface** — in `internal/gui/backend.go`, change the interface to add the method (after `ScanTable`):

```go
type Backend interface {
	ListTables(ctx context.Context) ([]string, error)
	DescribeTable(ctx context.Context, name string) (*dynamo.TableInfo, error)
	ScanTable(ctx context.Context, name string, limit int32,
		startKey map[string]types.AttributeValue,
		filterExpr string, names map[string]string, values map[string]interface{}) (*dynamo.ScanResult, error)
	QueryTable(ctx context.Context, input dynamo.QueryInput) (*dynamo.QueryResult, error)
}
```
(The existing `var _ Backend = (*dynamo.Client)(nil)` now also verifies `*dynamo.Client` has `QueryTable` — it does.)

- [ ] **Step 4: Add the route, request types, operator map, and handler** — in `internal/gui/server.go`:

(a) Update the import block to add `query` and `types`:
```go
import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/godynamo/internal/dynamo"
	"github.com/godynamo/internal/models"
	"github.com/godynamo/internal/query"
)
```

(b) Register the route — change `buildHandler` to add the `/query` line:
```go
func (s *server) buildHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /connect", s.handleConnect)
	mux.HandleFunc("GET /tables", s.handleListTables)
	mux.HandleFunc("GET /tables/{name}/schema", s.handleSchema)
	mux.HandleFunc("GET /tables/{name}/scan", s.handleScan)
	mux.HandleFunc("POST /tables/{name}/query", s.handleQuery)
	return s.withMiddleware(mux)
}
```

(c) Add the request types + operator map + handler (place after `handleScan`, before `writeJSON`):
```go
type queryRequest struct {
	Conditions []queryCondition `json:"conditions"`
	Limit      int32            `json:"limit"`
	Cursor     string           `json:"cursor"`
}

type queryCondition struct {
	Name  string `json:"name"`
	Op    string `json:"op"`
	Value string `json:"value"`
}

var queryOperators = map[string]query.Operator{
	"eq":           query.OpEquals,
	"ne":           query.OpNotEquals,
	"gt":           query.OpGreaterThan,
	"lt":           query.OpLessThan,
	"ge":           query.OpGreaterOrEqual,
	"le":           query.OpLessOrEqual,
	"contains":     query.OpContains,
	"not_contains": query.OpNotContains,
	"begins_with":  query.OpBeginsWith,
	"exists":       query.OpExists,
	"not_exists":   query.OpNotExists,
}

func (s *server) handleQuery(w http.ResponseWriter, r *http.Request) {
	backend, ok := s.getBackend()
	if !ok {
		writeError(w, http.StatusConflict, "not connected")
		return
	}
	name := r.PathValue("name")

	var req queryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	conds := make([]query.Condition, 0, len(req.Conditions))
	for _, c := range req.Conditions {
		op, known := queryOperators[c.Op]
		if !known {
			writeError(w, http.StatusBadRequest, "unknown operator: "+c.Op)
			return
		}
		conds = append(conds, query.Condition{Name: c.Name, Operator: op, Value: c.Value})
	}

	limit := int32(500)
	if req.Limit > 0 && req.Limit <= 1000 {
		limit = req.Limit
	}

	startKey, err := decodeCursor(req.Cursor)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	info, err := backend.DescribeTable(r.Context(), name)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	expr, names, values := query.BuildExpression(conds)
	plan := query.BuildPlan(info, expr, names, values)

	var (
		rawItems     []map[string]types.AttributeValue
		lastKey      map[string]types.AttributeValue
		count        int32
		scannedCount int32
		mode         string
	)

	if plan.Mode == query.ModeQuery {
		mode = "query"
		res, qerr := backend.QueryTable(r.Context(), dynamo.QueryInput{
			TableName:                name,
			IndexName:                plan.IndexName,
			KeyConditionExpression:   plan.KeyConditionExpression,
			FilterExpression:         plan.FilterExpression,
			ExpressionAttributeNames: plan.Names,
			ExpressionValues:         plan.Values,
			Limit:                    limit,
			ScanIndexForward:         true,
			StartKey:                 startKey,
		})
		if qerr != nil {
			writeError(w, http.StatusBadGateway, qerr.Error())
			return
		}
		rawItems, lastKey, count, scannedCount = res.Items, res.LastEvaluatedKey, res.Count, res.ScannedCount
	} else {
		mode = "scan"
		res, serr := backend.ScanTable(r.Context(), name, limit, startKey, plan.FilterExpression, plan.Names, plan.Values)
		if serr != nil {
			writeError(w, http.StatusBadGateway, serr.Error())
			return
		}
		rawItems, lastKey, count, scannedCount = res.Items, res.LastEvaluatedKey, res.Count, res.ScannedCount
	}

	cursor, err := encodeCursor(lastKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	items := make([]map[string]interface{}, len(rawItems))
	for i, item := range rawItems {
		converted := make(map[string]interface{}, len(item))
		for k, v := range item {
			converted[k] = models.AttributeValueToInterface(v)
		}
		items[i] = converted
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"mode":         mode,
		"items":        items,
		"cursor":       cursor,
		"count":        count,
		"scannedCount": scannedCount,
	})
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/gui/ -v` → all PASS (existing + 4 new). `go vet ./internal/gui/` → clean. `go build ./...` → clean.

- [ ] **Step 6: Commit**

```
git add internal/gui/backend.go internal/gui/server.go internal/gui/server_test.go
git commit -m "feat(gui): add POST /query endpoint with smart Query-vs-Scan planning"
```
(+ trailer)

---

## Task 6: Renderer — filter panel, page size, Resume fetching

**Files:**
- Modify: `electron/preload.js`
- Modify: `electron/renderer/index.html` (rewrite)
- Modify: `electron/renderer/app.js` (rewrite)
- Modify: `electron/renderer/styles.css` (append)

- [ ] **Step 1: `electron/preload.js` — page size on scan + add query** — replace the `scan:` method (and the closing of the object) with the updated `scan` + new `query`:

Replace:
```js
  scan: (name, cursor) => {
    const q = cursor ? `?cursor=${encodeURIComponent(cursor)}` : ''
    return call('GET', `/tables/${encodeURIComponent(name)}/scan${q}`)
  },
})
```
with:
```js
  scan: (name, cursor, limit) => {
    const params = new URLSearchParams()
    if (cursor) params.set('cursor', cursor)
    if (limit) params.set('limit', String(limit))
    const qs = params.toString()
    return call('GET', `/tables/${encodeURIComponent(name)}/scan${qs ? '?' + qs : ''}`)
  },
  query: (name, body) => call('POST', `/tables/${encodeURIComponent(name)}/query`, body),
})
```

- [ ] **Step 2: `electron/renderer/index.html` — rewrite** — READ the file first (required before overwrite), then WRITE this content (adds filter button, page-size select, renames Load more → Resume fetching, adds a mode badge, and a filter panel):

```html
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta http-equiv="Content-Security-Policy"
        content="default-src 'self'; script-src 'self'; connect-src http://127.0.0.1:* http://localhost:*; style-src 'self' 'unsafe-inline';" />
  <title>GoDynamo</title>
  <link rel="stylesheet" href="styles.css" />
</head>
<body>
  <section id="connect-screen" class="screen">
    <div class="connect-card">
      <h1>⚡ GoDynamo</h1>
      <div class="field">
        <label><input type="radio" name="mode" value="aws" checked /> AWS</label>
        <label><input type="radio" name="mode" value="local" /> DynamoDB Local</label>
      </div>
      <div class="field" id="aws-fields">
        <label for="region">Region</label>
        <select id="region"></select>
      </div>
      <div class="field hidden" id="local-fields">
        <label for="endpoint">Endpoint</label>
        <input type="text" id="endpoint" value="http://localhost:8000" />
      </div>
      <button id="connect-btn">Connect</button>
      <p id="connect-error" class="error"></p>
    </div>
  </section>

  <section id="main-screen" class="screen hidden">
    <aside id="sidebar">
      <input type="text" id="table-filter" placeholder="Filter tables…" />
      <ul id="table-list"></ul>
    </aside>
    <main id="content">
      <header id="toolbar">
        <span id="current-table"></span>
        <button id="filter-btn" disabled>Filter</button>
        <button id="schema-btn" disabled>Schema</button>
        <label id="pagesize-label">Page size
          <select id="page-size">
            <option>50</option>
            <option>100</option>
            <option>300</option>
            <option selected>500</option>
            <option>1000</option>
          </select>
        </label>
        <button id="more-btn" disabled>Resume fetching</button>
        <span id="mode-badge"></span>
        <span id="status"></span>
      </header>
      <section id="filter-panel" class="hidden">
        <div id="filter-rows"></div>
        <div id="filter-actions">
          <button id="filter-add">+ Condition</button>
          <button id="filter-apply">Apply</button>
          <button id="filter-clear">Clear</button>
        </div>
      </section>
      <div id="grid-wrap">
        <table id="grid"><thead></thead><tbody></tbody></table>
      </div>
    </main>
  </section>

  <div id="detail" class="hidden">
    <div class="detail-card">
      <header>
        <span id="detail-title"></span>
        <button id="detail-close">✕</button>
      </header>
      <pre id="detail-body"></pre>
    </div>
  </div>

  <script src="app.js"></script>
</body>
</html>
```

- [ ] **Step 3: `electron/renderer/app.js` — rewrite** — READ the file first, then WRITE this content:

```js
const AWS_REGIONS = [
  'us-east-1','us-east-2','us-west-1','us-west-2','af-south-1','ap-east-1',
  'ap-south-1','ap-south-2','ap-northeast-1','ap-northeast-2','ap-northeast-3',
  'ap-southeast-1','ap-southeast-2','ap-southeast-3','ap-southeast-4',
  'ca-central-1','eu-central-1','eu-central-2','eu-west-1','eu-west-2','eu-west-3',
  'eu-south-1','eu-south-2','eu-north-1','il-central-1','me-south-1','me-central-1','sa-east-1',
]

const OPERATORS = [
  { op: 'eq', label: '= Equals' },
  { op: 'ne', label: '≠ Not Equals' },
  { op: 'gt', label: '> Greater Than' },
  { op: 'lt', label: '< Less Than' },
  { op: 'ge', label: '≥ Greater or Equal' },
  { op: 'le', label: '≤ Less or Equal' },
  { op: 'contains', label: '∋ Contains' },
  { op: 'not_contains', label: '∌ Not Contains' },
  { op: 'begins_with', label: '^ Begins With' },
  { op: 'exists', label: '∃ Exists' },
  { op: 'not_exists', label: '∄ Not Exists' },
]

const state = {
  tables: [],
  currentTable: null,
  keys: { partition: '', sort: '' },
  schemaRaw: '',
  cursor: '',
  items: [],
  conditions: [],
  filterActive: false,
  mode: '',
  scanned: 0,
}

const $ = (id) => document.getElementById(id)
const show = (el) => el.classList.remove('hidden')
const hide = (el) => el.classList.add('hidden')

function initConnectScreen() {
  const regionSel = $('region')
  AWS_REGIONS.forEach((r) => {
    const opt = document.createElement('option')
    opt.value = r
    opt.textContent = r
    regionSel.appendChild(opt)
  })
  document.querySelectorAll('input[name="mode"]').forEach((radio) => {
    radio.addEventListener('change', () => {
      const mode = document.querySelector('input[name="mode"]:checked').value
      if (mode === 'aws') { show($('aws-fields')); hide($('local-fields')) }
      else { hide($('aws-fields')); show($('local-fields')) }
    })
  })
  $('connect-btn').addEventListener('click', onConnect)
}

async function onConnect() {
  const mode = document.querySelector('input[name="mode"]:checked').value
  const cfg = { mode }
  if (mode === 'aws') cfg.region = $('region').value
  else cfg.endpoint = $('endpoint').value

  $('connect-error').textContent = ''
  $('connect-btn').disabled = true
  try {
    const data = await window.api.connect(cfg)
    state.tables = data.tables || []
    renderTableList()
    hide($('connect-screen'))
    show($('main-screen'))
  } catch (err) {
    $('connect-error').textContent = err.message
  } finally {
    $('connect-btn').disabled = false
  }
}

function renderTableList() {
  const filter = $('table-filter').value.toLowerCase()
  const ul = $('table-list')
  ul.innerHTML = ''
  state.tables
    .filter((t) => t.toLowerCase().includes(filter))
    .forEach((t) => {
      const li = document.createElement('li')
      li.textContent = t
      li.className = t === state.currentTable ? 'active' : ''
      li.addEventListener('click', () => selectTable(t))
      ul.appendChild(li)
    })
}

async function selectTable(name) {
  state.currentTable = name
  state.cursor = ''
  state.items = []
  state.conditions = []
  state.filterActive = false
  state.mode = ''
  state.scanned = 0
  $('current-table').textContent = name
  $('status').textContent = 'Loading…'
  $('mode-badge').textContent = ''
  $('schema-btn').disabled = true
  $('filter-btn').disabled = true
  $('more-btn').disabled = true
  hide($('filter-panel'))
  renderFilterRows()
  renderTableList()
  try {
    const schema = await window.api.schema(name)
    state.keys = {
      partition: (schema.info && schema.info.PartitionKey) || '',
      sort: (schema.info && schema.info.SortKey) || '',
    }
    state.schemaRaw = schema.rawJSON || JSON.stringify(schema.info, null, 2)
    $('schema-btn').disabled = false
    $('filter-btn').disabled = false
    await loadPage(true)
  } catch (err) {
    $('status').textContent = 'Error: ' + err.message
  }
}

function pageSize() {
  return parseInt($('page-size').value, 10) || 500
}

function activeConditions() {
  return state.conditions
    .filter((c) => c.name.trim() !== '')
    .map((c) => ({ name: c.name, op: c.op, value: c.value }))
}

async function loadPage(reset) {
  const cursor = reset ? '' : state.cursor
  try {
    let data
    if (state.filterActive) {
      data = await window.api.query(state.currentTable, {
        conditions: activeConditions(),
        limit: pageSize(),
        cursor,
      })
      state.mode = data.mode || ''
      state.scanned = data.scannedCount || 0
    } else {
      data = await window.api.scan(state.currentTable, cursor, pageSize())
      state.mode = ''
      state.scanned = 0
    }
    if (reset) state.items = []
    state.items = state.items.concat(data.items || [])
    state.cursor = data.cursor || ''
    $('more-btn').disabled = !state.cursor
    updateStatus()
    renderGrid()
  } catch (err) {
    $('status').textContent = 'Error: ' + err.message
    $('more-btn').disabled = !state.cursor
  }
}

function updateStatus() {
  let s = `${state.items.length} returned`
  if (state.filterActive) {
    s += ` · scanned ${state.scanned}`
    $('mode-badge').textContent = state.mode ? state.mode.toUpperCase() : ''
  } else {
    $('mode-badge').textContent = ''
  }
  if (state.cursor) s += ' · more available'
  $('status').textContent = s
}

function renderFilterRows() {
  const wrap = $('filter-rows')
  wrap.innerHTML = ''
  state.conditions.forEach((cond, i) => {
    const row = document.createElement('div')
    row.className = 'filter-row'

    const nameIn = document.createElement('input')
    nameIn.type = 'text'
    nameIn.placeholder = 'attribute'
    nameIn.value = cond.name
    nameIn.addEventListener('input', () => { state.conditions[i].name = nameIn.value })

    const opSel = document.createElement('select')
    OPERATORS.forEach((o) => {
      const opt = document.createElement('option')
      opt.value = o.op
      opt.textContent = o.label
      if (o.op === cond.op) opt.selected = true
      opSel.appendChild(opt)
    })
    opSel.addEventListener('change', () => { state.conditions[i].op = opSel.value })

    const valIn = document.createElement('input')
    valIn.type = 'text'
    valIn.placeholder = 'value'
    valIn.value = cond.value
    valIn.addEventListener('input', () => { state.conditions[i].value = valIn.value })

    const rm = document.createElement('button')
    rm.textContent = '✕'
    rm.className = 'filter-remove'
    rm.addEventListener('click', () => removeCondition(i))

    row.appendChild(nameIn)
    row.appendChild(opSel)
    row.appendChild(valIn)
    row.appendChild(rm)
    wrap.appendChild(row)
  })
}

function addCondition() {
  state.conditions.push({ name: '', op: 'eq', value: '' })
  renderFilterRows()
}

function removeCondition(i) {
  state.conditions.splice(i, 1)
  renderFilterRows()
}

function toggleFilter() {
  const panel = $('filter-panel')
  if (panel.classList.contains('hidden')) {
    if (state.conditions.length === 0) addCondition()
    show(panel)
  } else {
    hide(panel)
  }
}

async function applyFilter() {
  state.filterActive = activeConditions().length > 0
  state.cursor = ''
  await loadPage(true)
}

async function clearFilter() {
  state.conditions = []
  state.filterActive = false
  renderFilterRows()
  state.cursor = ''
  await loadPage(true)
}

function columnOrder() {
  const cols = new Set()
  state.items.forEach((it) => Object.keys(it).forEach((k) => cols.add(k)))
  const { partition, sort } = state.keys
  const ordered = []
  if (partition && cols.has(partition)) { ordered.push(partition); cols.delete(partition) }
  if (sort && cols.has(sort)) { ordered.push(sort); cols.delete(sort) }
  return ordered.concat([...cols].sort())
}

function cellText(value) {
  if (value === null || value === undefined) return ''
  if (typeof value === 'object') return JSON.stringify(value)
  return String(value)
}

function renderGrid() {
  const cols = columnOrder()
  const thead = $('grid').querySelector('thead')
  const tbody = $('grid').querySelector('tbody')
  thead.innerHTML = ''
  tbody.innerHTML = ''

  const hr = document.createElement('tr')
  cols.forEach((c) => {
    const th = document.createElement('th')
    th.textContent = c
    hr.appendChild(th)
  })
  thead.appendChild(hr)

  state.items.forEach((item, idx) => {
    const tr = document.createElement('tr')
    cols.forEach((c) => {
      const td = document.createElement('td')
      const text = cellText(item[c])
      td.textContent = text.length > 80 ? text.slice(0, 77) + '…' : text
      tr.appendChild(td)
    })
    tr.addEventListener('click', () => showItem(idx))
    tbody.appendChild(tr)
  })
}

function showItem(idx) {
  $('detail-title').textContent = 'Item'
  $('detail-body').textContent = JSON.stringify(state.items[idx], null, 2)
  show($('detail'))
}

function showSchema() {
  $('detail-title').textContent = 'Schema: ' + state.currentTable
  $('detail-body').textContent = state.schemaRaw || ''
  show($('detail'))
}

window.addEventListener('DOMContentLoaded', () => {
  initConnectScreen()
  $('table-filter').addEventListener('input', renderTableList)
  $('schema-btn').addEventListener('click', showSchema)
  $('filter-btn').addEventListener('click', toggleFilter)
  $('filter-add').addEventListener('click', addCondition)
  $('filter-apply').addEventListener('click', applyFilter)
  $('filter-clear').addEventListener('click', clearFilter)
  $('more-btn').addEventListener('click', () => loadPage(false))
  $('page-size').addEventListener('change', () => { if (state.currentTable) loadPage(true) })
  $('detail-close').addEventListener('click', () => hide($('detail')))
})
```

- [ ] **Step 4: `electron/renderer/styles.css` — append** — add these rules to the END of the file:

```css
#pagesize-label { font-size: 12px; color: #828bb8; display: flex; align-items: center; gap: 4px; }
#pagesize-label select { padding: 2px 4px; font-size: 12px; }
#mode-badge { font-size: 11px; font-weight: bold; color: #0b0f1a; background: #7aa2f7; border-radius: 4px; padding: 1px 6px; }
#mode-badge:empty { display: none; }
#filter-panel { border-bottom: 1px solid #2a3450; padding: 12px; background: #0f1422; }
#filter-rows { display: flex; flex-direction: column; gap: 6px; }
.filter-row { display: flex; gap: 6px; align-items: center; }
.filter-row input[type="text"] { flex: 1; }
.filter-row select { min-width: 160px; }
.filter-remove { padding: 4px 8px; }
#filter-actions { margin-top: 8px; display: flex; gap: 8px; }
```

- [ ] **Step 5: Verify (do NOT launch the GUI)**

Run: `node --check electron/renderer/app.js` (no output), `node --check electron/preload.js` (no output), `go build ./...` (clean).

- [ ] **Step 6: Commit**

```
git add electron/preload.js electron/renderer/index.html electron/renderer/app.js electron/renderer/styles.css
git commit -m "feat(gui): add filter panel, page size, and Resume fetching to the renderer"
```
(+ trailer)

---

## Self-review (performed against the spec)

**1. Spec coverage**

| Spec requirement | Task |
|---|---|
| Shared `internal/query` package | Tasks 1–2 |
| `BuildExpression` (11 operators) extracted | Task 1 |
| `BuildPlan` Query-vs-Scan planner extracted | Task 2 |
| Characterization-test-first (lock TUI behavior) | Tasks 1, 2 (table-driven), 3 (UI delegation) |
| TUI `ui.FilterBuilder` delegates (behavior-preserving) | Task 3 |
| TUI `app.go scanTable` uses planner (behavior-preserving) | Task 4 |
| `Backend.QueryTable` + fake | Task 5 |
| `POST /tables/{name}/query` (mode/items/cursor/count/scannedCount) | Task 5 |
| op string tokens (eq…not_exists) | Task 5 (`queryOperators`) |
| Renderer filter panel (11 ops, add/remove, Apply/Clear) | Task 6 |
| Page-size control | Task 6 |
| "Resume fetching" button | Task 6 |
| Status: returned/scanned + Query/Scan badge | Task 6 (`updateStatus`) |
| No new Go deps; no AWS in tests | All |

No gaps.

**2. Placeholder scan:** No TBD/TODO/"handle errors"/"similar to". Every code step is complete file content or a full function/import block. ✓

**3. Type/name consistency:** `query.Operator`/`OpEquals…OpNotExists`, `query.Condition{Name,Operator,Value}`, `query.ParseValue`, `query.BuildExpression`, `query.Mode`/`ModeScan`/`ModeQuery`, `query.Plan{Mode,IndexName,KeyConditionExpression,FilterExpression,Names,Values}`, `query.BuildPlan(info,expr,names,values)` are used identically across Tasks 1–5. The bridge `queryRequest{Conditions,Limit,Cursor}` / `queryCondition{Name,Op,Value}` JSON keys (`conditions`,`op`,`limit`,`cursor`) match the preload `query(name, {conditions,limit,cursor})` and renderer `activeConditions()` (`{name,op,value}`). Response keys (`mode`,`items`,`cursor`,`count`,`scannedCount`) match `loadPage`/`updateStatus`. `dynamo.QueryInput`/`QueryResult` fields match `internal/dynamo/client.go`. ✓
