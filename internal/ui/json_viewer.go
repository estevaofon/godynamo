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

	// Search state
	SearchQuery  string
	TotalMatches int
	CurrentMatch int // 0-indexed
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
	j.TotalMatches = 0
	return j.renderValue(j.Data, 0, "root")
}

func (j *JSONViewer) renderValue(v interface{}, indent int, path string) string {
	indentStr := strings.Repeat(" ", indent)

	strVal := ""
	var result string

	switch val := v.(type) {
	case nil:
		strVal = "null"
		result = JSONNullStyle.Render(j.highlightText(strVal))

	case bool:
		strVal = fmt.Sprintf("%v", val)
		result = JSONBoolStyle.Render(j.highlightText(strVal))

	case float64:
		// Check if it's an integer
		if val == float64(int64(val)) {
			strVal = fmt.Sprintf("%.0f", val)
		} else {
			strVal = fmt.Sprintf("%v", val)
		}
		result = JSONNumberStyle.Render(j.highlightText(strVal))

	case int64:
		strVal = fmt.Sprintf("%d", val)
		result = JSONNumberStyle.Render(j.highlightText(strVal))

	case int:
		strVal = fmt.Sprintf("%d", val)
		result = JSONNumberStyle.Render(j.highlightText(strVal))

	case string:
		// For strings, we need to handle highlighting within the quotes
		// First, get the JSON escaped string including quotes
		escaped, _ := json.Marshal(val)
		strEscaped := string(escaped)

		// If we're searching, we might need to highlight inside the string
		if j.SearchQuery != "" && strings.Contains(strings.ToLower(val), strings.ToLower(j.SearchQuery)) {
			// This is complex because we want to highlight the unescaped content but render escaped
			// For simplicity in TUI, we'll highlight the search term in the escaped string if found
			// A better approach would be to highlight segments, but lipgloss styles the whole block
			// So we'll use a helper to style parts of the string
			result = JSONStringStyle.Render(j.highlightText(strEscaped))
		} else {
			result = JSONStringStyle.Render(strEscaped)
		}

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

			// Highlight key if it matches
			keyStr := fmt.Sprintf("\"%s\"", k)
			if j.SearchQuery != "" {
				b.WriteString(JSONKeyStyle.Render(j.highlightText(keyStr)))
			} else {
				b.WriteString(JSONKeyStyle.Render(keyStr))
			}

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

	return result
}

func (j *JSONViewer) highlightText(text string) string {
	if j.SearchQuery == "" {
		return text
	}

	lowerText := strings.ToLower(text)
	lowerQuery := strings.ToLower(j.SearchQuery)

	if !strings.Contains(lowerText, lowerQuery) {
		return text
	}

	// Simple case: split by query and join with styled query
	// Note: this doesn't preserve case of the query in the original text if we just join with SearchQuery
	// We need to find indices to preserve original casing

	var sb strings.Builder
	currentIndex := 0

	for {
		idx := strings.Index(lowerText[currentIndex:], lowerQuery)
		if idx == -1 {
			sb.WriteString(text[currentIndex:])
			break
		}

		absoluteIdx := currentIndex + idx
		sb.WriteString(text[currentIndex:absoluteIdx])

		// This is a match
		matchContent := text[absoluteIdx : absoluteIdx+len(lowerQuery)]

		// Determine style based on if this is the current active match
		style := SearchHighlightStyle
		if j.TotalMatches == j.CurrentMatch {
			style = SearchActiveHighlightStyle
		}

		sb.WriteString(style.Render(matchContent))
		j.TotalMatches++

		currentIndex = absoluteIdx + len(lowerQuery)
	}

	return sb.String()
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
