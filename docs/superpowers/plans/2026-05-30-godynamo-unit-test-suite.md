# GoDynamo Unit Test Suite Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a regression-test net (~30% → ~65%+ coverage) over GoDynamo's logic — data conversion, query building, the AWS client (via a mockable seam), and the app state machine — plus CI, fixing 3 latent bugs along the way.

**Architecture:** stdlib `testing`, table-driven, white-box (same-package) tests with hand-written fakes — matching the repo's existing convention (`gui.fakeBackend`). One additive production refactor introduces a `dynamoAPI` interface so `dynamo.Client` can be tested with a fake. **Hard rule: no test ever touches real AWS.**

**Tech Stack:** Go 1.24, `testing`, AWS SDK v2 (`dynamodb`, `dynamodb/types`), bubbletea, GitHub Actions.

---

## ⚠️ Non-negotiable safety rule (applies to every task)

Tests MUST mock AWS and NEVER call it. No test may call `dynamo.NewClient`, `config.LoadDefaultConfig`, `dynamodb.NewFromConfig`, or `DiscoverRegionsWithTables`, nor execute any `tea.Cmd` returned by `app.Update` that calls `m.client.*`. The dev machine's credentials point at the company production account — a real call could destroy data. If something can't be tested without AWS, it's out of scope. See spec: `docs/superpowers/specs/2026-05-30-godynamo-unit-test-suite-design.md`.

## Conventions for every test task

- Test files are `*_test.go` in the **same package** (white-box).
- Use `t.Run(name, ...)` subtests; table-driven where natural.
- **Two kinds of tasks:**
  - **Characterization** (most tasks): the code already works, so the new test should **PASS on first run** — it locks current behavior. If it fails, you found a bug or a wrong assumption — stop and investigate.
  - **Bug-fix (TDD)** (Tasks 2 & 3): write the test that exposes the bug → run → it FAILS (or panics) → apply the fix → run → PASS.
- No new `go.mod` dependencies.
- Commit after each task with a conventional-commit message.

## File map

| File | Status | Responsibility |
|------|--------|----------------|
| `Makefile` | create | test/race/vet/cover shortcuts |
| `.github/workflows/test.yml` | create | CI matrix (ubuntu `-race` / windows), no AWS creds |
| `internal/models/models_test.go` | create | conversions, round-trip, FormatValue |
| `internal/models/models.go` | modify | bug fixes #1 (FormatValue) and #2 (N precision) |
| `internal/ui/fuzzy_test.go` | create | FuzzyFind/fuzzyScore/HighlightMatches |
| `internal/ui/fuzzy.go` | modify | bug fix #3 (remove dead camelCase branch) |
| `internal/ui/components_test.go` | create | DataTable + List logic |
| `internal/ui/json_viewer_test.go` | create | Render/Toggle/Expand/Collapse/Format* |
| `internal/ui/filter_more_test.go` | create | fill filter gaps |
| `main_more_test.go` | create | selectMode edge cases |
| `internal/dynamo/client.go` | modify | add `dynamoAPI` seam (additive) |
| `internal/dynamo/client_test.go` | create | client methods via fake |
| `internal/app/helpers_test.go` | create | formatBytes/extractText/getSortedSelection/itemsToTable |
| `internal/app/update_test.go` | create | handlers + Update transitions + view smoke |

---

## Task 1: Tooling — Makefile

**Files:**
- Create: `Makefile`

- [ ] **Step 1: Create the Makefile**

```makefile
.PHONY: test race vet cover

# Unit tests only — NEVER hits real AWS (see test suite spec).
test:
	go test ./...

race:
	go test ./... -race

vet:
	go vet ./...

cover:
	go test ./... -coverprofile=coverage.out
	go tool cover -html=coverage.out
```

- [ ] **Step 2: Verify it runs the existing suite**

Run: `go test ./...`
Expected: all existing packages `ok` (query, gui, dynamo, main), others `no test files` or `ok`.

- [ ] **Step 3: Commit**

```bash
git add Makefile
git commit -m "build: add Makefile with test/race/vet/cover targets"
```

---

## Task 2: `models` package — conversions, round-trip, FormatValue (+ bug fixes #1, #2)

**Files:**
- Create: `internal/models/models_test.go`
- Modify: `internal/models/models.go` (FormatValue truncation; N precision)

- [ ] **Step 1: Write the test file (characterization + 2 bug-exposing tests)**

