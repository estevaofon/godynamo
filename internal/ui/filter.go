package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/godynamo/internal/query"
)

// FilterOperator represents a filter comparison operator
type FilterOperator int

const (
	OpEquals FilterOperator = iota
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

// FilterOperators is the list of all available operators
var FilterOperators = []struct {
	Op    FilterOperator
	Label string
	Sym   string
}{
	{OpEquals, "Equals", "="},
	{OpNotEquals, "Not Equals", "≠"},
	{OpGreaterThan, "Greater Than", ">"},
	{OpLessThan, "Less Than", "<"},
	{OpGreaterOrEqual, "Greater or Equal", "≥"},
	{OpLessOrEqual, "Less or Equal", "≤"},
	{OpContains, "Contains", "∋"},
	{OpNotContains, "Not Contains", "∌"},
	{OpBeginsWith, "Begins With", "^"},
	{OpExists, "Exists", "∃"},
	{OpNotExists, "Not Exists", "∄"},
}

// FilterCondition represents a single filter condition
type FilterCondition struct {
	AttributeName  textinput.Model
	Operator       FilterOperator
	AttributeValue textinput.Model
}

// FilterBuilder is a visual filter builder component
type FilterBuilder struct {
	Conditions    []FilterCondition
	ActiveCondIdx int
	ActiveField   int // 0=name, 1=operator, 2=value
	OperatorOpen  bool
	Width         int
	Height        int
}

// NewFilterBuilder creates a new FilterBuilder
func NewFilterBuilder() FilterBuilder {
	fb := FilterBuilder{
		Conditions:    []FilterCondition{},
		ActiveCondIdx: 0,
		ActiveField:   0,
		Width:         120,
		Height:        20,
	}
	fb.AddCondition()
	return fb
}

// SetWidth sets the width of the filter builder
func (f *FilterBuilder) SetWidth(width int) {
	f.Width = width
}

// AddCondition adds a new filter condition
func (f *FilterBuilder) AddCondition() {
	nameInput := textinput.New()
	nameInput.Placeholder = "attribute"
	nameInput.Width = 22
	nameInput.Prompt = ""
	nameInput.CharLimit = 50

	valueInput := textinput.New()
	valueInput.Placeholder = "value"
	valueInput.Width = 26
	valueInput.Prompt = ""
	valueInput.CharLimit = 100

	if len(f.Conditions) == 0 {
		nameInput.Focus()
	}

	f.Conditions = append(f.Conditions, FilterCondition{
		AttributeName:  nameInput,
		Operator:       OpEquals,
		AttributeValue: valueInput,
	})
}

// RemoveCondition removes the current condition
func (f *FilterBuilder) RemoveCondition() {
	if len(f.Conditions) > 1 {
		f.Conditions = append(f.Conditions[:f.ActiveCondIdx], f.Conditions[f.ActiveCondIdx+1:]...)
		if f.ActiveCondIdx >= len(f.Conditions) {
			f.ActiveCondIdx = len(f.Conditions) - 1
		}
		f.updateFocus()
	}
}

// Clear removes all conditions and adds a fresh one
func (f *FilterBuilder) Clear() {
	f.Conditions = []FilterCondition{}
	f.ActiveCondIdx = 0
	f.ActiveField = 0
	f.AddCondition()
}

func (f *FilterBuilder) updateFocus() {
	for i := range f.Conditions {
		f.Conditions[i].AttributeName.Blur()
		f.Conditions[i].AttributeValue.Blur()
	}

	if f.ActiveCondIdx < len(f.Conditions) {
		switch f.ActiveField {
		case 0:
			f.Conditions[f.ActiveCondIdx].AttributeName.Focus()
		case 2:
			f.Conditions[f.ActiveCondIdx].AttributeValue.Focus()
		}
	}
}

// NextField moves to the next field
func (f *FilterBuilder) NextField() {
	op := f.Conditions[f.ActiveCondIdx].Operator
	needsValue := op != OpExists && op != OpNotExists

	if f.ActiveField == 0 {
		f.ActiveField = 1
		f.OperatorOpen = true
	} else if f.ActiveField == 1 {
		f.OperatorOpen = false
		if needsValue {
			f.ActiveField = 2
		} else {
			// Move to next condition or stay
			if f.ActiveCondIdx < len(f.Conditions)-1 {
				f.ActiveCondIdx++
				f.ActiveField = 0
			}
		}
	} else if f.ActiveField == 2 {
		if f.ActiveCondIdx < len(f.Conditions)-1 {
			f.ActiveCondIdx++
			f.ActiveField = 0
		}
	}
	f.updateFocus()
}

// PrevField moves to the previous field
func (f *FilterBuilder) PrevField() {
	if f.ActiveField == 2 {
		f.ActiveField = 1
		f.OperatorOpen = true
	} else if f.ActiveField == 1 {
		f.OperatorOpen = false
		f.ActiveField = 0
	} else if f.ActiveField == 0 && f.ActiveCondIdx > 0 {
		f.ActiveCondIdx--
		op := f.Conditions[f.ActiveCondIdx].Operator
		if op == OpExists || op == OpNotExists {
			f.ActiveField = 1
		} else {
			f.ActiveField = 2
		}
	}
	f.updateFocus()
}

// NextOperator selects the next operator
func (f *FilterBuilder) NextOperator() {
	if f.ActiveField == 1 {
		current := int(f.Conditions[f.ActiveCondIdx].Operator)
		next := (current + 1) % len(FilterOperators)
		f.Conditions[f.ActiveCondIdx].Operator = FilterOperator(next)
	}
}

// PrevOperator selects the previous operator
func (f *FilterBuilder) PrevOperator() {
	if f.ActiveField == 1 {
		current := int(f.Conditions[f.ActiveCondIdx].Operator)
		prev := current - 1
		if prev < 0 {
			prev = len(FilterOperators) - 1
		}
		f.Conditions[f.ActiveCondIdx].Operator = FilterOperator(prev)
	}
}

// NextCondition moves to the next condition row
func (f *FilterBuilder) NextCondition() {
	if f.ActiveCondIdx < len(f.Conditions)-1 {
		f.ActiveCondIdx++
		f.ActiveField = 0
		f.OperatorOpen = false
		f.updateFocus()
	}
}

// PrevCondition moves to the previous condition row
func (f *FilterBuilder) PrevCondition() {
	if f.ActiveCondIdx > 0 {
		f.ActiveCondIdx--
		f.ActiveField = 0
		f.OperatorOpen = false
		f.updateFocus()
	}
}

// Update handles input - accepts tea.Msg to support unicode characters
func (f *FilterBuilder) Update(msg tea.Msg) tea.Cmd {
	if f.ActiveCondIdx >= len(f.Conditions) {
		return nil
	}

	var cmd tea.Cmd

	// Only update text inputs when they are focused (field 0 or 2)
	switch f.ActiveField {
	case 0:
		f.Conditions[f.ActiveCondIdx].AttributeName, cmd = f.Conditions[f.ActiveCondIdx].AttributeName.Update(msg)
	case 2:
		f.Conditions[f.ActiveCondIdx].AttributeValue, cmd = f.Conditions[f.ActiveCondIdx].AttributeValue.Update(msg)
	}

	return cmd
}

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

// View renders the filter builder
func (f *FilterBuilder) View() string {
	var b strings.Builder

	// Title
	b.WriteString(TitleStyle.Render("🔍 Filter Builder"))
	b.WriteString("\n\n")

	// Instructions
	b.WriteString(HelpStyle.Render("Tab/Shift+Tab: Navigate │ ↑↓: Operator │ Ctrl+A: Add │ Ctrl+D: Remove │ Enter: Apply"))
	b.WriteString("\n\n")

	// Labels row
	b.WriteString("   ")
	b.WriteString(lipgloss.NewStyle().Foreground(ColorTextMuted).Width(26).Render("Attribute Name"))
	b.WriteString(lipgloss.NewStyle().Foreground(ColorTextMuted).Width(20).Render("Operator"))
	b.WriteString(lipgloss.NewStyle().Foreground(ColorTextMuted).Width(30).Render("Value"))
	b.WriteString("\n")

	// Conditions
	for i, cond := range f.Conditions {
		isActive := i == f.ActiveCondIdx

		// Row number
		rowNum := fmt.Sprintf("%d.", i+1)
		if isActive {
			b.WriteString(lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true).Width(3).Render(rowNum))
		} else {
			b.WriteString(lipgloss.NewStyle().Foreground(ColorTextMuted).Width(3).Render(rowNum))
		}

		// Attribute Name
		nameContent := cond.AttributeName.View()
		nameStyle := lipgloss.NewStyle().Width(25)
		if isActive && f.ActiveField == 0 {
			nameStyle = nameStyle.Foreground(ColorPrimary)
		}
		b.WriteString(nameStyle.Render(nameContent))
		b.WriteString(" ")

		// Operator
		opInfo := FilterOperators[cond.Operator]
		opLabel := fmt.Sprintf("%s %-14s", opInfo.Sym, opInfo.Label)
		if isActive && f.ActiveField == 1 {
			b.WriteString(lipgloss.NewStyle().
				Foreground(ColorBg).
				Background(ColorSecondary).
				Bold(true).
				Padding(0, 1).
				Render(opLabel))
		} else {
			b.WriteString(lipgloss.NewStyle().
				Foreground(ColorSecondary).
				Padding(0, 1).
				Render(opLabel))
		}
		b.WriteString(" ")

		// Attribute Value (only if operator needs it)
		if cond.Operator != OpExists && cond.Operator != OpNotExists {
			valContent := cond.AttributeValue.View()
			valStyle := lipgloss.NewStyle().Width(30)
			if isActive && f.ActiveField == 2 {
				valStyle = valStyle.Foreground(ColorPrimary)
			}
			b.WriteString(valStyle.Render(valContent))
		} else {
			b.WriteString(lipgloss.NewStyle().
				Foreground(ColorTextMuted).
				Italic(true).
				Render("(no value needed)"))
		}

		b.WriteString("\n")

		// Show operator dropdown if active
		if isActive && f.ActiveField == 1 && f.OperatorOpen {
			b.WriteString(f.renderOperatorDropdown(cond.Operator))
		}
	}

	// Preview
	expr, _, _ := f.BuildExpression()
	if expr != "" {
		b.WriteString("\n")
		b.WriteString(HelpStyle.Render("Filter: "))
		b.WriteString(JSONStringStyle.Render(expr))
	}

	return b.String()
}

