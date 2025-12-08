package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// DataTable component for displaying tabular data
type DataTable struct {
	Headers       []string
	Rows          [][]string
	SelectedRow   int
	SelectedCol   int
	Offset        int
	HorizontalOff int // Horizontal scroll offset (column index)
	Width         int
	Height        int
	ColWidths     []int
	ShowRowNums   bool
	FocusEnabled  bool
}

// NewDataTable creates a new DataTable
func NewDataTable() DataTable {
	return DataTable{
		Headers:      []string{},
		Rows:         [][]string{},
		SelectedRow:  0,
		SelectedCol:  0,
		Offset:       0,
		ColWidths:    []int{},
		ShowRowNums:  true,
		FocusEnabled: true,
	}
}

// SetSize sets the table dimensions
func (t *DataTable) SetSize(width, height int) {
	t.Width = width
	t.Height = height
}

// SetData sets the table data
func (t *DataTable) SetData(headers []string, rows [][]string) {
	t.Headers = headers
	t.Rows = rows
	t.SelectedRow = 0
	t.SelectedCol = 0
	t.Offset = 0
	t.HorizontalOff = 0
	t.calculateColWidths()
}

// calculateColWidths calculates optimal column widths
func (t *DataTable) calculateColWidths() {
	if len(t.Headers) == 0 {
		return
	}

	t.ColWidths = make([]int, len(t.Headers))

	// Start with header widths
	for i, h := range t.Headers {
		t.ColWidths[i] = len(h)
	}

	// Check row values
	for _, row := range t.Rows {
		for i, cell := range row {
			if i < len(t.ColWidths) && len(cell) > t.ColWidths[i] {
				t.ColWidths[i] = len(cell)
			}
		}
	}

	// Cap widths and distribute available space
	maxColWidth := 40
	totalWidth := 0
	for i := range t.ColWidths {
		if t.ColWidths[i] > maxColWidth {
			t.ColWidths[i] = maxColWidth
		}
		totalWidth += t.ColWidths[i] + 3 // padding + separator
	}
}

// MoveUp moves selection up
func (t *DataTable) MoveUp() {
	if t.SelectedRow > 0 {
		t.SelectedRow--
		if t.SelectedRow < t.Offset {
			t.Offset = t.SelectedRow
		}
	}
}

// MoveDown moves selection down
func (t *DataTable) MoveDown() {
	if t.SelectedRow < len(t.Rows)-1 {
		t.SelectedRow++
		visibleRows := t.Height - 4 // account for headers and borders
		if t.SelectedRow >= t.Offset+visibleRows {
			t.Offset = t.SelectedRow - visibleRows + 1
		}
	}
}

// MoveLeft moves selection left and scrolls if needed
func (t *DataTable) MoveLeft() {
	if t.SelectedCol > 0 {
		t.SelectedCol--
		// Scroll left immediately when selected column goes before visible area
		if t.SelectedCol < t.HorizontalOff {
			t.HorizontalOff = t.SelectedCol
		}
	}
}

// MoveRight moves selection right and scrolls if needed
func (t *DataTable) MoveRight() {
	if t.SelectedCol < len(t.Headers)-1 {
		t.SelectedCol++
		// Scroll right immediately - move view with selection
		maxVisible := 4 // Show max 4 columns at a time for responsiveness
		if t.SelectedCol >= t.HorizontalOff+maxVisible {
			t.HorizontalOff = t.SelectedCol - maxVisible + 1
		}
	}
}

// GetSelectedRow returns the currently selected row
func (t *DataTable) GetSelectedRow() []string {
	if t.SelectedRow >= 0 && t.SelectedRow < len(t.Rows) {
		return t.Rows[t.SelectedRow]
	}
	return nil
}

