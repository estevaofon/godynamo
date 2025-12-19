package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/godynamo/internal/dynamo"
	"github.com/godynamo/internal/models"
	"github.com/godynamo/internal/ui"
)

// Messages
type (
	errMsg            struct{ err error }
	tablesLoadedMsg   struct{ tables []string }
	tableInfoMsg      struct{ info *dynamo.TableInfo }
	scanResultMsg     struct{ result *dynamo.ScanResult }
	queryResultMsg    struct{ result *dynamo.QueryResult }
	continuousScanMsg struct {
		result       *dynamo.ContinuousScanResult
		totalScanned int64
	}
	scanProgressMsg struct {
		itemsFound   int
		totalScanned int64
	}
	itemSavedMsg      struct{}
	itemDeletedMsg    struct{}
	tableCreatedMsg   struct{}
	connectionTestMsg struct {
		success bool
		err     error
		client  *dynamo.Client
		region  string
	}
	regionsDiscoveredMsg struct{ regions []dynamo.RegionInfo }
)

// View modes
type viewMode int

const (
	viewConnect viewMode = iota
	viewSelectRegion
	viewTables
	viewTableData
	viewItemDetail
	viewCreateItem
	viewEditItem
	viewCreateTable
	viewQuery
	viewConfirmDelete
	viewConfirmSave
	viewConfirmContinueScan
	viewExport
	viewSchema
)

// Focus areas
type focusArea int

const (
	focusSidebar focusArea = iota
	focusContent
	focusModal
)

// Model is the main application model
type Model struct {
	// DynamoDB client
	client *dynamo.Client

	// Connection settings
	connections []models.Connection

	// Current state
	view      viewMode
	focus     focusArea
	err       error
	statusMsg string
	loading   bool

	// Region discovery
	discoveredRegions  []dynamo.RegionInfo
	regionList         ui.List
	selectedRegion     string
	selectedRegionIdx  int
	regionDropdownOpen bool

	// Window dimensions
	width  int
	height int

	// Tables
	tables          []string
	filteredTables  []string
	tableFilter     string
	tableFilterMode bool
	tableList       ui.List
	currentTable    string
	tableInfo       *dynamo.TableInfo

	// Data view
	dataTable ui.DataTable
	items     []map[string]types.AttributeValue
	lastKey   map[string]types.AttributeValue
	pageSize  int32

	// Item view
	selectedItem map[string]types.AttributeValue
	jsonViewer   *ui.JSONViewer
	itemViewport viewport.Model

	// Query/Filter
	filterBuilder ui.FilterBuilder
	queryMode     string // "scan" or "query"
	filterExpr    string
	filterNames   map[string]string
	filterValues  map[string]interface{}

	// Continuous scan state
	scanCancel       context.CancelFunc
	scanTotalScanned int64
	scanItemsFound   int
	scanLastKey      map[string]types.AttributeValue

	// Create/Edit item
	itemEditor textarea.Model

	// Item Search
	searchInput textinput.Model
	searchMode  bool

	// Create table form
	createTableForm createTableForm

	// Confirm delete
	deleteTarget string

	// Export
	exportFormat string
	exportPath   string
}

type createTableForm struct {
	inputs      []textinput.Model
	focusIndex  int
	billingMode string
	hasSortKey  bool
}

// New creates a new Model
func New() Model {
	m := Model{
		view:      viewConnect,
		focus:     focusSidebar,
		pageSize:  500,
		loading:   true,
		statusMsg: "Connecting to AWS DynamoDB...",
	}

	m.initCreateTableForm()
	m.initFilterBuilder()
	m.initItemEditor()
	m.initSearchInput()

	m.tableList = ui.NewList("Tables", []string{})
	m.tableList.Height = 30

	m.regionList = ui.NewList("Regions with Tables", []string{})
	m.regionList.Height = 20

	m.dataTable = ui.NewDataTable()

	m.itemViewport = viewport.New(80, 20)

	return m
}

func (m *Model) initCreateTableForm() {
	inputs := make([]textinput.Model, 6)

	inputs[0] = textinput.New()
	inputs[0].Placeholder = "Table name"

	inputs[1] = textinput.New()
	inputs[1].Placeholder = "Partition key name (e.g., id)"

	inputs[2] = textinput.New()
	inputs[2].Placeholder = "Partition key type: S, N, or B"
	inputs[2].SetValue("S")

	inputs[3] = textinput.New()
	inputs[3].Placeholder = "Sort key name (optional)"

	inputs[4] = textinput.New()
	inputs[4].Placeholder = "Sort key type: S, N, or B"
	inputs[4].SetValue("S")

	inputs[5] = textinput.New()
	inputs[5].Placeholder = "Read/Write capacity (e.g., 5)"
	inputs[5].SetValue("5")

	m.createTableForm = createTableForm{
		inputs:      inputs,
		billingMode: "PAY_PER_REQUEST",
	}
}

func (m *Model) initFilterBuilder() {
	m.filterBuilder = ui.NewFilterBuilder()
	m.queryMode = "scan"
}

func (m *Model) initItemEditor() {
	ta := textarea.New()
	ta.Placeholder = `{
  "id": "123",
  "name": "Example"
}`
	ta.SetHeight(30)
	ta.SetWidth(100)
	ta.ShowLineNumbers = false // Disabled for clean copy/paste with mouse
	ta.CharLimit = 0           // No limit

	// Use SetPromptFunc to completely remove the prompt character
	ta.SetPromptFunc(0, func(lineIdx int) string {
		return ""
	})

	m.itemEditor = ta
}

// Init initializes the model
func (m Model) Init() tea.Cmd {
	// Start discovering regions immediately
	return m.discoverRegions()
}

func (m *Model) discoverRegions() tea.Cmd {
	return func() tea.Msg {
		regions, err := dynamo.DiscoverRegionsWithTables(context.Background(), false, "")
		if err != nil {
			return errMsg{err}
		}
		return regionsDiscoveredMsg{regions: regions}
	}
}

