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

	// An equals first-condition always carries a value placeholder; an empty
	// values map means malformed input from a non-BuildExpression caller — Scan.
	if len(values) == 0 {
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
		// Defensive fallback (mirrors the TUI); unreachable when the expression
		// comes from BuildExpression, which always names the first value :val0.
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

// PlanForIndex builds a Query plan that targets a specific index, or the base
// table when indexName == "". The first equality (=) condition on that target's
// partition key becomes the key condition; the remaining conditions become the
// filter (mirroring BuildPlan: only the partition key enters the key condition,
// any sort-key condition stays in the filter). It returns an error when the
// schema is missing, the index is unknown, or there is no equality on the
// target's partition key.
func PlanForIndex(info *dynamo.TableInfo, conds []Condition, indexName string) (Plan, error) {
	if info == nil {
		return Plan{}, fmt.Errorf("table schema unavailable")
	}

	keyAttr := info.PartitionKey
	if indexName != "" {
		found := false
		for _, gsi := range info.GSIs {
			if gsi.Name == indexName {
				keyAttr = gsi.PartitionKey
				found = true
				break
			}
		}
		if !found {
			return Plan{}, fmt.Errorf("unknown index: %s", indexName)
		}
	}
	if keyAttr == "" {
		return Plan{}, fmt.Errorf("target has no partition key")
	}

	keyIdx := -1
	for i, c := range conds {
		if c.Name == keyAttr && c.Operator == OpEquals && strings.TrimSpace(c.Value) != "" {
			keyIdx = i
			break
		}
	}
	if keyIdx < 0 {
		target := "table"
		if indexName != "" {
			target = "index " + indexName
		}
		return Plan{}, fmt.Errorf("%s requires an equality (=) condition on its partition key %q", target, keyAttr)
	}

	rest := make([]Condition, 0, len(conds))
	for i, c := range conds {
		if i != keyIdx {
			rest = append(rest, c)
		}
	}

	names := map[string]string{"#pk": keyAttr}
	values := map[string]interface{}{":pkval": ParseValue(conds[keyIdx].Value)}

	filterExpr, fNames, fValues := BuildExpression(rest)
	for k, v := range fNames {
		names[k] = v
	}
	for k, v := range fValues {
		values[k] = v
	}

	return Plan{
		Mode:                   ModeQuery,
		IndexName:              indexName,
		KeyConditionExpression: "#pk = :pkval",
		FilterExpression:       filterExpr,
		Names:                  names,
		Values:                 values,
	}, nil
}