// View renders the table
func (t *DataTable) View() string {
	if len(t.Headers) == 0 {
		return ContentStyle.Render("No data to display")
	}

	var b strings.Builder

	// Fixed width for row number column
	const rowNumWidth = 6

	// Ensure HorizontalOff is valid
	if t.HorizontalOff < 0 {
		t.HorizontalOff = 0
	}
	if t.HorizontalOff >= len(t.Headers) {
		t.HorizontalOff = len(t.Headers) - 1
	}

	// Ensure selected column is visible - this is the key fix!
	if t.SelectedCol < t.HorizontalOff {
		t.HorizontalOff = t.SelectedCol
	}

	// Use selected column as the starting point for visibility
	startCol := t.HorizontalOff
	
	// Calculate how many columns we can show
	availableWidth := t.Width - 15
	if t.ShowRowNums {
		availableWidth -= rowNumWidth
	}

	// Count columns that fit
	endCol := startCol
	usedWidth := 0

	for i := startCol; i < len(t.Headers) && i < len(t.ColWidths); i++ {
		colWidth := t.ColWidths[i] + 3
		if usedWidth+colWidth > availableWidth && i > startCol {
			break
		}
		usedWidth += colWidth
		endCol = i + 1
	}
	
	// Make sure selected column is in visible range
	if t.SelectedCol >= endCol && endCol < len(t.Headers) {
		// Shift view to show selected column
		startCol = t.SelectedCol
		endCol = startCol + 1
		t.HorizontalOff = startCol
	}

	// Render header
	var headerCells []string
	if t.ShowRowNums {
		headerCells = append(headerCells, TableHeaderStyle.Width(rowNumWidth).Render("#"))
	}
	
	// Show scroll indicator if there are columns to the left
	if startCol > 0 {
		headerCells = append(headerCells, TableHeaderStyle.Width(2).Render("◀"))
	}

	for i := startCol; i < endCol; i++ {
		h := t.Headers[i]
		width := t.ColWidths[i]
		if width > 0 {
			headerCells = append(headerCells, TableHeaderStyle.Width(width+2).Render(Truncate(h, width)))
		}
	}

	// Show scroll indicator if there are columns to the right
	if endCol < len(t.Headers) {
		headerCells = append(headerCells, TableHeaderStyle.Width(2).Render("▶"))
	}

	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, headerCells...))
	b.WriteString("\n")

	// Render rows
	visibleRows := t.Height - 4
	if visibleRows < 1 {
		visibleRows = 10
	}

	endRow := t.Offset + visibleRows
	if endRow > len(t.Rows) {
		endRow = len(t.Rows)
	}

	for rowIdx := t.Offset; rowIdx < endRow; rowIdx++ {
		row := t.Rows[rowIdx]
		var cells []string

		if t.ShowRowNums {
			numStyle := TableCellStyle
			if rowIdx == t.SelectedRow && t.FocusEnabled {
				numStyle = TableCellSelectedStyle
			}
			cells = append(cells, numStyle.Width(rowNumWidth).Render(fmt.Sprintf("%d", rowIdx+1)))
		}

		// Show scroll indicator for left
		if startCol > 0 {
			style := TableCellStyle
			if rowIdx == t.SelectedRow && t.FocusEnabled {
				style = TableCellSelectedStyle
			}
			cells = append(cells, style.Width(2).Render("◀"))
		}

		for colIdx := startCol; colIdx < endCol; colIdx++ {
			cell := ""
			if colIdx < len(row) {
				cell = row[colIdx]
			}
			if colIdx >= len(t.ColWidths) {
				break
			}
			width := t.ColWidths[colIdx]
			style := TableCellStyle
			if t.FocusEnabled && rowIdx == t.SelectedRow {
				if colIdx == t.SelectedCol {
					style = TableCellSelectedStyle.Bold(true)
				} else {
					style = TableCellSelectedStyle
				}
			}
			cells = append(cells, style.Width(width+2).Render(Truncate(cell, width)))
		}

		// Show scroll indicator for right
		if endCol < len(t.Headers) {
			style := TableCellStyle
			if rowIdx == t.SelectedRow && t.FocusEnabled {
				style = TableCellSelectedStyle
			}
			cells = append(cells, style.Width(2).Render("▶"))
		}

		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, cells...))
		b.WriteString("\n")
	}

	// Footer with row count
	footer := fmt.Sprintf("Showing %d-%d of %d rows", t.Offset+1, endRow, len(t.Rows))
	b.WriteString(HelpStyle.Render(footer))

	return b.String()
}