```go
package models

import (
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

func TestAttributeValueToInterface(t *testing.T) {
	cases := []struct {
		name string
		in   types.AttributeValue
		want interface{}
	}{
		{"string", &types.AttributeValueMemberS{Value: "hi"}, "hi"},
		{"int", &types.AttributeValueMemberN{Value: "42"}, int64(42)},
		{"float", &types.AttributeValueMemberN{Value: "4.5"}, 4.5},
		{"bool", &types.AttributeValueMemberBOOL{Value: true}, true},
		{"null", &types.AttributeValueMemberNULL{Value: true}, nil},
		{"stringset", &types.AttributeValueMemberSS{Value: []string{"a", "b"}}, []string{"a", "b"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := AttributeValueToInterface(c.in)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %#v want %#v", got, c.want)
			}
		})
	}
}

func TestAttributeValueToInterfaceNested(t *testing.T) {
	in := &types.AttributeValueMemberM{Value: map[string]types.AttributeValue{
		"name": &types.AttributeValueMemberS{Value: "x"},
		"tags": &types.AttributeValueMemberL{Value: []types.AttributeValue{
			&types.AttributeValueMemberN{Value: "1"},
		}},
	}}
	got := AttributeValueToInterface(in).(map[string]interface{})
	if got["name"] != "x" {
		t.Fatalf("name=%v", got["name"])
	}
	if !reflect.DeepEqual(got["tags"], []interface{}{int64(1)}) {
		t.Fatalf("tags=%#v", got["tags"])
	}
}

// Bug fix #2: large integers must NOT lose precision (ParseFloat would round
// 2^53+1 down to 2^53). Fails before the fix, passes after.
func TestAttributeValueToInterfaceLargeIntPrecision(t *testing.T) {
	in := &types.AttributeValueMemberN{Value: "9007199254740993"} // 2^53 + 1
	got := AttributeValueToInterface(in)
	if got != int64(9007199254740993) {
		t.Fatalf("large int lost precision: got %#v want int64(9007199254740993)", got)
	}
}

func TestInterfaceToAttributeValue(t *testing.T) {
	cases := []struct {
		name string
		in   interface{}
		want types.AttributeValue
	}{
		{"string", "hi", &types.AttributeValueMemberS{Value: "hi"}},
		{"int", 7, &types.AttributeValueMemberN{Value: "7"}},
		{"int64", int64(7), &types.AttributeValueMemberN{Value: "7"}},
		{"float", 4.5, &types.AttributeValueMemberN{Value: "4.5"}},
		{"bool", true, &types.AttributeValueMemberBOOL{Value: true}},
		{"nil", nil, &types.AttributeValueMemberNULL{Value: true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := InterfaceToAttributeValue(c.in)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %#v want %#v", got, c.want)
			}
		})
	}
}

func TestRoundTripItemJSON(t *testing.T) {
	item := map[string]types.AttributeValue{
		"id":     &types.AttributeValueMemberS{Value: "abc"},
		"age":    &types.AttributeValueMemberN{Value: "30"},
		"price":  &types.AttributeValueMemberN{Value: "9.99"},
		"active": &types.AttributeValueMemberBOOL{Value: true},
	}
	jsonStr, err := ItemToJSON(item, false)
	if err != nil {
		t.Fatalf("ItemToJSON: %v", err)
	}
	back, err := JSONToItem(jsonStr)
	if err != nil {
		t.Fatalf("JSONToItem: %v", err)
	}
	if !reflect.DeepEqual(back, item) {
		t.Fatalf("round-trip mismatch:\n got %#v\nwant %#v", back, item)
	}
}

func TestGetAttributeType(t *testing.T) {
	cases := []struct {
		in   types.AttributeValue
		want string
	}{
		{&types.AttributeValueMemberS{}, "S"},
		{&types.AttributeValueMemberN{}, "N"},
		{&types.AttributeValueMemberBOOL{}, "BOOL"},
		{&types.AttributeValueMemberNULL{}, "NULL"},
		{&types.AttributeValueMemberL{}, "L"},
		{&types.AttributeValueMemberM{}, "M"},
	}
	for _, c := range cases {
		if got := GetAttributeType(c.in); got != c.want {
			t.Errorf("got %q want %q", got, c.want)
		}
	}
}

func TestFormatValue(t *testing.T) {
	cases := []struct {
		name   string
		in     types.AttributeValue
		maxLen int
		want   string
	}{
		{"null", &types.AttributeValueMemberNULL{Value: true}, 0, "null"},
		{"short no trunc", &types.AttributeValueMemberS{Value: "hi"}, 10, "hi"},
		{"ascii trunc", &types.AttributeValueMemberS{Value: "abcdefghij"}, 8, "abcde..."},
		// Bug fix #1: rune-aware truncation must not split a multibyte rune.
		{"multibyte trunc", &types.AttributeValueMemberS{Value: "ααααα"}, 4, "α..."},
		// Bug fix #1: small maxLen must not panic (was str[:maxLen-3]).
		{"maxLen 1", &types.AttributeValueMemberS{Value: "hello"}, 1, "h"},
		{"maxLen 2", &types.AttributeValueMemberS{Value: "hello"}, 2, "he"},
		{"maxLen 3", &types.AttributeValueMemberS{Value: "hello"}, 3, "hel"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := FormatValue(c.in, c.maxLen); got != c.want {
				t.Fatalf("got %q want %q", got, c.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run the test — expect FAIL/PANIC (bugs not yet fixed)**

Run: `go test ./internal/models/ -run 'TestAttributeValueToInterfaceLargeIntPrecision|TestFormatValue' -v`
Expected: `TestAttributeValueToInterfaceLargeIntPrecision` FAILS (got int64(9007199254740992)); `TestFormatValue/maxLen_1` and `/maxLen_2` PANIC (slice bounds out of range); `multibyte trunc` FAILS.

- [ ] **Step 3: Apply bug fix #1 — rune-aware, panic-safe `FormatValue`**

In `internal/models/models.go`, replace the tail of `FormatValue`:

```go
	runes := []rune(str)
	if maxLen > 0 && len(runes) > maxLen {
		if maxLen <= 3 {
			return string(runes[:maxLen]) // no room for an ellipsis
		}
		return string(runes[:maxLen-3]) + "..."
	}
	return str
}
```

(Replaces the old `if maxLen > 0 && len(str) > maxLen { return str[:maxLen-3] + "..." } return str`.)

- [ ] **Step 4: Apply bug fix #2 — integer-first parsing in the `N` case**

In `internal/models/models.go`, replace the `*types.AttributeValueMemberN` case of `AttributeValueToInterface`:

```go
	case *types.AttributeValueMemberN:
		// Parse as integer first so values between 2^53 and 2^63 keep full
		// precision (ParseFloat would round them). Fall back to float for
		// decimals, preserving the "whole number → int64" display contract,
		// then to the raw string if it isn't numeric at all.
		if i, err := strconv.ParseInt(v.Value, 10, 64); err == nil {
			return i
		}
		if f, err := strconv.ParseFloat(v.Value, 64); err == nil {
			if f == float64(int64(f)) {
				return int64(f)
			}
			return f
		}
		return v.Value
```

- [ ] **Step 5: Run the full models suite — expect PASS**

Run: `go test ./internal/models/ -v`
Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/models/models_test.go internal/models/models.go
git commit -m "test(models): cover conversions + fix FormatValue panic and N precision bugs"
```

---

## Task 3: `ui/fuzzy` — matching logic (+ bug fix #3)

**Files:**
- Create: `internal/ui/fuzzy_test.go`
- Modify: `internal/ui/fuzzy.go` (remove dead camelCase branch + unused `unicode` import)

