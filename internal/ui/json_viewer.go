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
	CurrentMatch int   // 0-indexed
	MatchLines   []int // Line number for each match

	// Internal render state
	currentLine int
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
	j.MatchLines = make([]int, 0)
	j.currentLine = 0

	var sb strings.Builder
	j.renderNode(&sb, j.Data, 0, "root")
	return sb.String()
}

func (j *JSONViewer) write(sb *strings.Builder, s string) {
	sb.WriteString(s)
	// Update current line count
	j.currentLine += strings.Count(s, "\n")
}

func (j *JSONViewer) renderNode(sb *strings.Builder, v interface{}, indent int, path string) {
	indentStr := strings.Repeat(" ", indent)

	strVal := ""

	switch val := v.(type) {
	case nil:
		strVal = "null"
		j.write(sb, JSONNullStyle.Render(j.highlightText(strVal)))

	case bool:
		strVal = fmt.Sprintf("%v", val)
		j.write(sb, JSONBoolStyle.Render(j.highlightText(strVal)))

	case float64:
		// Check if it's an integer
		if val == float64(int64(val)) {
			strVal = fmt.Sprintf("%.0f", val)
		} else {
			strVal = fmt.Sprintf("%v", val)
		}
		j.write(sb, JSONNumberStyle.Render(j.highlightText(strVal)))

	case int64:
		strVal = fmt.Sprintf("%d", val)
		j.write(sb, JSONNumberStyle.Render(j.highlightText(strVal)))

	case int:
		strVal = fmt.Sprintf("%d", val)
		j.write(sb, JSONNumberStyle.Render(j.highlightText(strVal)))

	case string:
		// For strings, we need to handle highlighting within the quotes
		escaped, _ := json.Marshal(val)
		strEscaped := string(escaped)

		// If we're searching, we might need to highlight inside the string
		if j.SearchQuery != "" && strings.Contains(strings.ToLower(val), strings.ToLower(j.SearchQuery)) {
			j.write(sb, JSONStringStyle.Render(j.highlightText(strEscaped)))
		} else {
			j.write(sb, JSONStringStyle.Render(strEscaped))
		}

	case []interface{}:
		if len(val) == 0 {
			j.write(sb, "[]")
			return
		}

		if j.Collapsed[path] {
			j.write(sb, fmt.Sprintf("[...] %s", HelpStyle.Render(fmt.Sprintf("(%d items)", len(val)))))
			return
		}

		j.write(sb, "[\n")
		for i, item := range val {
			itemPath := fmt.Sprintf("%s[%d]", path, i)
			j.write(sb, indentStr)
			j.write(sb, strings.Repeat(" ", j.Indent))
			j.renderNode(sb, item, indent+j.Indent, itemPath)
			if i < len(val)-1 {
				j.write(sb, ",")
			}
			j.write(sb, "\n")
		}
		j.write(sb, indentStr)
		j.write(sb, "]")

	case map[string]interface{}:
		if len(val) == 0 {
			j.write(sb, "{}")
			return
		}

		if j.Collapsed[path] {
			j.write(sb, fmt.Sprintf("{...} %s", HelpStyle.Render(fmt.Sprintf("(%d keys)", len(val)))))
			return
		}

		// Sort keys for consistent output
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		j.write(sb, "{\n")
		for i, k := range keys {
			keyPath := fmt.Sprintf("%s.%s", path, k)
			j.write(sb, indentStr)
			j.write(sb, strings.Repeat(" ", j.Indent))

			// Highlight key if it matches
			keyStr := fmt.Sprintf("\"%s\"", k)
			if j.SearchQuery != "" {
				j.write(sb, JSONKeyStyle.Render(j.highlightText(keyStr)))
			} else {
				j.write(sb, JSONKeyStyle.Render(keyStr))
			}

			j.write(sb, ": ")
			j.renderNode(sb, val[k], indent+j.Indent, keyPath)
			if i < len(keys)-1 {
				j.write(sb, ",")
			}
			j.write(sb, "\n")
		}
		j.write(sb, indentStr)
		j.write(sb, "}")

	default:
		j.write(sb, fmt.Sprintf("%v", val))
	}
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

		// Record the line number for this match
		j.MatchLines = append(j.MatchLines, j.currentLine)

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