// Update handles messages
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	// Handle viewQuery separately to support unicode input
	if m.view == viewQuery {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "ctrl+c", "ctrl+q":
				return m, tea.Quit
			}
		}
		return m.updateQuery(msg)
	}

	// Handle item editor views separately to support full textarea functionality (Enter, etc.)
	if m.view == viewCreateItem || m.view == viewEditItem {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "ctrl+c", "ctrl+q":
				return m, tea.Quit
			}
		}
		return m.updateItemEditor(msg)
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.dataTable.SetSize(msg.Width-35, msg.Height-10)
		m.tableList.Height = msg.Height - 10
		m.itemViewport.Width = msg.Width - 40
		m.itemViewport.Height = msg.Height - 15
		// Resize item editor based on window
		m.itemEditor.SetWidth(msg.Width - 20)
		m.itemEditor.SetHeight(msg.Height - 12)
		return m, nil

	case tea.KeyMsg:
		// Global keys
		switch msg.String() {
		case "ctrl+c", "ctrl+q":
			return m, tea.Quit
		}

		// View-specific handling
		switch m.view {
		case viewConnect:
			return m.updateConnect(msg)
		case viewSelectRegion:
			return m.updateSelectRegion(msg)
		case viewTables:
			return m.updateTables(msg)
		case viewTableData:
			return m.updateTableData(msg)
		case viewItemDetail:
			return m.updateItemDetail(msg)
		case viewCreateTable:
			return m.updateCreateTable(msg)
		case viewConfirmDelete:
			return m.updateConfirmDelete(msg)
		case viewConfirmSave:
			return m.updateConfirmSave(msg)
		case viewConfirmContinueScan:
			return m.updateConfirmContinueScan(msg)
		case viewExport:
			return m.updateExport(msg)
		case viewSchema:
			return m.updateSchema(msg)
		}

	case errMsg:
		m.err = msg.err
		m.loading = false
		m.statusMsg = "Error: " + msg.err.Error()
		return m, nil

	case tablesLoadedMsg:
		m.tables = msg.tables
		m.filteredTables = msg.tables
		m.tableFilter = ""
		m.tableFilterMode = false
		m.tableList.SetItems(msg.tables)
		m.loading = false
		m.view = viewTables
		m.statusMsg = fmt.Sprintf("Loaded %d tables", len(msg.tables))
		return m, nil

	case tableInfoMsg:
		m.tableInfo = msg.info
		m.loading = false
		return m, nil

	case scanResultMsg:
		m.handleScanResult(msg.result)
		return m, nil

	case continuousScanMsg:
		m.handleContinuousScanResult(msg.result)
		// If timed out and there's more data, ask to continue
		if msg.result.TimedOut && msg.result.HasMore {
			m.scanLastKey = msg.result.LastEvaluatedKey
			m.scanTotalScanned = msg.result.TotalScanned
			m.scanItemsFound = len(msg.result.Items)
			m.view = viewConfirmContinueScan
		}
		return m, nil

	case queryResultMsg:
		m.handleQueryResult(msg.result)
		return m, nil

	case itemSavedMsg:
		m.statusMsg = "Item saved successfully"
		m.loading = false
		m.view = viewTableData
		return m, m.scanTable()

	case itemDeletedMsg:
		m.statusMsg = "Item deleted successfully"
		m.loading = false
		m.view = viewTableData
		return m, m.scanTable()

	case tableCreatedMsg:
		m.statusMsg = "Table created successfully"
		m.loading = false
		m.view = viewTables
		return m, m.loadTables()

	case connectionTestMsg:
		if msg.success {
			m.client = msg.client
			if msg.region != "" {
				m.selectedRegion = msg.region
			}
			m.loading = true
			m.statusMsg = "Connected! Loading tables..."
			return m, m.loadTables()
		} else {
			m.loading = false
			m.err = msg.err
			m.statusMsg = "Connection failed: " + msg.err.Error()
		}
		return m, nil

	case regionsDiscoveredMsg:
		m.loading = false
		m.discoveredRegions = msg.regions
		if len(msg.regions) == 0 {
			m.statusMsg = "No regions with tables found"
			m.err = fmt.Errorf("no DynamoDB tables found in any region")
			return m, nil
		}
		// Connect to first region and show tables with region dropdown
		m.selectedRegionIdx = 0
		m.selectedRegion = msg.regions[0].Region
		m.statusMsg = fmt.Sprintf("Found %d regions with tables", len(msg.regions))
		return m, m.connectToRegion(msg.regions[0].Region)
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) updateConnect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter", "r":
		// Retry connection
		m.loading = true
		m.err = nil
		m.statusMsg = "Scanning regions..."
		return m, m.discoverRegions()
	}
	return m, nil
}

func (m *Model) updateTables(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle region dropdown
	if m.regionDropdownOpen {
		switch msg.String() {
		case "up", "k":
			if m.selectedRegionIdx > 0 {
				m.selectedRegionIdx--
			}
		case "down", "j":
			if m.selectedRegionIdx < len(m.discoveredRegions)-1 {
				m.selectedRegionIdx++
			}
		case "enter":
			m.regionDropdownOpen = false
			newRegion := m.discoveredRegions[m.selectedRegionIdx].Region
			if newRegion != m.selectedRegion {
				m.selectedRegion = newRegion
				m.loading = true
				m.statusMsg = fmt.Sprintf("Switching to %s...", newRegion)
				return m, m.connectToRegion(newRegion)
			}
		case "esc":
			m.regionDropdownOpen = false
		}
		return m, nil
	}

	// Handle filter mode (fuzzy finder)
	if m.tableFilterMode {
		switch msg.String() {
		case "esc":
			m.tableFilterMode = false
			m.tableFilter = ""
			m.applyTableFilter()
		case "enter":
			m.tableFilterMode = false
			// Select current item
			if m.tableList.Selected >= 0 && m.tableList.Selected < len(m.filteredTables) {
				m.currentTable = m.filteredTables[m.tableList.Selected]
				m.loading = true
				m.view = viewTableData
				return m, tea.Batch(m.describeTable(), m.scanTable())
			}
		case "up":
			m.tableList.MoveUp()
		case "down":
			m.tableList.MoveDown()
		case "backspace":
			if len(m.tableFilter) > 0 {
				m.tableFilter = m.tableFilter[:len(m.tableFilter)-1]
				m.applyTableFilter()
			}
		case "ctrl+u":
			m.tableFilter = ""
			m.applyTableFilter()
		case "ctrl+n":
			m.tableFilterMode = false
			m.view = viewCreateTable
			m.createTableForm.inputs[0].Focus()
			m.createTableForm.focusIndex = 0
		case "ctrl+r":
			m.tableFilterMode = false
			return m, m.loadTables()
		default:
			// Add character to filter
			if len(msg.String()) == 1 {
				m.tableFilter += msg.String()
				m.applyTableFilter()
			}
		}
		return m, nil
	}

	switch msg.String() {
	case "up", "k":
		m.tableList.MoveUp()
	case "down", "j":
		m.tableList.MoveDown()
	case "enter":
		if m.tableList.Selected >= 0 && m.tableList.Selected < len(m.filteredTables) {
			m.currentTable = m.filteredTables[m.tableList.Selected]
			m.loading = true
			m.view = viewTableData
			return m, tea.Batch(m.describeTable(), m.scanTable())
		}
	case "ctrl+n":
		m.view = viewCreateTable
		m.createTableForm.inputs[0].Focus()
		m.createTableForm.focusIndex = 0
	case "ctrl+r":
		return m, m.loadTables()
	case "/":
		// Enter filter mode
		m.tableFilterMode = true
		m.tableFilter = ""
	case "tab":
		// Toggle region dropdown if multiple regions
		if len(m.discoveredRegions) > 1 {
			m.regionDropdownOpen = !m.regionDropdownOpen
		}
	case "q", "esc":
		if m.tableFilter != "" {
			m.tableFilter = ""
			m.applyTableFilter()
		} else {
			m.view = viewConnect
		}
	case "backspace":
		// Clear filter if there's residual text from previous search
		if m.tableFilter != "" {
			m.tableFilter = ""
			m.applyTableFilter()
		}
	default:
		// Quick filter: start typing to filter
		if len(msg.String()) == 1 && msg.String() != " " {
			m.tableFilterMode = true
			m.tableFilter = msg.String()
			m.applyTableFilter()
		}
	}
	return m, nil
}

func (m *Model) applyTableFilter() {
	if m.tableFilter == "" {
		m.filteredTables = m.tables
	} else {
		matches := ui.FuzzyFind(m.tableFilter, m.tables)
		m.filteredTables = make([]string, len(matches))
		for i, match := range matches {
			m.filteredTables[i] = match.Text
		}
	}
	m.tableList.SetItems(m.filteredTables)
	m.tableList.Selected = 0
}

