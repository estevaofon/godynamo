package app

import "github.com/charmbracelet/bubbles/textinput"

func (m *Model) initSearchInput() {
	ti := textinput.New()
	ti.Placeholder = "Search in item..."
	ti.CharLimit = 156
	ti.Width = 30
	m.searchInput = ti
}