// List component for simple list selection
type List struct {
	Items       []string
	Selected    int
	Offset      int
	Height      int
	Width       int
	Title       string
	ShowNumbers bool
}

// NewList creates a new List
func NewList(title string, items []string) List {
	return List{
		Title:       title,
		Items:       items,
		Selected:    0,
		Offset:      0,
		Height:      20,
		Width:       30,
		ShowNumbers: false,
	}
}

// MoveUp moves selection up
func (l *List) MoveUp() {
	if l.Selected > 0 {
		l.Selected--
		if l.Selected < l.Offset {
			l.Offset = l.Selected
		}
	}
}

// MoveDown moves selection down
func (l *List) MoveDown() {
	if l.Selected < len(l.Items)-1 {
		l.Selected++
		visible := l.Height - 2
		if l.Selected >= l.Offset+visible {
			l.Offset = l.Selected - visible + 1
		}
	}
}

// GetSelected returns the selected item
func (l *List) GetSelected() string {
	if l.Selected >= 0 && l.Selected < len(l.Items) {
		return l.Items[l.Selected]
	}
	return ""
}

// SetItems updates the list items
func (l *List) SetItems(items []string) {
	l.Items = items
	l.Selected = 0
	l.Offset = 0
}

// View renders the list
func (l *List) View() string {
	var b strings.Builder

	if l.Title != "" {
		b.WriteString(TitleStyle.Render(l.Title))
		b.WriteString("\n\n")
	}

	visible := l.Height - 2
	endIdx := l.Offset + visible
	if endIdx > len(l.Items) {
		endIdx = len(l.Items)
	}

	for i := l.Offset; i < endIdx; i++ {
		item := l.Items[i]
		if l.ShowNumbers {
			item = fmt.Sprintf("%d. %s", i+1, item)
		}

		if i == l.Selected {
			b.WriteString(SelectedStyle.Render("▸ " + item))
		} else {
			b.WriteString(ItemStyle.Render("  " + item))
		}
		b.WriteString("\n")
	}

	return SidebarStyle.Width(l.Width).Render(b.String())
}

// Form component for input forms
type Form struct {
	Title       string
	Fields      []FormField
	FocusedIdx  int
	Width       int
	Submitted   bool
	Cancelled   bool
}

// FormField represents a form field
type FormField struct {
	Label       string
	Input       textinput.Model
	Required    bool
	Description string
}

// NewForm creates a new Form
func NewForm(title string) Form {
	return Form{
		Title:  title,
		Fields: []FormField{},
		Width:  60,
	}
}

// AddField adds a field to the form
func (f *Form) AddField(label, placeholder string, required bool) {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.Width = f.Width - 10

	if len(f.Fields) == 0 {
		ti.Focus()
	}

	f.Fields = append(f.Fields, FormField{
		Label:    label,
		Input:    ti,
		Required: required,
	})
}

// AddPasswordField adds a password field
func (f *Form) AddPasswordField(label, placeholder string, required bool) {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.Width = f.Width - 10
	ti.EchoMode = textinput.EchoPassword
	ti.EchoCharacter = '•'

	if len(f.Fields) == 0 {
		ti.Focus()
	}

	f.Fields = append(f.Fields, FormField{
		Label:    label,
		Input:    ti,
		Required: required,
	})
}

// FocusNext moves focus to next field
func (f *Form) FocusNext() {
	if f.FocusedIdx < len(f.Fields)-1 {
		f.Fields[f.FocusedIdx].Input.Blur()
		f.FocusedIdx++
		f.Fields[f.FocusedIdx].Input.Focus()
	}
}