func (m *Model) updateTableData(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		m.dataTable.MoveUp()
	case "down", "j":
		m.dataTable.MoveDown()
	case "left", "h", "[":
		m.dataTable.MoveLeft()
		return m, nil
	case "right", "l", "]":
		m.dataTable.MoveRight()
		return m, nil
	case "H", "{":
		// Fast scroll left - move 3 columns
		for i := 0; i < 3; i++ {
			m.dataTable.MoveLeft()
		}
		return m, nil
	case "L", "}":
		// Fast scroll right - move 3 columns
		for i := 0; i < 3; i++ {
			m.dataTable.MoveRight()
		}
		return m, nil
	case "home", "0", "^":
		// Go to first column
		m.dataTable.SelectedCol = 0
		m.dataTable.HorizontalOff = 0
		return m, nil
	case "end", "$":
		// Go to last column
		if len(m.dataTable.Headers) > 0 {
			m.dataTable.SelectedCol = len(m.dataTable.Headers) - 1
			if m.dataTable.SelectedCol > 3 {
				m.dataTable.HorizontalOff = m.dataTable.SelectedCol - 3
			}
		}
		return m, nil
	case "enter":
		row := m.dataTable.GetSelectedRow()
		if row != nil && m.dataTable.SelectedRow < len(m.items) {
			m.selectedItem = m.items[m.dataTable.SelectedRow]
			m.prepareItemView()
			m.view = viewItemDetail
		}
	case "n":
		m.itemEditor.SetValue("{\n  \n}")
		m.view = viewCreateItem
		m.itemEditor.Focus()
	case "e":
		if m.dataTable.SelectedRow < len(m.items) {
			m.selectedItem = m.items[m.dataTable.SelectedRow]
			jsonStr, _ := models.ItemToJSON(m.selectedItem, true)
			m.itemEditor.SetValue(jsonStr)
			m.view = viewEditItem
			m.itemEditor.Focus()
		}
	case "d":
		if m.dataTable.SelectedRow < len(m.items) {
			m.selectedItem = m.items[m.dataTable.SelectedRow]
			m.view = viewConfirmDelete
		}
	case "y":
		// Copy selected cell value
		row := m.dataTable.GetSelectedRow()
		if row != nil && m.dataTable.SelectedCol < len(row) {
			value := row[m.dataTable.SelectedCol]
			if err := clipboard.WriteAll(value); err == nil {
				m.statusMsg = "✓ Copied cell value to clipboard"
			} else {
				m.statusMsg = "✗ Failed to copy: " + err.Error()
			}
		}
	case "Y":
		// Copy entire row as JSON
		if m.dataTable.SelectedRow < len(m.items) {
			item := m.items[m.dataTable.SelectedRow]
			jsonStr, err := models.ItemToJSON(item, true)
			if err == nil {
				if err := clipboard.WriteAll(jsonStr); err == nil {
					m.statusMsg = "✓ Copied row as JSON to clipboard"
				} else {
					m.statusMsg = "✗ Failed to copy: " + err.Error()
				}
			}
		}
	case "f":
		m.view = viewQuery
		// FilterBuilder auto-focuses on init
	case "s":
		m.prepareSchemaView()
		m.view = viewSchema
	case "x":
		m.view = viewExport
	case "pgdown", "ctrl+d":
		if m.lastKey != nil {
			return m, m.scanTableNext()
		}
	case "r":
		m.lastKey = nil
		return m, m.scanTable()
	case "q", "esc":
		m.view = viewTables
		m.currentTable = ""
		m.items = nil
		m.lastKey = nil
		// Clear filter when leaving table
		m.filterBuilder.Clear()
		m.filterExpr = ""
		m.filterNames = nil
		m.filterValues = nil
	case "+", "=":
		// Increase page size
		if m.pageSize < 1000 {
			m.pageSize += 100
			m.statusMsg = fmt.Sprintf("Page size: %d items", m.pageSize)
		}
	case "-", "_":
		// Decrease page size
		if m.pageSize > 50 {
			m.pageSize -= 100
			if m.pageSize < 50 {
				m.pageSize = 50
			}
			m.statusMsg = fmt.Sprintf("Page size: %d items", m.pageSize)
		}
	case "tab":
		if m.focus == focusSidebar {
			m.focus = focusContent
		} else {
			m.focus = focusSidebar
		}
	}
	return m, nil
}

func (m *Model) updateItemDetail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle search input
	if m.searchMode {
		switch msg.String() {
		case "esc":
			m.searchMode = false
			m.searchInput.SetValue("")
			m.jsonViewer.SearchQuery = ""
			m.updateItemViewContent()
			return m, nil
		case "enter":
			m.searchMode = false
			return m, nil
		case "ctrl+n", "n":
			if m.jsonViewer.TotalMatches > 0 {
				m.jsonViewer.CurrentMatch = (m.jsonViewer.CurrentMatch + 1) % m.jsonViewer.TotalMatches
				m.updateItemViewContent()
			}
			return m, nil
		case "ctrl+p", "N":
			if m.jsonViewer.TotalMatches > 0 {
				m.jsonViewer.CurrentMatch--
				if m.jsonViewer.CurrentMatch < 0 {
					m.jsonViewer.CurrentMatch = m.jsonViewer.TotalMatches - 1
				}
				m.updateItemViewContent()
			}
			return m, nil
		}

		var cmd tea.Cmd
		m.searchInput, cmd = m.searchInput.Update(msg)

		// Update search query
		m.jsonViewer.SearchQuery = m.searchInput.Value()
		// Reset current match when query changes
		m.jsonViewer.CurrentMatch = 0
		m.updateItemViewContent()

		return m, cmd
	}

	switch msg.String() {
	case "q", "esc":
		m.view = viewTableData
	case "/":
		m.searchMode = true
		m.searchInput.Focus()
		m.updateItemViewContent()
		return m, textinput.Blink
	case "n":
		if m.jsonViewer.TotalMatches > 0 {
			m.jsonViewer.CurrentMatch = (m.jsonViewer.CurrentMatch + 1) % m.jsonViewer.TotalMatches
			m.updateItemViewContent()
		}
	case "N":
		if m.jsonViewer.TotalMatches > 0 {
			m.jsonViewer.CurrentMatch--
			if m.jsonViewer.CurrentMatch < 0 {
				m.jsonViewer.CurrentMatch = m.jsonViewer.TotalMatches - 1
			}
			m.updateItemViewContent()
		}
	case "e":
		jsonStr, _ := models.ItemToJSON(m.selectedItem, true)
		m.itemEditor.SetValue(jsonStr)
		m.view = viewEditItem
		m.itemEditor.Focus()
	case "d":
		m.view = viewConfirmDelete
	case "y", "Y":
		// Copy item as JSON
		jsonStr, err := models.ItemToJSON(m.selectedItem, true)
		if err == nil {
			if err := clipboard.WriteAll(jsonStr); err == nil {
				m.statusMsg = "✓ Copied item as JSON to clipboard"
			} else {
				m.statusMsg = "✗ Failed to copy: " + err.Error()
			}
		}
	case "up", "k":
		m.itemViewport.LineUp(1)
	case "down", "j":
		m.itemViewport.LineDown(1)
	case "pgup":
		m.itemViewport.HalfViewUp()
	case "pgdown":
		m.itemViewport.HalfViewDown()
	}
	return m, nil
}

func (m *Model) updateItemViewContent() {
	if m.jsonViewer == nil {
		return
	}
	content := m.jsonViewer.Render()
	m.itemViewport.SetContent(content)
}

func (m *Model) updateItemEditor(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			m.view = viewTableData
			return m, nil
		case "ctrl+s":
			// Validate JSON before showing confirmation
			_, err := models.JSONToItem(m.itemEditor.Value())
			if err != nil {
				m.statusMsg = "Invalid JSON: " + err.Error()
				return m, nil
			}
			m.view = viewConfirmSave
			return m, nil
		}
	}
	// Pass all messages to the textarea (including Enter key for new lines)
	var cmd tea.Cmd
	m.itemEditor, cmd = m.itemEditor.Update(msg)
	return m, cmd
}

func (m *Model) updateCreateTable(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.view = viewTables
	case "tab", "down":
		m.createTableForm.focusIndex++
		if m.createTableForm.focusIndex >= len(m.createTableForm.inputs) {
			m.createTableForm.focusIndex = 0
		}
		m.updateCreateTableFocus()
	case "shift+tab", "up":
		m.createTableForm.focusIndex--
		if m.createTableForm.focusIndex < 0 {
			m.createTableForm.focusIndex = len(m.createTableForm.inputs) - 1
		}
		m.updateCreateTableFocus()
	case "enter":
		return m, m.createTable()
	default:
		var cmd tea.Cmd
		m.createTableForm.inputs[m.createTableForm.focusIndex], cmd = m.createTableForm.inputs[m.createTableForm.focusIndex].Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *Model) updateCreateTableFocus() {
	for i := range m.createTableForm.inputs {
		if i == m.createTableForm.focusIndex {
			m.createTableForm.inputs[i].Focus()
		} else {
			m.createTableForm.inputs[i].Blur()
		}
	}
}

