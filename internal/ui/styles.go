package ui

import (
	"github.com/charmbracelet/lipgloss"
)

// Theme colors - Cyberpunk/Neon aesthetic
var (
	// Primary colors
	ColorPrimary    = lipgloss.Color("#00FFFF") // Cyan
	ColorSecondary  = lipgloss.Color("#FF00FF") // Magenta
	ColorAccent     = lipgloss.Color("#FFFF00") // Yellow
	ColorSuccess    = lipgloss.Color("#00FF00") // Green
	ColorError      = lipgloss.Color("#FF0055") // Hot Pink
	ColorWarning    = lipgloss.Color("#FF9900") // Orange

	// Background colors
	ColorBg        = lipgloss.Color("#0D0D1A") // Deep dark blue
	ColorBgLight   = lipgloss.Color("#1A1A2E") // Slightly lighter
	ColorBgHighlight = lipgloss.Color("#16213E") // Highlight bg

	// Text colors
	ColorText       = lipgloss.Color("#E0E0E0") // Light gray
	ColorTextMuted  = lipgloss.Color("#6B7280") // Muted gray
	ColorTextBright = lipgloss.Color("#FFFFFF") // White
)

// Styles
var (
	// App container
	AppStyle = lipgloss.NewStyle().
		Background(ColorBg)

	// Title bar
	TitleStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary).
		Background(ColorBgLight).
		Padding(0, 2).
		MarginBottom(1)

	// Logo/Brand
	LogoStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorSecondary).
		Background(ColorBgLight).
		Padding(1, 4).
		Border(lipgloss.DoubleBorder()).
		BorderForeground(ColorPrimary)

	// Sidebar
	SidebarStyle = lipgloss.NewStyle().
		Width(30).
		Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Background(ColorBgLight)

	// Main content area
	ContentStyle = lipgloss.NewStyle().
		Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorSecondary)

	// Selected item
	SelectedStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorBg).
		Background(ColorPrimary).
		Padding(0, 1)

	// Normal list item
	ItemStyle = lipgloss.NewStyle().
		Foreground(ColorText).
		Padding(0, 1)

	// Table header
	TableHeaderStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorSecondary).
		Background(ColorBgLight).
		Padding(0, 1).
		Border(lipgloss.NormalBorder(), false, false, true, false).
		BorderForeground(ColorPrimary)

	// Table cell
	TableCellStyle = lipgloss.NewStyle().
		Foreground(ColorText).
		Padding(0, 1)

	// Table cell selected
	TableCellSelectedStyle = lipgloss.NewStyle().
		Foreground(ColorBg).
		Background(ColorPrimary).
		Padding(0, 1)

	// Status bar
	StatusBarStyle = lipgloss.NewStyle().
		Foreground(ColorText).
		Background(ColorBgLight).
		Padding(0, 2)

	// Help text
	HelpStyle = lipgloss.NewStyle().
		Foreground(ColorTextMuted).
		Italic(true)

	// Key binding
	KeyStyle = lipgloss.NewStyle().
		Foreground(ColorAccent).
		Bold(true)

	// Description
	DescStyle = lipgloss.NewStyle().
		Foreground(ColorTextMuted)

	// Error message
	ErrorStyle = lipgloss.NewStyle().
		Foreground(ColorError).
		Bold(true).
		Padding(0, 1)

	// Success message
	SuccessStyle = lipgloss.NewStyle().
		Foreground(ColorSuccess).
		Bold(true).
		Padding(0, 1)

	// Warning message
	WarningStyle = lipgloss.NewStyle().
		Foreground(ColorWarning).
		Bold(true).
		Padding(0, 1)

	// Info panel
	InfoPanelStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorAccent).
		Padding(1, 2).
		MarginTop(1)

	// Input field
	InputStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(0, 1)

	// Focused input
	InputFocusedStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorSecondary).
		Padding(0, 1)

	// Button
	ButtonStyle = lipgloss.NewStyle().
		Foreground(ColorText).
		Background(ColorBgLight).
		Padding(0, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorTextMuted)

	// Button focused
	ButtonFocusedStyle = lipgloss.NewStyle().
		Foreground(ColorBg).
		Background(ColorPrimary).
		Bold(true).
		Padding(0, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary)

	// Badge/Tag
	BadgeStyle = lipgloss.NewStyle().
		Foreground(ColorBg).
		Background(ColorSecondary).
		Padding(0, 1).
		Bold(true)

	// Type indicator
	TypeStyle = lipgloss.NewStyle().
		Foreground(ColorAccent).
		Bold(true)

	// Modal
	ModalStyle = lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(ColorPrimary).
		Background(ColorBgLight).
		Padding(2, 4)

	// Divider
	DividerStyle = lipgloss.NewStyle().
		Foreground(ColorTextMuted)

	// Tab inactive
	TabStyle = lipgloss.NewStyle().
		Foreground(ColorTextMuted).
		Padding(0, 2).
		Border(lipgloss.RoundedBorder(), true, true, false, true).
		BorderForeground(ColorTextMuted)

	// Tab active
	TabActiveStyle = lipgloss.NewStyle().
		Foreground(ColorPrimary).
		Bold(true).
		Padding(0, 2).
		Border(lipgloss.RoundedBorder(), true, true, false, true).
		BorderForeground(ColorPrimary)

	// JSON Key
	JSONKeyStyle = lipgloss.NewStyle().
		Foreground(ColorSecondary)

	// JSON String
	JSONStringStyle = lipgloss.NewStyle().
		Foreground(ColorSuccess)

	// JSON Number
	JSONNumberStyle = lipgloss.NewStyle().
		Foreground(ColorAccent)

	// JSON Boolean
	JSONBoolStyle = lipgloss.NewStyle().
		Foreground(ColorPrimary)

	// JSON Null
	JSONNullStyle = lipgloss.NewStyle().
		Foreground(ColorTextMuted).
		Italic(true)
)

// RenderHelp renders a help line with key bindings
func RenderHelp(bindings []KeyBinding) string {
	var result string
	for i, b := range bindings {
		if i > 0 {
			result += DividerStyle.Render(" │ ")
		}
		result += KeyStyle.Render(b.Key) + " " + DescStyle.Render(b.Desc)
	}
	return result
}

// KeyBinding represents a key binding for help display
type KeyBinding struct {
	Key  string
	Desc string
}

// Truncate truncates a string to a maximum length
func Truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// PadRight pads a string on the right to a specific width
func PadRight(s string, width int) string {
	if len(s) >= width {
		return s[:width]
	}
	return s + lipgloss.NewStyle().Width(width-len(s)).Render("")
}








