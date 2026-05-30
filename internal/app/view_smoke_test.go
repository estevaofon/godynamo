package app

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/godynamo/internal/dynamo"
	"github.com/godynamo/internal/ui"
)

// populatedModel builds a Model with enough in-memory state to exercise the
// data-bearing render paths. It NEVER sets m.client, so no view reaches AWS.
func populatedModel() Model {
	m := New()
	m.width, m.height = 120, 40
	m.currentTable = "Users"
	m.tableInfo = &dynamo.TableInfo{
		Name: "Users", Status: "ACTIVE", ItemCount: 2, SizeBytes: 4096,
		PartitionKey: "id", PartitionType: "S",
	}
	items := []map[string]types.AttributeValue{
		{"id": &types.AttributeValueMemberS{Value: "1"}, "name": &types.AttributeValueMemberS{Value: "alice"}},
		{"id": &types.AttributeValueMemberS{Value: "2"}, "name": &types.AttributeValueMemberS{Value: "bob"}},
	}
	m.handleScanResult(&dynamo.ScanResult{Items: items, Count: 2})
	m.dataTable.SetSize(100, 30)
	m.selectedItem = items[0]
	m.jsonViewer = ui.NewJSONViewer(map[string]interface{}{"id": "1", "name": "alice"})
	return m
}

func TestViewTableDataWithRows(t *testing.T) {
	m := populatedModel()
	m.view = viewTableData
	if out := m.View(); out == "" {
		t.Fatal("viewTableData empty with rows")
	}
}

func TestViewItemDetailWithSearch(t *testing.T) {
	m := populatedModel()
	m.view = viewItemDetail
	m.searchMode = true
	m.searchInput.SetValue("alice")
	m.jsonViewer.SearchQuery = "alice"
	m.jsonViewer.TotalMatches = 1
	_ = m.View() // exercises the searchMode branch (derefs m.jsonViewer)
}

func TestViewSchemaWithTableInfo(t *testing.T) {
	m := populatedModel()
	m.view = viewSchema
	if out := m.View(); out == "" {
		t.Fatal("viewSchema empty with tableInfo")
	}
}