func (m *Model) updateQuery(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			m.view = viewTableData
			return m, nil
		case "enter":
			if m.filterBuilder.ActiveField == 1 {
				// Confirm operator selection
				m.filterBuilder.NextField()
			} else {
				// Execute filter
				expr, names, values := m.filterBuilder.BuildExpression()
				m.filterExpr = expr
				m.filterNames = names
				m.filterValues = values
				m.view = viewTableData
				m.lastKey = nil
				return m, m.scanTable()
			}
			return m, nil
		case "tab":
			m.filterBuilder.NextField()
			return m, nil
		case "shift+tab":
			m.filterBuilder.PrevField()
			return m, nil
		case "up":
			if m.filterBuilder.ActiveField == 1 {
				m.filterBuilder.PrevOperator()
			} else {
				m.filterBuilder.PrevCondition()
			}
			return m, nil
		case "down":
			if m.filterBuilder.ActiveField == 1 {
				m.filterBuilder.NextOperator()
			} else {
				m.filterBuilder.NextCondition()
			}
			return m, nil
		case "ctrl+a":
			m.filterBuilder.AddCondition()
			return m, nil
		case "ctrl+d":
			m.filterBuilder.RemoveCondition()
			return m, nil
		case "ctrl+c":
			m.filterBuilder.Clear()
			m.filterExpr = ""
			m.filterNames = nil
			m.filterValues = nil
			return m, nil
		}
	}

	// Pass all other messages (including unicode runes) to the filter builder
	cmd := m.filterBuilder.Update(msg)
	return m, cmd
}

func (m *Model) updateSelectRegion(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		m.regionList.MoveUp()
	case "down", "j":
		m.regionList.MoveDown()
	case "enter":
		if m.regionList.Selected >= 0 && m.regionList.Selected < len(m.discoveredRegions) {
			region := m.discoveredRegions[m.regionList.Selected].Region
			m.loading = true
			m.statusMsg = fmt.Sprintf("Connecting to %s...", region)
			return m, m.connectToRegion(region)
		}
	case "q", "esc":
		m.view = viewConnect
	}
	return m, nil
}

func (m *Model) updateConfirmDelete(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		return m, m.deleteItem()
	case "n", "N", "esc":
		m.view = viewTableData
	}
	return m, nil
}

func (m *Model) updateConfirmSave(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		return m, m.saveItem()
	case "n", "N", "esc":
		// Go back to editor
		if m.view == viewConfirmSave {
			m.view = viewEditItem
		}
	}
	return m, nil
}

func (m *Model) updateExport(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.view = viewTableData
	case "j":
		m.exportFormat = "json"
		return m, m.exportData()
	case "c":
		m.exportFormat = "csv"
		return m, m.exportData()
	}
	return m, nil
}

// Commands

func (m *Model) connectToRegion(region string) tea.Cmd {
	return func() tea.Msg {
		cfg := dynamo.ConnectionConfig{
			Region:   region,
			UseLocal: false,
		}

		client, err := dynamo.NewClient(cfg)
		if err != nil {
			return connectionTestMsg{success: false, err: err}
		}

		return connectionTestMsg{success: true, client: client, region: region}
	}
}

func (m *Model) loadTables() tea.Cmd {
	return func() tea.Msg {
		tables, err := m.client.ListTables(context.Background())
		if err != nil {
			return errMsg{err}
		}
		sort.Strings(tables)
		return tablesLoadedMsg{tables}
	}
}

func (m *Model) describeTable() tea.Cmd {
	return func() tea.Msg {
		info, err := m.client.DescribeTable(context.Background(), m.currentTable)
		if err != nil {
			return errMsg{err}
		}
		return tableInfoMsg{info}
	}
}

func (m *Model) scanTable() tea.Cmd {
	return func() tea.Msg {
		// Check if we're filtering by partition key or GSI partition key with equals operator - use Query instead
		// DynamoDB only allows = operator for KeyConditionExpression on partition key
		if m.tableInfo != nil && m.filterExpr != "" && m.filterValues != nil {
			// Get the first filter attribute name
			if attrName, ok := m.filterNames["#attr0"]; ok {
				// Check if the first condition uses the equals operator (not <>, contains, etc.)
				// The expression would look like "#attr0 = :val0" for equals
				firstConditionIsEquals := strings.Contains(m.filterExpr, "#attr0 = :") ||
					(strings.Contains(m.filterExpr, "#attr0 =") && !strings.Contains(m.filterExpr, "#attr0 <>"))

				// Only use Query if the first condition is an equals comparison
				if firstConditionIsEquals {
					// Find the placeholder for the first attribute
					var firstPlaceholder string
					for p := range m.filterValues {
						if strings.HasPrefix(p, ":val0") {
							firstPlaceholder = p
							break
						}
					}
					if firstPlaceholder == "" {
						// Fallback to first placeholder found
						for p := range m.filterValues {
							firstPlaceholder = p
							break
						}
					}
					value := m.filterValues[firstPlaceholder]

					// Build additional filter expression for other conditions
					var additionalFilterExpr string
					additionalNames := make(map[string]string)
					additionalValues := make(map[string]interface{})

					// Check if there are multiple conditions
					if strings.Contains(m.filterExpr, " AND ") {
						// Split and get conditions after the first one
						parts := strings.SplitN(m.filterExpr, " AND ", 2)
						if len(parts) > 1 {
							additionalFilterExpr = parts[1]
							// Copy all names and values except the first ones
							for k, v := range m.filterNames {
								if k != "#attr0" {
									additionalNames[k] = v
								}
							}
							for k, v := range m.filterValues {
								if k != firstPlaceholder {
									additionalValues[k] = v
								}
							}
						}
					}

					// Check if it's the table's partition key
					if attrName == m.tableInfo.PartitionKey {
						keyCondition := fmt.Sprintf("#pk = %s", firstPlaceholder)

						queryInput := dynamo.QueryInput{
							TableName:              m.currentTable,
							KeyConditionExpression: keyCondition,
							ExpressionAttributeNames: map[string]string{
								"#pk": m.tableInfo.PartitionKey,
							},
							ExpressionValues: map[string]interface{}{
								firstPlaceholder: value,
							},
							Limit:            m.pageSize,
							ScanIndexForward: true,
						}

						// Add additional filter if present
						if additionalFilterExpr != "" {
							queryInput.FilterExpression = additionalFilterExpr
							for k, v := range additionalNames {
								queryInput.ExpressionAttributeNames[k] = v
							}
							for k, v := range additionalValues {
								queryInput.ExpressionValues[k] = v
							}
						}

						result, err := m.client.QueryTable(context.Background(), queryInput)
						if err != nil {
							return errMsg{err}
						}
						return queryResultMsg{result}
					}

					// Check if it's a GSI partition key
					for _, gsi := range m.tableInfo.GSIs {
						if gsi.PartitionKey == attrName {
							keyCondition := fmt.Sprintf("#pk = %s", firstPlaceholder)

							queryInput := dynamo.QueryInput{
								TableName:              m.currentTable,
								IndexName:              gsi.Name,
								KeyConditionExpression: keyCondition,
								ExpressionAttributeNames: map[string]string{
									"#pk": attrName,
								},
								ExpressionValues: map[string]interface{}{
									firstPlaceholder: value,
								},
								Limit:            m.pageSize,
								ScanIndexForward: true,
							}

							// Add additional filter if present
							if additionalFilterExpr != "" {
								queryInput.FilterExpression = additionalFilterExpr
								for k, v := range additionalNames {
									queryInput.ExpressionAttributeNames[k] = v
								}
								for k, v := range additionalValues {
									queryInput.ExpressionValues[k] = v
								}
							}

							result, err := m.client.QueryTable(context.Background(), queryInput)
							if err != nil {
								return errMsg{err}
							}
							return queryResultMsg{result}
						}
					}
				}
			}
		}

		// If there's a filter, use continuous scan with 3-minute timeout
		if m.filterExpr != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			// Store cancel function for potential cancellation
			m.scanCancel = cancel

			result, err := m.client.ScanTableContinuous(ctx, m.currentTable, int(m.pageSize), nil, m.filterExpr, m.filterNames, m.filterValues)
			cancel() // Clean up context

			if err != nil {
				return errMsg{err}
			}
			return continuousScanMsg{result: result, totalScanned: result.TotalScanned}
		}

		// No filter - use simple scan
		result, err := m.client.ScanTable(context.Background(), m.currentTable, m.pageSize, nil, m.filterExpr, m.filterNames, m.filterValues)
		if err != nil {
			return errMsg{err}
		}
		return scanResultMsg{result}
	}
}

