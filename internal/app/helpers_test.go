package app

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/godynamo/internal/dynamo"
)

func TestFormatBytes(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{512, "512 bytes"},
		{1024, "1.00 KB"},
		{1536, "1.50 KB"},
		{1048576, "1.00 MB"},
		{1073741824, "1.00 GB"},
	}
	for _, c := range cases {
		if got := formatBytes(c.in); got != c.want {
			t.Errorf("formatBytes(%d)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestExtractTextSingleLine(t *testing.T) {
	got := extractText("hello world", 0, 0, 0, 5)
	if got != "hello" {
		t.Fatalf("got %q want %q", got, "hello")
	}
}

func TestExtractTextMultiLine(t *testing.T) {
	got := extractText("abc\ndef\nghi", 0, 1, 2, 2)
	if got != "bc\ndef\ngh" {
		t.Fatalf("got %q want %q", got, "bc\ndef\ngh")
	}
}

func TestExtractTextNormalizesReversedRange(t *testing.T) {
	got := extractText("hello", 0, 5, 0, 0)
	if got != "hello" {
		t.Fatalf("got %q want %q", got, "hello")
	}
}

func TestGetSortedSelectionForward(t *testing.T) {
	sR, sC, eR, eC := getSortedSelection(0, 1, 0, 3)
	if sR != 0 || sC != 1 || eR != 0 || eC != 4 {
		t.Fatalf("got %d,%d,%d,%d", sR, sC, eR, eC)
	}
}

func TestGetSortedSelectionReversed(t *testing.T) {
	sR, sC, eR, eC := getSortedSelection(2, 5, 1, 2)
	if sR != 1 || sC != 2 || eR != 2 || eC != 6 {
		t.Fatalf("got %d,%d,%d,%d", sR, sC, eR, eC)
	}
}

func TestItemsToTableEmpty(t *testing.T) {
	m := New()
	headers, rows := m.itemsToTable(nil)
	if len(headers) != 0 || len(rows) != 0 {
		t.Fatalf("expected empty, got %v / %v", headers, rows)
	}
}

func TestItemsToTableOrdersKeysWithPartitionFirst(t *testing.T) {
	m := New()
	m.tableInfo = &dynamo.TableInfo{PartitionKey: "id", SortKey: "ts"}
	items := []map[string]types.AttributeValue{
		{
			"id":   &types.AttributeValueMemberS{Value: "1"},
			"ts":   &types.AttributeValueMemberN{Value: "100"},
			"name": &types.AttributeValueMemberS{Value: "alice"},
		},
	}
	headers, rows := m.itemsToTable(items)
	if len(headers) != 3 || headers[0] != "id" || headers[1] != "ts" {
		t.Fatalf("headers not ordered pk/sk first: %v", headers)
	}
	if headers[2] != "name" {
		t.Fatalf("third header=%q want name", headers[2])
	}
	if len(rows) != 1 || rows[0][0] != "1" || rows[0][2] != "alice" {
		t.Fatalf("row=%v", rows[0])
	}
}
