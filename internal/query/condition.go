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