// FocusPrev moves focus to previous field
func (f *Form) FocusPrev() {
	if f.FocusedIdx > 0 {
		f.Fields[f.FocusedIdx].Input.Blur()
		f.FocusedIdx--
		f.Fields[f.FocusedIdx].Input.Focus()
	}
}

// GetValue returns the value of a field by label
func (f *Form) GetValue(label string) string {
	for _, field := range f.Fields {
		if field.Label == label {
			return field.Input.Value()
		}
	}
	return ""
}

// SetValue sets the value of a field by label
func (f *Form) SetValue(label, value string) {
	for i := range f.Fields {
		if f.Fields[i].Label == label {
			f.Fields[i].Input.SetValue(value)
			return
		}
	}
}

// Validate checks if all required fields are filled
func (f *Form) Validate() []string {
	var errors []string
	for _, field := range f.Fields {
		if field.Required && field.Input.Value() == "" {
			errors = append(errors, fmt.Sprintf("%s is required", field.Label))
		}
	}
	return errors
}

// Update handles input updates
func (f *Form) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	if f.FocusedIdx < len(f.Fields) {
		f.Fields[f.FocusedIdx].Input, cmd = f.Fields[f.FocusedIdx].Input.Update(msg)
	}
	return cmd
}

// View renders the form
func (f *Form) View() string {
	var b strings.Builder

	b.WriteString(TitleStyle.Render(f.Title))
	b.WriteString("\n\n")

	for i, field := range f.Fields {
		label := field.Label
		if field.Required {
			label += " " + ErrorStyle.Render("*")
		}
		b.WriteString(label + "\n")

		style := InputStyle
		if i == f.FocusedIdx {
			style = InputFocusedStyle
		}
		b.WriteString(style.Render(field.Input.View()))
		b.WriteString("\n\n")
	}

	// Buttons
	submitBtn := ButtonStyle.Render("Submit")
	cancelBtn := ButtonStyle.Render("Cancel")
	if f.FocusedIdx >= len(f.Fields) {
		submitBtn = ButtonFocusedStyle.Render("Submit")
	}

	b.WriteString("\n")
	b.WriteString(submitBtn + "  " + cancelBtn)

	return ModalStyle.Width(f.Width).Render(b.String())
}

// Tabs component for tab navigation
type Tabs struct {
	Items    []string
	Active   int
}

// NewTabs creates a new Tabs component
func NewTabs(items []string) Tabs {
	return Tabs{
		Items:  items,
		Active: 0,
	}
}

// Next moves to next tab
func (t *Tabs) Next() {
	if t.Active < len(t.Items)-1 {
		t.Active++
	}
}

// Prev moves to previous tab
func (t *Tabs) Prev() {
	if t.Active > 0 {
		t.Active--
	}
}

// View renders the tabs
func (t *Tabs) View() string {
	var tabs []string
	for i, item := range t.Items {
		if i == t.Active {
			tabs = append(tabs, TabActiveStyle.Render(item))
		} else {
			tabs = append(tabs, TabStyle.Render(item))
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, tabs...)
}

// InfoBox displays information in a box
type InfoBox struct {
	Title   string
	Content string
	Width   int
}

// View renders the info box
func (i InfoBox) View() string {
	title := TitleStyle.Render(i.Title)
	return InfoPanelStyle.Width(i.Width).Render(title + "\n\n" + i.Content)
}

// StatusBar component
type StatusBar struct {
	Left   string
	Center string
	Right  string
	Width  int
}

// View renders the status bar
func (s StatusBar) View() string {
	leftStyle := StatusBarStyle.Width(s.Width / 3)
	centerStyle := StatusBarStyle.Width(s.Width / 3).Align(lipgloss.Center)
	rightStyle := StatusBarStyle.Width(s.Width / 3).Align(lipgloss.Right)

	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		leftStyle.Render(s.Left),
		centerStyle.Render(s.Center),
		rightStyle.Render(s.Right),
	)
}