func (m *Model) scanTableNext() tea.Cmd {
	return func() tea.Msg {
		result, err := m.client.ScanTable(context.Background(), m.currentTable, m.pageSize, m.lastKey, m.filterExpr, m.filterNames, m.filterValues)
		if err != nil {
			return errMsg{err}
		}
		return scanResultMsg{result}
	}
}

func (m *Model) handleScanResult(result *dynamo.ScanResult) {
	m.items = result.Items
	m.lastKey = result.LastEvaluatedKey
	m.loading = false
	m.statusMsg = fmt.Sprintf("Loaded %d items (page size: %d)", result.Count, m.pageSize)

	// Convert to table format
	headers, rows := m.itemsToTable(result.Items)
	m.dataTable.SetData(headers, rows)
}

func (m *Model) handleContinuousScanResult(result *dynamo.ContinuousScanResult) {
	m.items = result.Items
	m.lastKey = result.LastEvaluatedKey
	m.loading = false

	statusParts := []string{fmt.Sprintf("Found %d items", len(result.Items))}
	statusParts = append(statusParts, fmt.Sprintf("(scanned %d records)", result.TotalScanned))

	if result.TimedOut {
		statusParts = append(statusParts, "- Timeout reached")
	}
	if result.HasMore {
		statusParts = append(statusParts, "- More data available")
	}

	m.statusMsg = strings.Join(statusParts, " ")

	// Convert to table format
	headers, rows := m.itemsToTable(result.Items)
	m.dataTable.SetData(headers, rows)
}

func (m *Model) handleQueryResult(result *dynamo.QueryResult) {
	m.items = result.Items
	m.lastKey = result.LastEvaluatedKey
	m.loading = false
	m.statusMsg = fmt.Sprintf("Query returned %d items", result.Count)

	headers, rows := m.itemsToTable(result.Items)
	m.dataTable.SetData(headers, rows)
}

func (m *Model) itemsToTable(items []map[string]types.AttributeValue) ([]string, [][]string) {
	if len(items) == 0 {
		return []string{}, [][]string{}
	}

	// Collect all unique keys
	keySet := make(map[string]bool)
	for _, item := range items {
		for k := range item {
			keySet[k] = true
		}
	}

	// Sort keys, but put partition and sort keys first
	var headers []string
	var otherKeys []string

	for k := range keySet {
		if m.tableInfo != nil && (k == m.tableInfo.PartitionKey || k == m.tableInfo.SortKey) {
			continue
		}
		otherKeys = append(otherKeys, k)
	}
	sort.Strings(otherKeys)

	if m.tableInfo != nil {
		headers = append(headers, m.tableInfo.PartitionKey)
		if m.tableInfo.SortKey != "" {
			headers = append(headers, m.tableInfo.SortKey)
		}
	}
	headers = append(headers, otherKeys...)

	// Build rows
	rows := make([][]string, len(items))
	for i, item := range items {
		row := make([]string, len(headers))
		for j, h := range headers {
			if v, ok := item[h]; ok {
				row[j] = models.FormatValue(v, 50)
			} else {
				row[j] = ""
			}
		}
		rows[i] = row
	}

	return headers, rows
}

func (m *Model) prepareItemView() {
	item := models.NewItem(m.selectedItem)
	m.jsonViewer = ui.NewJSONViewer(item.Attributes)
	content := m.jsonViewer.Render()
	m.itemViewport.SetContent(content)
}

func (m *Model) saveItem() tea.Cmd {
	return func() tea.Msg {
		jsonStr := m.itemEditor.Value()
		item, err := models.JSONToItem(jsonStr)
		if err != nil {
			return errMsg{err}
		}

		err = m.client.PutItem(context.Background(), m.currentTable, item)
		if err != nil {
			return errMsg{err}
		}

		return itemSavedMsg{}
	}
}

func (m *Model) deleteItem() tea.Cmd {
	return func() tea.Msg {
		if m.tableInfo == nil {
			return errMsg{fmt.Errorf("table info not loaded")}
		}

		key := make(map[string]types.AttributeValue)
		if v, ok := m.selectedItem[m.tableInfo.PartitionKey]; ok {
			key[m.tableInfo.PartitionKey] = v
		}
		if m.tableInfo.SortKey != "" {
			if v, ok := m.selectedItem[m.tableInfo.SortKey]; ok {
				key[m.tableInfo.SortKey] = v
			}
		}

		err := m.client.DeleteItem(context.Background(), m.currentTable, key)
		if err != nil {
			return errMsg{err}
		}

		return itemDeletedMsg{}
	}
}

func (m *Model) createTable() tea.Cmd {
	return func() tea.Msg {
		input := dynamo.CreateTableInput{
			TableName:     m.createTableForm.inputs[0].Value(),
			PartitionKey:  m.createTableForm.inputs[1].Value(),
			PartitionType: strings.ToUpper(m.createTableForm.inputs[2].Value()),
			SortKey:       m.createTableForm.inputs[3].Value(),
			SortKeyType:   strings.ToUpper(m.createTableForm.inputs[4].Value()),
			BillingMode:   m.createTableForm.billingMode,
		}

		err := m.client.CreateTable(context.Background(), input)
		if err != nil {
			return errMsg{err}
		}

		return tableCreatedMsg{}
	}
}

func (m *Model) exportData() tea.Cmd {
	return func() tea.Msg {
		filename := fmt.Sprintf("%s.%s", m.currentTable, m.exportFormat)

		var data []byte
		var err error

		if m.exportFormat == "json" {
			var items []map[string]interface{}
			for _, item := range m.items {
				converted := make(map[string]interface{})
				for k, v := range item {
					converted[k] = models.AttributeValueToInterface(v)
				}
				items = append(items, converted)
			}
			data, err = json.MarshalIndent(items, "", "  ")
		} else {
			// CSV format
			headers, rows := m.itemsToTable(m.items)
			var b strings.Builder
			b.WriteString(strings.Join(headers, ",") + "\n")
			for _, row := range rows {
				// Escape commas and quotes
				escapedRow := make([]string, len(row))
				for i, cell := range row {
					if strings.ContainsAny(cell, ",\"\n") {
						escapedRow[i] = "\"" + strings.ReplaceAll(cell, "\"", "\"\"") + "\""
					} else {
						escapedRow[i] = cell
					}
				}
				b.WriteString(strings.Join(escapedRow, ",") + "\n")
			}
			data = []byte(b.String())
		}

		if err != nil {
			return errMsg{err}
		}

		// Get current directory
		cwd, _ := os.Getwd()
		filepath := filepath.Join(cwd, filename)

		err = os.WriteFile(filepath, data, 0644)
		if err != nil {
			return errMsg{err}
		}

		m.statusMsg = fmt.Sprintf("Exported to %s", filepath)
		m.view = viewTableData
		return nil
	}
}

