package ui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// JSONViewer renders JSON with syntax highlighting
type JSONViewer struct {
	Data      interface{}
	Collapsed map[string]bool
	Indent    int
}

// NewJSONViewer creates a new JSONViewer
func NewJSONViewer(data interface{}) *JSONViewer {
	return &JSONViewer{
		Data:      data,
		Collapsed: make(map[string]bool),
		Indent:    2,
	}
}

// Render returns a syntax-highlighted string representation
func (j *JSONViewer) Render() string {
	return j.renderValue(j.Data, 0, "root")
}

func (j *JSONViewer) renderValue(v interface{}, indent int, path string) string {
	indentStr := strings.Repeat(" ", indent)

	switch val := v.(type) {
	case nil:
		return JSONNullStyle.Render("null")

	case bool:
		return JSONBoolStyle.Render(fmt.Sprintf("%v", val))

	case float64:
		// Check if it's an integer
		if val == float64(int64(val)) {
			return JSONNumberStyle.Render(fmt.Sprintf("%.0f", val))
		}
		return JSONNumberStyle.Render(fmt.Sprintf("%v", val))

	case int64:
		return JSONNumberStyle.Render(fmt.Sprintf("%d", val))

	case int:
		return JSONNumberStyle.Render(fmt.Sprintf("%d", val))

	case string:
		escaped, _ := json.Marshal(val)
		return JSONStringStyle.Render(string(escaped))

	case []interface{}:
		if len(val) == 0 {
			return "[]"
		}

		if j.Collapsed[path] {
			return fmt.Sprintf("[...] %s", HelpStyle.Render(fmt.Sprintf("(%d items)", len(val))))
		}

		var b strings.Builder
		b.WriteString("[\n")
		for i, item := range val {
			itemPath := fmt.Sprintf("%s[%d]", path, i)
			b.WriteString(indentStr)
			b.WriteString(strings.Repeat(" ", j.Indent))
			b.WriteString(j.renderValue(item, indent+j.Indent, itemPath))
			if i < len(val)-1 {
				b.WriteString(",")
			}
			b.WriteString("\n")
		}
		b.WriteString(indentStr)
		b.WriteString("]")
		return b.String()

	case map[string]interface{}:
		if len(val) == 0 {
			return "{}"
		}

		if j.Collapsed[path] {
			return fmt.Sprintf("{...} %s", HelpStyle.Render(fmt.Sprintf("(%d keys)", len(val))))
		}

		// Sort keys for consistent output
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		var b strings.Builder
		b.WriteString("{\n")
		for i, k := range keys {
			keyPath := fmt.Sprintf("%s.%s", path, k)
			b.WriteString(indentStr)
			b.WriteString(strings.Repeat(" ", j.Indent))
			b.WriteString(JSONKeyStyle.Render(fmt.Sprintf("\"%s\"", k)))
			b.WriteString(": ")
			b.WriteString(j.renderValue(val[k], indent+j.Indent, keyPath))
			if i < len(keys)-1 {
				b.WriteString(",")
			}
			b.WriteString("\n")
		}
		b.WriteString(indentStr)
		b.WriteString("}")
		return b.String()

	default:
		return fmt.Sprintf("%v", val)
	}
}

// Toggle collapses or expands a path
func (j *JSONViewer) Toggle(path string) {
	j.Collapsed[path] = !j.Collapsed[path]
}

// ExpandAll expands all paths
func (j *JSONViewer) ExpandAll() {
	j.Collapsed = make(map[string]bool)
}

// CollapseAll collapses all paths
func (j *JSONViewer) CollapseAll() {
	j.collapseRecursive(j.Data, "root")
}

func (j *JSONViewer) collapseRecursive(v interface{}, path string) {
	switch val := v.(type) {
	case []interface{}:
		if len(val) > 0 {
			j.Collapsed[path] = true
			for i, item := range val {
				j.collapseRecursive(item, fmt.Sprintf("%s[%d]", path, i))
			}
		}
	case map[string]interface{}:
		if len(val) > 0 {
			j.Collapsed[path] = true
			for k, item := range val {
				j.collapseRecursive(item, fmt.Sprintf("%s.%s", path, k))
			}
		}
	}
}

// FormatJSONCompact returns a compact JSON string
func FormatJSONCompact(v interface{}) string {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(data)
}

// FormatJSONPretty returns a pretty-printed JSON string
func FormatJSONPretty(v interface{}) string {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(data)
}

