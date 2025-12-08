package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

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

// checkAWSCredentials checks if AWS credentials are available
func checkAWSCredentials() bool {
	// Check environment variables
	if os.Getenv("AWS_ACCESS_KEY_ID") != "" && os.Getenv("AWS_SECRET_ACCESS_KEY") != "" {
		return true
	}

	// Check AWS credentials file
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}

	credentialsFile := filepath.Join(home, ".aws", "credentials")
	if _, err := os.Stat(credentialsFile); err == nil {
		return true
	}

	// Check AWS config file (for SSO, etc.)
	configFile := filepath.Join(home, ".aws", "config")
	if _, err := os.Stat(configFile); err == nil {
		return true
	}

	return false
}

// getDefaultRegion returns the default AWS region
func getDefaultRegion() string {
	// Check environment variable
	if region := os.Getenv("AWS_REGION"); region != "" {
		return region
	}
	if region := os.Getenv("AWS_DEFAULT_REGION"); region != "" {
		return region
	}

	// Default to us-east-1
	return "us-east-1"
}

// Messages
type (
	errMsg              struct{ err error }
	tablesLoadedMsg     struct{ tables []string }
	tableInfoMsg        struct{ info *dynamo.TableInfo }
	scanResultMsg       struct{ result *dynamo.ScanResult }
	queryResultMsg      struct{ result *dynamo.QueryResult }
	itemSavedMsg        struct{}
	itemDeletedMsg      struct{}
	tableCreatedMsg     struct{}
	tableDeletedMsg     struct{}
	connectionTestMsg   struct{ 
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
	connForm    connectionForm

	// Current state
	view       viewMode
	focus      focusArea
	err        error
	statusMsg  string
	loading    bool

	// Region discovery
	discoveredRegions   []dynamo.RegionInfo
	regionList          ui.List
	selectedRegion      string
	selectedRegionIdx   int
	regionDropdownOpen  bool

	// Window dimensions
	width  int
	height int

	// Tables
	tables       []string
	tableList    ui.List
	currentTable string
	tableInfo    *dynamo.TableInfo

	// Data view
	dataTable    ui.DataTable
	items        []map[string]types.AttributeValue
	lastKey      map[string]types.AttributeValue
	pageSize     int32

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

	// Create/Edit item
	itemEditor textarea.Model

	// Create table form
	createTableForm createTableForm

	// Confirm delete
	deleteTarget string
	deleteType   string // "item" or "table"

	// Export
	exportFormat string
	exportPath   string
}

type connectionForm struct {
	inputs     []textinput.Model
	focusIndex int
	useLocal   bool
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
		view:     viewConnect,
		focus:    focusSidebar,
		pageSize: 50,
	}

	m.initConnectionForm()
	m.initCreateTableForm()
	m.initFilterBuilder()
	m.initItemEditor()

	m.tableList = ui.NewList("Tables", []string{})
	m.tableList.Height = 30

	m.regionList = ui.NewList("Regions with Tables", []string{})
	m.regionList.Height = 20

	m.dataTable = ui.NewDataTable()

	m.itemViewport = viewport.New(80, 20)

	return m
}

func (m *Model) initConnectionForm() {
	inputs := make([]textinput.Model, 4)

	// Check if AWS credentials exist
	hasAWSCreds := checkAWSCredentials()

	inputs[0] = textinput.New()
	inputs[0].Placeholder = "Leave empty for AWS, or http://localhost:8000 for local"
	if !hasAWSCreds {
		inputs[0].SetValue("http://localhost:8000")
	}
	inputs[0].Focus()

	inputs[1] = textinput.New()
	inputs[1].Placeholder = "us-east-1"
	inputs[1].SetValue(getDefaultRegion())

	inputs[2] = textinput.New()
	inputs[2].Placeholder = "Leave empty to use AWS credentials"
	if !hasAWSCreds {
		inputs[2].SetValue("local")
	}

	inputs[3] = textinput.New()
	inputs[3].Placeholder = "Leave empty to use AWS credentials"
	if !hasAWSCreds {
		inputs[3].SetValue("local")
	}
	inputs[3].EchoMode = textinput.EchoPassword
	inputs[3].EchoCharacter = '•'

	m.connForm = connectionForm{
		inputs:   inputs,
		useLocal: !hasAWSCreds, // Auto-detect: use local only if no AWS creds
	}
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
	ta.SetHeight(15)
	ta.SetWidth(70)
	ta.ShowLineNumbers = true
	m.itemEditor = ta
}

// Init initializes the model
func (m Model) Init() tea.Cmd {
	return textinput.Blink
}