// View renders the UI
func (m Model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	switch m.view {
	case viewConnect:
		return m.viewConnect()
	case viewSelectRegion:
		return m.viewSelectRegion()
	case viewTables:
		return m.viewTables()
	case viewTableData:
		return m.viewTableData()
	case viewItemDetail:
		return m.viewItemDetail()
	case viewCreateItem, viewEditItem:
		return m.viewItemEditor()
	case viewCreateTable:
		return m.viewCreateTable()
	case viewQuery:
		return m.viewQuery()
	case viewConfirmDelete:
		return m.viewConfirmDelete()
	case viewConfirmSave:
		return m.viewConfirmSave()
	case viewConfirmContinueScan:
		return m.viewConfirmContinueScan()
	case viewExport:
		return m.viewExport()
	case viewSchema:
		return m.viewSchema()
	}

	return ""
}

func (m Model) viewConnect() string {
	var b strings.Builder

	logo := ui.LogoStyle.Render("⚡ GoDynamo")
	b.WriteString(lipgloss.Place(m.width, 5, lipgloss.Center, lipgloss.Center, logo))
	b.WriteString("\n\n")

	title := ui.TitleStyle.Render("Connecting to AWS DynamoDB")
	b.WriteString(lipgloss.Place(m.width, 2, lipgloss.Center, lipgloss.Center, title))
	b.WriteString("\n\n")

	content := lipgloss.NewStyle().Width(60).Padding(1, 2).Align(lipgloss.Center)

	var statusContent strings.Builder

	if m.loading {
		statusContent.WriteString("\n")
		statusContent.WriteString(ui.WarningStyle.Render("🔍 Scanning regions for DynamoDB tables..."))
		statusContent.WriteString("\n\n")
		statusContent.WriteString(ui.HelpStyle.Render("Using credentials from ~/.aws or environment"))
		statusContent.WriteString("\n\n")
		statusContent.WriteString(ui.HelpStyle.Render("This may take a few seconds"))
		statusContent.WriteString("\n")
	} else if m.err != nil {
		statusContent.WriteString("\n")
		statusContent.WriteString(ui.ErrorStyle.Render("❌ Connection Failed"))
		statusContent.WriteString("\n\n")
		statusContent.WriteString(ui.ErrorStyle.Render(m.err.Error()))
		statusContent.WriteString("\n\n")
		statusContent.WriteString(ui.HelpStyle.Render("Check your AWS credentials and try again"))
		statusContent.WriteString("\n\n")
		statusContent.WriteString(ui.ButtonFocusedStyle.Render(" Retry "))
	}

	b.WriteString(lipgloss.Place(m.width, 0, lipgloss.Center, lipgloss.Top, content.Render(statusContent.String())))

	// Help
	help := ui.RenderHelp([]ui.KeyBinding{
		{Key: "Enter", Desc: "Retry"},
		{Key: "Ctrl+Q", Desc: "Quit"},
	})
	b.WriteString("\n\n")
	b.WriteString(lipgloss.Place(m.width, 0, lipgloss.Center, lipgloss.Bottom, help))

	return b.String()
}

func (m Model) viewSelectRegion() string {
	var b strings.Builder

	// Logo
	logo := ui.LogoStyle.Render("⚡ GoDynamo")
	b.WriteString(lipgloss.Place(m.width, 5, lipgloss.Center, lipgloss.Center, logo))
	b.WriteString("\n\n")

	title := ui.TitleStyle.Render("🌍 Select Region")
	b.WriteString(lipgloss.Place(m.width, 2, lipgloss.Center, lipgloss.Center, title))
	b.WriteString("\n")

	subtitle := ui.HelpStyle.Render("Found tables in the following regions:")
	b.WriteString(lipgloss.Place(m.width, 1, lipgloss.Center, lipgloss.Center, subtitle))
	b.WriteString("\n\n")

	// Region list
	listStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ui.ColorPrimary).
		Padding(1, 2).
		Width(50)

	var listContent strings.Builder
	for i, region := range m.discoveredRegions {
		item := fmt.Sprintf("%-20s %d tables", region.Region, region.TableCount)
		if i == m.regionList.Selected {
			listContent.WriteString(ui.SelectedStyle.Render("▸ " + item))
		} else {
			listContent.WriteString(ui.ItemStyle.Render("  " + item))
		}
		listContent.WriteString("\n")
	}

	b.WriteString(lipgloss.Place(m.width, 0, lipgloss.Center, lipgloss.Top, listStyle.Render(listContent.String())))
	b.WriteString("\n\n")

	// Status
	if m.statusMsg != "" {
		b.WriteString(lipgloss.Place(m.width, 0, lipgloss.Center, lipgloss.Top, ui.HelpStyle.Render(m.statusMsg)))
		b.WriteString("\n")
	}

	// Help
	help := ui.RenderHelp([]ui.KeyBinding{
		{Key: "↑/↓", Desc: "Navigate"},
		{Key: "Enter", Desc: "Select"},
		{Key: "q", Desc: "Back"},
	})
	b.WriteString("\n")
	b.WriteString(lipgloss.Place(m.width, 0, lipgloss.Center, lipgloss.Bottom, help))

	return b.String()
}

func (m Model) viewTables() string {
	var b strings.Builder

	// Header
	header := ui.TitleStyle.Render("⚡ GoDynamo - Tables")
	b.WriteString(header)
	b.WriteString("\n\n")

	// Region dropdown (if multiple regions)
	if len(m.discoveredRegions) > 1 {
		b.WriteString(ui.HelpStyle.Render("Region:"))
		b.WriteString("\n")

		// Current region button
		regionLabel := fmt.Sprintf(" 🌍 %s (%d tables) ▼ ",
			m.selectedRegion,
			len(m.tables))

		if m.regionDropdownOpen {
			b.WriteString(ui.ButtonFocusedStyle.Render(regionLabel))
		} else {
			b.WriteString(ui.ButtonStyle.Render(regionLabel))
		}

		// Dropdown list
		if m.regionDropdownOpen {
			b.WriteString("\n")
			dropdownStyle := lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(ui.ColorPrimary).
				Padding(0, 1)

			var dropdownContent strings.Builder
			for i, region := range m.discoveredRegions {
				item := fmt.Sprintf("%-15s %d tables", region.Region, region.TableCount)
				if i == m.selectedRegionIdx {
					dropdownContent.WriteString(ui.SelectedStyle.Render("▸ " + item))
				} else {
					dropdownContent.WriteString(ui.ItemStyle.Render("  " + item))
				}
				if i < len(m.discoveredRegions)-1 {
					dropdownContent.WriteString("\n")
				}
			}
			b.WriteString(dropdownStyle.Render(dropdownContent.String()))
		}
	} else if m.selectedRegion != "" {
		// Single region, just show it
		b.WriteString(ui.HelpStyle.Render("Region: "))
		b.WriteString(ui.BadgeStyle.Render(" 🌍 " + m.selectedRegion + " "))
	}
	b.WriteString("\n\n")

	// Search/Filter box
	searchIcon := "🔍 "
	searchContent := m.tableFilter

	searchBoxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(0, 1).
		Width(45)

	if m.tableFilterMode {
		searchBoxStyle = searchBoxStyle.BorderForeground(ui.ColorPrimary)
	} else {
		searchBoxStyle = searchBoxStyle.BorderForeground(ui.ColorTextMuted)
	}

	var searchText string
	if searchContent == "" {
		if m.tableFilterMode {
			searchText = searchIcon + "Type to search..."
		} else {
			searchText = searchIcon + "Press / or type to search"
		}
		b.WriteString(searchBoxStyle.Foreground(ui.ColorTextMuted).Render(searchText))
	} else {
		b.WriteString(searchBoxStyle.Render(searchIcon + searchContent + "▌"))
	}

	// Show filter results count
	if m.tableFilter != "" {
		b.WriteString("  ")
		b.WriteString(ui.HelpStyle.Render(fmt.Sprintf("%d/%d tables", len(m.filteredTables), len(m.tables))))
	}
	b.WriteString("\n\n")

	// Table list with fuzzy highlighting
	listStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ui.ColorPrimary).
		Padding(1, 2).
		Width(m.width - 6).
		Height(m.height - 18)

	var listContent strings.Builder

	if len(m.filteredTables) == 0 {
		if len(m.tables) == 0 {
			listContent.WriteString(ui.HelpStyle.Render("No tables found. Press Ctrl+N to create one."))
		} else {
			listContent.WriteString(ui.HelpStyle.Render("No tables match your search."))
		}
	} else {
		visibleStart := m.tableList.Offset
		visibleEnd := visibleStart + m.height - 20
		if visibleEnd > len(m.filteredTables) {
			visibleEnd = len(m.filteredTables)
		}

		for i := visibleStart; i < visibleEnd; i++ {
			tableName := m.filteredTables[i]
			isSelected := i == m.tableList.Selected

			if isSelected {
				listContent.WriteString(ui.SelectedStyle.Render("▸ " + tableName))
			} else {
				listContent.WriteString(ui.ItemStyle.Render("  " + tableName))
			}
			listContent.WriteString("\n")
		}
	}

	b.WriteString(listStyle.Render(listContent.String()))
	b.WriteString("\n\n")

	// Status
	if m.statusMsg != "" && !m.tableFilterMode {
		b.WriteString(ui.HelpStyle.Render(m.statusMsg))
		b.WriteString("\n")
	}

	// Help
	var helpBindings []ui.KeyBinding
	if m.tableFilterMode {
		helpBindings = append(helpBindings, ui.KeyBinding{Key: "↑/↓", Desc: "Navigate"})
		helpBindings = append(helpBindings, ui.KeyBinding{Key: "Enter", Desc: "Select"})
		helpBindings = append(helpBindings, ui.KeyBinding{Key: "Esc", Desc: "Clear"})
	} else {
		helpBindings = append(helpBindings, ui.KeyBinding{Key: "↑/↓", Desc: "Navigate"})
		helpBindings = append(helpBindings, ui.KeyBinding{Key: "/", Desc: "Search"})
		helpBindings = append(helpBindings, ui.KeyBinding{Key: "Enter", Desc: "Open"})
		if len(m.discoveredRegions) > 1 {
			helpBindings = append(helpBindings, ui.KeyBinding{Key: "Tab", Desc: "Region"})
		}
		helpBindings = append(helpBindings, ui.KeyBinding{Key: "Ctrl+N", Desc: "Create"})
		helpBindings = append(helpBindings, ui.KeyBinding{Key: "Ctrl+R", Desc: "Refresh"})
		helpBindings = append(helpBindings, ui.KeyBinding{Key: "q", Desc: "Back"})
	}

	help := ui.RenderHelp(helpBindings)
	b.WriteString(help)

	return b.String()
}