func (f *FilterBuilder) renderOperatorDropdown(current FilterOperator) string {
	var b strings.Builder
	b.WriteString("    ")

	dropdown := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(0, 1)

	var items []string
	for _, op := range FilterOperators {
		item := fmt.Sprintf("%s %s", op.Sym, op.Label)
		if op.Op == current {
			items = append(items, SelectedStyle.Render("▸ "+item))
		} else {
			items = append(items, ItemStyle.Render("  "+item))
		}
	}

	b.WriteString(dropdown.Render(strings.Join(items, "\n")))
	b.WriteString("\n")
	return b.String()
}

// HasFilters returns true if there are valid filters
func (f *FilterBuilder) HasFilters() bool {
	for _, cond := range f.Conditions {
		if strings.TrimSpace(cond.AttributeName.Value()) != "" {
			return true
		}
	}
	return false
}

// GetFilterSummary returns a short summary of active filters
func (f *FilterBuilder) GetFilterSummary() string {
	var parts []string
	for _, cond := range f.Conditions {
		name := strings.TrimSpace(cond.AttributeName.Value())
		if name == "" {
			continue
		}
		op := FilterOperators[cond.Operator]
		value := strings.TrimSpace(cond.AttributeValue.Value())

		if cond.Operator == OpExists || cond.Operator == OpNotExists {
			parts = append(parts, fmt.Sprintf("%s %s", name, op.Label))
		} else if value != "" {
			parts = append(parts, fmt.Sprintf("%s %s %s", name, op.Sym, value))
		}
	}

	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " AND ")
}