// Update handles messages
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.dataTable.SetSize(msg.Width-35, msg.Height-10)
		m.tableList.Height = msg.Height - 10
		m.itemViewport.Width = msg.Width - 40
		m.itemViewport.Height = msg.Height - 15
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
		case viewCreateItem, viewEditItem:
			return m.updateItemEditor(msg)
		case viewCreateTable:
			return m.updateCreateTable(msg)
		case viewQuery:
			return m.updateQuery(msg)
		case viewConfirmDelete:
			return m.updateConfirmDelete(msg)
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

	case tableDeletedMsg:
		m.statusMsg = "Table deleted successfully"
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
	case "tab", "down":
		if m.connForm.useLocal {
			m.connForm.focusIndex++
			if m.connForm.focusIndex >= len(m.connForm.inputs)+1 {
				m.connForm.focusIndex = 0
			}
			m.updateConnFormFocus()
		}

	case "shift+tab", "up":
		if m.connForm.useLocal {
			m.connForm.focusIndex--
			if m.connForm.focusIndex < 0 {
				m.connForm.focusIndex = len(m.connForm.inputs)
			}
			m.updateConnFormFocus()
		}

	case " ":
		m.connForm.useLocal = !m.connForm.useLocal
		m.err = nil
		// Reset focus when toggling
		m.connForm.focusIndex = len(m.connForm.inputs) // Focus on checkbox

	case "enter":
		m.loading = true
		m.err = nil
		if !m.connForm.useLocal {
			m.statusMsg = "Scanning regions..."
		}
		return m, m.connect()

	default:
		if m.connForm.useLocal && m.connForm.focusIndex < len(m.connForm.inputs) {
			var cmd tea.Cmd
			m.connForm.inputs[m.connForm.focusIndex], cmd = m.connForm.inputs[m.connForm.focusIndex].Update(msg)
			return m, cmd
		}
	}

	return m, nil
}

func (m *Model) updateConnFormFocus() {
	for i := range m.connForm.inputs {
		if i == m.connForm.focusIndex {
			m.connForm.inputs[i].Focus()
		} else {
			m.connForm.inputs[i].Blur()
		}
	}
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

	switch msg.String() {
	case "up", "k":
		m.tableList.MoveUp()
	case "down", "j":
		m.tableList.MoveDown()
	case "enter":
		m.currentTable = m.tableList.GetSelected()
		if m.currentTable != "" {
			m.loading = true
			m.view = viewTableData
			return m, tea.Batch(m.describeTable(), m.scanTable())
		}
	case "c":
		m.view = viewCreateTable
		m.createTableForm.inputs[0].Focus()
		m.createTableForm.focusIndex = 0
	case "r":
		return m, m.loadTables()
	case "tab":
		// Toggle region dropdown if multiple regions
		if len(m.discoveredRegions) > 1 {
			m.regionDropdownOpen = !m.regionDropdownOpen
		}
	case "q", "esc":
		m.view = viewConnect
	}
	return m, nil
}