func (m Model) viewTableData() string {
	var b strings.Builder

	// Header
	header := ui.TitleStyle.Render(fmt.Sprintf("⚡ %s", m.currentTable))
	if m.tableInfo != nil {
		info := fmt.Sprintf(" | PK: %s (%s)", m.tableInfo.PartitionKey, m.tableInfo.PartitionType)
		if m.tableInfo.SortKey != "" {
			info += fmt.Sprintf(" | SK: %s (%s)", m.tableInfo.SortKey, m.tableInfo.SortKeyType)
		}
		header += ui.HelpStyle.Render(info)
	}
	b.WriteString(header)
	b.WriteString("\n\n")

	if m.loading {
		b.WriteString(ui.ContentStyle.Render("Loading..."))
	} else if len(m.items) == 0 {
		b.WriteString(ui.ContentStyle.Render("No items found. Press 'n' to create one."))
	} else {
		b.WriteString(m.dataTable.View())
	}

	b.WriteString("\n\n")

	// Status bar
	status := m.statusMsg

	// Show column position
	if len(m.dataTable.Headers) > 0 {
		colInfo := fmt.Sprintf(" | Col %d/%d", m.dataTable.SelectedCol+1, len(m.dataTable.Headers))
		status += ui.HelpStyle.Render(colInfo)
	}

	filterSummary := m.filterBuilder.GetFilterSummary()
	if filterSummary != "" {
		status += ui.WarningStyle.Render(" | Filter: " + filterSummary)
	}
	if m.lastKey != nil {
		status += ui.HelpStyle.Render(" | More items available (PgDown)")
	}
	b.WriteString(ui.StatusBarStyle.Render(status))
	b.WriteString("\n")

	// Help
	help := ui.RenderHelp([]ui.KeyBinding{
		{Key: "↑↓", Desc: "Rows"},
		{Key: "←→/[]", Desc: "Cols"},
		{Key: "Enter", Desc: "View"},
		{Key: "y", Desc: "Copy"},
		{Key: "n", Desc: "New"},
		{Key: "e", Desc: "Edit"},
		{Key: "d", Desc: "Delete"},
		{Key: "f", Desc: "Filter"},
		{Key: "x", Desc: "Export"},
		{Key: "s", Desc: "Schema"},
		{Key: "q", Desc: "Back"},
	})
	b.WriteString(help)

	return b.String()
}

func (m Model) viewItemDetail() string {
	var b strings.Builder

	// Header
	header := ui.TitleStyle.Render("⚡ Item Details")
	b.WriteString(header)
	b.WriteString("\n\n")

	// Helper info or Search UI
	if m.searchMode {
		b.WriteString(ui.InputFocusedStyle.Render(m.searchInput.View()))

		// Match status
		if m.jsonViewer.TotalMatches > 0 {
			matchStatus := fmt.Sprintf(" %d/%d matches ", m.jsonViewer.CurrentMatch+1, m.jsonViewer.TotalMatches)
			b.WriteString(ui.HelpStyle.Render(matchStatus))
		} else if m.searchInput.Value() != "" {
			b.WriteString(ui.HelpStyle.Render(" No matches "))
		}
	} else {
		// Just help text
		b.WriteString(ui.HelpStyle.Render("Press / to search • n/N to next/prev • e to edit • d to delete"))
	}
	b.WriteString("\n")

	// Content
	b.WriteString(lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ui.ColorSecondary).
		Padding(1, 2).
		Width(m.width - 6).
		Height(m.height - 10).
		Render(m.itemViewport.View()))

	// Footer Help
	help := ui.RenderHelp([]ui.KeyBinding{
		{Key: "q/Esc", Desc: "Back"},
		{Key: "y", Desc: "Copy JSON"},
		{Key: "e", Desc: "Edit"},
		{Key: "d", Desc: "Delete"},
	})
	b.WriteString("\n")
	b.WriteString(lipgloss.Place(m.width, 0, lipgloss.Center, lipgloss.Bottom, help))

	return b.String()
}

func (m Model) viewItemEditor() string {
	var b strings.Builder

	title := "Create Item"
	if m.view == viewEditItem {
		title = "Edit Item"
	}
	header := ui.TitleStyle.Render(title)
	b.WriteString(header)
	b.WriteString("\n\n")

	b.WriteString(ui.HelpStyle.Render("Enter JSON for the item:"))
	b.WriteString("\n\n")

	// Use style without borders for clean copy/paste with mouse
	b.WriteString(ui.ContentNoBorderStyle.Width(m.width - 10).Render(m.itemEditor.View()))
	b.WriteString("\n\n")

	if m.err != nil {
		b.WriteString(ui.ErrorStyle.Render("Error: " + m.err.Error()))
		b.WriteString("\n\n")
	}

	help := ui.RenderHelp([]ui.KeyBinding{
		{Key: "Ctrl+S", Desc: "Save"},
		{Key: "Esc", Desc: "Cancel"},
	})
	b.WriteString(help)

	return b.String()
}