- [ ] **Step 1: Write the test file**

```go
package ui

import (
	"strings"
	"testing"
)

func TestFuzzyFindEmptyPatternReturnsAll(t *testing.T) {
	items := []string{"alpha", "beta"}
	got := FuzzyFind("", items)
	if len(got) != 2 {
		t.Fatalf("want 2 results, got %d", len(got))
	}
}

func TestFuzzyFindNoMatchExcluded(t *testing.T) {
	got := FuzzyFind("xyz", []string{"alpha", "beta"})
	if len(got) != 0 {
		t.Fatalf("want 0 matches, got %d (%v)", len(got), got)
	}
}

func TestFuzzyFindRanksExactAndPrefixHigher(t *testing.T) {
	items := []string{"u_status", "status", "status_history"}
	got := FuzzyFind("status", items)
	if len(got) == 0 {
		t.Fatal("expected matches")
	}
	// Exact match must rank first.
	if got[0].Text != "status" {
		t.Fatalf("want 'status' first, got %q (full: %v)", got[0].Text, got)
	}
	// Results must be sorted by descending score.
	for i := 1; i < len(got); i++ {
		if got[i-1].Score < got[i].Score {
			t.Fatalf("results not sorted by score: %v", got)
		}
	}
}

func TestFuzzyFindSubsequenceMatches(t *testing.T) {
	got := FuzzyFind("ac", []string{"abc"})
	if len(got) != 1 || got[0].Text != "abc" {
		t.Fatalf("want subsequence match on 'abc', got %v", got)
	}
}

func TestHighlightMatches(t *testing.T) {
	bold := func(s string) string { return "[" + s + "]" }
	plain := func(s string) string { return s }
	// match indices 0 and 2 of "abc" → a highlighted, b plain, c highlighted.
	got := HighlightMatches("abc", []int{0, 2}, plain, bold)
	if got != "[a]b[c]" {
		t.Fatalf("got %q want %q", got, "[a]b[c]")
	}
}

func TestHighlightMatchesNoMatches(t *testing.T) {
	plain := func(s string) string { return "<" + s + ">" }
	got := HighlightMatches("abc", nil, plain, strings.ToUpper)
	if got != "<abc>" {
		t.Fatalf("got %q", got)
	}
}
```

- [ ] **Step 2: Run — expect PASS (characterization)**

Run: `go test ./internal/ui/ -run 'Fuzzy|Highlight' -v`
Expected: all PASS (these lock current behavior before the cleanup).

- [ ] **Step 3: Apply bug fix #3 — remove dead camelCase branch**

In `internal/ui/fuzzy.go`, delete these 4 lines inside `fuzzyScore` (the text is always lower-cased by the caller, so this never fires, and `rune(text[textIdx])` is a byte-index bug):

```go
				// Bonus for camelCase match
				if unicode.IsLower(prevChar) && unicode.IsUpper(rune(text[textIdx])) {
					score += 15
				}
```

Then remove the now-unused `"unicode"` import from the import block at the top of the file.

- [ ] **Step 4: Run — expect PASS (behavior unchanged) + build clean**

Run: `go test ./internal/ui/ -run 'Fuzzy|Highlight' -v && go vet ./internal/ui/`
Expected: all PASS; vet clean (no "imported and not used").

- [ ] **Step 5: Commit**

```bash
git add internal/ui/fuzzy_test.go internal/ui/fuzzy.go
git commit -m "test(ui): cover fuzzy matching + remove dead camelCase branch"
```

---

## Task 4: `ui/components` — DataTable + List logic

**Files:**
- Create: `internal/ui/components_test.go`

- [ ] **Step 1: Write the test file**

```go
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
	if dt.ColWidths[0] != 2 { // max(len("id"), len("1")) = 2
		t.Errorf("col0 width=%d want 2", dt.ColWidths[0])
	}
	if dt.ColWidths[1] != 5 { // max(len("name"), len("alice")) = 5
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
	dt.MoveUp() // already at 0, stays
	if dt.SelectedRow != 0 {
		t.Fatalf("MoveUp past top: %d", dt.SelectedRow)
	}
	dt.MoveDown()
	dt.MoveDown()
	dt.MoveDown() // only 3 rows, last index 2
	if dt.SelectedRow != 2 {
		t.Fatalf("MoveDown past bottom: %d", dt.SelectedRow)
	}
}

func TestDataTableHorizontalNavBounds(t *testing.T) {
	dt := NewDataTable()
	dt.SetData([]string{"a", "b", "c"}, [][]string{{"1", "2", "3"}})
	dt.MoveLeft() // at 0, stays
	if dt.SelectedCol != 0 {
		t.Fatalf("MoveLeft past left: %d", dt.SelectedCol)
	}
	dt.MoveRight()
	dt.MoveRight()
	dt.MoveRight() // only 3 cols, last index 2
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
	l.MoveUp() // stays at 0
	if l.Selected != 0 {
		t.Fatalf("MoveUp past top: %d", l.Selected)
	}
	l.MoveDown()
	l.MoveDown()
	l.MoveDown() // 3 items, last index 2
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
```

- [ ] **Step 2: Run — expect PASS**

Run: `go test ./internal/ui/ -run 'DataTable|List' -v`
Expected: all PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/ui/components_test.go
git commit -m "test(ui): cover DataTable and List navigation/data logic"
```

---

## Task 5: `ui/json_viewer` — render + collapse state + formatters

**Files:**
- Create: `internal/ui/json_viewer_test.go`

- [ ] **Step 1: Write the test file**

```go
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
	_ = jv.Render() // smoke: must not panic
}
```

- [ ] **Step 2: Run — expect PASS**

Run: `go test ./internal/ui/ -run 'JSON' -v`
Expected: all PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/ui/json_viewer_test.go
git commit -m "test(ui): cover JSONViewer render, collapse state, and formatters"
```

---

## Task 6: `ui/filter` — fill gaps

**Files:**
- Create: `internal/ui/filter_more_test.go`

The existing `filter_test.go` covers single-condition delegation + number parsing + iota sync. Add: multi-condition AND, the "Exists" no-value operator, and empty-name skipping.