func (m *Model) updateTableData(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		m.dataTable.MoveUp()
	case "down", "j":
		m.dataTable.MoveDown()
	case "left", "h":
		m.dataTable.MoveLeft()
	case "right", "l":
		m.dataTable.MoveRight()
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
			m.deleteType = "item"
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
	switch msg.String() {
	case "q", "esc":
		m.view = viewTableData
	case "e":
		jsonStr, _ := models.ItemToJSON(m.selectedItem, true)
		m.itemEditor.SetValue(jsonStr)
		m.view = viewEditItem
		m.itemEditor.Focus()
	case "d":
		m.deleteType = "item"
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

func (m *Model) updateItemEditor(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.view = viewTableData
	case "ctrl+s":
		return m, m.saveItem()
	default:
		var cmd tea.Cmd
		m.itemEditor, cmd = m.itemEditor.Update(msg)
		return m, cmd
	}
	return m, nil
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

func (m *Model) updateQuery(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.view = viewTableData
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
	case "tab":
		m.filterBuilder.NextField()
	case "shift+tab":
		m.filterBuilder.PrevField()
	case "up":
		if m.filterBuilder.ActiveField == 1 {
			m.filterBuilder.PrevOperator()
		} else {
			m.filterBuilder.PrevCondition()
		}
	case "down":
		if m.filterBuilder.ActiveField == 1 {
			m.filterBuilder.NextOperator()
		} else {
			m.filterBuilder.NextCondition()
		}
	case "ctrl+a":
		m.filterBuilder.AddCondition()
	case "ctrl+d":
		m.filterBuilder.RemoveCondition()
	case "ctrl+c":
		m.filterBuilder.Clear()
		m.filterExpr = ""
		m.filterNames = nil
		m.filterValues = nil
	default:
		cmd := m.filterBuilder.Update(msg)
		return m, cmd
	}
	return m, nil
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
		if m.deleteType == "item" {
			return m, m.deleteItem()
		} else if m.deleteType == "table" {
			return m, m.deleteTable()
		}
	case "n", "N", "esc":
		if m.deleteType == "item" {
			m.view = viewTableData
		} else {
			m.view = viewTables
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

func (m *Model) connect() tea.Cmd {
	endpoint := m.connForm.inputs[0].Value()
	useLocal := m.connForm.useLocal
	region := m.connForm.inputs[1].Value()
	accessKey := m.connForm.inputs[2].Value()
	secretKey := m.connForm.inputs[3].Value()

	return func() tea.Msg {
		// If using local DynamoDB, connect directly
		if useLocal || endpoint != "" {
			cfg := dynamo.ConnectionConfig{
				Endpoint:  endpoint,
				Region:    region,
				AccessKey: accessKey,
				SecretKey: secretKey,
				UseLocal:  useLocal,
			}

			client, err := dynamo.NewClient(cfg)
			if err != nil {
				return connectionTestMsg{success: false, err: err}
			}

			// Test connection by listing tables
			_, err = client.ListTables(context.Background())
			if err != nil {
				return connectionTestMsg{success: false, err: err}
			}

			return connectionTestMsg{success: true, client: client, region: region}
		}

		// For AWS, discover regions with tables
		regions, err := dynamo.DiscoverRegionsWithTables(context.Background(), false, "")
		if err != nil {
			return connectionTestMsg{success: false, err: err}
		}

		return regionsDiscoveredMsg{regions: regions}
	}
}

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
	m.statusMsg = fmt.Sprintf("Loaded %d items", result.Count)

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

func (m *Model) deleteTable() tea.Cmd {
	return func() tea.Msg {
		err := m.client.DeleteTable(context.Background(), m.deleteTarget)
		if err != nil {
			return errMsg{err}
		}

		return tableDeletedMsg{}
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

	title := ui.TitleStyle.Render("Connect to DynamoDB")
	b.WriteString(lipgloss.Place(m.width, 2, lipgloss.Center, lipgloss.Center, title))
	b.WriteString("\n\n")

	form := lipgloss.NewStyle().Width(60).Padding(1, 2)

	var formContent strings.Builder

	// Show loading state
	if m.loading {
		formContent.WriteString("\n")
		formContent.WriteString(ui.WarningStyle.Render("🔍 Scanning regions for DynamoDB tables..."))
		formContent.WriteString("\n\n")
		formContent.WriteString(ui.HelpStyle.Render("This may take a few seconds"))
		formContent.WriteString("\n")
		b.WriteString(lipgloss.Place(m.width, 0, lipgloss.Center, lipgloss.Top, form.Render(formContent.String())))
		return b.String()
	}

	// Local checkbox first
	checkbox := "[ ]"
	if m.connForm.useLocal {
		checkbox = "[✓]"
	}
	checkStyle := ui.ItemStyle
	if m.connForm.focusIndex == len(m.connForm.inputs) {
		checkStyle = ui.SelectedStyle
	}
	formContent.WriteString(checkStyle.Render(checkbox+" Use Local DynamoDB") + "\n\n")

	if m.connForm.useLocal {
		// Show local-specific fields
		labels := []string{"Endpoint", "Region", "Access Key", "Secret Key"}
		for i, input := range m.connForm.inputs {
			style := ui.InputStyle
			if i == m.connForm.focusIndex {
				style = ui.InputFocusedStyle
			}
			formContent.WriteString(ui.ItemStyle.Render(labels[i]) + "\n")
			formContent.WriteString(style.Width(50).Render(input.View()) + "\n\n")
		}
	} else {
		// AWS mode - auto-detect regions
		formContent.WriteString(ui.HelpStyle.Render("AWS Mode: Regions will be auto-detected") + "\n")
		formContent.WriteString(ui.HelpStyle.Render("Using credentials from ~/.aws or environment") + "\n\n")
	}

	formContent.WriteString(ui.ButtonFocusedStyle.Render(" Connect "))

	b.WriteString(lipgloss.Place(m.width, 0, lipgloss.Center, lipgloss.Top, form.Render(formContent.String())))

	if m.err != nil {
		b.WriteString("\n")
		b.WriteString(lipgloss.Place(m.width, 0, lipgloss.Center, lipgloss.Top, ui.ErrorStyle.Render("Error: "+m.err.Error())))
	}

	// Help
	help := ui.RenderHelp([]ui.KeyBinding{
		{Key: "Space", Desc: "Toggle Local"},
		{Key: "Enter", Desc: "Connect"},
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
		b.WriteString(ui.HelpStyle.Render("Region: "))
		
		// Current region button
		regionLabel := fmt.Sprintf(" 🌍 %s (%d tables) ▼ ", 
			m.selectedRegion, 
			len(m.tables))
		
		if m.regionDropdownOpen {
			b.WriteString(ui.ButtonFocusedStyle.Render(regionLabel))
		} else {
			b.WriteString(ui.ButtonStyle.Render(regionLabel))
		}
		b.WriteString("\n")

		// Dropdown list
		if m.regionDropdownOpen {
			dropdownStyle := lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(ui.ColorPrimary).
				Padding(0, 1).
				MarginLeft(8)

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
			b.WriteString("\n")
		}
		b.WriteString("\n")
	} else if m.selectedRegion != "" {
		// Single region, just show it
		b.WriteString(ui.HelpStyle.Render("Region: "))
		b.WriteString(ui.BadgeStyle.Render(" 🌍 " + m.selectedRegion + " "))
		b.WriteString("\n\n")
	}

	// Table list
	m.tableList.Width = m.width - 4
	content := m.tableList.View()

	if len(m.tables) == 0 {
		content = ui.ContentStyle.Width(m.width - 4).Render("No tables found. Press 'c' to create one.")
	}

	b.WriteString(content)
	b.WriteString("\n\n")

	// Status
	if m.statusMsg != "" {
		b.WriteString(ui.HelpStyle.Render(m.statusMsg))
		b.WriteString("\n")
	}

	// Help
	var helpBindings []ui.KeyBinding
	helpBindings = append(helpBindings, ui.KeyBinding{Key: "↑/↓", Desc: "Navigate"})
	helpBindings = append(helpBindings, ui.KeyBinding{Key: "Enter", Desc: "Open"})
	if len(m.discoveredRegions) > 1 {
		helpBindings = append(helpBindings, ui.KeyBinding{Key: "Tab", Desc: "Region"})
	}
	helpBindings = append(helpBindings, ui.KeyBinding{Key: "c", Desc: "Create"})
	helpBindings = append(helpBindings, ui.KeyBinding{Key: "r", Desc: "Refresh"})
	helpBindings = append(helpBindings, ui.KeyBinding{Key: "q", Desc: "Back"})
	
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
		{Key: "↑↓←→", Desc: "Navigate"},
		{Key: "Enter", Desc: "View"},
		{Key: "y/Y", Desc: "Copy"},
		{Key: "n", Desc: "New"},
		{Key: "e", Desc: "Edit"},
		{Key: "d", Desc: "Delete"},
		{Key: "f", Desc: "Filter"},
		{Key: "s", Desc: "Schema"},
		{Key: "x", Desc: "Export"},
		{Key: "q", Desc: "Back"},
	})
	b.WriteString(help)

	return b.String()
}

func (m Model) viewItemDetail() string {
	var b strings.Builder

	header := ui.TitleStyle.Render("Item Details")
	b.WriteString(header)
	b.WriteString("\n\n")

	b.WriteString(ui.ContentStyle.Width(m.width - 10).Render(m.itemViewport.View()))
	b.WriteString("\n\n")

	help := ui.RenderHelp([]ui.KeyBinding{
		{Key: "↑/↓", Desc: "Scroll"},
		{Key: "y", Desc: "Copy JSON"},
		{Key: "e", Desc: "Edit"},
		{Key: "d", Desc: "Delete"},
		{Key: "q/Esc", Desc: "Back"},
	})
	b.WriteString(help)

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

	b.WriteString(ui.ContentStyle.Width(m.width - 10).Render(m.itemEditor.View()))
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

	var message string
	if m.deleteType == "item" {
		message = "Are you sure you want to delete this item?"
	} else {
		message = fmt.Sprintf("Are you sure you want to delete table '%s'?", m.deleteTarget)
	}

	content := ui.ModalStyle.Render(
		ui.TitleStyle.Render("⚠️ Confirm Delete") + "\n\n" +
			ui.WarningStyle.Render(message) + "\n\n" +
			ui.HelpStyle.Render("Press Y to confirm, N to cancel"),
	)

	b.WriteString(lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content))

	return b.String()
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

