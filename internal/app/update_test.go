package app

import (
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/godynamo/internal/dynamo"
)

// drive feeds one message through Update and returns the updated Model.
// The returned tea.Cmd is intentionally DISCARDED and never executed.
//
// Update returns tea.Model that is sometimes a Model value (message paths like
// WindowSize/errMsg) and sometimes a *Model (the pointer-receiver per-view key
// handlers such as updateTableData). Normalize both to a Model value.
func drive(m Model, msg tea.Msg) Model {
	switch v, _ := m.Update(msg); model := v.(type) {
	case Model:
		return model
	case *Model:
		return *model
	default:
		return m
	}
}

func TestUpdateWindowSizeSetsDimensions(t *testing.T) {
	m := drive(New(), tea.WindowSizeMsg{Width: 120, Height: 40})
	if m.width != 120 || m.height != 40 {
		t.Fatalf("dimensions not set: %d x %d", m.width, m.height)
	}
}

func TestUpdateErrMsgSetsErrorAndStopsLoading(t *testing.T) {
	m := New()
	m.loading = true
	m = drive(m, errMsg{err: errTest})
	if m.err == nil {
		t.Fatal("err not set")
	}
	if m.loading {
		t.Fatal("loading should be false after errMsg")
	}
}

func TestUpdateTablesLoadedPopulatesList(t *testing.T) {
	m := drive(New(), tablesLoadedMsg{tables: []string{"Users", "Orders"}})
	if len(m.tables) != 2 {
		t.Fatalf("tables=%v", m.tables)
	}
	if m.loading {
		t.Fatal("loading should be false after tables load")
	}
}

func TestHandleScanResultPopulatesTable(t *testing.T) {
	m := New()
	m.tableInfo = &dynamo.TableInfo{PartitionKey: "id"}
	m.handleScanResult(&dynamo.ScanResult{
		Items: []map[string]types.AttributeValue{
			{"id": &types.AttributeValueMemberS{Value: "1"}},
			{"id": &types.AttributeValueMemberS{Value: "2"}},
		},
		Count: 2,
	})
	if m.loading {
		t.Fatal("loading should be false")
	}
	if len(m.items) != 2 {
		t.Fatalf("items=%d", len(m.items))
	}
	if len(m.dataTable.Rows) != 2 {
		t.Fatalf("dataTable rows=%d", len(m.dataTable.Rows))
	}
}

func TestHandleContinuousScanResultStatusReflectsTimeout(t *testing.T) {
	m := New()
	m.tableInfo = &dynamo.TableInfo{PartitionKey: "id"}
	m.handleContinuousScanResult(&dynamo.ContinuousScanResult{
		Items:        []map[string]types.AttributeValue{{"id": &types.AttributeValueMemberS{Value: "1"}}},
		TotalScanned: 500,
		TimedOut:     true,
		HasMore:      true,
	})
	if !strings.Contains(m.statusMsg, "Timeout") {
		t.Fatalf("status should mention timeout: %q", m.statusMsg)
	}
}

func TestHandleQueryResultSetsStatus(t *testing.T) {
	m := New()
	m.tableInfo = &dynamo.TableInfo{PartitionKey: "id"}
	m.handleQueryResult(&dynamo.QueryResult{
		Items: []map[string]types.AttributeValue{{"id": &types.AttributeValueMemberS{Value: "1"}}},
		Count: 1,
	})
	if !strings.Contains(m.statusMsg, "1") {
		t.Fatalf("status=%q", m.statusMsg)
	}
}

// View smoke tests: each view must render without panicking once the model has
// minimal state (width/height set so layout math has sane inputs).
func TestViewSmokeAllModes(t *testing.T) {
	modes := []viewMode{
		viewConnect, viewSelectRegion, viewTables, viewTableData,
		viewItemDetail, viewCreateTable, viewQuery, viewExport, viewSchema,
	}
	for _, vm := range modes {
		m := New()
		m.width, m.height = 100, 30
		m.view = vm
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("view %d panicked: %v", vm, r)
				}
			}()
			_ = m.View()
		}()
	}
}

var errTest = testError("test error")

type testError string

func (e testError) Error() string { return string(e) }