- [ ] **Step 1: Write the test file**

```go
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
	// leave Conditions[0].AttributeName empty
	fb.Conditions[0].Operator = OpEquals
	fb.Conditions[0].AttributeValue.SetValue("x")

	expr, _, _ := fb.BuildExpression()
	if expr != "" {
		t.Fatalf("empty name should yield empty expr, got %q", expr)
	}
}
```

- [ ] **Step 2: Run — expect PASS**

Run: `go test ./internal/ui/ -run 'FilterBuilder' -v`
Expected: all PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/ui/filter_more_test.go
git commit -m "test(ui): cover multi-condition, Exists, and empty-name filter cases"
```

---

## Task 7: `main` — selectMode edge cases

**Files:**
- Create: `main_more_test.go` (package `main`, repo root)

First inspect the existing `main_test.go` to avoid duplicate test names, then add the missing cases.

- [ ] **Step 1: Check existing coverage**

Run: `grep -n "^func Test" main_test.go`
Expected: a list of existing test func names. Pick non-colliding names below (rename if needed).

- [ ] **Step 2: Write the test file**

```go
package main

import (
	"reflect"
	"testing"
)

func TestSelectModeDefaultsToGUI(t *testing.T) {
	m, rest := selectMode([]string{})
	if m != modeGUI {
		t.Fatalf("empty args: mode=%d want GUI", m)
	}
	if len(rest) != 0 {
		t.Fatalf("rest=%v", rest)
	}
}

func TestSelectModeTUI(t *testing.T) {
	m, rest := selectMode([]string{"tui", "--flag"})
	if m != modeTUI {
		t.Fatalf("mode=%d want TUI", m)
	}
	if !reflect.DeepEqual(rest, []string{"--flag"}) {
		t.Fatalf("rest=%v want [--flag]", rest)
	}
}

func TestSelectModeGUIAliasStripsArg(t *testing.T) {
	m, rest := selectMode([]string{"gui", "--port", "9000"})
	if m != modeGUI {
		t.Fatalf("mode=%d want GUI", m)
	}
	if !reflect.DeepEqual(rest, []string{"--port", "9000"}) {
		t.Fatalf("rest=%v want [--port 9000]", rest)
	}
}