func (m Model) viewCreateTable() string {
	var b strings.Builder

	header := ui.TitleStyle.Render("Create Table")
	b.WriteString(header)
	b.WriteString("\n\n")

	labels := []string{
		"Table Name",
		"Partition Key",
		"Partition Key Type (S/N/B)",
		"Sort Key (optional)",
		"Sort Key Type (S/N/B)",
		"Capacity (if provisioned)",
	}

	for i, input := range m.createTableForm.inputs {
		style := ui.InputStyle
		if i == m.createTableForm.focusIndex {
			style = ui.InputFocusedStyle
		}
		b.WriteString(ui.ItemStyle.Render(labels[i]) + "\n")
		b.WriteString(style.Width(50).Render(input.View()) + "\n\n")
	}

	b.WriteString(ui.ButtonFocusedStyle.Render(" Create Table "))
	b.WriteString("\n\n")

	if m.err != nil {
		b.WriteString(ui.ErrorStyle.Render("Error: " + m.err.Error()))
		b.WriteString("\n\n")
	}

	help := ui.RenderHelp([]ui.KeyBinding{
		{Key: "Tab", Desc: "Next field"},
		{Key: "Enter", Desc: "Create"},
		{Key: "Esc", Desc: "Cancel"},
	})
	b.WriteString(help)

	return b.String()
}

func (m Model) viewQuery() string {
	var b strings.Builder

	b.WriteString(m.filterBuilder.View())
	b.WriteString("\n\n")

	help := ui.RenderHelp([]ui.KeyBinding{
		{Key: "Tab", Desc: "Next"},
		{Key: "↑↓", Desc: "Operator"},
		{Key: "Ctrl+A", Desc: "Add"},
		{Key: "Ctrl+D", Desc: "Remove"},
		{Key: "Enter", Desc: "Apply"},
		{Key: "Ctrl+C", Desc: "Clear"},
		{Key: "Esc", Desc: "Cancel"},
	})
	b.WriteString(help)

	return b.String()
}

func (m Model) viewConfirmDelete() string {
	var b strings.Builder

	content := ui.ModalStyle.Render(
		ui.TitleStyle.Render("⚠️ Confirm Delete") + "\n\n" +
			ui.WarningStyle.Render("Are you sure you want to delete this item?") + "\n\n" +
			ui.HelpStyle.Render("Press Y to confirm, N to cancel"),
	)

	b.WriteString(lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content))

	return b.String()
}

func (m Model) viewConfirmSave() string {
	var b strings.Builder

	content := ui.ModalStyle.Render(
		ui.TitleStyle.Render("💾 Confirm Save") + "\n\n" +
			ui.WarningStyle.Render("Are you sure you want to save these changes?") + "\n\n" +
			ui.HelpStyle.Render("This will update the item in DynamoDB") + "\n\n" +
			ui.HelpStyle.Render("Press Y to confirm, N to cancel"),
	)

	b.WriteString(lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content))

	return b.String()
}

func (m Model) viewConfirmContinueScan() string {
	var b strings.Builder

	content := ui.ModalStyle.Render(
		ui.TitleStyle.Render("⏱️ Scan Timeout") + "\n\n" +
			ui.WarningStyle.Render("The scan has been running for 3 minutes.") + "\n\n" +
			ui.ItemStyle.Render(fmt.Sprintf("Found: %d items", m.scanItemsFound)) + "\n" +
			ui.ItemStyle.Render(fmt.Sprintf("Scanned: %d records", m.scanTotalScanned)) + "\n\n" +
			ui.HelpStyle.Render("The table has more data to scan.") + "\n\n" +
			ui.HelpStyle.Render("Press Y to continue scanning (3 more minutes)") + "\n" +
			ui.HelpStyle.Render("Press N to stop with current results"),
	)

	b.WriteString(lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content))

	return b.String()
}

func (m *Model) updateConfirmContinueScan(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		// Continue scanning
		m.view = viewTableData
		m.loading = true
		m.statusMsg = "Continuing scan..."
		return m, m.continueScan()
	case "n", "N", "esc":
		// Stop scanning, keep current results
		m.view = viewTableData
		m.statusMsg = fmt.Sprintf("Scan stopped. Found %d items (scanned %d records)", m.scanItemsFound, m.scanTotalScanned)
	}
	return m, nil
}

func (m *Model) continueScan() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()

		// Continue from where we left off, but we want to accumulate more items
		targetCount := m.scanItemsFound + int(m.pageSize)

		result, err := m.client.ScanTableContinuous(ctx, m.currentTable, targetCount, m.scanLastKey, m.filterExpr, m.filterNames, m.filterValues)
		if err != nil {
			return errMsg{err}
		}

		// Append new items to existing ones
		allItems := make([]map[string]types.AttributeValue, 0, len(m.items)+len(result.Items))
		allItems = append(allItems, m.items...)
		allItems = append(allItems, result.Items...)

		// Create a combined result
		combinedResult := &dynamo.ContinuousScanResult{
			Items:            allItems,
			LastEvaluatedKey: result.LastEvaluatedKey,
			TotalScanned:     m.scanTotalScanned + result.TotalScanned,
			HasMore:          result.HasMore,
			TimedOut:         result.TimedOut,
		}

		return continuousScanMsg{result: combinedResult, totalScanned: combinedResult.TotalScanned}
	}
}

func (m Model) viewExport() string {
	var b strings.Builder

	content := ui.ModalStyle.Render(
		ui.TitleStyle.Render("📦 Export Data") + "\n\n" +
			ui.ItemStyle.Render(fmt.Sprintf("Export %d items from %s", len(m.items), m.currentTable)) + "\n\n" +
			ui.ButtonStyle.Render("J") + " JSON format\n" +
			ui.ButtonStyle.Render("C") + " CSV format\n\n" +
			ui.HelpStyle.Render("Press Esc to cancel"),
	)

	b.WriteString(lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content))

	return b.String()
}

func (m *Model) updateSchema(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc":
		m.view = viewTableData
	case "y":
		// Copy schema as JSON
		if m.tableInfo != nil && m.tableInfo.RawJSON != "" {
			if err := clipboard.WriteAll(m.tableInfo.RawJSON); err == nil {
				m.statusMsg = "✓ Copied schema to clipboard"
			}
		}
	case "up", "k":
		m.itemViewport.LineUp(3)
	case "down", "j":
		m.itemViewport.LineDown(3)
	case "pgup":
		m.itemViewport.HalfViewUp()
	case "pgdown":
		m.itemViewport.HalfViewDown()
	}
	return m, nil
}

func (m *Model) prepareSchemaView() {
	if m.tableInfo == nil || m.tableInfo.RawJSON == "" {
		return
	}

	// Parse the JSON to get syntax highlighting
	var data interface{}
	json.Unmarshal([]byte(m.tableInfo.RawJSON), &data)

	viewer := ui.NewJSONViewer(data)
	content := viewer.Render()
	m.itemViewport.SetContent(content)
}

func (m Model) viewSchema() string {
	var b strings.Builder

	// Title
	b.WriteString(ui.TitleStyle.Render("📋 Table Schema: " + m.currentTable))
	b.WriteString("\n\n")

	if m.tableInfo == nil {
		b.WriteString(ui.ErrorStyle.Render("Schema not loaded"))
		return b.String()
	}

	// Quick info header
	quickInfo := fmt.Sprintf("Status: %s │ Items: %d │ Size: %s",
		m.tableInfo.Status,
		m.tableInfo.ItemCount,
		formatBytes(m.tableInfo.SizeBytes))
	b.WriteString(ui.HelpStyle.Render(quickInfo))
	b.WriteString("\n\n")

	// JSON content in viewport
	schemaStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ui.ColorPrimary).
		Padding(0, 1).
		Width(m.width - 10).
		Height(m.height - 12)

	b.WriteString(schemaStyle.Render(m.itemViewport.View()))
	b.WriteString("\n\n")

	// Help
	help := ui.RenderHelp([]ui.KeyBinding{
		{Key: "↑/↓", Desc: "Scroll"},
		{Key: "PgUp/PgDn", Desc: "Page"},
		{Key: "y", Desc: "Copy JSON"},
		{Key: "q/Esc", Desc: "Back"},
	})
	b.WriteString(help)

	return b.String()
}

func formatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d bytes", bytes)
	}
}