func TestSelectModeUnknownArgPassesThroughToGUI(t *testing.T) {
	m, rest := selectMode([]string{"--debug"})
	if m != modeGUI {
		t.Fatalf("mode=%d want GUI", m)
	}
	if !reflect.DeepEqual(rest, []string{"--debug"}) {
		t.Fatalf("rest=%v want [--debug]", rest)
	}
}
```

- [ ] **Step 3: Run — expect PASS**

Run: `go test . -run TestSelectMode -v`
Expected: all PASS.

- [ ] **Step 4: Commit**

```bash
git add main_more_test.go
git commit -m "test(main): cover selectMode default/tui/gui/passthrough"
```

---

## Task 8: `dynamo` — add the `dynamoAPI` seam (additive refactor)

**Files:**
- Modify: `internal/dynamo/client.go`

- [ ] **Step 1: Add the interface + compile-time assertion**

In `internal/dynamo/client.go`, immediately above the `Client` struct (around line 146), add:

```go
// dynamoAPI is the subset of *dynamodb.Client that Client depends on, extracted
// so tests can inject a fake and NEVER touch real AWS. Mirrors gui.Backend.
type dynamoAPI interface {
	ListTables(context.Context, *dynamodb.ListTablesInput, ...func(*dynamodb.Options)) (*dynamodb.ListTablesOutput, error)
	DescribeTable(context.Context, *dynamodb.DescribeTableInput, ...func(*dynamodb.Options)) (*dynamodb.DescribeTableOutput, error)
	Scan(context.Context, *dynamodb.ScanInput, ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error)
	Query(context.Context, *dynamodb.QueryInput, ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error)
	PutItem(context.Context, *dynamodb.PutItemInput, ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
	DeleteItem(context.Context, *dynamodb.DeleteItemInput, ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error)
	CreateTable(context.Context, *dynamodb.CreateTableInput, ...func(*dynamodb.Options)) (*dynamodb.CreateTableOutput, error)
	GetItem(context.Context, *dynamodb.GetItemInput, ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
}

// Compile-time guarantee that the real client satisfies the seam (fails fast if
// an SDK upgrade changes a signature).
var _ dynamoAPI = (*dynamodb.Client)(nil)
```

- [ ] **Step 2: Change the `Client.db` field type**

In the `Client` struct, change:

```go
	db       *dynamodb.Client
```
to:
```go
	db       dynamoAPI
```

Leave `NewClient` unchanged — `dynamodb.NewFromConfig(...)` returns `*dynamodb.Client`, which satisfies `dynamoAPI`.

- [ ] **Step 3: Verify zero behavior change — build + full existing suite**

Run: `go build ./... && go test ./...`
Expected: build succeeds; every previously-passing package still passes (esp. `internal/gui`, which uses `*dynamo.Client` through `gui.Backend`).

- [ ] **Step 4: Commit**

```bash
git add internal/dynamo/client.go
git commit -m "refactor(dynamo): extract dynamoAPI seam so Client is mockable"
```

---

## Task 9: `dynamo` — client method tests via fake

**Files:**
- Create: `internal/dynamo/client_test.go`

- [ ] **Step 1: Write the fake + tests**

```go
package dynamo

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// fakeAPI implements dynamoAPI with canned outputs — NEVER touches AWS.
// list/scan outputs are returned in sequence to exercise pagination loops.
type fakeAPI struct {
	listOuts  []*dynamodb.ListTablesOutput
	listCalls int
	describe  *dynamodb.DescribeTableOutput
	scanOuts  []*dynamodb.ScanOutput
	scanCalls int
	scanErr   error
	query     *dynamodb.QueryOutput
	getOut    *dynamodb.GetItemOutput
	putErr    error
	delErr    error
	createErr error

	lastScan   *dynamodb.ScanInput
	lastQuery  *dynamodb.QueryInput
	lastCreate *dynamodb.CreateTableInput
	lastPut    *dynamodb.PutItemInput
	lastDelete *dynamodb.DeleteItemInput
}

func (f *fakeAPI) ListTables(_ context.Context, _ *dynamodb.ListTablesInput, _ ...func(*dynamodb.Options)) (*dynamodb.ListTablesOutput, error) {
	out := f.listOuts[f.listCalls]
	f.listCalls++
	return out, nil
}
func (f *fakeAPI) DescribeTable(_ context.Context, _ *dynamodb.DescribeTableInput, _ ...func(*dynamodb.Options)) (*dynamodb.DescribeTableOutput, error) {
	return f.describe, nil
}
func (f *fakeAPI) Scan(_ context.Context, in *dynamodb.ScanInput, _ ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error) {
	f.lastScan = in
	if f.scanErr != nil {
		return nil, f.scanErr
	}
	out := f.scanOuts[f.scanCalls]
	f.scanCalls++
	return out, nil
}
func (f *fakeAPI) Query(_ context.Context, in *dynamodb.QueryInput, _ ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
	f.lastQuery = in
	return f.query, nil
}
func (f *fakeAPI) PutItem(_ context.Context, in *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	f.lastPut = in
	return &dynamodb.PutItemOutput{}, f.putErr
}
func (f *fakeAPI) DeleteItem(_ context.Context, in *dynamodb.DeleteItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error) {
	f.lastDelete = in
	return &dynamodb.DeleteItemOutput{}, f.delErr
}
func (f *fakeAPI) CreateTable(_ context.Context, in *dynamodb.CreateTableInput, _ ...func(*dynamodb.Options)) (*dynamodb.CreateTableOutput, error) {
	f.lastCreate = in
	return &dynamodb.CreateTableOutput{}, f.createErr
}
func (f *fakeAPI) GetItem(_ context.Context, _ *dynamodb.GetItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	return f.getOut, nil
}

func newTestClient(f *fakeAPI) *Client {
	return &Client{db: f, region: "us-east-1"}
}

func TestListTablesPaginates(t *testing.T) {
	f := &fakeAPI{listOuts: []*dynamodb.ListTablesOutput{
		{TableNames: []string{"a", "b"}, LastEvaluatedTableName: aws.String("b")},
		{TableNames: []string{"c"}}, // no LastEvaluatedTableName → stop
	}}
	got, err := newTestClient(f).ListTables(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a", "b", "c"}
	if len(got) != 3 || got[0] != "a" || got[2] != "c" {
		t.Fatalf("got %v want %v", got, want)
	}
	if f.listCalls != 2 {
		t.Fatalf("expected 2 paginated calls, got %d", f.listCalls)
	}
}

func TestDescribeTableParsesSchema(t *testing.T) {
	f := &fakeAPI{describe: &dynamodb.DescribeTableOutput{Table: &types.TableDescription{
		TableName:      aws.String("Users"),
		TableStatus:    types.TableStatusActive,
		ItemCount:      aws.Int64(10),
		TableSizeBytes: aws.Int64(2048),
		AttributeDefinitions: []types.AttributeDefinition{
			{AttributeName: aws.String("pk"), AttributeType: types.ScalarAttributeTypeS},
			{AttributeName: aws.String("sk"), AttributeType: types.ScalarAttributeTypeN},
		},
		KeySchema: []types.KeySchemaElement{
			{AttributeName: aws.String("pk"), KeyType: types.KeyTypeHash},
			{AttributeName: aws.String("sk"), KeyType: types.KeyTypeRange},
		},
		GlobalSecondaryIndexes: []types.GlobalSecondaryIndexDescription{
			{IndexName: aws.String("gsi1"), IndexStatus: types.IndexStatusActive,
				KeySchema: []types.KeySchemaElement{
					{AttributeName: aws.String("gpk"), KeyType: types.KeyTypeHash},
				}},
		},
	}}}
	info, err := newTestClient(f).DescribeTable(context.Background(), "Users")
	if err != nil {
		t.Fatal(err)
	}
	if info.PartitionKey != "pk" || info.PartitionType != "S" {
		t.Errorf("partition: %q/%q", info.PartitionKey, info.PartitionType)
	}
	if info.SortKey != "sk" || info.SortKeyType != "N" {
		t.Errorf("sort: %q/%q", info.SortKey, info.SortKeyType)
	}
	if len(info.GSIs) != 1 || info.GSIs[0].Name != "gsi1" || info.GSIs[0].PartitionKey != "gpk" {
		t.Errorf("gsi: %+v", info.GSIs)
	}
	if info.ItemCount != 10 || info.SizeBytes != 2048 {
		t.Errorf("counts: %d/%d", info.ItemCount, info.SizeBytes)
	}
}

func TestScanTablePassesFilterAndConvertsValues(t *testing.T) {
	f := &fakeAPI{scanOuts: []*dynamodb.ScanOutput{{
		Items: []map[string]types.AttributeValue{
			{"id": &types.AttributeValueMemberS{Value: "1"}},
		},
		Count:        1,
		ScannedCount: 5,
	}}}
	res, err := newTestClient(f).ScanTable(context.Background(), "T", 100, nil,
		"#a = :v", map[string]string{"#a": "name"}, map[string]interface{}{":v": "alice"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Count != 1 || res.ScannedCount != 5 || len(res.Items) != 1 {
		t.Fatalf("result=%+v", res)
	}
	if aws.ToString(f.lastScan.FilterExpression) != "#a = :v" {
		t.Errorf("filter not passed: %v", f.lastScan.FilterExpression)
	}
	v, ok := f.lastScan.ExpressionAttributeValues[":v"].(*types.AttributeValueMemberS)
	if !ok || v.Value != "alice" {
		t.Errorf("value not converted: %#v", f.lastScan.ExpressionAttributeValues[":v"])
	}
}

func TestScanTableContinuousAccumulatesAcrossPages(t *testing.T) {
	f := &fakeAPI{scanOuts: []*dynamodb.ScanOutput{
		{Items: []map[string]types.AttributeValue{{"id": &types.AttributeValueMemberS{Value: "1"}}},
			ScannedCount: 3, LastEvaluatedKey: map[string]types.AttributeValue{"id": &types.AttributeValueMemberS{Value: "1"}}},
		{Items: []map[string]types.AttributeValue{{"id": &types.AttributeValueMemberS{Value: "2"}}},
			ScannedCount: 4}, // no LastEvaluatedKey → exhausted
	}}
	res, err := newTestClient(f).ScanTableContinuous(context.Background(), "T", 10, nil, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 2 {
		t.Fatalf("want 2 accumulated items, got %d", len(res.Items))
	}
	if res.TotalScanned != 7 {
		t.Fatalf("TotalScanned=%d want 7", res.TotalScanned)
	}
	if res.HasMore || res.TimedOut {
		t.Fatalf("expected exhausted clean: hasMore=%v timedOut=%v", res.HasMore, res.TimedOut)
	}
}

func TestScanTableContinuousCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before the first iteration
	f := &fakeAPI{}
	res, err := newTestClient(f).ScanTableContinuous(ctx, "T", 10, nil, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.TimedOut {
		t.Fatal("cancelled context should set TimedOut=true")
	}
	if f.scanCalls != 0 {
		t.Fatalf("cancelled context must not call Scan, got %d calls", f.scanCalls)
	}
}

func TestQueryTablePassesIndexAndLimit(t *testing.T) {
	f := &fakeAPI{query: &dynamodb.QueryOutput{Count: 2}}
	_, err := newTestClient(f).QueryTable(context.Background(), QueryInput{
		TableName:              "T",
		IndexName:              "gsi1",
		KeyConditionExpression: "#a = :v",
		ExpressionValues:       map[string]interface{}{":v": 5},
		Limit:                  25,
	})
	if err != nil {
		t.Fatal(err)
	}
	if aws.ToString(f.lastQuery.IndexName) != "gsi1" {
		t.Errorf("index not passed: %v", f.lastQuery.IndexName)
	}
	if aws.ToInt32(f.lastQuery.Limit) != 25 {
		t.Errorf("limit not passed: %v", f.lastQuery.Limit)
	}
	n, ok := f.lastQuery.ExpressionAttributeValues[":v"].(*types.AttributeValueMemberN)
	if !ok || n.Value != "5" {
		t.Errorf("value not converted: %#v", f.lastQuery.ExpressionAttributeValues[":v"])
	}
}

func TestCreateTableBillingModes(t *testing.T) {
	t.Run("pay per request", func(t *testing.T) {
		f := &fakeAPI{}
		err := newTestClient(f).CreateTable(context.Background(), CreateTableInput{
			TableName: "T", PartitionKey: "pk", PartitionType: "S", BillingMode: "PAY_PER_REQUEST",
		})
		if err != nil {
			t.Fatal(err)
		}
		if f.lastCreate.BillingMode != types.BillingModePayPerRequest {
			t.Errorf("billing=%v", f.lastCreate.BillingMode)
		}
		if f.lastCreate.ProvisionedThroughput != nil {
			t.Error("PAY_PER_REQUEST must not set provisioned throughput")
		}
	})
	t.Run("provisioned with sort key", func(t *testing.T) {
		f := &fakeAPI{}
		err := newTestClient(f).CreateTable(context.Background(), CreateTableInput{
			TableName: "T", PartitionKey: "pk", PartitionType: "S",
			SortKey: "sk", SortKeyType: "N", BillingMode: "PROVISIONED",
			ReadCapacity: 5, WriteCapacity: 7,
		})
		if err != nil {
			t.Fatal(err)
		}
		if f.lastCreate.BillingMode != types.BillingModeProvisioned {
			t.Errorf("billing=%v", f.lastCreate.BillingMode)
		}
		if aws.ToInt64(f.lastCreate.ProvisionedThroughput.ReadCapacityUnits) != 5 {
			t.Errorf("read cap=%v", f.lastCreate.ProvisionedThroughput.ReadCapacityUnits)
		}
		if len(f.lastCreate.KeySchema) != 2 {
			t.Errorf("expected pk+sk schema, got %d", len(f.lastCreate.KeySchema))
		}
	})
}

func TestPutAndDeletePropagateErrors(t *testing.T) {
	f := &fakeAPI{putErr: errors.New("boom")}
	if err := newTestClient(f).PutItem(context.Background(), "T", nil); err == nil {
		t.Fatal("PutItem should propagate the error")
	}
	f2 := &fakeAPI{delErr: errors.New("boom")}
	if err := newTestClient(f2).DeleteItem(context.Background(), "T", nil); err == nil {
		t.Fatal("DeleteItem should propagate the error")
	}
}

func TestGetItemReturnsItem(t *testing.T) {
	f := &fakeAPI{getOut: &dynamodb.GetItemOutput{Item: map[string]types.AttributeValue{
		"id": &types.AttributeValueMemberS{Value: "1"},
	}}}
	got, err := newTestClient(f).GetItem(context.Background(), "T", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got["id"].(*types.AttributeValueMemberS).Value != "1" {
		t.Fatalf("got %#v", got)
	}
}

func TestInterfaceToAttributeValueConversions(t *testing.T) {
	cases := []struct {
		in   interface{}
		want string // expected member type tag
	}{
		{"s", "S"}, {7, "N"}, {int64(9), "N"}, {3.14, "N"}, {true, "BOOL"},
	}
	for _, c := range cases {
		got := interfaceToAttributeValue(c.in)
		if tag := memberTag(got); tag != c.want {
			t.Errorf("%v: got %s want %s", c.in, tag, c.want)
		}
	}
}

func memberTag(av types.AttributeValue) string {
	switch av.(type) {
	case *types.AttributeValueMemberS:
		return "S"
	case *types.AttributeValueMemberN:
		return "N"
	case *types.AttributeValueMemberBOOL:
		return "BOOL"
	default:
		return "?"
	}
}
```

- [ ] **Step 2: Run — expect PASS**

Run: `go test ./internal/dynamo/ -v`
Expected: all PASS (new client tests + existing profiles tests). **No network access occurs.**

- [ ] **Step 3: Commit**

```bash
git add internal/dynamo/client_test.go
git commit -m "test(dynamo): cover client methods via fake (no real AWS)"
```

---

## Task 10: `app` — pure helpers

**Files:**
- Create: `internal/app/helpers_test.go`

- [ ] **Step 1: Write the test file**

```go
package app

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/godynamo/internal/dynamo"
)

func TestFormatBytes(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{512, "512 bytes"},
		{1024, "1.00 KB"},
		{1536, "1.50 KB"},
		{1048576, "1.00 MB"},
		{1073741824, "1.00 GB"},
	}
	for _, c := range cases {
		if got := formatBytes(c.in); got != c.want {
			t.Errorf("formatBytes(%d)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestExtractTextSingleLine(t *testing.T) {
	got := extractText("hello world", 0, 0, 0, 5)
	if got != "hello" {
		t.Fatalf("got %q want %q", got, "hello")
	}
}

func TestExtractTextMultiLine(t *testing.T) {
	got := extractText("abc\ndef\nghi", 0, 1, 2, 2)
	if got != "bc\ndef\ngh" {
		t.Fatalf("got %q want %q", got, "bc\ndef\ngh")
	}
}

func TestExtractTextNormalizesReversedRange(t *testing.T) {
	// end before start on the same line — should swap and return the same span.
	got := extractText("hello", 0, 5, 0, 0)
	if got != "hello" {
		t.Fatalf("got %q want %q", got, "hello")
	}
}

func TestGetSortedSelectionForward(t *testing.T) {
	sR, sC, eR, eC := getSortedSelection(0, 1, 0, 3)
	if sR != 0 || sC != 1 || eR != 0 || eC != 4 { // end col made exclusive (+1)
		t.Fatalf("got %d,%d,%d,%d", sR, sC, eR, eC)
	}
}

func TestGetSortedSelectionReversed(t *testing.T) {
	// current before start → sorted so start<end, end col exclusive.
	sR, sC, eR, eC := getSortedSelection(2, 5, 1, 2)
	if sR != 1 || sC != 2 || eR != 2 || eC != 6 {
		t.Fatalf("got %d,%d,%d,%d", sR, sC, eR, eC)
	}
}

func TestItemsToTableEmpty(t *testing.T) {
	m := New()
	headers, rows := m.itemsToTable(nil)
	if len(headers) != 0 || len(rows) != 0 {
		t.Fatalf("expected empty, got %v / %v", headers, rows)
	}
}

func TestItemsToTableOrdersKeysWithPartitionFirst(t *testing.T) {
	m := New()
	m.tableInfo = &dynamo.TableInfo{PartitionKey: "id", SortKey: "ts"}
	items := []map[string]types.AttributeValue{
		{
			"id":   &types.AttributeValueMemberS{Value: "1"},
			"ts":   &types.AttributeValueMemberN{Value: "100"},
			"name": &types.AttributeValueMemberS{Value: "alice"},
		},
	}
	headers, rows := m.itemsToTable(items)
	if len(headers) != 3 || headers[0] != "id" || headers[1] != "ts" {
		t.Fatalf("headers not ordered pk/sk first: %v", headers)
	}
	if headers[2] != "name" { // remaining keys sorted alphabetically
		t.Fatalf("third header=%q want name", headers[2])
	}
	if len(rows) != 1 || rows[0][0] != "1" || rows[0][2] != "alice" {
		t.Fatalf("row=%v", rows[0])
	}
}
```

- [ ] **Step 2: Run — expect PASS**

Run: `go test ./internal/app/ -run 'FormatBytes|ExtractText|SortedSelection|ItemsToTable' -v`
Expected: all PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/app/helpers_test.go
git commit -m "test(app): cover pure helpers (formatBytes, extractText, selection, itemsToTable)"
```

---

## Task 11: `app` — message handlers, Update transitions, view smoke tests

**Files:**
- Create: `internal/app/update_test.go`

**CRITICAL:** these tests drive `Update` with message structs and assert state. They must NEVER execute a returned `tea.Cmd` (that would call real AWS). They never set `m.client`.

- [ ] **Step 1: Write the test file**

```go
package app

import (
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/godynamo/internal/dynamo"
)

// drive feeds one message through Update and returns the updated Model.
// The returned tea.Cmd is intentionally DISCARDED and never executed.
func drive(m Model, msg tea.Msg) Model {
	updated, _ := m.Update(msg)
	return updated.(Model)
}

func TestUpdateWindowSizeSetsDimensions(t *testing.T) {
	m := drive(New(), tea.WindowSizeMsg{Width: 120, Height: 40})
	if m.width != 120 || m.height != 40 {
		t.Fatalf("dimensions not set: %d x %d", m.width, m.height)
	}
}

func TestUpdateErrMsgSetsErrorAndStopsLoading(t *testing.T) {
	m := New()
	m.loading = true
	m = drive(m, errMsg{err: errTest})
	if m.err == nil {
		t.Fatal("err not set")
	}
	if m.loading {
		t.Fatal("loading should be false after errMsg")
	}
}

func TestUpdateTablesLoadedPopulatesList(t *testing.T) {
	m := drive(New(), tablesLoadedMsg{tables: []string{"Users", "Orders"}})
	if len(m.tables) != 2 {
		t.Fatalf("tables=%v", m.tables)
	}
	if m.loading {
		t.Fatal("loading should be false after tables load")
	}
}

func TestHandleScanResultPopulatesTable(t *testing.T) {
	m := New()
	m.tableInfo = &dynamo.TableInfo{PartitionKey: "id"}
	m.handleScanResult(&dynamo.ScanResult{
		Items: []map[string]types.AttributeValue{
			{"id": &types.AttributeValueMemberS{Value: "1"}},
			{"id": &types.AttributeValueMemberS{Value: "2"}},
		},
		Count: 2,
	})
	if m.loading {
		t.Fatal("loading should be false")
	}
	if len(m.items) != 2 {
		t.Fatalf("items=%d", len(m.items))
	}
	if len(m.dataTable.Rows) != 2 {
		t.Fatalf("dataTable rows=%d", len(m.dataTable.Rows))
	}
}

func TestHandleContinuousScanResultStatusReflectsTimeout(t *testing.T) {
	m := New()
	m.tableInfo = &dynamo.TableInfo{PartitionKey: "id"}
	m.handleContinuousScanResult(&dynamo.ContinuousScanResult{
		Items:        []map[string]types.AttributeValue{{"id": &types.AttributeValueMemberS{Value: "1"}}},
		TotalScanned: 500,
		TimedOut:     true,
		HasMore:      true,
	})
	if !strings.Contains(m.statusMsg, "Timeout") {
		t.Fatalf("status should mention timeout: %q", m.statusMsg)
	}
}

func TestHandleQueryResultSetsStatus(t *testing.T) {
	m := New()
	m.tableInfo = &dynamo.TableInfo{PartitionKey: "id"}
	m.handleQueryResult(&dynamo.QueryResult{
		Items: []map[string]types.AttributeValue{{"id": &types.AttributeValueMemberS{Value: "1"}}},
		Count: 1,
	})
	if !strings.Contains(m.statusMsg, "1") {
		t.Fatalf("status=%q", m.statusMsg)
	}
}

// View smoke tests: each view must render without panicking once the model has
// minimal state. We set width/height so layout math has sane inputs.
func TestViewSmokeAllModes(t *testing.T) {
	modes := []viewMode{
		viewConnect, viewSelectRegion, viewTables, viewTableData,
		viewItemDetail, viewCreateTable, viewQuery, viewExport, viewSchema,
	}
	for _, vm := range modes {
		m := New()
		m.width, m.height = 100, 30
		m.view = vm
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("view %d panicked: %v", vm, r)
				}
			}()
			_ = m.View()
		}()
	}
}

var errTest = testError("test error")

type testError string

func (e testError) Error() string { return string(e) }
```

- [ ] **Step 2: Run — expect PASS**

Run: `go test ./internal/app/ -v`
Expected: all PASS. If any `viewX` panics (e.g. nil `tableInfo` deref), add the minimal field to the model in `TestViewSmokeAllModes` before calling `View()` — do not change production code unless it's a real nil-safety bug, in which case stop and report it.

- [ ] **Step 3: Commit**

```bash
git add internal/app/update_test.go
git commit -m "test(app): cover message handlers, Update transitions, and view smoke tests"
```

---

## Task 12: CI — GitHub Actions workflow

**Files:**
- Create: `.github/workflows/test.yml`

- [ ] **Step 1: Write the workflow**

```yaml
name: test

on:
  push:
  pull_request:

permissions:
  contents: read

jobs:
  test:
    strategy:
      fail-fast: false
      matrix:
        include:
          - os: ubuntu-latest
            race: "-race"
          - os: windows-latest
            race: ""
    runs-on: ${{ matrix.os }}
    env:
      # SAFETY: guarantee no AWS credentials exist in CI. If a test ever tries to
      # reach AWS it fails on missing creds instead of hitting a real account.
      AWS_ACCESS_KEY_ID: ""
      AWS_SECRET_ACCESS_KEY: ""
      AWS_SESSION_TOKEN: ""
      AWS_PROFILE: ""
      AWS_EC2_METADATA_DISABLED: "true"
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.24"
          cache: true
      - name: Vet
        run: go vet ./...
      - name: Test
        run: go test ./... ${{ matrix.race }} -coverprofile=coverage.out
```

- [ ] **Step 2: Validate locally (mirror what CI runs)**

Run: `go vet ./... && go test ./... -coverprofile=coverage.out`
Expected: vet clean; all packages pass.

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/test.yml
git commit -m "ci: run vet + tests on ubuntu (-race) and windows, with no AWS creds"
```

---

## Task 13: Final verification

**Files:** none (verification only)

- [ ] **Step 1: Full suite with race detector**

Run: `go test ./... -race`
Expected: every package `ok`. No data-race reports.

- [ ] **Step 2: Coverage report**

Run: `go test ./... -cover`
Expected (approximate targets from the spec): `models` ~95%, `ui` ~55-65%, `dynamo` ~70%, `app` ~45%, `query` ≥92%, `gui` ≥64%, `main` ~60%; global ~65%+. Note actuals; if `app` or `dynamo` are far below target, identify the largest uncovered functions and add a focused test (still no AWS).

- [ ] **Step 3: Vet + build**

Run: `go vet ./... && go build ./...`
Expected: clean.

- [ ] **Step 4: Final commit (if any coverage top-ups were added)**

```bash
git add -A
git commit -m "test: coverage top-ups to hit suite targets"
```

---

## Self-review notes (resolved during planning)

- **Spec coverage:** every package in spec §5 maps to a task (models→T2, fuzzy→T3, components→T4, json_viewer→T5, filter→T6, main→T7, dynamo seam→T8, dynamo client→T9, app helpers→T10, app transitions+views→T11, CI→T12, Makefile→T1). The 3 bug fixes (spec §6) are in T2 (×2) and T3 (×1). The AWS-safety rule (spec critical warning) is enforced in T8/T9/T11 notes and the T12 no-creds env.
- **Type consistency:** `dynamoAPI` method set in T8 matches the fake's methods in T9. `Client{db: ...}` field type (`dynamoAPI`) matches the fake injection. Helper names (`formatBytes`, `extractText`, `getSortedSelection`, `itemsToTable`, `handleScanResult`, `handleContinuousScanResult`, `handleQueryResult`) and message types (`errMsg`, `tablesLoadedMsg`, `scanResultMsg`) match the real `app.go` symbols verified during planning.
- **Known adjustment points (flagged in-task, not placeholders):** T6 Step 2 (confirm the real FilterBuilder add-condition method name) and T11 Step 2 (add minimal model fields if a view smoke test reveals a real nil deref). Both are verification gates with explicit fallback instructions.
